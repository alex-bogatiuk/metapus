# Metapus — Single-Tenant Deployment

Deploy Metapus (backend + frontend + worker) from source using Docker Compose.

## Prerequisites

- Docker & Docker Compose v2
- Ports **5432**, **8080**, **3000** free (configurable in `.env`)

## Quick Start

```bash
# 1. Configure environment
cp .env.example .env
# Edit .env — at minimum set JWT_SECRET

# 2. Build all images
docker compose build

# 3. Start PostgreSQL
docker compose up -d postgres

# 4. Initialize meta-database (creates "tenants" DB + schema)
docker compose --profile init run --rm init init-meta

# 5. Create tenant (creates DB, runs migrations, seeds auth data)
docker compose --profile init run --rm init create --slug default --name "My Company"
# ⬆ Copy the "Tenant ID: ..." value from the output

# 6. Set TENANT_ID in .env
# Open .env and paste the Tenant ID:
#   TENANT_ID=<uuid-from-step-5>

# 7. Start everything
docker compose up -d

# 8. Rebuild frontend with the TENANT_ID baked in
docker compose build frontend
docker compose up -d frontend
```

Open **http://localhost:3000** and log in:
- **Email:** `admin@metapus.io`
- **Password:** `Admin123!`

## Services

| Service | Port | Description |
|---------|------|-------------|
| `postgres` | 5432 | PostgreSQL 17 |
| `metapus-app` | 8080 | Go API server |
| `metapus-worker` | — | Background job processor |
| `frontend` | 3000 | Next.js UI |

## Common Commands

```bash
# View logs
docker compose logs -f metapus-app
docker compose logs -f frontend

# Re-run migrations (after code update)
docker compose --profile init run --rm init migrate --all

# List tenants
docker compose --profile init run --rm init list

# Rebuild after code changes
docker compose build && docker compose up -d

# Stop everything
docker compose down

# Stop and remove volumes (DELETES DATA)
docker compose down -v
```

## Configuration

See `.env.example` for all available options.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JWT_SECRET` | **yes** | — | Secret key for JWT tokens |
| `TENANT_ID` | **yes** | — | UUID from `init create` step |
| `POSTGRES_PASSWORD` | no | `metapus` | PostgreSQL password |
| `LOG_LEVEL` | no | `info` | Log level (debug/info/warn/error) |
| `APP_PORT` | no | `8080` | API server host port |
| `FRONTEND_PORT` | no | `3000` | Frontend host port |
| `POSTGRES_PORT` | no | `5432` | PostgreSQL host port |
