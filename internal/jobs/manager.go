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
	mu              sync.RWMutex `json:"-"`
	ID              string       `json:"id"`
	Type            JobType      `json:"type"`
	SourcePath      string       `json:"sourcePath"`
	DestinationPath string    `json:"destinationPath"`
	Status          Status    `json:"status"`
	StatusDetail    string    `json:"statusDetail,omitempty"`
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
	VerifyOutput    bool      `json:"verifyOutput"` // Premium feature
	Verified        bool      `json:"verified"`
	DeleteSource    bool      `json:"deleteSource"`

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

func (m *Manager) updateJob(job *Job, fn func(*Job)) {
	job.mu.Lock()
	fn(job)
	job.mu.Unlock()
	m.Save()
}

func (m *Manager) processJob(job *Job) {
	job.mu.Lock()
	job.ctx, job.cancel = context.WithCancel(context.Background())
	job.Status = StatusProcessing
	job.StartedAt = time.Now()

	// Track input size
	if info, err := os.Stat(job.SourcePath); err == nil {
		job.InputSize = info.Size()
	}
	job.mu.Unlock()
	m.Save()

	// Premium Feature: AI Metadata Cleanup
	if m.config.IsPremium && m.ai != nil && job.Type == JobTypeOptimize {
		cleaner := meta.NewCleaner(m.ai)
		job.mu.RLock()
		sourcePath := job.SourcePath
		destPath := job.DestinationPath
		ctx := job.ctx
		job.mu.RUnlock()

		filename := filepath.Base(sourcePath)
		if cleanTitle, err := cleaner.CleanFilename(ctx, filename); err == nil {
			log.Printf("[Premium] AI cleaned filename: %s -> %s", filename, cleanTitle)
			job.mu.Lock()
			job.AICleaned = true
			// Adjust destination path if needed
			ext := filepath.Ext(destPath)
			dir := filepath.Dir(destPath)
			job.DestinationPath = filepath.Join(dir, cleanTitle+ext)
			job.mu.Unlock()
		}
	}

	var err error
	switch job.Type {
	case JobTypeExtract:
		err = m.runExtraction(job)
	case JobTypeOptimize:
		cleanPath := strings.TrimSpace(job.SourcePath)
		lowerPath := strings.ToLower(cleanPath)
		ext := filepath.Ext(cleanPath)
		log.Printf("[Job %s] Checking path for auto-extraction: '%s' (Ext: '%s')", job.ID, cleanPath, ext)

		if strings.HasSuffix(lowerPath, ".iso") || strings.HasSuffix(lowerPath, ".img") || strings.HasSuffix(lowerPath, ".mdf") {
			log.Printf("[Job %s] Detected disc image input. Starting auto-extraction...", job.ID)
			m.updateJob(job, func(j *Job) {
				j.StatusDetail = "Extracting"
			})

			// Ensure destination has a video extension, not a disc image extension
			job.mu.Lock()
			destExt := strings.ToLower(filepath.Ext(job.DestinationPath))
			if destExt == ".iso" || destExt == ".img" || destExt == ".mdf" {
				dir := filepath.Dir(job.DestinationPath)
				base := strings.TrimSuffix(filepath.Base(job.DestinationPath), filepath.Ext(job.DestinationPath))
				job.DestinationPath = filepath.Join(dir, base+".mkv")
				log.Printf("[Job %s] Corrected destination extension: %s", job.ID, job.DestinationPath)
			}
			job.mu.Unlock()

			if m.makemkv == nil {
				err = fmt.Errorf("makemkv not installed")
				break
			}

			// Scan disc
			var info *media.DiscInfo
			info, err = m.makemkv.ScanDisc(job.ctx, cleanPath)
			if err != nil {
				err = fmt.Errorf("scan failed: %v", err)
				break
			}
			if len(info.Titles) == 0 {
				err = fmt.Errorf("no titles found on disc")
				break
			}

			mainTitleIdx := info.FindLargestTitle()
			log.Printf("[Job %s] Identified main feature: Title %d (Total titles: %d)", job.ID, mainTitleIdx, len(info.Titles))

			// Auto-extract first
			extractDir := filepath.Join(filepath.Dir(job.DestinationPath), "extract_"+job.ID)
			if err = os.MkdirAll(extractDir, 0755); err != nil {
				err = fmt.Errorf("failed to create extract dir: %v", err)
				break
			}

			opts := media.ExtractOptions{
				SourcePath: cleanPath,
				OutputDir:  extractDir,
				TitleIndex: mainTitleIdx,
			}

			err = m.makemkv.ExtractWithProgress(job.ctx, opts, func(p media.TranscodeProgress) {
				m.updateJob(job, func(j *Job) {
					j.Progress = p.Percentage / 2 // First 50%
				})
			})

			if err != nil {
				err = fmt.Errorf("extraction failed: %v", err)
				break
			}

			// Find the extracted file
			files, _ := filepath.Glob(filepath.Join(extractDir, "*.mkv"))
			if len(files) == 0 {
				err = fmt.Errorf("extraction finished but no output file found")
				break
			}

			// Proceed to optimize using the extracted file
			extractedSource := files[0]
			log.Printf("[Job %s] Extraction complete. Proceeding to optimize: %s", job.ID, extractedSource)

			m.updateJob(job, func(j *Job) {
				j.StatusDetail = "Optimizing"
			})

			// Pass the extracted source explicitly
			err = m.runOptimizationFromPath(job, extractedSource)

			// Cleanup
			if err == nil {
				os.RemoveAll(extractDir)
			}
		} else {
			log.Printf("[Job %s] Path does not require extraction. Proceeding directly.", job.ID)
			m.updateJob(job, func(j *Job) {
				j.StatusDetail = "Optimizing"
			})
			err = m.runOptimization(job)
		}
	case JobTypeTest:
		err = m.runTest(job)
	}

	if err != nil {
		m.updateJob(job, func(j *Job) {
			j.Status = StatusFailed
			j.Error = err.Error()
			j.CompletedAt = time.Now()
		})
	} else {
		m.updateJob(job, func(j *Job) {
			j.Status = StatusCompleted
			j.Progress = 100
			j.CompletedAt = time.Now()
			// Track output size
			if info, err := os.Stat(j.DestinationPath); err == nil {
				j.OutputSize = info.Size()
			}
		})
	}

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
		m.updateJob(job, func(j *Job) {
			j.Progress = p.Percentage
		})
	})
	if err != nil {
		return fmt.Errorf("extraction failed: %v", err)
	}

	log.Printf("[Job %s] Extraction complete", job.ID)
	return nil
}

func (m *Manager) runOptimizationFromPath(job *Job, sourcePath string) error {
	if m.ffmpeg == nil {
		return fmt.Errorf("ffmpeg wrapper not initialized")
	}

	log.Printf("[Job %s] Starting optimization: %s", job.ID, sourcePath)

	// 1. Get media info for duration
	info, err := m.ffmpeg.GetMediaInfo(job.ctx, sourcePath)
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

	job.mu.RLock()
	destPath := job.DestinationPath
	upscale := job.Upscale
	resolution := job.Resolution
	createSubtitles := job.CreateSubtitles
	verifyOutput := job.VerifyOutput
	deleteSource := job.DeleteSource
	job.mu.RUnlock()

	opts := media.TranscodeOptions{
		InputPath:     sourcePath,
		OutputPath:    destPath,
		GPUVendor:     media.GPUVendor(m.config.GPUVendor),
		Preset:        media.QualityPreset(m.config.QualityPreset),
		CRF:           crf,
		TotalDuration: info.Duration,
		Upscale:       upscale,
		Resolution:    resolution,
	}

	log.Printf("[Job %s] Starting ffmpeg transcoding to: %s", job.ID, opts.OutputPath)

	err = m.ffmpeg.TranscodeWithProgress(job.ctx, opts, func(p media.TranscodeProgress) {
		m.updateJob(job, func(j *Job) {
			j.Progress = p.Percentage
			j.FPS = p.FPS
			j.ETA = p.ETA
		})
	})
	if err != nil {
		log.Printf("[Job %s] FFmpeg failed: %v", job.ID, err)
		return err
	}

	log.Printf("[Job %s] Transcoding completed successfully", job.ID)

	// 3. Premium Feature: AI Whisper Subtitles
	if m.config.IsPremium && createSubtitles && m.ai != nil {
		log.Printf("[Premium] Running Whisper subtitle generation...")
		generator := whisper.NewGenerator(m.ai)
		if srtPath, sErr := generator.GenerateSRT(job.ctx, destPath); sErr != nil {
			log.Printf("Warning: Whisper subtitle generation failed: %v", sErr)
			// Don't fail the whole job just because subtitles failed
		} else {
			log.Printf("[Premium] Subtitles generated: %s", srtPath)
			m.updateJob(job, func(j *Job) {
				j.AISubtitles = true
			})
		}
	}

	// 4. Premium Feature: AI Video Verification (Safe Delete)
	verified := false
	if m.config.IsPremium && verifyOutput && m.ai != nil {
		log.Printf("[Premium] Verifying video integrity with AI...")
		m.updateJob(job, func(j *Job) {
			j.StatusDetail = "Verifying"
		})

		if vOk, vErr := m.runVerificationFromPaths(job, sourcePath, destPath); vErr != nil {
			log.Printf("Warning: AI Verification failed to execute: %v", vErr)
			m.updateJob(job, func(j *Job) {
				j.Error = fmt.Sprintf("Verification error: %v", vErr)
			})
		} else if !vOk {
			log.Printf("[Premium] FAILURE: AI detected corruption in output video.")
			m.updateJob(job, func(j *Job) {
				j.Error = "AI Verification Failed: Corruption detected"
			})
			// We do NOT set verified=true
		} else {
			log.Printf("[Premium] SUCCESS: Video integrity verified by AI.")
			verified = true
			m.updateJob(job, func(j *Job) {
				j.Verified = true
			})
		}
	} else if !verifyOutput {
		// If verification is disabled, we consider it "safe" to delete if the user explicitly asked for it
		// (Standard behavior for non-verified delete)
		verified = true
	}

	// 5. Delete Source (Safe Delete)
	if deleteSource {
		if verified {
			log.Printf("[Job %s] Deleting source file: %s", job.ID, sourcePath)
			if err := os.Remove(sourcePath); err != nil {
				log.Printf("Warning: Failed to delete source file: %v", err)
			}
		} else {
			log.Printf("[Job %s] SKIPPING deletion. Verification failed or not run.", job.ID)
		}
	}

	return nil
}

func (m *Manager) runOptimization(job *Job) error {
	return m.runOptimizationFromPath(job, job.SourcePath)
}

func (m *Manager) runVerificationFromPaths(job *Job, srcPath, destPath string) (bool, error) {
	// Extract 5 frames from source and destination
	// 0%, 25%, 50%, 75%, 90% (avoid 100% as it might be black frame)
	timestamps := []float64{0.0, 0.25, 0.50, 0.75, 0.90}

	// Get durations
	// Get durations
	srcInfo, err := m.ffmpeg.GetMediaInfo(job.ctx, srcPath)
	if err != nil {
		return false, err
	}
	destInfo, err := m.ffmpeg.GetMediaInfo(job.ctx, destPath)
	if err != nil {
		return false, err
	}

	tempDir := filepath.Join(os.TempDir(), "vastiva_verify_"+job.ID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return false, err
	}
	defer os.RemoveAll(tempDir)

	var srcFrames, destFrames []string

	for i, pct := range timestamps {
		srcTime := srcInfo.Duration * pct
		destTime := destInfo.Duration * pct

		srcFrame := filepath.Join(tempDir, fmt.Sprintf("src_%d.jpg", i))
		destFrame := filepath.Join(tempDir, fmt.Sprintf("dest_%d.jpg", i))

		// Extract Source
		if err := m.ffmpeg.ExtractFrame(job.ctx, srcPath, srcTime, srcFrame); err != nil {
			return false, fmt.Errorf("failed to extract source frame %d: %v", i, err)
		}
		// Extract Dest
		if err := m.ffmpeg.ExtractFrame(job.ctx, destPath, destTime, destFrame); err != nil {
			return false, fmt.Errorf("failed to extract dest frame %d: %v", i, err)
		}

		srcFrames = append(srcFrames, srcFrame)
		destFrames = append(destFrames, destFrame)
	}

	return m.ai.VerifyMedia(job.ctx, srcFrames, destFrames)
}

func (m *Manager) runTest(job *Job) error {
	duration := 10 * time.Second
	start := time.Now()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-job.ctx.Done():
			return job.ctx.Err()
		case <-ticker.C:
			elapsed := time.Since(start)
			if elapsed >= duration {
				return nil
			}
			m.updateJob(job, func(j *Job) {
				j.Progress = int((elapsed.Seconds() / duration.Seconds()) * 100)
				j.FPS = 24.0
				j.ETA = formatDuration(duration - elapsed)
			})
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
