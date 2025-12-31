package runnergrpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strconv"
	"time"
	"fmt"

	"ci-platform/control-plane/internal/logstream"
	"ci-platform/control-plane/internal/store"
	"ci-platform/control-plane/internal/store/pgstore"
	"ci-platform/control-plane/proto/runnerpb"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RunnerServer struct {
	runnerpb.UnimplementedRunnerGatewayServer
	store store.Store

	// channel of pending deliveries from RabbitMQ consumer
	jobDeliveries <-chan amqp.Delivery
	leaseFor time.Duration

	hub           *logstream.Hub
	rabbit        *amqp.Channel 
}

func NewRunnerServer(store store.Store, jobDeliveries <-chan amqp.Delivery, leaseFor time.Duration, hub *logstream.Hub, rabbit *amqp.Channel) *RunnerServer {
	return &RunnerServer{
		store:         store,
		jobDeliveries: jobDeliveries,
		leaseFor:      leaseFor,
		hub: 			hub,
		rabbit:        rabbit,
	}
}

// Change req.RunnerId to req.RunnerId (should be correct) or check proto
// Change field names to match what's in your proto

func (s *RunnerServer) RegisterRunner(ctx context.Context, req *runnerpb.RegisterRunnerRequest) (*runnerpb.RegisterRunnerResponse, error) {
	if err := s.store.UpsertRunnerHeartbeat(ctx, req.RunnerName, req.Labels); err != nil {
		return nil, err
	}
	return &runnerpb.RegisterRunnerResponse{Ok: true}, nil
}

func (s *RunnerServer) Heartbeat(ctx context.Context, req *runnerpb.HeartbeatRequest) (*runnerpb.HeartbeatResponse, error) {
	if err := s.store.UpsertRunnerHeartbeat(ctx, req.RunnerId, req.Labels); err != nil {
		return nil, err	
	}
	return &runnerpb.HeartbeatResponse{Ok: true}, nil
}

func (s *RunnerServer) LeaseJob(ctx context.Context, req *runnerpb.LeaseJobRequest) (*runnerpb.LeaseJobResponse, error) {
	var d amqp.Delivery
	select {
	case d = <-s.jobDeliveries:
		// got a job delivery
	case <-time.After(2 * time.Second):
		// timeout waiting for a job
		return &runnerpb.LeaseJobResponse{HasJob: false}, nil	
	case <-ctx.Done():
		return nil, ctx.Err()	
	}
	
	// decode job_id
	var msg struct {
		JobID int64 `json:"job_id"`
	}
	if err := json.Unmarshal(d.Body, &msg); err != nil {
		log.Printf("Failed to unmarshal job delivery: %v", err)
		_ = d.Ack(false) // ack to remove the bad message
		return &runnerpb.LeaseJobResponse{HasJob: false}, nil
	}

	spec, err := s.store.LeaseJobByID(ctx, msg.JobID, req.RunnerId, s.leaseFor)
	if err != nil {
		log.Printf("Failed to lease job ID %d: %v", msg.JobID, err)
		if errors.Is(err, pgstore.ErrNotClaimable) {
			_ = d.Ack(false)
			return &runnerpb.LeaseJobResponse{HasJob: false}, nil
		} else {
			// other error
			// transient error: requeue
			_ = d.Nack(false, true)
			return nil, err
		}
	}
	
	run, err := s.store.GetRun(ctx, spec.RunID)
	if err != nil {
		log.Printf("Failed to get run: %v", err)
		_ = d.Nack(false, true)
		return nil, err
	}

	// log.Printf("🔍 ClaimJob - Run %d has repo: %s", run.ID, run.Repo)  // ✅ ADD DEBUG



		_ = d.Ack(false) // ✅ Success - ack the message
		resp := &runnerpb.LeaseJobResponse{
			HasJob: true,
			JobSpec: &runnerpb.JobSpec{
				// JobId:          strconv.FormatInt(spec.JobID, 10),
				// RunId:          strconv.FormatInt(spec.RunID, 10),
				//  Repo:     spec.Repo,
				// Ref:      spec.Ref,
				// CommitSha: spec.CommitSHA,
				// JobName:  spec.JobName,
				// ContainerImage: "ci-runner-image:latest", // hardcoded for now
				// RepoPath:       spec.Repo,
				Repo:     run.Repo,      // ✅ Should be the cloned path
				Ref:      run.Ref,
				CommitSha: run.CommitSHA,
				JobName:  spec.JobName,
				ContainerImage: "ci-runner-image:latest",
				RepoPath: run.Repo,      // ✅ This is what runner uses!
					},
		}
		for _, step := range spec.Steps {
			resp.JobSpec.Steps = append(resp.JobSpec.Steps, &runnerpb.StepSpec{
				// StepId:  step.StepID,
				StepId:  strconv.FormatInt(step.StepID, 10),
				Name:    step.Name,
				Command: step.Command,
			})
		}

		return resp, nil
	}

	func (s *RunnerServer) ReportEvent(stream runnerpb.RunnerGateway_ReportEventServer) error {
		ctx := stream.Context()
		for {
			ev, err := stream.Recv()
			if err == io.EOF {
					return stream.SendAndClose(&runnerpb.ReportAck{Ok: true})
				}
			if err != nil {
				return err
			}
			
			// Convert string IDs from proto to int64 for store
			jobID, _ := strconv.ParseInt(ev.JobId, 10, 64)

			switch e := ev.Event.(type) {
			case *runnerpb.RunnerEvent_StepStarted:
				stepID, _ := strconv.ParseInt(e.StepStarted.StepId, 10, 64)
				_ = s.store.MarkStepRunning(ctx, stepID)

			case *runnerpb.RunnerEvent_LongChunk:
				var stepID *int64
				if e.LongChunk.StepId != ""  && e.LongChunk.StepId != "0" {
					// sid := e.LongChunk.StepId
					sid, _ := strconv.ParseInt(e.LongChunk.StepId, 10, 64)
					stepID = &sid
				}
				_ = s.store.AppendStepLogChunk(ctx, jobID, stepID, e.LongChunk.Content)
				s.hub.Publish(jobID, logstream.Event{
					JobID: jobID,
					Content: e.LongChunk.Content,
				})

			case *runnerpb.RunnerEvent_StepFinished:
				// sid := e.StepFinished.StepId
				stepID, _ := strconv.ParseInt(e.StepFinished.StepId, 10, 64)
				exit := int(e.StepFinished.ExitCode)
				status := e.StepFinished.Status
				var errMsg *string
				if e.StepFinished.ErrorMessage != "" {
					msg := e.StepFinished.ErrorMessage
					errMsg = &msg
				}
				_ = s.store.MarkStepFinished(ctx, stepID, status, &exit, errMsg)
			
			case *runnerpb.RunnerEvent_JobFinished:
				status := e.JobFinished.Status
				var errMsg *string
				if e.JobFinished.ErrorMessage != "" {
					s := e.JobFinished.ErrorMessage
					errMsg = &s
				}

				// Get job to find its run_id
				job, err := s.store.GetJob(ctx, jobID)
				if err != nil {
					return fmt.Errorf("failed to get job: %w", err)
				}

				
				// Mark job as finished
				if err := s.store.MarkJobFinished(ctx, jobID, status, errMsg); err != nil {
					return fmt.Errorf("failed to mark job finished: %w", err)
				}


				// ✅ NEW: Update run status based on all jobs
				if err := s.store.CheckAndUpdateRunStatus(ctx, job.RunID); err != nil {
					log.Printf("Warning: failed to update run status: %v", err)
				}
						
				// Handle dependent jobs based on status
				if status == "success" {
					// Job succeeded - enqueue dependent jobs
					if err := s.enqueueDependentJobs(ctx, jobID); err != nil {
						log.Printf("Warning: failed to enqueue dependent jobs for job %d: %v", jobID, err)
					}
				} else if status == "failed" {
					// Job failed - skip dependent jobs
					if err := s.skipDependentJobs(ctx, jobID); err != nil {
						log.Printf("Warning: failed to skip dependent jobs for job %d: %v", jobID, err)
					}

					// ✅ Also update run status when job fails
					if err := s.store.CheckAndUpdateRunStatus(ctx, job.RunID); err != nil {
						log.Printf("Warning: failed to update run status: %v", err)
					}
				}

			}
	}
}

func (s *RunnerServer) enqueueDependentJobs(ctx context.Context, completedJobID int64) error {
	// 1. Get the completed job details
	completedJob, err := s.store.GetJob(ctx, completedJobID)
	if err != nil {
		return fmt.Errorf("failed to get completed job: %w", err)
	}
	if completedJob == nil {
		return fmt.Errorf("job %d not found", completedJobID)
	}

	// 2. Find jobs that depend on this job
	dependentJobs, err := s.store.GetDependentJobs(ctx, completedJob.RunID, completedJob.Name)
	if err != nil {
		return fmt.Errorf("failed to get dependent jobs: %w", err)
	}
	
	// 3. For each dependent job, check if all dependencies are satisfied
	for _, job := range dependentJobs {
		allMet, err := s.checkAllDependencies(ctx, job.ID, completedJob.RunID)
		if err != nil {
			log.Printf("Failed to check dependencies for job %d: %v", job.ID, err)
			continue
		}
		
		if allMet {
			// All dependencies met - enqueue the job
			if err := s.enqueueJob(ctx, job.ID); err != nil {
				log.Printf("Failed to enqueue job %d: %v", job.ID, err)
			} else {
				log.Printf("✅ Enqueued dependent job %d (%s)", job.ID, job.Name)
			}
		}
	}
	
	return nil
}

func (s *RunnerServer) skipDependentJobs(ctx context.Context, failedJobID int64) error{
	// 1. Get the failed job details
	failedJob, err := s.store.GetJob(ctx, failedJobID)
	if err != nil {
		return fmt.Errorf("failed to get failed job: %w", err)
	}
	if failedJob == nil {
		return fmt.Errorf("job %d not found", failedJobID)
	}

	// 2. Find jobs that depend on this job
	dependentJobs, err := s.store.GetDependentJobs(ctx, failedJob.RunID, failedJob.Name)
	if err != nil {
		return fmt.Errorf("failed to get dependent jobs: %w", err)
	}
	
	// 3. Mark all dependent jobs as skipped
	for _, job := range dependentJobs {
		if err := s.store.MarkJobSkipped(ctx, job.ID); err != nil {
			log.Printf("Failed to skip job %d: %v", job.ID, err)
		} else {
			log.Printf("⏭️  Skipped dependent job %d (%s) because job %d failed", job.ID, job.Name, failedJobID)
		}
		
		// Recursively skip jobs that depend on this skipped job
		_ = s.skipDependentJobs(ctx, job.ID)  // ✅ Add _ = to ignore return value
	}
	return nil
}


func (s *RunnerServer) checkAllDependencies(ctx context.Context, jobID int64, runID int64) (bool, error) {
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, fmt.Errorf("job %d not found", jobID)
	}
	
	// If no dependencies, it's ready
	if len(job.Needs) == 0 {
		return true, nil
	}
	// Check each dependency
	for _, depName := range job.Needs {
		depJob, err := s.store.GetJobByNameAndRun(ctx, runID, depName)
		if err != nil {
			return false, fmt.Errorf("failed to get dependency job %s: %w", depName, err)
		}
		if depJob == nil {
			return false, fmt.Errorf("dependency job %s not found", depName)
		}
		
		// Dependency must be successful
		if depJob.Status != "success" {
			return false, nil
		}
	}
	
	return true, nil
}

func (s *RunnerServer) enqueueJob(ctx context.Context, jobID int64) error {
	if s.rabbit == nil {
		return fmt.Errorf("RabbitMQ channel not configured")
	}
	
	body := fmt.Sprintf(`{"job_id":%d}`, jobID)
	
	err := s.rabbit.PublishWithContext(
		ctx,
		"",              // exchange
		"jobs.runnable", // routing key
		false,           // mandatory
		false,           // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        []byte(body),
		},
	)
	
	if err != nil {
		return fmt.Errorf("failed to publish to RabbitMQ: %w", err)
	}
	
	return nil // 
}