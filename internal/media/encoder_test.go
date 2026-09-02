package media

import (
	"strings"
	"testing"
)

// newTestWrapper builds a wrapper without probing the host for binaries, so
// argument-construction tests run everywhere — including CI images and
// developer machines without FFmpeg installed. The existing
// TestFFmpegWrapper_BuildArgs skips in exactly those environments, which means
// the encoder configuration was never actually covered.
func newTestWrapper() *FFmpegWrapper {
	return &FFmpegWrapper{ffmpegPath: "ffmpeg", ffprobePath: "ffprobe"}
}

func argString(args []string) string { return strings.Join(args, " ") }

// TestStreamMapsAreOptional covers sources with no subtitle or audio track.
// Without the "?" suffix FFmpeg aborts with "Stream map '0:s' matches no
// streams" — which is most MP4s and a good many MKVs.
func TestStreamMapsAreOptional(t *testing.T) {
	args := argString(newTestWrapper().buildFFmpegArgs(TranscodeOptions{
		InputPath:  "/input/test.mkv",
		OutputPath: "/output/test.mkv",
		GPUVendor:  GPUVendorCPU,
		CRF:        23,
	}))
	for _, want := range []string{"-map 0:V", "-map 0:a?", "-map 0:s?"} {
		if !strings.Contains(args, want) {
			t.Errorf("expected %q in stream maps\ngot: %s", want, args)
		}
	}
}

// The VAAPI path relies on hwupload rather than a pinned surface format.
// allow_profile_mismatch lets codecs VAAPI cannot decode fall back to software
// decoding, and hwupload is what gets those frames onto the GPU. Replacing it
// with scale_vaapi would break that path — and hevc_vaapi negotiates Main 10
// from p010 input without being told to, verified on an Arc A310.
func TestVAAPIUsesHwuploadWithoutUpscale(t *testing.T) {
	args := argString(newTestWrapper().buildFFmpegArgs(TranscodeOptions{
		InputPath:  "/input/test.mkv",
		OutputPath: "/output/test.mkv",
		GPUVendor:  GPUVendorIntel,
		CRF:        23,
	}))
	for _, want := range []string{"-c:v hevc_vaapi", "-vf hwupload"} {
		if !strings.Contains(args, want) {
			t.Errorf("expected %q\ngot: %s", want, args)
		}
	}
	if strings.Contains(args, "-profile:v") {
		t.Errorf("VAAPI profile must be left to encoder negotiation\ngot: %s", args)
	}
}

// When upscaling, scale_vaapi replaces hwupload: it operates on GPU frames
// directly, and uploading frames already in hardware format would fail.
func TestVAAPIUpscaleReplacesHwupload(t *testing.T) {
	args := argString(newTestWrapper().buildFFmpegArgs(TranscodeOptions{
		InputPath:  "/input/test.mkv",
		OutputPath: "/output/test.mkv",
		GPUVendor:  GPUVendorIntel,
		CRF:        23,
		Upscale:    true,
		Resolution: "4k",
	}))
	if !strings.Contains(args, "scale_vaapi=3840:2160") {
		t.Errorf("expected scale_vaapi at 4k\ngot: %s", args)
	}
	if strings.Contains(args, "hwupload") {
		t.Errorf("upscale path must not also hwupload\ngot: %s", args)
	}
}

// HDR sources must carry their colour signalling to the output, or the result
// can play back washed out on an HDR display.
func TestColorSignallingIsPreserved(t *testing.T) {
	args := argString(newTestWrapper().buildFFmpegArgs(TranscodeOptions{
		InputPath:      "/input/test.mkv",
		OutputPath:     "/output/test.mkv",
		GPUVendor:      GPUVendorIntel,
		CRF:            23,
		ColorPrimaries: "bt2020",
		ColorTransfer:  "smpte2084",
		ColorSpace:     "bt2020nc",
	}))
	for _, want := range []string{
		"-color_primaries bt2020",
		"-color_trc smpte2084",
		"-colorspace bt2020nc",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("expected %q\ngot: %s", want, args)
		}
	}
}

func TestColorSignallingOmittedWhenUnknown(t *testing.T) {
	args := argString(newTestWrapper().buildFFmpegArgs(TranscodeOptions{
		InputPath:      "/input/test.mkv",
		OutputPath:     "/output/test.mkv",
		GPUVendor:      GPUVendorCPU,
		CRF:            23,
		ColorPrimaries: "unknown",
		ColorTransfer:  "",
	}))
	if strings.Contains(args, "-color_primaries") || strings.Contains(args, "-color_trc") {
		t.Errorf("unknown/empty colour tags must not be forwarded\ngot: %s", args)
	}
}

// TestVAAPIEnlargesSurfacePool pins the fix for the production failures.
//
// The default VAAPI surface pool is too small for 2160p 10-bit HEVC. The
// decoder exhausts it, logs "Cannot allocate memory" from get_buffer(), drops
// frames, and exits 0 anyway. Measured on an Arc A310 over a 60s sample of a
// 2160p REMUX: 77 decode errors with the default pool, 0 with 32 extra frames.
func TestVAAPIEnlargesSurfacePool(t *testing.T) {
	for _, vendor := range []GPUVendor{GPUVendorIntel, GPUVendorAMD} {
		t.Run(string(vendor), func(t *testing.T) {
			args := argString(newTestWrapper().buildFFmpegArgs(TranscodeOptions{
				InputPath:  "/input/test.mkv",
				OutputPath: "/output/test.mkv",
				GPUVendor:  vendor,
				CRF:        23,
			}))
			if !strings.Contains(args, "-extra_hw_frames 32") {
				t.Errorf("VAAPI input args must enlarge the surface pool\ngot: %s", args)
			}
		})
	}
}

// The pool option is an accelerator input option and must not leak into the
// software path, where there is no hardware frame context at all.
func TestCPUPathHasNoHardwareOptions(t *testing.T) {
	args := argString(newTestWrapper().buildFFmpegArgs(TranscodeOptions{
		InputPath:  "/input/test.mkv",
		OutputPath: "/output/test.mkv",
		GPUVendor:  GPUVendorCPU,
		CRF:        23,
	}))
	for _, unwanted := range []string{"-extra_hw_frames", "-hwaccel", "hwupload"} {
		if strings.Contains(args, unwanted) {
			t.Errorf("CPU path must not contain %q\ngot: %s", unwanted, args)
		}
	}
}

func TestNoStdinIsSet(t *testing.T) {
	args := argString(newTestWrapper().buildFFmpegArgs(TranscodeOptions{
		InputPath:  "/input/test.mkv",
		OutputPath: "/output/test.mkv",
		GPUVendor:  GPUVendorCPU,
		CRF:        23,
	}))
	if !strings.Contains(args, "-nostdin") {
		t.Errorf("expected -nostdin so ffmpeg never blocks on stdin\ngot: %s", args)
	}
}

func TestBitDepthFromPixFmt(t *testing.T) {
	cases := map[string]int{
		"yuv420p":     8,
		"nv12":        8,
		"yuv420p10le": 10,
		"yuv422p10le": 10,
		"p010":        10,
		"yuv420p12le": 12,
		"":            0,
	}
	for pixFmt, want := range cases {
		if got := bitDepthFromPixFmt(pixFmt); got != want {
			t.Errorf("bitDepthFromPixFmt(%q) = %d, want %d", pixFmt, got, want)
		}
	}
}

// TestEncodingSummary uses the real Ant-Man 2160p REMUX values. The AI encoding
// analysis previously received RawJSON — the entire "-show_streams -show_format"
// output, which for this 48-stream source runs to tens of kilobytes and buries
// the handful of facts the decision turns on.
func TestEncodingSummary(t *testing.T) {
	info := &MediaInfo{
		CodecName:       "hevc",
		VideoWidth:      3840,
		VideoHeight:     2160,
		BitDepth:        10,
		PixFmt:          "yuv420p10le",
		ColorTransfer:   "smpte2084",
		Duration:        7205,
		Size:            50852853982,
		VideoStreams:    1,
		AudioStreams:    3,
		SubtitleStreams: 45,
	}

	got := info.EncodingSummary()

	for _, want := range []string{
		"hevc 3840x2160",
		"10-bit",
		"yuv420p10le",
		"HDR transfer smpte2084",
		"47.36 GB",
		"Average bitrate: 56.5 Mbps",
		"1 video, 3 audio, 45 subtitle",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q\ngot:\n%s", want, got)
		}
	}

	// The point of the summary is that it is small.
	if len(got) > 400 {
		t.Errorf("summary is %d bytes, expected it to stay compact:\n%s", len(got), got)
	}
}

func TestEncodingSummaryHandlesSparseProbe(t *testing.T) {
	// ffprobe can fail to report most fields on a damaged or exotic file; the
	// summary must not panic or emit obvious nonsense.
	got := (&MediaInfo{}).EncodingSummary()
	if !strings.Contains(got, "unknown") {
		t.Errorf("expected an unknown codec to be labelled, got:\n%s", got)
	}
	if strings.Contains(got, "Average bitrate") {
		t.Errorf("bitrate must be omitted when duration is unknown, got:\n%s", got)
	}
}
