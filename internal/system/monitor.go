package system

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Stats struct {
	CPUUsage    float64 `json:"cpuUsage"`
	MemoryUsage float64 `json:"memoryUsage"`
	DiskUsage   float64 `json:"diskUsage"`
	GPUUsage    float64 `json:"gpuUsage"`
	GPUTemp     float64 `json:"gpuTemp"`
	DiskFreeGB  float64 `json:"diskFreeGB"`
}

type DashboardStats struct {
	TotalStorageSaved     int64   `json:"totalStorageSaved"`
	TotalAIJobs           int     `json:"totalAIJobs"`
	TotalSubtitlesCreated int     `json:"totalSubtitlesCreated"`
	TotalUpscales         int     `json:"totalUpscales"`
	TotalCleaned          int     `json:"totalCleaned"`
	EfficiencyScore       float64 `json:"efficiencyScore"`
}

func GetStats() Stats {
	stats := Stats{}

	stats.CPUUsage = getCPUUsage()
	stats.MemoryUsage = getMemoryUsage()
	stats.DiskUsage, stats.DiskFreeGB = getDiskUsage("/")
	stats.GPUUsage, stats.GPUTemp = getGPUStats()

	return stats
}

// cpuSample holds a single /proc/stat aggregate reading.
type cpuSample struct {
	total float64
	idle  float64
}

var (
	cpuMu         sync.Mutex
	lastCPUSample *cpuSample
	lastCPUTime   time.Time
)

func readCPUSample() (*cpuSample, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		// Fields: cpu user nice system idle iowait irq softirq steal guest guest_nice
		if len(fields) < 5 {
			break
		}
		var vals [10]float64
		for i := 1; i < len(fields) && i <= 10; i++ {
			vals[i-1], _ = strconv.ParseFloat(fields[i], 64)
		}
		idle := vals[3] + vals[4] // idle + iowait
		total := 0.0
		for _, v := range vals {
			total += v
		}
		return &cpuSample{total: total, idle: idle}, nil
	}
	return nil, fmt.Errorf("cpu line not found in /proc/stat")
}

func getCPUUsage() float64 {
	cpuMu.Lock()
	defer cpuMu.Unlock()

	sample, err := readCPUSample()
	if err != nil {
		return 0
	}

	now := time.Now()

	if lastCPUSample == nil || now.Sub(lastCPUTime) > 30*time.Second {
		// First call or stale sample: store and return 0 rather than a bogus value.
		lastCPUSample = sample
		lastCPUTime = now
		return 0
	}

	deltaTotal := sample.total - lastCPUSample.total
	deltaIdle := sample.idle - lastCPUSample.idle

	lastCPUSample = sample
	lastCPUTime = now

	if deltaTotal <= 0 {
		return 0
	}

	usage := ((deltaTotal - deltaIdle) / deltaTotal) * 100
	if usage < 0 {
		usage = 0
	}
	if usage > 100 {
		usage = 100
	}
	return usage
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
	// 1. Try nvidia-smi
	out, err := exec.Command("nvidia-smi", "--query-gpu=utilization.gpu,temperature.gpu", "--format=csv,noheader,nounits").Output()
	if err == nil {
		fields := strings.Split(strings.TrimSpace(string(out)), ",")
		if len(fields) >= 2 {
			usage, _ := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
			temp, _ := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64)
			return usage, temp
		}
	}

	// 2. Try intel_gpu_top for Intel GPUs (Arc/iGPU)
	// Requires root/cap_sys_admin and intel-gpu-tools installed
	// We run it for a short burst to sample usage
	// timeout 0.5s intel_gpu_top -J -s 500 -o -
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "intel_gpu_top", "-J", "-s", "500", "-o", "-")
	out, err = cmd.Output()
	// Ignore error as timeout kills the process which causes an error code, but we might get output
	if len(out) > 0 {
		// Parse max "busy": val from JSON output using regex to avoid complex struct
		// JSON format: "engines": { "Render/3D/0": { "busy": 10.0 ... } }
		// pattern matches all "busy": followed by number
		re := regexp.MustCompile(`"busy":\s*([\d\.]+)`)
		matches := re.FindAllStringSubmatch(string(out), -1)

		var maxBusy float64 = 0
		for _, match := range matches {
			if len(match) > 1 {
				val, _ := strconv.ParseFloat(match[1], 64)
				if val > maxBusy {
					maxBusy = val
				}
			}
		}

		if maxBusy > 0 {
			return maxBusy, 0
		}
	}

	// 3. Fallback: Check for running ffmpeg processes with hardware acceleration flags
	// This provides a visual indication of activity even if we can't get precise metrics
	// due to container permission limits.
	pgrepOut, err := exec.Command("pgrep", "-a", "ffmpeg").Output()
	if err == nil {
		processes := strings.Split(string(pgrepOut), "\n")
		gpuProcessCount := 0
		for _, p := range processes {
			lowerP := strings.ToLower(p)
			// Check for VAAPI, QSV, NVENC, CUDA flags
			if strings.Contains(lowerP, "vaapi") ||
				strings.Contains(lowerP, "qsv") ||
				strings.Contains(lowerP, "nvenc") ||
				strings.Contains(lowerP, "cuda") {
				gpuProcessCount++
			}
		}

		if gpuProcessCount > 0 {
			// Estimate ~40% load per stream, capped at 90%
			estimatedLoad := float64(gpuProcessCount) * 40.0
			if estimatedLoad > 90.0 {
				estimatedLoad = 90.0
			}
			return estimatedLoad, 0
		}
	}

	return 0, 0
}
