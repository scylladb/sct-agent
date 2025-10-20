package executor

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/scylladb/sct-agent/internal/storage"
)

type Executor struct {
	maxConcurrent int
	semaphore     chan struct{}
	storage       storage.Storage
	mutex         sync.RWMutex
	cancelFuncs   map[string]context.CancelFunc
}

func NewExecutor(maxConcurrent int, storage storage.Storage) *Executor {
	return &Executor{
		maxConcurrent: maxConcurrent,
		semaphore:     make(chan struct{}, maxConcurrent),
		storage:       storage,
		cancelFuncs:   make(map[string]context.CancelFunc),
	}
}

func (e *Executor) Execute(req *storage.ExecuteRequest) (*storage.Job, error) {
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 1800
	}

	priority := req.Priority
	if priority == "" {
		priority = "normal"
	}

	job := &storage.Job{
		ID:         uuid.New().String(),
		Command:    req.Command,
		Args:       req.Args,
		WorkingDir: req.WorkingDir,
		Env:        req.Env,
		Timeout:    timeout,
		Priority:   priority,
		Tags:       req.Tags,
		Status:     storage.StatusQueued,
		CreatedAt:  time.Now(),
	}

	if err := e.storage.Save(job); err != nil {
		return nil, fmt.Errorf("failed to save job: %w", err)
	}

	go e.executeJob(job)

	return job, nil
}

func (e *Executor) GetJob(id string) (*storage.Job, error) {
	if job, exists := e.storage.Get(id); exists {
		return job, nil
	}
	return nil, fmt.Errorf("job not found")
}

func (e *Executor) CancelJob(id string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	job, exists := e.storage.Get(id)
	if !exists {
		return fmt.Errorf("job not found")
	}

	if job.Status != storage.StatusQueued && job.Status != storage.StatusRunning {
		return fmt.Errorf("job cannot be cancelled (status: %s)", job.Status)
	}

	if cancelFunc, exists := e.cancelFuncs[id]; exists {
		cancelFunc()
		delete(e.cancelFuncs, id)
	}

	now := time.Now()
	job.Status = storage.StatusCancelled
	job.CompletedAt = &now
	if job.StartedAt != nil {
		job.DurationMs = time.Since(*job.StartedAt).Milliseconds()
	}

	return e.storage.Save(job)
}

func (e *Executor) ListJobs(status storage.JobStatus, limit, offset int, since *time.Time) ([]*storage.Job, int, error) {
	return e.storage.List(status, limit, offset, since)
}

func (e *Executor) GetStats() map[string]int {
	return map[string]int{
		"total":     e.storage.Count(),
		"queued":    e.storage.CountByStatus(storage.StatusQueued),
		"running":   e.storage.CountByStatus(storage.StatusRunning),
		"completed": e.storage.CountByStatus(storage.StatusCompleted),
		"failed":    e.storage.CountByStatus(storage.StatusFailed),
		"cancelled": e.storage.CountByStatus(storage.StatusCancelled),
	}
}

func (e *Executor) executeJob(job *storage.Job) {
	e.semaphore <- struct{}{}
	defer func() { <-e.semaphore }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(job.Timeout)*time.Second)
	defer cancel()

	e.mutex.Lock()
	e.cancelFuncs[job.ID] = cancel
	e.mutex.Unlock()

	defer func() {
		e.mutex.Lock()
		delete(e.cancelFuncs, job.ID)
		e.mutex.Unlock()
	}()

	now := time.Now()
	job.Status = storage.StatusRunning
	job.StartedAt = &now
	e.storage.Save(job)

	e.runCommand(ctx, job)

	completedAt := time.Now()
	job.CompletedAt = &completedAt
	job.DurationMs = completedAt.Sub(*job.StartedAt).Milliseconds()

	e.storage.Save(job)
}

func (e *Executor) runCommand(ctx context.Context, job *storage.Job) {
	cmd := exec.CommandContext(ctx, job.Command, job.Args...)

	if job.WorkingDir != "" {
		cmd.Dir = job.WorkingDir
	}

	if len(job.Env) > 0 {
		cmd.Env = make([]string, 0, len(job.Env))
		for key, value := range job.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	var stdout, stderr []byte
	var err error

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		job.Status = storage.StatusFailed
		job.Error = fmt.Sprintf("failed to create stdout pipe: %v", err)
		exitCode := -1
		job.ExitCode = &exitCode
		return
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		job.Status = storage.StatusFailed
		job.Error = fmt.Sprintf("failed to create stderr pipe: %v", err)
		exitCode := -1
		job.ExitCode = &exitCode
		return
	}

	if err := cmd.Start(); err != nil {
		job.Status = storage.StatusFailed
		job.Error = fmt.Sprintf("failed to start command: %v", err)
		exitCode := -1
		job.ExitCode = &exitCode
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		stdout, _ = io.ReadAll(stdoutPipe)
	}()

	go func() {
		defer wg.Done()
		stderr, _ = io.ReadAll(stderrPipe)
	}()

	wg.Wait()

	err = cmd.Wait()

	job.Stdout = string(stdout)
	job.Stderr = string(stderr)

	if err != nil {
		job.Status = storage.StatusFailed
		job.Error = err.Error()

		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode := exitError.ExitCode()
			job.ExitCode = &exitCode
		} else {
			exitCode := -1
			job.ExitCode = &exitCode
		}
	} else {
		job.Status = storage.StatusCompleted
		exitCode := 0
		job.ExitCode = &exitCode
	}

	if ctx.Err() == context.DeadlineExceeded {
		job.Status = storage.StatusFailed
		job.Error = "command timed out"
		if job.Stderr == "" {
			job.Stderr = "Command execution timed out"
		}
	} else if ctx.Err() == context.Canceled {
		job.Status = storage.StatusCancelled
		job.Error = "command cancelled"
		if job.Stderr == "" {
			job.Stderr = "Command execution cancelled"
		}
	}
}

func (e *Executor) Shutdown(ctx context.Context) error {
	e.mutex.Lock()
	for _, cancel := range e.cancelFuncs {
		cancel()
	}
	e.mutex.Unlock()

	// wait for all jobs to complete or timeout
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if e.storage.CountByStatus(storage.StatusRunning) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
