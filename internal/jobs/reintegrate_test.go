package jobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Vasteva/MediaConverter/internal/config"
)

func TestPlanReplacement(t *testing.T) {
	cases := []struct {
		name      string
		source    string
		wantFinal string
		wantTemp  string
	}{
		{
			name:      "mkv source keeps its name",
			source:    "/storage/movies/Ant-Man (2015)/Ant-Man.2015.REMUX.mkv",
			wantFinal: "/storage/movies/Ant-Man (2015)/Ant-Man.2015.REMUX.mkv",
			wantTemp:  "/storage/movies/Ant-Man (2015)/" + TempFilePrefix + "job1.mkv",
		},
		{
			// Container changes, so this is a retire-and-replace rather than an
			// overwrite: the .avi leaves the library and a .mkv takes its place.
			name:      "avi source becomes mkv",
			source:    "/storage/movies/Old Film (1985)/old.avi",
			wantFinal: "/storage/movies/Old Film (1985)/old.mkv",
			wantTemp:  "/storage/movies/Old Film (1985)/" + TempFilePrefix + "job1.mkv",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := planReplacement(tc.source, "job1")
			if got.Final != tc.wantFinal {
				t.Errorf("Final = %q, want %q", got.Final, tc.wantFinal)
			}
			if got.Temp != tc.wantTemp {
				t.Errorf("Temp = %q, want %q", got.Temp, tc.wantTemp)
			}
			if filepath.Dir(got.Temp) != filepath.Dir(got.Final) {
				t.Error("temp and final must share a directory, or the rename is not atomic")
			}
		})
	}
}

// The holding path mirrors the library layout, so two films with the same
// filename in different folders cannot overwrite each other's original.
func TestHoldingPathPreservesLayout(t *testing.T) {
	got := holdingPathFor("/storage/.held", "/storage", "/storage/movies/Aliens (1986)/aliens.mkv")
	want := "/storage/.held/movies/Aliens (1986)/aliens.mkv"
	if got != want {
		t.Errorf("holdingPathFor = %q, want %q", got, want)
	}
}

func TestHoldingPathOutsideRootFallsBackToBasename(t *testing.T) {
	got := holdingPathFor("/storage/.held", "/storage", "/elsewhere/movie.mkv")
	want := "/storage/.held/movie.mkv"
	if got != want {
		t.Errorf("holdingPathFor = %q, want %q", got, want)
	}
}

// testManager builds a manager whose media root and holding directory live
// under dir.
func testManager(t *testing.T, dir string) *Manager {
	t.Helper()
	mgr, err := NewManager(&config.Config{
		MaxConcurrentJobs: 1,
		SourceDir:         filepath.Join(dir, "library"),
		HoldingDir:        filepath.Join(dir, "held"),
		ReplaceInPlace:    true,
		PUID:              -1,
		PGID:              -1,
	}, nil, "")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestReintegrateSwapsFileAndRetainsOriginal(t *testing.T) {
	dir := t.TempDir()
	mgr := testManager(t, dir)

	source := filepath.Join(dir, "library", "movies", "Aliens (1986)", "aliens.mkv")
	write(t, source, "original content")

	paths := planReplacement(source, "job1")
	write(t, paths.Temp, "transcoded content")

	if err := mgr.reintegrate(&Job{ID: "job1"}, paths); err != nil {
		t.Fatalf("reintegrate: %v", err)
	}

	// The transcode now occupies the library position.
	got, err := os.ReadFile(paths.Final)
	if err != nil {
		t.Fatalf("reading promoted file: %v", err)
	}
	if string(got) != "transcoded content" {
		t.Errorf("library file contains %q, want the transcode", got)
	}

	// The original is retained, not deleted — this is what makes a bad batch
	// reversible.
	held := filepath.Join(dir, "held", "movies", "Aliens (1986)", "aliens.mkv")
	original, err := os.ReadFile(held)
	if err != nil {
		t.Fatalf("original was not retained at %s: %v", held, err)
	}
	if string(original) != "original content" {
		t.Errorf("held file contains %q, want the original", original)
	}

	// No temp file left behind.
	if _, err := os.Stat(paths.Temp); !os.IsNotExist(err) {
		t.Error("temp file still present after promotion")
	}
}

// A source whose container changes leaves the library with the new file and
// without the old one.
func TestReintegrateRetiresDifferentContainer(t *testing.T) {
	dir := t.TempDir()
	mgr := testManager(t, dir)

	source := filepath.Join(dir, "library", "movies", "Old (1985)", "old.avi")
	write(t, source, "original")

	paths := planReplacement(source, "job2")
	write(t, paths.Temp, "transcoded")

	if err := mgr.reintegrate(&Job{ID: "job2"}, paths); err != nil {
		t.Fatalf("reintegrate: %v", err)
	}

	if _, err := os.Stat(paths.Final); err != nil {
		t.Errorf("expected %s in the library: %v", paths.Final, err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Error("the .avi original should have left the library")
	}
}

// If promotion fails after the original has been moved, the original must be
// restored — the library is never left without the title.
func TestReintegrateRestoresOriginalWhenPromotionFails(t *testing.T) {
	dir := t.TempDir()
	mgr := testManager(t, dir)

	source := filepath.Join(dir, "library", "movies", "Alpha (2018)", "alpha.mkv")
	write(t, source, "original content")

	paths := planReplacement(source, "job3")
	// Deliberately do NOT create the temp file, so the promotion rename fails.

	err := mgr.reintegrate(&Job{ID: "job3"}, paths)
	if err == nil {
		t.Fatal("expected reintegrate to fail when the transcode is missing")
	}

	got, readErr := os.ReadFile(source)
	if readErr != nil {
		t.Fatalf("original was not restored to the library: %v", readErr)
	}
	if string(got) != "original content" {
		t.Errorf("restored file contains %q, want the original", got)
	}

	held := filepath.Join(dir, "held", "movies", "Alpha (2018)", "alpha.mkv")
	if _, statErr := os.Stat(held); !os.IsNotExist(statErr) {
		t.Error("original should not remain in holding after being restored")
	}
}

// A retained original is the only remaining copy, so a second job must never
// overwrite one.
func TestReintegrateRefusesToOverwriteHeldOriginal(t *testing.T) {
	dir := t.TempDir()
	mgr := testManager(t, dir)

	source := filepath.Join(dir, "library", "movies", "Aliens (1986)", "aliens.mkv")
	write(t, source, "second original")
	write(t, filepath.Join(dir, "held", "movies", "Aliens (1986)", "aliens.mkv"), "first original")

	paths := planReplacement(source, "job4")
	write(t, paths.Temp, "transcoded")

	if err := mgr.reintegrate(&Job{ID: "job4"}, paths); err == nil {
		t.Fatal("expected refusal to overwrite an already-held original")
	}

	// Both files must be untouched.
	if got, _ := os.ReadFile(source); string(got) != "second original" {
		t.Errorf("source was modified: %q", got)
	}
	held, _ := os.ReadFile(filepath.Join(dir, "held", "movies", "Aliens (1986)", "aliens.mkv"))
	if string(held) != "first original" {
		t.Errorf("held original was overwritten: %q", held)
	}
}

// cleanupTemp must only ever remove files it created.
func TestCleanupTempRefusesRealFiles(t *testing.T) {
	dir := t.TempDir()
	mgr := testManager(t, dir)

	real := filepath.Join(dir, "library", "aliens.mkv")
	write(t, real, "precious")

	mgr.cleanupTemp(&Job{ID: "job5"}, real)

	if _, err := os.Stat(real); err != nil {
		t.Fatal("cleanupTemp deleted a file that is not a temp transcode")
	}

	temp := filepath.Join(dir, "library", TempFilePrefix+"job5.mkv")
	write(t, temp, "scratch")
	mgr.cleanupTemp(&Job{ID: "job5"}, temp)
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Error("cleanupTemp did not remove a temp file")
	}
}
