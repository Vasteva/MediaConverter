package media

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ProgressCallback is called periodically with transcoding progress
type ProgressCallback func(progress TranscodeProgress)

// TranscodeProgress contains real-time transcoding metrics
type TranscodeProgress struct {
	Frame           int     // Current frame number
	FPS             float64 // Frames per second
	Bitrate         string  // Current bitrate
	Size            string  // Output file size
	Time            string  // Current timestamp
	Speed           string  // Processing speed (e.g., "2.5x")
	SpeedMultiplier float64 // Numerical speed multiplier
	Percentage      int     // Percentage complete (0-100)
	ETA             string  // Estimated time remaining
}

// TranscodeWithProgress executes FFmpeg with real-time progress monitoring
func (f *FFmpegWrapper) TranscodeWithProgress(ctx context.Context, opts TranscodeOptions, callback ProgressCallback) error {
	args := f.buildFFmpegArgs(opts)

	// Add progress output
	args = append([]string{"-progress", "pipe:2"}, args...)

	cmd := exec.CommandContext(ctx, f.ffmpegPath, args...)

	// Capture stderr for progress
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Parse progress in a goroutine
	go f.parseProgress(stderr, opts.TotalDuration, callback)

	// Wait for completion
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w", err)
	}

	return nil
}

// parseProgress parses FFmpeg progress output
func (f *FFmpegWrapper) parseProgress(reader io.Reader, totalDuration float64, callback ProgressCallback) {
	scanner := bufio.NewScanner(reader)
	progress := TranscodeProgress{}

	// Regex patterns for parsing
	frameRegex := regexp.MustCompile(`frame=\s*(\d+)`)
	fpsRegex := regexp.MustCompile(`fps=\s*([\d.]+)`)
	bitrateRegex := regexp.MustCompile(`bitrate=\s*([\d.]+\w+/s)`)
	sizeRegex := regexp.MustCompile(`size=\s*(\d+\w+)`)
	timeRegex := regexp.MustCompile(`time=\s*([\d:\.]+)`)
	speedRegex := regexp.MustCompile(`speed=\s*([\d.]+x)`)

	for scanner.Scan() {
		line := scanner.Text()

		// Parse individual metrics (same as before)
		if matches := frameRegex.FindStringSubmatch(line); len(matches) > 1 {
			progress.Frame, _ = strconv.Atoi(matches[1])
		}
		if matches := fpsRegex.FindStringSubmatch(line); len(matches) > 1 {
			progress.FPS, _ = strconv.ParseFloat(matches[1], 64)
		}
		if matches := bitrateRegex.FindStringSubmatch(line); len(matches) > 1 {
			progress.Bitrate = matches[1]
		}
		if matches := sizeRegex.FindStringSubmatch(line); len(matches) > 1 {
			progress.Size = matches[1]
		}
		if matches := timeRegex.FindStringSubmatch(line); len(matches) > 1 {
			progress.Time = matches[1]
		}
		if matches := speedRegex.FindStringSubmatch(line); len(matches) > 1 {
			progress.Speed = matches[1]
			speedStr := strings.TrimSuffix(matches[1], "x")
			progress.SpeedMultiplier, _ = strconv.ParseFloat(speedStr, 64)
		}

		// Calculate derived metrics
		if totalDuration > 0 && progress.Time != "" {
			progress.Percentage = CalculatePercentage(progress.Time, totalDuration)
			progress.ETA = EstimateETA(progress.Time, totalDuration, progress.Speed)
		}

		// Call the callback with updated progress
		if callback != nil && progress.Frame > 0 {
			callback(progress)
		}
	}
}

// CalculatePercentage calculates completion percentage based on duration
func CalculatePercentage(currentTime string, totalDuration float64) int {
	current := parseTimeToSeconds(currentTime)
	if totalDuration == 0 {
		return 0
	}

	percentage := int((current / totalDuration) * 100)
	if percentage > 100 {
		percentage = 100
	}

	return percentage
}

// parseTimeToSeconds converts FFmpeg time format (HH:MM:SS.ms) to seconds
func parseTimeToSeconds(timeStr string) float64 {
	parts := strings.Split(timeStr, ":")
	if len(parts) != 3 {
		return 0
	}

	hours, _ := strconv.ParseFloat(parts[0], 64)
	minutes, _ := strconv.ParseFloat(parts[1], 64)
	seconds, _ := strconv.ParseFloat(parts[2], 64)

	return hours*3600 + minutes*60 + seconds
}

// EstimateETA calculates estimated time remaining
func EstimateETA(currentTime string, totalDuration float64, speed string) string {
	current := parseTimeToSeconds(currentTime)
	remaining := totalDuration - current

	if remaining <= 0 {
		return "00:00:00"
	}

	// Parse speed multiplier (e.g., "2.5x" -> 2.5)
	speedMultiplier := 1.0
	if strings.HasSuffix(speed, "x") {
		speedStr := strings.TrimSuffix(speed, "x")
		speedMultiplier, _ = strconv.ParseFloat(speedStr, 64)
	}

	if speedMultiplier == 0 {
		speedMultiplier = 1.0
	}

	// Calculate ETA in seconds
	etaSeconds := remaining / speedMultiplier

	// Format as HH:MM:SS
	hours := int(etaSeconds / 3600)
	minutes := int((etaSeconds - float64(hours*3600)) / 60)
	seconds := int(etaSeconds) % 60

	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}
