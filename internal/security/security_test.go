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
