# Database-per-Tenant Migration Status

## ✅ Completed (January 14, 2026)

### Core Infrastructure
- [x] `internal/core/tenant/types.go` - Tenant struct with database connection info
- [x] `internal/core/tenant/context.go` - Context utilities for Pool, TxManager, Tenant
- [x] `internal/core/tenant/registry.go` - PostgresRegistry for meta-database
- [x] `internal/core/tenant/manager.go` - MultiTenantManager with connection pooling
- [x] `internal/core/entity/base.go` - BaseEntity without per-tenant discriminator fields
- [x] `internal/core/entity/catalog.go` - NewCatalog without tenantID parameter
- [x] `internal/core/entity/document.go` - NewDocument without tenantID parameter

### HTTP Layer
- [x] `internal/infrastructure/http/v1/middleware/tenant.go` - TenantDB middleware
- [x] `internal/infrastructure/http/v1/router.go` - Updated service instantiation
- [x] `internal/infrastructure/http/v1/handlers/catalog.go` - MapCreateDTO without tenantID
- [x] `internal/infrastructure/http/v1/handlers/document.go` - MapCreateDTO without tenantID
- [x] `internal/infrastructure/http/v1/handlers/health.go` - MultiTenantHealthHandler
- [x] All catalog handlers updated (currency, counterparty, nomenclature, unit, warehouse)
- [x] All document handlers updated (goods_receipt, goods_issue, inventory)

### DTOs
- [x] All `ToEntity()` methods updated to not require tenantID
- [x] `internal/infrastructure/http/v1/dto/*.go` - All Create*Request DTOs

### Domain Services
- [x] `internal/domain/service.go` - CatalogService gets TxManager from context
- [x] `internal/domain/catalogs/currency/service.go` - No TxManager in constructor
- [x] `internal/domain/catalogs/counterparty/service.go` - No TxManager in constructor
- [x] `internal/domain/catalogs/nomenclature/service.go` - No TxManager in constructor
- [x] `internal/domain/catalogs/warehouse/service.go` - No TxManager in constructor
- [x] `internal/domain/catalogs/unit/service.go` - No TxManager in constructor
- [x] `internal/domain/documents/goods_receipt/service.go` - TxManager from context
- [x] `internal/domain/documents/goods_issue/service.go` - TxManager from context
- [x] `internal/domain/documents/inventory/service.go` - TxManager from context
- [x] `internal/domain/registers/stock/service.go` - No TxManager needed
- [x] `internal/domain/posting/engine.go` - TxManager from context

### Repositories (Context-based TxManager)
- [x] `internal/infrastructure/storage/postgres/catalog_repo/base.go` - getTxManager from context
- [x] `internal/infrastructure/storage/postgres/document_repo/base.go` - getTxManager from context
- [x] All concrete catalog repos (currency, counterparty, nomenclature, warehouse, unit)
- [x] All concrete document repos (goods_receipt, goods_issue, inventory)
- [x] `internal/infrastructure/storage/postgres/register_repo/stock.go` - No tenant filtering in queries
- [x] `internal/infrastructure/storage/postgres/report_repo/reports.go` - Context-based TxManager

### Auth Repositories
- [x] `internal/infrastructure/storage/postgres/auth_repo/user.go` - Context-based repositories (tenant comes from selected database)
- [x] `internal/infrastructure/storage/postgres/auth_repo/role.go` - Context-based repositories (tenant comes from selected database)
- [x] `internal/infrastructure/storage/postgres/auth_repo/permission.go` - Context-based
- [x] `internal/infrastructure/storage/postgres/auth_repo/token.go` - Context-based

### Entry Points
- [x] `cmd/server/main.go` - MetaPool, TenantManager initialization
- [x] `cmd/worker/main.go` - MultiTenantWorker for background tasks
- [x] `cmd/tenant/main.go` - CLI for tenant management

### Migrations
- [x] `db/meta/00001_tenants.sql` - Meta-database schema for tenants
- [x] `db/migrations/*` - Tenant database schema (applied per-tenant)

### Tests
- [x] `internal/infrastructure/storage/postgres/struct_utils_test.go` - Updated for new entity constructors
- [x] `internal/infrastructure/storage/postgres/struct_utils_bench_test.go` - Updated for new entity constructors

## Architecture Summary

### Database-per-Tenant Model
- Each tenant has their own PostgreSQL database
- Meta-database stores tenant metadata (slug, db_name, db_host, status, etc.)
- Physical database isolation - no tenant filtering in SQL queries

### Request Flow
1. HTTP Request with `X-Tenant-ID` header
2. `TenantDB` middleware:
   - Resolves tenant by **tenantID (UUID)** from meta-database
   - Obtains/creates connection pool from MultiTenantManager
   - Creates request-scoped TxManager
   - Injects Pool, TxManager, Tenant into context
3. Handlers/Services/Repositories get TxManager from context
4. All database operations use tenant-specific pool

### MultiTenantManager Features
- Lazy loading of connection pools
- Idle pool eviction (configurable timeout)
- Health checks for active pools
- Max pool limit (default: 100)
- Reference counting for graceful shutdown
- Automatic pool cleanup on Close()

### New Environment Variables
```bash
# Meta-database connection
META_DATABASE_URL=postgres://user:pass@host:5432/metapus_meta

# Tenant database credentials (used to connect to tenant DBs)
TENANT_DB_USER=postgres
TENANT_DB_PASSWORD=password

# MultiTenantManager configuration
TENANT_POOL_IDLE_TIMEOUT=10m
TENANT_MAX_POOLS=100
TENANT_MAX_CONNS_PER_POOL=10
```

### CLI Commands
```bash
# Create new tenant
go run cmd/tenant/main.go create --slug=acme --name="ACME Corp"

# List tenants
go run cmd/tenant/main.go list

# Run migrations for all tenants
go run cmd/tenant/main.go migrate

# Run migrations for one tenant
go run cmd/tenant/main.go migrate --id <tenant-uuid>

# Suspend tenant
go run cmd/tenant/main.go suspend <tenant-uuid>

# Activate tenant
go run cmd/tenant/main.go activate <tenant-uuid>
```

## Build Status
```
✅ go build ./... - SUCCESS
✅ go test ./... -short - SUCCESS
```
