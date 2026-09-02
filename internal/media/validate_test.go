package media

import (
	"context"
	"os"
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
