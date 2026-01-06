# Build stage - Use latest Rust
FROM rust:alpine AS builder

WORKDIR /app/runner

# Install build dependencies
RUN apk add --no-cache musl-dev protobuf-dev protoc openssl-dev openssl-libs-static

# Copy proto files to match expected path (../control-plane/proto)
COPY control-plane/proto /app/control-plane/proto

# Copy runner files
COPY runner/Cargo.toml ./
COPY runner/build.rs ./

# Create dummy src for dependency caching
RUN mkdir -p src && echo "fn main() {}" > src/main.rs

# Build dependencies only
RUN cargo build --release 2>/dev/null || true

# Remove dummy files
RUN rm -rf src

# Copy actual runner source code
COPY runner/src ./src

# Build the actual binary
RUN cargo build --release

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    git \
    docker-cli \
    bash \
    curl

# Copy binary from builder
COPY --from=builder /app/runner/target/release/ci-runner .

ENTRYPOINT ["./ci-runner"]