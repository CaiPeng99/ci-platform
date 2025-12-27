package main

import (
	"context"
	"log"
	"net"
	"os"
	"time"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/grpc"

	"ci-platform/control-plane/internal/queue"       // ✅ Add queue package
	"ci-platform/control-plane/internal/runnergrpc"
	"ci-platform/control-plane/internal/store/pgstore"
	"ci-platform/control-plane/proto/runnerpb"
	"ci-platform/control-plane/internal/logstream"
	"ci-platform/control-plane/internal/httpapi"
)

func main() {
	ctx := context.Background()

	// 1. Connect to Postgres
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL not set")
	}
	
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()
	log.Println("✅ Connected to PostgreSQL")

	store := pgstore.New(pool)

	// 2. Connect to RabbitMQ
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		log.Fatal("RABBITMQ_URL not set")
	}
	
	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()
	log.Println("✅ Connected to RabbitMQ")

	

	// 3. Start RabbitMQ Consumer (from queue package)
	consumer, err := queue.StartJobConsumer(ctx, conn, "jobs.runnable", 10, 100)
	if err != nil {
		log.Fatalf("Failed to start consumer: %v", err)
	}
	defer consumer.Close()
	log.Println("✅ RabbitMQ consumer started on queue: jobs.runnable")

	// Added: reate a channel for HTTP API to publish jobs
	rabbitCh, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to create RabbitMQ channel: %v", err)
	}
	defer rabbitCh.Close()
	
	// 4. Create log streaming hub
	hub := logstream.NewHub()
	log.Println("✅ Log streaming hub created")


		

	// 5. Start gRPC Server (from runnergrpc package)(pass hub to runner server)
	runnerServer := runnergrpc.NewRunnerServer(store, consumer.Deliveries(), 60*time.Second, hub, rabbitCh)
	
	grpcServer := grpc.NewServer()
	runnerpb.RegisterRunnerGatewayServer(grpcServer, runnerServer)

	listener, err := net.Listen("tcp", ":9090")
	if err != nil {
		log.Fatalf("Failed to listen on :9090: %v", err)
	}

	// Start gRPC server in a goroutine
	go func() {
		log.Println("✅ gRPC server listening on :9090")
		if err := grpcServer.Serve(listener); err != nil {
			log.Fatalf("gRPC server failed: %v", err)
		}
	}()

	// 6. Start HTTP API server (pass hub to http api server)
	httpServer := httpapi.New(store, hub, rabbitCh)
	httpHandler := httpServer.Handler()

	httpAddr := ":8080"
	log.Printf("✅ HTTP REST + SSE server listening on %s", httpAddr)
	log.Println("Control plane ready to serve requests...")
	log.Println("  - gRPC: :9090")
	log.Println("  - HTTP: :8080")
	
	if err := http.ListenAndServe(httpAddr, httpHandler); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}	
}