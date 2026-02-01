package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsInDirectory(t *testing.T) {
	s := &Scanner{}
	tests := []struct {
		path string
		dir  string
		want bool
	}{
		{"/a/b/c", "/a/b", true},
		{"/a/b/c", "/a/b/d", false},
		{"/a/b/c", "/a/b/c", true},
		{"/a/b", "/a/b/c", false},
	}
	for _, tt := range tests {
		if got := s.isInDirectory(tt.path, tt.dir); got != tt.want {
			t.Errorf("isInDirectory(%q, %q) = %v, want %v", tt.path, tt.dir, got, tt.want)
		}
	}
}

func TestMatchesPatterns(t *testing.T) {
	s := &Scanner{}
	watchDir := WatchDirectory{
		IncludePatterns: []string{"*.mkv", "*.mp4"},
		ExcludePatterns: []string{"*sample*"},
	}

	tests := []struct {
		path string
		want bool
	}{
		{"test.mkv", true},
		{"test.mp4", true},
		{"test.avi", false},
		{"sample.mkv", false},
		{"my_sample_file.mp4", false},
	}
	for _, tt := range tests {
		if got := s.matchesPatterns(tt.path, watchDir); got != tt.want {
			t.Errorf("matchesPatterns(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestProcessedDB(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "processed.json")
	db := &ProcessedDB{
		filePath:  tmpFile,
		processed: make(map[string]ProcessedFile),
	}

	f := ProcessedFile{
		Path: "/test/file.mkv",
		Hash: "abc",
	}

	db.MarkProcessed(f)

	if !db.IsProcessed("/test/file.mkv") {
		t.Error("expected file to be marked as processed")
	}

	// Test persistence
	db2 := &ProcessedDB{
		filePath:  tmpFile,
		processed: make(map[string]ProcessedFile),
	}
	if err := db2.Load(); err != nil {
		t.Fatalf("failed to load DB: %v", err)
	}

	if !db2.IsProcessed("/test/file.mkv") {
		t.Error("expected file to be loaded from disk")
	}
}

func TestCalculateHash(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "hash_test.txt")
	os.WriteFile(tmpFile, []byte("hello world"), 0644)

	hash, err := calculateFileHash(tmpFile)
	if err != nil {
		t.Fatalf("failed to calculate hash: %v", err)
	}

	if hash == "" {
		t.Error("expected non-empty hash")
	}

	// Hash should be consistent
	hash2, _ := calculateFileHash(tmpFile)
	if hash != hash2 {
		t.Error("expected consistent hash")
	}
}
