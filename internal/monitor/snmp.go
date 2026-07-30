package monitor

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

const (
	qnapRoot      = ".1.3.6.1.4.1.24681.1"
	systemInfoEx  = qnapRoot + ".3"
	fanTableEx    = systemInfoEx + ".15.1"
	volumeTableEx = systemInfoEx + ".17.1"
	diskTableV2   = qnapRoot + ".4.1.1.1.1.5.2.1"
)

func (c *Collector) collectSNMP() (System, []Fan, []Disk, []Volume, error) {
	client := &gosnmp.GoSNMP{
		Target:         c.cfg.NASIP,
		Port:           c.cfg.SNMPPort,
		Community:      c.cfg.SNMPCommunity,
		Version:        gosnmp.Version2c,
		Timeout:        3 * time.Second,
		Retries:        1,
		MaxRepetitions: 24,
	}
	if err := client.Connect(); err != nil {
		return System{CPUPercent: -1}, nil, nil, nil, fmt.Errorf("SNMP 连接失败: %w", err)
	}
	defer client.Conn.Close()

	scalars := []string{
		systemInfoEx + ".1.0", systemInfoEx + ".2.0", systemInfoEx + ".3.0",
		systemInfoEx + ".4.0", systemInfoEx + ".5.0", systemInfoEx + ".6.0",
		systemInfoEx + ".12.0", systemInfoEx + ".13.0",
	}
	packet, err := client.Get(scalars)
	if err != nil {
		return System{CPUPercent: -1}, nil, nil, nil, fmt.Errorf("SNMP 系统指标: %w", err)
	}
	system := System{CPUPercent: -1}
	for _, variable := range packet.Variables {
		switch normalizeOID(variable.Name) {
		case normalizeOID(systemInfoEx + ".1.0"):
			system.CPUPercent = number(variable.Value)
		case normalizeOID(systemInfoEx + ".2.0"):
			system.MemoryTotal = uintNumber(variable.Value)
		case normalizeOID(systemInfoEx + ".3.0"):
			free := uintNumber(variable.Value)
			if system.MemoryTotal >= free {
				system.MemoryUsed = system.MemoryTotal - free
				if system.MemoryTotal > 0 {
					system.MemoryPercent = float64(system.MemoryUsed) * 100 / float64(system.MemoryTotal)
				}
			}
		case normalizeOID(systemInfoEx + ".4.0"):
			system.UptimeSeconds = number(variable.Value) / 100
		case normalizeOID(systemInfoEx + ".5.0"):
			system.CPUTemperature = number(variable.Value)
		case normalizeOID(systemInfoEx + ".6.0"):
			system.Temperature = number(variable.Value)
		case normalizeOID(systemInfoEx + ".12.0"):
			system.Model = textValue(variable.Value)
		case normalizeOID(systemInfoEx + ".13.0"):
			system.Hostname = textValue(variable.Value)
		}
	}

	fanRows, fanErr := walkRows(client, fanTableEx)
	diskRows, diskErr := walkRows(client, diskTableV2)
	volumeRows, volumeErr := walkRows(client, volumeTableEx)
	fans := parseFans(fanRows)
	disks := parseDisks(diskRows)
	volumes := parseVolumes(volumeRows)
	if fanErr != nil && diskErr != nil && volumeErr != nil {
		return system, fans, disks, volumes, fmt.Errorf("SNMP 表格不可用: fan=%v; disk=%v; volume=%v", fanErr, diskErr, volumeErr)
	}
	return system, fans, disks, volumes, nil
}

func walkRows(client *gosnmp.GoSNMP, root string) (map[string]map[int]any, error) {
	rows := map[string]map[int]any{}
	root = normalizeOID(root)
	err := client.BulkWalk(root, func(variable gosnmp.SnmpPDU) error {
		name := normalizeOID(variable.Name)
		relative := strings.TrimPrefix(name, root+".")
		parts := strings.Split(relative, ".")
		if len(parts) < 2 {
			return nil
		}
		column := parts[0]
		index, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			return nil
		}
		if rows[column] == nil {
			rows[column] = map[int]any{}
		}
		rows[column][index] = variable.Value
		return nil
	})
	return rows, err
}

func parseFans(rows map[string]map[int]any) []Fan {
	var fans []Fan
	for _, index := range rowIndexes(rows) {
		fans = append(fans, Fan{
			ID: strconv.Itoa(index), Name: textValue(rows["2"][index]),
			Speed: number(rows["3"][index]),
		})
	}
	return fans
}

func parseDisks(rows map[string]map[int]any) []Disk {
	var disks []Disk
	for _, index := range rowIndexes(rows) {
		smart := fmt.Sprint(int64Number(rows["5"][index]))
		switch smart {
		case "0":
			smart = "良好"
		case "1":
			smart = "警告"
		case "2":
			smart = "异常"
		case "-1":
			smart = "错误"
		}
		disks = append(disks, Disk{
			ID: strconv.Itoa(index), Bay: fmt.Sprint(int64Number(rows["2"][index])),
			Model: textValue(rows["8"][index]), Status: textValue(rows["4"][index]),
			Smart: smart, Temperature: number(rows["6"][index]),
			Capacity: uintNumber(rows["9"][index]),
		})
	}
	return disks
}

func parseVolumes(rows map[string]map[int]any) []Volume {
	var volumes []Volume
	for _, index := range rowIndexes(rows) {
		total := uintNumber(rows["4"][index])
		free := uintNumber(rows["5"][index])
		used := uint64(0)
		if total >= free {
			used = total - free
		}
		percent := 0.0
		if total > 0 {
			percent = float64(used) * 100 / float64(total)
		}
		volumes = append(volumes, Volume{
			Name: textValue(rows["2"][index]), Filesystem: textValue(rows["3"][index]),
			Status: textValue(rows["6"][index]), Total: total, Used: used,
			Free: free, Percent: percent,
		})
	}
	return volumes
}

func rowIndexes(rows map[string]map[int]any) []int {
	seen := map[int]bool{}
	for _, values := range rows {
		for index := range values {
			seen[index] = true
		}
	}
	indexes := make([]int, 0, len(seen))
	for index := range seen {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}

func normalizeOID(value string) string {
	if strings.HasPrefix(value, ".") {
		return value
	}
	return "." + value
}

func textValue(value any) string {
	switch typed := value.(type) {
	case []byte:
		return strings.TrimSpace(string(typed))
	case string:
		return strings.TrimSpace(typed)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float32:
		return float64(typed)
	case float64:
		return typed
	case int:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint:
		return float64(typed)
	case uint32:
		return float64(typed)
	case uint64:
		return float64(typed)
	case *big.Int:
		result, _ := strconv.ParseFloat(typed.String(), 64)
		return result
	case []byte:
		return parseNumber(string(typed))
	case string:
		return parseNumber(typed)
	default:
		if integer := gosnmp.ToBigInt(value); integer != nil {
			result, _ := strconv.ParseFloat(integer.String(), 64)
			return result
		}
		return 0
	}
}

func uintNumber(value any) uint64 {
	result := number(value)
	if result <= 0 {
		return 0
	}
	return uint64(result)
}

func int64Number(value any) int64 {
	return int64(number(value))
}

func parseNumber(value string) float64 {
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(value, "%"), " C"))
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	result, _ := strconv.ParseFloat(fields[0], 64)
	return result
}
