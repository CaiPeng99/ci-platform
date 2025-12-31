package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"ci-platform/control-plane/proto/runnerpb"
	// "ci-platform/control-plane/internal/storage"
)

func main() {
	// Configuration
	controlPlaneAddr := getEnv("CONTROL_PLANE_ADDR", "localhost:9090")
	runnerID := getEnv("RUNNER_ID", "runner-1")

	log.Printf("Starting runner: %s", runnerID)
	log.Printf("Connecting to control plane: %s", controlPlaneAddr)

	// Connect to control plane via gRPC
	conn, err := grpc.Dial(controlPlaneAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to control plane: %v", err)
	}
	defer conn.Close()

	client := runnerpb.NewRunnerGatewayClient(conn)
	ctx := context.Background()

	// Register runner
	_, err = client.RegisterRunner(ctx, &runnerpb.RegisterRunnerRequest{
		RunnerName: runnerID,
		Labels:     []string{"linux", "docker"},
	})
	if err != nil {
		log.Fatalf("Failed to register runner: %v", err)
	}
	log.Println("✅ Runner registered")

	// Main loop: request jobs and execute them
	for {
		log.Println("Requesting job from control plane...")

		// Request a job
		resp, err := client.LeaseJob(ctx, &runnerpb.LeaseJobRequest{
			RunnerId: runnerID,
			Labels:   []string{"linux", "docker"},
		})
		if err != nil {
			log.Printf("Failed to lease job: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if !resp.HasJob {
			log.Println("No jobs available, waiting...")
			time.Sleep(5 * time.Second)
			continue
		}

		// Execute the job
		log.Printf("✅ Got job: %s (ID: %s)", resp.JobSpec.JobName, resp.JobSpec.JobId)
		executeJob(ctx, client, runnerID, resp.JobSpec)
	}
}

func executeJob(ctx context.Context, client runnerpb.RunnerGatewayClient, runnerID string, job *runnerpb.JobSpec) {
	// Create a stream for reporting events
	stream, err := client.ReportEvent(ctx)
	if err != nil {
		log.Printf("Failed to create event stream: %v", err)
		return
	}
	defer stream.CloseAndRecv()

	log.Printf("Executing job %s with %d steps", job.JobId, len(job.Steps))

	jobSuccess := true

	// Execute each step
	for _, step := range job.Steps {
		log.Printf("  Step %s: %s", step.StepId, step.Name)

		// Report step started
		stream.Send(&runnerpb.RunnerEvent{
			RunnerId: runnerID,
			JobId:    job.JobId,
			Event: &runnerpb.RunnerEvent_StepStarted{
				StepStarted: &runnerpb.StepStarted{
					StepId:   step.StepId,
					TsUnixMs: time.Now().UnixMilli(),
				},
			},
		})

		// Execute the command
		cmd := exec.Command("sh", "-c", step.Command)
		output, err := cmd.CombinedOutput()

		// Send log output
		stream.Send(&runnerpb.RunnerEvent{
			RunnerId: runnerID,
			JobId:    job.JobId,
			Event: &runnerpb.RunnerEvent_LongChunk{
				LongChunk: &runnerpb.LongChunk{
					StepId:   step.StepId,
					Content:  string(output),
					TsUnixMs: time.Now().UnixMilli(),
				},
			},
		})

		// Report step finished
		exitCode := 0
		status := "success"
		var errMsg string

		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
			status = "failure"
			errMsg = err.Error()
			jobSuccess = false
			log.Printf("  ❌ Step %s failed: %v", step.Name, err)
		} else {
			log.Printf("  ✅ Step %s completed", step.Name)
		}

		stream.Send(&runnerpb.RunnerEvent{
			RunnerId: runnerID,
			JobId:    job.JobId,
			Event: &runnerpb.RunnerEvent_StepFinished{
				StepFinished: &runnerpb.StepFinished{
					StepId:       step.StepId,
					ExitCode:     int32(exitCode),
					Status:       status,
					ErrorMessage: errMsg,
					TsUnixMs:     time.Now().UnixMilli(),
				},
			},
		})

		// Stop executing steps if one fails
		if !jobSuccess {
			break
		}
	}

	// Report job finished
	jobStatus := "success"
	if !jobSuccess {
		jobStatus = "failure"
	}

	stream.Send(&runnerpb.RunnerEvent{
		RunnerId: runnerID,
		JobId:    job.JobId,
		Event: &runnerpb.RunnerEvent_JobFinished{
			JobFinished: &runnerpb.JobFinished{
				Status:   jobStatus,
				TsUnixMs: time.Now().UnixMilli(),
			},
		},
	})

	log.Printf("Job %s completed with status: %s", job.JobId, jobStatus)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}