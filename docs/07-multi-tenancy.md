# Multi-Tenancy: Database-per-Tenant

> Каждый тенант работает в **своей отдельной PostgreSQL базе данных**. Физическая изоляция без фильтрации по tenant в SQL-запросах.

---

## Общая схема

```
HTTP Request (X-Tenant-ID: "550e8400-...")
     │
     ▼
TenantDB Middleware
├── uuid.Parse(header)
├── manager.GetPool(ctx, tenantID)
│   ├── Fast path: sync.Map.Load → existing pool
│   └── Slow path: registry lookup → pgxpool.New → ManagedPool
├── managedPool.AcquireRef()  (reference counting)
├── txManager := NewTxManagerFromRawPool(pool)
└── ctx = WithPool + WithTxManager + WithTenant
     │
     ▼
Handler → Service → Repository
├── txm := tenant.MustGetTxManager(ctx)
├── txm.RunInTransaction(ctx, fn)
└── querier.Exec(ctx, "INSERT INTO cat_xxx ...")
     │
     ▼
PostgreSQL: mt_acme (tenant-specific database)
```

---

## Идентификация тенанта

- **HTTP заголовок:** `X-Tenant-ID` (UUID из meta-database `tenants.id`)
- **JWT токен:** содержит `tid` (tenant ID) — проверяется на совпадение с заголовком
- **Meta-database:** `metapus_meta` — хранит метаданные тенантов (slug, db_name, db_host, status, plan)

---

## TenantDB Middleware — точка входа

Middleware `TenantDB` выполняет 5 шагов для каждого HTTP-запроса:

1. **Извлечение UUID** из `X-Tenant-ID`. Невалидный → 400, отсутствующий → 401
2. **Получение пула** через `Manager.GetPool()`. Ошибки маппятся на HTTP-статусы:
   - `ErrTenantNotFound` → 404
   - `ErrTenantNotActive` → 403
   - `ErrMaxPoolLimit` → 503
3. **Reference counting** — `AcquireRef/ReleaseRef` отслеживает активные запросы
4. **Создание TxManager** — `NewTxManagerFromRawPool(pool)` для каждого запроса
5. **Инжекция в context** — Pool, TxManager и Tenant доступны через `context.Context`

---

## Manager.GetPool — управление пулами

### Fast path (lock-free)
```
pools.Load(tenantID) → found → mp.Touch() → return mp
```

### Slow path (создание нового пула)
1. Проверка лимита: `poolCount >= MaxTotalPools` → `ErrMaxPoolLimit`
2. Lookup в Registry: `SELECT * FROM tenants WHERE id = $1` (meta-database)
3. Проверка статуса: только `active` тенанты могут принимать запросы
4. Построение DSN: `postgres://user:pass@host:port/mt_{slug}`
5. Создание `pgxpool.Pool` с конфигурацией (MaxConns=10, MinConns=2, HealthCheckPeriod)
6. Обёртка в `ManagedPool` (lastUsed, refCount, unhealthySince)
7. Атомарное сохранение: `sync.Map.LoadOrStore` — race protection

---

## Context Chain — от middleware до SQL

```
Middleware:  ctx = tenant.WithTxManager(ctx, txManager)
Handler:    service.Create(ctx, entity)       // ctx передаётся как есть
Service:    txm := s.getTxManager(ctx)        // из context или static (тесты)
            txm.RunInTransaction(ctx, fn)
Repository: querier := getTxManager(ctx).GetQuerier(ctx)
            querier.Exec(ctx, "INSERT INTO cat_xxx ...")
```

**Handler не знает о multi-tenancy** — просто передаёт ctx. Изоляция прозрачна.

---

## Защита от подмены тенанта

Auth middleware (выполняется **после** TenantDB) проверяет:
- `resolvedTenantID` (из X-Tenant-ID заголовка) == `user.TenantID` (из JWT `tid` claim)
- При несовпадении → `403 Forbidden: tenant mismatch`

Это предотвращает атаку: валидный JWT одного тенанта + X-Tenant-ID другого.

---

## Фоновые процессы

### Eviction Loop (каждые ~15 мин)
- Обходит все пулы через `sync.Map.Range`
- **Никогда** не закрывает пулы с `refCount > 0`
- Закрывает unhealthy пулы без активных запросов
- Закрывает idle пулы (lastUsed < threshold)

### Health Check Loop (каждую ~1 мин)
- `pool.Ping(ctx)` для каждого пула
- Первый failed ping → маркировка `unhealthySince`
- Успешный ping после failure → recovery (unhealthySince = 0)
- Failed ping + refCount == 0 → немедленное закрытие

### Graceful Shutdown
```
Manager.Close()
├── m.cancel()  — отмена context фоновых горутин
├── m.wg.Wait() — ожидание evictionLoop + healthCheckLoop
└── pools.Range → mp.pool.Close() для всех пулов
```

---

## Stats — мониторинг

Endpoint `/health/tenants` предоставляет:
- `TotalPools` — общее количество активных пулов
- Per-tenant: TotalConns, IdleConns, AcquiredConns, ActiveRefs, LastUsed

---

## Важные следствия

- **Нет "shared DB" режима** — данные разных клиентов не смешиваются
- **SQL без фильтрации по tenant** — изоляция обеспечивается выбором базы
- **JWT содержит tenant ID** — middleware проверяет совпадение
- **Не добавляй tenant discriminator колонки** в бизнес-таблицы

---

## Переменные окружения

```bash
META_DATABASE_URL=postgres://user:pass@host:5432/metapus_meta
TENANT_DB_USER=postgres
TENANT_DB_PASSWORD=password
TENANT_POOL_IDLE_TIMEOUT=10m
TENANT_MAX_POOLS=100
TENANT_MAX_CONNS_PER_POOL=10
```

---

## CLI управления тенантами

```bash
go run cmd/tenant/main.go create --slug=acme --name="ACME Corp"
go run cmd/tenant/main.go list
go run cmd/tenant/main.go migrate              # все тенанты
go run cmd/tenant/main.go migrate --id <uuid>  # один тенант
go run cmd/tenant/main.go suspend <uuid>
go run cmd/tenant/main.go activate <uuid>
```

---

## Связанные документы

- [08-auth-and-security.md](08-auth-and-security.md) — tenant match в auth middleware
- [11-transactions.md](11-transactions.md) — TxManager создание и использование
- [13-request-lifecycle.md](13-request-lifecycle.md) — место TenantDB в middleware chain
- [18-migration-status.md](18-migration-status.md) — статус миграции на DB-per-Tenant
