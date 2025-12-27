// package main

// import (
// 	"encoding/json"
// 	"log"
// 	"net/http"
// 	"os"
// 	"time"

// 	"github.com/go-chi/chi/v5"            // the routing library (handles URL routing)
// 	"github.com/go-chi/chi/v5/middleware" // middleware for logging, recovering from panics, etc.

// 	// "ci-platform/api/db"
// 	// "ci-platform/api/handlers"
// )

// func main() {
// 	// Read env vars (you can move this into config package if you want)
// 	dsn := os.Getenv("DATABASE_URL")
// 	if dsn == "" {
// 		// dsn = "host=localhost user=ci_user password=ci_pass dbname=ci_db port=5432 sslmode=disable"
// 		dsn = "postgres://ci:ci@localhost:5432/ci?sslmode=disable" // default for local dev
// 	}

// 	database, err = db.OpenDB(dsn)
// 	if err != nil {
// 		log.Fatalf("Cannot connect to db: %v", err) // fatal error if we can't connect
// 	}
// 	defer database.Close() // ensure we close the db when main exits

// 	r := chi.NewRouter() // create a new router
// 	r.use(middleware.Logger)         // log each request
// 	r.use(middleware.Recoverer)	  // recover from panics

// 	//Healhth Check Endpoint
// 	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
// 		// response := map[string]string{"status": "ok"}
// 		// w.Header().Set("Content-Type", "application/json")
// 		// json.NewEncoder(w).Encode(response)
// 		w.Header().Set("Content-Type", "application/json")  // Add this!
// 		w.WriteHeader(http.StatusOK)
// 		w.Write([]byte(`{"status":"ok"}`))
// 	})

// 	// Register handlers, injecting db dependency
// 	projectHandler := handlers.NewProjectHandler(database)
// 	runHandler := handlers.NewRunHandler(database)
	
// 	// Define routes
// 	r.Route("/projects", func(r chi.Router) {
// 		r.Get("/", projectHandler.ListProjects)       // GET /projects
// 		r.Post("/", projectHandler.CreateProject)     // POST /projects
// 		r.Get("/{projectID}/runs", runHandler.CreateRunForProject) // GET /projects/{projectID}
// 		// Add more project routes as needed
// 	})

// 	r.Route("/runs", func(r chi.Router) {
// 		// r.Get("/", runHandler.ListRuns)			   // GET /runs
// 		r.Get("/{runID}", runHandler.GetRunByID)          // GET /runs/{runID}
// 		// Add more run routes as needed
// 	})

// 	srv := &http.Server{
// 		Handler:      r,
// 		Addr:         ":8080",
// 		WriteTimeout: 15 * time.Second,
// 		ReadTimeout:  15 * time.Second,
// 		IdleTimeout:  60 * time.Second,
// 	}

// 	log.Println("Starting server on :8080")
// 	if err := srv.ListenAndServe(); err != nil  && err != http.ErrServerClosed {
// 		log.Fatalf("Server failed to start: %v", err)
// 	}


// 	// http.HandleFunc("/health", healthHandler) // when someone visits /health, call healthHandler
// 	// http.ListenAndServe(":8080", nil) // start the server on port 8080, nil means use default handler
// }

package store