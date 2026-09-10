package jobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Vasteva/MediaConverter/internal/config"
	"github.com/Vasteva/MediaConverter/internal/media"
)

func TestPlanReplacement(t *testing.T) {
	cases := []struct {
		name          string
		source        string
		finalBaseName string
		wantFinal     string
		wantTemp      string
	}{
		{
			name:      "mkv source keeps its name when not renamed",
			source:    "/storage/movies/Ant-Man (2015)/Ant-Man.2015.REMUX.mkv",
			wantFinal: "/storage/movies/Ant-Man (2015)/Ant-Man.2015.REMUX.mkv",
			wantTemp:  "/storage/movies/Ant-Man (2015)/" + TempFilePrefix + "job1.mkv",
		},
		{
			// Container changes, so this is a retire-and-replace rather than an
			// overwrite: the .avi leaves the library and a .mkv takes its place.
			name:      "avi source becomes mkv when not renamed",
			source:    "/storage/movies/Old Film (1985)/old.avi",
			wantFinal: "/storage/movies/Old Film (1985)/old.mkv",
			wantTemp:  "/storage/movies/Old Film (1985)/" + TempFilePrefix + "job1.mkv",
		},
		{
			// The AI-cleaned title lands in the library, in the source's own
			// directory, regardless of the release name the file arrived under.
			name:          "AI-cleaned title is used",
			source:        "/storage/movies/Dolittle (2020)/Dolittle.2020.1080p.BluRay.x264-FuzerHD.mkv",
			finalBaseName: "Dolittle (2020)",
			wantFinal:     "/storage/movies/Dolittle (2020)/Dolittle (2020).mkv",
			wantTemp:      "/storage/movies/Dolittle (2020)/" + TempFilePrefix + "job1.mkv",
		},
		{
			// A dotted title (sequel year, decimal) must survive intact.
			name:          "dotted AI title survives",
			source:        "/storage/movies/9½ Weeks (1986)/9.5.Weeks.1986.mkv",
			finalBaseName: "9.5 Weeks (1986)",
			wantFinal:     "/storage/movies/9½ Weeks (1986)/9.5 Weeks (1986).mkv",
			wantTemp:      "/storage/movies/9½ Weeks (1986)/" + TempFilePrefix + "job1.mkv",
		},
		{
			// Defence: a stray separator in the name cannot move the output.
			name:          "path components in the name are stripped",
			source:        "/storage/movies/M (1931)/m.mkv",
			finalBaseName: "../../etc/M (1931)",
			wantFinal:     "/storage/movies/M (1931)/M (1931).mkv",
			wantTemp:      "/storage/movies/M (1931)/" + TempFilePrefix + "job1.mkv",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := planReplacement(tc.source, "job1", tc.finalBaseName)
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

	paths := planReplacement(source, "job1", "")
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

	paths := planReplacement(source, "job2", "")
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

	paths := planReplacement(source, "job3", "")
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

	paths := planReplacement(source, "job4", "")
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

// TestReintegrateRefusesToOverwriteUnrelatedFinalFile covers #47: promoting a
// container-changing replacement (movie.avi -> movie.mkv) used no guard on
// Final at all, so os.Rename's replace-on-collision semantics silently
// destroyed an unrelated movie.mkv that happened to already sit in the
// library next to the .avi being replaced.
func TestReintegrateRefusesToOverwriteUnrelatedFinalFile(t *testing.T) {
	dir := t.TempDir()
	mgr := testManager(t, dir)

	source := filepath.Join(dir, "library", "movies", "Old (1985)", "old.avi")
	write(t, source, "avi original")
	// Unrelated pre-existing file at the exact path old.avi's transcode would
	// be promoted to.
	unrelatedFinal := filepath.Join(dir, "library", "movies", "Old (1985)", "old.mkv")
	write(t, unrelatedFinal, "unrelated pre-existing file")

	paths := planReplacement(source, "job6", "")
	write(t, paths.Temp, "transcoded")

	if err := mgr.reintegrate(&Job{ID: "job6"}, paths); err == nil {
		t.Fatal("expected refusal to overwrite an unrelated file at the output path")
	}

	if got, _ := os.ReadFile(unrelatedFinal); string(got) != "unrelated pre-existing file" {
		t.Errorf("unrelated file at the output path was overwritten: %q", got)
	}
	// Nothing should have moved: refusing must happen before the source is
	// touched, not after it's already been relocated to holding.
	if got, _ := os.ReadFile(source); string(got) != "avi original" {
		t.Errorf("source was modified: %q", got)
	}
	held := filepath.Join(dir, "held", "movies", "Old (1985)", "old.avi")
	if _, statErr := os.Stat(held); !os.IsNotExist(statErr) {
		t.Error("source should not have been moved to holding when the refusal fires")
	}
}

// The common case — source and Final are the same path, e.g. movie.mkv
// replacing itself in place — must still work: Final legitimately "already
// exists" here (it's the very file about to move to holding), so the #47
// guard above must not fire on it.
func TestReintegrateAllowsSameContainerSelfReplacement(t *testing.T) {
	dir := t.TempDir()
	mgr := testManager(t, dir)

	source := filepath.Join(dir, "library", "movies", "Same (2020)", "same.mkv")
	write(t, source, "original")

	paths := planReplacement(source, "job7", "")
	if paths.Final != paths.Source {
		t.Fatalf("test assumption broken: Final (%s) != Source (%s)", paths.Final, paths.Source)
	}
	write(t, paths.Temp, "transcoded")

	if err := mgr.reintegrate(&Job{ID: "job7"}, paths); err != nil {
		t.Fatalf("reintegrate: %v", err)
	}
	got, err := os.ReadFile(paths.Final)
	if err != nil {
		t.Fatalf("reading promoted file: %v", err)
	}
	if string(got) != "transcoded" {
		t.Errorf("library file contains %q, want the transcode", got)
	}
}

// The scanner watches the library directory, so the rename that puts a
// transcode into place fires a watch event. reintegrate must claim the
// destination path (OnOutputClaimed) before that rename and before the file
// exists, so the watcher doesn't queue a second optimise of the job's own
// output. On success the claim is not released — the completion hook promotes
// it to a durable entry.
func TestReintegrateClaimsOutputBeforeItAppears(t *testing.T) {
	dir := t.TempDir()
	mgr := testManager(t, dir)

	source := filepath.Join(dir, "library", "movies", "Pressure (2026)", "pressure.mp4")
	write(t, source, "original")
	paths := planReplacement(source, "job8", "")
	write(t, paths.Temp, "transcoded")

	var claimed, released []string
	mgr.OnOutputClaimed = func(p string) {
		if _, err := os.Stat(paths.Final); err == nil {
			t.Errorf("output claimed after %s already existed on disk", paths.Final)
		}
		claimed = append(claimed, p)
	}
	mgr.OnOutputReleased = func(p string) { released = append(released, p) }

	if err := mgr.reintegrate(&Job{ID: "job8"}, paths); err != nil {
		t.Fatalf("reintegrate: %v", err)
	}
	if len(claimed) != 1 || claimed[0] != paths.Final {
		t.Errorf("claimed = %v, want [%s]", claimed, paths.Final)
	}
	if len(released) != 0 {
		t.Errorf("released = %v, want none on success", released)
	}
}

// When the promotion fails, the claim must be released so a real file that
// later lands at that path is still picked up.
func TestReintegrateReleasesOutputWhenPromotionFails(t *testing.T) {
	dir := t.TempDir()
	mgr := testManager(t, dir)

	source := filepath.Join(dir, "library", "movies", "Alpha (2018)", "alpha.mkv")
	write(t, source, "original")
	paths := planReplacement(source, "job9", "")
	// No temp file — the promotion rename fails.

	var claimed, released []string
	mgr.OnOutputClaimed = func(p string) { claimed = append(claimed, p) }
	mgr.OnOutputReleased = func(p string) { released = append(released, p) }

	if err := mgr.reintegrate(&Job{ID: "job9"}, paths); err == nil {
		t.Fatal("expected reintegrate to fail")
	}
	if len(claimed) != 1 || claimed[0] != paths.Final {
		t.Errorf("claimed = %v, want [%s]", claimed, paths.Final)
	}
	if len(released) != 1 || released[0] != paths.Final {
		t.Errorf("released = %v, want [%s]", released, paths.Final)
	}
}

// A source that is itself a prior replace-in-place output — already HEVC, with
// its original retained in the holding dir — must be skipped, not re-encoded
// only to die in reintegrate over the retained original.
func TestAlreadyReplacedInPlaceReason(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		SourceDir:      filepath.Join(dir, "library"),
		HoldingDir:     filepath.Join(dir, "held"),
		ReplaceInPlace: true,
	}
	source := filepath.Join(dir, "library", "movies", "Dolittle (2020)", "dolittle.mkv")
	hevc := &media.MediaInfo{CodecName: "hevc"}

	if r := alreadyReplacedInPlaceReason(cfg, source, hevc); r != "" {
		t.Errorf("no retained original yet — expected no skip, got %q", r)
	}

	write(t, holdingPathFor(cfg.HoldingDir, cfg.SourceDir, source), "the h264 original")

	if alreadyReplacedInPlaceReason(cfg, source, hevc) == "" {
		t.Error("HEVC source with a retained original at its holding path should be skipped")
	}
	if r := alreadyReplacedInPlaceReason(cfg, source, &media.MediaInfo{CodecName: "h264"}); r != "" {
		t.Errorf("a non-HEVC source must still run, got skip %q", r)
	}

	off := cfg
	off.ReplaceInPlace = false
	if r := alreadyReplacedInPlaceReason(off, source, hevc); r != "" {
		t.Errorf("replace-in-place off — expected no skip, got %q", r)
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
