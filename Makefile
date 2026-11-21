.PHONY: build lint lint-go lint-frontend typecheck test-unit test-integration test migrate migrate-extensions seed server frontend check check-extensions check-all changelog

# Default environment variables for local development
export TENANT_DB_USER ?= metapus
export TENANT_DB_PASSWORD ?= metapus
VERSION ?= dev
LDFLAGS := -ldflags="-X main.Version=$(VERSION) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)"

# Build
build:
	go build $(LDFLAGS) -o bin/ ./cmd/server ./cmd/worker ./cmd/tenant ./cmd/seed ./cmd/crypto-server ./cmd/crypto-worker ./cmd/crypto-tenant ./cmd/crypto-seed

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
	go run ./cmd/integration-test

.PHONY: test-pdf
test-pdf:
	go run ./cmd/pdf-renderer-smoke

test: test-unit

# Database (crypto tenants: the platform's, the crypto and the extensions' migrations)
migrate:
	go run ./cmd/crypto-tenant migrate --all

# Extension migrations (db/extensions/) are applied by every tenant migration
migrate-extensions: migrate

seed:
	go run ./cmd/crypto-seed

# Run
server:
	go run ./cmd/crypto-server

frontend:
	cd frontend && npm run dev

# Extension compatibility check
check-extensions:
	@echo "=== Checking extension compatibility ==="
	@if [ -n "$$(find extensions -name '*.go' 2>/dev/null)" ]; then \
		go build ./extensions/... && \
		go vet ./extensions/... && \
		echo "=== Extensions OK ==="; \
	else \
		echo "=== No extensions found, skipping ==="; \
	fi

# Full check (CI-style)
check: build lint typecheck test-unit test-http-contracts

.PHONY: generate-http-contracts test-http-contracts
generate-http-contracts:
	go run ./cmd/http-contracts

test-http-contracts:
	go run ./cmd/http-contracts -check

# Full check including extensions
check-all: check check-extensions

# Show breaking changes in extension API
changelog:
	@echo "=== Breaking changes in Extension API ==="
	@git diff HEAD~1 -- internal/platform/ internal/infrastructure/http/v1/catalog_factory.go internal/infrastructure/http/v1/document_factory.go internal/infrastructure/http/v1/factory_registry.go | grep "^[+-].*interface\|^[+-].*func\|^[+-].*type" || echo "No breaking changes detected"
