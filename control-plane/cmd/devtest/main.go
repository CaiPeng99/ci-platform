/*
Connect to Postgres

# CreateRun → CreateJob → CreateStep

# MarkJobQueued

Publish {job_id} to RabbitMQ

# Consume that message and call LeaseJobByID

Mark step running/finished + append logs

# Mark job finished

Simulate lease expiry + requeue test (if you implemented recovery funcs)
*/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"ci-platform/control-plane/internal/store/pgstore"

	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

)

type JobMsg struct {
	JobID int64 `json:"job_id"`
}

func mustEnv(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func main() {
	ctx := context.Background()

	dbURL := mustEnv("DATABASE_URL", "postgres://ci:ci@localhost:5432/ci?sslmode=disable")
	amqpURL := mustEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	queueName := mustEnv("JOB_QUEUE", "jobs.runnable")

	// --- Postgres pool ---
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Cannot connect to db: %v", err)
	}
	defer pool.Close()

	store := pgstore.New(pool)

	// --- RabbitMQ connection ---
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	// --- Create Run, Job, Step ---
	runID, err := store.CreateRun(ctx, "example/repo", "refs/heads/main", "commitsha123", "push")
	if err != nil {
		log.Fatalf("CreateRun failed: %v", err)
	}
	fmt.Printf("Created Run ID: %d\n", runID)

	jobID, err := store.CreateJob(ctx, runID, "test", []string{}, "linux", 3)
	if err != nil {
		log.Fatalf("CreateJob failed: %v", err)
	}
	fmt.Printf("Created Job ID: %d\n", jobID)
	
	step1, err := store.CreateStep(ctx, jobID, 0, "Install", `echo "Installing..."`)
	if err != nil {
		log.Fatalf("CreateStep failed: %v", err)
	}
	step2, err := store.CreateStep(ctx, jobID, 1, "Test", `echo "Testing..."`)
	if err != nil {
		log.Fatalf("CreateStep failed: %v", err)
	}
	fmt.Printf("Created Step IDs: %d, %d\n", step1, step2)

	// 2) Mark job as queued (scheduler would do this, not implemented here, assume it's done)
	if err := store.MarkJobQueued(ctx, jobID); err != nil {
		log.Fatalf("MarkJobQueued failed: %v", err)
	}
	fmt.Printf("Marked Job ID %d as queued\n", jobID)

	// 3) Publish job ID to RabbitMQ
	jobMsg := JobMsg{JobID: jobID}
	body, err := json.Marshal(jobMsg)
	if err := ch.PublishWithContext(
		ctx,
		"",
		queueName,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	); err != nil {
		log.Fatalf("Failed to publish job message: %v", err)
	}
	fmt.Printf("Published Job ID %d to queue %s\n", jobID, queueName)

	// fmt.Println("Check the RabbitMQ UI now! Waiting 30 seconds...")
	// time.Sleep(30 * time.Second)

	// 4) Consume that message (runner/dispatcher would do this)
	msgs, err := ch.Consume(
		queueName,
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	msg := <-msgs
	var receivedJobMsg JobMsg
	if err := json.Unmarshal(msg.Body, &receivedJobMsg); err != nil {
		_ = msg.Nack(false, false)
		log.Fatalf("Failed to unmarshal job message: %v", err)
	}
	fmt.Printf("Received Job ID %d from queue\n", receivedJobMsg.JobID)

	// 5) Lease the job by ID (runner would call this via gRPC -> store)
	runnerID := "runner-dev-1"
	spec, err := store.LeaseJobByID(ctx, receivedJobMsg.JobID, runnerID, 60*time.Second)
	if err != nil {
		_ = msg.Nack(false, false)
		log.Fatalf("LeaseJobByID failed: %v", err)
	}
	_ = msg.Ack(false)
	fmt.Println("Lease job to runner:", runnerID)
	fmt.Printf("JobSpec: job=%d run=%d steps=%d\n", spec.JobID, spec.RunID, len(spec.Steps))

	// ✅ Add this debug check
	// var checkStatus string
	// pool.QueryRow(ctx, "SELECT status FROM jobs WHERE id = $1", receivedJobMsg.JobID).Scan(&checkStatus)
	// fmt.Printf("DEBUG: Job status after lease: '%s'\n", checkStatus)


	// //temporary test for LeaseExpriesAt
	// fmt.Println("\n--- Testing Lease Recovery ---")

	// _, err = pool.Exec(ctx, 
	// 	"UPDATE jobs SET lease_expires_at = NOW() - INTERVAL '10 minutes' WHERE id = $1",
	// 	receivedJobMsg.JobID)
	// if err != nil {
	// 	log.Printf("Failed to expire lease: %v", err)
	// } else {
	// 	fmt.Println("Manually expired the lease for testing")
	// }


	// expiredJobs, err := store.FindExpiredLeases(ctx, 50)
	// fmt.Printf("Found expired jobs: %v\n", expiredJobs)

	// for _, jobID := range expiredJobs {
	// 	err = store.RequeueJob(ctx, jobID, "lease expired")
	// 	if err != nil {
	// 		log.Printf("Failed to requeue job %d: %v", jobID, err)
	// 	} else {
	// 		fmt.Printf("Requeued job %d\n", jobID)
	// 	}
	// }

	// fmt.Println("--- Recovery Test Complete ---\n")
	// }
	
	// 6) Simulate runner step events + logs
	// step 1
	if err := store.MarkStepRunning(ctx, spec.Steps[0].StepID); err != nil {
		log.Fatalf("MarkStepRunning failed: %v", err)
	}
	{
		sid := spec.Steps[0].StepID
		if err := store.AppendStepLogChunk(ctx, spec.JobID, &sid, "Installing dependencies...\n"); err != nil {
			log.Fatalf("AppendStepLogChunk failed: %v", err)
		}
		exit0 := 0
		if err := store.MarkStepFinished(ctx, sid, "success", &exit0, nil); err != nil {
			log.Fatalf("MarkStepFinished failed: %v", err)
		}
		
	}

	// step 2
	if err := store.MarkStepRunning(ctx, spec.Steps[1].StepID); err != nil {
		log.Fatalf("MarkStepRunning failed: %v", err)
	}
	{
		sid := spec.Steps[1].StepID
		if err := store.AppendStepLogChunk(ctx, spec.JobID, &sid, "Running tests...\nPASS\n"); err != nil {
			log.Fatalf("AppendStepLogChunk failed: %v", err)
		}
		exit0 := 0
		if err := store.MarkStepFinished(ctx, sid, "success", &exit0, nil); err != nil {
			log.Fatalf("MarkStepFinished failed: %v", err)
		}
	}
	// 7) Mark job finished
	if err := store.MarkJobFinished(ctx, spec.JobID, "success", nil); err != nil {
		log.Fatalf("MarkJobFinished failed: %v", err)
	}
	fmt.Printf("Job ID %d marked as finished\n", spec.JobID)
	fmt.Println("DONE. Now inspect DB tables runs/jobs/steps/log_chunks.")

	}

