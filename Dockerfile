# Build stage
FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binaries
ARG VERSION=dev
ARG BUILD_PARALLELISM=2
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o /bin/server ./cmd/crypto-server
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o /bin/worker ./cmd/crypto-worker
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o /bin/tenant ./cmd/tenant
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w" -o /bin/seed ./cmd/seed
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w" -o /bin/crypto-preflight ./cmd/crypto-preflight
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w" -o /bin/integration-config ./cmd/integration-config
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w" -o /bin/crypto-record-wallet-funding ./cmd/crypto-record-wallet-funding
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w" -o /bin/public-test-bootstrap ./cmd/public-test-bootstrap
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w" -o /bin/aml-testnet-config ./cmd/aml-testnet-config
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w" -o /bin/aml-testnet-bootstrap ./cmd/aml-testnet-bootstrap
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w" -o /bin/aml-pool-controller ./cmd/aml-pool-controller
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w" -o /bin/aml-wallet-provision ./cmd/aml-wallet-provision
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w" -o /bin/healthcheck ./cmd/healthcheck

RUN mkdir -p /monitor-state && chmod 0700 /monitor-state
RUN CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_PARALLELISM} -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o /bin/crypto-ingestion-monitor ./cmd/crypto-ingestion-monitor

# Init stage — Alpine-based with shell for one-time setup commands
# Usage: docker compose run --rm init init-meta
#        docker compose run --rm init create --slug default --name "My Company"
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d AS init

WORKDIR /app
COPY --from=builder /bin/tenant ./tenant
COPY --from=builder /bin/seed ./seed
COPY --from=builder /bin/crypto-preflight ./crypto-preflight
COPY --from=builder /bin/integration-config ./integration-config
COPY --from=builder /bin/crypto-record-wallet-funding ./crypto-record-wallet-funding
COPY --from=builder /bin/public-test-bootstrap ./public-test-bootstrap
COPY --from=builder /bin/aml-testnet-config ./aml-testnet-config
COPY --from=builder /bin/aml-testnet-bootstrap ./aml-testnet-bootstrap
COPY --from=builder /bin/aml-pool-controller ./aml-pool-controller
COPY --from=builder /bin/aml-wallet-provision ./aml-wallet-provision
COPY --from=builder /bin/healthcheck ./healthcheck
COPY --from=builder /app/db/migrations/ ./db/migrations/
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["./tenant"]

# Runtime stage
FROM gcr.io/distroless/static-debian12@sha256:a9fcaedd4c9b59e12dd65d954f0b5044f19b0647a8a3712e77205df9e7b102cd AS runtime

WORKDIR /app

# Copy binaries
COPY --from=builder /bin/server ./server
COPY --from=builder /bin/worker ./worker
COPY --from=builder /bin/tenant ./tenant
COPY --from=builder /bin/seed ./seed
COPY --from=builder /bin/crypto-preflight ./crypto-preflight
COPY --from=builder /bin/integration-config ./integration-config
COPY --from=builder /bin/crypto-record-wallet-funding ./crypto-record-wallet-funding
COPY --from=builder /bin/public-test-bootstrap ./public-test-bootstrap
COPY --from=builder /bin/healthcheck ./healthcheck

# Copy migration SQL files (needed by /tenant migrate)
COPY --from=builder /app/db/migrations/ ./db/migrations/

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Distroless provides the fixed nonroot user/group (65532). The runtime does
# not need to mutate application files or bind privileged ports.
USER nonroot:nonroot

# Default to server
ENTRYPOINT ["./server"]

# Independent read-only ingestion monitor with ERP alert delivery.
FROM gcr.io/distroless/static-debian12@sha256:a9fcaedd4c9b59e12dd65d954f0b5044f19b0647a8a3712e77205df9e7b102cd AS ingestion-monitor
WORKDIR /app
COPY --from=builder /bin/crypto-ingestion-monitor ./crypto-ingestion-monitor
COPY --from=builder /bin/healthcheck ./healthcheck
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --chown=65532:65532 --from=builder /monitor-state /var/lib/metapus/ingestion-monitor
USER nonroot:nonroot
ENTRYPOINT ["./crypto-ingestion-monitor"]

# Dedicated provisioning consumer; isolated from signing workers.
FROM gcr.io/distroless/static-debian12@sha256:a9fcaedd4c9b59e12dd65d954f0b5044f19b0647a8a3712e77205df9e7b102cd AS aml-pool-controller
WORKDIR /app
COPY --from=builder /bin/aml-pool-controller ./aml-pool-controller
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
USER nonroot:nonroot
ENTRYPOINT ["./aml-pool-controller"]

# Keep the default image as the API/worker runtime.
FROM runtime AS final
