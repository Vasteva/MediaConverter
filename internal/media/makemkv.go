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

// MakeMKVWrapper handles MakeMKV disc extraction
type MakeMKVWrapper struct {
	makemkvconPath string
}

// NewMakeMKVWrapper creates a new MakeMKV wrapper
func NewMakeMKVWrapper() (*MakeMKVWrapper, error) {
	path, err := exec.LookPath("makemkvcon")
	if err != nil {
		return nil, fmt.Errorf("makemkvcon not found in PATH: %w", err)
	}
	return &MakeMKVWrapper{makemkvconPath: path}, nil
}

// DiscInfo contains information about a disc
type DiscInfo struct {
	Type   string // "DVD", "Blu-ray", "ISO"
	Name   string
	Titles []TitleInfo
}

// TitleInfo contains information about a single title on a disc
type TitleInfo struct {
	Index        int
	Duration     string
	ChapterCount int
	Size         int64
	Description  string
}

// ExtractOptions contains parameters for disc extraction
type ExtractOptions struct {
	SourcePath string // Path to disc device or ISO file
	OutputDir  string
	MinLength  int // Minimum title length in seconds (0 = all titles)
	TitleIndex int // Specific title to extract (0 = all)
}

// ScanDisc scans a disc or ISO and returns available titles
func (m *MakeMKVWrapper) ScanDisc(ctx context.Context, sourcePath string) (*DiscInfo, error) {
	args := []string{
		"info",
		fmt.Sprintf("file:%s", sourcePath),
	}

	cmd := exec.CommandContext(ctx, m.makemkvconPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("makemkvcon scan failed: %w\nOutput: %s", err, string(output))
	}

	return m.parseDiscInfo(string(output)), nil
}

// Extract extracts titles from a disc or ISO
func (m *MakeMKVWrapper) Extract(ctx context.Context, opts ExtractOptions) error {
	return m.ExtractWithProgress(ctx, opts, nil)
}

// ExtractWithProgress extracts titles from a disc or ISO with real-time progress monitoring
func (m *MakeMKVWrapper) ExtractWithProgress(ctx context.Context, opts ExtractOptions, callback ProgressCallback) error {
	// Determine what to extract
	titleArg := "all"
	if opts.TitleIndex > 0 {
		titleArg = strconv.Itoa(opts.TitleIndex)
	}

	args := []string{
		"-r", // Robot mode for parsable output
		"mkv",
		fmt.Sprintf("file:%s", opts.SourcePath),
		titleArg,
		opts.OutputDir,
	}

	// Add minimum length filter if specified
	if opts.MinLength > 0 {
		args = append(args, "--minlength", strconv.Itoa(opts.MinLength))
	}

	cmd := exec.CommandContext(ctx, m.makemkvconPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start makemkvcon: %w", err)
	}

	// Parse progress in a goroutine
	if callback != nil {
		go m.parseExtractProgress(stdout, callback)
	} else {
		// If no callback, we still need to consume stdout
		go func() {
			buf := make([]byte, 1024)
			for {
				if _, err := stdout.Read(buf); err != nil {
					break
				}
			}
		}()
	}

	// Wait for completion
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("makemkvcon extraction failed: %w", err)
	}

	return nil
}

// parseExtractProgress parses MakeMKV robot mode output for progress
func (m *MakeMKVWrapper) parseExtractProgress(reader io.Reader, callback ProgressCallback) {
	scanner := bufio.NewScanner(reader)
	progress := TranscodeProgress{}

	for scanner.Scan() {
		line := scanner.Text()

		// PRGV:current,total,max
		if strings.HasPrefix(line, "PRGV:") {
			parts := strings.Split(strings.TrimPrefix(line, "PRGV:"), ",")
			if len(parts) >= 3 {
				total, _ := strconv.ParseFloat(parts[1], 64)
				max, _ := strconv.ParseFloat(parts[2], 64)
				if max > 0 {
					progress.Percentage = int((total / max) * 100)
					if callback != nil {
						callback(progress)
					}
				}
			}
		}
	}
}

// parseDiscInfo parses MakeMKV output to extract disc information
func (m *MakeMKVWrapper) parseDiscInfo(output string) *DiscInfo {
	info := &DiscInfo{
		Titles: []TitleInfo{},
	}

	lines := strings.Split(output, "\n")

	// Regular expressions for parsing
	titleRegex := regexp.MustCompile(`TINFO:(\d+),\d+,\d+,"([^"]*)"`)
	durationRegex := regexp.MustCompile(`TINFO:(\d+),9,0,"([^"]*)"`)
	chaptersRegex := regexp.MustCompile(`TINFO:(\d+),8,0,"(\d+)"`)

	titleMap := make(map[int]*TitleInfo)

	for _, line := range lines {
		// Parse disc name
		if strings.Contains(line, "CINFO:2,0") {
			matches := regexp.MustCompile(`CINFO:2,0,"([^"]*)"`).FindStringSubmatch(line)
			if len(matches) > 1 {
				info.Name = matches[1]
			}
		}

		// Parse title information
		if matches := titleRegex.FindStringSubmatch(line); len(matches) > 2 {
			titleIdx, _ := strconv.Atoi(matches[1])
			if _, exists := titleMap[titleIdx]; !exists {
				titleMap[titleIdx] = &TitleInfo{Index: titleIdx}
			}
			titleMap[titleIdx].Description = matches[2]
		}

		// Parse duration
		if matches := durationRegex.FindStringSubmatch(line); len(matches) > 2 {
			titleIdx, _ := strconv.Atoi(matches[1])
			if _, exists := titleMap[titleIdx]; !exists {
				titleMap[titleIdx] = &TitleInfo{Index: titleIdx}
			}
			titleMap[titleIdx].Duration = matches[2]
		}

		// Parse chapter count
		if matches := chaptersRegex.FindStringSubmatch(line); len(matches) > 2 {
			titleIdx, _ := strconv.Atoi(matches[1])
			chapterCount, _ := strconv.Atoi(matches[2])
			if _, exists := titleMap[titleIdx]; !exists {
				titleMap[titleIdx] = &TitleInfo{Index: titleIdx}
			}
			titleMap[titleIdx].ChapterCount = chapterCount
		}
	}

	// Convert map to slice
	for _, title := range titleMap {
		info.Titles = append(info.Titles, *title)
	}

	return info
}

// GetOutputFilename generates the expected output filename for a title
func (m *MakeMKVWrapper) GetOutputFilename(discName string, titleIndex int) string {
	// MakeMKV typically outputs as: title_t00.mkv
	if discName != "" {
		return fmt.Sprintf("%s_t%02d.mkv", sanitizeFilename(discName), titleIndex)
	}
	return fmt.Sprintf("title_t%02d.mkv", titleIndex)
}

// sanitizeFilename removes invalid characters from filenames
func sanitizeFilename(name string) string {
	// Replace invalid characters with underscores
	invalidChars := regexp.MustCompile(`[<>:"/\\|?*]`)
	sanitized := invalidChars.ReplaceAllString(name, "_")

	// Remove leading/trailing spaces and dots
	sanitized = strings.TrimSpace(sanitized)
	sanitized = strings.Trim(sanitized, ".")

	return sanitized
}

// FindLargestTitle returns the index of the title with the longest duration
func (d *DiscInfo) FindLargestTitle() int {
	if len(d.Titles) == 0 {
		return 0
	}

	largestIdx := 0
	maxDuration := 0

	for _, title := range d.Titles {
		duration := parseDurationToSeconds(title.Duration)
		if duration > maxDuration {
			maxDuration = duration
			largestIdx = title.Index
		}
	}

	return largestIdx
}

// parseDurationToSeconds converts duration string (HH:MM:SS) to seconds
func parseDurationToSeconds(duration string) int {
	parts := strings.Split(duration, ":")
	if len(parts) != 3 {
		return 0
	}

	hours, _ := strconv.Atoi(parts[0])
	minutes, _ := strconv.Atoi(parts[1])
	seconds, _ := strconv.Atoi(parts[2])

	return hours*3600 + minutes*60 + seconds
}
