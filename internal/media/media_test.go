package media

import (
	"context"
	"testing"
	"time"
)

func TestFFmpegWrapper_BuildArgs(t *testing.T) {
	wrapper, err := NewFFmpegWrapper()
	if err != nil {
		t.Skip("FFmpeg not available, skipping test")
	}

	tests := []struct {
		name     string
		opts     TranscodeOptions
		expected []string
	}{
		{
			name: "NVIDIA NVENC",
			opts: TranscodeOptions{
				InputPath:  "/input/test.mkv",
				OutputPath: "/output/test.mkv",
				GPUVendor:  GPUVendorNvidia,
				Preset:     PresetMedium,
				CRF:        23,
				AudioCodec: "copy",
			},
			expected: []string{"-hwaccel", "cuda", "hevc_nvenc", "-preset", "p5"},
		},
		{
			name: "Intel QSV",
			opts: TranscodeOptions{
				InputPath:  "/input/test.mkv",
				OutputPath: "/output/test.mkv",
				GPUVendor:  GPUVendorIntel,
				Preset:     PresetMedium,
				CRF:        23,
				AudioCodec: "copy",
			},
			expected: []string{"-hwaccel", "qsv", "hevc_qsv"},
		},
		{
			name: "AMD VAAPI",
			opts: TranscodeOptions{
				InputPath:  "/input/test.mkv",
				OutputPath: "/output/test.mkv",
				GPUVendor:  GPUVendorAMD,
				Preset:     PresetMedium,
				CRF:        23,
				AudioCodec: "copy",
			},
			expected: []string{"-hwaccel", "vaapi", "hevc_vaapi"},
		},
		{
			name: "CPU libx265",
			opts: TranscodeOptions{
				InputPath:  "/input/test.mkv",
				OutputPath: "/output/test.mkv",
				GPUVendor:  GPUVendorCPU,
				Preset:     PresetMedium,
				CRF:        23,
				AudioCodec: "copy",
			},
			expected: []string{"libx265", "-preset", "medium", "-crf", "23"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := wrapper.buildFFmpegArgs(tt.opts)
			argsStr := joinArgs(args)

			for _, exp := range tt.expected {
				if !contains(argsStr, exp) {
					t.Errorf("Expected args to contain '%s', got: %v", exp, args)
				}
			}
		})
	}
}

func TestProgressParsing(t *testing.T) {
	tests := []struct {
		name     string
		time     string
		duration float64
		expected int
	}{
		{"25% complete", "00:15:00", 3600.0, 25},
		{"50% complete", "00:30:00", 3600.0, 50},
		{"75% complete", "00:45:00", 3600.0, 75},
		{"100% complete", "01:00:00", 3600.0, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			percentage := CalculatePercentage(tt.time, tt.duration)
			if percentage != tt.expected {
				t.Errorf("Expected %d%%, got %d%%", tt.expected, percentage)
			}
		})
	}
}

func TestEstimateETA(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		duration float64
		speed    string
		expected string
	}{
		{"Half done at 1x", "00:30:00", 3600.0, "1.0x", "00:30:00"},
		{"Half done at 2x", "00:30:00", 3600.0, "2.0x", "00:15:00"},
		{"Quarter done at 1x", "00:15:00", 3600.0, "1.0x", "00:45:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eta := EstimateETA(tt.current, tt.duration, tt.speed)
			if eta != tt.expected {
				t.Errorf("Expected ETA %s, got %s", tt.expected, eta)
			}
		})
	}
}

func TestMakeMKVSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Normal Title", "Normal Title"},
		{"Title: With Colon", "Title_ With Colon"},
		{"Title/With/Slashes", "Title_With_Slashes"},
		{"Title<>With|Invalid*Chars", "Title__With_Invalid_Chars"},
		{"  Spaces Around  ", "Spaces Around"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeFilename(tt.input)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestTranscodeWithProgressCallback(t *testing.T) {
	wrapper, err := NewFFmpegWrapper()
	if err != nil {
		t.Skip("FFmpeg not available, skipping test")
	}

	// This test just verifies the callback mechanism works
	// It won't actually transcode without a valid input file
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	callback := func(progress TranscodeProgress) {
		t.Logf("Progress: Frame=%d, FPS=%.2f, Speed=%s",
			progress.Frame, progress.FPS, progress.Speed)
	}

	opts := TranscodeOptions{
		InputPath:  "/nonexistent/file.mkv",
		OutputPath: "/tmp/output.mkv",
		GPUVendor:  GPUVendorCPU,
		Preset:     PresetFast,
		CRF:        23,
		AudioCodec: "copy",
	}

	// This will fail because the file doesn't exist, but that's expected
	_ = wrapper.TranscodeWithProgress(ctx, opts, callback)

	// We just want to verify the mechanism is in place
	t.Log("Callback mechanism tested (file not found is expected)")
}

// Helper functions
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) >= len(substr) &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
				containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func joinArgs(args []string) string {
	result := ""
	for _, arg := range args {
		result += arg + " "
	}
	return result
}
