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
			name: "Intel VAAPI",
			opts: TranscodeOptions{
				InputPath:  "/input/test.mkv",
				OutputPath: "/output/test.mkv",
				GPUVendor:  GPUVendorIntel,
				Preset:     PresetMedium,
				CRF:        23,
				AudioCodec: "copy",
			},
			expected: []string{"-hwaccel", "vaapi", "hevc_vaapi"},
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

func TestDiscInfoTitleDurationSeconds(t *testing.T) {
	info := &DiscInfo{Titles: []TitleInfo{
		{Index: 0, Duration: "00:05:00"},
		{Index: 1, Duration: "01:32:14"},
		{Index: 2, Duration: "bogus"},
	}}

	cases := []struct {
		name string
		idx  int
		want float64
	}{
		{"found, parses cleanly", 1, 5534},
		{"found, unparsable duration string", 2, 0},
		{"index not present on disc", 7, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := info.TitleDurationSeconds(tc.idx); got != tc.want {
				t.Errorf("TitleDurationSeconds(%d) = %v, want %v", tc.idx, got, tc.want)
			}
		})
	}
}

func TestParseFrameRate(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  float64
	}{
		{"NTSC film rate", "24000/1001", 23.976023976023978},
		{"whole number rate", "25/1", 25},
		{"unknown, ffprobe's 0/0", "0/0", 0},
		{"empty string", "", 0},
		{"plain decimal, no slash", "29.97", 29.97},
		{"garbage denominator", "30/abc", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseFrameRate(tc.input); got != tc.want {
				t.Errorf("parseFrameRate(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestBitsPerPixel exercises the density calculation #39's skip-if-efficient
// filter is built on.
func TestBitsPerPixel(t *testing.T) {
	cases := []struct {
		name string
		info *MediaInfo
		want float64
	}{
		{
			name: "well-formed 1080p24 source",
			// 100 MB over 60s at 1920x1080/24fps.
			info: &MediaInfo{Size: 100 << 20, Duration: 60, VideoWidth: 1920, VideoHeight: 1080, FrameRate: 24},
			want: (float64(100<<20) * 8 / 60) / (1920 * 1080 * 24),
		},
		{"missing size", &MediaInfo{Duration: 60, VideoWidth: 1920, VideoHeight: 1080, FrameRate: 24}, 0},
		{"missing duration", &MediaInfo{Size: 100 << 20, VideoWidth: 1920, VideoHeight: 1080, FrameRate: 24}, 0},
		{"missing dimensions", &MediaInfo{Size: 100 << 20, Duration: 60, FrameRate: 24}, 0},
		{"missing frame rate", &MediaInfo{Size: 100 << 20, Duration: 60, VideoWidth: 1920, VideoHeight: 1080}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.info.BitsPerPixel(); got != tc.want {
				t.Errorf("BitsPerPixel() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestIsAlreadyEfficient covers #39: only HEVC/AV1 sources at or below the
// density floor should be treated as not worth re-encoding.
func TestIsAlreadyEfficient(t *testing.T) {
	cases := []struct {
		name         string
		codec        string
		bitsPerPixel float64
		floor        float64
		want         bool
	}{
		{"HEVC below the floor", "hevc", 0.04, 0.06, true},
		{"HEVC exactly at the floor", "hevc", 0.06, 0.06, true},
		{"HEVC above the floor", "hevc", 0.07, 0.06, false},
		{"AV1 below the floor", "av1", 0.04, 0.06, true},
		{"H.264 below the floor still re-encodes", "h264", 0.04, 0.06, false},
		{"unknown codec", "", 0.04, 0.06, false},
		{"zero density never skips", "hevc", 0, 0.06, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsAlreadyEfficient(tc.codec, tc.bitsPerPixel, tc.floor)
			if got != tc.want {
				t.Errorf("IsAlreadyEfficient(%q, %v, %v) = %v, want %v",
					tc.codec, tc.bitsPerPixel, tc.floor, got, tc.want)
			}
		})
	}
}

// TestShouldRefuseCRFSuggestion covers #40: an AI CRF suggestion more
// indulgent than the configured default, on a source already in this
// pipeline's target codec, should be refused.
func TestShouldRefuseCRFSuggestion(t *testing.T) {
	cases := []struct {
		name                     string
		codec                    string
		suggestedCRF, defaultCRF int
		want                     bool
	}{
		{"HEVC suggestion more indulgent than default is refused", "hevc", 20, 23, true},
		{"HEVC suggestion equal to default is trusted", "hevc", 23, 23, false},
		{"HEVC suggestion more aggressive than default is trusted", "hevc", 28, 23, false},
		{"AV1 suggestion more indulgent than default is refused", "av1", 20, 23, true},
		{"H.264 suggestion more indulgent than default is still trusted", "h264", 20, 23, false},
		{"unknown codec is trusted", "", 20, 23, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldRefuseCRFSuggestion(tc.codec, tc.suggestedCRF, tc.defaultCRF)
			if got != tc.want {
				t.Errorf("ShouldRefuseCRFSuggestion(%q, %d, %d) = %v, want %v",
					tc.codec, tc.suggestedCRF, tc.defaultCRF, got, tc.want)
			}
		})
	}
}

func TestParseRatio(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantNum int
		wantDen int
	}{
		{"anamorphic NTSC widescreen", "32:27", 32, 27},
		{"square pixels", "1:1", 1, 1},
		{"ffprobe's unknown", "0:1", 0, 0},
		{"empty string", "", 0, 0},
		{"no separator", "11", 0, 0},
		{"negative numerator", "-1:1", 0, 0},
		{"zero denominator", "1:0", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			num, den := parseRatio(tc.input)
			if num != tc.wantNum || den != tc.wantDen {
				t.Errorf("parseRatio(%q) = (%d, %d), want (%d, %d)", tc.input, num, den, tc.wantNum, tc.wantDen)
			}
		})
	}
}

// TestUpscaleTargetDimensions covers #42: an upscale target must preserve
// the source's display aspect ratio (stored dimensions × SAR), not just
// stretch stored pixels to fill a square-pixel box.
func TestUpscaleTargetDimensions(t *testing.T) {
	cases := []struct {
		name                       string
		srcW, srcH, sarNum, sarDen int
		targetW, targetH           int
		wantW, wantH               int
	}{
		{
			name: "anamorphic 16:9 DVD maps cleanly to 1080p",
			// 720x480 stored, 32:27 SAR — the classic NTSC anamorphic
			// widescreen case. True DAR is exactly 16:9.
			srcW: 720, srcH: 480, sarNum: 32, sarDen: 27,
			targetW: 1920, targetH: 1080,
			wantW: 1920, wantH: 1080,
		},
		{
			name: "anamorphic 4:3 DVD fits within the box, not stretched to fill it",
			// 720x480 stored, 8:9 SAR — true DAR is 4:3. The old bare
			// scale=1920:1080 stretched this to 16:9, which is the
			// distortion #42 is about.
			srcW: 720, srcH: 480, sarNum: 8, sarDen: 9,
			targetW: 1920, targetH: 1080,
			wantW: 1440, wantH: 1080,
		},
		{
			name: "square-pixel cinemascope isn't stretched to fill the height either",
			// No SAR (square pixels) but not 16:9 — the general case the
			// old code got wrong for any non-16:9 source, anamorphic or not.
			srcW: 1920, srcH: 800, sarNum: 0, sarDen: 0,
			targetW: 1920, targetH: 1080,
			wantW: 1920, wantH: 800,
		},
		{
			name: "unknown source dimensions falls back to the target box",
			srcW: 0, srcH: 0, sarNum: 0, sarDen: 0,
			targetW: 1920, targetH: 1080,
			wantW: 1920, wantH: 1080,
		},
		{
			name: "odd computed dimensions are rounded up to even",
			srcW: 3, srcH: 2, sarNum: 0, sarDen: 0,
			targetW: 101, targetH: 101,
			wantW: 102, wantH: 68,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotW, gotH := upscaleTargetDimensions(tc.srcW, tc.srcH, tc.sarNum, tc.sarDen, tc.targetW, tc.targetH)
			if gotW != tc.wantW || gotH != tc.wantH {
				t.Errorf("upscaleTargetDimensions(%d, %d, %d, %d, %d, %d) = (%d, %d), want (%d, %d)",
					tc.srcW, tc.srcH, tc.sarNum, tc.sarDen, tc.targetW, tc.targetH, gotW, gotH, tc.wantW, tc.wantH)
			}
			if gotW%2 != 0 || gotH%2 != 0 {
				t.Errorf("upscaleTargetDimensions(...) = (%d, %d), want both dimensions even", gotW, gotH)
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

	// This will fail because the file doesn't exist, and we expect an error.
	err = wrapper.TranscodeWithProgress(ctx, opts, callback)
	if err == nil {
		t.Error("expected an error for nonexistent input file, got nil")
	}
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
