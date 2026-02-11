# Управление транзакциями в Metapus: TxManager, вложенные транзакции и Optimistic Locking

Карта показывает полный путь управления транзакциями: от интерфейса `tx.Manager` до PostgreSQL `BEGIN/COMMIT/ROLLBACK`, вложенные транзакции через savepoints, optimistic locking через `version`, и паттерн `GetQuerier` для прозрачной работы внутри и вне транзакций.

**Ключевые точки:**
- Интерфейс `tx.Manager` в domain layer (1a)
- `TxManager` — реализация с tracing и timeout (2a)
- Вложенные транзакции: reuse vs savepoint (3a)
- `GetQuerier` — единый интерфейс для tx/pool (4a)
- Optimistic locking через `version` (5a)
- Rollback protection и context cancellation (6a)

---

## Trace 1: tx.Manager Interface — Абстракция для domain layer

Domain layer зависит от интерфейса `tx.Manager`, а не от конкретной реализации. Это позволяет тестировать бизнес-логику без PostgreSQL.

```
Dependency Inversion: Domain → Interface ← Infrastructure
│
├── core/tx/manager.go <-- 1a
│   └── type Manager interface {
│       └── RunInTransaction(ctx, fn func(ctx) error) error
│   }
│
├── type ReadOnlyManager interface { <-- 1b
│   └── Manager + ReadOnly(ctx, fn) error
│   }
│
├── Domain layer использует интерфейс:
│   └── domain/service.go <-- 1c
│       └── CatalogService[T] struct {
│           txManager tx.Manager  // интерфейс, не *postgres.TxManager
│       }
│
└── Infrastructure предоставляет реализацию:
    └── postgres/tx_manager.go <-- 1d
        └── var _ tx.Manager = (*TxManager)(nil)  // compile-time check
```

| ID | Описание | Файл |
|----|----------|------|
| **1a** | Интерфейс `tx.Manager`. Единственный метод `RunInTransaction` — выполняет функцию в транзакции с автоматическим commit/rollback | `core/tx/manager.go:15` |
| **1b** | `ReadOnlyManager`. Расширение для read-only транзакций (`SET TRANSACTION READ ONLY`) — лучше производительность, нет блокировок | `core/tx/manager.go:26` |
| **1c** | Использование в сервисе. `CatalogService` хранит `tx.Manager` как интерфейс. В production — `*postgres.TxManager`, в тестах — mock | `domain/service.go:20` |
| **1d** | Compile-time check. `var _ tx.Manager = (*TxManager)(nil)` гарантирует, что `TxManager` реализует интерфейс на этапе компиляции | `postgres/tx_manager.go:22` |

---

## Trace 2: Новая транзакция — BEGIN → fn → COMMIT/ROLLBACK

Полный путь создания транзакции с statement timeout, tracing span и rollback protection.

```
TxManager.RunInTransaction(ctx, fn) <-- tx_manager.go:88
│
├── RunInTransactionWithOptions(ctx, DefaultTxOptions(), fn) <-- 2a
│   │
│   ├── DefaultTxOptions() <-- 2b
│   │   ├── IsolationLevel: ReadCommitted
│   │   ├── AccessMode: ReadWrite
│   │   ├── StatementTimeout: 30s
│   │   └── UseSavepoint: false
│   │
│   ├── Tracing span <-- 2c
│   │   └── tracer.Start(ctx, "transaction",
│   │       attribute.String("tx.isolation", "ReadCommitted"))
│   │
│   ├── Проверка существующей транзакции <-- 2d
│   │   └── m.GetTx(ctx) → ctx.Value(txKey{}).(*Tx)
│   │       ├── != nil → handleNestedTransaction [Trace 3]
│   │       └── == nil → startNewTransaction ↓
│   │
│   └── startNewTransaction(ctx, opts, fn) <-- 2e
│       │
│       ├── pool.BeginTx(ctx, pgx.TxOptions{...}) <-- 2f
│       │   └── PostgreSQL: BEGIN TRANSACTION ISOLATION LEVEL READ COMMITTED
│       │
│       ├── SET LOCAL statement_timeout <-- 2g
│       │   └── tx.Exec("SET LOCAL statement_timeout = '30000ms'")
│       │       └── защита от runaway queries (убивает запрос через 30с)
│       │       └── LOCAL = действует только внутри транзакции
│       │
│       ├── Сохранение tx в context <-- 2h
│       │   └── wrappedTx := &Tx{Tx: tx, nested: false}
│       │       └── txCtx := context.WithValue(ctx, txKey{}, wrappedTx)
│       │
│       ├── executeWithRollbackProtection(txCtx, tx, fn) <-- 2i
│       │   └── err := fn(txCtx)
│       │       ├── err != nil → tx.Rollback(context.Background())
│       │       │   └── Background context для rollback
│       │       │       (даже если original ctx отменён)
│       │       └── err == nil → продолжаем
│       │
│       └── tx.Commit(ctx) <-- 2j
│           └── PostgreSQL: COMMIT
```

| ID | Описание | Файл |
|----|----------|------|
| **2a** | Entry point. `RunInTransaction` вызывает `RunInTransactionWithOptions` с default опциями | `tx_manager.go:88` |
| **2b** | Default options. `ReadCommitted` + `ReadWrite` + 30s timeout. Подходит для большинства операций | `tx_manager.go:41` |
| **2c** | OpenTelemetry tracing. Каждая транзакция создаёт span с атрибутом isolation level. Позволяет отслеживать длительность транзакций в Jaeger/Tempo | `tx_manager.go:95` |
| **2d** | Проверка вложенности. Если в context уже есть активная транзакция → переход к nested transaction logic | `tx_manager.go:102` |
| **2e** | Создание новой транзакции. Полный цикл: BEGIN → SET timeout → fn() → COMMIT или ROLLBACK | `tx_manager.go:111` |
| **2f** | BEGIN TRANSACTION. `pool.BeginTx` начинает PostgreSQL транзакцию с указанным isolation level и access mode | `tx_manager.go:112` |
| **2g** | Statement timeout. `SET LOCAL` действует только внутри текущей транзакции. Защита от зависших запросов — PostgreSQL автоматически прерывает запрос через 30 секунд | `tx_manager.go:121` |
| **2h** | Tx в context. Транзакция оборачивается в `Tx` struct и сохраняется в context. Все последующие вызовы `GetQuerier` будут использовать эту транзакцию | `tx_manager.go:130` |
| **2i** | Rollback protection. При ошибке `fn` вызывается `tx.Rollback` с `context.Background()` — гарантирует rollback даже при отменённом parent context | `tx_manager.go:181` |
| **2j** | COMMIT. Если `fn` вернула nil и rollback не нужен — транзакция коммитится | `tx_manager.go:139` |

---

## Trace 3: Вложенные транзакции — Reuse vs Savepoint

Когда `RunInTransaction` вызывается внутри другой транзакции, есть два сценария: reuse (по умолчанию) и savepoint (явно запрошенный).

```
Вложенная транзакция
│
├── Сценарий 1: Reuse (UseSavepoint=false, default) <-- 3a
│   └── handleNestedTransaction(ctx, existing, opts, fn)
│       └── fn(ctx) — выполняем в существующей транзакции
│           └── БЕЗ SAVEPOINT, БЕЗ дополнительного BEGIN
│               └── Если fn вернёт error — вся внешняя транзакция
│                   будет откатана (нет частичного rollback)
│
├── Сценарий 2: Savepoint (UseSavepoint=true) <-- 3b
│   └── handleNestedTransaction(ctx, existing, opts, fn)
│       │
│       ├── SAVEPOINT sp_1707000000000 <-- 3c
│       │   └── existing.Exec("SAVEPOINT sp_...")
│       │
│       ├── fn(ctx) <-- 3d
│       │   ├── error → ROLLBACK TO SAVEPOINT sp_... <-- 3e
│       │   │   └── Откатывает ТОЛЬКО вложенную часть
│       │   │       └── Внешняя транзакция продолжает работать
│       │   └── success → RELEASE SAVEPOINT sp_... <-- 3f
│       │       └── Фиксирует вложенную часть
│       │           └── Финальный COMMIT — во внешней транзакции
│       │
│       └── WARNING: Savepoints are expensive <-- 3g
│           └── Используйте ТОЛЬКО когда нужен частичный rollback
│
├── Пример: Posting Engine (всегда reuse)
│   └── service.Create → txm.RunInTransaction
│       └── внутри: postingEngine.Post → txm.RunInTransaction
│           └── GetTx(ctx) != nil → reuse (fn выполняется в той же tx)
```

| ID | Описание | Файл |
|----|----------|------|
| **3a** | Reuse (default). Вложенный вызов `RunInTransaction` просто выполняет `fn(ctx)` в существующей транзакции. Без overhead | `tx_manager.go:148` |
| **3b** | Savepoint. Создаёт PostgreSQL SAVEPOINT для частичного rollback. Используется когда нужна атомарность внутри транзакции | `tx_manager.go:153` |
| **3c** | SAVEPOINT. Имя генерируется из `time.Now().UnixNano()` для уникальности | `tx_manager.go:154` |
| **3d** | Выполнение fn. Код выполняется между SAVEPOINT и RELEASE/ROLLBACK | `tx_manager.go:161` |
| **3e** | Частичный rollback. `ROLLBACK TO SAVEPOINT` откатывает только изменения после SAVEPOINT. Внешняя транзакция не затронута | `tx_manager.go:163` |
| **3f** | Release. `RELEASE SAVEPOINT` фиксирует вложенные изменения (но COMMIT — только во внешней транзакции) | `tx_manager.go:171` |
| **3g** | Performance warning. Savepoints добавляют overhead. По умолчанию отключены (`UseSavepoint: false`) | `tx_manager.go:36` |

---

## Trace 4: GetQuerier — Единый интерфейс для запросов

`GetQuerier` возвращает `pgx.Tx` внутри транзакции или `pgxpool.Pool` вне её. Репозитории не знают, в каком режиме работают.

```
GetQuerier Pattern
│
├── Querier interface <-- 4a
│   └── type Querier interface {
│       ├── Exec(ctx, sql, args...) → CommandTag
│       ├── Query(ctx, sql, args...) → Rows
│       └── QueryRow(ctx, sql, args...) → Row
│   }
│   └── Реализуют: pgx.Tx И pgxpool.Pool
│
├── TxManager.GetQuerier(ctx) <-- 4b
│   ├── if tx := m.GetTx(ctx); tx != nil
│   │   └── return tx.Tx  (pgx.Tx — внутри транзакции)
│   └── else
│       └── return m.pool  (pgxpool.Pool — вне транзакции)
│
├── Использование в репозитории <-- 4c
│   └── BaseCatalogRepo.Create(ctx, entity)
│       └── querier := r.getTxManager(ctx).GetQuerier(ctx)
│           └── querier.Exec(ctx, "INSERT INTO ...")
│               └── Прозрачно: tx или pool — без if/else
│
└── Пример: чтение вне транзакции <-- 4d
    └── service.GetByID(ctx, id)
        └── repo.GetByID(ctx, id) — НЕТ RunInTransaction
            └── GetQuerier(ctx) → pool
                └── pool.QueryRow(ctx, "SELECT ...")
```

| ID | Описание | Файл |
|----|----------|------|
| **4a** | Querier interface. Общий интерфейс для `Exec/Query/QueryRow`. Реализуется и `pgx.Tx`, и `pgxpool.Pool` | `tx_manager.go:204` |
| **4b** | GetQuerier. Проверяет наличие транзакции в context. Возвращает tx если есть, pool если нет | `tx_manager.go:211` |
| **4c** | Прозрачность для репозитория. Репозиторий не знает, работает ли он в транзакции — один и тот же код для обоих случаев | `catalog_repo/base.go:103` |
| **4d** | Чтение без транзакции. `GetByID` не оборачивается в `RunInTransaction` — `GetQuerier` возвращает pool напрямую | `domain/service.go:124` |

---

## Trace 5: Optimistic Locking — Конкурентные обновления через version

Каждая сущность имеет поле `version`. При обновлении version проверяется в WHERE и инкрементируется. Если version не совпал — значит кто-то обновил раньше.

```
Optimistic Locking Flow
│
├── Чтение сущности <-- 5a
│   └── SELECT * FROM cat_counterparties WHERE id = $1
│       └── entity{id: "xxx", name: "ACME", version: 3}
│
├── Обновление с version check <-- 5b
│   └── BaseCatalogRepo.Update(ctx, entity)
│       │
│       ├── data := StructToMap(entity)
│       │   └── {id: "xxx", name: "ACME Corp", version: 3, ...}
│       │
│       ├── squirrel.Update(tableName)
│       │   ├── .SetMap(filteredData)     — все поля кроме id, version
│       │   ├── .Set("version", Expr("version + 1"))  <-- 5c
│       │   ├── .Where(Eq{"id": entityID})
│       │   ├── .Where(Eq{"version": 3})              <-- 5d
│       │   └── .Suffix("RETURNING version")           <-- 5e
│       │
│       └── SQL результат:
│           UPDATE cat_counterparties
│           SET name = 'ACME Corp', ..., version = version + 1
│           WHERE id = 'xxx' AND version = 3
│           RETURNING version
│
├── Результат:
│   ├── Успех (строка обновлена) <-- 5f
│   │   ├── Scan(&newVersion) → newVersion = 4
│   │   └── entity.SetVersion(4) — обновляем в памяти
│   │
│   └── Конфликт (строка НЕ обновлена) <-- 5g
│       └── pgx.ErrNoRows (WHERE version=3 не нашёл строку)
│           └── apperror.NewConcurrentModification(tableName, entityID)
│               └── HTTP 409 Conflict
│                   └── "entity was modified by another request"
```

| ID | Описание | Файл |
|----|----------|------|
| **5a** | Чтение с version. Каждая сущность наследует `BaseEntity.Version int` | `entity/base.go` |
| **5b** | Update с проверкой. SQL UPDATE включает `WHERE version = $N` — обновление происходит только если version не изменился | `catalog_repo/base.go:113` |
| **5c** | Инкремент version. `version = version + 1` атомарно в SQL. Не зависит от значения в Go | `catalog_repo/base.go:146` |
| **5d** | WHERE version. Ключевая часть optimistic locking — если кто-то обновил сущность между чтением и записью, WHERE не найдёт строку | `catalog_repo/base.go:148` |
| **5e** | RETURNING version. Новый version возвращается сразу, без дополнительного SELECT | `catalog_repo/base.go:149` |
| **5f** | Успешное обновление. Новый version сохраняется в entity через `SetVersion()` — предотвращает stale object | `catalog_repo/base.go:168` |
| **5g** | Конфликт. `ErrNoRows` означает, что version изменился (кто-то другой обновил). Возвращается 409 Conflict | `catalog_repo/base.go:162` |

---

## Trace 6: Rollback Protection — Обработка отмены context

При отмене context (timeout, client disconnect) rollback должен всё равно выполниться.

```
Rollback Protection
│
├── executeWithRollbackProtection(ctx, tx, fn) <-- 6a
│   │
│   ├── err := fn(ctx)
│   │   ├── err == nil → return nil (caller делает Commit)
│   │   │
│   │   └── err != nil → нужен Rollback <-- 6b
│   │       └── tx.Rollback(context.Background())
│   │           │
│   │           ├── ПОЧЕМУ context.Background()?
│   │           │   └── Original ctx может быть cancelled
│   │           │       (timeout, client disconnect)
│   │           │   └── pgx НЕ выполнит Rollback с cancelled ctx
│   │           │   └── Background() гарантирует выполнение <-- 6c
│   │           │
│   │           ├── Rollback success → return original err
│   │           └── Rollback failure → log error + return original err
│   │
│   └── Важно: ошибка fn ВСЕГДА возвращается вызывающему <-- 6d
│       └── даже если Rollback тоже провалился
│           └── оригинальная ошибка важнее ошибки Rollback
│
├── Serializable Isolation (для критических операций) <-- 6e
│   └── SerializableTxOptions()
│       └── IsolationLevel: pgx.Serializable
│           └── PostgreSQL детектит serialization anomalies
│               └── error → автоматический retry на уровне приложения
│
└── ReadOnly Transactions <-- 6f
    └── TxManager.ReadOnly(ctx, fn)
        └── AccessMode: pgx.ReadOnly
            └── PostgreSQL запрещает INSERT/UPDATE/DELETE
                └── Лучше производительность (нет write locks)
```

| ID | Описание | Файл |
|----|----------|------|
| **6a** | Rollback protection. Обёртка над `fn` — гарантирует rollback при любой ошибке | `tx_manager.go:181` |
| **6b** | Rollback при ошибке. Вызывается немедленно при `err != nil` от `fn` | `tx_manager.go:186` |
| **6c** | Background context. `context.Background()` для rollback — критически важно. Cancelled context не позволит pgx выполнить SQL `ROLLBACK` | `tx_manager.go:186` |
| **6d** | Приоритет ошибок. Оригинальная ошибка `fn` всегда возвращается. Ошибка rollback только логируется | `tx_manager.go:187` |
| **6e** | Serializable isolation. Для критических операций (банковские переводы, проведение документов). PostgreSQL автоматически обнаруживает аномалии | `tx_manager.go:51` |
| **6f** | Read-only транзакции. PostgreSQL запрещает запись. Меньше блокировок, лучше производительность для отчётов | `tx_manager.go:219` |

---

## Trace 7: Сводная диаграмма — Транзакционная архитектура

```
┌─────────────────────────────────────────────────────────────────────────┐
│              ТРАНЗАКЦИОННАЯ АРХИТЕКТУРА METAPUS                          │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Domain Layer (чистая бизнес-логика)                                    │
│  ┌──────────────────────────────────────────────────────┐              │
│  │  tx.Manager (interface)                                │              │
│  │  └── RunInTransaction(ctx, fn) error                   │              │
│  │                                                        │              │
│  │  Сервис не знает:                                      │              │
│  │  • Какая БД (PostgreSQL? SQLite?)                      │              │
│  │  • Какой isolation level                                │              │
│  │  • Есть ли вложенные транзакции                        │              │
│  └──────────────────────────────────────────────────────┘              │
│                          │                                               │
│                          ▼                                               │
│  Infrastructure Layer (PostgreSQL реализация)                            │
│  ┌──────────────────────────────────────────────────────┐              │
│  │  TxManager (*pgxpool.Pool)                             │              │
│  │                                                        │              │
│  │  RunInTransaction(ctx, fn):                            │              │
│  │  ┌─────────────────────────────────────────────┐      │              │
│  │  │ GetTx(ctx) == nil?                            │      │              │
│  │  │ ├── YES: startNewTransaction                  │      │              │
│  │  │ │   ├── BEGIN (ReadCommitted, ReadWrite)      │      │              │
│  │  │ │   ├── SET LOCAL statement_timeout = 30s     │      │              │
│  │  │ │   ├── ctx = WithTx(ctx, tx)                 │      │              │
│  │  │ │   ├── fn(ctx)                                │      │              │
│  │  │ │   │   ├── OK  → COMMIT                      │      │              │
│  │  │ │   │   └── ERR → ROLLBACK (background ctx)   │      │              │
│  │  │ │                                              │      │              │
│  │  │ └── NO: handleNestedTransaction               │      │              │
│  │  │     ├── UseSavepoint=false → fn(ctx) (reuse)  │      │              │
│  │  │     └── UseSavepoint=true                      │      │              │
│  │  │         ├── SAVEPOINT sp_xxx                   │      │              │
│  │  │         ├── fn(ctx)                            │      │              │
│  │  │         │   ├── OK  → RELEASE SAVEPOINT        │      │              │
│  │  │         │   └── ERR → ROLLBACK TO SAVEPOINT    │      │              │
│  │  └─────────────────────────────────────────────┘      │              │
│  │                                                        │              │
│  │  GetQuerier(ctx):                                      │              │
│  │  ┌─────────────────────────────────────────────┐      │              │
│  │  │ GetTx(ctx) != nil → return pgx.Tx             │      │              │
│  │  │ GetTx(ctx) == nil → return pgxpool.Pool        │      │              │
│  │  └─────────────────────────────────────────────┘      │              │
│  │                                                        │              │
│  │  Optimistic Locking:                                   │              │
│  │  ┌─────────────────────────────────────────────┐      │              │
│  │  │ UPDATE ... SET version = version + 1          │      │              │
│  │  │ WHERE id = $1 AND version = $2                │      │              │
│  │  │ RETURNING version                              │      │              │
│  │  │ → ErrNoRows = ConcurrentModification (409)    │      │              │
│  │  └─────────────────────────────────────────────┘      │              │
│  └──────────────────────────────────────────────────────┘              │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```
