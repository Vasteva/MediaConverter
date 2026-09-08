package scanner

import (
	"context"
	"github.com/Vasteva/MediaConverter/internal/config"
	"github.com/Vasteva/MediaConverter/internal/jobs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

// TestScannerConfigValidateFloorsScanInterval covers #36: Validate() must
// never leave ScanIntervalSec at a value time.NewTicker would panic on.
func TestScannerConfigValidateFloorsScanInterval(t *testing.T) {
	cases := []struct {
		name  string
		input int
		want  int
	}{
		{"zero — the cleared-field-in-the-UI case", 0, DefaultScanIntervalSec},
		{"negative", -5, DefaultScanIntervalSec},
		{"below the floor", 30, DefaultScanIntervalSec},
		{"exactly the floor", MinScanIntervalSec, MinScanIntervalSec},
		{"comfortably above the floor", 900, 900},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &ScannerConfig{ScanIntervalSec: tc.input}
			c.Validate()
			if c.ScanIntervalSec != tc.want {
				t.Errorf("Validate() with ScanIntervalSec=%d left it at %d, want %d",
					tc.input, c.ScanIntervalSec, tc.want)
			}
			if c.ScanIntervalSec <= 0 {
				t.Fatalf("Validate() left ScanIntervalSec at %d — time.NewTicker would panic on this", c.ScanIntervalSec)
			}
		})
	}
}

// TestQueueFileValidatesPath covers #37: QueueFile takes a raw path
// straight from POST /api/scanner/queue with no other gate — unlike a file
// createJobForFile discovers itself, already confined to a configured watch
// directory — so it must reject anything outside SourceDir on its own.
func TestQueueFileValidatesPath(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	outside := filepath.Join(dir, "outside.mkv")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	inside := filepath.Join(sourceDir, "movie.mkv")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}

	cfg := &config.Config{SourceDir: sourceDir, DestDir: filepath.Join(dir, "dest")}
	jm, err := jobs.NewManager(cfg, nil, filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatalf("jobs.NewManager: %v", err)
	}

	scannerCfg := &ScannerConfig{
		Mode:               ScanModeManual,
		OptimizeExtensions: []string{".mkv"},
		ProcessedFilePath:  filepath.Join(dir, "processed.json"),
	}
	scannerCfg.Validate()
	s, err := NewScanner(scannerCfg, jm, filepath.Join(dir, "scanner_config.json"))
	if err != nil {
		t.Fatalf("NewScanner: %v", err)
	}
	t.Cleanup(s.Stop)

	if err := s.QueueFile(outside); err == nil {
		t.Error("expected QueueFile to reject a path outside SourceDir")
	}
	if err := s.QueueFile(inside); err != nil {
		t.Errorf("expected QueueFile to accept a path inside SourceDir, got: %v", err)
	}
}

// TestFailedJobDoesNotPermanentlyExcludeFileFromScans covers #48:
// createJobForFile used to write a durable ProcessedDB entry the moment a
// job was created, before the job had done anything — so a job that later
// failed left the file marked processed forever, and shouldProcessFile (the
// gate every scan checks before creating a job) would skip it on every scan
// after that, forever. MarkInFlight/ClearInFlight replace that with an entry
// CompleteProcessed clears on failure instead of promoting.
func TestFailedJobDoesNotPermanentlyExcludeFileFromScans(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	source := filepath.Join(sourceDir, "movie.mkv")
	if err := os.WriteFile(source, []byte("x"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	cfg := &config.Config{SourceDir: sourceDir, DestDir: filepath.Join(dir, "dest")}
	jm, err := jobs.NewManager(cfg, nil, filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatalf("jobs.NewManager: %v", err)
	}

	scannerCfg := &ScannerConfig{
		Mode:               ScanModeManual,
		AutoCreateJobs:     true,
		OptimizeExtensions: []string{".mkv"},
		ProcessedFilePath:  filepath.Join(dir, "processed.json"),
	}
	scannerCfg.Validate()
	s, err := NewScanner(scannerCfg, jm, filepath.Join(dir, "scanner_config.json"))
	if err != nil {
		t.Fatalf("NewScanner: %v", err)
	}
	t.Cleanup(s.Stop)

	watchDir := WatchDirectory{Path: sourceDir}

	if !s.shouldProcessFile(source, watchDir) {
		t.Fatal("test setup: file should be eligible before any job exists for it")
	}
	if err := s.createJobForFile(source); err != nil {
		t.Fatalf("createJobForFile: %v", err)
	}
	if s.shouldProcessFile(source, watchDir) {
		t.Fatal("expected the file to be ineligible while its job is in flight")
	}

	// The job fails.
	s.CompleteProcessed(&jobs.Job{SourcePath: source, Status: jobs.StatusFailed})

	if !s.shouldProcessFile(source, watchDir) {
		t.Error("a failed job left the file ineligible for future scans — it will never be retried")
	}
}

// TestCompletedJobPromotesInFlightEntryToDurable is the success-path
// counterpart to TestFailedJobDoesNotPermanentlyExcludeFileFromScans: a job
// that actually finishes must still leave the file durably marked processed,
// not merely in-flight, so it isn't queued again on the next scan.
func TestCompletedJobPromotesInFlightEntryToDurable(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	source := filepath.Join(sourceDir, "movie.mkv")
	if err := os.WriteFile(source, []byte("x"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	cfg := &config.Config{SourceDir: sourceDir, DestDir: filepath.Join(dir, "dest")}
	jm, err := jobs.NewManager(cfg, nil, filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatalf("jobs.NewManager: %v", err)
	}

	scannerCfg := &ScannerConfig{
		Mode:               ScanModeManual,
		OptimizeExtensions: []string{".mkv"},
		ProcessedFilePath:  filepath.Join(dir, "processed.json"),
	}
	scannerCfg.Validate()
	s, err := NewScanner(scannerCfg, jm, filepath.Join(dir, "scanner_config.json"))
	if err != nil {
		t.Fatalf("NewScanner: %v", err)
	}
	t.Cleanup(s.Stop)

	if err := s.QueueFile(source); err != nil {
		t.Fatalf("QueueFile: %v", err)
	}

	s.CompleteProcessed(&jobs.Job{SourcePath: source, Status: jobs.StatusCompleted})

	var found *ProcessedFile
	for _, f := range s.processedDB.GetAll() {
		if f.Path == source {
			f := f
			found = &f
		}
	}
	if found == nil {
		t.Fatal("expected a durable entry for the source after successful completion")
	}
	if found.InFlight {
		t.Error("entry is still marked in-flight after successful completion")
	}
	if found.ProcessedAt.IsZero() {
		t.Error("expected ProcessedAt to be set on the durable entry")
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

// TestConcurrentJobRunWithConfigAndScannerConfigChurn covers #43: config.Config
// was mutated in place with no synchronisation at all, Manager.ai was written
// under a mutex but read without one, ScannerConfig was read from scan
// goroutines without the lock UpdateConfig used to swap it, and Stop() could
// deadlock via a stopCh field read after another goroutine had already set it
// to nil. This runs a real JobTypeTest job through the worker pool while one
// goroutine hammers system config the way POST /api/config does and another
// repeatedly calls Scanner.UpdateConfig — which itself calls Stop() then
// Start() — the way POST /api/scanner/config does. None of it asserts much
// beyond "this doesn't crash, deadlock, or trip -race", which is the point:
// the four bugs this guards were only ever visible under -race or a hang.
func TestConcurrentJobRunWithConfigAndScannerConfigChurn(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}

	// config.Load(), not a bare &config.Config{}: Config.mu is nil-tolerant
	// specifically so single-threaded tests can skip Load() and construct a
	// literal directly (see config.go), but that tolerance means a bare
	// literal used under real concurrency — as this test needs — silently
	// gets NO locking at all. Load() is the only path that sets mu.
	cfg := config.Load()
	cfg.SourceDir = sourceDir
	cfg.DestDir = filepath.Join(dir, "dest")
	cfg.MaxConcurrentJobs = 2
	jm, err := jobs.NewManager(cfg, nil, filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatalf("jobs.NewManager: %v", err)
	}
	jm.Start()
	defer jm.Stop()

	scannerCfg := &ScannerConfig{
		Mode:               ScanModeManual,
		OptimizeExtensions: []string{".mkv"},
		ProcessedFilePath:  filepath.Join(dir, "processed.json"),
	}
	scannerCfg.Validate()
	s, err := NewScanner(scannerCfg, jm, filepath.Join(dir, "scanner_config.json"))
	if err != nil {
		t.Fatalf("NewScanner: %v", err)
	}
	defer s.Stop()

	job := &jobs.Job{ID: "race-job", Type: jobs.JobTypeTest, Status: jobs.StatusPending}
	jm.AddJob(job)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Hammer system config the same way POST /api/config does.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; ; n++ {
			select {
			case <-stop:
				return
			default:
			}
			cfg.WithLock(func() {
				cfg.CRF = n % 51
				cfg.DeleteSource = n%2 == 0
			})
			_ = jm.GetConfig()
		}
	}()

	// Hammer scanner config the same way POST /api/scanner/config does.
	// UpdateConfig itself calls Stop() then Start() — exactly where the
	// stopCh-nil-after-close race lived.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; ; n++ {
			select {
			case <-stop:
				return
			default:
			}
			newCfg := &ScannerConfig{
				Mode:               ScanModeManual,
				OptimizeExtensions: []string{".mkv"},
				ProcessedFilePath:  filepath.Join(dir, "processed.json"),
				DefaultPriority:    n,
			}
			if err := s.UpdateConfig(newCfg); err != nil {
				t.Errorf("UpdateConfig: %v", err)
				return
			}
		}
	}()

	// Read scanner state concurrently, as the HTTP handlers do.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = s.GetConfig()
			_, _ = s.Discover()
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	if status := job.GetStatus(); status == jobs.StatusPending {
		t.Errorf("expected job to be picked up by the worker, still Pending")
	}
	jm.CancelJob(job.ID)
}

// TestScanSkipsHiddenDirectories covers the holding directory used by
// replace-in-place. It sits inside the media root and holds the originals of
// already-converted titles, so a scan that descended into it would queue every
// replaced original for conversion again — undoing the work and refilling the
// disk. The same rule covers MakeMKV's .extract_ temp directories.
func TestScanSkipsHiddenDirectories(t *testing.T) {
	root := t.TempDir()

	mustWrite := func(rel string) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		return p
	}

	wanted := mustWrite("movies/Aliens (1986)/aliens.mkv")
	mustWrite(".vastiva-replaced/movies/Aliens (1986)/aliens.mkv") // held original
	mustWrite(".extract_job1/title.mkv")                           // in-progress extraction
	mustWrite("movies/Alpha (2018)/" + jobs.TempFilePrefix + "j2.mkv")

	// scanDirectory selects on s.ctx, so it must be set.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Scanner{
		ctx: ctx,
		config: &ScannerConfig{
			OptimizeExtensions: []string{".mkv"},
			ExtractExtensions:  []string{".iso"},
		},
	}

	found, err := s.scanDirectory(WatchDirectory{Path: root, Recursive: true})
	if err != nil {
		t.Fatalf("scanDirectory: %v", err)
	}

	if len(found) != 1 || found[0] != wanted {
		t.Errorf("expected only %q, got %v", wanted, found)
	}
}
