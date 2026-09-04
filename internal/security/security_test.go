package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "security_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	allowedBase := filepath.Join(tmpDir, "allowed")
	os.MkdirAll(allowedBase, 0755)

	tests := []struct {
		name         string
		path         string
		allowedBases []string
		wantErr      bool
	}{
		{
			name:         "valid absolute path",
			path:         filepath.Join(allowedBase, "test.txt"),
			allowedBases: []string{allowedBase},
			wantErr:      false,
		},
		{
			name:         "valid relative path",
			path:         "test.txt",
			allowedBases: []string{allowedBase},
			wantErr:      false,
		},
		{
			name:         "directory traversal attempt",
			path:         "../../etc/passwd",
			allowedBases: []string{allowedBase},
			wantErr:      true,
		},
		{
			name:         "path outside base (absolute)",
			path:         "/tmp/outside.txt",
			allowedBases: []string{allowedBase},
			wantErr:      true,
		},
		{
			name:         "empty path",
			path:         "",
			allowedBases: []string{allowedBase},
			wantErr:      true,
		},
		{
			name:         "multiple bases - second one matches",
			path:         filepath.Join(tmpDir, "other", "test.txt"),
			allowedBases: []string{allowedBase, filepath.Join(tmpDir, "other")},
			wantErr:      false,
		},
		{
			name:         "adjacent directory bypass",
			path:         allowedBase + "_extra/test.txt",
			allowedBases: []string{allowedBase},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create directories for test if needed
			for _, base := range tt.allowedBases {
				os.MkdirAll(base, 0755)
			}

			got, err := ValidatePath(tt.path, tt.allowedBases...)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if !strings.HasPrefix(got, allowedBase) && len(tt.allowedBases) == 1 {
					t.Errorf("ValidatePath() got = %v, expected in %v", got, allowedBase)
				}
			}
		})
	}
}

// TestValidatePathResolvesSymlinks covers #37: filepath.Clean is purely
// lexical, so a path that reads as contained can still point outside the
// sandbox via a symlink.
func TestValidatePathResolvesSymlinks(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatalf("mkdir allowed: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("writing secret.txt: %v", err)
	}

	t.Run("existing symlink escaping the sandbox is rejected", func(t *testing.T) {
		link := filepath.Join(allowed, "escape")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		target := filepath.Join(link, "secret.txt")
		if _, err := ValidatePath(target, allowed); err == nil {
			t.Errorf("expected a symlink pointing outside %s to be rejected, got no error", allowed)
		}
	})

	t.Run("symlink pointing back inside the sandbox is accepted", func(t *testing.T) {
		realDir := filepath.Join(allowed, "real")
		if err := os.MkdirAll(realDir, 0o755); err != nil {
			t.Fatalf("mkdir real: %v", err)
		}
		link := filepath.Join(allowed, "loop")
		if err := os.Symlink(realDir, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		got, err := ValidatePath(filepath.Join(link, "file.mkv"), allowed)
		if err != nil {
			t.Fatalf("expected an in-sandbox symlink to be accepted, got: %v", err)
		}
		wantPrefix, _ := filepath.EvalSymlinks(realDir)
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("resolved path %s does not start with the real directory %s", got, wantPrefix)
		}
	})

	t.Run("not-yet-existing file behind an escaping symlink is still rejected", func(t *testing.T) {
		// The classic case for a destination path: the file being written
		// doesn't exist yet, but its parent directory is a symlink to
		// somewhere outside the sandbox.
		link := filepath.Join(allowed, "escape2")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		target := filepath.Join(link, "new_output.mkv")
		if _, err := ValidatePath(target, allowed); err == nil {
			t.Errorf("expected a not-yet-existing path behind an escaping symlink to be rejected, got no error")
		}
	})

	t.Run("the allowed base itself being a symlink still works", func(t *testing.T) {
		realBase := filepath.Join(root, "real_base")
		if err := os.MkdirAll(realBase, 0o755); err != nil {
			t.Fatalf("mkdir real_base: %v", err)
		}
		linkedBase := filepath.Join(root, "linked_base")
		if err := os.Symlink(realBase, linkedBase); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		got, err := ValidatePath(filepath.Join(linkedBase, "movie.mkv"), linkedBase)
		if err != nil {
			t.Fatalf("expected a path inside a symlinked base to be accepted, got: %v", err)
		}
		wantPrefix, _ := filepath.EvalSymlinks(realBase)
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("resolved path %s does not start with the real base %s", got, wantPrefix)
		}
	})
}

func TestMaskKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "long key",
			key:  "sk-1234567890abcdef",
			want: "sk-1....cdef",
		},
		{
			name: "short key",
			key:  "short",
			want: "****",
		},
		{
			name: "exact 8 chars",
			key:  "12345678",
			want: "****",
		},
		{
			name: "empty key",
			key:  "",
			want: "****",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskKey(tt.key); got != tt.want {
				t.Errorf("MaskKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
