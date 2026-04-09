# Build stage
FROM golang:1.25-alpine AS builder

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
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o /bin/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o /bin/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o /bin/tenant ./cmd/tenant
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/healthcheck ./cmd/healthcheck

# Init stage — Alpine-based with shell for one-time setup commands
# Usage: docker compose run --rm init init-meta
#        docker compose run --rm init create --slug default --name "My Company"
FROM alpine:3.21 AS init

WORKDIR /app
COPY --from=builder /bin/tenant ./tenant
COPY --from=builder /bin/healthcheck ./healthcheck
COPY --from=builder /app/db/migrations/ ./db/migrations/
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["./tenant"]

# Runtime stage
FROM gcr.io/distroless/static-debian12

WORKDIR /app

# Copy binaries
COPY --from=builder /bin/server ./server
COPY --from=builder /bin/worker ./worker
COPY --from=builder /bin/tenant ./tenant
COPY --from=builder /bin/healthcheck ./healthcheck

# Copy migration SQL files (needed by /tenant migrate)
COPY --from=builder /app/db/migrations/ ./db/migrations/

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Default to server
ENTRYPOINT ["./server"]
