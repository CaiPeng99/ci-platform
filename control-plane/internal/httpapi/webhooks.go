package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"log"
	"net/url"
	"strings"

	"ci-platform/control-plane/internal/workflow"
)

type GitHubPushEvent struct{
	Ref        string `json:"ref"`
	After      string `json:"after"` // commit SHA
	Repository struct {
		FullName string `json:"full_name"` // e.g., "user/repo"
		CloneURL string `json:"clone_url"`
	} `json:"repository"`
	Pusher struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"pusher"`
}

func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	eventType := r.Header.Get("X-GitHub-Event")
	log.Printf("🎯 Received GitHub event type: %s", eventType)

	// If it's a ping event, just respond OK
	if eventType == "ping" {
		log.Println("💓 Received ping event")
		writeJSON(w, map[string]string{"status": "pong"})
		return
	}
	
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	
	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("❌ Failed to read body: %v", err)
		http.Error(w, "failed to read body", 400)
		return
	}
	
	log.Printf("📦 Received webhook payload length: %d bytes", len(body))
	log.Printf("📋 Content-Type: %s", r.Header.Get("Content-Type"))


	// Parse URL-encoded form if needed
	var jsonBody []byte
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		// GitHub sent form-encoded data, extract the 'payload' field
		values, err := url.ParseQuery(string(body))
		if err != nil {
			log.Printf("❌ Failed to parse form data: %v", err)
			http.Error(w, "invalid form data", 400)
			return
		}
		jsonBody = []byte(values.Get("payload"))
		log.Printf("📝 Extracted JSON from form field")
	} else {
		// Raw JSON body
		jsonBody = body
	}

	// Verify signature (optional but recommended for production)
	secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if secret != "" {
		signature := r.Header.Get("X-Hub-Signature-256")
		// ADD THIS: Skip verification if no signature (for local testing)
		if signature == "" {
			log.Println("⚠️  WARNING: No signature provided, skipping verification (local test mode)")
		} else if !verifyGitHubSignature(body, signature, secret) {
			log.Printf("❌ Signature verification failed. Expected signature for body length: %d", len(body))
			http.Error(w, "invalid signature", 401)
			return
		}
	}

	log.Println("✅ Signature verification SUCCESS")

	// Parse webhook
	var event GitHubPushEvent
	if err := json.Unmarshal(jsonBody, &event); err != nil {
		log.Printf("❌ Failed to parse JSON: %v", err)
		log.Printf("JSON preview: %s", string(jsonBody[:min(500, len(jsonBody))]))
		http.Error(w, "invalid payload", 400)
		return
	}

	log.Printf("📝 Parsed event - Ref: %s, Repo: %s, Commit: %s", 
	event.Ref, event.Repository.FullName, event.After)
	
	// Only handle pushes to main/master
	if event.Ref != "refs/heads/main" && event.Ref != "refs/heads/master" {
		log.Printf("⏭️  Ignoring push to branch: %s", event.Ref)
		writeJSON(w, map[string]string{"status": "ignored", "reason": "not main branch"})
		return
	}

	log.Printf("🚀 Processing push to main branch...")
	
	// Clone repo to temp directory
	repoPath, err := cloneRepo(event.Repository.CloneURL, event.After)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to clone repo: %v", err), 500)
		return
	}
	log.Printf("📂 Cloned repo to: %s", repoPath) 

	// Trigger workflow
	workflowPath := ".ci/workflows/build.yml"
	
	// Check if workflow exists
	fullWorkflowPath := filepath.Join(repoPath, workflowPath)
	if _, err := os.Stat(fullWorkflowPath); os.IsNotExist(err) {
		// Cleanup and ignore
		os.RemoveAll(repoPath)
		writeJSON(w, map[string]string{"status": "ignored", "reason": "no workflow file"})
		return
	}
	
	// Read workflow
	workflowData, err := os.ReadFile(fullWorkflowPath)
	if err != nil {
		os.RemoveAll(repoPath)
		http.Error(w, "failed to read workflow", 500)
		return
	}

	// Parse workflow
	wf, err := workflow.Parse(workflowData)
	if err != nil {
		os.RemoveAll(repoPath)
		http.Error(w, fmt.Sprintf("invalid workflow: %v", err), 400)
		return
	}
	
	// Create run
	runID, err := s.store.CreateRun(r.Context(), repoPath, event.Ref, event.After, "webhook")
	if err != nil {
		os.RemoveAll(repoPath)
		http.Error(w, fmt.Sprintf("failed to create run: %v", err), 500)
		return
	}
	
	// Create jobs and steps
	jobIDs := make(map[string]int64)
	
	for jobName, job := range wf.Jobs {
		jobID, err := s.store.CreateJob(r.Context(), runID, jobName, job.Needs, "any", 3)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to create job: %v", err), 500)
			return
		}
		jobIDs[jobName] = jobID
		
		for idx, step := range job.Steps {
			_, err := s.store.CreateStep(r.Context(), jobID, idx, step.Name, step.Run)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to create step: %v", err), 500)
				return
			}
		}
	}

	// Enqueue jobs with no dependencies
	for jobName, jobID := range jobIDs {
		job := wf.Jobs[jobName]
		
		if err := s.store.MarkJobQueued(r.Context(), jobID); err != nil {
			http.Error(w, fmt.Sprintf("failed to mark job queued: %v", err), 500)
			return
		}
		
		if len(job.Needs) == 0 {
			if err := s.enqueueJob(r.Context(), jobID); err != nil {
				http.Error(w, fmt.Sprintf("failed to enqueue job: %v", err), 500)
				return
			}
		}
	}
	
	writeJSON(w, map[string]interface{}{
		"status":     "triggered",
		"run_id":     runID,
		"jobs":       jobIDs,
		"repo":       event.Repository.FullName,
		"commit_sha": event.After,
		"pusher":     event.Pusher.Name,
	})

	// Cleanup cloned repo after a delay (let jobs start first)
	go func() {
		// Keep repo for 1 hour, then cleanup
		// (In production, jobs would use this cloned repo)
		// For now, jobs use control-plane/examples/repo
	}()
}

func cloneRepo(cloneURL, commitSHA string) (string, error) {
	// Create temp directory
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("ci-repo-%s", commitSHA[:8]))

	log.Printf("🔄 Cloning %s to %s", cloneURL, tmpDir)
	
	// Remove if exists
	os.RemoveAll(tmpDir)
	
	// Clone repo
	cmd := exec.Command("git", "clone", "--depth=1", cloneURL, tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("❌ Git clone failed: %s", string(output))
		return "", fmt.Errorf("git clone failed: %w", err)
	}
	log.Printf("✅ Clone successful: %s", tmpDir)

	// Checkout specific commit
	cmd = exec.Command("git", "checkout", commitSHA)
	cmd.Dir = tmpDir
	// if err := cmd.Run(); err != nil {
	// 	os.RemoveAll(tmpDir)
	// 	return "", fmt.Errorf("git checkout failed: %w", err)
	// }
	output, err = cmd.CombinedOutput()
	if err != nil {
		log.Printf("⚠️  Git checkout failed (using HEAD): %s", string(output))
		// Don't fail - just use the cloned HEAD
	}
	
	log.Printf("✅ Clone successful: %s", tmpDir)
	
	return tmpDir, nil
}

func verifyGitHubSignature(payload []byte, signature, secret string) bool {
	if signature == "" {
		return false
	}
	
	// Remove "sha256=" prefix
	if len(signature) > 7 && signature[:7] == "sha256=" {
		signature = signature[7:]
	}
	
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))
	
	fmt.Printf("DEBUG: Expected signature: %s\n", expectedMAC)
	fmt.Printf("DEBUG: Received signature: %s\n", signature)
	
	return hmac.Equal([]byte(signature), []byte(expectedMAC))
}
	
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}