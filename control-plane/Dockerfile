# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy dependency files
# COPY go.mod go.sum ./
COPY go.mod ./
COPY go.sum* ./

RUN go mod download

# Copy source code
# COPY . .
COPY control-plane/ ./control-plane/

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./control-plane/cmd/server/main.go

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates git

WORKDIR /app

COPY --from=builder /app/server .
COPY --from=builder /app/control-plane/examples ./examples   

EXPOSE 8080 9090

CMD ["./server"]