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
