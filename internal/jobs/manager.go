package jobs

import (
	"context"
	"log"
	"os/exec"
	"sync"
	"time"
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
}

func NewManager(maxConcurrent int) *Manager {
	return &Manager{
		jobs:          make(map[string]*Job),
		queue:         make(chan *Job, 1000),
		maxConcurrent: maxConcurrent,
		stopCh:        make(chan struct{}),
	}
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
	// TODO: Implement MakeMKV extraction
	log.Printf("[Job %s] Running extraction: %s", job.ID, job.SourcePath)
	return nil
}

func (m *Manager) runOptimization(job *Job) error {
	// TODO: Implement FFmpeg optimization
	log.Printf("[Job %s] Running optimization: %s", job.ID, job.SourcePath)
	return nil
}
