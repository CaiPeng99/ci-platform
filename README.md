# 🚀 CI Platform

A **distributed CI platform** built from scratch with a microservice architecture. Define workflows in YAML, execute jobs across multiple Rust-powered runners, and stream build logs in real-time to a React dashboard.

> Think GitHub Actions — but self-hosted and fully under your control.


---

## ✨ Features

- **YAML Workflow Definition** — Define CI pipelines in simple YAML files committed to your repo
- **DAG-based Job Orchestration** — Jobs with dependencies are resolved and executed in the correct order using a Directed Acyclic Graph engine
- **Distributed Rust Runners** — Multiple runners can pick up and execute jobs concurrently inside Docker containers
- **Real-time Log Streaming** — Build logs stream live from Docker containers → gRPC → SSE → React dashboard
- **GitHub Webhook Integration** — Automatically trigger pipelines on `git push` with HMAC-SHA256 signature verification
- **Artifact Storage** — Build outputs stored in MinIO using presigned URLs for direct, efficient transfer
- **Polyglot Microservices** — Go for the orchestration brain, Rust for high-performance job execution

---

## 🏛️ Architecture

```
Developer / GitHub
        │
        │  HTTP (curl or webhook)
        ▼
  ┌──────────────┐
  │   Nginx :80  │  ← Single entry point (reverse proxy)
  └──────┬───────┘
         │ proxy_pass /api/* → :8080
         ▼
  ┌─────────────────────────────────────────┐
  │         Control Plane (Go)              │
  │                                         │
  │  REST API :8080   gRPC Server :9090     │
  │  ├─ YAML Parser   ├─ LeaseJob()         │
  │  ├─ DAG Engine    ├─ Log Stream         │
  │  ├─ SSE Broker    └─ Presigned URLs     │
  │  └─ Webhook Handler                     │
  └──────┬──────────┬──────────┬────────────┘
         │          │          │
         ▼          ▼          ▼
   PostgreSQL   RabbitMQ    MinIO
   (state)      (job queue) (artifacts)

  ┌──────────────────────────┐
  │     Rust Runner(s)       │  ← connects to gRPC :9090 directly
  │  ├─ Polls for jobs       │     (bypasses Nginx)
  │  ├─ Executes in Docker   │
  │  ├─ Streams logs → gRPC  │
  │  └─ Uploads artifacts    │
  └──────────────────────────┘
         │
         │ SSE  /api/jobs/{id}/logs/stream
         ▼
  ┌──────────────────────────┐
  │   React Dashboard        │  ← via Nginx :80
  │   Real-time logs & runs  │
  └──────────────────────────┘
```
In a detailed view:

![Architecture diagram](assets/architect.png)
---

## 🔄 How It Works

### Trigger a Build (Manual)
```
curl POST → Nginx:80 → Control Plane:8080
  → Parse YAML → Build DAG
  → PostgreSQL (create run + jobs)
  → RabbitMQ (publish jobs)
  → HTTP 200 returned immediately ✅
```

### GitHub Auto-Trigger
```
git push → GitHub Webhook → Nginx:80
  → Control Plane verifies HMAC-SHA256
  → Clone repo → same as manual trigger
```

### Runner Execution
```
Rust Runner → gRPC:9090 → LeaseJob()
  → Execute steps inside Docker container
  → Capture stdout/stderr
  → Stream logs → gRPC → Control Plane
  → Control Plane: save to PostgreSQL + broadcast via SSE
  → React Dashboard renders logs in real-time
```

### Artifact Upload/Download
```
Rust Runner → gRPC:9090 → request presigned URL
  → Control Plane asks MinIO
  → MinIO returns presigned URL
  → Runner uploads directly to MinIO:9000 (bypasses Control Plane)
  → Client downloads directly from MinIO:9000
```

---

## 📁 Project Structure

```
ci-platform/
├── docker-compose.yml        # Full stack orchestration
├── nginx.conf                # Reverse proxy config
│
├── control-plane/            # Go — orchestration brain
│   ├── cmd/server/           # Main entrypoint
│   ├── internal/
│   │   ├── api/              # REST API & webhook handlers
│   │   ├── grpc/             # gRPC server (runner communication)
│   │   ├── workflow/         # YAML parser & DAG engine
│   │   ├── queue/            # RabbitMQ integration
│   │   └── store/            # PostgreSQL data layer
│   ├── proto/                # Protobuf definitions
│   └── Dockerfile
│
├── runner/                   # Rust — job executor
│   ├── src/main.rs           # gRPC client + Docker execution
│   ├── Cargo.toml
│   └── Dockerfile
│
└── dashboard/                # React — frontend
    ├── src/
    │   ├── components/       # UI components
    │   └── hooks/            # SSE log streaming hook
    └── Dockerfile
```

---

## 🚀 Getting Started

### Prerequisites

- [Docker](https://www.docker.com/) & Docker Compose
- Go 1.21+ *(for local development)*
- Rust 1.70+ *(for local development)*
- Node.js 18+ *(for local development)*

### Quick Start (Docker Compose)

```bash
# Clone the repository
git clone https://github.com/yourusername/ci-platform.git
cd ci-platform

# Start all services
docker-compose up --build

# Access the dashboard
open http://localhost
```

### Local Development

**1. Start infrastructure only**
```bash
docker-compose up -d postgres rabbitmq minio
```

**2. Run the Control Plane**
```bash
cd control-plane
DATABASE_URL="postgres://postgres:postgres@localhost:5432/cicd?sslmode=disable" \
RABBITMQ_URL="amqp://guest:guest@localhost:5672/" \
MINIO_ENDPOINT="localhost:9000" \
MINIO_ACCESS_KEY="minioadmin" \
MINIO_SECRET_KEY="minioadmin" \
go run ./cmd/server
```

**3. Start a Rust Runner**
```bash
cd runner
RUNNER_ID="rust-runner-1" \
CONTROL_PLANE_ADDR="http://localhost:9090" \
cargo run --release
```

**4. Launch the Dashboard**
```bash
cd dashboard
npm install
npm run dev
```

---

## 📝 Workflow Example

Create a `.ci/workflows/build.yml` in your repository:

```yaml
name: Build and Test

jobs:
  build:
    steps:
      - name: Build
        run: |
          echo "Building..."
          go build -o app .

  test:
    needs: [build]        # DAG dependency: test runs after build
    steps:
      - name: Run Tests
        run: |
          echo "Testing..."
          go test ./...
```

The `needs` field defines the DAG — `test` will only run after `build` succeeds.

---

## ⚙️ Configuration

| Environment Variable | Service | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | Control Plane | — | PostgreSQL connection string |
| `RABBITMQ_URL` | Control Plane | — | RabbitMQ connection string |
| `MINIO_ENDPOINT` | Control Plane | — | MinIO host:port |
| `MINIO_ACCESS_KEY` | Control Plane | `minioadmin` | MinIO access key |
| `MINIO_SECRET_KEY` | Control Plane | `minioadmin` | MinIO secret key |
| `GITHUB_WEBHOOK_SECRET` | Control Plane | — | HMAC secret for webhook verification |
| `RUNNER_ID` | Runner | — | Unique runner identifier |
| `CONTROL_PLANE_ADDR` | Runner | — | gRPC address of the control plane |

---

## 🛠️ Tech Stack

| Layer | Technology | Role |
|---|---|---|
| Control Plane | Go + gRPC | Orchestration, REST API, SSE broadcaster |
| Job Execution | Rust | High-performance Docker-based job runner |
| Frontend | React | Real-time dashboard with SSE log streaming |
| Job Queue | RabbitMQ | Async job distribution |
| Database | PostgreSQL | Run/job state persistence |
| Artifact Storage | MinIO | S3-compatible build artifact storage |
| Reverse Proxy | Nginx | Single entry point, routes HTTP traffic |
| Containerization | Docker Compose | Full stack orchestration |

---

## 🔑 Key Design Decisions

**Why Go for the Control Plane?**
Go's goroutines make it trivial to run the REST server, gRPC server, and SSE broadcaster concurrently in a single binary with minimal resource usage.

**Why Rust for the Runner?**
Rust gives memory safety guarantees and zero-cost abstractions — critical for a runner that executes untrusted code inside Docker containers at high throughput.

**Why does the Runner bypass Nginx for gRPC?**
Runners connect directly to gRPC `:9090` instead of going through Nginx because runners need persistent, long-lived streaming connections. Nginx requires extra configuration to proxy gRPC well.

**Why presigned URLs for artifacts?**
Direct client-to-MinIO transfers bypass the Control Plane entirely, reducing its load and improving transfer speeds for large artifacts.

---

## 📄 License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.

---

<p align="center">Built with Go 🐹 and Rust 🦀</p>
