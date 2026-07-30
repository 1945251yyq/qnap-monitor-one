package monitor

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type cpuSample struct {
	total uint64
	idle  uint64
}

type netSample struct {
	rx   uint64
	tx   uint64
	time time.Time
}

func (c *Collector) collectHost(now time.Time) (System, []Volume, []Share, []Network, error) {
	system := System{Hostname: c.cfg.Hostname, CPUPercent: -1}
	var errs []string

	currentCPU, err := readCPU(filepath.Join(c.cfg.ProcRoot, "stat"))
	if err != nil {
		errs = append(errs, err.Error())
	} else {
		c.mu.Lock()
		if c.prevCPU.total > 0 && currentCPU.total > c.prevCPU.total {
			totalDelta := currentCPU.total - c.prevCPU.total
			idleDelta := currentCPU.idle - c.prevCPU.idle
			system.CPUPercent = 100 * float64(totalDelta-idleDelta) / float64(totalDelta)
		}
		c.prevCPU = currentCPU
		c.mu.Unlock()
	}

	total, available, err := readMemory(filepath.Join(c.cfg.ProcRoot, "meminfo"))
	if err != nil {
		errs = append(errs, err.Error())
	} else {
		system.MemoryTotal = total
		system.MemoryUsed = total - available
		if total > 0 {
			system.MemoryPercent = float64(system.MemoryUsed) * 100 / float64(total)
		}
	}
	if uptime, err := readFloatFile(filepath.Join(c.cfg.ProcRoot, "uptime")); err == nil {
		system.UptimeSeconds = uptime
	}
	system.ZFSARCBytes = readARC(c.cfg.ProcRoot)
	volumes := c.collectVolumes(&system)
	shares := c.collectShares()
	networks, err := c.collectNetworks(now)
	if err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return system, volumes, shares, networks, errors.New(strings.Join(errs, "; "))
	}
	return system, volumes, shares, networks, nil
}

func readCPU(path string) (cpuSample, error) {
	file, err := os.Open(path)
	if err != nil {
		return cpuSample{}, fmt.Errorf("读取主机 CPU: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return cpuSample{}, errors.New("主机 /proc/stat 为空")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuSample{}, errors.New("主机 /proc/stat 格式异常")
	}
	var values []uint64
	for _, field := range fields[1:] {
		value, _ := strconv.ParseUint(field, 10, 64)
		values = append(values, value)
	}
	var total uint64
	for index, value := range values {
		if index == 8 || index == 9 {
			continue
		}
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return cpuSample{total: total, idle: idle}, nil
}

func readMemory(path string) (uint64, uint64, error) {
	values := map[string]uint64{}
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("读取主机内存: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, _ := strconv.ParseUint(fields[1], 10, 64)
		values[strings.TrimSuffix(fields[0], ":")] = value * 1024
	}
	total := values["MemTotal"]
	available := values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	if total == 0 {
		return 0, 0, errors.New("主机 MemTotal 不可用")
	}
	return total, minUint64(available, total), nil
}

func readFloatFile(path string) (float64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(content))
	if len(fields) == 0 {
		return 0, errors.New("数值文件为空")
	}
	return strconv.ParseFloat(fields[0], 64)
}

func readARC(procRoot string) uint64 {
	for _, path := range []string{
		filepath.Join(procRoot, "spl/kstat/zfs/arcstats"),
		filepath.Join(procRoot, "lpl/kstat/zfs/arcstats"),
	} {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 3 && fields[0] == "size" {
				value, _ := strconv.ParseUint(fields[len(fields)-1], 10, 64)
				file.Close()
				return value
			}
		}
		file.Close()
	}
	return 0
}

func (c *Collector) collectVolumes(system *System) []Volume {
	patterns := []string{
		filepath.Join(c.cfg.ShareRoot, "ZFS*_DATA"),
		filepath.Join(c.cfg.ShareRoot, "CACHEDEV*_DATA"),
	}
	var paths []string
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		paths = append(paths, matches...)
	}
	seen := map[string]bool{}
	var volumes []Volume
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		var stats syscall.Statfs_t
		if err := syscall.Statfs(path, &stats); err != nil {
			continue
		}
		total := stats.Blocks * uint64(stats.Bsize)
		free := stats.Bavail * uint64(stats.Bsize)
		used := total - free
		percent := 0.0
		if total > 0 {
			percent = float64(used) * 100 / float64(total)
		}
		name := filepath.Base(path)
		fs := "ext"
		if strings.HasPrefix(name, "ZFS") {
			fs = "zfs"
		}
		volumes = append(volumes, Volume{
			Name: name, Path: path, Filesystem: fs, Status: "Ready",
			Total: total, Used: used, Free: free, Percent: percent,
		})
		if system.VolumeName == "" {
			system.VolumeName = name
			system.Filesystem = fs
			if fs == "zfs" {
				system.Platform = "QuTS hero"
			} else {
				system.Platform = "QTS"
			}
		}
	}
	if system.Platform == "" {
		system.Platform = "QTS/QuTS"
	}
	return volumes
}

func (c *Collector) collectShares() []Share {
	file, err := os.Open(filepath.Join(c.cfg.ConfigRoot, "smb.conf"))
	if err != nil {
		return nil
	}
	defer file.Close()
	var shares []Share
	current := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			continue
		}
		if current == "" || strings.EqualFold(current, "global") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "path") {
			path := strings.TrimSpace(value)
			shares = append(shares, Share{Name: current, Path: path})
			current = ""
		}
	}
	c.mu.RLock()
	cached := make(map[string]uint64, len(c.shareSizes))
	for path, size := range c.shareSizes {
		cached[path] = size
	}
	lastScan := c.lastShareScan
	c.mu.RUnlock()
	for index := range shares {
		if size, ok := cached[shares[index].Path]; ok {
			shares[index].Size = size
			shares[index].Scanned = true
		}
	}
	if c.cfg.ShareScanEnabled && (lastScan.IsZero() || time.Since(lastScan) >= c.cfg.ShareScanInterval) {
		next := map[string]uint64{}
		for index := range shares {
			size := directorySize(shares[index].Path)
			next[shares[index].Path] = size
			shares[index].Size = size
			shares[index].Scanned = true
		}
		c.mu.Lock()
		c.shareSizes = next
		c.lastShareScan = time.Now()
		c.mu.Unlock()
	}
	return shares
}

func directorySize(root string) uint64 {
	var total uint64
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += uint64(info.Size())
		}
		return nil
	})
	return total
}

func (c *Collector) collectNetworks(now time.Time) ([]Network, error) {
	file, err := os.Open(filepath.Join(c.cfg.ProcRoot, "net/dev"))
	if err != nil {
		return nil, fmt.Errorf("读取主机网络: %w", err)
	}
	defer file.Close()
	var networks []Network
	next := map[string]netSample{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, ":") {
			continue
		}
		namePart, valuesPart, _ := strings.Cut(line, ":")
		name := strings.TrimSpace(namePart)
		if name == "lo" || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "veth") {
			continue
		}
		fields := strings.Fields(valuesPart)
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		item := Network{Name: name, RxBytes: rx, TxBytes: tx}
		if old, ok := c.prevNet[name]; ok && now.After(old.time) {
			seconds := now.Sub(old.time).Seconds()
			if rx >= old.rx {
				item.RxPerSec = float64(rx-old.rx) / seconds
			}
			if tx >= old.tx {
				item.TxPerSec = float64(tx-old.tx) / seconds
			}
		}
		operstate, _ := os.ReadFile(filepath.Join(c.cfg.SysRoot, "class/net", name, "operstate"))
		item.LinkUp = strings.TrimSpace(string(operstate)) == "up"
		speed, _ := os.ReadFile(filepath.Join(c.cfg.SysRoot, "class/net", name, "speed"))
		item.SpeedMbps, _ = strconv.ParseInt(strings.TrimSpace(string(speed)), 10, 64)
		device, _ := filepath.EvalSymlinks(filepath.Join(c.cfg.SysRoot, "class/net", name, "device"))
		item.PCIeBDF = filepath.Base(device)
		next[name] = netSample{rx: rx, tx: tx, time: now}
		networks = append(networks, item)
	}
	c.mu.Lock()
	c.prevNet = next
	c.mu.Unlock()
	return networks, scanner.Err()
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
