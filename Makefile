.PHONY: build lint test-unit test-integration test migrate seed server frontend check check-extensions check-all changelog

# Default environment variables for local development
export TENANT_DB_USER ?= metapus
export TENANT_DB_PASSWORD ?= metapus
VERSION ?= dev
LDFLAGS := -ldflags="-X main.Version=$(VERSION) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)"

# Build
build:
	go build $(LDFLAGS) ./cmd/server
	go build $(LDFLAGS) ./cmd/worker

# Lint
lint:
	golangci-lint run ./...
	cd frontend && npm run lint

lint-go:
	golangci-lint run ./...

lint-frontend:
	cd frontend && npm run lint

# Type-check frontend
typecheck:
	cd frontend && npx tsc --noEmit

# Tests
test-unit:
	go test -short -race ./...

test-integration:
	go test -race -run Integration ./...

test: test-unit

# Database
migrate:
	go run cmd/tenant/main.go migrate

# Migrate only extension tables (auto-discovers extensions/*/migrations/)
migrate-extensions:
	@echo "=== Extension migrations ==="
	go run cmd/tenant/main.go migrate --all

seed:
	go run cmd/seed/main.go

# Run
server:
	go run ./cmd/server

frontend:
	cd frontend && npm run dev

# Extension compatibility check
check-extensions:
	@echo "=== Checking extension compatibility ==="
	go build ./extensions/...
	go vet ./extensions/...
	@echo "=== Extensions OK ==="

# Full check (CI-style)
check: build lint typecheck test-unit

# Full check including extensions
check-all: check check-extensions

# Show breaking changes in extension API
changelog:
	@echo "=== Breaking changes in Extension API ==="
	@git diff HEAD~1 -- internal/platform/ internal/infrastructure/http/v1/catalog_factory.go internal/infrastructure/http/v1/document_factory.go internal/infrastructure/http/v1/factory_registry.go | grep "^[+-].*interface\|^[+-].*func\|^[+-].*type" || echo "No breaking changes detected"

