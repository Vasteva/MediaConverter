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
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rwurtz/vastiva/internal/jobs"
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
	ProcessedFilePath   string           `json:"processedFilePath"` // Track processed files

	// Job creation settings
	DefaultPriority int    `json:"defaultPriority"`
	OutputDirectory string `json:"outputDirectory"`

	// File type handling
	ExtractExtensions  []string `json:"extractExtensions"`  // e.g., [".iso"]
	OptimizeExtensions []string `json:"optimizeExtensions"` // e.g., [".mkv", ".mp4", ".avi"]
}

// Scanner manages automatic file discovery and job creation
type Scanner struct {
	config      *ScannerConfig
	jobManager  *jobs.Manager
	watcher     *fsnotify.Watcher
	processedDB *ProcessedDB
	mu          sync.RWMutex
	stopCh      chan struct{}
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
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
}

// NewScanner creates a new file scanner
func NewScanner(config *ScannerConfig, jobManager *jobs.Manager) (*Scanner, error) {
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
		config:      config,
		jobManager:  jobManager,
		processedDB: processedDB,
		stopCh:      make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Initialize file watcher if needed
	if config.Mode == ScanModeWatch || config.Mode == ScanModeHybrid {
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
	if !s.config.Enabled {
		log.Println("[Scanner] Disabled, not starting")
		return nil
	}

	log.Printf("[Scanner] Starting in %s mode", s.config.Mode)

	switch s.config.Mode {
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
		return fmt.Errorf("unknown scan mode: %s", s.config.Mode)
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
	s.mu.Unlock()

	if s.watcher != nil {
		s.watcher.Close()
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

// UpdateConfig updates the scanner configuration and restarts if necessary
func (s *Scanner) UpdateConfig(newCfg *ScannerConfig) error {
	s.mu.Lock()
	wasEnabled := s.config.Enabled
	s.config = newCfg
	s.mu.Unlock()

	log.Println("[Scanner] Configuration updated, restarting scanner...")

	// Stop the scanner if it's running
	s.Stop()

	// Re-initialize context and stop channel
	s.mu.Lock()
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	// Re-initialize watcher if mode changed to watch or hybrid
	if newCfg.Mode == ScanModeWatch || newCfg.Mode == ScanModeHybrid {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			return fmt.Errorf("failed to create file watcher: %w", err)
		}
		s.watcher = watcher
	} else {
		s.watcher = nil
	}

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
	log.Println("[Scanner] Starting full scan of all directories")

	var allErrors []error
	filesFound := 0
	jobsCreated := 0

	for _, watchDir := range s.config.WatchDirectories {
		files, err := s.scanDirectory(watchDir)
		if err != nil {
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
		}
	}

	log.Printf("[Scanner] Scan complete: %d files found, %d jobs created", filesFound, jobsCreated)

	if len(allErrors) > 0 {
		return fmt.Errorf("scan completed with %d errors", len(allErrors))
	}

	return nil
}

// scanDirectory scans a single directory
func (s *Scanner) scanDirectory(watchDir WatchDirectory) ([]string, error) {
	var files []string

	walkFunc := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			// If not recursive and not the root directory, skip
			if !watchDir.Recursive && path != watchDir.Path {
				return filepath.SkipDir
			}
			return nil
		}

		// Check if file matches patterns
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
	if !s.config.AutoCreateJobs {
		log.Printf("[Scanner] Found file %s (auto-create disabled)", path)
		return nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	var jobType jobs.JobType

	// Determine job type based on extension
	if s.containsExtension(s.config.ExtractExtensions, ext) {
		jobType = jobs.JobTypeExtract
	} else if s.containsExtension(s.config.OptimizeExtensions, ext) {
		jobType = jobs.JobTypeOptimize
	} else {
		log.Printf("[Scanner] Skipping %s: unknown extension %s", path, ext)
		return nil
	}

	// Generate output path
	outputPath := s.generateOutputPath(path, jobType)

	// Create job
	job := &jobs.Job{
		ID:              generateJobID(),
		Type:            jobType,
		SourcePath:      path,
		DestinationPath: outputPath,
		Status:          jobs.StatusPending,
		Priority:        s.config.DefaultPriority,
		CreateSubtitles: s.config.AutoCreateSubtitles,
		CreatedAt:       time.Now(),
	}

	s.jobManager.AddJob(job)

	// Mark as processed
	s.processedDB.MarkProcessed(path, job.ID, string(jobType))

	log.Printf("[Scanner] Created %s job %s for %s", jobType, job.ID, path)

	return nil
}

// generateOutputPath creates an output path for a file
func (s *Scanner) generateOutputPath(inputPath string, jobType jobs.JobType) string {
	filename := filepath.Base(inputPath)
	ext := filepath.Ext(filename)
	nameWithoutExt := strings.TrimSuffix(filename, ext)

	outputDir := s.config.OutputDirectory
	if outputDir == "" {
		outputDir = filepath.Dir(inputPath)
	}

	switch jobType {
	case jobs.JobTypeExtract:
		// For extraction, create a subdirectory
		return filepath.Join(outputDir, nameWithoutExt)
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
	for _, watchDir := range s.config.WatchDirectories {
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

	log.Println("[Scanner] File watcher started")

	for {
		select {
		case <-s.stopCh:
			return

		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}

			if event.Op&fsnotify.Create == fsnotify.Create {
				s.handleNewFile(event.Name)
			}

		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[Scanner] Watcher error: %v", err)
		}
	}
}

// handleNewFile processes a newly created file
func (s *Scanner) handleNewFile(path string) {
	// Find matching watch directory
	for _, watchDir := range s.config.WatchDirectories {
		if s.isInDirectory(path, watchDir.Path) && s.matchesPatterns(path, watchDir) {
			// Wait for file age requirement if configured
			if watchDir.MinFileAgeMinutes > 0 {
				go s.delayedProcess(path, watchDir)
			} else {
				if s.shouldProcessFile(path, watchDir) {
					s.createJobForFile(path)
				}
			}
			break
		}
	}
}

// delayedProcess waits before processing a file
func (s *Scanner) delayedProcess(path string, watchDir WatchDirectory) {
	delay := time.Duration(watchDir.MinFileAgeMinutes) * time.Minute
	log.Printf("[Scanner] Delaying processing of %s for %v", path, delay)

	select {
	case <-time.After(delay):
		if s.shouldProcessFile(path, watchDir) {
			s.createJobForFile(path)
		}
	case <-s.stopCh:
		return
	}
}

// periodicScan runs periodic scans
func (s *Scanner) periodicScan() {
	defer s.wg.Done()

	interval := time.Duration(s.config.ScanIntervalSec) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("[Scanner] Periodic scan started (interval: %v)", interval)

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			log.Println("[Scanner] Running periodic scan...")
			if err := s.ScanAll(); err != nil {
				log.Printf("[Scanner] Periodic scan error: %v", err)
			}
		}
	}
}

// isInDirectory checks if a path is within a directory
func (s *Scanner) isInDirectory(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// generateJobID creates a unique job ID
func generateJobID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(6)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
	}
	return string(b)
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
	defer db.mu.RUnlock()

	data, err := json.Marshal(db.processed)
	if err != nil {
		return err
	}

	return os.WriteFile(db.filePath, data, 0644)
}

// IsProcessed checks if a file has been processed
func (db *ProcessedDB) IsProcessed(path string) bool {
	db.mu.RLock()
	defer db.mu.RUnlock()

	_, exists := db.processed[path]
	return exists
}

// MarkProcessed marks a file as processed
func (db *ProcessedDB) MarkProcessed(path, jobID, jobType string) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Calculate file hash for verification
	hash, _ := calculateFileHash(path)

	db.processed[path] = ProcessedFile{
		Path:        path,
		Hash:        hash,
		ProcessedAt: time.Now(),
		JobID:       jobID,
		JobType:     jobType,
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
