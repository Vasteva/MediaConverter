package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// GPUVendor represents the hardware acceleration type
type GPUVendor string

const (
	GPUVendorNvidia GPUVendor = "nvidia"
	GPUVendorIntel  GPUVendor = "intel"
	GPUVendorAMD    GPUVendor = "amd"
	GPUVendorCPU    GPUVendor = "cpu"
)

// QualityPreset defines encoding speed/quality tradeoff
type QualityPreset string

const (
	PresetFast   QualityPreset = "fast"
	PresetMedium QualityPreset = "medium"
	PresetSlow   QualityPreset = "slow"
)

// TranscodeOptions contains all parameters for FFmpeg transcoding
type TranscodeOptions struct {
	InputPath     string
	OutputPath    string
	GPUVendor     GPUVendor
	Preset        QualityPreset
	CRF           int
	AudioCodec    string // "copy", "aac", "ac3"
	Container     string // "mkv", "mp4"
	TotalDuration float64
	Upscale       bool   // Premium feature: AI Super Resolution
	Resolution    string // "1080p", "4k"
}

// FFmpegWrapper handles FFmpeg command execution
type FFmpegWrapper struct {
	ffmpegPath  string
	ffprobePath string
}

// NewFFmpegWrapper creates a new FFmpeg wrapper
func NewFFmpegWrapper() (*FFmpegWrapper, error) {
	// Check if ffmpeg is available
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}
	ffprobePath, _ := exec.LookPath("ffprobe")
	return &FFmpegWrapper{ffmpegPath: path, ffprobePath: ffprobePath}, nil
}

// Transcode executes FFmpeg transcoding with the given options
func (f *FFmpegWrapper) Transcode(ctx context.Context, opts TranscodeOptions) error {
	args := f.buildFFmpegArgs(opts)

	cmd := exec.CommandContext(ctx, f.ffmpegPath, args...)

	// Capture output for debugging
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// ExtractFrame extracts a single frame at a specific timestamp
func (f *FFmpegWrapper) ExtractFrame(ctx context.Context, inputPath string, timestamp float64, outputPath string) error {
	args := []string{
		"-ss", fmt.Sprintf("%.2f", timestamp),
		"-i", inputPath,
		"-frames:v", "1",
		"-q:v", "2", // High quality JPEG
		"-y",
		outputPath,
	}

	cmd := exec.CommandContext(ctx, f.ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("frame extraction failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// buildFFmpegArgs constructs the FFmpeg command arguments
func (f *FFmpegWrapper) buildFFmpegArgs(opts TranscodeOptions) []string {
	args := []string{
		"-hide_banner",
		"-loglevel", "info",
		"-stats",
	}

	// Hardware acceleration input
	args = append(args, f.getHWAccelInputArgs(opts.GPUVendor)...)

	// Input file
	args = append(args, "-i", opts.InputPath)

	// Video encoding
	args = append(args, f.getVideoEncoderArgs(opts)...)

	// Audio encoding
	args = append(args, f.getAudioEncoderArgs(opts.AudioCodec)...)

	// Subtitle handling (copy all)
	args = append(args, "-c:s", "copy")

	// Map all streams
	args = append(args, "-map", "0")

	// Output file
	args = append(args, "-y", opts.OutputPath)

	return args
}

// getHWAccelInputArgs returns hardware acceleration input arguments
func (f *FFmpegWrapper) getHWAccelInputArgs(vendor GPUVendor) []string {
	switch vendor {
	case GPUVendorNvidia:
		return []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda"}
	case GPUVendorIntel:
		// Use VAAPI for Intel on Linux/Docker as it's more reliable than QSV in containers
		return []string{"-hwaccel", "vaapi", "-hwaccel_device", "/dev/dri/renderD128", "-hwaccel_output_format", "vaapi"}
	case GPUVendorAMD:
		return []string{"-hwaccel", "vaapi", "-hwaccel_device", "/dev/dri/renderD128", "-hwaccel_output_format", "vaapi"}
	default:
		return []string{}
	}
}

// getUpscaleFilter returns the video filter string for upscaling.
// Must be GPU-aware: Intel/AMD decode with -hwaccel_output_format vaapi, so frames
// are already in VAAPI GPU memory and require scale_vaapi (not the software scale filter).
func (f *FFmpegWrapper) getUpscaleFilter(opts TranscodeOptions) string {
	if !opts.Upscale {
		return ""
	}

	targetW, targetH := 1920, 1080
	if opts.Resolution == "4k" {
		targetW, targetH = 3840, 2160
	}

	switch opts.GPUVendor {
	case GPUVendorNvidia:
		return fmt.Sprintf("scale_cuda=%d:%d", targetW, targetH)
	case GPUVendorIntel, GPUVendorAMD:
		// Frames are already in VAAPI format from hwaccel; scale_vaapi operates on
		// GPU frames directly and handles HDR/10-bit content natively.
		return fmt.Sprintf("scale_vaapi=%d:%d", targetW, targetH)
	default:
		return fmt.Sprintf("scale=%d:%d:flags=lanczos", targetW, targetH)
	}
}

// getVideoEncoderArgs returns video encoder arguments based on GPU vendor
func (f *FFmpegWrapper) getVideoEncoderArgs(opts TranscodeOptions) []string {
	args := []string{}
	upscaleFilter := f.getUpscaleFilter(opts)

	switch opts.GPUVendor {
	case GPUVendorNvidia:
		if upscaleFilter != "" {
			args = append(args, "-vf", upscaleFilter)
		}
		args = append(args,
			"-c:v", "hevc_nvenc",
			"-preset", f.mapPresetToNvenc(opts.Preset),
			"-rc", "vbr",
			"-cq", fmt.Sprintf("%d", opts.CRF),
			"-b:v", "0",
			"-profile:v", "main10",
			"-tier", "high",
		)
	case GPUVendorIntel, GPUVendorAMD:
		// With -hwaccel_output_format vaapi, decoded frames are already on the GPU.
		// scale_vaapi operates on those frames directly — appending hwupload would
		// fail (can't upload frames that are already in hardware format).
		// hwupload is kept for the no-upscale path as a safety net for any stream
		// that falls back to software decoding.
		filter := "hwupload"
		if upscaleFilter != "" {
			filter = upscaleFilter // scale_vaapi; no hwupload needed
		}
		args = append(args,
			"-c:v", "hevc_vaapi",
			"-qp", fmt.Sprintf("%d", opts.CRF),
			"-vf", filter,
		)
	default: // CPU
		if upscaleFilter != "" {
			args = append(args, "-vf", upscaleFilter)
		}
		args = append(args,
			"-c:v", "libx265",
			"-preset", string(opts.Preset),
			"-crf", fmt.Sprintf("%d", opts.CRF),
			"-pix_fmt", "yuv420p10le",
			"-x265-params", "profile=main10",
		)
	}

	return args
}

// mapPresetToNvenc maps generic preset to NVENC-specific preset
func (f *FFmpegWrapper) mapPresetToNvenc(preset QualityPreset) string {
	switch preset {
	case PresetFast:
		return "p4"
	case PresetMedium:
		return "p5"
	case PresetSlow:
		return "p7"
	default:
		return "p5"
	}
}

// getAudioEncoderArgs returns audio encoder arguments
func (f *FFmpegWrapper) getAudioEncoderArgs(codec string) []string {
	if codec == "" || codec == "copy" {
		return []string{"-c:a", "copy"}
	}

	switch strings.ToLower(codec) {
	case "aac":
		return []string{"-c:a", "aac", "-b:a", "256k"}
	case "ac3":
		return []string{"-c:a", "ac3", "-b:a", "640k"}
	default:
		return []string{"-c:a", "copy"}
	}
}

// GetMediaInfo retrieves basic media information using ffprobe
func (f *FFmpegWrapper) GetMediaInfo(ctx context.Context, path string) (*MediaInfo, error) {
	if f.ffprobePath == "" {
		return nil, fmt.Errorf("ffprobe not found")
	}

	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	}

	cmd := exec.CommandContext(ctx, f.ffprobePath, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	// Parse basic info from JSON
	var probeData struct {
		Format struct {
			Duration string `json:"duration"`
			Size     string `json:"size"`
		} `json:"format"`
	}

	if err := json.Unmarshal(output, &probeData); err == nil {
		duration, _ := strconv.ParseFloat(probeData.Format.Duration, 64)
		size, _ := strconv.ParseInt(probeData.Format.Size, 10, 64)
		return &MediaInfo{
			Path:     path,
			Filename: filepath.Base(path),
			Duration: duration,
			Size:     size,
			RawJSON:  string(output),
		}, nil
	}

	return &MediaInfo{
		Path:     path,
		Filename: filepath.Base(path),
		RawJSON:  string(output),
	}, nil
}

// MediaInfo contains metadata about a media file
type MediaInfo struct {
	Path     string
	Filename string
	Duration float64
	Size     int64
	RawJSON  string
}
