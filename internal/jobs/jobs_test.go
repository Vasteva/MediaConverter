package jobs

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Vasteva/MediaConverter/internal/config"
)

func TestManager_AddAndGetJob(t *testing.T) {
	cfg := &config.Config{MaxConcurrentJobs: 2}
	mgr, _ := NewManager(cfg, nil, "")

	job := &Job{
		ID:         "test-job-1",
		Type:       JobTypeTest,
		SourcePath: "/tmp/source.mkv",
		Status:     StatusPending,
	}

	mgr.AddJob(job)

	retrieved := mgr.GetJob("test-job-1")
	if retrieved == nil {
		t.Fatal("expected to retrieve job, got nil")
	}
	if retrieved.ID != job.ID {
		t.Errorf("expected job ID %s, got %s", job.ID, retrieved.ID)
	}

	all := mgr.GetAllJobs()
	if len(all) != 1 {
		t.Errorf("expected 1 job in list, got %d", len(all))
	}
}

func TestManager_CancelJob(t *testing.T) {
	cfg := &config.Config{MaxConcurrentJobs: 2}
	mgr, _ := NewManager(cfg, nil, "")

	job := &Job{
		ID:     "test-cancel",
		Type:   JobTypeTest,
		Status: StatusPending,
	}
	job.ctx, job.cancel = context.WithCancel(context.Background())

	mgr.AddJob(job)

	// Test cancelling before it starts
	success := mgr.CancelJob("test-cancel")
	if !success {
		t.Error("expected CancelJob to return true")
	}

	if got := job.GetStatus(); got != StatusCancelled {
		t.Errorf("expected status Cancelled, got %s", got)
	}
}

func TestManager_Lifecycle(t *testing.T) {
	cfg := &config.Config{MaxConcurrentJobs: 1}
	mgr, _ := NewManager(cfg, nil, "")

	mgr.Start()

	job := &Job{
		ID:     "test-lifecycle",
		Type:   JobTypeTest,
		Status: StatusPending,
	}

	mgr.AddJob(job)

	// Wait a bit for the worker to pick it up and process
	// JobTypeTest in runTest waits 10 seconds, which is too long for unit tests.
	// But we can check if it's no longer pending or wait a very short time.

	// Actually, let's verify it gets picked up
	time.Sleep(100 * time.Millisecond)

	// Status is guarded by job.mu, not by the manager lock — reading it under
	// mgr.mu was the data race the detector reported here.
	if status := job.GetStatus(); status == StatusPending {
		t.Errorf("expected job to be picked up by worker (status Processing or later), but it is still Pending")
	}

	mgr.Stop()
}

// mustNotBlock fails the test if fn has not returned within d.
//
// A deadlocked call never returns, so without this the test would hang until
// the whole package timed out ten minutes later, reported as an unrelated
// panic. This turns it into a named failure in seconds.
func mustNotBlock(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %s — deadlocked", what, d)
	}
}

// TestCancelJobDoesNotDeadlock covers a self-deadlock on the manager mutex.
//
// CancelJob held m.mu.Lock() and then called Save(), which takes m.mu.RLock().
// sync.RWMutex is not reentrant, so the goroutine blocked against a lock it
// already held and never released it — wedging every later GetAllJobs, AddJob
// and Save behind it, until the process was restarted. It is wired to the
// Cancel button, so it triggered on first use.
func TestCancelJobDoesNotDeadlock(t *testing.T) {
	mgr, err := NewManager(&config.Config{MaxConcurrentJobs: 1}, nil, "")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.AddJob(&Job{ID: "cancel-me", Type: JobTypeTest, Status: StatusPending})

	var cancelled bool
	mustNotBlock(t, 5*time.Second, "CancelJob", func() {
		cancelled = mgr.CancelJob("cancel-me")
	})
	if !cancelled {
		t.Error("CancelJob returned false for a job that exists")
	}
	if got := mgr.GetJob("cancel-me").GetStatus(); got != StatusCancelled {
		t.Errorf("status = %s, want %s", got, StatusCancelled)
	}

	// The mutex must still be usable afterwards.
	mustNotBlock(t, 5*time.Second, "GetAllJobs after CancelJob", func() {
		mgr.GetAllJobs()
	})
}

// PurgeJobs had the same defect, reached from the Clear Failed button.
func TestPurgeJobsDoesNotDeadlock(t *testing.T) {
	mgr, err := NewManager(&config.Config{MaxConcurrentJobs: 1}, nil, "")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.AddJob(&Job{ID: "failed-1", Type: JobTypeTest, Status: StatusFailed})
	mgr.AddJob(&Job{ID: "failed-2", Type: JobTypeTest, Status: StatusFailed})
	mgr.AddJob(&Job{ID: "ok-1", Type: JobTypeTest, Status: StatusCompleted})

	var purged int
	mustNotBlock(t, 5*time.Second, "PurgeJobs", func() {
		purged = mgr.PurgeJobs(StatusFailed)
	})
	if purged != 2 {
		t.Errorf("purged %d jobs, want 2", purged)
	}

	mustNotBlock(t, 5*time.Second, "GetAllJobs after PurgeJobs", func() {
		if n := len(mgr.GetAllJobs()); n != 1 {
			t.Errorf("%d jobs remain, want 1", n)
		}
	})
}

// TestConcurrentSerializationIsRaceFree reproduces the reported data race:
// a worker mutating job fields while another goroutine serialises the job.
//
// In production the writer is the FFmpeg progress callback, firing several
// times a second, and the readers are Manager.Save, the /api/jobs handler and
// the SSE broadcaster. Under -race this fails without the lock in
// Job.MarshalJSON; it passes trivially without -race, which is why CI never
// caught it.
func TestConcurrentSerializationIsRaceFree(t *testing.T) {
	mgr, err := NewManager(&config.Config{MaxConcurrentJobs: 1}, nil, "")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	job := &Job{ID: "racy", Type: JobTypeTest, Status: StatusPending}
	mgr.AddJob(job)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer: the progress-callback pattern.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			mgr.updateJob(job, func(j *Job) {
				j.Progress = i % 100
				j.FPS = float64(i)
				j.ETA = "00:00:01"
				j.Status = StatusProcessing
			})
		}
	}()

	// Readers: the three production serialisation paths.
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := json.Marshal(job); err != nil {
					t.Errorf("Marshal: %v", err)
					return
				}
				for _, j := range mgr.GetAllJobs() {
					_, _ = json.Marshal(j)
				}
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// Serialising under the lock must not change the JSON.
func TestMarshalJSONPreservesFields(t *testing.T) {
	job := &Job{
		ID:              "j1",
		Type:            JobTypeOptimize,
		SourcePath:      "/storage/in.mkv",
		DestinationPath: "/output/out.mkv",
		Status:          StatusProcessing,
		Progress:        42,
		FPS:             23.976,
		InputSize:       1000,
		OutputSize:      500,
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	for key, want := range map[string]interface{}{
		"id":              "j1",
		"type":            "optimize",
		"sourcePath":      "/storage/in.mkv",
		"destinationPath": "/output/out.mkv",
		"status":          "processing",
		"progress":        float64(42),
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}

	// The mutex must not leak into the output.
	if _, ok := got["mu"]; ok {
		t.Error("serialised output contains the mutex field")
	}

	// A round trip must still work — Load depends on it.
	var back Job
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	// Print fields individually: formatting the struct itself would copy its
	// mutex, which go vet's copylocks check rejects.
	if back.ID != job.ID || back.Status != job.Status || back.Progress != job.Progress {
		t.Errorf("round trip lost data: id=%q status=%q progress=%d",
			back.ID, back.Status, back.Progress)
	}
}

// TestCancelledJobIsNotProcessed covers the defect that made cancelling a
// queued job a no-op.
//
// CancelJob marks the job but cannot remove it from the priority heap. Without
// a check in processJob the worker popped it and overwrote the status straight
// back to processing, so the job ran anyway and the UI showed it flip from
// cancelled back to processing.
func TestCancelledJobIsNotProcessed(t *testing.T) {
	mgr, err := NewManager(&config.Config{MaxConcurrentJobs: 1}, nil, "")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	job := &Job{ID: "cancelled-before-start", Type: JobTypeTest, Status: StatusPending}
	mgr.AddJob(job)

	if !mgr.CancelJob(job.ID) {
		t.Fatal("CancelJob returned false")
	}

	// Start workers only now, so the job is guaranteed to be cancelled before
	// any worker can pop it.
	mgr.Start()
	defer mgr.Stop()
	time.Sleep(300 * time.Millisecond)

	if got := job.GetStatus(); got != StatusCancelled {
		t.Errorf("status = %s, want it to stay %s — the worker processed a cancelled job",
			got, StatusCancelled)
	}
	job.mu.RLock()
	started := job.StartedAt
	job.mu.RUnlock()
	if !started.IsZero() {
		t.Error("StartedAt was set, so processJob ran on a cancelled job")
	}
}

func TestCancelAllActive(t *testing.T) {
	mgr, err := NewManager(&config.Config{MaxConcurrentJobs: 1}, nil, "")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	mgr.AddJob(&Job{ID: "pending-1", Type: JobTypeTest, Status: StatusPending})
	mgr.AddJob(&Job{ID: "pending-2", Type: JobTypeTest, Status: StatusPending})
	mgr.AddJob(&Job{ID: "processing-1", Type: JobTypeTest, Status: StatusProcessing})
	mgr.AddJob(&Job{ID: "completed-1", Type: JobTypeTest, Status: StatusCompleted})
	mgr.AddJob(&Job{ID: "failed-1", Type: JobTypeTest, Status: StatusFailed})

	var cancelled int
	mustNotBlock(t, 5*time.Second, "CancelAllActive", func() {
		cancelled = mgr.CancelAllActive()
	})

	if cancelled != 3 {
		t.Errorf("cancelled %d, want 3 (two pending, one processing)", cancelled)
	}

	// History must be left intact — this stops work, it does not clear records.
	for id, want := range map[string]Status{
		"pending-1":    StatusCancelled,
		"pending-2":    StatusCancelled,
		"processing-1": StatusCancelled,
		"completed-1":  StatusCompleted,
		"failed-1":     StatusFailed,
	} {
		if got := mgr.GetJob(id).GetStatus(); got != want {
			t.Errorf("%s: status = %s, want %s", id, got, want)
		}
	}

	if n := len(mgr.GetAllJobs()); n != 5 {
		t.Errorf("%d jobs remain, want all 5 — cancelling must not delete records", n)
	}

	// Calling it again with nothing active is a no-op.
	if again := mgr.CancelAllActive(); again != 0 {
		t.Errorf("second call cancelled %d, want 0", again)
	}
}

// Cancelling in bulk must notify subscribers for each job, so the SSE stream
// and the UI reflect every change rather than only the last.
func TestCancelAllActiveNotifiesPerJob(t *testing.T) {
	mgr, err := NewManager(&config.Config{MaxConcurrentJobs: 1}, nil, "")
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	var mu sync.Mutex
	notified := map[string]int{}
	mgr.OnJobUpdate = func(j *Job) {
		mu.Lock()
		notified[j.ID]++
		mu.Unlock()
	}

	mgr.AddJob(&Job{ID: "a", Type: JobTypeTest, Status: StatusPending})
	mgr.AddJob(&Job{ID: "b", Type: JobTypeTest, Status: StatusPending})

	mu.Lock()
	notified = map[string]int{} // discard the AddJob notifications
	mu.Unlock()

	mgr.CancelAllActive()

	mu.Lock()
	defer mu.Unlock()
	for _, id := range []string{"a", "b"} {
		if notified[id] == 0 {
			t.Errorf("no update broadcast for job %s", id)
		}
	}
}
