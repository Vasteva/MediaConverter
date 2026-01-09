package jobs

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/rwurtz/vastiva/internal/config"
	"github.com/rwurtz/vastiva/internal/media"
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
}

func NewManager(maxConcurrent int, cfg *config.Config) (*Manager, error) {
	ffmpeg, err := media.NewFFmpegWrapper()
	if err != nil {
		log.Printf("Warning: FFmpeg not available: %v", err)
		log.Println("Optimization jobs will not be available")
		// FFmpeg is optional, continue without it
	}

	makemkv, err := media.NewMakeMKVWrapper()
	if err != nil {
		log.Printf("Warning: MakeMKV not available: %v", err)
		log.Println("Extraction jobs will not be available")
		// MakeMKV is optional, continue without it
	}

	return &Manager{
		jobs:          make(map[string]*Job),
		queue:         make(chan *Job, 1000),
		maxConcurrent: maxConcurrent,
		stopCh:        make(chan struct{}),
		config:        cfg,
		ffmpeg:        ffmpeg,
		makemkv:       makemkv,
	}, nil
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

func (m *Manager) processJob(job *Job) {
	job.ctx, job.cancel = context.WithCancel(context.Background())
	job.Status = StatusProcessing
	job.StartedAt = time.Now()

	var err error
	switch job.Type {
	case JobTypeExtract:
		err = m.runExtraction(job)
	case JobTypeOptimize:
		err = m.runOptimization(job)
	case JobTypeTest:
		err = m.runTest(job)
	}

	if err != nil {
		job.Status = StatusFailed
		job.Error = err.Error()
	} else if job.Status != StatusCancelled {
		job.Status = StatusCompleted
		job.Progress = 100
	}
	job.CompletedAt = time.Now()
}

func (m *Manager) runExtraction(job *Job) error {
	if m.makemkv == nil {
		return fmt.Errorf("MakeMKV is not available")
	}

	log.Printf("[Job %s] Starting extraction: %s", job.ID, job.SourcePath)

	// Scan the disc first
	discInfo, err := m.makemkv.ScanDisc(job.ctx, job.SourcePath)
	if err != nil {
		return fmt.Errorf("failed to scan disc: %w", err)
	}

	log.Printf("[Job %s] Found %d titles on disc: %s", job.ID, len(discInfo.Titles), discInfo.Name)

	// Determine which title to extract
	titleIndex := discInfo.FindLargestTitle()
	if titleIndex == 0 && len(discInfo.Titles) > 0 {
		titleIndex = discInfo.Titles[0].Index
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(job.DestinationPath, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Extract the title
	extractOpts := media.ExtractOptions{
		SourcePath: job.SourcePath,
		OutputDir:  job.DestinationPath,
		TitleIndex: titleIndex,
		MinLength:  300, // 5 minutes minimum
	}

	job.Progress = 10
	log.Printf("[Job %s] Extracting title %d...", job.ID, titleIndex)

	if err := m.makemkv.Extract(job.ctx, extractOpts); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	job.Progress = 100
	log.Printf("[Job %s] Extraction completed successfully", job.ID)

	return nil
}

func (m *Manager) runOptimization(job *Job) error {
	if m.ffmpeg == nil {
		return fmt.Errorf("FFmpeg is not available - cannot run optimization jobs")
	}

	log.Printf("[Job %s] Starting optimization: %s", job.ID, job.SourcePath)

	// Verify input file exists
	if _, err := os.Stat(job.SourcePath); os.IsNotExist(err) {
		return fmt.Errorf("source file does not exist: %s", job.SourcePath)
	}

	// Get media info to calculate progress
	mediaInfo, err := m.ffmpeg.GetMediaInfo(job.ctx, job.SourcePath)
	if err != nil {
		log.Printf("[Job %s] Warning: Could not get media info: %v", job.ID, err)
	}

	// Determine output path
	outputPath := job.DestinationPath
	if outputPath == "" {
		// Generate output path based on source
		dir := filepath.Dir(job.SourcePath)
		base := filepath.Base(job.SourcePath)
		ext := filepath.Ext(base)
		name := base[:len(base)-len(ext)]
		outputPath = filepath.Join(dir, name+"_optimized.mkv")
	}

	// Create output directory if needed
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Configure transcoding options
	transcodeOpts := media.TranscodeOptions{
		InputPath:  job.SourcePath,
		OutputPath: outputPath,
		GPUVendor:  media.GPUVendor(m.config.GPUVendor),
		Preset:     media.QualityPreset(m.config.QualityPreset),
		CRF:        m.config.CRF,
		AudioCodec: "copy",
		Container:  "mkv",
	}

	// Progress callback
	progressCallback := func(progress media.TranscodeProgress) {
		job.FPS = progress.FPS

		// Calculate percentage if we have duration
		if mediaInfo != nil && mediaInfo.Duration > 0 {
			job.Progress = media.CalculatePercentage(progress.Time, mediaInfo.Duration)
			job.ETA = media.EstimateETA(progress.Time, mediaInfo.Duration, progress.Speed)
		}

		log.Printf("[Job %s] Progress: %d%% | FPS: %.1f | Speed: %s | ETA: %s",
			job.ID, job.Progress, job.FPS, progress.Speed, job.ETA)
	}

	// Run transcoding with progress monitoring
	log.Printf("[Job %s] Starting transcoding with %s acceleration...", job.ID, m.config.GPUVendor)
	if err := m.ffmpeg.TranscodeWithProgress(job.ctx, transcodeOpts, progressCallback); err != nil {
		return fmt.Errorf("transcoding failed: %w", err)
	}

	job.Progress = 100
	job.ETA = "00:00:00"
	log.Printf("[Job %s] Optimization completed successfully: %s", job.ID, outputPath)

	return nil
}

func (m *Manager) runTest(job *Job) error {
	log.Printf("[Job %s] Starting test job: %s", job.ID, job.SourcePath)

	totalSteps := 100
	duration := 20 * time.Second // 20 seconds total duration
	stepDuration := duration / time.Duration(totalSteps)

	// Simulate progress
	for i := 0; i <= totalSteps; i++ {
		// Check for cancellation
		select {
		case <-job.ctx.Done():
			return fmt.Errorf("job cancelled")
		default:
			// Continue
		}

		job.Progress = i
		job.FPS = 60.0

		// Calculate fake ETA
		remainingSteps := totalSteps - i
		etaDuration := time.Duration(remainingSteps) * stepDuration
		job.ETA = formatDuration(etaDuration)

		if i%10 == 0 {
			log.Printf("[Job %s] Progress: %d%% | FPS: %.1f | ETA: %s", job.ID, job.Progress, job.FPS, job.ETA)
		}

		time.Sleep(stepDuration)
	}

	job.Progress = 100
	job.ETA = "00:00:00"
	log.Printf("[Job %s] Test job completed successfully", job.ID)
	return nil
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
