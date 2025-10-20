package storage

import (
	"time"
)

type JobStatus string

const (
	StatusQueued    JobStatus = "queued"
	StatusRunning   JobStatus = "running"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
)

type Job struct {
	ID          string            `json:"job_id"`
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Timeout     int               `json:"timeout,omitempty"`
	Priority    string            `json:"priority,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Status      JobStatus         `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	ExitCode    *int              `json:"exit_code,omitempty"`
	Stdout      string            `json:"stdout,omitempty"`
	Stderr      string            `json:"stderr,omitempty"`
	Error       string            `json:"error,omitempty"`
	DurationMs  int64             `json:"duration_ms,omitempty"`
}

type ExecuteRequest struct {
	Command    string            `json:"command" binding:"required"`
	Args       []string          `json:"args"`
	WorkingDir string            `json:"working_dir"`
	Env        map[string]string `json:"env"`
	Timeout    int               `json:"timeout"`
	Priority   string            `json:"priority"`
	Tags       map[string]string `json:"tags"`
}

type ExecuteResponse struct {
	JobID     string    `json:"job_id"`
	Status    JobStatus `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	Command   string    `json:"command"`
	Message   string    `json:"message"`
}

type JobListResponse struct {
	Commands []Job `json:"commands"`
	Total    int   `json:"total"`
	Limit    int   `json:"limit"`
	Offset   int   `json:"offset"`
}

type HealthResponse struct {
	Status        string                 `json:"status"`
	Version       string                 `json:"version"`
	UptimeSeconds int64                  `json:"uptime_seconds"`
	RunningJobs   int                    `json:"running_jobs"`
	CompletedJobs int                    `json:"completed_jobs"`
	System        map[string]interface{} `json:"system,omitempty"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

type Storage interface {
	Save(job *Job) error
	Get(id string) (*Job, bool)
	List(status JobStatus, limit, offset int, since *time.Time) ([]*Job, int, error)
	Delete(id string) error
	Count() int
	CountByStatus(status JobStatus) int
}
