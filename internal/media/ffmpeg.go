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

	// Source colour signalling, copied through to the output so HDR content
	// keeps its metadata instead of rendering washed out.
	ColorPrimaries string
	ColorTransfer  string
	ColorSpace     string
}

// ApplySourceInfo fills the source-derived fields of opts from a probed
// source, leaving caller-set fields untouched.
func (o *TranscodeOptions) ApplySourceInfo(info *MediaInfo) {
	if info == nil {
		return
	}
	o.TotalDuration = info.Duration
	o.ColorPrimaries = info.ColorPrimaries
	o.ColorTransfer = info.ColorTransfer
	o.ColorSpace = info.ColorSpace
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
		"-nostdin", // never block waiting on stdin when run as a subprocess
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

	// Map video (excluding attached pictures like cover art), audio, and subtitles.
	// Capital V excludes streams with the ATTACHED_PIC disposition, which prevents
	// hardware encoders (VAAPI/NVENC) from attempting to encode PNG/JPEG cover art
	// they cannot handle.
	//
	// The "?" suffix makes audio and subtitle maps optional. Without it, FFmpeg
	// aborts with "Stream map '0:s' matches no streams" on any source that has no
	// subtitle track — which is most MP4s and a good many MKVs.
	args = append(args, "-map", "0:V", "-map", "0:a?", "-map", "0:s?")

	// Output file
	args = append(args, "-y", opts.OutputPath)

	return args
}

// getHWAccelInputArgs returns hardware acceleration input arguments
func (f *FFmpegWrapper) getHWAccelInputArgs(vendor GPUVendor) []string {
	switch vendor {
	case GPUVendorNvidia:
		return []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda"}
	case GPUVendorIntel, GPUVendorAMD:
		// Use VAAPI for Intel/AMD on Linux/Docker as it's more reliable than QSV in containers.
		// allow_profile_mismatch lets FFmpeg fall back to software decoding for source codecs
		// (e.g. XviD/mpeg4 ASP) that VAAPI cannot decode, instead of aborting.
		return []string{"-hwaccel", "vaapi", "-hwaccel_device", "/dev/dri/renderD128", "-hwaccel_output_format", "vaapi", "-hwaccel_flags", "allow_profile_mismatch"}
	default:
		return []string{}
	}
}

// targetResolution returns the pixel dimensions for an upscale target.
func targetResolution(resolution string) (int, int) {
	if resolution == "4k" {
		return 3840, 2160
	}
	return 1920, 1080
}

// getUpscaleFilter returns the video filter string for upscaling.
// Must be GPU-aware: Intel/AMD decode with -hwaccel_output_format vaapi, so frames
// are already in VAAPI GPU memory and require scale_vaapi (not the software scale filter).
func (f *FFmpegWrapper) getUpscaleFilter(opts TranscodeOptions) string {
	if !opts.Upscale {
		return ""
	}

	targetW, targetH := targetResolution(opts.Resolution)

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

// getColorArgs copies the source colour signalling to the output. Without
// this, HDR content loses its primaries and transfer characteristics and
// renders washed out on an HDR display.
func (f *FFmpegWrapper) getColorArgs(opts TranscodeOptions) []string {
	args := []string{}
	if opts.ColorPrimaries != "" && opts.ColorPrimaries != "unknown" {
		args = append(args, "-color_primaries", opts.ColorPrimaries)
	}
	if opts.ColorTransfer != "" && opts.ColorTransfer != "unknown" {
		args = append(args, "-color_trc", opts.ColorTransfer)
	}
	if opts.ColorSpace != "" && opts.ColorSpace != "unknown" {
		args = append(args, "-colorspace", opts.ColorSpace)
	}
	return args
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
		// that falls back to software decoding, which allow_profile_mismatch
		// permits for codecs VAAPI cannot decode (e.g. XviD/mpeg4 ASP).
		//
		// The profile is deliberately left to the encoder: hevc_vaapi negotiates
		// Main 10 from p010 input on its own. Pinning -profile:v main10 here would
		// break 8-bit sources arriving as nv12 through the software-fallback path.
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
		// 10-bit output for the same reasons as the VAAPI path above.
		args = append(args,
			"-c:v", "libx265",
			"-preset", string(opts.Preset),
			"-crf", fmt.Sprintf("%d", opts.CRF),
			"-pix_fmt", "yuv420p10le",
			"-x265-params", "profile=main10",
		)
	}

	// Preserve HDR/wide-gamut signalling from the source.
	args = append(args, f.getColorArgs(opts)...)

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
		Streams []struct {
			CodecType        string `json:"codec_type"`
			Width            int    `json:"width"`
			Height           int    `json:"height"`
			PixFmt           string `json:"pix_fmt"`
			BitsPerRawSample string `json:"bits_per_raw_sample"`
			ColorPrimaries   string `json:"color_primaries"`
			ColorTransfer    string `json:"color_transfer"`
			ColorSpace       string `json:"color_space"`
			Disposition      struct {
				AttachedPic int `json:"attached_pic"`
			} `json:"disposition"`
			SideDataList []struct {
				SideDataType string `json:"side_data_type"`
				DVProfile    int    `json:"dv_profile"`
			} `json:"side_data_list"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(output, &probeData); err == nil {
		duration, _ := strconv.ParseFloat(probeData.Format.Duration, 64)
		size, _ := strconv.ParseInt(probeData.Format.Size, 10, 64)
		info := &MediaInfo{
			Path:     path,
			Filename: filepath.Base(path),
			Duration: duration,
			Size:     size,
			RawJSON:  string(output),
		}

		primaryVideoFound := false
		for _, s := range probeData.Streams {
			switch s.CodecType {
			case "video":
				// Cover art is carried as a video stream with the ATTACHED_PIC
				// disposition; it is not the primary video and must not drive
				// encoder settings.
				if s.Disposition.AttachedPic == 1 {
					continue
				}
				info.VideoStreams++
				if primaryVideoFound {
					continue
				}
				primaryVideoFound = true
				info.VideoWidth = s.Width
				info.VideoHeight = s.Height
				info.PixFmt = s.PixFmt
				info.ColorPrimaries = s.ColorPrimaries
				info.ColorTransfer = s.ColorTransfer
				info.ColorSpace = s.ColorSpace

				info.BitDepth = bitDepthFromPixFmt(s.PixFmt)
				if info.BitDepth == 0 {
					if b, convErr := strconv.Atoi(s.BitsPerRawSample); convErr == nil {
						info.BitDepth = b
					}
				}

				for _, sd := range s.SideDataList {
					if sd.DVProfile > 0 {
						info.DVProfile = sd.DVProfile
					}
				}
			case "audio":
				info.AudioStreams++
			case "subtitle":
				info.SubtitleStreams++
			}
		}
		return info, nil
	}

	return &MediaInfo{
		Path:     path,
		Filename: filepath.Base(path),
		RawJSON:  string(output),
	}, nil
}

// MediaInfo contains metadata about a media file
type MediaInfo struct {
	Path        string
	Filename    string
	Duration    float64
	Size        int64
	RawJSON     string
	VideoWidth  int
	VideoHeight int

	// Pixel format and bit depth drive encoder profile selection. Encoding a
	// 10-bit source with an 8-bit encoder profile fails at encoder init, after
	// the output file has already been created — which is how a 0-byte output
	// gets left behind.
	PixFmt   string // e.g. "yuv420p", "yuv420p10le"
	BitDepth int    // 8, 10 or 12; 0 when unknown

	// Colour signalling, copied to the output so HDR content doesn't lose its
	// metadata and render washed out.
	ColorPrimaries string // e.g. "bt2020"
	ColorTransfer  string // e.g. "smpte2084" (PQ/HDR10), "arib-std-b67" (HLG)
	ColorSpace     string // e.g. "bt2020nc"

	// DVProfile is the Dolby Vision profile from the DOVI configuration record,
	// or 0 when the stream carries no Dolby Vision metadata.
	DVProfile int

	VideoStreams    int
	AudioStreams    int
	SubtitleStreams int
}

// IsHDR reports whether the source uses a HDR transfer function.
func (m *MediaInfo) IsHDR() bool {
	return m.ColorTransfer == "smpte2084" || m.ColorTransfer == "arib-std-b67"
}

// IsDolbyVision reports whether the source carries Dolby Vision metadata.
func (m *MediaInfo) IsDolbyVision() bool { return m.DVProfile > 0 }

// HasHDR10BaseLayer reports whether a Dolby Vision stream is backwards
// compatible. Profiles 7 and 8 carry an HDR10 base layer that survives a
// re-encode; profile 5 does not, and transcoding it without tonemapping
// produces badly shifted colour.
func (m *MediaInfo) HasHDR10BaseLayer() bool {
	return m.DVProfile == 7 || m.DVProfile == 8
}

// bitDepthFromPixFmt derives a bit depth from an FFmpeg pixel format name.
// Returns 0 when the format is unrecognised.
func bitDepthFromPixFmt(pixFmt string) int {
	switch {
	case pixFmt == "":
		return 0
	case strings.Contains(pixFmt, "12le"), strings.Contains(pixFmt, "12be"):
		return 12
	case strings.Contains(pixFmt, "10le"), strings.Contains(pixFmt, "10be"), pixFmt == "p010":
		return 10
	default:
		return 8
	}
}
