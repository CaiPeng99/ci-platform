package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"os"
	"path/filepath"

	amqp "github.com/rabbitmq/amqp091-go"

	"ci-platform/control-plane/internal/store"
	"ci-platform/control-plane/internal/logstream"
	"ci-platform/control-plane/internal/workflow"
	
)

type Server struct {
	store store.Store
	hub   *logstream.Hub
	rabbit *amqp.Channel
}

func New(store store.Store, hub *logstream.Hub, rabbit *amqp.Channel) *Server {
	return &Server{
		store: store,
		hub:   hub,
		rabbit: rabbit,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/runs", s.handleRuns) // GET
	mux.HandleFunc("/runs/trigger", s.handleTrigger) // POST
	mux.HandleFunc("/runs/", s.handleRunsSubroutes) // GET /runs/{id}/jobs
	mux.HandleFunc("/jobs/", s.handleJobsSubroutes) // GET /jobs/{id}, /jobs/{id}/logs

	return withCORS(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {		
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	runs, err := s.store.ListRuns(r.Context(), 50)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list runs: %v", err), 500)
		return
	}

	writeJSON(w, runs)
}

func (s *Server) handleRunsSubroutes(w http.ResponseWriter, r *http.Request) {
	// expects /runs/{runID}/jobs
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "runs" || parts[2] != "jobs" {
		// http.Error(w, "Not found", ) # http.StatusNotFound
		http.NotFound(w, r)
		return
	}

	runID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		http.Error(w, "Invalid run ID", 400) // http.StatusBadRequest
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405) // http.StatusMethodNotAllowed
		return
	}

	jobs, err := s.store.ListJobsByRun(r.Context(), runID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list jobs: %v", err), 500)
		return
	}

	writeJSON(w, jobs)
}

func (s *Server) handleJobsSubroutes(w http.ResponseWriter, r *http.Request) {
	// expects:
	// /jobs/{jobID}
	// /jobs/{jobID}/logs
	// /jobs/{jobID}/logs/stream
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "jobs" {
		http.NotFound(w, r)
		return
	}

	jobID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		http.Error(w, "Invalid job ID", 400)
		return
	}

	// Handle /jobs/{jobID}
	if len(parts) == 2 {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", 405)
			return
		}

		job, err := s.store.GetJob(r.Context(), jobID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get job: %v", err), 500)
			return
		}
		if job == nil {
			http.NotFound(w, r)
			return
		}

		writeJSON(w, job)
		return
	}

	// Handle /jobs/{jobID}/logs or losgs/stream
	if len(parts) >= 3 && parts[2] == "logs" {
		if len(parts) == 3 {
			// /jobs/{jobID}/logs
			s.handeJobLogs(w, r, jobID)
			return
		} else if len(parts) == 4 && parts[3] == "stream" {
			// /jobs/{jobID}/logs/stream
			s.handleJobLogStream(w, r, jobID)
			return
		}
	}

	http.NotFound(w, r)
}

func (s *Server) handeJobLogs(w http.ResponseWriter, r *http.Request, jobID int64) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}

	
	afterID := int64(0)
	if v := r.URL.Query().Get("after_id"); v != "" {
		if n,err := strconv.ParseInt(v, 10, 64); err == nil {
			afterID = n
		}
	}

	chunks, err := s.store.ListLogChunks(r.Context(), jobID, afterID, 500)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list log chunks: %v", err), 500)
		return
	}

	writeJSON(w, chunks)
}

func (s *Server) handleJobLogStream(w http.ResponseWriter, r *http.Request, jobID int64) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", 405)
		return
	}

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", 500)
		return
	}

	// 1) replay existing logs
	chunks, err := s.store.ListLogChunks(r.Context(), jobID, 0, 2000)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list log chunks: %v", err), 500)
		return
	}
	for _, chunk := range chunks {
		writeSSE(w, "log_chunk", chunk.Content)
	}
	flusher.Flush()
	
	// 2) subscribe to new logs
	sub, unsub := s.hub.Subscribe(jobID, 200)
	defer unsub()

	// keep ticker alive so proxies don't close connection
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			writeSSE(w, "ping", "ok")
			flusher.Flush()
		case chunk := <-sub:
			writeSSE(w, "log_chunk", chunk.Content)
			flusher.Flush()

		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	// encoder.SetEscapeHTML(false)
	encoder.Encode(v)
}

func writeSSE(w http.ResponseWriter, event string, data string) {
	// SSE frames: event + data lines + blank line
	line := strings.Split(data, "\n")
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	for _, l := range line {
		fmt.Fprintf(w, "data: %s\n", l)
	}
	fmt.Fprintf(w, "\n")
}

// minimal CORS for local React dev server
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	
	var req struct {
		RepoPath     string `json:"repo_path"`
		WorkflowPath string `json:"workflow_path"`
		Ref          string `json:"ref"`
		CommitSHA    string `json:"commit_sha"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	
	// Validate required fields
	if req.RepoPath == "" || req.WorkflowPath == "" {
		http.Error(w, "repo_path and workflow_path are required", 400)
		return
	}

	// Set defaults
	if req.Ref == "" {
		req.Ref = "refs/heads/main"
	}
	if req.CommitSHA == "" {
		req.CommitSHA = "unknown"
	}

	// 1. Read workflow file
	workflowFilePath := filepath.Join(req.RepoPath, req.WorkflowPath)
	workflowData, err := os.ReadFile(workflowFilePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("workflow file not found: %v", err), 404)
		return
	}
	
	// 2. Parse workflow YAML
	wf, err := workflow.Parse(workflowData)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid workflow: %v", err), 400)
		return
	}

	// 3. Create run (add trigger parameter)
	runID, err := s.store.CreateRun(r.Context(), req.RepoPath, req.Ref, req.CommitSHA, "api")
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create run: %v", err), 500)
		return
	}

	// 4. Create jobs and steps from workflow
	jobIDs := make(map[string]int64) // map job name -> job ID

	for jobName, job := range wf.Jobs {
		// Create job (add runOn and maxAttempts parameters)
		jobID, err := s.store.CreateJob(r.Context(), runID, jobName, job.Needs, "any", 3)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to create job: %v", err), 500)
			return
		}
		jobIDs[jobName] = jobID
		
		// Create steps for this job (add idx parameter)
		for idx, step := range job.Steps {
			_, err := s.store.CreateStep(r.Context(), jobID, idx, step.Name, step.Run)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to create step: %v", err), 500)
				return
			}
		}
	}

	// 5. Mark jobs as queued and enqueue runnable ones to RabbitMQ
	// Jobs with no dependencies are immediately runnable
	for jobName, jobID := range jobIDs {
		job := wf.Jobs[jobName]
		
		if err := s.store.MarkJobQueued(r.Context(), jobID); err != nil {
			http.Error(w, fmt.Sprintf("failed to mark job queued: %v", err), 500)
			return
		}
		
		// If job has no dependencies, enqueue it now
		if len(job.Needs) == 0 {
			if err := s.enqueueJob(r.Context(), jobID); err != nil {
				http.Error(w, fmt.Sprintf("failed to enqueue job: %v", err), 500)
				return
			}
		}
	}
	
	writeJSON(w, map[string]interface{}{
		"run_id": runID,
		"jobs":   jobIDs,
		"status": "triggered",
	})
}


func (s *Server) enqueueJob(ctx context.Context, jobID int64) error {
	
	if s.rabbit == nil {
		// If no RabbitMQ channel, just log (useful for testing)
		fmt.Printf("No RabbitMQ configured - would enqueue job %d\n", jobID)
		return nil
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
	
	fmt.Printf("✅ Enqueued job %d to RabbitMQ\n", jobID)
	return nil
}
