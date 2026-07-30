package monitor

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type procSample struct {
	ticks uint64
	start uint64
}

type rawProcess struct {
	pid     int
	uid     int
	name    string
	command string
	state   string
	ticks   uint64
	start   uint64
	rss     uint64
	threads int
}

var sensitiveArgument = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|access[_-]?key|auth|credential|community)(\s*=\s*|\s+)([^\s]+)`)

func (c *Collector) collectProcesses(now time.Time) ([]Process, error) {
	totalCPU, err := readCPU(filepath.Join(c.cfg.ProcRoot, "stat"))
	if err != nil {
		return nil, err
	}
	users := c.loadUsers()
	entries, err := os.ReadDir(c.cfg.ProcRoot)
	if err != nil {
		return nil, fmt.Errorf("读取主机进程: %w", err)
	}
	var raw []rawProcess
	next := map[int]procSample{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		item, ok := c.readProcess(pid)
		if !ok {
			continue
		}
		raw = append(raw, item)
		next[pid] = procSample{ticks: item.ticks, start: item.start}
	}

	c.mu.Lock()
	oldCPU := c.prevProcCPU
	oldProcs := c.prevProcs
	c.prevProcCPU = totalCPU
	c.prevProcs = next
	c.mu.Unlock()

	totalDelta := uint64(0)
	if totalCPU.total >= oldCPU.total {
		totalDelta = totalCPU.total - oldCPU.total
	}
	cpuCount := countCPUs(filepath.Join(c.cfg.ProcRoot, "stat"))
	memoryTotal, _, _ := readMemory(filepath.Join(c.cfg.ProcRoot, "meminfo"))
	processes := make([]Process, 0, len(raw))
	for _, item := range raw {
		cpuPercent := 0.0
		if old, ok := oldProcs[item.pid]; ok && old.start == item.start && item.ticks >= old.ticks && totalDelta > 0 {
			cpuPercent = float64(item.ticks-old.ticks) * float64(cpuCount) * 100 / float64(totalDelta)
		}
		memoryPercent := 0.0
		if memoryTotal > 0 {
			memoryPercent = float64(item.rss) * 100 / float64(memoryTotal)
		}
		processes = append(processes, Process{
			PID: item.pid, User: users[item.uid], Name: item.name,
			Command: item.command, State: processState(item.state),
			CPUPercent: cpuPercent, MemoryPercent: memoryPercent,
			RSSBytes: item.rss, Threads: item.threads,
		})
	}
	sort.Slice(processes, func(i, j int) bool {
		if processes[i].CPUPercent == processes[j].CPUPercent {
			return processes[i].RSSBytes > processes[j].RSSBytes
		}
		return processes[i].CPUPercent > processes[j].CPUPercent
	})
	if len(processes) > c.cfg.ProcessTopN {
		processes = processes[:c.cfg.ProcessTopN]
	}
	_ = now
	return processes, nil
}

func (c *Collector) readProcess(pid int) (rawProcess, bool) {
	root := filepath.Join(c.cfg.ProcRoot, strconv.Itoa(pid))
	content, err := os.ReadFile(filepath.Join(root, "stat"))
	if err != nil {
		return rawProcess{}, false
	}
	line := strings.TrimSpace(string(content))
	left := strings.IndexByte(line, '(')
	right := strings.LastIndexByte(line, ')')
	if left < 0 || right <= left {
		return rawProcess{}, false
	}
	fields := strings.Fields(line[right+2:])
	if len(fields) < 22 {
		return rawProcess{}, false
	}
	userTicks, err1 := strconv.ParseUint(fields[11], 10, 64)
	systemTicks, err2 := strconv.ParseUint(fields[12], 10, 64)
	threads, err3 := strconv.Atoi(fields[17])
	start, err4 := strconv.ParseUint(fields[19], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return rawProcess{}, false
	}
	uid, rss := readProcessStatus(filepath.Join(root, "status"))
	commandBytes, _ := os.ReadFile(filepath.Join(root, "cmdline"))
	command := strings.TrimSpace(strings.ReplaceAll(string(commandBytes), "\x00", " "))
	name := line[left+1 : right]
	if command == "" {
		command = "[" + name + "]"
	}
	command = sensitiveArgument.ReplaceAllString(command, "$1$2***")
	command = strings.Join(strings.Fields(command), " ")
	if len(command) > c.cfg.CommandLimit {
		command = command[:c.cfg.CommandLimit-1] + "…"
	}
	return rawProcess{
		pid: pid, uid: uid, name: name, command: command, state: fields[0],
		ticks: userTicks + systemTicks, start: start, rss: rss, threads: threads,
	}, true
}

func readProcessStatus(path string) (int, uint64) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	uid := 0
	var rss uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "Uid":
			uid, _ = strconv.Atoi(fields[1])
		case "VmRSS":
			rss, _ = strconv.ParseUint(fields[1], 10, 64)
			rss *= 1024
		}
	}
	return uid, rss
}

func (c *Collector) loadUsers() map[int]string {
	users := map[int]string{}
	for _, path := range []string{
		filepath.Join(c.cfg.ConfigRoot, "passwd"),
		filepath.Join(c.cfg.ProcRoot, "1/root/etc/passwd"),
	} {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Split(scanner.Text(), ":")
			if len(fields) < 3 {
				continue
			}
			uid, err := strconv.Atoi(fields[2])
			if err == nil {
				users[uid] = fields[0]
			}
		}
		file.Close()
		if len(users) > 0 {
			break
		}
	}
	return users
}

func countCPUs(path string) int {
	content, _ := os.ReadFile(path)
	count := 0
	for _, line := range strings.Split(string(content), "\n") {
		if matched, _ := regexp.MatchString(`^cpu[0-9]+\s`, line); matched {
			count++
		}
	}
	if count < 1 {
		return 1
	}
	return count
}

func processState(state string) string {
	states := map[string]string{
		"R": "运行", "S": "睡眠", "D": "不可中断", "Z": "僵尸",
		"T": "已停止", "t": "跟踪停止", "I": "内核空闲",
	}
	if value, ok := states[state]; ok {
		return value
	}
	return state
}
