# Posting Engine — Проведение документов

> Полный путь проведения документа: от HTTP-запроса до записи движений в регистры и автоматического обновления остатков через триггеры.

---

## Общая схема проведения

```
Service.Post(docID)
├── repo.GetByID() — загрузка документа + строки
├── updateDoc callback — func() { repo.Update(doc) }
└── postingEngine.Post(ctx, doc, updateDoc)
    └── Engine.doPost()
        ├── doc.CanPost(ctx) — валидация
        └── txm.RunInTransaction()
            ├── reverseAllMovements() — реверс старых через все recorders (если перепроведение)
            ├── collectMovements() — Visitor pattern по всем visitors
            │   ├── StockVisitor   → doc.(StockMovementSource)
            │   ├── CostVisitor    → doc.(CostMovementSource)
            │   └── SettlementVisitor → doc.(SettlementMovementSource)
            ├── for each recorder (PostingValidator): — предпроверка
            │   └── StockRecorder.ValidateBeforePost() — остатки, resource ordering
            ├── for each recorder: RecordFromSet() — запись движений
            │   ├── StockRecorder      → reg_stock_movements      → Trigger → balances
            │   ├── CostRecorder       → reg_cost_movements       → Trigger → balances
            │   └── SettlementRecorder → reg_settlement_movements → Trigger → balances
            ├── doc.MarkPosted() — Posted=true, PostedVersion++
            └── updateDoc(ctx) — сохранение документа
```

---

## Шаг 1: Entry Point — Сервис документа

Сервис загружает документ (шапку + строки) и передаёт управление движку:

```go
func (s *Service) Post(ctx context.Context, docID id.ID) error {
    doc, _ := s.repo.GetByID(ctx, docID)
    lines, _ := s.repo.GetLines(ctx, docID)
    doc.Lines = lines
    
    updateDoc := func(ctx context.Context) error { return s.repo.Update(ctx, doc) }
    return s.postingEngine.Post(ctx, doc, updateDoc)
}
```

---

## Шаг 2: Engine.doPost — координация

Централизованный движок (`posting.Engine`) координирует весь процесс **в единой атомарной транзакции**:

1. **Валидация** — `doc.CanPost(ctx)`
2. **Начало транзакции** — `txm.RunInTransaction()`
3. **Реверс старых движений** — если документ уже был проведён (перепроведение)
4. **Генерация движений** — `doc.GenerateMovements()`
5. **Проверка остатков** — для расходных движений, с пессимистической блокировкой
6. **Запись движений** — batch insert через COPY
7. **Маркировка документа** — `Posted=true`, `PostedVersion++`
8. **Сохранение документа** — вызов callback
9. **After-post hooks**

---

## Шаг 3: Генерация движений документом (Visitor Pattern)

Документ **не** создаёт движения напрямую. Вместо этого:
1. Документ реализует один или несколько **Source-интерфейсов** (`StockMovementSource`, `CostMovementSource`, `SettlementMovementSource`).
2. Зарегистрированные **Visitor**-ы опрашивают документ и собирают движения в `MovementSet`.

### Source-интерфейсы
```go
type StockMovementSource interface {
    GenerateStockMovements(ctx context.Context) ([]entity.StockMovement, error)
}
type CostMovementSource interface {
    GenerateCostMovements(ctx context.Context) ([]entity.CostMovement, error)
}
type SettlementMovementSource interface {
    GenerateSettlementMovements(ctx context.Context) ([]entity.SettlementMovement, error)
}
```

Документ реализует только те интерфейсы, которые ему нужны. Visitor пропускает документ, если интерфейс не реализован.

### GoodsReceipt (приход)
```go
// Реализует StockMovementSource
func (g *GoodsReceipt) GenerateStockMovements(ctx context.Context) ([]entity.StockMovement, error) {
    var movements []entity.StockMovement
    for _, line := range g.Lines {
        baseQty := line.Quantity * line.Coefficient
        movements = append(movements, entity.NewStockMovement(
            g.ID, "GoodsReceipt", g.PostedVersion+1, g.Date,
            entity.RecordTypeReceipt,
            g.WarehouseID, line.ProductID, baseQty,
        ))
    }
    return movements, nil
}
```

### GoodsIssue (расход)
Аналогично, но с `RecordTypeExpense` — **уменьшает** остаток.

---

## Шаг 4: Проверка остатков (Stock Validation)

Для расходных документов движок проверяет наличие товара с **пессимистической блокировкой**:

```
validateStockAvailability()
├── Сбор expense-движений
├── Группировка по warehouse+product (pointer map)
├── Детерминированная сортировка ← предотвращение deadlock (resource ordering)
└── CheckAndReserveStock()
    └── для каждого товара:
        ├── GetBalanceForUpdate() — SELECT ... FOR UPDATE
        └── if quantity < required → InsufficientStock error
```

**Resource Ordering:** сортировка по ключам измерений (warehouseID + productID) **до** блокировок. Предотвращает deadlock AB-BA.

---

## Шаг 5: Запись движений

Движения записываются через **batch insert (COPY)** — эффективная массовая вставка.

`BaseAccumulationRepo[T]` — generic-репозиторий, обобщающий `CreateMovements` / `DeleteMovementsByRecorder` для всех регистров накопления. Конкретные репозитории (`StockRepo`, `CostRepo`, `SettlementRepo`) встраивают его и добавляют специфичные запросы (балансы, обороты).

```go
// Все три регистра используют общий generic:
type StockRepo struct {
    BaseAccumulationRepo[entity.StockMovement]
}

// BaseAccumulationRepo.CreateMovements — COPY batch insert
// BaseAccumulationRepo.DeleteMovementsByRecorder — batch delete
```

---

## Шаг 6: Триггер обновления балансов

PostgreSQL триггеры **автоматически** обновляют таблицы остатков при каждой вставке/удалении движения:

| Регистр | Триггер | Таблица остатков |
|---------|---------|------------------|
| Stock | `update_stock_balance()` | `reg_stock_balances` |
| Cost | `update_cost_balance()` | `reg_cost_balances` |
| Settlement | `update_settlement_balance()` | `reg_settlement_balances` |

```sql
-- Каждый регистр имеет триггер одинаковой структуры:
-- receipt → +resource
-- expense → -resource
-- UPSERT через ON CONFLICT
```

**Immutable Ledger:** движения **никогда не обновляются** (UPDATE). При перепроведении — старые удаляются, новые вставляются.

---

## Unpost — отмена проведения

```
Service.Unpost(docID)
└── postingEngine.Unpost(ctx, doc, updateDoc)
    └── txm.RunInTransaction()
        ├── reverseAllMovements(recorderID)  — итерирует все recorders:
        │   ├── StockRecorder.ReverseMovements()      → DELETE + Trigger
        │   ├── CostRecorder.ReverseMovements()       → DELETE + Trigger
        │   └── SettlementRecorder.ReverseMovements() → DELETE + Trigger
        ├── doc.MarkUnposted() — Posted=false
        └── updateDoc(ctx) — сохранение
```

При DELETE триггер применяет **обратную операцию** к балансу (receipt → -qty, expense → +qty).

---

## Перепроведение

При повторном проведении уже проведённого документа:

1. Удаление старых движений (`ReverseMovements`) → триггер откатывает балансы
2. Генерация новых движений (`GenerateMovements`) с новой версией
3. Проверка остатков (для расхода)
4. Запись новых движений → триггер обновляет балансы
5. Обновление `PostedVersion`

Всё в **одной транзакции** — атомарно.

---

## Алгоритм проведения (полный)

```
1. IDEMPOTENCY CHECK
   └── Если ключ уже обработан → return cached_result

2. ПРЕДВАРИТЕЛЬНАЯ ВАЛИДАЦИЯ (БЕЗ транзакции)
   └── Парсинг, обязательные поля, типы данных

3. BEGIN TRANSACTION (REPEATABLE READ)
   └── SET LOCAL statement_timeout = '30s'

4. СОРТИРОВКА РЕСУРСОВ (Resource Ordering)
   └── ORDER BY product_id ASC — предотвращение Deadlock

5. ЧТЕНИЕ СТАРЫХ ДВИЖЕНИЙ (если перепроведение)

6. ГЕНЕРАЦИЯ НОВЫХ ДВИЖЕНИЙ

7. ПРОВЕРКА ОСТАТКОВ
   └── UPDATE reg_stock_balances SET quantity = quantity + $delta
       WHERE (quantity + $delta) >= 0
       └── RowsAffected = 0 → ROLLBACK + ErrInsufficientStock

8. ЗАПИСЬ ДВИЖЕНИЙ (pgx.CopyFrom)

9. COMMIT + UPDATE IDEMPOTENCY STATUS
```

---

## Правила

- **NO** `UPDATE` на `reg_*_movements` таблицах (Immutable Ledger)
- **NO** глобальных блокировок — только row-level locking
- **Всегда** сортировать ресурсы перед блокировкой (resource ordering)
- **Всегда** обрабатывать `CONCURRENT_MODIFICATION` ошибки
- Движения регистров **версионируются** (`recorder_version`)

---

---

## Архитектура регистров

### Typed Framework (Типизированный фреймворк)

Все регистры накопления строятся по единому паттерну:

```
┌─────────────────────────────────────────────────┐
│  entity/register.go                              │
│  MovementBase → StockMovement / CostMovement /   │
│                 SettlementMovement                │
│  StockBalance / CostBalance / SettlementBalance   │
└─────────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────────┐
│  BaseAccumulationRepo[T] (Go generics)           │
│  CreateMovements(COPY) + DeleteByRecorder         │
│  ↕ embedded by:                                   │
│  StockRepo │ CostRepo │ SettlementRepo            │
└─────────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────────┐
│  domain/registers/{stock,cost,settlement}/        │
│  Repository interface + Service                   │
└─────────────────────────────────────────────────┘
         ↓
┌─────────────────────────────────────────────────┐
│  posting.Engine                                   │
│  visitors:  StockVisitor, CostVisitor,            │
│             SettlementVisitor                     │
│  recorders: StockRecorder, CostRecorder,          │
│             SettlementRecorder                    │
│  Source interfaces: документ реализует нужные     │
└─────────────────────────────────────────────────┘
```

### Регистры

| Регистр | Измерения | Ресурсы | Таблица движений | Таблица остатков |
|---------|-----------|---------|-----------------|------------------|
| **Stock** | warehouse_id, product_id | quantity | `reg_stock_movements` | `reg_stock_balances` |
| **Cost** | warehouse_id, product_id, currency_id | quantity, amount | `reg_cost_movements` | `reg_cost_balances` |
| **Settlement** | counterparty_id, contract_id, currency_id | amount | `reg_settlement_movements` | `reg_settlement_balances` |

### Добавление нового регистра

1. Определить entity в `core/entity/register.go` (Movement + Balance structs, embed `MovementBase`)
2. Создать domain-пакет `domain/registers/{name}/` с `Repository` интерфейсом и `Service`
3. Создать repo в `register_repo/{name}.go`, embed `BaseAccumulationRepo[T]`
4. Добавить SQL миграцию (movements + balances + trigger + recalculate function)
5. Добавить `{Name}Visitor` в `posting/visitor.go` + `{Name}MovementSource` interface
6. Создать `{Name}Recorder` реализующий `RegisterRecorder` (и `PostingValidator` при необходимости) в `posting/recorder.go`
7. Зарегистрировать visitor + recorder при создании Engine:
   ```go
   recorders := posting.DefaultRecorders(stockSvc, costSvc, settlementSvc)
   recorders = append(recorders, &custom.MyRecorder{})
   engine := posting.NewEngine(docLocker, recorders...)
   engine.AddVisitor(&custom.MyVisitor{})
   ```
8. Добавить данные в `MovementSet`: использовать `set.SetExtension("name", movements)` и `set.GetExtension("name")` для кастомных регистров (встроенные используют типизированные поля `StockMovements`, `CostMovements`, `SettlementMovements`)

---

## Пакетные операции (Batch Actions)

Пакетные операции позволяют провести/отменить проведение/пометить на удаление сразу множество документов. Поддерживаются два режима:

### Режим 1: Явные ID (BatchAction)

```
POST /api/v1/document/{entity}/batch-action
Content-Type: application/json

{
  "ids": ["uuid1", "uuid2", ...],    // max 500
  "action": "post"                    // post | unpost | setDeletionMark | clearDeletionMark
}
```

Клиент передаёт конкретный список ID (до 500 штук). Используется при ручном выделении строк на текущей странице.

### Режим 2: По фильтру — Virtual Select All (BatchActionByFilter)

```
POST /api/v1/document/{entity}/batch-action-by-filter
Content-Type: application/json
Accept: text/event-stream          // ← опционально, включает SSE-стриминг

{
  "filter": [...],                  // JSON-encoded []filter.Item (текущие фильтры списка)
  "action": "post",
  "excludeIds": ["uuid3"],          // ID, которые пользователь вручную снял
  "includeDeleted": false,
  "orderBy": "-date",
  "search": ""
}
```

Используется при "виртуальном выделении всех" — клиент нажимает «Выбрать все N документов», сервер сам разрешает ID:

```
Client: «Выбрать все 2 000»
  ↓
Server: ListIDs(filter, limit=100000)    → []id.ID
  ↓ remove excludeIds
  ↓ dispatch by Accept header
  ├── Accept: text/event-stream → SSE streaming
  └── default → JSON response
```

**Safety limit:** `DefaultBatchFilterLimit = 100 000` — максимальное количество ID, разрешаемых за один запрос.

---

## Параллельная обработка (Worker Pool)

Все batch-операции обрабатываются **параллельно** через bounded worker pool.

### Архитектура

```
Main goroutine                    Worker Pool (sem = 5)
┌──────────────────┐              ┌──────────────────────────┐
│ for r := range   │◄── results ──│ goroutine 1: Post(doc_1) │
│   results {      │    channel   │ goroutine 2: Post(doc_2) │
│   processed++    │              │ goroutine 3: Post(doc_3) │
│   writeSSE(...)  │              │ goroutine 4: Post(doc_4) │
│ }                │              │ goroutine 5: Post(doc_5) │
└──────────────────┘              └──────────────────────────┘
```

### Ключевые свойства

- **`batchConcurrency`** — настраивается через `Настройки → Производительность` (по умолчанию 5). Ограничивается `ClampBatchConcurrency(value, maxConnsPerTenant/2)` для защиты от исчерпания пула подключений.
- **Каждый документ — отдельная транзакция.** Ошибка в одном документе не откатывает другие.
- **Advisory Lock** на документ (`LockDocument`) предотвращает двойное проведение одного и того же документа.
- **SSE-записи** происходят **только** на main goroutine — нет concurrent writes в response writer.
- **Отмена (Cancellation):** при `ctx.Done()` новые workers не запускаются, in-flight workers завершаются (их DB-операции получают context cancellation).
- **Graceful fallback:** если `sys_settings` недоступна — используется `defaultBatchConcurrency = 5`.

### Производительность

| Документов | Sequential (1 worker) | Parallel (5 workers) | Ускорение |
|-----------|----------------------|---------------------|-----------|
| 2 000     | ~120 сек             | ~27 сек             | ~4.5x     |

### Реализация

```go
// internal/infrastructure/http/v1/handlers/document.go

// Concurrency now read dynamically from sys_settings.performance.batchConcurrency
// with clamp and fallback:
func (h *BaseDocumentHandler[T, C, U]) getBatchConcurrency(ctx context.Context) int {
    if h.settingsRepo == nil { return defaultBatchConcurrency } // 5
    s, _ := h.settingsRepo.Get(ctx)
    return settings.ClampBatchConcurrency(s.Performance.BatchConcurrency, maxConnsPerTenant)
}

func (h *BaseDocumentHandler[T, C, U]) executeBatchConcurrent(
    ctx context.Context, ids []id.ID, action string, concurrency int,
) <-chan batchWorkerResult {
    results := make(chan batchWorkerResult, concurrency*2)
    sem := make(chan struct{}, concurrency)

    go func() {
        defer close(results)
        var wg sync.WaitGroup

        for i, docID := range ids {
            select {
            case <-ctx.Done():
                wg.Wait()
                return
            case sem <- struct{}{}: // acquire
            }

            wg.Add(1)
            go func(idx int, did id.ID) {
                defer func() { <-sem; wg.Done() }()
                err := h.executeAction(ctx, did, action)
                results <- batchWorkerResult{idx: idx, id: did, err: err}
            }(i, docID)
        }
        wg.Wait()
    }()
    return results
}
```

---

## SSE-стриминг прогресса

При передаче `Accept: text/event-stream` сервер переключается в режим SSE — поток событий прогресса вместо одного JSON-ответа.

### Протокол

```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no                // отключает буферизацию Nginx

data: {"type":"started","total":2000}

data: {"type":"progress","processed":50,"success":48,"failed":2,"total":2000}
data: {"type":"progress","processed":100,"success":95,"failed":5,"total":2000}
...
data: {"type":"completed","processed":2000,"success":1980,"failed":20,"total":2000}
```

### Типы событий

| Тип | Когда | Поля |
|-----|-------|------|
| `started` | В начале | `total` |
| `progress` | Каждые 50 документов | `processed`, `success`, `failed`, `total` |
| `completed` | По завершении | `processed`, `success`, `failed`, `total` |
| `cancelled` | При отмене клиентом | `processed`, `success`, `failed`, `total` |

**Интервал прогресса:** `sseProgressInterval = 50` — событие `progress` отправляется каждые 50 обработанных документов.

### Обработка таймаутов

Go `http.Server.WriteTimeout` (30 сек по умолчанию) убивает долгие SSE-соединения. Решение — снятие deadline для конкретного запроса:

```go
// Для SSE-запросов: снимаем write deadline
rc := http.NewResponseController(c.Writer)
_ = rc.SetWriteDeadline(time.Time{})   // Go 1.20+, per-request override
```

### Клиентская отмена

Клиент может прервать операцию через `AbortController`:

```typescript
const controller = new AbortController();
fetchSSE(url, body, {
  signal: controller.signal,
  onEvent: (event) => { /* update progress toast */ },
});
// Отмена:
controller.abort();
```

При разрыве соединения:
1. `ctx.Done()` срабатывает на сервере
2. Новые workers не запускаются
3. In-flight workers получают контекст отмены и завершаются
4. Отправляется финальное событие `cancelled`

---

## Фронтенд: Virtual Select All

### UX-поток (Gmail-style)

```
1. Пользователь нажимает ☑ в заголовке → выделяются строки текущей страницы
2. Появляется баннер: «Выбраны все 100 на странице. Выбрать все 2 000?»
3. Клик → virtual mode ON (selectedIds пустой, excludedIds пустой)
4. Можно снять ☑ с отдельных строк → они попадают в excludeIds
5. Контекстное меню: «Провести (1 999)» (2000 − 1 excluded)
6. Клик → POST batch-action-by-filter с excludeIds + Accept: text/event-stream
7. Прогресс-toast: «Проведено: 200 / 1 999 (10%)» + кнопка «Отмена»
```

### Ключевые хуки

| Хук | Ответственность |
|-----|----------------|
| `useListSelection` | Состояние выделения: `selectedIds`, `excludedIds`, `virtualMode`, `virtualTotal` |
| `useDocumentBatchActions` | Оркестрация batch-вызовов, SSE-прогресс, toast, отмена |

### SSE-клиент

```typescript
// frontend/lib/sse-fetch.ts
// POST-based SSE клиент через fetch + ReadableStream
// Поддержка: auth headers, tenant context, AbortSignal, JSON fallback
```

---

## Системные настройки (sys_settings)

Таблица `sys_settings` — single-row tenant-level конфигурация (аналог "Константы" в 1С:Предприятие).

### Архитектура

```
┌─────────────────────────────────────────────────────┐
│ sys_settings (singleton = TRUE)                    │
├─────────────┬───────────────────────────────────────┤
│ organization│ JSONB: company, INN, KPP, contacts    │
│ accounting  │ JSONB: tax, VAT, currency, numbering  │
│ performance │ JSONB: {"batchConcurrency": 5}        │
│ version     │ INT: optimistic locking counter       │
│ updated_at  │ TIMESTAMPTZ                           │
└─────────────┴───────────────────────────────────────┘
```

### API

| Метод | Endpoint | Описание |
|-------|----------|----------|
| `GET` | `/api/v1/settings` | Получить все секции |
| `PATCH` | `/api/v1/settings/:section` | Обновить секцию с оптимистичной блокировкой |

### Оптимистичная блокировка

```
PATCH /api/v1/settings/performance
{
  "data": {"batchConcurrency": 3},
  "version": 1
}

→ 200 OK             // version → 2
→ 409 Conflict       // другой пользователь обновил раньше
```

### Safety Clamp

```go
// internal/domain/settings/model.go
func ClampBatchConcurrency(value, maxConnsPerTenant int) int {
    maxPoolHalf := maxConnsPerTenant / 2   // 10/2 = 5
    return max(1, min(value, maxPoolHalf))
}
```

### Frontend

Секция `Настройки → Производительность` с ползунком 1–5 и рекомендацией.
Сохранение через `useSettingsStore.saveSection("performance")`.

---

## Связанные документы

- [05-domain-layer.md](05-domain-layer.md) — GenerateMovements в моделях документов
- [11-transactions.md](11-transactions.md) — TxManager и транзакции
- [04-core-layer.md](04-core-layer.md) — StockMovement, CostMovement, SettlementMovement типы
- [09-crud-pipeline.md](09-crud-pipeline.md) — общий CRUD pipeline
