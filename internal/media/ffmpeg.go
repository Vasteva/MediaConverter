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

	// Source dimensions and sample aspect ratio, needed to compute an
	// upscale target that preserves the source's display aspect ratio
	// instead of stretching it — see getUpscaleFilter.
	SourceWidth  int
	SourceHeight int
	SARNum       int
	SARDen       int
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
	o.SourceWidth = info.VideoWidth
	o.SourceHeight = info.VideoHeight
	o.SARNum = info.SARNum
	o.SARDen = info.SARDen
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
		//
		// extra_hw_frames enlarges the VAAPI surface pool. The default is too small
		// for 2160p 10-bit HEVC: the decoder exhausts it, reports "Cannot allocate
		// memory" from get_buffer(), drops the affected frames, and still exits 0 —
		// so the output looks complete and correctly timed while missing picture
		// data. Under concurrent jobs the allocation fails at startup instead and
		// FFmpeg dies before writing anything, leaving a 0-byte file.
		//
		// Measured on an Arc A310 against a 2160p 10-bit HEVC REMUX, 60s sample:
		//   default pool       77 decode errors
		//   extra_hw_frames 32  0 decode errors
		//
		// 32 extra 2160p p010 surfaces cost roughly 800 MB of the card's 4 GB.
		// If this ever needs to vary by hardware it should become configurable;
		// hardcoded until there is evidence that it does.
		return []string{
			"-hwaccel", "vaapi",
			"-hwaccel_device", "/dev/dri/renderD128",
			"-hwaccel_output_format", "vaapi",
			"-hwaccel_flags", "allow_profile_mismatch",
			"-extra_hw_frames", "32",
		}
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

// upscaleTargetDimensions computes an upscale target that fits within
// targetW×targetH while preserving the source's display aspect ratio —
// stored width × SAR ÷ height — rather than its stored pixel dimensions.
//
// A bare scale=targetW:targetH ignores aspect ratio entirely: it stretches
// whatever the source's shape is to exactly fill a square-pixel target box,
// which distorts anamorphic sources (non-square pixels, common on DVD rips)
// and non-16:9 sources alike. This picks whichever axis is aspect-limiting
// and shrinks the other, so the output is never larger than the target box
// but always correctly proportioned — no padding, so a source that isn't
// 16:9 comes out at a non-16:9 resolution rather than letterboxed.
//
// sarNum/sarDen of 0 (unknown, or ffprobe's own "0:1" for it) is treated as
// 1:1 — square pixels, i.e. today's stored-dimension behaviour — rather than
// guessing. srcW/srcH of 0 (unprobeable) falls back to the target box as-is.
func upscaleTargetDimensions(srcW, srcH, sarNum, sarDen, targetW, targetH int) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return targetW, targetH
	}
	if sarNum <= 0 || sarDen <= 0 {
		sarNum, sarDen = 1, 1
	}

	dar := float64(srcW) * float64(sarNum) / (float64(srcH) * float64(sarDen))

	outW := targetW
	outH := int(float64(targetW)/dar + 0.5)
	if outH > targetH {
		outH = targetH
		outW = int(float64(targetH)*dar + 0.5)
	}

	// Even dimensions: required by the 4:2:0 chroma subsampling this
	// pipeline's HEVC output always uses.
	if outW%2 != 0 {
		outW++
	}
	if outH%2 != 0 {
		outH++
	}
	return outW, outH
}

// getUpscaleFilter returns the video filter string for upscaling.
// Must be GPU-aware: Intel/AMD decode with -hwaccel_output_format vaapi, so frames
// are already in VAAPI GPU memory and require scale_vaapi (not the software scale filter).
//
// setsar=1 alone does not fix an anamorphic source — it only stops the
// *output* from being tagged non-square, after the stretch already happened
// during scaling. upscaleTargetDimensions computes dimensions that are
// correct before scaling; setsar=1 here just confirms the now-correct output
// as square-pixel rather than leaving it tagged with the source's SAR.
func (f *FFmpegWrapper) getUpscaleFilter(opts TranscodeOptions) string {
	if !opts.Upscale {
		return ""
	}

	targetW, targetH := targetResolution(opts.Resolution)
	outW, outH := upscaleTargetDimensions(opts.SourceWidth, opts.SourceHeight, opts.SARNum, opts.SARDen, targetW, targetH)

	switch opts.GPUVendor {
	case GPUVendorNvidia:
		return fmt.Sprintf("scale_cuda=%d:%d,setsar=1", outW, outH)
	case GPUVendorIntel, GPUVendorAMD:
		// Frames are already in VAAPI format from hwaccel; scale_vaapi operates on
		// GPU frames directly and handles HDR/10-bit content natively. setsar is a
		// metadata-only filter and passes hardware frames through untouched.
		return fmt.Sprintf("scale_vaapi=%d:%d,setsar=1", outW, outH)
	default:
		return fmt.Sprintf("scale=%d:%d:flags=lanczos,setsar=1", outW, outH)
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
			CodecType         string `json:"codec_type"`
			CodecName         string `json:"codec_name"`
			Width             int    `json:"width"`
			Height            int    `json:"height"`
			PixFmt            string `json:"pix_fmt"`
			BitsPerRawSample  string `json:"bits_per_raw_sample"`
			RFrameRate        string `json:"r_frame_rate"`
			AvgFrameRate      string `json:"avg_frame_rate"`
			SampleAspectRatio string `json:"sample_aspect_ratio"`
			ColorPrimaries    string `json:"color_primaries"`
			ColorTransfer     string `json:"color_transfer"`
			ColorSpace        string `json:"color_space"`
			Disposition       struct {
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
				info.CodecName = s.CodecName
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

				// avg_frame_rate is the true mean over the stream; r_frame_rate is
				// the container's nominal rate and is used as a fallback for the
				// (uncommon) case where avg_frame_rate comes back "0/0".
				info.FrameRate = parseFrameRate(s.AvgFrameRate)
				if info.FrameRate <= 0 {
					info.FrameRate = parseFrameRate(s.RFrameRate)
				}

				// Anamorphic sources (DVD rips especially) store non-square
				// pixels: stored width×height isn't the display aspect
				// ratio. SARNum/SARDen come back 0 for ffprobe's "0:1"
				// (unknown) or anything else unparseable; upscaleTargetDimensions
				// treats that the same as an explicit "1:1" — no correction
				// needed — rather than guessing.
				info.SARNum, info.SARDen = parseRatio(s.SampleAspectRatio)

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
	CodecName string  // e.g. "hevc", "h264"
	PixFmt    string  // e.g. "yuv420p", "yuv420p10le"
	BitDepth  int     // 8, 10 or 12; 0 when unknown
	FrameRate float64 // frames per second; 0 when unknown

	// SARNum/SARDen are the source's sample (pixel) aspect ratio. Both 0
	// means unknown or square (1:1) — an anamorphic source (common on DVD
	// rips) has non-square pixels, so its stored VideoWidth/VideoHeight
	// ratio is not its display aspect ratio.
	SARNum int
	SARDen int

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

// EncodingSummary renders the properties that matter when choosing an encoder
// setting, as a few lines of plain text.
//
// This exists because the AI encoding analysis previously received RawJSON —
// the complete "-show_streams -show_format" output. For a UHD REMUX with 48
// streams that is tens of kilobytes of mostly irrelevant detail, which buries
// the handful of facts that actually inform the decision and, on a small local
// model, reliably produces unusable answers.
//
// Average bitrate is included because it is the single strongest signal for how
// much headroom a re-encode has, and it is not directly present in the probe.
func (m *MediaInfo) EncodingSummary() string {
	var b strings.Builder

	codec := m.CodecName
	if codec == "" {
		codec = "unknown"
	}
	fmt.Fprintf(&b, "Video: %s %dx%d", codec, m.VideoWidth, m.VideoHeight)
	if m.BitDepth > 0 {
		fmt.Fprintf(&b, ", %d-bit", m.BitDepth)
	}
	if m.PixFmt != "" {
		fmt.Fprintf(&b, " (%s)", m.PixFmt)
	}
	if m.IsHDR() {
		fmt.Fprintf(&b, ", HDR transfer %s", m.ColorTransfer)
	}
	if m.IsDolbyVision() {
		fmt.Fprintf(&b, ", Dolby Vision profile %d", m.DVProfile)
	}
	b.WriteString("\n")

	if m.Duration > 0 {
		fmt.Fprintf(&b, "Duration: %.0f seconds\n", m.Duration)
	}
	if m.Size > 0 {
		fmt.Fprintf(&b, "Size: %.2f GB\n", float64(m.Size)/(1<<30))
		if m.Duration > 0 {
			mbps := (float64(m.Size) * 8) / m.Duration / 1e6
			fmt.Fprintf(&b, "Average bitrate: %.1f Mbps\n", mbps)
		}
	}
	fmt.Fprintf(&b, "Streams: %d video, %d audio, %d subtitle",
		m.VideoStreams, m.AudioStreams, m.SubtitleStreams)

	return b.String()
}

// BitsPerPixel returns the source's average bits per pixel per frame — total
// bitrate divided by pixel count and frame rate. This is the standard density
// measure for how much information an encode carries per picture, independent
// of resolution and duration, and is what IsAlreadyEfficient compares against
// to decide whether a source is already an efficient encode.
//
// Returns 0 when size, duration, dimensions, or frame rate aren't known — the
// zero value never satisfies IsAlreadyEfficient's "at or below the floor"
// check, so an unprobeable source is never mistakenly skipped.
func (m *MediaInfo) BitsPerPixel() float64 {
	if m.Size <= 0 || m.Duration <= 0 || m.VideoWidth <= 0 || m.VideoHeight <= 0 || m.FrameRate <= 0 {
		return 0
	}
	bitsPerSecond := float64(m.Size) * 8 / m.Duration
	pixelsPerSecond := float64(m.VideoWidth) * float64(m.VideoHeight) * m.FrameRate
	return bitsPerSecond / pixelsPerSecond
}

// parseFrameRate parses an ffprobe rational frame rate string ("24000/1001",
// "25/1") into frames per second. Returns 0 for "0/0" (ffprobe's way of
// saying unknown) or anything else unparseable.
func parseFrameRate(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}
	n, err1 := strconv.ParseFloat(num, 64)
	d, err2 := strconv.ParseFloat(den, 64)
	if err1 != nil || err2 != nil || d == 0 {
		return 0
	}
	return n / d
}

// parseRatio parses ffprobe's "N:D" ratio format (sample_aspect_ratio,
// display_aspect_ratio) into integers. Returns 0, 0 for anything that isn't
// a clean, positive N:D pair — including ffprobe's own "0:1" for "unknown".
func parseRatio(s string) (num, den int) {
	n, d, ok := strings.Cut(s, ":")
	if !ok {
		return 0, 0
	}
	numer, err1 := strconv.Atoi(strings.TrimSpace(n))
	denom, err2 := strconv.Atoi(strings.TrimSpace(d))
	if err1 != nil || err2 != nil || numer <= 0 || denom <= 0 {
		return 0, 0
	}
	return numer, denom
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
