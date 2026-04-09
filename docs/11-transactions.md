# Управление транзакциями

> TxManager, вложенные транзакции через savepoints, optimistic locking через `version`, и паттерн `GetQuerier` для прозрачной работы внутри и вне транзакций.

---

## tx.Manager Interface — абстракция для domain

Domain зависит от интерфейса, не от реализации:

```go
// internal/core/tx/manager.go
type Manager interface {
    RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type ReadOnlyManager interface {
    Manager
    ReadOnly(ctx context.Context, fn func(ctx context.Context) error) error
}
```

- В production — `*postgres.TxManager`
- В тестах — mock/fake
- Compile-time check: `var _ tx.Manager = (*TxManager)(nil)`

---

## Новая транзакция — BEGIN → fn → COMMIT/ROLLBACK

```
TxManager.RunInTransaction(ctx, fn)
│
├── DefaultTxOptions:
│   ├── IsolationLevel: ReadCommitted
│   ├── AccessMode: ReadWrite
│   ├── StatementTimeout: 30s
│   └── UseSavepoint: false
│
├── Tracing span (OpenTelemetry)
│
├── Проверка существующей транзакции в context
│   ├── есть → handleNestedTransaction [см. ниже]
│   └── нет  → startNewTransaction ↓
│
└── startNewTransaction:
    ├── pool.BeginTx(ctx, pgx.TxOptions{...})
    │   └── BEGIN TRANSACTION ISOLATION LEVEL READ COMMITTED
    ├── SET LOCAL statement_timeout = '30000ms'
    │   └── Защита от runaway queries (LOCAL = только внутри транзакции)
    ├── ctx = context.WithValue(ctx, txKey{}, wrappedTx)
    ├── fn(ctx)
    │   ├── err != nil → tx.Rollback(context.Background())
    │   └── err == nil → tx.Commit(ctx)
    └── return
```

---

## Вложенные транзакции — Reuse vs Savepoint

Когда `RunInTransaction` вызывается внутри другой транзакции:

### Reuse (default, `UseSavepoint=false`)
```go
fn(ctx)  // выполняем в существующей транзакции, БЕЗ SAVEPOINT
// Если fn вернёт error — вся внешняя транзакция будет откатана
```

### Savepoint (`UseSavepoint=true`)
```
SAVEPOINT sp_1707000000000
├── fn(ctx)
│   ├── error → ROLLBACK TO SAVEPOINT (только вложенная часть)
│   └── success → RELEASE SAVEPOINT (финальный COMMIT — во внешней tx)
```

**Пример:** Posting Engine всегда reuse — сервис создаёт транзакцию, внутри вызывает postingEngine.Post, который тоже вызывает RunInTransaction → reuse.

**Warning:** Savepoints дорогие. Используйте только когда нужен частичный rollback.

---

## GetQuerier — единый интерфейс

Репозитории не знают, в каком режиме работают:

```go
type Querier interface {
    Exec(ctx, sql, args...) (CommandTag, error)
    Query(ctx, sql, args...) (Rows, error)
    QueryRow(ctx, sql, args...) Row
}
// Реализуют: pgx.Tx И pgxpool.Pool

func (m *TxManager) GetQuerier(ctx context.Context) Querier {
    if tx := m.GetTx(ctx); tx != nil {
        return tx.Tx    // внутри транзакции
    }
    return m.pool       // вне транзакции
}
```

Репозиторий:
```go
querier := r.getTxManager(ctx).GetQuerier(ctx)
querier.Exec(ctx, "INSERT INTO ...")  // прозрачно: tx или pool
```

---

## Optimistic Locking — через version

Каждая сущность имеет `BaseEntity.Version int`. При обновлении:

```sql
UPDATE cat_counterparties
SET name = 'ACME Corp', ..., version = version + 1
WHERE id = 'xxx' AND version = 3
RETURNING version
```

| Результат | Действие |
|-----------|----------|
| Строка обновлена | `Scan(&newVersion)` → `entity.SetVersion(4)` |
| `ErrNoRows` | `apperror.NewConcurrentModification()` → **HTTP 409** |

**Ключевые моменты:**
- `version = version + 1` атомарно в SQL
- `WHERE version = $N` — если кто-то обновил раньше, WHERE не найдёт строку
- `RETURNING version` — новый version без дополнительного SELECT

---

## Rollback Protection

При отмене context (timeout, client disconnect) rollback должен выполниться:

```go
func executeWithRollbackProtection(ctx context.Context, tx pgx.Tx, fn func(ctx) error) error {
    err := fn(ctx)
    if err != nil {
        tx.Rollback(context.Background())  // ← Background, не original ctx!
        return err
    }
    return nil
}
```

**Почему `context.Background()`?** Original ctx может быть cancelled — pgx не выполнит ROLLBACK с cancelled ctx.

**Приоритет ошибок:** оригинальная ошибка `fn` всегда возвращается. Ошибка rollback только логируется.

---

## Специальные режимы

### Serializable Isolation
Для критических операций (банковские переводы, проведение):
```go
SerializableTxOptions() // IsolationLevel: pgx.Serializable
```
PostgreSQL автоматически детектит serialization anomalies → retry на уровне приложения.

### Read-Only Transactions
```go
TxManager.ReadOnly(ctx, fn)  // AccessMode: pgx.ReadOnly
```
PostgreSQL запрещает INSERT/UPDATE/DELETE. Меньше блокировок, лучше производительность для отчётов.

---

## Стратегии блокировок

### Optimistic Locking (большинство операций)
- Используй `Version` field и compare-and-swap в SQL
- Подходит для справочников и документов

### Pessimistic Locking (критические вычисления)
- `SELECT ... FOR UPDATE` для проверки остатков перед проведением
- Всегда блокируй в **фиксированном порядке** (resource ordering)

### Handling Conflicts
- `AffectedRows == 0` → `CONCURRENT_MODIFICATION`
- Prefer retries (with backoff) для transient конфликтов
- **NO** indefinite locks — транзакции должны быть короткими

---

## Связанные документы

- [10-posting-engine.md](10-posting-engine.md) — транзакции при проведении
- [07-multi-tenancy.md](07-multi-tenancy.md) — создание TxManager в middleware
- [04-core-layer.md](04-core-layer.md) — tx.Manager interface
- [09-crud-pipeline.md](09-crud-pipeline.md) — транзакции в CRUD pipeline
