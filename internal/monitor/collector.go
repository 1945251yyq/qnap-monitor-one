package monitor

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type Collector struct {
	cfg           Config
	mu            sync.RWMutex
	latest        Snapshot
	prevCPU       cpuSample
	prevProcCPU   cpuSample
	prevNet       map[string]netSample
	prevProcs     map[int]procSample
	shareSizes    map[string]uint64
	lastShareScan time.Time
	shareScanning bool
	lastSample    time.Time
}

func NewCollector(cfg Config) *Collector {
	return &Collector{
		cfg:        cfg,
		prevNet:    map[string]netSample{},
		prevProcs:  map[int]procSample{},
		shareSizes: map[string]uint64{},
		latest: Snapshot{
			Timestamp:  time.Now(),
			System:     System{Hostname: cfg.Hostname},
			Collectors: map[string]Status{},
		},
	}
}

func (c *Collector) Latest() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

func (c *Collector) Collect(ctx context.Context) Snapshot {
	now := time.Now()
	snapshot := Snapshot{
		Timestamp:  now,
		System:     System{Hostname: c.cfg.Hostname},
		Collectors: map[string]Status{},
	}

	type result struct {
		name  string
		err   error
		apply func(*Snapshot)
	}
	results := make(chan result, 4)
	var wg sync.WaitGroup
	run := func(name string, fn func() (func(*Snapshot), error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			apply, err := fn()
			select {
			case results <- result{name: name, err: err, apply: apply}:
			case <-ctx.Done():
			}
		}()
	}

	run("host", func() (func(*Snapshot), error) {
		system, storage, volumes, shares, networks, err := c.collectHost(now)
		return func(s *Snapshot) {
			s.System = mergeSystem(s.System, system)
			s.Storage = storage
			s.Volumes = volumes
			s.Shares = shares
			s.Networks = networks
		}, err
	})
	run("snmp", func() (func(*Snapshot), error) {
		system, fans, disks, volumes, err := c.collectSNMP()
		return func(s *Snapshot) {
			// Host /proc plus evictable ARC matches QNAP Resource Monitor.
			// Preserve it when SNMP finishes after the host collector.
			hostMemory := s.System.MemoryTotal > 0
			memoryTotal, memoryUsed := s.System.MemoryTotal, s.System.MemoryUsed
			memoryAvailable, memoryPercent := s.System.MemoryAvailable, s.System.MemoryPercent
			arcSize, arcEvictable := s.System.ZFSARCBytes, s.System.ZFSARCEvictableBytes
			s.System = mergeSystem(s.System, system)
			if hostMemory {
				s.System.MemoryTotal, s.System.MemoryUsed = memoryTotal, memoryUsed
				s.System.MemoryAvailable, s.System.MemoryPercent = memoryAvailable, memoryPercent
				s.System.ZFSARCBytes, s.System.ZFSARCEvictableBytes = arcSize, arcEvictable
			}
			s.Fans = fans
			s.Disks = disks
			if len(s.Volumes) == 0 {
				s.Volumes = volumes
			}
		}, err
	})
	run("pcie", func() (func(*Snapshot), error) {
		devices, err := c.collectPCIe()
		return func(s *Snapshot) { s.PCIe = devices }, err
	})
	run("processes", func() (func(*Snapshot), error) {
		processes, err := c.collectProcesses(now)
		return func(s *Snapshot) { s.Processes = processes }, err
	})

	go func() {
		wg.Wait()
		close(results)
	}()
	for item := range results {
		status := Status{OK: item.err == nil, UpdatedAt: now, Message: "正常"}
		if item.err != nil {
			status.Message = item.err.Error()
		}
		if item.apply != nil {
			item.apply(&snapshot)
		}
		snapshot.Collectors[item.name] = status
	}
	if ctx.Err() != nil {
		snapshot.Collectors["scheduler"] = Status{OK: false, Message: ctx.Err().Error(), UpdatedAt: now}
	}

	c.mu.Lock()
	c.latest = snapshot
	c.lastSample = now
	c.mu.Unlock()
	return snapshot
}

func (c *Collector) Run(ctx context.Context, sink func(Snapshot)) {
	collect := func() {
		sampleCtx, cancel := context.WithTimeout(ctx, maxDuration(c.cfg.Interval*8/10, 5*time.Second))
		defer cancel()
		snapshot := c.Collect(sampleCtx)
		if sink != nil {
			sink(snapshot)
		}
		for name, status := range snapshot.Collectors {
			if !status.OK {
				log.Printf("collector %s: %s", name, status.Message)
			}
		}
	}
	collect()
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
		}
	}
}

func mergeSystem(base, value System) System {
	if value.Hostname != "" {
		base.Hostname = value.Hostname
	}
	if value.Model != "" {
		base.Model = value.Model
	}
	if value.Platform != "" {
		base.Platform = value.Platform
	}
	if value.Filesystem != "" {
		base.Filesystem = value.Filesystem
	}
	if value.VolumeName != "" {
		base.VolumeName = value.VolumeName
	}
	if value.UptimeSeconds > 0 {
		base.UptimeSeconds = value.UptimeSeconds
	}
	if value.CPUPercent >= 0 {
		base.CPUPercent = value.CPUPercent
	}
	if value.CPUTemperature > 0 {
		base.CPUTemperature = value.CPUTemperature
	}
	if value.Temperature > 0 {
		base.Temperature = value.Temperature
	}
	if value.MemoryTotal > 0 {
		base.MemoryTotal = value.MemoryTotal
		base.MemoryUsed = value.MemoryUsed
		base.MemoryAvailable = value.MemoryAvailable
		base.MemoryPercent = value.MemoryPercent
	}
	if value.ZFSARCBytes > 0 {
		base.ZFSARCBytes = value.ZFSARCBytes
	}
	if value.ZFSARCEvictableBytes > 0 {
		base.ZFSARCEvictableBytes = value.ZFSARCEvictableBytes
	}
	return base
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func statusError(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
