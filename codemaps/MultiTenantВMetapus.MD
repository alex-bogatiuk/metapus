# Multi-Tenant Database Isolation в Metapus: От заголовка X-Tenant-ID до изолированной БД

Карта показывает полный путь изоляции данных в Database-per-Tenant архитектуре: резолвинг тенанта, управление пулами соединений, инжекция в context, и фоновые процессы (eviction, health check, graceful shutdown).

**Ключевые точки:**
- Middleware резолвит тенанта из заголовка (1a)
- Manager создаёт/переиспользует пул соединений (2a)
- TxManager инжектируется в context (3a)
- Репозитории получают TxManager из context (4a)
- Фоновые процессы: eviction idle пулов, health check (5a)
- Graceful shutdown с reference counting (6a)

---

## Trace 1: TenantDB Middleware — Резолвинг тенанта из HTTP-заголовка

Middleware `TenantDB` — входная точка multi-tenancy. Извлекает UUID тенанта из заголовка, получает пул соединений, создаёт TxManager и инжектирует всё в request context.

```
HTTP Request с заголовком X-Tenant-ID
│
├── middleware.TenantDB(manager) <-- tenant.go:29
│   │
│   ├── Извлечение tenant UUID из заголовка <-- 1a
│   │   ├── rawTenantID := c.GetHeader("X-Tenant-ID")
│   │   ├── если пуст → apperror.NewValidation("tenant is required") → Abort
│   │   └── uuid.Parse(rawTenantID) — валидация формата UUID
│   │
│   ├── Получение пула из Manager <-- 1b
│   │   └── managedPool, err := manager.GetPool(ctx, tenantID)
│   │       ├── ErrTenantNotFound → 404 Not Found
│   │       ├── ErrTenantNotActive → 403 Forbidden
│   │       ├── ErrMaxPoolLimit → 503 Service Unavailable
│   │       └── success → [переход в Trace 2]
│   │
│   ├── Reference counting <-- 1c
│   │   ├── managedPool.AcquireRef() — инкремент refCount
│   │   └── defer managedPool.ReleaseRef() — декремент при завершении
│   │
│   ├── Создание TxManager <-- 1d
│   │   └── txManager := postgres.NewTxManagerFromRawPool(managedPool.Pool())
│   │
│   └── Инжекция в context <-- 1e
│       ├── ctx = tenant.WithPool(ctx, pool)       — pgxpool.Pool
│       ├── ctx = tenant.WithTxManager(ctx, txm)   — tx.Manager
│       ├── ctx = tenant.WithTenant(ctx, tenant)    — *Tenant struct
│       ├── c.Request = c.Request.WithContext(ctx)
│       ├── c.Set("tenant_uuid", tenant.ID)         — gin context
│       └── c.Set("tx_manager", txManager)           — gin context
```

| ID | Описание | Файл |
|----|----------|------|
| **1a** | Извлечение tenant ID. UUID тенанта берётся из заголовка `X-Tenant-ID`. Невалидный UUID → 400. Отсутствующий заголовок → 401 | `middleware/tenant.go:34` |
| **1b** | Получение пула. `Manager.GetPool()` возвращает `*ManagedPool` — обёртку над `pgxpool.Pool` с lifecycle tracking. Разные ошибки маппятся на разные HTTP-статусы | `middleware/tenant.go:57` |
| **1c** | Reference counting. `AcquireRef/ReleaseRef` отслеживает количество активных запросов, использующих пул. Пул не может быть закрыт, пока `refCount > 0` | `middleware/tenant.go:79` |
| **1d** | Создание TxManager. Для каждого запроса создаётся новый `TxManager`, привязанный к пулу конкретного тенанта | `middleware/tenant.go:83` |
| **1e** | Инжекция в context. Pool, TxManager и Tenant инжектируются в request context. Все нижележащие слои получают их через `tenant.MustGetTxManager(ctx)` | `middleware/tenant.go:86` |

---

## Trace 2: Manager.GetPool — Создание и переиспользование пулов

Manager хранит пулы в `sync.Map` и использует pattern fast path / slow path для минимизации блокировок.

```
Manager.GetPool(ctx, tenantID) <-- manager.go:130
│
├── Fast path: пул уже существует <-- 2a
│   └── pools.Load(tenantID)
│       ├── found → mp.Touch() → return mp
│       └── not found → slow path
│
└── Slow path: createPool(ctx, tenantID) <-- 2b
    │
    ├── Проверка лимита пулов <-- 2c
    │   └── if poolCount >= MaxTotalPools → ErrMaxPoolLimit
    │
    ├── Lookup тенанта в Registry <-- 2d
    │   └── registry.GetByID(ctx, tenantID)
    │       └── SELECT * FROM tenants WHERE id = $1
    │           └── Meta-database (metapus_meta)
    │               ├── not found → ErrTenantNotFound
    │               └── found → Tenant{Slug, DBName, DBHost, Status, Plan}
    │
    ├── Проверка статуса <-- 2e
    │   └── if !tenant.IsActive() → ErrTenantNotActive
    │       └── Status: active | suspended | deleted
    │
    ├── Построение DSN <-- 2f
    │   └── tenant.DSN(user, password)
    │       └── "postgres://user:pass@host:port/mt_acme?sslmode=disable"
    │
    ├── Создание pgxpool.Pool <-- 2g
    │   ├── pgxpool.ParseConfig(dsn)
    │   ├── poolCfg.MaxConns = MaxConnsPerTenant (10)
    │   ├── poolCfg.MinConns = MinConnsPerTenant (2)
    │   ├── poolCfg.HealthCheckPeriod = HealthCheckPeriod
    │   ├── pgxpool.NewWithConfig(createCtx, poolCfg)
    │   └── pool.Ping(createCtx) — верификация соединения
    │
    ├── Обёртка в ManagedPool <-- 2h
    │   └── ManagedPool{pool, tenant, lastUsed, refCount}
    │
    └── Атомарное сохранение (race protection) <-- 2i
        └── pools.LoadOrStore(tenantID, mp)
            ├── stored → poolCount.Add(1) → return mp
            └── loaded (другая горутина успела раньше)
                └── pool.Close() нашего → return theirs
```

| ID | Описание | Файл |
|----|----------|------|
| **2a** | Fast path. `sync.Map.Load` — lock-free чтение. Если пул существует, обновляется `lastUsed` timestamp и возвращается немедленно | `manager.go:132` |
| **2b** | Slow path. Создание нового пула — дорогая операция с сетевым подключением к БД тенанта | `manager.go:143` |
| **2c** | Лимит пулов. `MaxTotalPools` (default 100) ограничивает общее количество одновременных пулов. Защита от OOM при большом числе тенантов | `manager.go:145` |
| **2d** | Lookup в Registry. Тенант ищется в meta-database (`metapus_meta`). Registry — интерфейс, реализация — `PostgresRegistry` | `manager.go:150` |
| **2e** | Проверка статуса. Только `active` тенанты могут принимать запросы. `suspended`/`deleted` → ошибка | `manager.go:158` |
| **2f** | Построение DSN. Формат: `postgres://user:pass@host:port/mt_{slug}`. Имя БД генерируется как `mt_` + slug тенанта | `types.go:55` |
| **2g** | Создание пула. `pgxpool.New` с конфигурацией per-tenant: max/min conns, health check period, connect timeout | `manager.go:179` |
| **2h** | ManagedPool. Обёртка над `pgxpool.Pool` с атомарными счётчиками: `lastUsed` (для eviction), `refCount` (для graceful shutdown), `unhealthySince` (для health check) | `manager.go:190` |
| **2i** | Race protection. `sync.Map.LoadOrStore` атомарно — если другая горутина создала пул первой, наш пул закрывается и используется чужой | `manager.go:197` |

---

## Trace 3: Context Chain — Как TxManager попадает к репозиторию

Цепочка передачи TxManager от middleware до SQL-запроса через context.

```
┌─────────────────────────────────────────────────────────────────────────┐
│              ПЕРЕДАЧА TXMANAGER ЧЕРЕЗ CONTEXT                            │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Middleware (tenant.go)                                                  │
│  └── ctx = tenant.WithTxManager(ctx, txManager) <-- 3a                 │
│      └── context.WithValue(ctx, txManagerKey, txm)                     │
│                                                                          │
│  Handler (catalog.go)                                                   │
│  └── service.Create(ctx, entity)  — ctx передаётся как есть <-- 3b    │
│                                                                          │
│  Service (service.go)                                                   │
│  └── txm := s.getTxManager(ctx) <-- 3c                                │
│      ├── if s.txManager != nil → return s.txManager (static mode)      │
│      └── else → tenant.GetTxManager(ctx) (per-request mode)            │
│          └── ctx.Value(txManagerKey).(tx.Manager)                       │
│                                                                          │
│  Service → txm.RunInTransaction(ctx, fn)                                │
│  └── TxManager.RunInTransaction(ctx, fn) <-- 3d                       │
│      └── pool.BeginTx → pgx.Tx → context.WithValue(ctx, txKey, tx)    │
│                                                                          │
│  Repository (catalog_repo/base.go)                                      │
│  └── querier := r.getTxManager(ctx).GetQuerier(ctx) <-- 3e            │
│      └── postgres.MustGetTxManager(ctx)                                │
│          └── ctx.Value(txManagerKey).(*TxManager)                       │
│              └── GetQuerier(ctx)                                        │
│                  ├── if tx in context → return tx (inside transaction)  │
│                  └── else → return pool (outside transaction)           │
│                                                                          │
│  SQL-запрос                                                              │
│  └── querier.Exec(ctx, "INSERT INTO cat_xxx ...") <-- 3f              │
│      └── Запрос идёт в БД конкретного тенанта                         │
│          └── postgres://...@host:port/mt_acme                           │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

| ID | Описание | Файл |
|----|----------|------|
| **3a** | Инжекция в context. Middleware сохраняет TxManager в request context через `tenant.WithTxManager`. Каждый запрос получает свой TxManager | `tenant/context.go:57` |
| **3b** | Прозрачная передача. Handler не знает о multi-tenancy — просто передаёт ctx сервису | `handlers/catalog.go:144` |
| **3c** | Получение TxManager. Сервис пытается получить TxManager из конфига (для тестов) или из context (production) | `domain/service.go:48` |
| **3d** | Создание транзакции. TxManager использует пул тенанта для `BeginTx`. Транзакция сохраняется в context для вложенных вызовов | `postgres/tx_manager.go:111` |
| **3e** | Querier resolution. Репозиторий получает TxManager из context и вызывает `GetQuerier` — возвращает `pgx.Tx` внутри транзакции или `pgxpool.Pool` вне её | `catalog_repo/base.go:103` |
| **3f** | Изолированный SQL. Запрос выполняется в БД конкретного тенанта. Никакой фильтрации по `tenant_id` в WHERE — изоляция физическая | — |

---

## Trace 4: Auth + Tenant Match — Защита от подмены тенанта

Middleware `Auth` проверяет, что `tenant_id` из JWT-токена совпадает с `X-Tenant-ID` из заголовка. Это предотвращает атаку подмены тенанта.

```
Защита от подмены тенанта
│
├── TenantDB middleware (выполняется ПЕРВЫМ)
│   └── ctx = tenant.WithTenant(ctx, tenant) <-- 4a
│       └── resolvedTenantID = tenant.ID
│
├── Auth middleware (выполняется ВТОРЫМ)
│   ├── JWT token → user.TenantID <-- 4b
│   └── Проверка совпадения <-- 4c
│       ├── resolvedTenantID := tenant.GetTenantID(ctx)
│       └── if resolvedTenantID != user.TenantID
│           └── apperror.NewForbidden("tenant mismatch")
│               └── details: {header_tenant_id, token_tenant_id}
```

| ID | Описание | Файл |
|----|----------|------|
| **4a** | Tenant из заголовка. TenantDB middleware резолвит тенант из `X-Tenant-ID` и сохраняет в context | `middleware/tenant.go:88` |
| **4b** | Tenant из токена. JWT-токен содержит `tenant_id` — идентификатор тенанта, к которому привязан пользователь | `middleware/auth.go:38` |
| **4c** | Cross-check. Если `X-Tenant-ID` (заголовок) не совпадает с `tenant_id` (JWT) → 403 Forbidden. Предотвращает доступ к данным чужого тенанта | `middleware/auth.go:47` |

---

## Trace 5: Background Workers — Eviction и Health Check

Manager запускает два фоновых цикла: eviction неиспользуемых пулов и health check активных.

```
Manager Background Workers
│
├── evictionLoop() <-- 5a
│   ├── ticker := PoolIdleTimeout / 2 (каждые 15 мин при default)
│   └── for { select { case <-ticker.C: evictIdlePools() } }
│       │
│       └── evictIdlePools() <-- 5b
│           └── pools.Range(func(tenantID, mp) {
│               ├── if mp.refCount > 0 → skip (активные запросы)
│               ├── if mp.unhealthySince > 0 && refCount == 0
│               │   └── closePool(tenantID, "unhealthy pool") <-- 5c
│               └── if mp.lastUsed < threshold
│                   └── closePool(tenantID, "idle timeout") <-- 5d
│           })
│
├── healthCheckLoop() <-- 5e
│   ├── ticker := HealthCheckPeriod (каждую 1 мин при default)
│   └── for { select { case <-ticker.C: checkPoolsHealth() } }
│       │
│       └── checkPoolsHealth() <-- 5f
│           └── pools.Range(func(tenantID, mp) {
│               └── mp.pool.Ping(ctx)
│                   ├── ошибка + unhealthySince == 0
│                   │   └── mp.unhealthySince.Store(time.Now()) <-- 5g
│                   ├── ошибка + refCount == 0
│                   │   └── closePool(tenantID, "health check failed")
│                   └── успех + unhealthySince != 0
│                       └── mp.unhealthySince.Store(0) — recovered <-- 5h
│           })
│
└── closePool(tenantID, mp, reason) <-- 5i
    ├── pools.Delete(tenantID)
    ├── mp.pool.Close()
    ├── poolCount.Add(-1)
    └── log.Info("closed pool", tenantID, reason, totalPools)
```

| ID | Описание | Файл |
|----|----------|------|
| **5a** | Eviction loop. Фоновая горутина, проверяющая idle-пулы каждые `PoolIdleTimeout/2` (default 15 минут) | `manager.go:215` |
| **5b** | Eviction logic. Обходит все пулы через `sync.Map.Range`. Никогда не закрывает пулы с `refCount > 0` | `manager.go:232` |
| **5c** | Закрытие unhealthy пулов. Если пул помечен как unhealthy и нет активных запросов — закрывается немедленно | `manager.go:246` |
| **5d** | Закрытие idle пулов. Если `lastUsed < now - PoolIdleTimeout` и нет активных запросов — пул закрывается | `manager.go:250` |
| **5e** | Health check loop. Фоновая горутина, пингующая все пулы каждые `HealthCheckPeriod` (default 1 минута) | `manager.go:259` |
| **5f** | Health check logic. `pool.Ping(ctx)` проверяет доступность БД тенанта | `manager.go:276` |
| **5g** | Маркировка unhealthy. При первом failed ping устанавливается `unhealthySince` timestamp. Пул НЕ закрывается сразу, если есть активные запросы | `manager.go:286` |
| **5h** | Recovery. Если пул был unhealthy, но ping прошёл — `unhealthySince` сбрасывается в 0 | `manager.go:301` |
| **5i** | Закрытие пула. Удаление из `sync.Map`, закрытие `pgxpool.Pool`, декремент `poolCount`, логирование | `manager.go:309` |

---

## Trace 6: Graceful Shutdown — Безопасное завершение работы

При остановке приложения Manager корректно закрывает все пулы и ожидает завершения фоновых горутин.

```
Manager.Close() <-- manager.go:322
│
├── Остановка фоновых воркеров <-- 6a
│   ├── m.cancel() — отмена context
│   └── m.wg.Wait() — ожидание evictionLoop + healthCheckLoop
│
└── Закрытие всех пулов <-- 6b
    └── pools.Range(func(key, value) {
        └── mp.pool.Close()
            └── pgxpool.Pool.Close() — закрывает все соединения
    })
```

| ID | Описание | Файл |
|----|----------|------|
| **6a** | Остановка воркеров. `cancel()` отменяет context, `wg.Wait()` ожидает завершения обеих горутин (eviction + health check) | `manager.go:326` |
| **6b** | Закрытие пулов. Все пулы закрываются безусловно. В production к этому моменту все запросы уже завершены (Kubernetes graceful shutdown period) | `manager.go:331` |

---

## Trace 7: Stats — Мониторинг состояния пулов

Manager предоставляет endpoint `/health/tenants` для мониторинга состояния всех пулов.

```
Manager.Stats() <-- manager.go:342
│
├── stats.TotalPools = poolCount.Load() <-- 7a
│
└── pools.Range(func(tenantID, mp) { <-- 7b
    ├── poolStats := mp.pool.Stat()
    │   ├── TotalConns  — всего соединений
    │   ├── IdleConns   — свободные
    │   └── AcquiredConns — занятые
    │
    └── TenantPoolStats{
        ├── TenantID, DBName
        ├── TotalConns, IdleConns, AcquiredConns
        ├── ActiveRefs — текущие HTTP-запросы к этому тенанту
        └── LastUsed — когда пул использовался последний раз
    }
})
```

| ID | Описание | Файл |
|----|----------|------|
| **7a** | Общая статистика. `poolCount` — атомарный счётчик всех активных пулов | `manager.go:344` |
| **7b** | Per-tenant статистика. Для каждого пула: количество соединений, активных запросов, время последнего использования | `manager.go:346` |

---

## Trace 8: Сводная диаграмма — Полный путь запроса через Multi-Tenant слой

```
┌─────────────────────────────────────────────────────────────────────────┐
│               DATABASE-PER-TENANT: ПОЛНЫЙ ПУТЬ ЗАПРОСА                   │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  HTTP Request                                                            │
│  Headers: X-Tenant-ID: "550e8400-..."                                   │
│       │                                                                  │
│       ▼                                                                  │
│  ┌──────────── MIDDLEWARE ───────────────────────────────────┐          │
│  │ TenantDB middleware                                        │          │
│  │ ├── uuid.Parse("550e8400-...")                              │          │
│  │ └── manager.GetPool(ctx, "550e8400-...")                    │          │
│  └───────────────────────────┬────────────────────────────────┘          │
│                              │                                           │
│                              ▼                                           │
│  ┌──────────── MANAGER ──────────────────────────────────────┐          │
│  │                                                             │          │
│  │  sync.Map: pools                                            │          │
│  │  ┌─────────────┬─────────────┬─────────────┐               │          │
│  │  │  tenant_aaa │  tenant_bbb │  tenant_ccc │               │          │
│  │  │  ┌────────┐ │  ┌────────┐ │  ┌────────┐ │               │          │
│  │  │  │Pool    │ │  │Pool    │ │  │Pool    │ │               │          │
│  │  │  │10 conns│ │  │10 conns│ │  │10 conns│ │               │          │
│  │  │  │refCnt=3│ │  │refCnt=0│ │  │refCnt=1│ │               │          │
│  │  │  └────────┘ │  └────────┘ │  └────────┘ │               │          │
│  │  └─────────────┴─────────────┴─────────────┘               │          │
│  │                                                             │          │
│  │  Background workers:                                        │          │
│  │  ├── evictionLoop (каждые 15 мин)                          │          │
│  │  └── healthCheckLoop (каждую 1 мин)                        │          │
│  │                                                             │          │
│  └───────────────────────────┬────────────────────────────────┘          │
│                              │ ManagedPool                               │
│                              ▼                                           │
│  ┌──────────── CONTEXT INJECTION ────────────────────────────┐          │
│  │ ctx = WithPool(ctx, pool)                                   │          │
│  │ ctx = WithTxManager(ctx, NewTxManagerFromRawPool(pool))     │          │
│  │ ctx = WithTenant(ctx, &Tenant{Slug:"acme", DB:"mt_acme"})  │          │
│  └───────────────────────────┬────────────────────────────────┘          │
│                              │                                           │
│                              ▼                                           │
│  ┌──────────── SERVICE + REPO ───────────────────────────────┐          │
│  │ txm := tenant.MustGetTxManager(ctx)                         │          │
│  │ txm.RunInTransaction(ctx, func(ctx) {                       │          │
│  │     querier := txm.GetQuerier(ctx) // → pgx.Tx             │          │
│  │     querier.Exec(ctx, "INSERT INTO cat_xxx ...")             │          │
│  │ })                                                           │          │
│  └───────────────────────────┬────────────────────────────────┘          │
│                              │                                           │
│                              ▼                                           │
│  ┌──────────── PostgreSQL ───────────────────────────────────┐          │
│  │                                                             │          │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐           │          │
│  │  │  mt_acme   │  │  mt_globex │  │  mt_initech│           │          │
│  │  │  (tenant A)│  │  (tenant B)│  │  (tenant C)│           │          │
│  │  │            │  │            │  │            │           │          │
│  │  │ cat_xxx    │  │ cat_xxx    │  │ cat_xxx    │           │          │
│  │  │ doc_xxx    │  │ doc_xxx    │  │ doc_xxx    │           │          │
│  │  │ reg_xxx    │  │ reg_xxx    │  │ reg_xxx    │           │          │
│  │  └────────────┘  └────────────┘  └────────────┘           │          │
│  │                                                             │          │
│  │  ┌─────────────────────────────────────────────┐           │          │
│  │  │  metapus_meta (meta-database)                │           │          │
│  │  │  tenants: id, slug, db_name, status, plan    │           │          │
│  │  └─────────────────────────────────────────────┘           │          │
│  └─────────────────────────────────────────────────────────────┘          │
│                                                                          │
│  ГАРАНТИЯ ИЗОЛЯЦИИ:                                                     │
│  • Каждый тенант = отдельная PostgreSQL database                        │
│  • Никакой фильтрации по tenant_id в SQL                                │
│  • TxManager привязан к пулу конкретного тенанта                        │
│  • Auth middleware проверяет tenant match (JWT vs header)                │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```
