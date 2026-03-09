# Database Setup Guide

This guide describes how to set up the Metapus database from scratch using Docker and apply migrations.

## Prerequisites

- Docker & Docker Compose
- Go (to install `goose` migration tool)

## 1. Start Database

We use PostgreSQL 16. Start it using docker-compose:

```bash
docker-compose up -d postgres
```

This will start a Postgres container on port 5432.
Credentials (from docker-compose.yml):
- User: `postgres`
- Password: `postgres`
- Database: `metapus`

## 2. Install Migration Tool (Goose)

If you haven't installed `goose` yet:

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

## 3. Apply Migrations

Run the migrations to Initialize the schema. UUIDv7 support is configured automatically in the first migration (`00001_init_extensions.sql`).

```bash
cd db/migrations
goose postgres "user=postgres password=postgres dbname=metapus sslmode=disable" up
```

## 4. Verify Installation

You can connect to the database to verify schemas are created for Database-per-Tenant mode.

```bash
docker exec -it metapus-postgres psql -U postgres -d metapus
```

Run `\d users` or `\d cat_organizations` to verify schemas.

## UUIDv7 Support

The project uses UUIDv7 for primary keys.
- **PostgreSQL implementation**: Handled by a custom PL/pgSQL function `gen_random_uuid_v7()` defined in `00001_init_extensions.sql`.
- **Go implementation**: Ensure your Go code uses a library compatible with UUIDv7 (e.g., `google/uuid` with v7 support or specific v7 libraries) when generating IDs application-side, though the database will auto-generate them if omitted.

## Fresh Start (Reset)

To completely reset the database:

```bash
docker-compose down -v
docker-compose up -d postgres
# Wait for DB to be ready
goose -dir db/migrations postgres "user=postgres password=postgres dbname=metapus sslmode=disable" up
```
