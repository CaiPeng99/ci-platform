package store

import (
	"context"
	"time"
)

type Run struct {
	ID int64
	Repo string
	Ref string
	CommitSHA string
	Branch string
	Trigger string
	Status string
	CreatedAt time.Time
	StartedAt *time.Time
	FinishedAt *time.Time
	ErrorMessage *string
	
}

type Job struct {
	ID int64
	RunID int64
	Name string
	RunsOn string
	Status string
	Needs []string
	Attempts int
	MaxAttempts int
	RunnerID *string
	LeaseExpiresAt time.Time     // ✅ NOT *time.Time
	CreatedAt      time.Time
	StartedAt      time.Time     // ✅ NOT *time.Time
	FinishedAt     time.Time     // ✅ NOT *time.Time
	ErrorMessage *string
}

type Step struct {
	ID int64
	JobID int64
	Idx int
	Name string
	Status string
	Command string
	ExitCode *int
	CreatedAt time.Time
	StartedAt *time.Time
	FinishedAt *time.Time
}

type JobSpec struct {
	JobID int64
	RunID int64
	Repo string
	Ref string
	CommitSHA string
	JobName string
	Steps []StepSpec
}

type StepSpec struct {
	StepID int64
	Name string
	Command string
}

type LogChunk struct {
	ID int64
	StepID *int64
	JobID int64
	Content string
	Timestamp time.Time
}

type Store interface {
	// Run related methods
	CreateRun(ctx context.Context, repo, ref, sha, trigger string) (int64, error)
	MarkRunRunning(ctx context.Context, runID int64) error
	MarkRunFinished(ctx context.Context, runID int64, status string, errorMessage *string) error

	// Job/Steps creation
	CreateJob(ctx context.Context, runID int64, name string, needs []string, runsOn string, maxAttempts int) (int64, error)
	CreateStep(ctx context.Context, jobID int64, idx int, name, command string) (int64, error)

	// scheduler state
	MarkJobQueued(ctx context.Context, jobID int64) error

	// Leasing (runner)
	UpsertRunnerHeartbeat(ctx context.Context, runnerID string, labels []string) error

	// Atomically claim a job for a runner; returns full job spec(including steps) or nil if no job is available
	LeaseJobByID(ctx context.Context,  jobID int64, runnerID string,leaseFor time.Duration) (*JobSpec, error)

	// Steps/job status updates (called from runner events)
	MarkStepRunning(ctx context.Context, stepID int64) error
	MarkStepFinished(ctx context.Context, stepID int64, status string, exitCode *int, errorMessage *string) error
	AppendStepLogChunk(ctx context.Context, jobID int64, stepID *int64, content string) error
	MarkJobFinished(ctx context.Context, jobID int64, status string, errorMessage *string) error

	// Crash recovery
	FindExpiredLeases(ctx context.Context, limit int) ([]int64, error)
	RequeueJob(ctx context.Context, jobID int64, reason string) error

	ListRuns(ctx context.Context, limit int) ([]Run, error)
	ListJobsByRun(ctx context.Context, runID int64) ([]Job, error)
	GetJob(ctx context.Context, jobID int64) (*Job, error)

	//logs
	ListLogChunks(ctx context.Context, jobID int64, afterID int64, limit int) ([]LogChunk, error)
	
	GetDependentJobs(ctx context.Context, runID int64, jobName string) ([]Job, error)
	GetJobByNameAndRun(ctx context.Context, runID int64, jobName string) (*Job, error)
	MarkJobSkipped(ctx context.Context, jobID int64) error

	// added
	GetRun(ctx context.Context, runID int64) (*Run, error) 
}