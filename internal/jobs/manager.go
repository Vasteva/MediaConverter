package jobs

import (
	"container/heap"
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
	"github.com/Vasteva/MediaConverter/internal/config"
	"github.com/Vasteva/MediaConverter/internal/media"
	"github.com/Vasteva/MediaConverter/internal/subtitles"
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

type AILog struct {
	Timestamp  time.Time `json:"timestamp"`
	Operation  string    `json:"operation"` // "metadata_cleaning" | "encoding_analysis" | "subtitle_download" | "verification"
	Provider   string    `json:"provider"`
	Detail     string    `json:"detail"` // human-readable summary
	DurationMs int64     `json:"durationMs"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
}

type Job struct {
	mu              sync.RWMutex `json:"-"`
	ID              string       `json:"id"`
	Type            JobType      `json:"type"`
	SourcePath      string       `json:"sourcePath"`
	DestinationPath string       `json:"destinationPath"`
	Status          Status       `json:"status"`
	StatusDetail    string       `json:"statusDetail,omitempty"`
	Progress        int          `json:"progress"`
	ETA             string       `json:"eta"`
	FPS             float64      `json:"fps"`
	Priority        int          `json:"priority"`
	CreatedAt       time.Time    `json:"createdAt"`
	StartedAt       time.Time    `json:"startedAt,omitempty"`
	CompletedAt     time.Time    `json:"completedAt,omitempty"`
	Error           string       `json:"error,omitempty"`
	CreateSubtitles bool         `json:"createSubtitles"` // Premium feature
	Upscale         bool         `json:"upscale"`         // Premium feature
	Resolution      string       `json:"resolution"`      // Premium feature
	InputSize       int64        `json:"inputSize"`
	OutputSize      int64        `json:"outputSize"`
	AICleaned       bool         `json:"aiCleaned"`
	AISubtitles     bool         `json:"aiSubtitles"`
	VerifyOutput    bool         `json:"verifyOutput"` // Premium feature
	Verified        bool         `json:"verified"`
	DeleteSource    bool         `json:"deleteSource"`
	MaxRetries      int          `json:"maxRetries"` // 0 = disabled
	RetryCount      int          `json:"retryCount"`
	AILogs          []AILog      `json:"aiLogs,omitempty"`

	// Internal
	ctx    context.Context
	cancel context.CancelFunc
	cmd    *exec.Cmd
}

// priorityQueue implements heap.Interface for *Job.
// Higher Priority value = dequeued first. Equal-priority jobs are ordered FIFO by CreatedAt.
type priorityQueue []*Job

func (pq priorityQueue) Len() int { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool {
	if pq[i].Priority != pq[j].Priority {
		return pq[i].Priority > pq[j].Priority // max-heap
	}
	return pq[i].CreatedAt.Before(pq[j].CreatedAt) // FIFO tiebreak
}
func (pq priorityQueue) Swap(i, j int) { pq[i], pq[j] = pq[j], pq[i] }
func (pq *priorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*Job))
}
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	job := old[n-1]
	old[n-1] = nil
	*pq = old[:n-1]
	return job
}

type Manager struct {
	jobs          map[string]*Job
	pq            priorityQueue
	pqMu          sync.Mutex
	pqCond        *sync.Cond
	maxConcurrent int
	mu            sync.RWMutex
	wg            sync.WaitGroup
	stopCh        chan struct{}
	config        *config.Config
	ffmpeg        *media.FFmpegWrapper
	makemkv       *media.MakeMKVWrapper
	ai            ai.Provider
	OnJobComplete func(*Job)
	OnJobUpdate   func(*Job)
	jobsFilePath  string
	loadErr       string // non-empty if jobs.json existed but could not be parsed
}

// LoadError returns the error message from the initial jobs file load, if any.
func (m *Manager) LoadError() string {
	return m.loadErr
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
		maxConcurrent: cfg.MaxConcurrentJobs,
		stopCh:        make(chan struct{}),
		config:        cfg,
		ffmpeg:        ffmpeg,
		makemkv:       makemkv,
		ai:            aiProvider,
		jobsFilePath:  jobsFilePath,
	}
	m.pqCond = sync.NewCond(&m.pqMu)

	// Load existing jobs from disk
	if err := m.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("ERROR: Could not load existing jobs from %s: %v (queue will start empty)", jobsFilePath, err)
		m.loadErr = err.Error()
	}

	return m, nil
}

func (m *Manager) Start() {
	log.Printf("Job manager started with %d workers", m.maxConcurrent)
	for i := 0; i < m.maxConcurrent; i++ {
		m.wg.Add(1)
		go m.worker(i)
	}
	m.wg.Add(1)
	go m.scheduleWatcher()
}

func (m *Manager) Stop() {
	close(m.stopCh)
	m.pqCond.Broadcast() // wake workers blocked in Wait so they can observe stopCh
	m.wg.Wait()
	log.Println("Job manager stopped")
}

// GetAI returns the current AI provider
func (m *Manager) GetAI() ai.Provider {
	return m.ai
}

// GetConfig returns the current configuration
func (m *Manager) GetConfig() *config.Config {
	return m.config
}

func (m *Manager) worker(id int) {
	defer m.wg.Done()
	for {
		m.pqMu.Lock()
		// Wait until there is work in the schedule window, or a stop signal.
		for m.pq.Len() == 0 || !m.isInScheduleWindow() {
			select {
			case <-m.stopCh:
				m.pqMu.Unlock()
				return
			default:
			}
			m.pqCond.Wait() // releases pqMu; re-acquires it on wakeup
		}
		// Re-check stop after wakeup (Stop broadcasts to unblock waiting workers).
		select {
		case <-m.stopCh:
			m.pqMu.Unlock()
			return
		default:
		}
		job := heap.Pop(&m.pq).(*Job)
		m.pqMu.Unlock()
		m.processJob(job)
	}
}

func (m *Manager) AddJob(job *Job) {
	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()
	m.Save()
	if m.OnJobUpdate != nil {
		m.OnJobUpdate(job)
	}
	m.pqMu.Lock()
	heap.Push(&m.pq, job)
	m.pqMu.Unlock()
	m.pqCond.Signal()
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

// GetVideoResolution returns the video width and height for a media file using ffprobe.
func (m *Manager) GetVideoResolution(ctx context.Context, path string) (width, height int, err error) {
	if m.ffmpeg == nil {
		return 0, 0, fmt.Errorf("ffmpeg not available")
	}
	info, err := m.ffmpeg.GetMediaInfo(ctx, path)
	if err != nil {
		return 0, 0, err
	}
	return info.VideoWidth, info.VideoHeight, nil
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
	if m.OnJobUpdate != nil {
		m.OnJobUpdate(job)
	}
}

func (m *Manager) appendAILog(job *Job, entry AILog) {
	job.mu.Lock()
	job.AILogs = append(job.AILogs, entry)
	job.mu.Unlock()
	m.Save()
	if m.OnJobUpdate != nil {
		m.OnJobUpdate(job)
	}
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
		t0Meta := time.Now()
		if cleanTitle, err := cleaner.CleanFilename(ctx, filename); err == nil {
			log.Printf("[Premium] AI cleaned filename: %s -> %s", filename, cleanTitle)
			job.mu.Lock()
			job.AICleaned = true
			// Adjust destination path if needed
			ext := filepath.Ext(destPath)
			dir := filepath.Dir(destPath)
			job.DestinationPath = filepath.Join(dir, cleanTitle+ext)
			job.mu.Unlock()
			m.appendAILog(job, AILog{
				Timestamp:  t0Meta,
				Operation:  "metadata_cleaning",
				Provider:   m.ai.GetName(),
				Detail:     fmt.Sprintf("Renamed: '%s' → '%s'", filename, cleanTitle),
				DurationMs: time.Since(t0Meta).Milliseconds(),
				Success:    true,
			})
		} else {
			m.appendAILog(job, AILog{
				Timestamp:  t0Meta,
				Operation:  "metadata_cleaning",
				Provider:   m.ai.GetName(),
				Detail:     "No rename needed",
				DurationMs: time.Since(t0Meta).Milliseconds(),
				Success:    false,
				Error:      err.Error(),
			})
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

			t0Extract := time.Now()
			m.appendAILog(job, AILog{
				Timestamp:  t0Extract,
				Operation:  "extraction_start",
				Provider:   "System",
				Detail:     fmt.Sprintf("Starting MakeMKV extraction of %s", filepath.Base(cleanPath)),
				DurationMs: 0,
				Success:    true,
			})

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
			m.appendAILog(job, AILog{
				Timestamp:  t0Extract,
				Operation:  "extraction_complete",
				Provider:   "System",
				Detail:     fmt.Sprintf("Extraction complete (%s), proceeding to optimize", time.Since(t0Extract).Round(time.Second)),
				DurationMs: time.Since(t0Extract).Milliseconds(),
				Success:    true,
			})

			m.updateJob(job, func(j *Job) {
				j.StatusDetail = "Optimizing"
			})

			// Pass the extracted source explicitly
			err = m.runOptimizationFromPath(job, extractedSource)

			// Cleanup
			if err == nil {
				os.RemoveAll(extractDir)
				// If the job manager is configured to delete source, and we just converted an ISO,
				// runOptimizationFromPath won't delete the ISO because its 'sourcePath' (extractedSource)
				// is the intermediate MKV. We need to handle the original source deletion here.
				if m.config.DeleteSource {
					// We only delete if it was verified or if verifyOutput is false
					verified := false
					if !m.config.VerifyOutput {
						verified = true // assume verified if check is disabled
					} else {
						// Re-check if verified bit was set during runOptimizationFromPath (it writes to job.Verified)
						job.mu.RLock()
						verified = job.Verified
						job.mu.RUnlock()
					}

					if verified {
						log.Printf("[Job %s] Deleting original disc image source: %s", job.ID, cleanPath)
						if dErr := os.Remove(cleanPath); dErr != nil {
							log.Printf("Warning: Failed to delete disc image source: %v", dErr)
						} else {
							m.appendAILog(job, AILog{
								Timestamp:  time.Now(),
								Operation:  "file_deleted",
								Provider:   "System",
								Detail:     fmt.Sprintf("Source disc image deleted: %s", cleanPath),
								DurationMs: 0,
								Success:    true,
							})
						}
					}
				}
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
		job.mu.RLock()
		retryCount := job.RetryCount
		maxRetries := job.MaxRetries
		job.mu.RUnlock()

		if retryCount < maxRetries {
			// Exponential backoff: 2^retryCount seconds, capped at 60s
			backoff := time.Duration(1<<uint(retryCount)) * time.Second
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
			log.Printf("[Job %s] Failed (attempt %d/%d), retrying in %s: %v", job.ID, retryCount+1, maxRetries, backoff, err)
			m.updateJob(job, func(j *Job) {
				j.Status = StatusPending
				j.RetryCount = retryCount + 1
				j.Error = fmt.Sprintf("Retry %d/%d: %v", retryCount+1, maxRetries, err)
				j.Progress = 0
				j.FPS = 0
				j.ETA = ""
				j.StatusDetail = fmt.Sprintf("Retrying in %s", backoff.Round(time.Second))
			})
			time.Sleep(backoff)
			m.pqMu.Lock()
			heap.Push(&m.pq, job)
			m.pqMu.Unlock()
			m.pqCond.Signal()
			return
		}

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
			// Track output size (walk directories for extract jobs)
			if info, err := os.Stat(j.DestinationPath); err == nil {
				if info.IsDir() {
					var total int64
					filepath.Walk(j.DestinationPath, func(_ string, fi os.FileInfo, err error) error { //nolint:errcheck
						if err == nil && !fi.IsDir() {
							total += fi.Size()
						}
						return nil
					})
					j.OutputSize = total
				} else {
					j.OutputSize = info.Size()
				}
			}
		})
	}

	// Persist job state to disk
	m.Save()

	if m.OnJobComplete != nil {
		m.OnJobComplete(job)
	}
}

func (m *Manager) isInScheduleWindow() bool {
	sched := m.config.Schedule
	if !sched.Enabled {
		return true
	}
	loc := time.UTC
	if sched.Timezone != "" {
		if l, err := time.LoadLocation(sched.Timezone); err == nil {
			loc = l
		}
	}
	now := time.Now().In(loc)
	if len(sched.AllowedDays) > 0 {
		ok := false
		for _, d := range sched.AllowedDays {
			if int(now.Weekday()) == d {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	h, s, e := now.Hour(), sched.StartHour, sched.EndHour
	if s == e {
		return true // same hour = unrestricted
	}
	if s < e {
		return h >= s && h < e // daytime window
	}
	return h >= s || h < e // overnight window (e.g. 22–06)
}

func (m *Manager) scheduleWatcher() {
	defer m.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	wasAllowed := m.isInScheduleWindow()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			isAllowed := m.isInScheduleWindow()
			if isAllowed && !wasAllowed {
				log.Printf("[Scheduler] Processing window opened — waking workers")
				m.pqCond.Broadcast()
			}
			wasAllowed = isAllowed
		}
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
		t0Enc := time.Now()
		if suggestedCRF, err := cleaner.AnalyzeEncoding(job.ctx, info.RawJSON); err == nil {
			log.Printf("[Premium] AI suggested CRF: %d (System Default: %d)", suggestedCRF, crf)
			defaultCRF := crf
			crf = suggestedCRF
			m.appendAILog(job, AILog{
				Timestamp:  t0Enc,
				Operation:  "encoding_analysis",
				Provider:   m.ai.GetName(),
				Detail:     fmt.Sprintf("Suggested CRF %d (system default: %d)", suggestedCRF, defaultCRF),
				DurationMs: time.Since(t0Enc).Milliseconds(),
				Success:    true,
			})
		} else {
			log.Printf("[Premium] AI analysis failed: %v", err)
			m.appendAILog(job, AILog{
				Timestamp:  t0Enc,
				Operation:  "encoding_analysis",
				Provider:   m.ai.GetName(),
				Detail:     fmt.Sprintf("Analysis failed, using CRF %d", crf),
				DurationMs: time.Since(t0Enc).Milliseconds(),
				Success:    false,
				Error:      err.Error(),
			})
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
	t0Trans := time.Now()
	m.appendAILog(job, AILog{
		Timestamp:  t0Trans,
		Operation:  "transcoding_start",
		Provider:   "System",
		Detail:     fmt.Sprintf("Starting FFmpeg transcoding → %s", filepath.Base(opts.OutputPath)),
		DurationMs: 0,
		Success:    true,
	})

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
	m.appendAILog(job, AILog{
		Timestamp:  t0Trans,
		Operation:  "transcoding_complete",
		Provider:   "System",
		Detail:     fmt.Sprintf("Transcoding complete (%s)", time.Since(t0Trans).Round(time.Second)),
		DurationMs: time.Since(t0Trans).Milliseconds(),
		Success:    true,
	})

	// 3. Subtitle Download
	subtitleMode := m.config.SubtitleMode
	shouldDownload := subtitleMode == "always" || (subtitleMode == "selective" && createSubtitles)
	if shouldDownload && m.config.SubtitleAPIKey != "" {
		log.Printf("[Subtitles] Attempting subtitle download for: %s", filepath.Base(destPath))
		dl := subtitles.NewDownloader(
			m.config.SubtitleAPIKey,
			m.config.SubtitleUsername,
			m.config.SubtitlePassword,
			m.config.SubtitleLang,
		)
		t0Sub := time.Now()
		if srtContent, sErr := dl.Download(job.ctx, destPath); sErr != nil {
			log.Printf("Warning: Subtitle download failed: %v", sErr)
			m.appendAILog(job, AILog{
				Timestamp:  t0Sub,
				Operation:  "subtitle_download",
				Provider:   "opensubtitles",
				Detail:     fmt.Sprintf("Download failed for %s", filepath.Base(destPath)),
				DurationMs: time.Since(t0Sub).Milliseconds(),
				Success:    false,
				Error:      sErr.Error(),
			})
		} else {
			srtPath := strings.TrimSuffix(destPath, filepath.Ext(destPath)) + ".srt"
			if wErr := os.WriteFile(srtPath, []byte(srtContent), 0644); wErr != nil {
				log.Printf("Warning: Failed to save SRT file: %v", wErr)
				m.appendAILog(job, AILog{
					Timestamp:  t0Sub,
					Operation:  "subtitle_download",
					Provider:   "opensubtitles",
					Detail:     fmt.Sprintf("Downloaded but failed to save SRT: %s", filepath.Base(srtPath)),
					DurationMs: time.Since(t0Sub).Milliseconds(),
					Success:    false,
					Error:      wErr.Error(),
				})
			} else {
				log.Printf("[Subtitles] Saved: %s", srtPath)
				m.updateJob(job, func(j *Job) {
					j.AISubtitles = true
				})
				m.appendAILog(job, AILog{
					Timestamp:  t0Sub,
					Operation:  "subtitle_download",
					Provider:   "opensubtitles",
					Detail:     fmt.Sprintf("Downloaded %s subtitles (OpenSubtitles)", m.config.SubtitleLang),
					DurationMs: time.Since(t0Sub).Milliseconds(),
					Success:    true,
				})
			}
		}
	}

	// 4. Premium Feature: AI Video Verification (Safe Delete)
	verified := false
	if m.config.IsPremium && verifyOutput && m.ai != nil {
		log.Printf("[Premium] Verifying video integrity with AI...")
		m.updateJob(job, func(j *Job) {
			j.StatusDetail = "Verifying"
		})

		t0Ver := time.Now()
		if vOk, vErr := m.runVerificationFromPaths(job, sourcePath, destPath); vErr != nil {
			log.Printf("Warning: AI Verification failed to execute: %v", vErr)
			m.updateJob(job, func(j *Job) {
				j.Error = fmt.Sprintf("Verification error: %v", vErr)
			})
			m.appendAILog(job, AILog{
				Timestamp:  t0Ver,
				Operation:  "verification",
				Provider:   m.ai.GetName(),
				Detail:     "Verification failed to execute",
				DurationMs: time.Since(t0Ver).Milliseconds(),
				Success:    false,
				Error:      vErr.Error(),
			})
		} else if !vOk {
			log.Printf("[Premium] FAILURE: AI detected corruption in output video.")
			m.updateJob(job, func(j *Job) {
				j.Error = "AI Verification Failed: Corruption detected"
			})
			m.appendAILog(job, AILog{
				Timestamp:  t0Ver,
				Operation:  "verification",
				Provider:   m.ai.GetName(),
				Detail:     "FAIL — corruption detected",
				DurationMs: time.Since(t0Ver).Milliseconds(),
				Success:    false,
			})
			// We do NOT set verified=true
		} else {
			log.Printf("[Premium] SUCCESS: Video integrity verified by AI.")
			verified = true
			m.updateJob(job, func(j *Job) {
				j.Verified = true
			})
			m.appendAILog(job, AILog{
				Timestamp:  t0Ver,
				Operation:  "verification",
				Provider:   m.ai.GetName(),
				Detail:     "Video integrity verified: PASS",
				DurationMs: time.Since(t0Ver).Milliseconds(),
				Success:    true,
			})
		}
	} else {
		// AI verification not available (not premium, no AI, or verifyOutput disabled).
		// Still require the output file to exist and be non-empty before allowing
		// source deletion, to guard against silent failures.
		if info, statErr := os.Stat(destPath); statErr == nil && info.Size() > 0 {
			verified = true
		} else {
			log.Printf("[Job %s] WARNING: deleteSource requested but output file is missing or empty: %s", job.ID, destPath)
		}
	}

	// 5. Delete Source (Safe Delete)
	if deleteSource {
		if verified {
			log.Printf("[Job %s] Deleting source file: %s", job.ID, sourcePath)
			if err := os.Remove(sourcePath); err != nil {
				log.Printf("Warning: Failed to delete source file: %v", err)
			} else {
				m.appendAILog(job, AILog{
					Timestamp:  time.Now(),
					Operation:  "file_deleted",
					Provider:   "System",
					Detail:     fmt.Sprintf("Source file deleted: %s", sourcePath),
					DurationMs: 0,
					Success:    true,
				})
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

	tmp := m.jobsFilePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("failed to write jobs file: %w", err)
	}
	if err := os.Rename(tmp, m.jobsFilePath); err != nil {
		return fmt.Errorf("failed to rename jobs file: %w", err)
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
			job.RetryCount = 0 // fresh start after server restart
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
	pending := make([]*Job, 0)
	for _, job := range m.jobs {
		if job.Status == StatusPending {
			pending = append(pending, job)
		}
	}
	m.mu.RUnlock()

	if len(pending) == 0 {
		return
	}

	m.pqMu.Lock()
	for _, job := range pending {
		heap.Push(&m.pq, job)
	}
	m.pqMu.Unlock()
	m.pqCond.Broadcast()

	log.Printf("Requeued %d pending jobs", len(pending))
}
