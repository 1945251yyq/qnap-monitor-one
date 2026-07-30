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

func (c *Collector) collectHost(now time.Time) (System, StorageSummary, []Volume, []Share, []Network, error) {
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

	total, available, arcSize, arcEvictable, err := readHostMemory(
		filepath.Join(c.cfg.ProcRoot, "meminfo"), c.cfg.ProcRoot,
	)
	if err != nil {
		errs = append(errs, err.Error())
	} else {
		system.MemoryTotal = total
		system.MemoryUsed = total - available
		system.MemoryAvailable = available
		if total > 0 {
			system.MemoryPercent = float64(system.MemoryUsed) * 100 / float64(total)
		}
	}
	if uptime, err := readFloatFile(filepath.Join(c.cfg.ProcRoot, "uptime")); err == nil {
		system.UptimeSeconds = uptime
	}
	system.ZFSARCBytes = arcSize
	system.ZFSARCEvictableBytes = arcEvictable
	volumes := c.collectVolumes(&system)
	shares := c.collectShares()
	storage := c.summarizeStorage(shares, volumes)
	networks, err := c.collectNetworks(now)
	if err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return system, storage, volumes, shares, networks, errors.New(strings.Join(errs, "; "))
	}
	return system, storage, volumes, shares, networks, nil
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
	return readMemoryValues(path, 0)
}

func readHostMemory(path, procRoot string) (uint64, uint64, uint64, uint64, error) {
	arcSize, arcEvictable := readARC(procRoot)
	total, available, err := readMemoryValues(path, arcEvictable)
	return total, available, arcSize, arcEvictable, err
}

func readMemoryValues(path string, arcEvictable uint64) (uint64, uint64, error) {
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
	// Match QNAP Resource Monitor: page cache, buffers and evictable ZFS ARC
	// are reclaimable, so they are shown as available rather than used RAM.
	available := values["MemFree"] + values["Buffers"] + values["Cached"] + arcEvictable
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

func readARC(procRoot string) (uint64, uint64) {
	for _, path := range []string{
		filepath.Join(procRoot, "spl/kstat/zfs/arcstats"),
		filepath.Join(procRoot, "lpl/kstat/zfs/arcstats"),
	} {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		var size, evictable uint64
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 3 && (fields[0] == "size" || fields[0] == "evictable") {
				value, _ := strconv.ParseUint(fields[len(fields)-1], 10, 64)
				if fields[0] == "size" {
					size = value
				} else {
					evictable = value
				}
			}
		}
		file.Close()
		if size > 0 || evictable > 0 {
			return size, evictable
		}
	}
	return 0, 0
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
			path := strings.Trim(strings.TrimSpace(value), `"`)
			if !strings.HasPrefix(path, "/share/") || strings.Contains(path, "%") {
				current = ""
				continue
			}
			mountedPath := filepath.Join(c.cfg.ShareRoot, strings.TrimPrefix(path, "/share/"))
			realPath, realErr := filepath.EvalSymlinks(mountedPath)
			if realErr != nil {
				current = ""
				continue
			}
			realShareRoot, rootErr := filepath.EvalSymlinks(c.cfg.ShareRoot)
			if rootErr != nil {
				realShareRoot = filepath.Clean(c.cfg.ShareRoot)
			}
			relative := strings.TrimPrefix(realPath, realShareRoot+string(os.PathSeparator))
			volumeName := strings.SplitN(relative, string(os.PathSeparator), 2)[0]
			if !isQNAPVolumeName(volumeName) {
				current = ""
				continue
			}
			shares = append(shares, Share{
				Name: current, Path: path, RealPath: realPath,
				VolumeName: volumeName, Included: true,
			})
			current = ""
		}
	}
	shares = uniqueShares(shares)
	c.mu.RLock()
	cached := make(map[string]uint64, len(c.shareSizes))
	for path, size := range c.shareSizes {
		cached[path] = size
	}
	lastScan := c.lastShareScan
	scanning := c.shareScanning
	c.mu.RUnlock()
	markIncludedShares(shares)
	for index := range shares {
		if size, ok := cached[shares[index].Path]; ok {
			shares[index].Size = size
			shares[index].Scanned = true
		}
	}
	if c.cfg.ShareScanEnabled && !scanning && (lastScan.IsZero() || time.Since(lastScan) >= c.cfg.ShareScanInterval) {
		c.mu.Lock()
		if !c.shareScanning {
			c.shareScanning = true
			scanTargets := append([]Share(nil), shares...)
			go c.scanShareSizes(scanTargets)
		}
		c.mu.Unlock()
	}
	return shares
}

func (c *Collector) scanShareSizes(shares []Share) {
	next := make(map[string]uint64, len(shares))
	for _, share := range shares {
		next[share.Path] = directorySize(share.RealPath)
	}
	c.mu.Lock()
	c.shareSizes = next
	c.lastShareScan = time.Now()
	c.shareScanning = false
	c.mu.Unlock()
}

func isQNAPVolumeName(name string) bool {
	return (strings.HasPrefix(name, "ZFS") || strings.HasPrefix(name, "CACHEDEV")) &&
		strings.HasSuffix(name, "_DATA")
}

func uniqueShares(shares []Share) []Share {
	seen := map[string]bool{}
	result := make([]Share, 0, len(shares))
	for _, share := range shares {
		if seen[share.RealPath] {
			continue
		}
		seen[share.RealPath] = true
		result = append(result, share)
	}
	return result
}

func markIncludedShares(shares []Share) {
	for index := range shares {
		shares[index].Included = true
		for other := range shares {
			if index == other {
				continue
			}
			parent := filepath.Clean(shares[other].RealPath) + string(os.PathSeparator)
			if strings.HasPrefix(filepath.Clean(shares[index].RealPath), parent) {
				shares[index].Included = false
				break
			}
		}
	}
}

func (c *Collector) summarizeStorage(shares []Share, volumes []Volume) StorageSummary {
	summary := StorageSummary{ShareCount: len(shares), ScanComplete: len(shares) > 0}
	volumeByName := make(map[string]Volume, len(volumes))
	for _, volume := range volumes {
		volumeByName[volume.Name] = volume
	}
	pools := map[string]uint64{}
	for _, share := range shares {
		volume, ok := volumeByName[share.VolumeName]
		if !ok {
			continue
		}
		key := c.storagePoolKey(volume)
		if volume.Total > pools[key] {
			pools[key] = volume.Total
		}
		if !share.Scanned {
			summary.ScanComplete = false
		} else if share.Included {
			summary.Used += share.Size
		}
	}
	for _, total := range pools {
		summary.Total += total
	}
	summary.PoolCount = len(pools)
	if summary.Total == 0 && len(volumes) > 0 {
		summary.Total = volumes[0].Total
		summary.PoolCount = 1
	}
	if !summary.ScanComplete {
		// Until the folder scan is complete, a statfs value is more honest than
		// displaying zero used space.
		for _, volume := range volumes {
			if summary.Used == 0 && volume.Used > 0 {
				summary.Used = volume.Used
			}
		}
	}
	if summary.Used > summary.Total {
		summary.Used = summary.Total
	}
	summary.Free = summary.Total - summary.Used
	if summary.Total > 0 {
		summary.Percent = float64(summary.Used) * 100 / float64(summary.Total)
	}
	return summary
}

func (c *Collector) storagePoolKey(volume Volume) string {
	file, err := os.Open(filepath.Join(c.cfg.ProcRoot, "mounts"))
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 3 {
				continue
			}
			mountpoint := strings.ReplaceAll(fields[1], `\040`, " ")
			if filepath.Clean(mountpoint) != filepath.Clean(volume.Path) {
				continue
			}
			source := strings.ReplaceAll(fields[0], `\040`, " ")
			if strings.EqualFold(fields[2], "zfs") {
				return "zfs:" + strings.SplitN(source, "/", 2)[0]
			}
			return "source:" + source
		}
	}
	// QNAP maps the datasets of one QuTS pool to the same apparent capacity.
	// Capacity is a safer fallback than multiplying every ZFS*_DATA dataset.
	if volume.Filesystem == "zfs" {
		return fmt.Sprintf("zfs-capacity:%d", volume.Total)
	}
	return "volume:" + volume.Name
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
