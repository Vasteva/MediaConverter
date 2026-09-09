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
	"sort"
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

	// queued is true whenever this Job is currently sitting in m.pq, guarded
	// by mu like every other field a worker and an API handler can touch at
	// once. Without it, RetryJob and the auto-retry backoff path (#44) could
	// each push the same *Job onto the heap independently — RetryJob sees
	// Status already reset to Pending during the backoff sleep and has no way
	// to tell the job is about to be re-pushed when the sleep ends — so it
	// gets popped twice and run by two workers at once, both writing the same
	// output file.
	queued bool
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

	// OnOutputClaimed / OnOutputReleased bracket the moment a replace-in-place
	// job's finished transcode is renamed into the library. The scanner wires
	// these to mark that path in-flight before it appears on disk: the rename
	// fires a filesystem event its directory watcher sees, and without the
	// claim that event races the completion hook and queues a second optimise
	// of the job's own output (the case that slips through is an .mp4/.avi
	// source becoming a differently-named .mkv — a same-name .mkv overwrite is
	// already covered by the source path's own entry). OnOutputReleased undoes
	// the claim when the rename fails; a successful job's completion hook
	// supersedes it with a durable entry.
	OnOutputClaimed  func(path string)
	OnOutputReleased func(path string)

	jobsFilePath string
	loadErr      string // non-empty if jobs.json existed but could not be parsed

	// saveMu guards lastProgressSave, the throttle updateJobProgress uses to
	// cap how often a high-frequency progress tick triggers a full Save() (#45).
	saveMu           sync.Mutex
	lastProgressSave time.Time
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

// GetAI returns the current AI provider. UpdateAIProvider can swap it from an
// HTTP goroutine at any time, so every reader — including internal callers —
// goes through this rather than the m.ai field directly (#43).
func (m *Manager) GetAI() ai.Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ai
}

// GetConfig returns a point-in-time copy of the current configuration.
// Deliberately a value, not the shared *config.Config: that pointer is
// mutated in place by POST /api/config from an HTTP goroutine while workers
// and scanner scans read it, with nothing to synchronize the two (#43).
// Field access on the returned value works exactly like it did on the old
// pointer (cfg.SomeField), so this doesn't change any existing call site's
// syntax — it just makes what they get back an inert snapshot instead of a
// live, concurrently-mutable pointer.
func (m *Manager) GetConfig() config.Config {
	return m.config.Snapshot()
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
		job.clearQueued()
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
	job.markQueued()
	m.pqMu.Lock()
	heap.Push(&m.pq, job)
	m.pqMu.Unlock()
	m.pqCond.Signal()
}

// PurgeJobs removes every job with the given status from the tracked job set.
//
// A purged job that is still Pending may still be sitting in the priority
// queue — deleting it from m.jobs alone does not stop a worker from popping
// and running it (#44). It gets the same treatment CancelJob gives an active
// job: cancel its context (a no-op for a job that never started) and mark it
// Cancelled, so processJob's existing "cancelled before it started" guard
// also catches this case, rather than running a job whose record just
// vanished out from under it.
func (m *Manager) PurgeJobs(status Status) int {
	m.mu.Lock()
	count := 0
	var purged []*Job
	for id, job := range m.jobs {
		if job.GetStatus() == status {
			delete(m.jobs, id)
			purged = append(purged, job)
			count++
		}
	}
	m.mu.Unlock()

	for _, job := range purged {
		job.mu.Lock()
		if job.cancel != nil {
			job.cancel()
		}
		job.Status = StatusCancelled
		job.mu.Unlock()
	}

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

// GetVideoResolution returns the video width, height, codec, and
// bits-per-pixel density for a media file using ffprobe, for the scanner's
// resolution (#41) and already-efficient (#39) pre-queue filters. One probe
// serves both so scanning a directory doesn't cost a second ffprobe per file.
func (m *Manager) GetVideoResolution(ctx context.Context, path string) (width, height int, codec string, bitsPerPixel float64, err error) {
	if m.ffmpeg == nil {
		return 0, 0, "", 0, fmt.Errorf("ffmpeg not available")
	}
	info, err := m.ffmpeg.GetMediaInfo(ctx, path)
	if err != nil {
		return 0, 0, "", 0, err
	}
	return info.VideoWidth, info.VideoHeight, info.CodecName, info.BitsPerPixel(), nil
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

// markQueued sets queued and reports whether it was already true. Every path
// that pushes a Job onto the heap calls this first and skips the push if it
// reports true — that's what stops RetryJob and the auto-retry backoff path
// from both pushing the same job (#44).
func (j *Job) markQueued() (alreadyQueued bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	alreadyQueued = j.queued
	j.queued = true
	return alreadyQueued
}

// clearQueued marks a job as no longer sitting in the heap. Called once a
// worker pops it, so a later retry is free to push it again.
func (j *Job) clearQueued() {
	j.mu.Lock()
	j.queued = false
	j.mu.Unlock()
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
	// A job the auto-retry backoff path (processJob's failure branch) has
	// already reset to Pending and is about to re-push once its sleep ends
	// looks identical to any other pending job from here — the queued flag
	// is what's left to tell them apart. Without this check, both this push
	// and that one land, and two workers run the same job at once (#44).
	if job.queued {
		job.mu.Unlock()
		m.mu.Unlock()
		return fmt.Errorf("job is already queued")
	}
	job.queued = true

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

// progressSaveInterval caps how often a progress-only tick — Progress/FPS/ETA
// during a transcode or extraction, which FFmpeg/MakeMKV emit several times a
// second — triggers Save(). Save() marshals and rewrites every job in the
// manager, not just this one, so without a throttle here that whole-file
// rewrite fired at FFmpeg's stats rate rather than the job's own progress
// (#45).
//
// A var, not a const: tests shrink it to keep TestUpdateJobProgressThrottlesSaves
// fast rather than waiting out a real second.
var progressSaveInterval = time.Second

// updateJobProgress is updateJob for high-frequency progress ticks. It always
// broadcasts via OnJobUpdate, so a progress bar watching SSE stays live, but
// throttles the disk write to at most once per progressSaveInterval.
//
// Only use this for fields the next tick will overwrite anyway (Progress,
// FPS, ETA). Anything that represents a real transition — a status change,
// an error, a completion — must go through updateJob instead: a save skipped
// here and never retried is a state that silently never reached disk.
func (m *Manager) updateJobProgress(job *Job, fn func(*Job)) {
	job.mu.Lock()
	fn(job)
	job.mu.Unlock()

	m.saveMu.Lock()
	due := time.Since(m.lastProgressSave) >= progressSaveInterval
	if due {
		m.lastProgressSave = time.Now()
	}
	m.saveMu.Unlock()
	if due {
		m.Save()
	}

	if m.OnJobUpdate != nil {
		m.OnJobUpdate(job)
	}
}

// maxRetainedTerminalJobs caps how many completed/failed jobs jobs.json
// accumulates. Nothing else prunes history — a long-running install (the
// scanner auto-creates one job per source file it finds) grows the file, and
// Save()'s full-marshal cost with it, forever (#45).
//
// A var, not a const: TestPruneOldJobsCapsTerminalJobCount lowers it rather
// than creating hundreds of jobs to reach the real cap.
var maxRetainedTerminalJobs = 500

// pruneOldJobs drops the oldest completed/failed jobs once more than
// maxRetainedTerminalJobs are tracked, keeping the most recent by
// CompletedAt. Pending and processing jobs are never touched. Called once a
// job reaches a terminal state, so retention enforces itself without a
// separate background sweep.
func (m *Manager) pruneOldJobs() {
	type terminalJob struct {
		job         *Job
		completedAt time.Time
	}

	m.mu.Lock()
	var terminal []terminalJob
	for _, job := range m.jobs {
		job.mu.RLock()
		status := job.Status
		completedAt := job.CompletedAt
		job.mu.RUnlock()
		if status == StatusCompleted || status == StatusFailed {
			terminal = append(terminal, terminalJob{job, completedAt})
		}
	}
	if len(terminal) <= maxRetainedTerminalJobs {
		m.mu.Unlock()
		return
	}

	sort.Slice(terminal, func(i, j int) bool {
		return terminal[i].completedAt.Before(terminal[j].completedAt)
	})
	for _, t := range terminal[:len(terminal)-maxRetainedTerminalJobs] {
		delete(m.jobs, t.job.ID)
	}
	m.mu.Unlock()

	// Save takes m.mu itself, so it must not be called while the write lock
	// is held — see the same note on PurgeJobs. Without this, the pruned
	// entries stay on disk until some other job happens to trigger a save.
	m.Save()
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
	if aiProvider := m.GetAI(); m.config.Snapshot().IsPremium && aiProvider != nil && job.Type == JobTypeOptimize {
		m.applyAIRename(job, aiProvider)
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
			// Deferred, not conditional on the optimize step below succeeding
			// (#47): extraction has already produced a full-size intermediate
			// MKV in this directory by the time we get there, and a failed
			// optimize left it behind forever. Harmless to run again on a
			// retry — MkdirAll above recreates the directory, and RemoveAll
			// on a path that's already gone is a no-op.
			defer os.RemoveAll(extractDir)

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
				m.updateJobProgress(job, func(j *Job) {
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
			var optimizeVerified bool
			optimizeVerified, err = m.runOptimizationFromPath(job, extractedSource)

			// extractDir cleanup is deferred above, unconditionally — this
			// gates only the disc-image DeleteSource decision, which must
			// never fire on a failed optimize.
			if err == nil {
				// runOptimizationFromPath already used optimizeVerified to decide
				// whether to delete its own sourcePath argument — the intermediate
				// MKV (extractedSource), not the original disc image. The disc
				// image is this job's actual DeleteSource target, handled here,
				// gated on the SAME verification result runOptimizationFromPath
				// already computed rather than a second, independently-derived
				// copy of it (the previous version of this code re-checked
				// job.Verified, which is only ever set when AI verification
				// actually ran and passed — so with verifyOutput on but AI
				// verification inconclusive, that re-check silently read false
				// too, but for the wrong reason, and a future edit to either path
				// could easily let the two diverge and delete on a result that
				// was never actually verified).
				if job.DeleteSource {
					if optimizeVerified {
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
					} else {
						log.Printf("[Job %s] SKIPPING disc image deletion — optimization output not verified", job.ID)
					}
				}
			}
		} else {
			log.Printf("[Job %s] Path does not require extraction. Proceeding directly.", job.ID)
			m.updateJob(job, func(j *Job) {
				j.StatusDetail = "Optimizing"
			})
			_, err = m.runOptimization(job)
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
			// A manual RetryJob call during the sleep above already reset
			// this job to Pending and pushed it — markQueued reports that so
			// this path can skip its own push rather than queuing the same
			// *Job a second time (#44).
			if !job.markQueued() {
				m.pqMu.Lock()
				heap.Push(&m.pq, job)
				m.pqMu.Unlock()
				m.pqCond.Signal()
			}
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
	m.pruneOldJobs()

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
// aiProvider is passed in rather than read from m.ai internally: the caller
// already decided to call this based on one read of the current provider,
// and re-reading m.ai here — four more times, across a network call to the
// model — could see UpdateAIProvider swap it mid-function otherwise (#43).
func (m *Manager) applyAIRename(job *Job, aiProvider ai.Provider) {
	cleaner := meta.NewCleaner(aiProvider)

	job.mu.RLock()
	sourcePath := job.SourcePath
	destPath := job.DestinationPath
	ctx := job.ctx
	job.mu.RUnlock()

	filename := filepath.Base(sourcePath)
	started := time.Now()

	cleanTitle, source, err := cleaner.CleanFilename(ctx, filename)
	if err != nil {
		m.appendAILog(job, AILog{
			Timestamp:  started,
			Operation:  "metadata_cleaning",
			Provider:   aiProvider.GetName(),
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
			Provider:   aiProvider.GetName(),
			Detail:     "Rename rejected — resolved outside the destination directory",
			DurationMs: time.Since(started).Milliseconds(),
			Success:    false,
			Error:      fmt.Sprintf("unsafe title: %q", cleanTitle),
		})
		return
	}

	log.Printf("[Job %s] Renamed via %s: %s -> %s", job.ID, source, filename, cleanTitle)
	job.mu.Lock()
	job.AICleaned = true
	job.DestinationPath = renamed
	job.mu.Unlock()

	m.appendAILog(job, AILog{
		Timestamp:  started,
		Operation:  "metadata_cleaning",
		Provider:   aiProvider.GetName(),
		Detail:     fmt.Sprintf("Renamed via %s: '%s' → '%s'", source, filename, cleanTitle),
		DurationMs: time.Since(started).Milliseconds(),
		Success:    true,
	})
}

func (m *Manager) isInScheduleWindow() bool {
	sched := m.config.Snapshot().Schedule
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
		m.updateJobProgress(job, func(j *Job) {
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

	// 7. Validate before trusting the extraction or deleting anything. MakeMKV
	// exits 0 even when a scratched disc or a drive read error truncates the
	// title partway through, and the bare size>0 check this replaces accepted
	// that as a complete extraction — which is how a source disc image has
	// been deleted while the only remaining copy was a truncated stub.
	verified := false
	m.updateJob(job, func(j *Job) { j.StatusDetail = "Validating" })
	if m.ffmpeg == nil {
		log.Printf("[Job %s] Warning: ffmpeg not available — cannot validate the extraction, source will be retained", job.ID)
		m.appendAILog(job, AILog{
			Timestamp: time.Now(),
			Operation: "output_validation",
			Provider:  "System",
			Detail:    "Validation could not run (ffmpeg unavailable) — output kept, source retained",
			Success:   false,
		})
	} else {
		expectedDuration := info.TitleDurationSeconds(mainTitleIdx)
		if valErr := m.ffmpeg.ValidateExtractedOutput(job.ctx, expectedDuration, finalPath); valErr != nil {
			log.Printf("[Job %s] Extraction output rejected: %v", job.ID, valErr)
			m.appendAILog(job, AILog{
				Timestamp: time.Now(),
				Operation: "output_validation",
				Provider:  "System",
				Detail:    "Extraction output rejected — see error",
				Success:   false,
				Error:     valErr.Error(),
			})
			m.discardOutput(job, finalPath, "failed extraction validation")
			return valErr
		}
		verified = true
		m.appendAILog(job, AILog{
			Timestamp: time.Now(),
			Operation: "output_validation",
			Provider:  "System",
			Detail:    "Extraction validated: duration and video stream within tolerance",
			Success:   true,
		})
	}

	// 8. Delete the source disc image only once the extraction is verified.
	if deleteSource {
		if verified {
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
			log.Printf("[Job %s] SKIPPING source deletion — extraction not verified", job.ID)
		}
	}

	return nil
}

// runOptimizationFromPath transcodes sourcePath and returns whether the
// output was verified — either by AI verification when it ran, or, when that
// is unavailable, by the deterministic ValidateOutput gate alone — plus any
// error. Callers that need to gate a second deletion of their own (the ISO
// auto-extract path deleting the original disc image, once the optimize step
// on the extracted MKV has run) use this returned value rather than
// re-deriving it, which is what previously let that second deletion run on
// its own, looser copy of the verification logic.
func (m *Manager) runOptimizationFromPath(job *Job, sourcePath string) (bool, error) {
	if m.ffmpeg == nil {
		return false, fmt.Errorf("ffmpeg wrapper not initialized")
	}

	// One snapshot for this job's whole run, rather than re-reading m.config
	// (and m.ai) at each of the ~15 points below that need a setting. A job
	// can run for hours; POST /api/config changing CRF or swapping the AI
	// provider midway through must not be able to tear what a single
	// encode's decisions are based on (#43).
	cfg := m.config.Snapshot()
	aiProvider := m.GetAI()

	log.Printf("[Job %s] Starting optimization: %s", job.ID, sourcePath)

	// A zero-byte source — a broken download or a placeholder left by an
	// external tool — makes ffprobe fail with a bare "exit status 1" that
	// tells the operator nothing; catch it first with a message that does.
	// This is a plain failure, not a skip: the file isn't done, and once it
	// is actually populated a later scan retries it (the in-flight marker is
	// cleared on failure, #48).
	if err := media.CheckSourceFile(sourcePath); err != nil {
		log.Printf("[Job %s] %v", job.ID, err)
		m.appendAILog(job, AILog{
			Timestamp: time.Now(),
			Operation: "source_rejected",
			Provider:  "System",
			Detail:    err.Error(),
			Success:   false,
			Error:     err.Error(),
		})
		return false, err
	}

	// 1. Probe the source. Everything downstream — encoder profile, colour
	// signalling, and the output validation gate — is derived from this.
	info, err := m.ffmpeg.GetMediaInfo(job.ctx, sourcePath)
	if err != nil {
		log.Printf("[Job %s] Error getting media info: %v", job.ID, err)
		return false, fmt.Errorf("failed to get media info: %w", err)
	}

	log.Printf("[Job %s] Source: %.2fs, %dx%d, %s (%d-bit), transfer=%q, DV profile=%d",
		job.ID, info.Duration, info.VideoWidth, info.VideoHeight,
		info.PixFmt, info.BitDepth, info.ColorTransfer, info.DVProfile)

	// Reject inputs this pipeline cannot encode correctly, and skip sources
	// that are already an efficient HEVC/AV1 encode — re-encoding either
	// produces a colour-shifted Dolby Vision profile 5 output, or is
	// generational loss for no size benefit. The two are distinguished by
	// error type: a skip is not a failure, and completes the job rather than
	// retrying or failing it.
	if err := media.CheckSourceSupported(info, cfg.DensityFloor); err != nil {
		var skipErr *media.SkipEncodeError
		if errors.As(err, &skipErr) {
			log.Printf("[Job %s] %s", job.ID, skipErr.Reason)
			m.appendAILog(job, AILog{
				Timestamp: time.Now(),
				Operation: "source_skipped",
				Provider:  "System",
				Detail:    skipErr.Reason,
				Success:   true,
			})
			m.updateJob(job, func(j *Job) { j.StatusDetail = skipErr.Reason })
			return false, nil
		}
		log.Printf("[Job %s] %v", job.ID, err)
		m.appendAILog(job, AILog{
			Timestamp: time.Now(),
			Operation: "source_rejected",
			Provider:  "System",
			Detail:    err.Error(),
			Success:   false,
			Error:     err.Error(),
		})
		return false, err
	}
	if info.IsDolbyVision() {
		log.Printf("[Job %s] Note: Dolby Vision profile %d — encoding the HDR10 base layer; "+
			"Dolby Vision metadata will not survive the re-encode", job.ID, info.DVProfile)
	}

	// 2. Premium Feature: AI Adaptive Encoding
	crf := cfg.CRF
	defaultCRF := crf
	if cfg.IsPremium && aiProvider != nil && !cfg.OverrideAICRF {
		cleaner := meta.NewCleaner(aiProvider)
		log.Printf("[Premium] AI analyzing media for optimal encoding settings...")
		t0Enc := time.Now()
		// Pass a short summary rather than the full ffprobe dump: for a UHD
		// REMUX with 48 streams the raw JSON is tens of kilobytes that bury the
		// few facts the decision actually turns on.
		//
		// job.ctx alone carries no deadline — only cancellation — so an
		// unresponsive provider would otherwise stall this worker for as long
		// as the shared HTTP client's own timeout allows. This suggestion is
		// optional (the fallback below is the configured CRF, already a good
		// answer), so it gets a much shorter bound of its own rather than
		// borrowing that general-purpose one (#49).
		analyzeCtx, cancelAnalyze := context.WithTimeout(job.ctx, 30*time.Second)
		suggestedCRF, err := cleaner.AnalyzeEncoding(analyzeCtx, info.EncodingSummary())
		cancelAnalyze()
		if err == nil {
			// A suggestion more indulgent than the configured default on a
			// source already in this pipeline's target codec is how a REMUX
			// got re-encoded at CRF 20 — near-transparent — and inflated
			// instead of shrunk. Refuse it rather than trust it blindly.
			if media.ShouldRefuseCRFSuggestion(info.CodecName, suggestedCRF, defaultCRF) {
				log.Printf("[Job %s] Ignoring AI CRF suggestion %d — more indulgent than the configured default %d on an already-%s source",
					job.ID, suggestedCRF, defaultCRF, strings.ToUpper(info.CodecName))
				m.appendAILog(job, AILog{
					Timestamp: t0Enc,
					Operation: "encoding_analysis",
					Provider:  aiProvider.GetName(),
					Detail: fmt.Sprintf("Suggested CRF %d refused — more indulgent than the configured default %d on an already-%s source",
						suggestedCRF, defaultCRF, strings.ToUpper(info.CodecName)),
					DurationMs: time.Since(t0Enc).Milliseconds(),
					Success:    true,
				})
			} else {
				log.Printf("[Job %s] AI suggested CRF %d (system default %d)", job.ID, suggestedCRF, defaultCRF)
				crf = suggestedCRF
				m.appendAILog(job, AILog{
					Timestamp:  t0Enc,
					Operation:  "encoding_analysis",
					Provider:   aiProvider.GetName(),
					Detail:     fmt.Sprintf("Suggested CRF %d (system default: %d)", suggestedCRF, defaultCRF),
					DurationMs: time.Since(t0Enc).Milliseconds(),
					Success:    true,
				})
			}
		} else {
			// Not an error condition: the configured CRF is a perfectly good
			// answer, and this path is taken whenever the model declines to
			// produce a usable number. Logged at info level so it stops looking
			// like a fault in the logs.
			log.Printf("[Job %s] No AI CRF suggestion, using configured CRF %d (%v)", job.ID, crf, err)
			m.appendAILog(job, AILog{
				Timestamp:  t0Enc,
				Operation:  "encoding_analysis",
				Provider:   aiProvider.GetName(),
				Detail:     fmt.Sprintf("No suggestion available — using configured CRF %d", crf),
				DurationMs: time.Since(t0Enc).Milliseconds(),
				Success:    true,
				Error:      err.Error(),
			})
		}
	} else if cfg.IsPremium && aiProvider != nil && cfg.OverrideAICRF {
		log.Printf("[Job %s] AI CRF override enabled — using configured CRF %d", job.ID, crf)
		m.appendAILog(job, AILog{
			Timestamp: time.Now(),
			Operation: "encoding_analysis",
			Provider:  "System",
			Detail:    fmt.Sprintf("AI CRF override enabled — using configured CRF %d", crf),
			Success:   true,
		})
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
	replacing := cfg.ReplaceInPlace && cfg.HoldingDir != ""
	var replacement replacementPaths
	if replacing {
		replacement = planReplacement(sourcePath, job.ID)
		destPath = replacement.Temp
		defer m.cleanupTemp(job, replacement.Temp)
		log.Printf("[Job %s] Replace-in-place: writing %s, will become %s",
			job.ID, filepath.Base(replacement.Temp), filepath.Base(replacement.Final))
	} else if cfg.ReplaceInPlace {
		log.Printf("[Job %s] REPLACE_IN_PLACE is set but HOLDING_DIR is empty — "+
			"writing to the configured destination instead. Replacement needs somewhere "+
			"to retain the original.", job.ID)
	}

	if sourcePath == destPath {
		return false, fmt.Errorf("source and destination paths are identical (%s): FFmpeg cannot encode a file in-place", sourcePath)
	}

	opts := media.TranscodeOptions{
		InputPath:  sourcePath,
		OutputPath: destPath,
		GPUVendor:  media.GPUVendor(cfg.GPUVendor),
		Preset:     media.QualityPreset(cfg.QualityPreset),
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
		m.updateJobProgress(job, func(j *Job) {
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
		return false, err
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
		return false, valErr
	}
	m.appendAILog(job, AILog{
		Timestamp:  t0Val,
		Operation:  "output_validation",
		Provider:   "System",
		Detail:     "Output validated: duration, streams and size within tolerance",
		DurationMs: time.Since(t0Val).Milliseconds(),
		Success:    true,
	})

	// A valid output is not necessarily a worthwhile one. Below the savings
	// floor, replacing the source would be a regression, not an optimisation
	// — discard the output and keep the original rather than reporting success.
	if outInfo, statErr := os.Stat(destPath); statErr == nil &&
		!media.MeetsSavingsFloor(info.Size, outInfo.Size(), cfg.SavingsFloor) {
		savingsPct := (1 - float64(outInfo.Size())/float64(info.Size)) * 100
		detail := fmt.Sprintf("Output only %.1f%% smaller than source (floor %.0f%%) — kept original",
			savingsPct, cfg.SavingsFloor*100)
		log.Printf("[Job %s] %s", job.ID, detail)
		m.appendAILog(job, AILog{
			Timestamp: time.Now(),
			Operation: "savings_floor",
			Provider:  "System",
			Detail:    detail,
			Success:   false,
		})
		m.discardOutput(job, destPath, detail)
		m.updateJob(job, func(j *Job) { j.StatusDetail = detail })
		return false, nil
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
	subtitleMode := cfg.SubtitleMode
	shouldDownload := subtitleMode == "always" || (subtitleMode == "selective" && createSubtitles)
	if shouldDownload && cfg.SubtitleAPIKey != "" {
		log.Printf("[Subtitles] Attempting subtitle download for: %s", filepath.Base(destPath))
		dl := subtitles.NewDownloader(
			cfg.SubtitleAPIKey,
			cfg.SubtitleUsername,
			cfg.SubtitlePassword,
			cfg.SubtitleLang,
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
					Detail:     fmt.Sprintf("Downloaded %s subtitles (OpenSubtitles)", cfg.SubtitleLang),
					DurationMs: time.Since(t0Sub).Milliseconds(),
					Success:    true,
				})
			}
		}
	}

	// 4. Premium Feature: AI Video Verification (Safe Delete)
	verified := false
	if cfg.IsPremium && verifyOutput && aiProvider != nil {
		log.Printf("[Premium] Verifying video integrity with AI...")
		m.updateJob(job, func(j *Job) {
			j.StatusDetail = "Verifying"
		})

		t0Ver := time.Now()
		if vOk, vErr := m.runVerificationFromPaths(job, sourcePath, destPath, aiProvider); vErr != nil {
			// The check could not be run. Treat that as inconclusive rather than
			// as a pass: the deterministic gate above already cleared the output,
			// so keep it and complete the job, but leave Verified false so the
			// source is not deleted.
			log.Printf("[Job %s] Warning: AI verification could not run: %v", job.ID, vErr)
			m.appendAILog(job, AILog{
				Timestamp:  t0Ver,
				Operation:  "verification",
				Provider:   aiProvider.GetName(),
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
				Provider:   aiProvider.GetName(),
				Detail:     "FAIL — corruption detected",
				DurationMs: time.Since(t0Ver).Milliseconds(),
				Success:    false,
			})
			m.discardOutput(job, destPath, "failed AI verification")
			return false, fmt.Errorf("AI verification failed: corruption detected in output")
		} else {
			log.Printf("[Premium] SUCCESS: Video integrity verified by AI.")
			verified = true
			m.updateJob(job, func(j *Job) {
				j.Verified = true
			})
			m.appendAILog(job, AILog{
				Timestamp:  t0Ver,
				Operation:  "verification",
				Provider:   aiProvider.GetName(),
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
			return false, fmt.Errorf("replacement skipped: output could not be verified")
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
			return false, err
		}

		// The job's real output is the promoted file, not the temp path.
		m.updateJob(job, func(j *Job) { j.DestinationPath = replacement.Final })

		// The original has been moved to holding, not deleted — deleteSource is
		// not the mechanism here and must not also run.
		return true, nil
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

	return verified, nil
}

func (m *Manager) runOptimization(job *Job) (bool, error) {
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

func (m *Manager) runVerificationFromPaths(job *Job, srcPath, destPath string, aiProvider ai.Provider) (bool, error) {
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

	return aiProvider.VerifyMedia(job.ctx, srcFrames, destFrames)
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
			m.updateJobProgress(job, func(j *Job) {
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
		job.markQueued()
		heap.Push(&m.pq, job)
	}
	m.pqMu.Unlock()
	m.pqCond.Broadcast()

	log.Printf("Requeued %d pending jobs", len(pending))
}
