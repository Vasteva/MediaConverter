package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vasteva/MediaConverter/internal/jobs"
	"github.com/Vasteva/MediaConverter/internal/media"
	"github.com/Vasteva/MediaConverter/internal/security"
	"github.com/Vasteva/MediaConverter/internal/util"
	"github.com/fsnotify/fsnotify"
)

// ScanMode defines how the scanner operates
type ScanMode string

const (
	ScanModeManual   ScanMode = "manual"   // No automatic scanning
	ScanModeStartup  ScanMode = "startup"  // Scan once on startup
	ScanModePeriodic ScanMode = "periodic" // Periodic scans at interval
	ScanModeWatch    ScanMode = "watch"    // Real-time file system watching
	ScanModeHybrid   ScanMode = "hybrid"   // Startup + Watch + Periodic backup
)

// MinScanIntervalSec is the floor for ScanIntervalSec — time.NewTicker
// panics on zero or negative, and this is also the value the settings UI's
// number input already advertises as its minimum. Exported so routes.go can
// reject an out-of-range POST /api/scanner/config with the same number
// Validate() would otherwise silently repair it to.
//
// DefaultScanIntervalSec is what Validate() floors an out-of-range value to,
// matching system Config's own SCANNER_INTERVAL_SEC default so a repaired
// value looks like a deliberate choice rather than an arbitrary one.
const (
	MinScanIntervalSec     = 60
	DefaultScanIntervalSec = 300
)

// WatchDirectory represents a directory to monitor
type WatchDirectory struct {
	Path              string   `json:"path"`
	Recursive         bool     `json:"recursive"`
	IncludePatterns   []string `json:"includePatterns"` // e.g., ["*.mkv", "*.iso"]
	ExcludePatterns   []string `json:"excludePatterns"` // e.g., ["*_optimized.mkv"]
	MinFileSizeMB     int64    `json:"minFileSizeMB"`
	MinFileAgeMinutes int      `json:"minFileAgeMinutes"` // Wait before processing new files
}

// ScannerConfig holds all scanner configuration
type ScannerConfig struct {
	Mode                ScanMode         `json:"mode"`
	Enabled             bool             `json:"enabled"`
	WatchDirectories    []WatchDirectory `json:"watchDirectories"`
	ScanIntervalSec     int              `json:"scanIntervalSec"` // For periodic mode
	AutoCreateJobs      bool             `json:"autoCreateJobs"`
	AutoCreateSubtitles bool             `json:"autoCreateSubtitles"`
	AutoUpscale         bool             `json:"autoUpscale"` // Default: false — opt-in per job, never a silent default (#42)
	AutoResolution      string           `json:"autoResolution"`
	ProcessedFilePath   string           `json:"processedFilePath"` // Track processed files

	// Job creation settings
	DefaultPriority int    `json:"defaultPriority"`
	OutputDirectory string `json:"outputDirectory"`

	// File type handling
	ExtractExtensions  []string `json:"extractExtensions"`  // e.g., [".iso"]
	OptimizeExtensions []string `json:"optimizeExtensions"` // e.g., [".mkv", ".mp4", ".avi"]
}

func (c *ScannerConfig) Validate() {
	if len(c.ExtractExtensions) == 0 {
		c.ExtractExtensions = []string{".iso", ".img", ".mdf"}
	}
	if len(c.OptimizeExtensions) == 0 {
		c.OptimizeExtensions = []string{
			".mkv", ".mp4", ".avi", ".mov", ".m4v",
			".mpg", ".mpeg", ".wmv", ".flv", ".webm",
		}
	}
	if c.ProcessedFilePath == "" {
		c.ProcessedFilePath = "/data/processed.json"
	}
	if c.Mode == "" {
		c.Mode = ScanModeManual
	}
	// Floor, not just a zero-check: time.NewTicker panics on any non-positive
	// duration, and periodicScan builds one straight from this field with no
	// check of its own. A cleared Settings UI field previously arrived here
	// as 0 (see routes.go's rejection of the same case for how), which
	// crashed the whole process the moment periodic/hybrid mode next ticked.
	// Applies regardless of Mode — irrelevant in manual/startup/watch, but
	// keeps this the one place that guarantees no live ticker ever sees a
	// bad value (#36).
	if c.ScanIntervalSec < MinScanIntervalSec {
		c.ScanIntervalSec = DefaultScanIntervalSec
	}
	// AutoUpscale has no non-zero default to apply here — Go's zero value
	// for a bool already is the "off" default this field wants, and every
	// path that builds a ScannerConfig (LoadScannerConfig's defaults, a
	// freshly-unmarshaled scanner_config.json, BodyParser on POST
	// /api/scanner/config) leaves an absent key at that same zero value.
	// Recorded here so the absence of a default-setting line isn't mistaken
	// for an oversight (#42).
}

type ScanStatus struct {
	IsScanning   bool      `json:"isScanning"`
	CurrentPath  string    `json:"currentPath"`
	FilesScanned int       `json:"filesScanned"`
	LastScan     time.Time `json:"lastScan"`
	LastResult   string    `json:"lastResult"`
	LastError    string    `json:"lastError"`
	Duration     string    `json:"duration"`
}

// Scanner manages automatic file discovery and job creation
type Scanner struct {
	config         *ScannerConfig
	configFilePath string
	jobManager     *jobs.Manager
	watcher        *fsnotify.Watcher
	processedDB    *ProcessedDB
	mu             sync.RWMutex
	// Status
	status   ScanStatus
	statusMu sync.RWMutex

	isScanning atomic.Bool
	pendingMu  sync.Mutex
	pending    map[string]time.Time

	stopCh chan struct{}
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// ProcessedDB tracks files that have been processed
type ProcessedDB struct {
	mu        sync.RWMutex
	filePath  string
	processed map[string]ProcessedFile
}

// ProcessedFile contains metadata about a processed file
type ProcessedFile struct {
	Path        string    `json:"path"`
	Hash        string    `json:"hash"`
	ProcessedAt time.Time `json:"processedAt"`
	JobID       string    `json:"jobId"`
	JobType     string    `json:"jobType"`
	InputSize   int64     `json:"inputSize"`
	OutputSize  int64     `json:"outputSize"`
	AISubtitles bool      `json:"aiSubtitles"`
	AIUpscale   bool      `json:"aiUpscale"`
	AICleaned   bool      `json:"aiCleaned"`

	// InFlight marks an entry written when a job was merely created for this
	// path, not when it finished (#48). It exists only to stop a second scan
	// from queuing the same file again while a job for it is still pending or
	// processing; ClearInFlight removes it if that job fails, so a transient
	// failure doesn't permanently hide the file from future scans the way a
	// durable entry would.
	InFlight bool `json:"inFlight,omitempty"`
}

// NewScanner creates a new file scanner
func NewScanner(config *ScannerConfig, jobManager *jobs.Manager, configFilePath string) (*Scanner, error) {
	if config == nil {
		return nil, fmt.Errorf("scanner config is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize processed file database
	processedDB, err := NewProcessedDB(config.ProcessedFilePath)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize processed DB: %w", err)
	}

	scanner := &Scanner{
		config:         config,
		configFilePath: configFilePath,
		jobManager:     jobManager,
		processedDB:    processedDB,
		stopCh:         make(chan struct{}),
		pending:        make(map[string]time.Time),
		ctx:            ctx,
		cancel:         cancel,
	}

	// Try to load persisted config, overriding defaults
	if err := scanner.loadConfig(); err != nil {
		log.Printf("[Scanner] No persisted config found or failed to load, using defaults: %v", err)
	} else {
		log.Println("[Scanner] Loaded settings from persistence")
		// Re-initialize processedDB if the config file specified a different path
		if scanner.config.ProcessedFilePath != processedDB.filePath {
			if newDB, dbErr := NewProcessedDB(scanner.config.ProcessedFilePath); dbErr == nil {
				scanner.processedDB = newDB
			} else {
				log.Printf("[Scanner] Warning: could not re-init processedDB at %s: %v — keeping original", scanner.config.ProcessedFilePath, dbErr)
			}
		}
	}

	// Initialize file watcher if needed
	if scanner.config.Mode == ScanModeWatch || scanner.config.Mode == ScanModeHybrid {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create file watcher: %w", err)
		}
		scanner.watcher = watcher
	}

	return scanner, nil
}

// Start begins the scanner based on configured mode
func (s *Scanner) Start() error {
	// One read of the config pointer for this call: s.config is swapped
	// wholesale (never mutated in place) by UpdateConfig under s.mu, so a
	// single locked read is enough to make every reference below consistent
	// with itself, rather than each racing UpdateConfig independently (#43).
	cfg := s.GetConfig()

	if !cfg.Enabled {
		log.Println("[Scanner] Disabled, not starting")
		return nil
	}

	log.Printf("[Scanner] Starting in %s mode", cfg.Mode)

	switch cfg.Mode {
	case ScanModeManual:
		// Do nothing, manual scans only
		return nil

	case ScanModeStartup:
		// Single scan on startup
		return s.ScanAll()

	case ScanModePeriodic:
		// Periodic scanning
		s.wg.Add(1)
		go s.periodicScan()
		return nil

	case ScanModeWatch:
		// Real-time watching
		if err := s.setupWatchers(); err != nil {
			return err
		}
		s.wg.Add(1)
		go s.watchFiles()
		return nil

	case ScanModeHybrid:
		// Initial scan + watching + periodic backup
		if err := s.ScanAll(); err != nil {
			log.Printf("[Scanner] Initial scan failed: %v", err)
		}
		if err := s.setupWatchers(); err != nil {
			return err
		}
		s.wg.Add(2)
		go s.watchFiles()
		go s.periodicScan()
		return nil

	default:
		return fmt.Errorf("unknown scan mode: %s", cfg.Mode)
	}
}

// Stop gracefully stops the scanner
func (s *Scanner) Stop() {
	s.mu.Lock()
	if s.stopCh == nil {
		s.mu.Unlock()
		return
	}

	log.Println("[Scanner] Stopping...")
	close(s.stopCh)
	s.stopCh = nil // Mark as stopped
	s.cancel()
	w := s.watcher // capture under lock to avoid TOCTOU with UpdateConfig
	s.mu.Unlock()

	if w != nil {
		w.Close()
	}

	s.wg.Wait()

	// Save processed files database
	if err := s.processedDB.Save(); err != nil {
		log.Printf("[Scanner] Failed to save processed DB: %v", err)
	}

	log.Println("[Scanner] Stopped")
}

// GetConfig returns the current scanner configuration
func (s *Scanner) GetConfig() *ScannerConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// GetProcessedFiles returns all files processed by the scanner
func (s *Scanner) GetProcessedFiles() []ProcessedFile {
	return s.processedDB.GetAll()
}

func (s *Scanner) GetStatus() ScanStatus {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return s.status
}

// CompleteProcessed is the jobs.Manager.OnJobComplete hook, called when a
// scanner-created job reaches ANY terminal state — success or failure alike.
//
// A failed job produced nothing worth recording, and must not leave the file
// durably marked processed: createJobForFile's MarkInFlight call already
// stands in for it while the job runs, precisely so a transient failure here
// can clear that entry instead and let a future scan retry the file (#48).
//
// On success, both the source and the produced output are recorded. Marking
// only the source is enough while output goes to a directory the scanner
// does not watch, but replace-in-place puts the result inside the library —
// where the next scan would treat it as a new source and re-encode it,
// repeatedly.
func (s *Scanner) CompleteProcessed(job *jobs.Job) {
	if job.GetStatus() != jobs.StatusCompleted {
		s.processedDB.ClearInFlight(job.SourcePath)
		return
	}

	s.processedDB.MarkProcessed(ProcessedFile{
		Path:        job.SourcePath,
		JobID:       job.ID,
		JobType:     string(job.Type),
		ProcessedAt: time.Now(),
		InputSize:   job.InputSize,
		OutputSize:  job.OutputSize,
		AISubtitles: job.AISubtitles,
		AIUpscale:   job.Upscale,
		AICleaned:   job.AICleaned,
	})

	if dest := job.DestinationPath; dest != "" && dest != job.SourcePath {
		s.processedDB.MarkProcessed(ProcessedFile{
			Path:        dest,
			JobID:       job.ID,
			JobType:     string(job.Type),
			ProcessedAt: time.Now(),
		})
	}
}

// UpdateConfig updates the scanner configuration and restarts if necessary
func (s *Scanner) UpdateConfig(newCfg *ScannerConfig) error {
	newCfg.Validate()
	s.mu.Lock()
	wasEnabled := s.config.Enabled
	s.config = newCfg
	s.mu.Unlock()

	// Persist changes
	if err := s.saveConfig(); err != nil {
		log.Printf("[Scanner] Failed to persist config: %v", err)
	}

	log.Println("[Scanner] Configuration updated, restarting scanner...")

	// Stop the scanner if it's running
	s.Stop()

	// Re-initialize context and stop channel
	s.mu.Lock()
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	// Re-initialize watcher if mode changed to watch or hybrid
	var newWatcher *fsnotify.Watcher
	if newCfg.Mode == ScanModeWatch || newCfg.Mode == ScanModeHybrid {
		var err error
		newWatcher, err = fsnotify.NewWatcher()
		if err != nil {
			return fmt.Errorf("failed to create file watcher: %w", err)
		}
	}
	s.mu.Lock()
	s.watcher = newWatcher
	s.mu.Unlock()

	// Start if enabled
	if newCfg.Enabled {
		return s.Start()
	} else if wasEnabled {
		log.Println("[Scanner] Scanner disabled")
	}

	return nil
}

// ScanAll scans all configured directories
func (s *Scanner) ScanAll() error {
	if s.isScanning.Swap(true) {
		return fmt.Errorf("scan already in progress")
	}

	s.statusMu.Lock()
	s.status.IsScanning = true
	s.status.FilesScanned = 0
	s.status.CurrentPath = "Initializing..."
	startTime := time.Now()
	s.statusMu.Unlock()

	defer func() {
		duration := time.Since(startTime)
		s.statusMu.Lock()
		s.status.IsScanning = false
		s.status.LastScan = time.Now()
		s.status.Duration = duration.String()
		s.status.CurrentPath = ""
		s.statusMu.Unlock()
		s.isScanning.Store(false)
	}()

	cfg := s.GetConfig()

	if len(cfg.WatchDirectories) == 0 {
		log.Println("[Scanner] WARNING: ScanAll() called but no watch directories are configured — nothing to scan")
		return nil
	}

	log.Println("[Scanner] Starting full scan of all directories")

	var allErrors []error
	filesFound := 0
	jobsCreated := 0

	for _, watchDir := range cfg.WatchDirectories {
		files, err := s.scanDirectory(watchDir)
		if err != nil {
			// If the scanner was stopped mid-walk, exit cleanly without an error.
			if s.ctx.Err() != nil {
				log.Printf("[Scanner] Scan interrupted by stop signal")
				return nil
			}
			allErrors = append(allErrors, err)
			continue
		}

		filesFound += len(files)

		for _, file := range files {
			if s.shouldProcessFile(file, watchDir) {
				if err := s.createJobForFile(file); err != nil {
					log.Printf("[Scanner] Failed to create job for %s: %v", file, err)
				} else {
					jobsCreated++
				}
			}

			s.statusMu.Lock()
			s.status.FilesScanned++
			s.statusMu.Unlock()
		}
	}

	s.statusMu.Lock()
	s.status.LastResult = fmt.Sprintf("Scan complete: %d media files found, %d jobs created", filesFound, jobsCreated)
	if len(allErrors) > 0 {
		s.status.LastError = fmt.Sprintf("Completed with %d errors", len(allErrors))
		s.statusMu.Unlock()
		return fmt.Errorf("scan completed with %d errors", len(allErrors))
	}
	s.status.LastError = "" // clear previous errors
	s.statusMu.Unlock()

	log.Printf("[Scanner] %s", s.status.LastResult)

	return nil
}

// scanDirectory scans a single directory
func (s *Scanner) scanDirectory(watchDir WatchDirectory) ([]string, error) {
	var files []string
	cfg := s.GetConfig()

	// Build combined set of known extensions for fast pre-filtering
	knownExts := make(map[string]bool, len(cfg.ExtractExtensions)+len(cfg.OptimizeExtensions))
	for _, e := range cfg.ExtractExtensions {
		knownExts[strings.ToLower(e)] = true
	}
	for _, e := range cfg.OptimizeExtensions {
		knownExts[strings.ToLower(e)] = true
	}

	walkFunc := func(path string, info os.FileInfo, err error) error {
		// Abort the walk if the scanner has been stopped.
		select {
		case <-s.ctx.Done():
			return fmt.Errorf("scanner stopped")
		default:
		}

		if err != nil {
			return err
		}

		// Handle directories
		if info.IsDir() {
			// Skip hidden directories. This covers MakeMKV extraction temp dirs
			// (.extract_*), whose contents are still being written, and the
			// holding directory used by replace-in-place — which sits inside the
			// media root and holds the originals of already-converted titles. A
			// scan that descended into it would queue every replaced original for
			// conversion again, undoing the work and filling the disk.
			//
			// The root itself is never skipped, so a watch directory that happens
			// to be hidden still works.
			if path != watchDir.Path && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			// If not recursive and not the root directory, skip
			if !watchDir.Recursive && path != watchDir.Path {
				return filepath.SkipDir
			}
			// Update current path only on directory transitions (much cheaper than per-file)
			s.statusMu.Lock()
			s.status.CurrentPath = path
			s.statusMu.Unlock()
			return nil
		}

		// Skip in-progress transcodes. Replace-in-place writes its temp file
		// into the library with a media extension, so without this the scanner
		// would queue a job for a file that is still being written.
		if strings.HasPrefix(info.Name(), jobs.TempFilePrefix) {
			return nil
		}

		// Pre-filter: skip files with unknown extensions immediately
		ext := strings.ToLower(filepath.Ext(path))
		if !knownExts[ext] {
			return nil
		}

		// Check if file matches user-defined include/exclude patterns
		if s.matchesPatterns(path, watchDir) {
			files = append(files, path)
		}

		return nil
	}

	if err := filepath.Walk(watchDir.Path, walkFunc); err != nil {
		return nil, fmt.Errorf("failed to walk directory %s: %w", watchDir.Path, err)
	}

	return files, nil
}

// matchesPatterns checks if a file matches include/exclude patterns
func (s *Scanner) matchesPatterns(path string, watchDir WatchDirectory) bool {
	filename := filepath.Base(path)

	// Check exclude patterns first
	for _, pattern := range watchDir.ExcludePatterns {
		if matched, _ := filepath.Match(pattern, filename); matched {
			return false
		}
	}

	// If no include patterns, accept all (that aren't excluded)
	if len(watchDir.IncludePatterns) == 0 {
		return true
	}

	// Check include patterns
	for _, pattern := range watchDir.IncludePatterns {
		if matched, _ := filepath.Match(pattern, filename); matched {
			return true
		}
	}

	return false
}

// shouldProcessFile determines if a file should be processed
func (s *Scanner) shouldProcessFile(path string, watchDir WatchDirectory) bool {
	// Check if already processed
	if s.processedDB.IsProcessed(path) {
		return false
	}

	// Check file info
	info, err := os.Stat(path)
	if err != nil {
		log.Printf("[Scanner] Failed to stat %s: %v", path, err)
		return false
	}

	// Check minimum file size
	if watchDir.MinFileSizeMB > 0 {
		sizeMB := info.Size() / (1024 * 1024)
		if sizeMB < watchDir.MinFileSizeMB {
			log.Printf("[Scanner] Skipping %s: too small (%d MB < %d MB)", path, sizeMB, watchDir.MinFileSizeMB)
			return false
		}
	}

	// Check minimum file age
	if watchDir.MinFileAgeMinutes > 0 {
		age := time.Since(info.ModTime())
		minAge := time.Duration(watchDir.MinFileAgeMinutes) * time.Minute
		if age < minAge {
			log.Printf("[Scanner] Skipping %s: too new (age: %v < %v)", path, age, minAge)
			return false
		}
	}

	return true
}

// createJobForFile creates an appropriate job for a file
func (s *Scanner) createJobForFile(path string) error {
	cfg := s.GetConfig()

	if !cfg.AutoCreateJobs {
		log.Printf("[Scanner] Found file %s (auto-create disabled)", path)
		return nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	var jobType jobs.JobType

	// Determine job type based on extension
	if s.containsExtension(cfg.ExtractExtensions, ext) {
		if s.jobManager.GetConfig().AutoConvertISO {
			jobType = jobs.JobTypeOptimize
		} else {
			jobType = jobs.JobTypeExtract
		}
	} else if s.containsExtension(cfg.OptimizeExtensions, ext) {
		jobType = jobs.JobTypeOptimize
	} else {
		log.Printf("[Scanner] Skipping %s: unknown extension %s", path, ext)
		return nil
	}

	// Resolution and codec/density filtering are library policy, not scanning
	// policy — both live on the system config so POST /api/jobs applies the
	// same rules (#41).
	sysCfg := s.jobManager.GetConfig()

	// Skip high-resolution files for optimize jobs if configured
	if jobType == jobs.JobTypeOptimize && sysCfg.SkipHighResolution {
		if s.isHighResolution(path, sysCfg.ResolutionHeightThreshold) {
			log.Printf("[Scanner] Skipping %s: resolution >= %dp", path, sysCfg.ResolutionHeightThreshold)
			s.processedDB.MarkProcessed(ProcessedFile{Path: path, JobType: string(jobType)})
			return nil
		}
	}

	// Skip sources that are already an efficient HEVC/AV1 encode — re-encoding
	// them again is generational loss for no size benefit. The job path
	// re-checks this too (media.CheckSourceSupported), but catching it here
	// means it never occupies a queue slot in the first place.
	if jobType == jobs.JobTypeOptimize && s.isAlreadyEfficientEncode(path) {
		log.Printf("[Scanner] Skipping %s: already an efficient encode", path)
		s.processedDB.MarkProcessed(ProcessedFile{Path: path, JobType: string(jobType)})
		return nil
	}

	// Generate output path
	outputPath := s.generateOutputPath(path, jobType)

	// Skip if the expected output already exists — file was processed before tracking began
	if outInfo, err := os.Stat(outputPath); err == nil && outInfo.Size() > 0 {
		log.Printf("[Scanner] Skipping %s: output already exists at %s", path, outputPath)
		s.processedDB.MarkProcessed(ProcessedFile{Path: path, JobType: string(jobType)})
		return nil
	}

	// Inherit delete/verify settings from system config so scanner jobs
	// behave identically to manually-created jobs.

	// Create job
	job := &jobs.Job{
		ID:              util.GenerateID(),
		Type:            jobType,
		SourcePath:      path,
		DestinationPath: outputPath,
		Status:          jobs.StatusPending,
		Priority:        cfg.DefaultPriority,
		CreateSubtitles: cfg.AutoCreateSubtitles,
		Upscale:         cfg.AutoUpscale,
		Resolution:      cfg.AutoResolution,
		DeleteSource:    sysCfg.DeleteSource,
		VerifyOutput:    sysCfg.VerifyOutput,
		CreatedAt:       time.Now(),
	}

	s.jobManager.AddJob(job)

	// In-flight, not a durable entry (#48): a transient failure must not
	// permanently exclude this file from future scans. CompleteProcessed
	// promotes this to a durable entry on success, or clears it on failure.
	s.processedDB.MarkInFlight(ProcessedFile{
		Path:    path,
		JobID:   job.ID,
		JobType: string(jobType),
	})

	log.Printf("[Scanner] Created %s job %s for %s", jobType, job.ID, path)

	return nil
}

// generateOutputPath creates an output path for a file
func (s *Scanner) generateOutputPath(inputPath string, jobType jobs.JobType) string {
	filename := filepath.Base(inputPath)
	ext := filepath.Ext(filename)
	nameWithoutExt := strings.TrimSuffix(filename, ext)

	outputDir := s.GetConfig().OutputDirectory
	if outputDir == "" {
		outputDir = filepath.Dir(inputPath)
	}

	switch jobType {
	case jobs.JobTypeExtract:
		// Place the extracted MKV alongside the source file, named after it
		return filepath.Join(outputDir, nameWithoutExt+".mkv")
	case jobs.JobTypeOptimize:
		// For optimization, add suffix
		return filepath.Join(outputDir, nameWithoutExt+"_optimized.mkv")
	default:
		return filepath.Join(outputDir, filename)
	}
}

// containsExtension checks if an extension is in the list
func (s *Scanner) containsExtension(extensions []string, ext string) bool {
	for _, e := range extensions {
		if strings.EqualFold(e, ext) {
			return true
		}
	}
	return false
}

// setupWatchers configures file system watchers for all directories
func (s *Scanner) setupWatchers() error {
	for _, watchDir := range s.GetConfig().WatchDirectories {
		if err := s.addWatcher(watchDir); err != nil {
			return err
		}
	}
	return nil
}

// addWatcher adds a watcher for a directory
func (s *Scanner) addWatcher(watchDir WatchDirectory) error {
	if watchDir.Recursive {
		// Add watchers for all subdirectories
		return filepath.Walk(watchDir.Path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if err := s.watcher.Add(path); err != nil {
					return err
				}
				log.Printf("[Scanner] Watching directory: %s", path)
			}
			return nil
		})
	} else {
		// Just watch the root directory
		if err := s.watcher.Add(watchDir.Path); err != nil {
			return err
		}
		log.Printf("[Scanner] Watching directory: %s", watchDir.Path)
	}
	return nil
}

// watchFiles monitors file system events
func (s *Scanner) watchFiles() {
	defer s.wg.Done()

	// Capture the watcher and stop-channel references once, rather than
	// re-reading s.watcher/s.stopCh on every select iteration. Stop() closes
	// stopCh and then sets the field to nil under s.mu (#43); re-reading the
	// field here could observe nil after the close and permanently disable
	// this case (a nil channel is never ready), rather than the intended
	// one-time exit. Closing a channel is visible on this already-captured
	// value regardless of what the field is later set to.
	s.mu.RLock()
	w := s.watcher
	stopCh := s.stopCh
	s.mu.RUnlock()

	if w == nil {
		log.Println("[Scanner] File watcher started but no watcher configured")
		return
	}

	log.Println("[Scanner] File watcher started")

	for {
		select {
		case <-stopCh:
			return

		case event, ok := <-w.Events:
			if !ok {
				return
			}

			if event.Op&fsnotify.Create == fsnotify.Create {
				s.handleNewFile(event.Name)
			}

		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("[Scanner] Watcher error: %v", err)
		}
	}
}

// handleNewFile processes a newly created file
func (s *Scanner) handleNewFile(path string) {
	cfg := s.GetConfig()

	// Pre-filter by known extension before doing expensive directory/pattern checks
	ext := strings.ToLower(filepath.Ext(path))
	knownExt := s.containsExtension(cfg.ExtractExtensions, ext) ||
		s.containsExtension(cfg.OptimizeExtensions, ext)
	if !knownExt {
		return
	}

	// Find matching watch directory
	for _, watchDir := range cfg.WatchDirectories {
		if s.isInDirectory(path, watchDir.Path) && s.matchesPatterns(path, watchDir) {
			s.pendingMu.Lock()
			if _, exists := s.pending[path]; exists {
				s.pendingMu.Unlock()
				return
			}
			s.pending[path] = time.Now()
			s.pendingMu.Unlock()

			// Wait for file age requirement if configured
			if watchDir.MinFileAgeMinutes > 0 {
				// Tracked in wg like every other long-lived scanner goroutine,
				// so Stop() actually waits for it instead of returning while
				// it's still in flight (#43).
				s.wg.Add(1)
				go s.delayedProcess(path, watchDir)
			} else {
				if s.shouldProcessFile(path, watchDir) {
					s.createJobForFile(path)
				}
				s.pendingMu.Lock()
				delete(s.pending, path)
				s.pendingMu.Unlock()
			}
			break
		}
	}
}

// delayedProcess waits before processing a file
func (s *Scanner) delayedProcess(path string, watchDir WatchDirectory) {
	defer s.wg.Done()

	// Captured once, not read as s.stopCh per select entry — see the same
	// note in watchFiles (#43).
	s.mu.RLock()
	stopCh := s.stopCh
	s.mu.RUnlock()

	delay := time.Duration(watchDir.MinFileAgeMinutes) * time.Minute
	log.Printf("[Scanner] Delaying processing of %s for %v", path, delay)

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		if s.shouldProcessFile(path, watchDir) {
			s.createJobForFile(path)
		}
	case <-stopCh:
		// Exit
	}

	s.pendingMu.Lock()
	delete(s.pending, path)
	s.pendingMu.Unlock()
}

// periodicScan runs periodic scans
func (s *Scanner) periodicScan() {
	defer s.wg.Done()

	// Both captured once, not re-read per select entry — see the note in
	// watchFiles. Without this, a stopCh read as nil after Stop() races the
	// close leaves only the ticker case live, so Stop()'s s.wg.Wait() would
	// hang forever waiting on a goroutine that can no longer see stop (#43).
	s.mu.RLock()
	stopCh := s.stopCh
	interval := time.Duration(s.config.ScanIntervalSec) * time.Second
	s.mu.RUnlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("[Scanner] Periodic scan started (interval: %v)", interval)

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			log.Println("[Scanner] Running periodic scan...")
			if err := s.ScanAll(); err != nil {
				log.Printf("[Scanner] Periodic scan error: %v", err)
			}
		}
	}
}

// DiscoveredFile represents a file found during a discovery scan
type DiscoveredFile struct {
	Path                  string  `json:"path"`
	Name                  string  `json:"name"`
	SizeBytes             int64   `json:"sizeBytes"`
	Extension             string  `json:"extension"`
	JobType               string  `json:"jobType"` // "optimize" or "extract"
	EstimatedOutputBytes  int64   `json:"estimatedOutputBytes"`
	EstimatedSavingsBytes int64   `json:"estimatedSavingsBytes"`
	EstimatedSavingsPct   float64 `json:"estimatedSavingsPct"`
	VideoWidth            int     `json:"videoWidth"`
	VideoHeight           int     `json:"videoHeight"`
}

// isHighResolution returns true if the video at path has a height >= threshold.
// On any error (e.g. ffprobe unavailable) it returns false so the file is included.
func (s *Scanner) isHighResolution(path string, threshold int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, height, _, _, err := s.jobManager.GetVideoResolution(ctx, path)
	if err != nil {
		return false
	}
	return height >= threshold
}

// isAlreadyEfficientEncode returns true if the video at path is already an
// HEVC/AV1 encode dense enough that re-encoding it again would be
// generational loss for no size benefit (#39). On any error it returns false
// so the file is included rather than silently dropped.
func (s *Scanner) isAlreadyEfficientEncode(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _, codec, bitsPerPixel, err := s.jobManager.GetVideoResolution(ctx, path)
	if err != nil {
		return false
	}
	return media.IsAlreadyEfficient(codec, bitsPerPixel, s.jobManager.GetConfig().DensityFloor)
}

// Discover scans all watch directories and returns files that would be processed,
// without creating any jobs.
func (s *Scanner) Discover() ([]DiscoveredFile, error) {
	cfg := s.GetConfig()

	var result []DiscoveredFile

	for _, watchDir := range cfg.WatchDirectories {
		files, err := s.scanDirectory(watchDir)
		if err != nil {
			log.Printf("[Scanner] Discover: error scanning %s: %v", watchDir.Path, err)
			continue
		}

		for _, path := range files {
			if !s.shouldProcessFile(path, watchDir) {
				continue
			}

			info, err := os.Stat(path)
			if err != nil {
				continue
			}

			ext := strings.ToLower(filepath.Ext(path))
			var jobType string
			if s.containsExtension(cfg.ExtractExtensions, ext) {
				jobType = "extract"
			} else if s.containsExtension(cfg.OptimizeExtensions, ext) {
				jobType = "optimize"
			} else {
				continue
			}

			// Skip if the expected output file already exists (e.g. processed before
			// tracking began, or processedDB was cleared).
			outputPath := s.generateOutputPath(path, jobs.JobType(jobType))
			if outInfo, err := os.Stat(outputPath); err == nil && outInfo.Size() > 0 {
				s.processedDB.MarkProcessed(ProcessedFile{Path: path, JobType: jobType})
				continue
			}

			// Probe video resolution for optimize jobs and apply the high-res and
			// already-efficient filters — one probe covers both (#41, #39).
			var videoWidth, videoHeight int
			if jobType == "optimize" {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				w, h, codec, bpp, _ := s.jobManager.GetVideoResolution(ctx, path)
				cancel()
				videoWidth, videoHeight = w, h
				sysCfg := s.jobManager.GetConfig()
				if sysCfg.SkipHighResolution && h >= sysCfg.ResolutionHeightThreshold {
					continue
				}
				if media.IsAlreadyEfficient(codec, bpp, sysCfg.DensityFloor) {
					continue
				}
			}

			estimatedOutput, pct := estimateSavings(ext, info.Size())

			result = append(result, DiscoveredFile{
				Path:                  path,
				Name:                  filepath.Base(path),
				SizeBytes:             info.Size(),
				Extension:             ext,
				JobType:               jobType,
				EstimatedOutputBytes:  estimatedOutput,
				EstimatedSavingsBytes: info.Size() - estimatedOutput,
				EstimatedSavingsPct:   pct,
				VideoWidth:            videoWidth,
				VideoHeight:           videoHeight,
			})
		}
	}

	return result, nil
}

// QueueFile creates a job for a specific file path, bypassing AutoCreateJobs.
func (s *Scanner) QueueFile(path string) error {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	// Security: unlike a file createJobForFile discovers itself (already
	// confined to a configured watch directory), this path arrives raw from
	// POST /api/scanner/queue with no validation at all (#37).
	validPath, err := security.ValidatePath(path, s.jobManager.GetConfig().SourceDir)
	if err != nil {
		return err
	}
	path = validPath

	ext := strings.ToLower(filepath.Ext(path))
	var jobType jobs.JobType

	if s.containsExtension(cfg.ExtractExtensions, ext) {
		jobType = jobs.JobTypeExtract
	} else if s.containsExtension(cfg.OptimizeExtensions, ext) {
		jobType = jobs.JobTypeOptimize
	} else {
		return fmt.Errorf("unsupported extension: %s", ext)
	}

	outputPath := s.generateOutputPath(path, jobType)

	job := &jobs.Job{
		ID:              util.GenerateID(),
		Type:            jobType,
		SourcePath:      path,
		DestinationPath: outputPath,
		Status:          jobs.StatusPending,
		Priority:        cfg.DefaultPriority,
		CreateSubtitles: cfg.AutoCreateSubtitles,
		Upscale:         cfg.AutoUpscale,
		Resolution:      cfg.AutoResolution,
		CreatedAt:       time.Now(),
	}

	s.jobManager.AddJob(job)

	// In-flight, not durable — see the same note in createJobForFile (#48).
	s.processedDB.MarkInFlight(ProcessedFile{
		Path:    path,
		JobID:   job.ID,
		JobType: string(jobType),
	})

	log.Printf("[Scanner] Queued %s job %s for %s", jobType, job.ID, path)
	return nil
}

// estimateSavings estimates the output file size and savings % based on file extension.
func estimateSavings(ext string, sizeBytes int64) (estimatedOutput int64, pct float64) {
	switch ext {
	case ".avi", ".wmv", ".mpg", ".mpeg", ".flv":
		pct = 55
	case ".mkv", ".mp4", ".m4v", ".mov":
		pct = 40
	case ".webm":
		pct = 15
	case ".iso", ".img", ".mdf":
		return sizeBytes, 0 // extraction operation, not compression
	default:
		pct = 35
	}
	estimatedOutput = int64(float64(sizeBytes) * (1 - pct/100))
	return estimatedOutput, pct
}

// isInDirectory checks if a path is within a directory
func (s *Scanner) isInDirectory(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// NewProcessedDB creates a new processed file database
func NewProcessedDB(filePath string) (*ProcessedDB, error) {
	db := &ProcessedDB{
		filePath:  filePath,
		processed: make(map[string]ProcessedFile),
	}

	// Load existing database if it exists
	if err := db.Load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return db, nil
}

// Load reads the processed files database from disk
func (db *ProcessedDB) Load() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	data, err := os.ReadFile(db.filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &db.processed)
}

// Save writes the processed files database to disk
func (db *ProcessedDB) Save() error {
	db.mu.RLock()
	data, err := json.Marshal(db.processed)
	db.mu.RUnlock()
	if err != nil {
		return err
	}

	tmp := db.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, db.filePath)
}

// IsProcessed checks if a file has been processed
func (db *ProcessedDB) IsProcessed(path string) bool {
	db.mu.RLock()
	defer db.mu.RUnlock()

	_, exists := db.processed[path]
	return exists
}

// GetAll returns all processed files
func (db *ProcessedDB) GetAll() []ProcessedFile {
	db.mu.RLock()
	defer db.mu.RUnlock()

	files := make([]ProcessedFile, 0, len(db.processed))
	for _, f := range db.processed {
		files = append(files, f)
	}
	return files
}

// MarkProcessed marks a file as processed and saves the database
func (db *ProcessedDB) MarkProcessed(f ProcessedFile) {
	// Calculate file hash if not provided (before locking)
	if f.Hash == "" {
		hash, _ := calculateFileHash(f.Path)
		f.Hash = hash
	}

	if f.ProcessedAt.IsZero() {
		f.ProcessedAt = time.Now()
	}

	db.mu.Lock()
	db.processed[f.Path] = f
	db.mu.Unlock()

	db.Save()
}

// MarkInFlight records that a job now exists for f.Path, without the durable
// hash/timestamp bookkeeping MarkProcessed does — a job that's merely been
// created hasn't produced anything to hash yet, and completion (via
// MarkProcessed, called from CompleteProcessed) will overwrite this entry
// with the real one anyway. Existing only to make IsProcessed report true
// while the job is in flight, so a second scan doesn't queue it again (#48).
func (db *ProcessedDB) MarkInFlight(f ProcessedFile) {
	f.InFlight = true
	db.mu.Lock()
	db.processed[f.Path] = f
	db.mu.Unlock()
	db.Save()
}

// ClearInFlight removes an in-flight entry for path, making the file visible
// to future scans again. Called when a job fails, so a transient failure
// doesn't permanently exclude the file the way a durable MarkProcessed entry
// would (#48). A no-op if the entry is missing or has since been promoted to
// a durable one — a job's completion callback runs exactly once, so which of
// those two happens is determined by the job's own final status, not by a
// race between them.
func (db *ProcessedDB) ClearInFlight(path string) {
	// Save takes db.mu itself (see the note on MarkProcessed), so it must not
	// be called while the write lock from this method is still held.
	db.mu.Lock()
	f, exists := db.processed[path]
	cleared := exists && f.InFlight
	if cleared {
		delete(db.processed, path)
	}
	db.mu.Unlock()

	if cleared {
		db.Save()
	}
}

// calculateFileHash computes SHA256 hash of first 1MB of file
func calculateFileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	// Only hash first 1MB for performance
	if _, err := io.CopyN(hash, file, 1024*1024); err != nil && err != io.EOF {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// Persistence

func (s *Scanner) saveConfig() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.config, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}

	tmp := s.configFilePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.configFilePath)
}

func (s *Scanner) loadConfig() error {
	data, err := os.ReadFile(s.configFilePath)
	if err != nil {
		return err
	}

	var newCfg ScannerConfig
	if err := json.Unmarshal(data, &newCfg); err != nil {
		return err
	}

	newCfg.Validate()

	s.mu.Lock()
	s.config = &newCfg
	s.mu.Unlock()

	return nil
}
