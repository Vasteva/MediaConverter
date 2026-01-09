package jobs

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/rwurtz/vastiva/internal/ai"
	"github.com/rwurtz/vastiva/internal/ai/meta"
	"github.com/rwurtz/vastiva/internal/ai/whisper"
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
	CreateSubtitles bool      `json:"createSubtitles"` // Premium feature
	Upscale         bool      `json:"upscale"`         // Premium feature
	Resolution      string    `json:"resolution"`      // Premium feature

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
}

func NewManager(cfg *config.Config, aiProvider ai.Provider) (*Manager, error) {
	ffmpeg, err := media.NewFFmpegWrapper()
	if err != nil {
		log.Printf("Warning: FFmpeg not available: %v", err)
	}

	makemkv, err := media.NewMakeMKVWrapper()
	if err != nil {
		log.Printf("Warning: MakeMKV not available: %v", err)
	}

	return &Manager{
		jobs:          make(map[string]*Job),
		queue:         make(chan *Job, 1000),
		maxConcurrent: cfg.MaxConcurrentJobs,
		stopCh:        make(chan struct{}),
		config:        cfg,
		ffmpeg:        ffmpeg,
		makemkv:       makemkv,
		ai:            aiProvider,
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

	// Premium Feature: AI Metadata Cleanup
	if m.config.IsPremium && m.ai != nil && job.Type == JobTypeOptimize {
		cleaner := meta.NewCleaner(m.ai)
		filename := filepath.Base(job.SourcePath)
		if cleanTitle, err := cleaner.CleanFilename(job.ctx, filename); err == nil {
			log.Printf("[Premium] AI cleaned filename: %s -> %s", filename, cleanTitle)
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
		err = m.runOptimization(job)
	case JobTypeTest:
		err = m.runTest(job)
	}

	if err != nil {
		job.Status = StatusFailed
		job.Error = err.Error()
	} else {
		job.Status = StatusCompleted
		job.Progress = 100
	}
	job.CompletedAt = time.Now()
}

func (m *Manager) runExtraction(job *Job) error {
	if m.makemkv == nil {
		return fmt.Errorf("makemkv wrapper not initialized")
	}
	// TODO: Full extraction logic
	return nil
}

func (m *Manager) runOptimization(job *Job) error {
	if m.ffmpeg == nil {
		return fmt.Errorf("ffmpeg wrapper not initialized")
	}

	// 1. Get media info for duration
	info, err := m.ffmpeg.GetMediaInfo(job.ctx, job.SourcePath)
	if err != nil {
		log.Printf("Warning: Could not get media info: %v", err)
	}

	// 2. Premium Feature: AI Adaptive Encoding
	crf := m.config.CRF
	if m.config.IsPremium && m.ai != nil {
		cleaner := meta.NewCleaner(m.ai)
		log.Printf("[Premium] AI analyzing media for optimal encoding settings...")
		if suggestedCRF, err := cleaner.AnalyzeEncoding(job.ctx, info.RawJSON); err == nil {
			log.Printf("[Premium] AI suggested CRF: %d (System Default: %d)", suggestedCRF, crf)
			crf = suggestedCRF
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

	err = m.ffmpeg.TranscodeWithProgress(job.ctx, opts, func(p media.TranscodeProgress) {
		job.Progress = p.Percentage
		job.FPS = p.FPS
		job.ETA = p.ETA
	})
	if err != nil {
		return err
	}

	// 3. Premium Feature: AI Whisper Subtitles
	if m.config.IsPremium && job.CreateSubtitles && m.ai != nil {
		log.Printf("[Premium] Running Whisper subtitle generation...")
		generator := whisper.NewGenerator(m.ai)
		if srtPath, sErr := generator.GenerateSRT(job.ctx, job.DestinationPath); sErr != nil {
			log.Printf("Warning: Whisper subtitle generation failed: %v", sErr)
			// Don't fail the whole job just because subtitles failed
		} else {
			log.Printf("[Premium] Subtitles generated: %s", srtPath)
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
