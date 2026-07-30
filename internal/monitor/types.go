package monitor

import "time"

type Snapshot struct {
	Timestamp  time.Time         `json:"timestamp"`
	System     System            `json:"system"`
	Fans       []Fan             `json:"fans"`
	Disks      []Disk            `json:"disks"`
	Volumes    []Volume          `json:"volumes"`
	Shares     []Share           `json:"shares"`
	Networks   []Network         `json:"networks"`
	PCIe       []PCIeDevice      `json:"pcie"`
	Processes  []Process         `json:"processes"`
	Collectors map[string]Status `json:"collectors"`
}

type System struct {
	Hostname       string  `json:"hostname"`
	Model          string  `json:"model"`
	Platform       string  `json:"platform"`
	Filesystem     string  `json:"filesystem"`
	VolumeName     string  `json:"volumeName"`
	UptimeSeconds  float64 `json:"uptimeSeconds"`
	CPUPercent     float64 `json:"cpuPercent"`
	CPUTemperature float64 `json:"cpuTemperature"`
	Temperature    float64 `json:"temperature"`
	MemoryTotal    uint64  `json:"memoryTotal"`
	MemoryUsed     uint64  `json:"memoryUsed"`
	MemoryPercent  float64 `json:"memoryPercent"`
	ZFSARCBytes    uint64  `json:"zfsArcBytes"`
}

type Fan struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Speed float64 `json:"speed"`
}

type Disk struct {
	ID          string  `json:"id"`
	Bay         string  `json:"bay"`
	Model       string  `json:"model"`
	Status      string  `json:"status"`
	Smart       string  `json:"smart"`
	Temperature float64 `json:"temperature"`
	Capacity    uint64  `json:"capacity"`
}

type Volume struct {
	Name       string  `json:"name"`
	Path       string  `json:"path"`
	Filesystem string  `json:"filesystem"`
	Status     string  `json:"status"`
	Total      uint64  `json:"total"`
	Used       uint64  `json:"used"`
	Free       uint64  `json:"free"`
	Percent    float64 `json:"percent"`
}

type Share struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    uint64 `json:"size"`
	Scanned bool   `json:"scanned"`
}

type Network struct {
	Name      string  `json:"name"`
	RxBytes   uint64  `json:"rxBytes"`
	TxBytes   uint64  `json:"txBytes"`
	RxPerSec  float64 `json:"rxPerSec"`
	TxPerSec  float64 `json:"txPerSec"`
	LinkUp    bool    `json:"linkUp"`
	SpeedMbps int64   `json:"speedMbps"`
	PCIeBDF   string  `json:"pcieBdf,omitempty"`
}

type PCIeDevice struct {
	BDF         string  `json:"bdf"`
	Slot        string  `json:"slot"`
	Category    string  `json:"category"`
	Vendor      string  `json:"vendor"`
	DeviceID    string  `json:"deviceId"`
	Model       string  `json:"model"`
	Driver      string  `json:"driver"`
	Link        string  `json:"link"`
	Capability  string  `json:"capability"`
	Temperature float64 `json:"temperature,omitempty"`
	PowerWatts  float64 `json:"powerWatts,omitempty"`
	FanRPM      float64 `json:"fanRpm,omitempty"`
	GPUPercent  float64 `json:"gpuPercent,omitempty"`
	MemoryTotal uint64  `json:"memoryTotal,omitempty"`
	MemoryUsed  uint64  `json:"memoryUsed,omitempty"`
	MemoryPct   float64 `json:"memoryPercent,omitempty"`
	IsExpansion bool    `json:"isExpansion"`
}

type Process struct {
	PID           int     `json:"pid"`
	User          string  `json:"user"`
	Name          string  `json:"name"`
	Command       string  `json:"command"`
	State         string  `json:"state"`
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryPercent float64 `json:"memoryPercent"`
	RSSBytes      uint64  `json:"rssBytes"`
	Threads       int     `json:"threads"`
}

type Status struct {
	OK        bool      `json:"ok"`
	Message   string    `json:"message"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type HistoryPoint struct {
	Time          int64   `json:"time"`
	CPU           float64 `json:"cpu"`
	Memory        float64 `json:"memory"`
	CPUTemp       float64 `json:"cpuTemp"`
	SystemTemp    float64 `json:"systemTemp"`
	NetworkRx     float64 `json:"networkRx"`
	NetworkTx     float64 `json:"networkTx"`
	VolumePercent float64 `json:"volumePercent"`
}
