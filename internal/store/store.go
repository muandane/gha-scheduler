package store

import (
	"context"
	"time"
)

// Status is the job lifecycle state persisted in the store.
type Status string

const (
	StatusQueued        Status = "queued"
	StatusDispatching   Status = "dispatching"
	StatusDispatched    Status = "dispatched"
	StatusScheduled     Status = "scheduled"
	StatusRunning       Status = "running"
	StatusSucceeded     Status = "succeeded"
	StatusFailed        Status = "failed"
	StatusDispatchError Status = "dispatch_error"
)

// RunnerSpecSnapshot captures resolved labelquery fields at dispatch time.
type RunnerSpecSnapshot struct {
	CPU          int
	Arch         string
	Pool         string
	CacheEnabled bool
	LabelsJSON   string
}

// Job is a persisted workflow job record.
type Job struct {
	JobID              string
	RunID              string
	Owner              string
	Repo               string
	Status             Status
	WebhookAt          time.Time
	DispatchAt         time.Time
	JobCreatedAt       time.Time
	ScheduledAt        time.Time
	RunningAt          time.Time
	CompletedAt        time.Time
	DispatchLatencySec float64
	ScheduleLatencySec float64
	JobDurationSec     float64
	CPU                int
	Arch               string
	Pool               string
	CacheEnabled       bool
	LabelsJSON         string
	ExitCode           *int
	PodName            string
	DispatchError      string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// TimelinePhase is one step in a job detail response.
type TimelinePhase struct {
	Name string    `json:"name"`
	At   time.Time `json:"at"`
}

// ListQuery filters job list results.
type ListQuery struct {
	Repo   string
	Status Status
	Limit  int
	Cursor string
}

// ListResult is a paginated job list.
type ListResult struct {
	Jobs       []Job
	NextCursor string
}

// Stats holds operational aggregates for the console header.
type Stats struct {
	DispatchP50       float64 `json:"dispatch_p50"`
	DispatchP95       float64 `json:"dispatch_p95"`
	ScheduleP50       float64 `json:"schedule_p50"`
	ScheduleP95       float64 `json:"schedule_p95"`
	DispatchErrors24h int64   `json:"dispatch_errors_24h"`
	ActiveJobs        int64   `json:"active_jobs"`
	CompletedJobs     int64   `json:"completed_jobs"`
}

// JobStore persists scheduler job lifecycle data.
type JobStore interface {
	UpsertQueued(ctx context.Context, owner, repo, runID, jobID string, at time.Time) error
	MarkDispatching(ctx context.Context, jobID string, spec RunnerSpecSnapshot, at time.Time) error
	MarkJobCreated(ctx context.Context, jobID string, at time.Time) error
	MarkScheduled(ctx context.Context, jobID, podName string, at time.Time) error
	MarkRunning(ctx context.Context, jobID string, at time.Time) error
	MarkCompleted(ctx context.Context, jobID string, exitCode int, at time.Time) error
	MarkDispatchError(ctx context.Context, jobID, reason string, at time.Time) error

	GetJob(ctx context.Context, jobID string) (*Job, error)
	ListJobs(ctx context.Context, q ListQuery) (ListResult, error)
	Stats(ctx context.Context, since time.Time) (Stats, error)
	Prune(ctx context.Context, before time.Time) (int64, error)

	Close() error
}
