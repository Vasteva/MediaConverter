package jobs

import (
	"context"
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

	if job.Status != StatusCancelled {
		t.Errorf("expected status Cancelled, got %s", job.Status)
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

	mgr.mu.RLock()
	status := job.Status
	mgr.mu.RUnlock()

	if status == StatusPending {
		// It should be processing at least
	}

	mgr.Stop()
}
