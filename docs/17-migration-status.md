# Статус миграции на Database-per-Tenant

> Текущее состояние миграции, настройка окружения, переменные среды и CLI-команды.

---

## Статус: Завершено (14 января 2026)

Полная миграция на Database-per-Tenant модель завершена. Все компоненты обновлены.

---

## Завершённые компоненты

### Core Infrastructure
- `internal/core/tenant/types.go` — Tenant struct с данными подключения
- `internal/core/tenant/context.go` — Context утилиты: Pool, TxManager, Tenant
- `internal/core/tenant/registry.go` — PostgresRegistry для meta-database
- `internal/core/tenant/manager.go` — MultiTenantManager с connection pooling
- `internal/core/entity/base.go` — BaseEntity без tenant discriminator
- `internal/core/entity/catalog.go` — NewCatalog без tenantID
- `internal/core/entity/document.go` — NewDocument без tenantID

### HTTP Layer
- `middleware/tenant.go` — TenantDB middleware
- `router.go` — Обновлённая инициализация сервисов
- Все catalog handlers (currency, counterparty, nomenclature, unit, warehouse)
- Все document handlers (goods_receipt, goods_issue)
- `handlers/health.go` — MultiTenantHealthHandler

### DTOs
- Все `ToEntity()` методы обновлены — не требуют tenantID

### Domain Services
- `domain/service.go` — CatalogService получает TxManager из context
- Все catalog services — без TxManager в конструкторе
- Все document services — TxManager из context
- `posting/engine.go` — TxManager из context

### Repositories
- `catalog_repo/base.go` — getTxManager из context
- `document_repo/base.go` — getTxManager из context
- Все concrete catalog/document repos
- `register_repo/stock.go` — без tenant фильтрации
- `report_repo/reports.go` — context-based TxManager
- Все auth repos (user, role, permission, token) — context-based

### Entry Points
- `cmd/server/main.go` — MetaPool, TenantManager инициализация
- `cmd/worker/main.go` — MultiTenantWorker для background задач
- `cmd/tenant/main.go` — CLI для управления тенантами

### Migrations
- `db/meta/00001_tenants.sql` — Meta-database схема
- `db/migrations/*` — Tenant database schema (применяется per-tenant)

### Tests
- `struct_utils_test.go` — обновлён для новых конструкторов entity
- `struct_utils_bench_test.go` — обновлён

### Build
```
go build ./... — SUCCESS
go test ./... -short — SUCCESS
```

---

## Настройка окружения

### Prerequisites
- Docker & Docker Compose
- Go (для `goose` migration tool)
- PostgreSQL 16+

### Запуск БД
```bash
docker-compose up -d postgres
```
Credentials (из docker-compose.yml): user=`postgres`, password=`postgres`, database=`metapus`

### Установка Goose
```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

### Применение миграций
```bash
cd db/migrations
goose postgres "user=postgres password=postgres dbname=metapus sslmode=disable" up
```

### UUIDv7
PostgreSQL: `gen_random_uuid_v7()` — custom PL/pgSQL функция из `00001_init_extensions.sql`.
Go: `google/uuid` с поддержкой v7.

### Сброс БД
```bash
docker-compose down -v
docker-compose up -d postgres
goose -dir db/migrations postgres "..." up
```

---

## Переменные окружения

```bash
# Meta-database
META_DATABASE_URL=postgres://user:pass@host:5432/metapus_meta

# Tenant database credentials
TENANT_DB_USER=postgres
TENANT_DB_PASSWORD=password

# MultiTenantManager
TENANT_POOL_IDLE_TIMEOUT=10m
TENANT_MAX_POOLS=100
TENANT_MAX_CONNS_PER_POOL=10
```

---

## CLI — управление тенантами

```bash
# Создать нового тенанта
go run cmd/tenant/main.go create --slug=acme --name="ACME Corp"

# Список тенантов
go run cmd/tenant/main.go list

# Миграции для всех тенантов
go run cmd/tenant/main.go migrate

# Миграции для одного тенанта
go run cmd/tenant/main.go migrate --id <tenant-uuid>

# Приостановить тенанта
go run cmd/tenant/main.go suspend <tenant-uuid>

# Активировать тенанта
go run cmd/tenant/main.go activate <tenant-uuid>
```

---

## Полезные команды

```bash
# Компиляция
go build ./cmd/server

# Запуск тестов
go test ./...

# Миграции
goose -dir db/migrations postgres "..." up

# Линтер
golangci-lint run

# Форматирование
go fmt ./...
```

---

## Связанные документы

- [07-multi-tenancy.md](07-multi-tenancy.md) — архитектура Database-per-Tenant
- [17-development-rules.md](17-development-rules.md) — правила миграций
- [03-project-structure.md](03-project-structure.md) — структура cmd/ и db/
