package jobs

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
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

// MarshalJSON serialises a Job while holding its read lock.
//
// Worker goroutines mutate Progress, FPS, ETA, Status and StatusDetail
// continuously during a transcode — the FFmpeg progress callback fires several
// times a second. Meanwhile three separate call sites serialise jobs from other
// goroutines: Manager.Save, the /api/jobs handler via GetAllJobs, and the SSE
// broadcaster on every update. None of them took job.mu, which the race
// detector reports on any run that processes a job.
//
// Locking at the marshal boundary fixes all three at once, and any call site
// added later, without changing a single signature.
func (j *Job) MarshalJSON() ([]byte, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	// The local type sheds this method, so the nested Marshal does not recurse.
	// Converting the pointer avoids copying the struct — and its mutex — which
	// go vet's copylocks check would reject.
	type jobFields Job
	return json.Marshal((*jobFields)(j))
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

func (m *Manager) PurgeJobs(status Status) int {
	m.mu.Lock()
	count := 0
	for id, job := range m.jobs {
		if job.GetStatus() == status {
			delete(m.jobs, id)
			count++
		}
	}
	m.mu.Unlock()

	// Save takes m.mu itself, so it must not be called while the write lock is
	// held. See the note on Save.
	if count > 0 {
		m.Save()
	}
	return count
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
	m.mu.RLock()
	job, ok := m.jobs[id]
	m.mu.RUnlock()
	if !ok {
		return false
	}

	// cancel and Status are both guarded by job.mu — cancel is assigned in
	// processJob under that lock, and Status is written by the worker on every
	// state change.
	job.mu.Lock()
	if job.cancel != nil {
		job.cancel()
	}
	job.Status = StatusCancelled
	job.mu.Unlock()

	// Save takes m.mu itself, so it must not be called while a manager lock is
	// held. See the note on Save.
	m.Save()
	if m.OnJobUpdate != nil {
		m.OnJobUpdate(job)
	}
	return true
}

// CancelAllActive cancels every job that is pending or processing, and returns
// how many were cancelled.
//
// Completed, failed and already-cancelled jobs are left alone: this stops work,
// it does not clear history. That is what PurgeJobs is for.
func (m *Manager) CancelAllActive() int {
	// Snapshot under the manager lock, then take job locks outside it. Holding
	// both at once would invert the ordering used elsewhere, and Save must never
	// be called while a manager lock is held.
	m.mu.RLock()
	candidates := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		candidates = append(candidates, job)
	}
	m.mu.RUnlock()

	cancelled := make([]*Job, 0, len(candidates))
	for _, job := range candidates {
		job.mu.Lock()
		if job.Status != StatusPending && job.Status != StatusProcessing {
			job.mu.Unlock()
			continue
		}
		if job.cancel != nil {
			job.cancel() // kills the FFmpeg subprocess for in-flight jobs
		}
		job.Status = StatusCancelled
		job.mu.Unlock()
		cancelled = append(cancelled, job)
	}

	if len(cancelled) == 0 {
		return 0
	}

	// One save for the batch rather than one per job — cancelling a full queue
	// would otherwise rewrite the entire jobs file once per entry.
	m.Save()

	if m.OnJobUpdate != nil {
		for _, job := range cancelled {
			m.OnJobUpdate(job)
		}
	}

	log.Printf("Cancelled %d active job(s)", len(cancelled))
	return len(cancelled)
}

// GetStatus returns the job's current status under its lock.
//
// Status is written by the worker goroutine throughout a transcode, so reading
// the field directly from any other goroutine is a data race.
func (j *Job) GetStatus() Status {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.Status
}

// RetryJob resets a job and adds it back to the priority queue
func (m *Manager) RetryJob(id string) error {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("job not found")
	}

	job.mu.Lock()
	if job.Status == StatusProcessing {
		job.mu.Unlock()
		m.mu.Unlock()
		return fmt.Errorf("job is already processing")
	}

	// Reset job state for retry
	job.Status = StatusPending
	job.StatusDetail = "Retrying"
	job.Progress = 0
	job.Error = ""
	job.StartedAt = time.Time{}
	job.CompletedAt = time.Time{}
	job.RetryCount++
	job.Verified = false
	job.mu.Unlock()
	m.mu.Unlock()

	m.Save()
	if m.OnJobUpdate != nil {
		m.OnJobUpdate(job)
	}

	// Add to priority queue
	m.pqMu.Lock()
	heap.Push(&m.pq, job)
	m.pqMu.Unlock()
	m.pqCond.Signal()

	return nil
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

	// A cancelled job stays in the priority queue — CancelJob marks it but has
	// no way to remove it from the heap. Without this check the worker pops it
	// and overwrites the status straight back to processing, so cancelling
	// anything still queued did nothing at all: the job ran, and the UI showed
	// it flip from cancelled back to processing.
	if job.Status == StatusCancelled {
		job.mu.Unlock()
		log.Printf("[Job %s] Skipping — cancelled before it started", job.ID)
		return
	}

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
		m.applyAIRename(job)
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
			extractDir := filepath.Join(filepath.Dir(job.DestinationPath), ".extract_"+job.ID)
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

// applyAIRename asks the AI provider for a clean title and, if the result is
// usable and safe, renames the job's destination file accordingly.
//
// The title comes from a model and is turned into a write path, so it gets two
// independent checks: meta.ExtractTitle sanitises the string, and this function
// confirms the joined path did not leave the destination directory. Either
// check failing leaves the original destination untouched — a bad rename is
// never worth failing an otherwise good transcode over.
func (m *Manager) applyAIRename(job *Job) {
	cleaner := meta.NewCleaner(m.ai)

	job.mu.RLock()
	sourcePath := job.SourcePath
	destPath := job.DestinationPath
	ctx := job.ctx
	job.mu.RUnlock()

	filename := filepath.Base(sourcePath)
	started := time.Now()

	cleanTitle, err := cleaner.CleanFilename(ctx, filename)
	if err != nil {
		m.appendAILog(job, AILog{
			Timestamp:  started,
			Operation:  "metadata_cleaning",
			Provider:   m.ai.GetName(),
			Detail:     "Keeping original filename",
			DurationMs: time.Since(started).Milliseconds(),
			Success:    false,
			Error:      err.Error(),
		})
		return
	}

	dir := filepath.Dir(destPath)
	renamed := filepath.Join(dir, cleanTitle+filepath.Ext(destPath))

	if filepath.Dir(renamed) != dir {
		log.Printf("[Job %s] Rejecting AI rename: %q would write outside %s", job.ID, cleanTitle, dir)
		m.appendAILog(job, AILog{
			Timestamp:  started,
			Operation:  "metadata_cleaning",
			Provider:   m.ai.GetName(),
			Detail:     "Rename rejected — resolved outside the destination directory",
			DurationMs: time.Since(started).Milliseconds(),
			Success:    false,
			Error:      fmt.Sprintf("unsafe title: %q", cleanTitle),
		})
		return
	}

	log.Printf("[Job %s] AI cleaned filename: %s -> %s", job.ID, filename, cleanTitle)
	job.mu.Lock()
	job.AICleaned = true
	job.DestinationPath = renamed
	job.mu.Unlock()

	m.appendAILog(job, AILog{
		Timestamp:  started,
		Operation:  "metadata_cleaning",
		Provider:   m.ai.GetName(),
		Detail:     fmt.Sprintf("Renamed: '%s' → '%s'", filename, cleanTitle),
		DurationMs: time.Since(started).Milliseconds(),
		Success:    true,
	})
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

	job.mu.RLock()
	sourcePath := job.SourcePath
	deleteSource := job.DeleteSource
	job.mu.RUnlock()

	log.Printf("[Job %s] Starting disc extraction for %s", job.ID, sourcePath)

	// 1. Scan disc to find titles
	m.updateJob(job, func(j *Job) {
		j.StatusDetail = "Scanning"
	})
	info, err := m.makemkv.ScanDisc(job.ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("failed to scan disc: %v", err)
	}
	if len(info.Titles) == 0 {
		return fmt.Errorf("no titles found on disc")
	}

	// 2. Find the main feature (largest title)
	mainTitleIdx := info.FindLargestTitle()
	log.Printf("[Job %s] Detected main feature: Title %d", job.ID, mainTitleIdx)

	// 3. Extract to a temporary directory so we can rename the output cleanly
	m.updateJob(job, func(j *Job) {
		j.StatusDetail = "Extracting"
	})
	tmpDir := filepath.Join(filepath.Dir(sourcePath), ".extract_"+job.ID)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp extract dir: %v", err)
	}

	opts := media.ExtractOptions{
		SourcePath: sourcePath,
		OutputDir:  tmpDir,
		TitleIndex: mainTitleIdx,
	}
	err = m.makemkv.ExtractWithProgress(job.ctx, opts, func(p media.TranscodeProgress) {
		m.updateJob(job, func(j *Job) {
			j.Progress = p.Percentage
		})
	})
	if err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("extraction failed: %v", err)
	}

	// 4. Find the extracted MKV in the temp dir
	mkvFiles, _ := filepath.Glob(filepath.Join(tmpDir, "*.mkv"))
	if len(mkvFiles) == 0 {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("extraction finished but no output file found")
	}

	// 5. Move it alongside the source ISO, named after the ISO (no subfolder)
	sourceBase := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	finalPath := filepath.Join(filepath.Dir(sourcePath), sourceBase+".mkv")
	if err := os.Rename(mkvFiles[0], finalPath); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to move extracted file: %v", err)
	}
	os.RemoveAll(tmpDir)

	// 6. Update job destination to the final file path
	m.updateJob(job, func(j *Job) {
		j.DestinationPath = finalPath
		if fi, statErr := os.Stat(finalPath); statErr == nil {
			j.OutputSize = fi.Size()
		}
	})
	log.Printf("[Job %s] Extraction complete: %s", job.ID, finalPath)

	// 7. Delete the source ISO if configured and the output is verified non-empty
	if deleteSource {
		if fi, statErr := os.Stat(finalPath); statErr == nil && fi.Size() > 0 {
			log.Printf("[Job %s] Deleting source disc image: %s", job.ID, sourcePath)
			if delErr := os.Remove(sourcePath); delErr != nil {
				log.Printf("Warning: Failed to delete source disc image: %v", delErr)
			} else {
				m.appendAILog(job, AILog{
					Timestamp:  time.Now(),
					Operation:  "file_deleted",
					Provider:   "System",
					Detail:     fmt.Sprintf("Source disc image deleted: %s", sourcePath),
					DurationMs: 0,
					Success:    true,
				})
			}
		} else {
			log.Printf("[Job %s] SKIPPING source deletion — output file missing or empty", job.ID)
		}
	}

	return nil
}

func (m *Manager) runOptimizationFromPath(job *Job, sourcePath string) error {
	if m.ffmpeg == nil {
		return fmt.Errorf("ffmpeg wrapper not initialized")
	}

	log.Printf("[Job %s] Starting optimization: %s", job.ID, sourcePath)

	// 1. Probe the source. Everything downstream — encoder profile, colour
	// signalling, and the output validation gate — is derived from this.
	info, err := m.ffmpeg.GetMediaInfo(job.ctx, sourcePath)
	if err != nil {
		log.Printf("[Job %s] Error getting media info: %v", job.ID, err)
		return fmt.Errorf("failed to get media info: %w", err)
	}

	log.Printf("[Job %s] Source: %.2fs, %dx%d, %s (%d-bit), transfer=%q, DV profile=%d",
		job.ID, info.Duration, info.VideoWidth, info.VideoHeight,
		info.PixFmt, info.BitDepth, info.ColorTransfer, info.DVProfile)

	// Reject inputs this pipeline cannot encode correctly, rather than emitting
	// a broken file that looks like a success.
	if err := media.CheckSourceSupported(info); err != nil {
		log.Printf("[Job %s] %v", job.ID, err)
		m.appendAILog(job, AILog{
			Timestamp: time.Now(),
			Operation: "source_rejected",
			Provider:  "System",
			Detail:    err.Error(),
			Success:   false,
			Error:     err.Error(),
		})
		return err
	}
	if info.IsDolbyVision() {
		log.Printf("[Job %s] Note: Dolby Vision profile %d — encoding the HDR10 base layer; "+
			"Dolby Vision metadata will not survive the re-encode", job.ID, info.DVProfile)
	}

	// 2. Premium Feature: AI Adaptive Encoding
	crf := m.config.CRF
	if m.config.IsPremium && m.ai != nil {
		cleaner := meta.NewCleaner(m.ai)
		log.Printf("[Premium] AI analyzing media for optimal encoding settings...")
		t0Enc := time.Now()
		// Pass a short summary rather than the full ffprobe dump: for a UHD
		// REMUX with 48 streams the raw JSON is tens of kilobytes that bury the
		// few facts the decision actually turns on.
		if suggestedCRF, err := cleaner.AnalyzeEncoding(job.ctx, info.EncodingSummary()); err == nil {
			log.Printf("[Job %s] AI suggested CRF %d (system default %d)", job.ID, suggestedCRF, crf)
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
			// Not an error condition: the configured CRF is a perfectly good
			// answer, and this path is taken whenever the model declines to
			// produce a usable number. Logged at info level so it stops looking
			// like a fault in the logs.
			log.Printf("[Job %s] No AI CRF suggestion, using configured CRF %d (%v)", job.ID, crf, err)
			m.appendAILog(job, AILog{
				Timestamp:  t0Enc,
				Operation:  "encoding_analysis",
				Provider:   m.ai.GetName(),
				Detail:     fmt.Sprintf("No suggestion available — using configured CRF %d", crf),
				DurationMs: time.Since(t0Enc).Milliseconds(),
				Success:    true,
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

	// Replace-in-place writes a temp file beside the source and swaps it in once
	// validated, so the optimised file takes the source's position in the
	// library. Without this the output lands in a flat directory that the media
	// server does not scan, and accumulates there indefinitely.
	replacing := m.config.ReplaceInPlace && m.config.HoldingDir != ""
	var replacement replacementPaths
	if replacing {
		replacement = planReplacement(sourcePath, job.ID)
		destPath = replacement.Temp
		defer m.cleanupTemp(job, replacement.Temp)
		log.Printf("[Job %s] Replace-in-place: writing %s, will become %s",
			job.ID, filepath.Base(replacement.Temp), filepath.Base(replacement.Final))
	} else if m.config.ReplaceInPlace {
		log.Printf("[Job %s] REPLACE_IN_PLACE is set but HOLDING_DIR is empty — "+
			"writing to the configured destination instead. Replacement needs somewhere "+
			"to retain the original.", job.ID)
	}

	if sourcePath == destPath {
		return fmt.Errorf("source and destination paths are identical (%s): FFmpeg cannot encode a file in-place", sourcePath)
	}

	opts := media.TranscodeOptions{
		InputPath:  sourcePath,
		OutputPath: destPath,
		GPUVendor:  media.GPUVendor(m.config.GPUVendor),
		Preset:     media.QualityPreset(m.config.QualityPreset),
		CRF:        crf,
		Upscale:    upscale,
		Resolution: resolution,
	}
	// Carries duration, bit depth, and colour signalling from the probe.
	opts.ApplySourceInfo(info)

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
		var integrityErr *media.TranscodeIntegrityError
		if errors.As(err, &integrityErr) {
			// FFmpeg exited 0 but dropped frames. Report it as its own failure
			// mode — it is not a crash, and calling it one hides the cause.
			log.Printf("[Job %s] Decode errors during transcode, output discarded: %v",
				job.ID, integrityErr.Findings)
			m.appendAILog(job, AILog{
				Timestamp: time.Now(),
				Operation: "decode_errors",
				Provider:  "System",
				Detail: fmt.Sprintf("FFmpeg exited 0 but reported %d decode error(s) — output incomplete",
					len(integrityErr.Findings)),
				Success: false,
				Error:   strings.Join(integrityErr.Findings, " | "),
			})
		} else {
			log.Printf("[Job %s] FFmpeg failed: %v", job.ID, err)
		}
		m.discardOutput(job, destPath, "transcode failed")
		return err
	}

	// Validate before believing the exit code. A transcode can stop early and
	// leave a well-formed but truncated file, which exits zero and passes an
	// existence check. Nothing downstream — success status, source deletion —
	// may happen until this passes.
	m.updateJob(job, func(j *Job) { j.StatusDetail = "Validating" })
	t0Val := time.Now()
	if valErr := m.ffmpeg.ValidateOutput(job.ctx, info, destPath); valErr != nil {
		log.Printf("[Job %s] Output rejected: %v", job.ID, valErr)
		m.appendAILog(job, AILog{
			Timestamp:  t0Val,
			Operation:  "output_validation",
			Provider:   "System",
			Detail:     "Output rejected — see error",
			DurationMs: time.Since(t0Val).Milliseconds(),
			Success:    false,
			Error:      valErr.Error(),
		})
		m.discardOutput(job, destPath, "failed validation")
		return valErr
	}
	m.appendAILog(job, AILog{
		Timestamp:  t0Val,
		Operation:  "output_validation",
		Provider:   "System",
		Detail:     "Output validated: duration, streams and size within tolerance",
		DurationMs: time.Since(t0Val).Milliseconds(),
		Success:    true,
	})

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
			// The check could not be run. Treat that as inconclusive rather than
			// as a pass: the deterministic gate above already cleared the output,
			// so keep it and complete the job, but leave Verified false so the
			// source is not deleted.
			log.Printf("[Job %s] Warning: AI verification could not run: %v", job.ID, vErr)
			m.appendAILog(job, AILog{
				Timestamp:  t0Ver,
				Operation:  "verification",
				Provider:   m.ai.GetName(),
				Detail:     "Verification could not run — output kept, source retained",
				DurationMs: time.Since(t0Ver).Milliseconds(),
				Success:    false,
				Error:      vErr.Error(),
			})
		} else if !vOk {
			// The model looked at the output and judged it broken. That is a
			// failed job, not a successful one with a note attached.
			log.Printf("[Job %s] FAILURE: AI detected corruption in output video.", job.ID)
			m.appendAILog(job, AILog{
				Timestamp:  t0Ver,
				Operation:  "verification",
				Provider:   m.ai.GetName(),
				Detail:     "FAIL — corruption detected",
				DurationMs: time.Since(t0Ver).Milliseconds(),
				Success:    false,
			})
			m.discardOutput(job, destPath, "failed AI verification")
			return fmt.Errorf("AI verification failed: corruption detected in output")
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
		// AI verification not available (not premium, no AI, or verifyOutput
		// disabled). The deterministic gate above has already confirmed the
		// output is a complete, probeable transcode of the source — duration,
		// streams and size all within tolerance — which is a far stronger
		// guarantee than the size>0 check this replaced. That check accepted a
		// 2.7 MB stub of a 60 GB source as grounds for deleting the original.
		verified = true
	}

	// 5. Reintegration. The transcode is validated and, where AI verification is
	// enabled, verified — so it can now take the source's place in the library.
	if replacing {
		if !verified {
			// Verification is inconclusive or failed. Keep the library exactly
			// as it is: the temp file is discarded by the deferred cleanup.
			log.Printf("[Job %s] Skipping replacement — output not verified", job.ID)
			m.appendAILog(job, AILog{
				Timestamp: time.Now(),
				Operation: "reintegration_skipped",
				Provider:  "System",
				Detail:    "Output was not verified; library left unchanged",
				Success:   false,
			})
			return fmt.Errorf("replacement skipped: output could not be verified")
		}

		if err := m.reintegrate(job, replacement); err != nil {
			log.Printf("[Job %s] Reintegration failed: %v", job.ID, err)
			m.appendAILog(job, AILog{
				Timestamp: time.Now(),
				Operation: "reintegration_failed",
				Provider:  "System",
				Detail:    "Could not place the transcode in the library",
				Success:   false,
				Error:     err.Error(),
			})
			return err
		}

		// The job's real output is the promoted file, not the temp path.
		m.updateJob(job, func(j *Job) { j.DestinationPath = replacement.Final })

		// The original has been moved to holding, not deleted — deleteSource is
		// not the mechanism here and must not also run.
		return nil
	}

	// Ownership for the non-replacing path, so outputs are still usable from
	// outside the container.
	if err := m.fileOwnership().Apply(destPath); err != nil {
		log.Printf("[Job %s] Warning: %v", job.ID, err)
	}

	// 6. Delete Source (Safe Delete)
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

// discardOutput removes a rejected transcode output so a broken file is never
// left sitting at the destination looking like a successful conversion.
//
// FFmpeg creates the output file before it starts encoding, so an encoder that
// fails at initialisation leaves a 0-byte file behind, and one that dies partway
// leaves a truncated one. Neither is distinguishable from a good result by
// looking at the directory.
func (m *Manager) discardOutput(job *Job, destPath, reason string) {
	if destPath == "" {
		return
	}
	fi, err := os.Stat(destPath)
	if err != nil {
		return // nothing was written
	}
	if fi.IsDir() {
		log.Printf("[Job %s] Refusing to discard %s: it is a directory", job.ID, destPath)
		return
	}
	if err := os.Remove(destPath); err != nil {
		log.Printf("[Job %s] Warning: could not remove rejected output %s: %v", job.ID, destPath, err)
		return
	}
	log.Printf("[Job %s] Removed rejected output (%s, %d bytes): %s",
		job.ID, reason, fi.Size(), destPath)
	m.appendAILog(job, AILog{
		Timestamp: time.Now(),
		Operation: "output_discarded",
		Provider:  "System",
		Detail:    fmt.Sprintf("Removed %d-byte output — %s", fi.Size(), reason),
		Success:   true,
	})
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

// Save persists all jobs to disk.
//
// Takes m.mu itself, so callers must not hold any manager lock when calling it.
// RWMutex is not reentrant: CancelJob and PurgeJobs previously called Save
// while holding m.mu.Lock(), which deadlocked the goroutine against a lock it
// already held and left the mutex permanently locked — wedging every later
// GetAllJobs, AddJob and Save behind it. Both are wired to UI buttons.
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
		if job.GetStatus() == StatusPending {
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
