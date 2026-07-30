package monitor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (c *Collector) collectPCIe() ([]PCIeDevice, error) {
	root := filepath.Join(c.cfg.SysRoot, "bus/pci/devices")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("读取 PCIe: %w", err)
	}
	nvidiaMetricsByBDF := c.collectQNAPNVIDIA()
	var devices []PCIeDevice
	for _, entry := range entries {
		bdf := strings.ToLower(entry.Name())
		dir := filepath.Join(root, entry.Name())
		class := trimHex(readFile(filepath.Join(dir, "class")))
		if len(class) < 2 {
			continue
		}
		baseClass := class[:2]
		if !eligiblePCIClass(baseClass) || strings.HasPrefix(class, "0403") {
			continue
		}
		if c.cfg.ExcludeBDFs[bdf] {
			continue
		}
		vendorID := trimHex(readFile(filepath.Join(dir, "vendor")))
		deviceID := trimHex(readFile(filepath.Join(dir, "device")))
		slot := readFile(filepath.Join(dir, "physical_slot"))
		nvidiaInfo := c.nvidiaInfo(bdf)
		hasDRM := c.hasDedicatedDRM(bdf)
		isExpansion := c.cfg.IncludeBDFs[bdf] || slot != "" || nvidiaInfo != nil || hasDRM
		if !isExpansion {
			continue
		}
		driver := "未加载"
		if value, err := filepath.EvalSymlinks(filepath.Join(dir, "driver")); err == nil {
			driver = filepath.Base(value)
		}
		device := PCIeDevice{
			BDF: bdf, Slot: defaultString(slot, "未标记"), Category: pciCategory(baseClass),
			Vendor: pciVendor(vendorID), DeviceID: deviceID,
			Model: pciVendor(vendorID) + " 0x" + deviceID, Driver: driver,
			Link: pcieLink(dir), Capability: "仅设备识别", IsExpansion: true,
		}
		if nvidiaInfo != nil {
			device.Model = defaultString(nvidiaInfo["Model"], device.Model)
			device.Capability = "基础监控"
		}
		c.readDRMMetrics(bdf, &device)
		c.readHwmon(dir, &device)
		if metrics, ok := nvidiaMetricsByBDF[bdf]; ok {
			applyNVIDIAMetrics(&device, metrics)
		}
		if device.Temperature > 0 || device.PowerWatts > 0 || device.FanRPM > 0 || device.GPUPercent > 0 || device.MemoryTotal > 0 {
			device.Capability = "完整监控"
		}
		devices = append(devices, device)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].BDF < devices[j].BDF })
	return devices, nil
}

type nvidiaMetrics struct {
	model, bdf                                                string
	gpu, memoryTotalMB, memoryUsedMB, temperature, fan, power float64
	encoder, decoder                                          float64
}

func (c *Collector) collectQNAPNVIDIA() map[string]nvidiaMetrics {
	result := map[string]nvidiaMetrics{}
	installPath := qpkgInstallPath(filepath.Join(c.cfg.ConfigRoot, "qpkg.conf"), "NVIDIA_GPU_DRV")
	sharePrefix := filepath.Clean(c.cfg.ShareRoot) + string(os.PathSeparator)
	if installPath == "" || !strings.HasPrefix(filepath.Clean(installPath), sharePrefix) {
		return result
	}
	var binary string
	for _, candidate := range []string{
		"usr/lib/bin/nvidia-smi", "usr/bin/nvidia-smi", "usr/nvidia/bin/nvidia-smi",
	} {
		path := filepath.Join(installPath, candidate)
		if info, err := os.Stat(path); err == nil && info.Mode()&0o111 != 0 {
			binary = path
			break
		}
	}
	if binary == "" {
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary,
		"--query-gpu=pci.bus_id,name,utilization.gpu,memory.total,memory.used,temperature.gpu,fan.speed,power.draw,utilization.encoder,utilization.decoder",
		"--format=csv,noheader,nounits",
	)
	command.Env = append(os.Environ(), "LD_LIBRARY_PATH="+strings.Join([]string{
		filepath.Join(installPath, "usr/lib"),
		filepath.Join(installPath, "usr/lib64"),
		filepath.Join(installPath, "usr/nvidia/lib64"),
	}, ":"))
	output, err := command.Output()
	if err != nil {
		return result
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Split(line, ",")
		if len(fields) < 10 {
			continue
		}
		for index := range fields {
			fields[index] = strings.TrimSpace(fields[index])
		}
		bdf := normalizeNVIDIABDF(fields[0])
		if bdf == "" {
			continue
		}
		result[bdf] = nvidiaMetrics{
			bdf: bdf, model: fields[1], gpu: parseFloat(fields[2]),
			memoryTotalMB: parseFloat(fields[3]), memoryUsedMB: parseFloat(fields[4]),
			temperature: parseFloat(fields[5]), fan: parseFloat(fields[6]),
			power: parseFloat(fields[7]), encoder: parseFloat(fields[8]), decoder: parseFloat(fields[9]),
		}
	}
	return result
}

func qpkgInstallPath(path, packageName string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	inSection := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inSection = line == "["+packageName+"]"
			continue
		}
		if !inSection {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "Install_Path") {
			return strings.Trim(strings.TrimSpace(value), "\"")
		}
	}
	return ""
}

func normalizeNVIDIABDF(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "00000000:")
	if strings.Count(value, ":") == 1 {
		value = "0000:" + value
	}
	return value
}

func applyNVIDIAMetrics(device *PCIeDevice, metrics nvidiaMetrics) {
	device.Model = defaultString(metrics.model, device.Model)
	device.GPUPercent = metrics.gpu
	device.MemoryTotal = uint64(metrics.memoryTotalMB * 1024 * 1024)
	device.MemoryUsed = uint64(metrics.memoryUsedMB * 1024 * 1024)
	if device.MemoryTotal > 0 {
		device.MemoryPct = float64(device.MemoryUsed) * 100 / float64(device.MemoryTotal)
	}
	device.Temperature = metrics.temperature
	device.FanPercent = metrics.fan
	device.PowerWatts = metrics.power
	device.EncoderPct = metrics.encoder
	device.DecoderPct = metrics.decoder
	device.Capability = "QNAP NVIDIA 完整监控"
}

func eligiblePCIClass(class string) bool {
	switch class {
	case "01", "02", "03", "04", "10", "12", "13":
		return true
	default:
		return false
	}
}

func pciCategory(class string) string {
	return map[string]string{
		"01": "存储控制器", "02": "网卡", "03": "显卡", "04": "多媒体设备",
		"10": "加密设备", "12": "AI 加速器", "13": "仪器设备",
	}[class]
}

func pciVendor(id string) string {
	if value, ok := map[string]string{
		"10de": "NVIDIA", "1002": "AMD", "1022": "AMD", "8086": "Intel",
		"15b3": "NVIDIA-Mellanox", "14e4": "Broadcom", "1000": "Broadcom-LSI",
		"1b4b": "Marvell", "11ab": "Marvell", "1d6a": "Aquantia",
		"10ec": "Realtek", "1c5c": "SK hynix", "144d": "Samsung",
		"1987": "Phison", "1e60": "Hailo", "1ac1": "Google",
	}[id]; ok {
		return value
	}
	return "PCI-" + id
}

func (c *Collector) nvidiaInfo(bdf string) map[string]string {
	paths := []string{
		filepath.Join(c.cfg.ProcRoot, "driver/nvidia/gpus", bdf, "information"),
		filepath.Join(c.cfg.ProcRoot, "driver/nvidia/gpus", "00000000:"+strings.TrimPrefix(bdf, "0000:"), "information"),
	}
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		result := map[string]string{}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			key, value, ok := strings.Cut(scanner.Text(), ":")
			if ok {
				result[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
		file.Close()
		return result
	}
	return nil
}

func (c *Collector) hasDedicatedDRM(bdf string) bool {
	for _, dir := range c.drmDeviceDirs(bdf) {
		value, _ := strconv.ParseUint(readFile(filepath.Join(dir, "mem_info_vram_total")), 10, 64)
		if value > 0 {
			return true
		}
	}
	return false
}

func (c *Collector) drmDeviceDirs(bdf string) []string {
	pattern := filepath.Join(c.cfg.SysRoot, "class/drm/card[0-9]*/device")
	paths, _ := filepath.Glob(pattern)
	var result []string
	for _, path := range paths {
		target, err := filepath.EvalSymlinks(path)
		if err == nil && filepath.Base(target) == bdf {
			result = append(result, path)
		}
	}
	return result
}

func (c *Collector) readDRMMetrics(bdf string, device *PCIeDevice) {
	for _, dir := range c.drmDeviceDirs(bdf) {
		device.GPUPercent = parseFloat(readFile(filepath.Join(dir, "gpu_busy_percent")))
		device.MemoryTotal, _ = strconv.ParseUint(readFile(filepath.Join(dir, "mem_info_vram_total")), 10, 64)
		device.MemoryUsed, _ = strconv.ParseUint(readFile(filepath.Join(dir, "mem_info_vram_used")), 10, 64)
		if device.MemoryTotal > 0 {
			device.MemoryPct = float64(device.MemoryUsed) * 100 / float64(device.MemoryTotal)
		}
		return
	}
}

func (c *Collector) readHwmon(deviceDir string, device *PCIeDevice) {
	patterns := []string{
		filepath.Join(deviceDir, "hwmon/hwmon*"),
		filepath.Join(deviceDir, "*/hwmon/hwmon*"),
	}
	for _, pattern := range patterns {
		dirs, _ := filepath.Glob(pattern)
		for _, dir := range dirs {
			if device.Temperature == 0 {
				device.Temperature = firstMetric(dir, "temp*_input", 1000)
			}
			if device.FanRPM == 0 {
				device.FanRPM = firstMetric(dir, "fan*_input", 1)
			}
			if device.PowerWatts == 0 {
				device.PowerWatts = firstMetric(dir, "power*_average", 1_000_000)
				if device.PowerWatts == 0 {
					device.PowerWatts = firstMetric(dir, "power*_input", 1_000_000)
				}
			}
		}
	}
}

func firstMetric(dir, pattern string, divisor float64) float64 {
	files, _ := filepath.Glob(filepath.Join(dir, pattern))
	for _, path := range files {
		value := parseFloat(readFile(path))
		if value != 0 {
			return value / divisor
		}
	}
	return 0
}

func pcieLink(dir string) string {
	speed := readFile(filepath.Join(dir, "current_link_speed"))
	width := readFile(filepath.Join(dir, "current_link_width"))
	if speed == "" && width == "" {
		return "未提供"
	}
	return fmt.Sprintf("%s x%s", defaultString(speed, "未知"), defaultString(width, "?"))
}

func readFile(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

func trimHex(value string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "0x"))
}

func parseFloat(value string) float64 {
	result, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return result
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
