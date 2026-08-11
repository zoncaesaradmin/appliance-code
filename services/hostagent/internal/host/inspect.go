package host

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type Info struct {
	Hostname          string        `json:"hostname"`
	OperatingSystem   string        `json:"operatingSystem"`
	KernelVersion     string        `json:"kernelVersion,omitempty"`
	Architecture      string        `json:"architecture"`
	ContainerHostname string        `json:"containerHostname,omitempty"`
	Network           NetworkStatus `json:"network"`
}

type Stats struct {
	UptimeSeconds        float64 `json:"uptimeSeconds"`
	Load1                float64 `json:"load1"`
	Load5                float64 `json:"load5"`
	Load15               float64 `json:"load15"`
	MemoryTotalBytes     uint64  `json:"memoryTotalBytes"`
	MemoryAvailableBytes uint64  `json:"memoryAvailableBytes"`
	FilesystemTotalBytes uint64  `json:"filesystemTotalBytes"`
	FilesystemFreeBytes  uint64  `json:"filesystemFreeBytes"`
	FilesystemAvailBytes uint64  `json:"filesystemAvailableBytes"`
	LogicalCPUCount      int     `json:"logicalCpuCount"`
}

type Health struct {
	Status             string `json:"status"`
	HostRootAccessible bool   `json:"hostRootAccessible"`
	ProcMounted        bool   `json:"procMounted"`
	HostnameReadable   bool   `json:"hostnameReadable"`
	OSReleaseReadable  bool   `json:"osReleaseReadable"`
}

func CollectInfo(root string) (Info, error) {
	hostname := Hostname(root)
	osName := operatingSystem(root)
	kernelVersion := hostKernelVersion(root)
	containerHostname, _ := os.Hostname()
	return Info{
		Hostname:          hostname,
		OperatingSystem:   osName,
		KernelVersion:     kernelVersion,
		Architecture:      runtime.GOARCH,
		ContainerHostname: containerHostname,
		Network:           CollectNetwork(root),
	}, nil
}

func Hostname(root string) string {
	return hostHostname(root)
}

func MDNSAdvertisedName(root string) string {
	hostname := strings.TrimSpace(Hostname(root))
	if hostname == "" {
		return ""
	}
	if i := strings.Index(hostname, "."); i > 0 {
		hostname = hostname[:i]
	}
	hostname = strings.TrimSpace(strings.TrimSuffix(hostname, ".local"))
	if hostname == "" {
		return ""
	}
	return hostname + ".local"
}

func hostHostname(root string) string {
	if hostname, err := readTrimmed(filepath.Join(root, "proc/sys/kernel/hostname")); err == nil && hostname != "" {
		return hostname
	}
	hostname, _ := readTrimmed(filepath.Join(root, "etc/hostname"))
	return hostname
}

func hostKernelVersion(root string) string {
	if version, err := readTrimmed(filepath.Join(root, "proc/sys/kernel/osrelease")); err == nil && version != "" {
		return version
	}
	if version, err := nthField(filepath.Join(root, "proc/version"), 3); err == nil {
		return version
	}
	return ""
}

func CollectStats(root string) (Stats, error) {
	uptime, err := readUptime(filepath.Join(root, "proc/uptime"))
	if err != nil {
		return Stats{}, err
	}
	load1, load5, load15, err := readLoadAvg(filepath.Join(root, "proc/loadavg"))
	if err != nil {
		return Stats{}, err
	}
	memTotal, memAvail, err := readMemInfo(filepath.Join(root, "proc/meminfo"))
	if err != nil {
		return Stats{}, err
	}
	fsTotal, fsFree, fsAvail, err := statFS(root)
	if err != nil {
		return Stats{}, err
	}
	return Stats{
		UptimeSeconds:        uptime,
		Load1:                load1,
		Load5:                load5,
		Load15:               load15,
		MemoryTotalBytes:     memTotal,
		MemoryAvailableBytes: memAvail,
		FilesystemTotalBytes: fsTotal,
		FilesystemFreeBytes:  fsFree,
		FilesystemAvailBytes: fsAvail,
		LogicalCPUCount:      runtime.NumCPU(),
	}, nil
}

func CollectHealth(root string) Health {
	info, infoErr := os.Stat(root)
	_, procErr := os.Stat(filepath.Join(root, "proc"))
	_, hostErr := os.Stat(filepath.Join(root, "etc/hostname"))
	_, osReleaseErr := os.Stat(filepath.Join(root, "etc/os-release"))

	healthy := infoErr == nil && info != nil && info.IsDir() && procErr == nil && hostErr == nil && osReleaseErr == nil
	status := "degraded"
	if healthy {
		status = "ok"
	}
	return Health{
		Status:             status,
		HostRootAccessible: infoErr == nil,
		ProcMounted:        procErr == nil,
		HostnameReadable:   hostErr == nil,
		OSReleaseReadable:  osReleaseErr == nil,
	}
}

func operatingSystem(root string) string {
	values, err := parseEnvFile(filepath.Join(root, "etc/os-release"))
	if err != nil {
		return runtime.GOOS
	}
	if value := strings.TrimSpace(values["PRETTY_NAME"]); value != "" {
		return value
	}
	if value := strings.TrimSpace(values["NAME"]); value != "" {
		return value
	}
	return runtime.GOOS
}

func parseEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = strings.Trim(value, `"`)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func readTrimmed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func nthField(path string, index int) (string, error) {
	text, err := readTrimmed(path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(text)
	if index <= 0 || len(fields) < index {
		return "", fmt.Errorf("field %d missing in %s", index, path)
	}
	return fields[index-1], nil
}

func readUptime(path string) (float64, error) {
	text, err := readTrimmed(path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return 0, fmt.Errorf("proc uptime missing first field")
	}
	return strconv.ParseFloat(fields[0], 64)
}

func readLoadAvg(path string) (float64, float64, float64, error) {
	text, err := readTrimmed(path)
	if err != nil {
		return 0, 0, 0, err
	}
	fields := strings.Fields(text)
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("proc loadavg missing first three fields")
	}
	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, 0, 0, err
	}
	load5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, 0, err
	}
	load15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return 0, 0, 0, err
	}
	return load1, load5, load15, nil
}

func readMemInfo(path string) (uint64, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	var total, available uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total, err = parseMemInfoLine(line)
			if err != nil {
				return 0, 0, err
			}
		case strings.HasPrefix(line, "MemAvailable:"):
			available, err = parseMemInfoLine(line)
			if err != nil {
				return 0, 0, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	if total == 0 {
		return 0, 0, fmt.Errorf("meminfo missing MemTotal")
	}
	return total, available, nil
}

func parseMemInfoLine(line string) (uint64, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, fmt.Errorf("invalid meminfo line %q", line)
	}
	value, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, err
	}
	return value * 1024, nil
}

func statFS(path string) (uint64, uint64, uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, 0, err
	}
	return stat.Blocks * uint64(stat.Bsize), stat.Bfree * uint64(stat.Bsize), stat.Bavail * uint64(stat.Bsize), nil
}
