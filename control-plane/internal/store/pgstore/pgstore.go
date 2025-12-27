package pgstore

import (
	"context"
	"errors"
	"time"
	"log"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"ci-platform/control-plane/internal/store"
	"github.com/jackc/pgx/v5"
	
)

type PGStore struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

var ErrNotClaimable = errors.New("job not claimable")

func (s *PGStore) CreateRun(ctx context.Context, repo, ref, sha, trigger string) (int64, error) {
	var runID int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO runs (repo, ref, commit_sha, trigger)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		repo, ref, sha, trigger).Scan(&runID)
	// if err != nil {
	// 	return 0, err
	// }
	return runID, err
}

func (s *PGStore) CreateJob(ctx context.Context, runID int64, name string, needs []string, runOn string,maxAttempts int) (int64, error) {
	var jobID int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO jobs (run_id, name, needs, max_attempts, runs_on)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		runID, name, needs, maxAttempts, runOn).Scan(&jobID)
	// if err != nil {
	// 	return 0, err
	// }
	return jobID, err
}

func (s *PGStore) CreateStep(ctx context.Context, jobID int64, idx int, name, command string) (int64, error) {
	var stepID int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO steps (job_id, idx, name, command)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		jobID, idx, name, command).Scan(&stepID)
	// if err != nil {
	// 	return 0, err
	// }
	return stepID, err
}

func (s *PGStore) MarkJobQueued(ctx context.Context, jobID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE jobs
		 SET status = 'queued'
		 WHERE id = $1 AND status = 'pending'`,
		jobID)
	return err
}

/*
Later improvement (recommended):
Allow running jobs with expired lease to be reclaimed (for crash recovery), but do it carefully (only if attempts < max_attempts).
*/
func (s *PGStore) LeaseJobByID(ctx context.Context, jobID int64, runnerID string, leaseFor time.Duration) (*store.JobSpec, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {_ = tx.Rollback(ctx) }()

	leaseUntil := time.Now().Add(leaseFor)

	// Claim the job only if it's queued (or lease expired and still running).
	// MVP: only allow queued -> running.
	var runID int64
	var repo, ref, commitSHA, jobName string

	row := tx.QueryRow(ctx,
		`UPDATE jobs j
		SET status = 'running', runner_id = $2, leased_at = now(), lease_expires_at = $3,
			attempts = attempts + 1,
			started_at = COALESCE(j.started_at, NOW())
		
		FROM runs r
		WHERE j.id = $1 AND j.status = 'queued' AND j.run_id = r.id
			RETURNING j.run_id, r.repo, r.ref, r.commit_sha, j.name`,
		jobID, runnerID, leaseUntil)

	if err := row.Scan(&runID, &repo, &ref, &commitSHA, &jobName); err != nil {
			log.Printf("Failed to lease job: %v", err)  // ✅ Add this
			return nil, ErrNotClaimable
	}

	// Load steps in order
	rows, err := tx.Query(ctx,
		`SELECT id, name, command
		 FROM steps
		 WHERE job_id = $1
		 ORDER BY idx ASC`,
		jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []store.StepSpec

	for rows.Next() {
		var sid int64
		var name, command string
		if err := rows.Scan(&sid, &name, &command); err != nil {
			return nil, err
		}
		steps = append(steps, store.StepSpec{
			StepID:   sid,
			Name:    name,
			Command: command,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &store.JobSpec{
		JobID:        jobID,
		RunID:     runID,
		JobName:      jobName,
		Repo:      repo,
		Ref:       ref,
		CommitSHA: commitSHA,
		Steps:     steps,
	}, nil
}

func (s *PGStore) AppendStepLogChunk(ctx context.Context, jobID int64, stepID *int64, content string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO log_chunks (step_id, job_id, content)
		 VALUES ($1, $2, $3)`,
		stepID, jobID, content)
	return err
}	

// started_at = COALESCE(started_at, NOW())Only sets if NULLWant to preserve first time set
func (s *PGStore) MarkStepRunning(ctx context.Context, stepID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE steps
		 SET status = 'running', started_at = COALESCE(started_at, NOW())
		 WHERE id = $1 AND status = 'pending'`,
		stepID)
	return err
}

func (s *PGStore) MarkStepFinished(ctx context.Context, stepID int64, status string, exitCode *int, errMsg *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE steps
		 SET status = $2, exit_code = $3, finished_at = NOW(), error_message = $4
		 WHERE id = $1`,
		stepID, status, exitCode, errMsg)
	return err
}

func (s *PGStore) MarkJobFinished(ctx context.Context, jobID int64, status string, errMsg *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE jobs
		 SET status = $2, finished_at = NOW(), error_message = $3, lease_expires_at = NULL
		 WHERE id = $1`,
		jobID, status, errMsg)
	return err
}

// added MarkRunFinished
func (s *PGStore) MarkRunFinished(ctx context.Context, runID int64, status string, errMsg *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE runs
		 SET status = $2, finished_at = NOW(), error_message = $3
		 WHERE id = $1`,
		runID, status, errMsg)
	return err
}

//added MarkRunRunning
func (s *PGStore) MarkRunRunning(ctx context.Context, runID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE runs
		 SET status = 'running', started_at = COALESCE(started_at, NOW())
		 WHERE id = $1`,
		runID)
	return err
}

func (s *PGStore) UpsertRunnerHeartbeat(ctx context.Context, runnerID string, labels []string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO runners (id, labels, last_heartbeat)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (id) DO UPDATE
		 SET labels = $2, last_heartbeat = NOW()`,
		runnerID, labels)
	return err
}	

func (s *PGStore) FindExpiredLeases(ctx context.Context, limit int) ([]int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id
		 FROM jobs
		 WHERE status = 'running' AND lease_expires_at IS NOT NULL
		 AND lease_expires_at < NOW()
		 ORDER BY lease_expires_at ASC
		 LIMIT $1`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobIDs []int64
	for rows.Next() {
		var jobID int64
		if err := rows.Scan(&jobID); err != nil {
			return nil, err
		}
		jobIDs = append(jobIDs, jobID)
	}
	return jobIDs, nil
}

func (s * PGStore) RequeueJob(ctx context.Context, jobID int64, reason string) error {
	// ✅ Add debug: Check current job state
	var currentStatus string
	var currentAttempts, maxAttempts int
	err := s.pool.QueryRow(ctx,
		`SELECT status, attempts, max_attempts FROM jobs WHERE id = $1`,
		jobID).Scan(&currentStatus, &currentAttempts, &maxAttempts)
	if err == nil {
		log.Printf("DEBUG RequeueJob: Job %d current state: status='%s', attempts=%d, max_attempts=%d",
			jobID, currentStatus, currentAttempts, maxAttempts)
	}
	
	result, err := s.pool.Exec(ctx,
		`UPDATE jobs
		 SET status = 'queued', runner_id = NULL, leased_at = NULL, lease_expires_at = NULL, error_message = $2
		 WHERE id = $1 AND status = 'running' AND attempts < max_attempts`,
		jobID, reason)
	
		if err != nil {
		return err
	}
	
	// Check if any rows were updated
	if result.RowsAffected() == 0 {
		// Job might have already been requeued, finished, or hit max attempts
		return fmt.Errorf("job %d not requeued (already finished or max attempts reached)", jobID)
	}
	
	return nil
	// return err
}

// func (s *PGStore) GetJob(ctx context.Context, jobID int64) (*store.Job, error) {
// 	var job store.Job
// 	err := s.pool.QueryRow(ctx,
// 		`SELECT id, run_id, name, status, attempts, max_attempts, runs_on, runner_id, started_at, finished_at, error_message
// 		 FROM jobs
// 		 WHERE id = $1`,
// 		jobID).Scan(&job.ID, &job.RunID, &job.Name, &job.Status, &job.Attempts, &job.MaxAttempts,&job.RunsOn,
// 		&job.RunnerID, &job.StartedAt, &job.FinishedAt, &job.ErrorMessage)
// 	if err != nil {
// 		if errors.Is(err, pgx.ErrNoRows) {
// 			return nil, nil // job not found
// 		}
// 		return nil, err
// 	}
// 	return &job, nil
// }

func (s *PGStore) GetJob(ctx context.Context, jobID int64) (*store.Job, error) {
	query := `
		SELECT id, run_id, name, status, needs, attempts, max_attempts, runs_on,
		       runner_id, lease_expires_at, created_at, started_at, finished_at, error_message
		FROM jobs
		WHERE id = $1
	`
	
	var job store.Job
	var runnerID *string
	var runsOn *string
	var leaseExpiresAt, createdAt, startedAt, finishedAt *time.Time
	var errorMsg *string
	var needs []string
	
	err := s.pool.QueryRow(ctx, query, jobID).Scan(
		&job.ID,
		&job.RunID,
		&job.Name,
		&job.Status,
		&needs,
		&job.Attempts,
		&job.MaxAttempts,
		&runsOn,
		&runnerID,
		&leaseExpiresAt,
		&createdAt,
		&startedAt,
		&finishedAt,
		&errorMsg,
	)
	
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	
	job.Needs = needs
	if runsOn != nil {
		job.RunsOn = *runsOn
	}
	if runnerID != nil {
		job.RunnerID = runnerID
	}
	if leaseExpiresAt != nil {
		job.LeaseExpiresAt = *leaseExpiresAt
	}
	if createdAt != nil {
		job.CreatedAt = *createdAt
	}
	if startedAt != nil {
		job.StartedAt = *startedAt
	}
	if finishedAt != nil {
		job.FinishedAt = *finishedAt
	}
	if errorMsg != nil {
		job.ErrorMessage = errorMsg
	}
	
	return &job, nil
}

func (s *PGStore) ListLogChunks(ctx context.Context, jobID int64, afterID int64, limit int) ([]store.LogChunk, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, step_id, job_id, content, timestamp
		 FROM log_chunks
		 WHERE job_id = $1 AND id > $2
		 ORDER BY id ASC
		 LIMIT $3`,
		jobID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []store.LogChunk
	for rows.Next() {
		var chunk store.LogChunk
		if err := rows.Scan(&chunk.ID, &chunk.StepID, &chunk.JobID, &chunk.Content, &chunk.Timestamp); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// func (s *PGStore) ListJobsByRun(ctx context.Context, runID int64) ([]store.Job, error) {
// 	rows, err := s.pool.Query(ctx,
// 		`SELECT id, run_id, name, runs_on, status, attempts, max_attempts, runner_id, created_at,started_at, finished_at, error_message
// 		 FROM jobs
// 		 WHERE run_id = $1
// 		 ORDER BY id ASC`,
// 		runID)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer rows.Close()

// 	var jobs []store.Job
// 	for rows.Next() {
// 		var job store.Job
// 		if err := rows.Scan(&job.ID, &job.RunID, &job.Name, &job.Status, &job.Attempts, &job.MaxAttempts,&job.RunsOn,
// 			&job.RunnerID, &job.StartedAt, &job.CreatedAt, &job.FinishedAt, &job.ErrorMessage); err != nil {
// 			return nil, err
// 		}
// 		jobs = append(jobs, job)
// 	}
// 	return jobs, nil
// }
func (s *PGStore) ListJobsByRun(ctx context.Context, runID int64) ([]store.Job, error) {
	query := `
		SELECT id, run_id, name, status, needs, attempts, max_attempts, runs_on,
		       runner_id, lease_expires_at, created_at, started_at, finished_at, error_message
		FROM jobs
		WHERE run_id = $1
		ORDER BY id ASC
	`
	
	rows, err := s.pool.Query(ctx, query, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []store.Job
	for rows.Next() {
		var job store.Job
		var runnerID *string
		var runsOn *string
		var leaseExpiresAt, createdAt, startedAt, finishedAt *time.Time
		var errorMsg *string
		var needs []string
		
		err := rows.Scan(
			&job.ID,
			&job.RunID,
			&job.Name,
			&job.Status,
			&needs,
			&job.Attempts,
			&job.MaxAttempts,
			&runsOn,
			&runnerID,
			&leaseExpiresAt,
			&createdAt,
			&startedAt,
			&finishedAt,
			&errorMsg,
		)
		if err != nil {
			return nil, err
		}
		
		job.Needs = needs
		if runsOn != nil {
			job.RunsOn = *runsOn
		}
		if runnerID != nil {
			job.RunnerID = runnerID
		}
		if leaseExpiresAt != nil {
			job.LeaseExpiresAt = *leaseExpiresAt
		}
		if createdAt != nil {
			job.CreatedAt = *createdAt
		}
		if startedAt != nil {
			job.StartedAt = *startedAt
		}
		if finishedAt != nil {
			job.FinishedAt = *finishedAt
		}
		if errorMsg != nil {
			job.ErrorMessage = errorMsg
		}
		
		jobs = append(jobs, job)
	}
	
	return jobs, rows.Err()
}

func (s *PGStore) ListRuns(ctx context.Context, limit int) ([]store.Run, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, repo, ref, commit_sha, trigger, status, created_at, started_at, finished_at, error_message
		 FROM runs
		 ORDER BY id DESC
		 LIMIT $1`,
		limit)
	if err != nil {
		return nil, err
	}			
	defer rows.Close()

	var runs []store.Run
	for rows.Next() {
		var run store.Run
		if err := rows.Scan(&run.ID, &run.Repo, &run.Ref, &run.CommitSHA, &run.Trigger,
			&run.Status, &run.CreatedAt, &run.StartedAt, &run.FinishedAt, &run.ErrorMessage); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (s *PGStore) GetDependentJobs(ctx context.Context, runID int64, jobName string) ([]store.Job, error) {
	query := `
		SELECT id, run_id, name, status, needs, attempts, max_attempts, runs_on,
		       runner_id, lease_expires_at, created_at, started_at, finished_at, error_message
		FROM jobs
		WHERE run_id = $1
		  AND $2 = ANY(needs)
		  AND status IN ('queued', 'pending')
		ORDER BY id ASC
	`
	
	rows, err := s.pool.Query(ctx, query, runID, jobName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var jobs []store.Job
	for rows.Next() {
		var job store.Job
		var runnerID *string
		var runsOn *string 
		var leaseExpiresAt, createdAt, startedAt, finishedAt *time.Time
		var errorMsg *string
		var needs []string
		
		err := rows.Scan(
			&job.ID, &job.RunID, &job.Name, &job.Status, &needs,
			&job.Attempts, &job.MaxAttempts, &runsOn, 
			&runnerID, &leaseExpiresAt, &job.CreatedAt,
			&startedAt, &finishedAt, &errorMsg,
		)
		if err != nil {
			return nil, err
		}
		
		job.Needs = needs
		if runsOn != nil {
			job.RunsOn = *runsOn
		}
		if runnerID != nil {
			job.RunnerID = runnerID
		}
		if leaseExpiresAt != nil {
			job.LeaseExpiresAt = *leaseExpiresAt
		}
		if createdAt != nil {
			job.CreatedAt = *createdAt
		}
		if startedAt != nil {
			job.StartedAt = *startedAt
		}
		if finishedAt != nil {
			job.FinishedAt = *finishedAt
		}
		if errorMsg != nil {
			job.ErrorMessage = errorMsg
		}
		
		jobs = append(jobs, job)
	}
	
	return jobs, rows.Err()
}

func (s *PGStore) GetJobByNameAndRun(ctx context.Context, runID int64, jobName string) (*store.Job, error) {
	query := `
		SELECT id, run_id, name, status, needs, attempts, max_attempts, runs_on,
		       runner_id, lease_expires_at, created_at, started_at, finished_at, error_message
		FROM jobs
		WHERE run_id = $1 AND name = $2
	`
	
	var job store.Job
	var runnerID *string
	var runsOn *string
	var leaseExpiresAt, startedAt, createdAt, finishedAt *time.Time
	var errorMsg *string
	var needs []string
	
	err := s.pool.QueryRow(ctx, query, runID, jobName).Scan(
		&job.ID, &job.RunID, &job.Name, &job.Status, &needs,
		&job.Attempts, &job.MaxAttempts,&runsOn, 
		&runnerID, &leaseExpiresAt, &job.CreatedAt,
		&startedAt, &finishedAt, &errorMsg,
	)
	
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	
	job.Needs = needs
	if runsOn != nil {
		job.RunsOn = *runsOn
	}
	if runnerID != nil {
		job.RunnerID = runnerID
	}
	if leaseExpiresAt != nil {
		job.LeaseExpiresAt = *leaseExpiresAt
	}
	if createdAt != nil {
		job.CreatedAt = *createdAt
	}
	if startedAt != nil {
		job.StartedAt = *startedAt
	}
	if finishedAt != nil {
		job.FinishedAt = *finishedAt
	}
	if errorMsg != nil {
		job.ErrorMessage = errorMsg
	}
	
	return &job, nil
}

func (s *PGStore) MarkJobSkipped(ctx context.Context, jobID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE jobs 
		 SET status = 'skipped', updated_at = now() 
		 WHERE id = $1`,
		jobID)
	return err
}