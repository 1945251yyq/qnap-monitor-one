package monitor

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr        string
	NASIP             string
	Hostname          string
	SNMPCommunity     string
	SNMPPort          uint16
	Interval          time.Duration
	HistoryRetention  time.Duration
	ProcRoot          string
	SysRoot           string
	ConfigRoot        string
	ShareRoot         string
	DataRoot          string
	ProcessTopN       int
	CommandLimit      int
	IncludeBDFs       map[string]bool
	ExcludeBDFs       map[string]bool
	ShareScanEnabled  bool
	ShareScanInterval time.Duration
}

func ConfigFromEnv() Config {
	return Config{
		ListenAddr:        env("LISTEN_ADDR", ":8080"),
		NASIP:             env("NAS_IP", "127.0.0.1"),
		Hostname:          env("QNAP_HOSTNAME", "QNAP-NAS"),
		SNMPCommunity:     env("SNMP_COMMUNITY", "public"),
		SNMPPort:          uint16(envInt("SNMP_PORT", 161, 1, 65535)),
		Interval:          envDuration("COLLECT_INTERVAL", 10*time.Second),
		HistoryRetention:  envDuration("HISTORY_RETENTION", 30*24*time.Hour),
		ProcRoot:          env("QNAP_PROC_ROOT", "/host-proc"),
		SysRoot:           env("QNAP_SYS_ROOT", "/host-sys"),
		ConfigRoot:        env("QNAP_CONFIG_ROOT", "/host-etc-config"),
		ShareRoot:         env("QNAP_SHARE_ROOT", "/share"),
		DataRoot:          env("DATA_ROOT", "/data"),
		ProcessTopN:       envInt("PROCESS_TOP_N", 15, 5, 50),
		CommandLimit:      envInt("PROCESS_COMMAND_LIMIT", 220, 80, 500),
		IncludeBDFs:       envSet("PCIE_INCLUDE_BDFS"),
		ExcludeBDFs:       envSet("PCIE_EXCLUDE_BDFS"),
		ShareScanEnabled:  envBool("SHARE_SIZE_SCAN", false),
		ShareScanInterval: envDuration("SHARE_SCAN_INTERVAL", time.Hour),
	}
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback, minValue, maxValue int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil {
		return fallback
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envBool(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func envSet(name string) map[string]bool {
	result := map[string]bool{}
	for _, item := range strings.Split(os.Getenv(name), ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" {
			result[item] = true
		}
	}
	return result
}
