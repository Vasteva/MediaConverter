package media

import (
	"bufio"
	"bytes"
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

	// Capture stderr for progress and error reporting
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Keeps a bounded tail for error reporting and scans the whole stream for
	// frame-loss markers as it goes.
	monitor := newStderrMonitor()

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Parse progress while also capturing output to our monitor
	parseDone := make(chan struct{})
	go func() {
		f.parseProgress(io.TeeReader(stderr, monitor), opts.TotalDuration, callback)
		close(parseDone)
	}()

	// Wait for completion
	waitErr := cmd.Wait()
	<-parseDone // Ensure parsing is finished; monitor is only read after this

	if waitErr != nil {
		return fmt.Errorf("ffmpeg failed: %w\nOutput:\n%s", waitErr, monitor.Tail())
	}

	// A zero exit status is not proof of a clean run. Under VAAPI memory
	// pressure FFmpeg reports a per-frame failure, drops the affected frames,
	// and carries on to exit 0 — producing an output of the right duration with
	// picture data missing. It happens on both sides of the graph:
	//
	//   Error submitting packet to decoder: Cannot allocate memory   (decode)
	//   Failed to inject frame into filter network: Cannot allocate memory
	//
	// Neither the exit code nor a duration check catches it, so the stderr has
	// to be read.
	if findings := monitor.Findings(); len(findings) > 0 {
		return &TranscodeIntegrityError{Findings: findings, Tail: monitor.Tail()}
	}

	return nil
}

// TranscodeIntegrityError reports that FFmpeg exited successfully but logged
// errors indicating frames were lost or corrupted while decoding or filtering.
type TranscodeIntegrityError struct {
	Findings []string // one representative stderr line per distinct problem
	Tail     string   // bounded tail of stderr, for diagnosis
}

func (e *TranscodeIntegrityError) Error() string {
	return fmt.Sprintf("ffmpeg exited 0 but reported %d frame-loss error(s), so the output is missing picture data: %s",
		len(e.Findings), strings.Join(e.Findings, " | "))
}

// frameLossPatterns are stderr markers that mean picture data was lost — a
// frame FFmpeg failed to decode, filter, or otherwise carry through to the
// encoder. Deliberately conservative: every entry indicates a dropped or
// corrupt frame, not a recoverable container-level complaint.
var frameLossPatterns = []string{
	// Decode side: the surface pool is exhausted or the bitstream is damaged.
	"Error submitting packet to decoder",
	"get_buffer() failed",
	"thread_get_buffer() failed",
	"Error parsing NAL unit",
	"Decoding error",
	"corrupt decoded frame",
	"error while decoding MB",
	// Filter side: a frame reached the filter graph but could not pass through
	// it. Seen on a 1080p VAAPI transcode that ran the entire film and then
	// failed on the final frame with "Cannot allocate memory" — none of the
	// decode-side patterns fired, so without these the job would exit 0 with
	// frames missing from the end.
	"Error while filtering",
	"Failed to inject frame into filter network",
}

const (
	maxStderrTail        = 8 << 10 // bytes of stderr retained for error messages
	maxFrameLossFindings = 8       // distinct problems recorded before we stop
	maxUnterminatedRun   = 4 << 10 // flush a pathologically long line at this size
)

// stderrMonitor keeps a bounded tail of FFmpeg's stderr while scanning the full
// stream for frame-loss markers.
//
// The previous implementation accumulated the entire stream in a
// strings.Builder and used only the last 2 KB of it. With "-progress pipe:2"
// emitting a block of key=value lines several times a second, a multi-hour
// 2160p transcode retained tens of megabytes to report two kilobytes.
//
// Not safe for concurrent use: writes come from the progress-parsing goroutine,
// and results are only read after that goroutine has finished.
type stderrMonitor struct {
	tail     []byte
	partial  []byte
	findings []string
	seen     map[string]bool
}

func newStderrMonitor() *stderrMonitor {
	return &stderrMonitor{seen: make(map[string]bool)}
}

func (m *stderrMonitor) Write(p []byte) (int, error) {
	m.tail = append(m.tail, p...)
	if len(m.tail) > maxStderrTail {
		// Copy in place so the backing array stays bounded.
		m.tail = append(m.tail[:0], m.tail[len(m.tail)-maxStderrTail:]...)
	}

	// FFmpeg separates progress updates with \r as well as \n.
	m.partial = append(m.partial, p...)
	for {
		i := bytes.IndexAny(m.partial, "\r\n")
		if i < 0 {
			break
		}
		m.scanLine(string(m.partial[:i]))
		m.partial = append(m.partial[:0], m.partial[i+1:]...)
	}
	if len(m.partial) > maxUnterminatedRun {
		m.scanLine(string(m.partial))
		m.partial = m.partial[:0]
	}

	return len(p), nil
}

func (m *stderrMonitor) scanLine(line string) {
	if len(m.findings) >= maxFrameLossFindings {
		return
	}
	for _, pattern := range frameLossPatterns {
		if !strings.Contains(line, pattern) {
			continue
		}
		// Record each distinct problem once. FFmpeg repeats these per frame,
		// and one representative line is enough to explain the failure.
		if !m.seen[pattern] {
			m.seen[pattern] = true
			m.findings = append(m.findings, strings.TrimSpace(line))
		}
		return
	}
}

// Findings returns one representative stderr line per distinct frame-loss problem.
func (m *stderrMonitor) Findings() []string { return m.findings }

// Tail returns the retained end of the stderr stream.
func (m *stderrMonitor) Tail() string {
	if len(m.tail) == maxStderrTail {
		return "..." + string(m.tail)
	}
	return string(m.tail)
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
