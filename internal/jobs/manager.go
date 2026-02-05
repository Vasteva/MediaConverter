package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Vasteva/MediaConverter/internal/ai"
	"github.com/Vasteva/MediaConverter/internal/ai/meta"
	"github.com/Vasteva/MediaConverter/internal/ai/whisper"
	"github.com/Vasteva/MediaConverter/internal/config"
	"github.com/Vasteva/MediaConverter/internal/media"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
)

type JobType string

const (
	JobTypeExtract  JobType = "extract"
	JobTypeOptimize JobType = "optimize"
	JobTypeTest     JobType = "test"
)

type Job struct {
	ID              string    `json:"id"`
	Type            JobType   `json:"type"`
	SourcePath      string    `json:"sourcePath"`
	DestinationPath string    `json:"destinationPath"`
	Status          Status    `json:"status"`
	Progress        int       `json:"progress"`
	ETA             string    `json:"eta"`
	FPS             float64   `json:"fps"`
	Priority        int       `json:"priority"`
	CreatedAt       time.Time `json:"createdAt"`
	StartedAt       time.Time `json:"startedAt,omitempty"`
	CompletedAt     time.Time `json:"completedAt,omitempty"`
	Error           string    `json:"error,omitempty"`
	CreateSubtitles bool      `json:"createSubtitles"` // Premium feature
	Upscale         bool      `json:"upscale"`         // Premium feature
	Resolution      string    `json:"resolution"`      // Premium feature
	InputSize       int64     `json:"inputSize"`
	OutputSize      int64     `json:"outputSize"`
	AICleaned       bool      `json:"aiCleaned"`
	AISubtitles     bool      `json:"aiSubtitles"`

	// Internal
	ctx    context.Context
	cancel context.CancelFunc
	cmd    *exec.Cmd
}

type Manager struct {
	jobs          map[string]*Job
	queue         chan *Job
	maxConcurrent int
	mu            sync.RWMutex
	wg            sync.WaitGroup
	stopCh        chan struct{}
	config        *config.Config
	ffmpeg        *media.FFmpegWrapper
	makemkv       *media.MakeMKVWrapper
	ai            ai.Provider
	OnJobComplete func(*Job)
	jobsFilePath  string
}

func NewManager(cfg *config.Config, aiProvider ai.Provider, jobsFilePath string) (*Manager, error) {
	ffmpeg, err := media.NewFFmpegWrapper()
	if err != nil {
		log.Printf("Warning: FFmpeg not available: %v", err)
	}

	makemkv, err := media.NewMakeMKVWrapper()
	if err != nil {
		log.Printf("Warning: MakeMKV not available: %v", err)
	}

	m := &Manager{
		jobs:          make(map[string]*Job),
		queue:         make(chan *Job, 1000),
		maxConcurrent: cfg.MaxConcurrentJobs,
		stopCh:        make(chan struct{}),
		config:        cfg,
		ffmpeg:        ffmpeg,
		makemkv:       makemkv,
		ai:            aiProvider,
		jobsFilePath:  jobsFilePath,
	}

	// Load existing jobs from disk
	if err := m.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: Could not load existing jobs: %v", err)
	}

	return m, nil
}

func (m *Manager) Start() {
	log.Printf("Job manager started with %d workers", m.maxConcurrent)
	for i := 0; i < m.maxConcurrent; i++ {
		m.wg.Add(1)
		go m.worker(i)
	}
}

func (m *Manager) Stop() {
	close(m.stopCh)
	m.wg.Wait()
	log.Println("Job manager stopped")
}

// GetAI returns the current AI provider
func (m *Manager) GetAI() ai.Provider {
	return m.ai
}

func (m *Manager) worker(id int) {
	defer m.wg.Done()
	for {
		select {
		case <-m.stopCh:
			return
		case job := <-m.queue:
			m.processJob(job)
		}
	}
}

func (m *Manager) AddJob(job *Job) {
	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()
	m.Save() // Persist to disk
	m.queue <- job
}

func (m *Manager) GetJob(id string) *Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[id]
}

func (m *Manager) GetAllJobs() []*Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		result = append(result, job)
	}
	return result
}

func (m *Manager) CancelJob(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job, ok := m.jobs[id]; ok && job.cancel != nil {
		job.cancel()
		job.Status = StatusCancelled
		return true
	}
	return false
}

func (m *Manager) UpdateAIProvider(provider ai.Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ai = provider
	log.Printf("Job manager AI provider updated")
}

func (m *Manager) processJob(job *Job) {
	job.ctx, job.cancel = context.WithCancel(context.Background())
	job.Status = StatusProcessing
	job.StartedAt = time.Now()

	// Track input size
	if info, err := os.Stat(job.SourcePath); err == nil {
		job.InputSize = info.Size()
	}

	// Premium Feature: AI Metadata Cleanup
	if m.config.IsPremium && m.ai != nil && job.Type == JobTypeOptimize {
		cleaner := meta.NewCleaner(m.ai)
		filename := filepath.Base(job.SourcePath)
		if cleanTitle, err := cleaner.CleanFilename(job.ctx, filename); err == nil {
			log.Printf("[Premium] AI cleaned filename: %s -> %s", filename, cleanTitle)
			job.AICleaned = true
			// Adjust destination path if needed
			ext := filepath.Ext(job.DestinationPath)
			dir := filepath.Dir(job.DestinationPath)
			job.DestinationPath = filepath.Join(dir, cleanTitle+ext)
		}
	}

	var err error
	switch job.Type {
	case JobTypeExtract:
		err = m.runExtraction(job)
	case JobTypeOptimize:
		ext := strings.ToLower(filepath.Ext(job.SourcePath))
		if ext == ".iso" || ext == ".img" || ext == ".mdf" {
			log.Printf("[Job %s] Detected disc image input. Starting auto-extraction...", job.ID)

			// Auto-extract first
			// Create a temporary extraction folder
			extractDir := filepath.Join(filepath.Dir(job.DestinationPath), "extract_"+job.ID)
			if err := os.MkdirAll(extractDir, 0755); err != nil {
				job.Status = StatusFailed
				job.Error = fmt.Sprintf("failed to create extract dir: %v", err)
				break
			}

			// Need a temporary job struct to reuse runExtraction logic or call makemkv directly
			// Calling runExtraction on THIS job would be confusing status-wise, but efficient code-wise.
			// Let's call m.makemkv.ExtractWithProgress directly.

			if m.makemkv == nil {
				job.Status = StatusFailed
				job.Error = "makemkv not installed"
				break
			}

			// Scan disc
			info, err := m.makemkv.ScanDisc(job.ctx, job.SourcePath)
			if err != nil {
				job.Status = StatusFailed
				job.Error = fmt.Sprintf("scan failed: %v", err)
				break
			}
			if len(info.Titles) == 0 {
				job.Status = StatusFailed
				job.Error = "no titles found"
				break
			}

			mainTitleIdx := info.FindLargestTitle()
			log.Printf("[Job %s] Identified main feature: Title %d", job.ID, mainTitleIdx)

			opts := media.ExtractOptions{
				SourcePath: job.SourcePath,
				OutputDir:  extractDir,
				TitleIndex: mainTitleIdx,
			}

			extractErr := m.makemkv.ExtractWithProgress(job.ctx, opts, func(p media.TranscodeProgress) {
				// We can reflect extraction progress in the main job, maybe scaled 0-50%?
				// For now let's just show raw percentage but user might see it reset.
				// Better: show it as part of status or log?
				// The UI just shows "Processing" and a % bar.
				// Let's accept 0-100 for extraction, then 0-100 for optimization.
				job.Progress = p.Percentage
				m.Save() // Persist updates
			})

			if extractErr != nil {
				job.Status = StatusFailed
				job.Error = fmt.Sprintf("extraction failed: %v", extractErr)
				break
			}

			// Find the extracted file
			files, _ := filepath.Glob(filepath.Join(extractDir, "*.mkv"))
			if len(files) == 0 {
				job.Status = StatusFailed
				job.Error = "extraction finished but no output file found"
				break
			}

			// Update source path to the new extracted file for the optimization step
			originalSource := job.SourcePath
			job.SourcePath = files[0]
			log.Printf("[Job %s] Extraction complete. File: %s", job.ID, job.SourcePath)

			// Now proceed to standard optimization
			err = m.runOptimization(job)

			// Cleanup extraction artifacts if successful (optional, but good for "consolidated")
			// But maybe user wants to keep the rip? The user said "consolidate if extraction required then extract it".
			// Usually implies "Extract -> Encode -> Delete Rip".
			// Let's delete the intermediate rip to save space.
			if err == nil {
				os.RemoveAll(extractDir)
				job.SourcePath = originalSource // Restore original source path for record keeping?
			}
		} else {
			err = m.runOptimization(job)
		}
	case JobTypeTest:
		err = m.runTest(job)
	}

	if err != nil {
		job.Status = StatusFailed
		job.Error = err.Error()
	} else {
		job.Status = StatusCompleted
		job.Progress = 100

		// Track output size
		if info, err := os.Stat(job.DestinationPath); err == nil {
			job.OutputSize = info.Size()
		}
	}
	job.CompletedAt = time.Now()

	// Persist job state to disk
	m.Save()

	if m.OnJobComplete != nil {
		m.OnJobComplete(job)
	}
}

func (m *Manager) runExtraction(job *Job) error {
	if m.makemkv == nil {
		return fmt.Errorf("makemkv wrapper not initialized")
	}

	log.Printf("[Job %s] Starting disc extraction for %s", job.ID, job.SourcePath)

	// 1. Scan disc to find titles
	info, err := m.makemkv.ScanDisc(job.ctx, job.SourcePath)
	if err != nil {
		return fmt.Errorf("failed to scan disc: %v", err)
	}

	if len(info.Titles) == 0 {
		return fmt.Errorf("no titles found on disc")
	}

	// 2. Find the main feature (largest title)
	mainTitleIdx := info.FindLargestTitle()
	log.Printf("[Job %s] Detected main feature: Title %d", job.ID, mainTitleIdx)

	// 3. Ensure destination directory exists
	if err := os.MkdirAll(job.DestinationPath, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %v", err)
	}

	// 4. Run extraction
	opts := media.ExtractOptions{
		SourcePath: job.SourcePath,
		OutputDir:  job.DestinationPath,
		TitleIndex: mainTitleIdx,
	}

	err = m.makemkv.ExtractWithProgress(job.ctx, opts, func(p media.TranscodeProgress) {
		job.Progress = p.Percentage
	})
	if err != nil {
		return fmt.Errorf("extraction failed: %v", err)
	}

	log.Printf("[Job %s] Extraction complete", job.ID)
	return nil
}

func (m *Manager) runOptimization(job *Job) error {
	if m.ffmpeg == nil {
		return fmt.Errorf("ffmpeg wrapper not initialized")
	}

	log.Printf("[Job %s] Starting optimization: %s", job.ID, job.SourcePath)

	// 1. Get media info for duration
	info, err := m.ffmpeg.GetMediaInfo(job.ctx, job.SourcePath)
	if err != nil {
		log.Printf("[Job %s] Error getting media info: %v", job.ID, err)
		return fmt.Errorf("failed to get media info: %w", err)
	}

	log.Printf("[Job %s] Media duration: %.2f seconds", job.ID, info.Duration)

	// 2. Premium Feature: AI Adaptive Encoding
	crf := m.config.CRF
	if m.config.IsPremium && m.ai != nil {
		cleaner := meta.NewCleaner(m.ai)
		log.Printf("[Premium] AI analyzing media for optimal encoding settings...")
		if suggestedCRF, err := cleaner.AnalyzeEncoding(job.ctx, info.RawJSON); err == nil {
			log.Printf("[Premium] AI suggested CRF: %d (System Default: %d)", suggestedCRF, crf)
			crf = suggestedCRF
		} else {
			log.Printf("[Premium] AI analysis failed: %v", err)
		}
	}

	opts := media.TranscodeOptions{
		InputPath:     job.SourcePath,
		OutputPath:    job.DestinationPath,
		GPUVendor:     media.GPUVendor(m.config.GPUVendor),
		Preset:        media.QualityPreset(m.config.QualityPreset),
		CRF:           crf,
		TotalDuration: info.Duration,
		Upscale:       job.Upscale,
		Resolution:    job.Resolution,
	}

	log.Printf("[Job %s] Starting ffmpeg transcoding to: %s", job.ID, opts.OutputPath)

	err = m.ffmpeg.TranscodeWithProgress(job.ctx, opts, func(p media.TranscodeProgress) {
		job.Progress = p.Percentage
		job.FPS = p.FPS
		job.ETA = p.ETA
	})
	if err != nil {
		log.Printf("[Job %s] FFmpeg failed: %v", job.ID, err)
		return err
	}

	log.Printf("[Job %s] Transcoding completed successfully", job.ID)

	// 3. Premium Feature: AI Whisper Subtitles
	if m.config.IsPremium && job.CreateSubtitles && m.ai != nil {
		log.Printf("[Premium] Running Whisper subtitle generation...")
		generator := whisper.NewGenerator(m.ai)
		if srtPath, sErr := generator.GenerateSRT(job.ctx, job.DestinationPath); sErr != nil {
			log.Printf("Warning: Whisper subtitle generation failed: %v", sErr)
			// Don't fail the whole job just because subtitles failed
		} else {
			log.Printf("[Premium] Subtitles generated: %s", srtPath)
			job.AISubtitles = true
		}
	}

	return nil
}

func (m *Manager) runTest(job *Job) error {
	duration := 10 * time.Second
	start := time.Now()
	for {
		select {
		case <-job.ctx.Done():
			return job.ctx.Err()
		case <-time.After(500 * time.Millisecond):
			elapsed := time.Since(start)
			if elapsed >= duration {
				return nil
			}
			job.Progress = int((elapsed.Seconds() / duration.Seconds()) * 100)
			job.FPS = 24.0
			job.ETA = formatDuration(duration - elapsed)
		}
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// Save persists all jobs to disk
func (m *Manager) Save() error {
	if m.jobsFilePath == "" {
		return nil // No persistence configured
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create a slice of jobs for serialization
	jobList := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobList = append(jobList, job)
	}

	data, err := json.MarshalIndent(jobList, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal jobs: %w", err)
	}

	if err := os.WriteFile(m.jobsFilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write jobs file: %w", err)
	}

	return nil
}

// Load reads persisted jobs from disk
func (m *Manager) Load() error {
	if m.jobsFilePath == "" {
		return nil // No persistence configured
	}

	data, err := os.ReadFile(m.jobsFilePath)
	if err != nil {
		return err
	}

	var jobList []*Job
	if err := json.Unmarshal(data, &jobList); err != nil {
		return fmt.Errorf("failed to unmarshal jobs: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	pendingJobs := 0
	for _, job := range jobList {
		// Reset processing jobs to pending (interrupted by restart)
		if job.Status == StatusProcessing {
			job.Status = StatusPending
			pendingJobs++
		}
		m.jobs[job.ID] = job
	}

	log.Printf("Loaded %d jobs from disk (%d pending)", len(jobList), pendingJobs)
	return nil
}

// RequeuePendingJobs adds all pending jobs back to the queue (call after Start())
func (m *Manager) RequeuePendingJobs() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, job := range m.jobs {
		if job.Status == StatusPending {
			m.queue <- job
			count++
		}
	}

	if count > 0 {
		log.Printf("Requeued %d pending jobs", count)
	}
}
