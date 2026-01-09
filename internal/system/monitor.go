package system

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type Stats struct {
	CPUUsage    float64 `json:"cpuUsage"`
	MemoryUsage float64 `json:"memoryUsage"`
	DiskUsage   float64 `json:"diskUsage"`
	GPUUsage    float64 `json:"gpuUsage"`
	GPUTemp     float64 `json:"gpuTemp"`
	DiskFreeGB  float64 `json:"diskFreeGB"`
}

func GetStats() Stats {
	stats := Stats{}

	stats.CPUUsage = getCPUUsage()
	stats.MemoryUsage = getMemoryUsage()
	stats.DiskUsage, stats.DiskFreeGB = getDiskUsage("/")
	stats.GPUUsage, stats.GPUTemp = getGPUStats()

	return stats
}

func getCPUUsage() float64 {
	// Simple /proc/stat parser for CPU usage
	// We'd need two samples to be accurate, for now let's use load average as a proxy
	// or just a mock if we want it fast. But let's try reading loadavg.
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) > 0 {
		load, _ := strconv.ParseFloat(fields[0], 64)
		// Return load normalized to 100% (assuming multiple cores, this is rough)
		return load * 10
	}
	return 0
}

func getMemoryUsage() float64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()

	var total, available float64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %f kB", &total)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fmt.Sscanf(line, "MemAvailable: %f kB", &available)
		}
	}

	if total > 0 {
		return ((total - available) / total) * 100
	}
	return 0
}

func getDiskUsage(path string) (float64, float64) {
	out, err := exec.Command("df", "-B1", path).Output()
	if err != nil {
		return 0, 0
	}

	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return 0, 0
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return 0, 0
	}

	total, _ := strconv.ParseFloat(fields[1], 64)
	used, _ := strconv.ParseFloat(fields[2], 64)
	avail, _ := strconv.ParseFloat(fields[3], 64)

	if total > 0 {
		return (used / total) * 100, avail / (1024 * 1024 * 1024)
	}
	return 0, 0
}

func getGPUStats() (float64, float64) {
	// Try nvidia-smi
	out, err := exec.Command("nvidia-smi", "--query-gpu=utilization.gpu,temperature.gpu", "--format=csv,noheader,nounits").Output()
	if err == nil {
		fields := strings.Split(strings.TrimSpace(string(out)), ",")
		if len(fields) >= 2 {
			usage, _ := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
			temp, _ := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64)
			return usage, temp
		}
	}
	return 0, 0
}
