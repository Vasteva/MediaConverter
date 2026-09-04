package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCheckSourceSupported(t *testing.T) {
	cases := []struct {
		name      string
		info      *MediaInfo
		wantError bool
	}{
		{"nil info", nil, false},
		{"no dolby vision", &MediaInfo{DVProfile: 0}, false},
		{"profile 5 has no HDR10 base layer", &MediaInfo{DVProfile: 5, Filename: "x.mkv"}, true},
		{"profile 7 is backwards compatible", &MediaInfo{DVProfile: 7}, false},
		{"profile 8 is backwards compatible", &MediaInfo{DVProfile: 8}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckSourceSupported(tc.info)
			if tc.wantError && err == nil {
				t.Error("expected an error, got nil")
			}
			if !tc.wantError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// The size checks in ValidateOutput run before any ffprobe call, so they are
// exercisable without FFmpeg present. These are the cases that let the eight
// dead outputs through the old size>0 gate.
func TestValidateOutputRejectsBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	wrapper := newTestWrapper()

	write := func(name string, size int) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}
		return p
	}

	// A 60 GB REMUX source, as in the observed failures.
	src := &MediaInfo{Size: 60 << 30, Duration: 7200, AudioStreams: 1}

	t.Run("missing output", func(t *testing.T) {
		err := wrapper.ValidateOutput(context.Background(), src, filepath.Join(dir, "absent.mkv"))
		if err == nil {
			t.Fatal("expected a missing output to be rejected")
		}
	})

	t.Run("zero byte output", func(t *testing.T) {
		if err := wrapper.ValidateOutput(context.Background(), src, write("zero.mkv", 0)); err == nil {
			t.Fatal("expected a 0-byte output to be rejected")
		}
	})

	t.Run("truncated stub", func(t *testing.T) {
		// 2.7 MB from a 60 GB source — the real Alien Resurrection case, which
		// the previous size>0 check accepted as grounds for deleting the source.
		err := wrapper.ValidateOutput(context.Background(), src, write("stub.mkv", 2_783_983))
		if err == nil {
			t.Fatal("expected a truncated stub to be rejected")
		}
		var vErr *OutputValidationError
		if !asValidationError(err, &vErr) {
			t.Fatalf("expected *OutputValidationError, got %T", err)
		}
		if len(vErr.Reasons) == 0 {
			t.Error("validation error should explain why the output was rejected")
		}
	})
}

func asValidationError(err error, target **OutputValidationError) bool {
	v, ok := err.(*OutputValidationError)
	if ok {
		*target = v
	}
	return ok
}

// generateTestVideo creates a short, real, ffprobe-readable MKV using
// ffmpeg's built-in test source, so the duration-check path below can run
// against an actual probe rather than only the pre-probe size checks.
// Skips the calling test if ffmpeg itself is unavailable, matching the
// pattern used elsewhere in this package (see TestFFmpegWrapper_BuildArgs).
//
// Uses a per-pixel random-noise source rather than the smoother testsrc
// pattern: testsrc's analytic gradient predicts almost perfectly under H.264,
// so even a lossless encode of a few seconds lands at tens of kilobytes —
// under ValidateExtractedOutput's 1 MB floor, which would make the
// duration-check test below fail on the floor check instead of exercising
// what it's meant to test. Noise defeats that prediction and reliably clears
// the floor in a couple of seconds of real encode time.
func generateTestVideo(t *testing.T, path string, durationSec int) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("nullsrc=s=320x240:d=%d:r=10,geq=random(1)*255:128:128", durationSec),
		"-c:v", "libx264", "-qp", "0", "-y", path)
	if err := cmd.Run(); err != nil {
		t.Skipf("could not generate test video with ffmpeg: %v", err)
	}
}

// The size and existence checks in ValidateExtractedOutput run before any
// ffprobe call, exactly like ValidateOutput's — see
// TestValidateOutputRejectsBrokenFiles. This is the gate that replaced the
// bare fi.Size() > 0 check in runExtraction, which had no floor at all and
// accepted a disc-read-error stub as a complete extraction.
func TestValidateExtractedOutputRejectsBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	wrapper := newTestWrapper()

	t.Run("missing output", func(t *testing.T) {
		err := wrapper.ValidateExtractedOutput(context.Background(), 3600, filepath.Join(dir, "absent.mkv"))
		if err == nil {
			t.Fatal("expected a missing output to be rejected")
		}
	})

	t.Run("zero byte output", func(t *testing.T) {
		p := filepath.Join(dir, "zero.mkv")
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}
		if err := wrapper.ValidateExtractedOutput(context.Background(), 3600, p); err == nil {
			t.Fatal("expected a 0-byte output to be rejected")
		}
	})

	t.Run("stub below the size floor", func(t *testing.T) {
		p := filepath.Join(dir, "stub.mkv")
		if err := os.WriteFile(p, make([]byte, 1024), 0o600); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}
		err := wrapper.ValidateExtractedOutput(context.Background(), 3600, p)
		if err == nil {
			t.Fatal("expected a sub-floor stub to be rejected")
		}
		var vErr *OutputValidationError
		if !asValidationError(err, &vErr) {
			t.Fatalf("expected *OutputValidationError, got %T", err)
		}
	})
}

// TestValidateExtractedOutputCatchesTruncation exercises the actual failure
// mode this function exists for: MakeMKV exits 0 after a scratched disc or a
// drive read error truncates the title partway through, leaving a real,
// probeable, but short MKV behind. That is exactly what the old
// fi.Size() > 0 gate in runExtraction accepted as a complete extraction —
// which is how a source disc image was deleted while the only remaining copy
// was a stub.
func TestValidateExtractedOutputCatchesTruncation(t *testing.T) {
	wrapper, err := NewFFmpegWrapper()
	if err != nil {
		t.Skip("FFmpeg not available, skipping test")
	}

	dir := t.TempDir()
	out := filepath.Join(dir, "extracted.mkv")
	generateTestVideo(t, out, 2) // a real, 2-second, playable file

	t.Run("duration close to what the disc reported passes", func(t *testing.T) {
		if err := wrapper.ValidateExtractedOutput(context.Background(), 2, out); err != nil {
			t.Fatalf("expected a matching-duration output to pass, got: %v", err)
		}
	})

	t.Run("output far shorter than the disc title is rejected", func(t *testing.T) {
		// MakeMKV reported a 5-minute title; what actually landed on disk is 2
		// seconds — the shape of a read error that truncated the extraction
		// without a nonzero exit code.
		err := wrapper.ValidateExtractedOutput(context.Background(), 300, out)
		if err == nil {
			t.Fatal("expected a truncated extraction to be rejected")
		}
		var vErr *OutputValidationError
		if !asValidationError(err, &vErr) || len(vErr.Reasons) == 0 {
			t.Fatalf("expected a descriptive *OutputValidationError, got %v (%T)", err, err)
		}
	})
}
