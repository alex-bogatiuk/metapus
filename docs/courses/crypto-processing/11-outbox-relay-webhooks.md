# Модуль 11: Outbox Relay и Система Вебхуков

> **Цель:** Понять, как Metapus гарантированно доставляет события из транзакционной БД во внешний мир — через Transactional Outbox Pattern и HMAC-подписанные вебхуки.

---

## 11.1. Проблема: Dual-Write

Когда документ проводится (Post), нужно **одновременно**:
1. Записать движения в регистры (БД)
2. Уведомить мерчанта (HTTP webhook)
3. Запустить автоматизации (Telegram, email)

Наивный подход — вызвать HTTP внутри транзакции — ломается по трём причинам:

```
                    ┌─────────────────────────┐
 TX BEGIN           │  1. INSERT movements     │ ✅ OK
                    │  2. POST webhook         │ ❌ Timeout 10s
                    │  3. TX COMMIT            │ 💀 statement_timeout 30s
                    └─────────────────────────┘
```

| Проблема | Почему критично |
|----------|----------------|
| **Длинная TX** | HTTP timeout (10s) + retries блокируют connection pool |
| **Частичный отказ** | TX откатилась, но webhook уже доставлен → inconsistency |
| **Порядок** | Параллельные goroutines → события приходят не по порядку |

**Решение:** Transactional Outbox Pattern.

---

## 11.2. Архитектура Outbox

```
┌──────────────────────────────────────────────────────────────┐
│                      TRANSACTION                             │
│                                                              │
│   PostingEngine.Post()                                       │
│     ├─ RecordMovements → reg_stock_movements                 │
│     └─ updateDoc(ctx) → doc_goods_receipts                   │
│                                                              │
│   DocumentOutboxDecorator.Post()                             │
│     └─ OutboxPublisher.Publish() → sys_outbox (pending)      │
│                                                              │
│   COMMIT ─────── атомарно: движения + outbox msg ────────────│
└──────────────────────────────────────────────────────────────┘

           ↕ 500ms polling

┌──────────────────────────────────────────────────────────────┐
│  Worker: OutboxRelay.ProcessBatch()                          │
│    Phase 1: CTE + FOR UPDATE SKIP LOCKED → claim batch       │
│    Phase 2: handler.Handle(msg) → Telegram / Email / Webhook │
│    Phase 3: Mark published / retry on failure                │
└──────────────────────────────────────────────────────────────┘
```

### Ключевые гарантии

| Гарантия | Как обеспечена |
|----------|---------------|
| **At-least-once delivery** | Retry с exponential backoff до 5 попыток |
| **Атомарность** | Outbox INSERT в той же TX, что и бизнес-операция |
| **Ordering** | `ORDER BY created_at` в relay query |
| **No lost messages** | `RecoverStuck()` возвращает зависшие в `processing` |
| **Горизонтальное масштабирование** | `FOR UPDATE SKIP LOCKED` — несколько relay не конфликтуют |

---

## 11.3. Схема данных

### Файл: `db/migrations/00002_sys_core.sql`

```sql
-- Partitioned для эффективной архивации по дате
CREATE TABLE sys_outbox (
    id             UUID          NOT NULL,
    aggregate_type VARCHAR(50)   NOT NULL,    -- "goods_receipt", "crypto_invoice"
    aggregate_id   UUID          NOT NULL,    -- ID документа
    event_type     VARCHAR(50)   NOT NULL,    -- "posted", "created", "updated"
    payload        JSONB         NOT NULL,    -- полный snapshot документа
    status         outbox_status NOT NULL DEFAULT 'pending',
    retry_count    INT           NOT NULL DEFAULT 0,
    last_error     TEXT,
    next_retry_at  TIMESTAMPTZ,              -- exponential backoff
    created_at     TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    published_at   TIMESTAMPTZ,
    PRIMARY KEY (id, created_at)             -- composite PK для partitioning
) PARTITION BY RANGE (created_at);
```

**Индексы — ключ к производительности relay:**

```sql
-- Relay poll: "дай pending, готовые к retry"
CREATE INDEX idx_outbox_pending ON sys_outbox (created_at)
    WHERE status = 'pending';

-- Retry scheduling
CREATE INDEX idx_outbox_retry ON sys_outbox (next_retry_at)
    WHERE status = 'pending' AND next_retry_at IS NOT NULL;

-- Stuck recovery: "кто завис в processing?"
CREATE INDEX idx_outbox_stuck ON sys_outbox (created_at)
    WHERE status = 'processing';
```

### Dead Letter Queue

Сообщения, провалившие 5 попыток, перемещаются в `sys_outbox_dlq`:

```sql
CREATE TABLE sys_outbox_dlq (
    id             UUID PRIMARY KEY,
    aggregate_type VARCHAR(50) NOT NULL,
    payload        JSONB       NOT NULL,
    retry_count    INT         NOT NULL,
    last_error     TEXT,
    created_at     TIMESTAMPTZ NOT NULL,
    failed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    failure_reason TEXT
);
```

---

## 11.4. Publisher: Запись в Outbox

### Файл: `internal/infrastructure/storage/postgres/outbox.go`

```go
// Publish ДОЛЖЕН вызываться внутри транзакции.
// Это ключевой инвариант: если TX откатится — outbox msg тоже откатится.
func (p *OutboxPublisher) Publish(ctx context.Context, event domain.DomainEvent) error {
    txManager := MustGetTxManager(ctx)
    tx := txManager.GetTx(ctx)
    if tx == nil {
        return fmt.Errorf("outbox publish requires transaction context")
    }

    payloadBytes, _ := json.Marshal(event.Payload)

    _, err = tx.Exec(ctx, `
        INSERT INTO sys_outbox (id, aggregate_type, aggregate_id,
                                event_type, payload, status, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
    `, id.New(), event.AggregateType, event.AggregateID,
       event.EventType, payloadBytes, OutboxStatusPending, time.Now().UTC())

    return err
}
```

### Batch-вариант с `pgx.Batch`

Для операций, генерирующих несколько событий (UpdateAndRepost → `updated` + `posted`), используется `PublishBatch` с `pgx.Batch` — один round-trip вместо N:

```go
func (p *OutboxPublisher) PublishBatch(ctx context.Context, events []domain.DomainEvent) error {
    batch := &pgx.Batch{}
    for _, event := range events {
        payloadBytes, _ := json.Marshal(event.Payload)
        batch.Queue(`INSERT INTO sys_outbox ...`, ...)
    }
    results := tx.SendBatch(ctx, batch) // 1 round-trip
    defer results.Close()
    // ...
}
```

---

## 11.5. Decorator Pattern: Автоматическая генерация событий

### Файл: `internal/domain/document_automation.go`

Вместо ручного вызова `outbox.Publish()` в каждом handler'е, используется **Decorator Pattern** (Middleware):

```go
// WithOutboxEvents оборачивает DocumentService[T] прозрачным декоратором,
// который публикует outbox-события ПОСЛЕ успешного выполнения операции.
func WithOutboxEvents[T any](
    entityName string,
    publisher  OutboxPublisher,
    resolver   CurrencyMetadataResolver,
) ServiceMiddleware[T] {
    return func(next DocumentService[T]) DocumentService[T] {
        if publisher == nil {
            return next // graceful degradation
        }
        return &DocumentOutboxDecorator[T]{
            next: next, publisher: publisher,
            entityName: entityName, currencyResolver: resolver,
        }
    }
}
```

### Регистрация — одна строка на сущность

```go
// internal/content/document_registrations.go
domain.WithOutboxEvents[*goods_receipt.GoodsReceipt](
    "goods_receipt", deps.OutboxPublisher, deps.CurrencyMetadataResolver,
),
domain.WithOutboxEvents[*crypto_invoice.CryptoInvoice](
    "crypto_invoice", deps.OutboxPublisher, deps.CurrencyMetadataResolver,
),
domain.WithOutboxEvents[*crypto_payment.CryptoPayment](
    "crypto_payment", deps.OutboxPublisher, deps.CurrencyMetadataResolver,
),
```

### Тонкость: `emitInOwnTx`

PostingEngine открывает и **коммитит собственную TX**. К моменту возврата в decorator TX уже закрыта. Поэтому для `Post`/`Unpost` используется `emitInOwnTx`:

```go
func (d *DocumentOutboxDecorator[T]) emitInOwnTx(ctx context.Context, action string, entity T) {
    ev := d.buildEvent(ctx, action, entity)
    txm, _ := tenant.GetTxManager(ctx)
    // Открываем НОВУЮ короткую TX только для INSERT в outbox
    txm.RunInTransaction(ctx, func(txCtx context.Context) error {
        return d.publisher.Publish(txCtx, *ev)
    })
}
```

### Обогащение payload: `humanAmounts`

Decorator автоматически конвертирует `MinorUnits` → `float64` и `CryptoAmount` → `int64` через reflection, чтобы шаблоны автоматизаций могли работать с человекочитаемыми значениями:

```go
// 150000 (MinorUnits, 2 decimals) → 1500.00
// 6000000 (CryptoAmount)          → 6000000 (без конвертации — divisor на frontend)
```

---

## 11.6. Relay: Доставка сообщений

### Файл: `internal/infrastructure/storage/postgres/outbox.go`

Relay использует **Two-Phase Atomic Claim Pattern**:

```go
func (r *OutboxRelay) ProcessBatch(ctx context.Context) (int, error) {
    // ══════ Phase 1: Atomic Claim ══════
    // CTE + FOR UPDATE SKIP LOCKED — один SQL statement = implicit TX.
    // Несколько relay instances НЕ конфликтуют.
    rows, _ := r.pool.Query(ctx, `
        WITH batch AS (
            SELECT id, created_at
            FROM sys_outbox
            WHERE status = $1
              AND (next_retry_at IS NULL OR next_retry_at <= NOW())
            ORDER BY created_at          -- FIFO ordering
            LIMIT $2
            FOR UPDATE SKIP LOCKED       -- concurrency-safe
        )
        UPDATE sys_outbox o
        SET status = $3                  -- pending → processing
        FROM batch b
        WHERE o.id = b.id AND o.created_at = b.created_at
        RETURNING o.*
    `, OutboxStatusPending, r.batchSize, OutboxStatusProcessing)

    // ══════ Phase 2: Process OUTSIDE transaction ══════
    for _, msg := range messages {
        r.processMessage(ctx, msg) // может вызывать HTTP, Telegram API
    }
}
```

### Retry с exponential backoff

```go
func (r *OutboxRelay) processMessage(ctx context.Context, msg *OutboxMessage) error {
    err := r.handler.Handle(ctx, msg)

    if err != nil {
        // Exponential backoff: 1min, 2min, 3min, 4min, 5min → DLQ
        nextRetry := time.Now().Add(time.Duration(msg.RetryCount+1) * time.Minute)

        _, _ = r.pool.Exec(ctx, `
            UPDATE sys_outbox
            SET status = CASE WHEN retry_count >= 4 THEN 'failed' ELSE 'pending' END,
                retry_count = retry_count + 1,
                last_error = $3,
                next_retry_at = $4
            WHERE id = $5
        `, ...)
        return err
    }

    // ✅ Success → mark published
    r.pool.Exec(ctx, `
        UPDATE sys_outbox SET status = 'published', published_at = $1 WHERE id = $2
    `, now, msg.ID)
    return nil
}
```

### Recovery: зависшие сообщения

Если worker упал посреди обработки, сообщение застряло в `processing`. Раз в час:

```go
func (r *OutboxRelay) RecoverStuck(ctx context.Context, timeout time.Duration) (int64, error) {
    // Всё, что в 'processing' дольше 5 минут → обратно в 'pending'
    cutoff := time.Now().Add(-timeout)
    result, _ := r.pool.Exec(ctx, `
        UPDATE sys_outbox SET status = 'pending'
        WHERE status = 'processing' AND created_at < $1
    `, cutoff)
    return result.RowsAffected(), nil
}
```

---

## 11.7. Worker Integration

### Файл: `cmd/worker/main.go`

```go
// ── Wiring ──
engine, _ := w.buildAutomationEngine()
handler := &automationOutboxHandler{engine: engine, log: w.log}
relay := postgres.NewOutboxRelay(mp.Pool(), 100, handler)

pollInterval := 500 * time.Millisecond
ticker := time.NewTicker(pollInterval)
defer ticker.Stop()

// ── Main Loop ──
for {
    select {
    case <-ctx.Done():
        return
    case <-ticker.C:
        // RecordIfWork: skip DB write when outbox empty (99.9% of ticks)
        recorder.RecordIfWork(ctx, "outbox.relay", "outbox", func(ctx context.Context) (int, error) {
            return relay.ProcessBatch(ctx)
        })
    case <-cleanupTicker.C:  // 1 hour
        // Recover stuck messages
        relay.RecoverStuck(ctx, postgres.DefaultStuckTimeout())
    }
}
```

### Handler: Outbox → Automation Engine

```go
type automationOutboxHandler struct {
    engine *automation.Engine
    log    *logger.Logger
}

func (h *automationOutboxHandler) Handle(ctx context.Context, msg *postgres.OutboxMessage) error {
    var payload map[string]any
    json.Unmarshal(msg.Payload, &payload)

    // Fix JSON numbers: float64 → int64 для Go templates
    automation.SanitizePayloadNumbers(payload)

    // Специальный replay-маршрут
    if msg.AggregateType == "automation_history" && msg.EventType == "replay" {
        return h.engine.DeliverReplay(ctx, historyID)
    }

    // Основной маршрут: запуск правил автоматизации
    return h.engine.HandleEvent(ctx, msg.EventType, payload)
}
```

---

## 11.8. Webhook Dispatcher

### Файл: `internal/domain/crypto/webhook.go`

Вебхуки — один из **адаптеров** в Automation Engine. Мерчант получает HTTP POST при ключевых событиях платёжного цикла.

### Типы событий

| Event | Когда | Данные |
|-------|-------|--------|
| `invoice.paid` | Первый платёж по инвойсу | invoiceId, amount, txHash |
| `invoice.confirmed` | Платёж набрал нужные подтверждения | invoiceId, paymentId, confirmations |
| `invoice.expired` | TTL инвойса истёк | invoiceId, expiredAt |
| `withdrawal.confirmed` | Вывод средств подтверждён | withdrawalId, amount, txHash |

### HMAC-SHA256 подпись (Stripe-pattern)

```go
// Headers, которые получает мерчант:
// X-Metapus-Event:       "invoice.confirmed"
// X-Metapus-Signature:   HMAC-SHA256(timestamp + "." + body, secret)
// X-Metapus-Timestamp:   "2026-05-19T10:30:00Z"
// X-Metapus-Delivery-ID: "019e3e2d-4a96-7f23-..."  (для идемпотентности)

func (d *WebhookDispatcher) sign(payload []byte, secret, timestamp string) string {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(timestamp))  // timestamp включён в подпись
    mac.Write([]byte("."))        // разделитель
    mac.Write(payload)            // тело запроса
    return hex.EncodeToString(mac.Sum(nil))
}
```

**Зачем timestamp в подписи?** Без него атакующий может перехватить и переиграть (replay) старый webhook. Мерчант должен проверять: `|now - timestamp| < 5 минут`.

### SSRF Protection

Webhook URL контролируется мерчантом → вектор SSRF. Три уровня защиты:

```
┌─────────────────────────────────────────────────┐
│  Level 1: Validate at merchant creation         │
│  urlsafe.ValidatePublicURL(url, "webhookUrl")   │
│    → HTTPS only                                 │
│    → No localhost, *.internal                    │
│    → DNS resolution → no private IPs            │
│    → No 169.254.x.x (cloud metadata)            │
├─────────────────────────────────────────────────┤
│  Level 2: Re-validate at dispatch time          │
│  ValidateWebhookURL(webhookURL) // defence-in-  │
│  depth against DNS rebinding                    │
├─────────────────────────────────────────────────┤
│  Level 3: Block redirects in HTTP client        │
│  CheckRedirect: return http.ErrUseLastResponse  │
│  → prevents redirect chain to internal IPs      │
└─────────────────────────────────────────────────┘
```

### Полный `Dispatch` flow

```go
func (d *WebhookDispatcher) Dispatch(ctx context.Context,
    webhookURL, webhookSecret string,
    event WebhookEventType,
    data map[string]interface{},
) error {
    // 1. Defence-in-depth: re-validate URL
    if err := ValidateWebhookURL(webhookURL); err != nil {
        return err
    }

    // 2. Build payload
    payload := WebhookPayload{
        Event:     event,
        Timestamp: time.Now().UTC(),
        Data:      data,
    }
    body, _ := json.Marshal(payload)

    // 3. Sign with HMAC-SHA256
    signature := d.sign(body, webhookSecret, payload.Timestamp.Format(time.RFC3339))

    // 4. Build request with security headers
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
    req.Header.Set("X-Metapus-Event", string(event))
    req.Header.Set("X-Metapus-Signature", signature)
    req.Header.Set("X-Metapus-Timestamp", payload.Timestamp.Format(time.RFC3339))
    req.Header.Set("X-Metapus-Delivery-ID", id.New().String())

    // 5. Deliver (timeout 10s, no redirects)
    resp, err := d.httpClient.Do(req)
    if resp.StatusCode >= 300 {
        return fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
    }
    return nil
}
```

---

## 11.9. Жизненный цикл сообщения

```
                    ┌─────────┐
        TX INSERT → │ pending │ ← RecoverStuck (если зависло в processing)
                    └────┬────┘
                         │ relay claims (FOR UPDATE SKIP LOCKED)
                    ┌────▼──────┐
                    │processing │
                    └────┬──────┘
                    ┌────┴────┐
               success     failure
                    │         │
              ┌─────▼──┐  ┌──▼──────┐
              │published│  │ pending │ ← retry_count++, next_retry_at
              └────────┘  └────┬────┘
                               │ retry_count >= 5
                          ┌────▼───┐
                          │ failed │ → MoveToDLQ() → sys_outbox_dlq
                          └────────┘
```

**FSM переходы:**

| From | To | Условие |
|------|----|---------|
| `pending` | `processing` | Relay claim (CTE + SKIP LOCKED) |
| `processing` | `published` | Handler.Handle() → success |
| `processing` | `pending` | Handler.Handle() → error, retry_count < 5 |
| `processing` | `pending` | RecoverStuck (timeout 5 min) |
| `pending` | `failed` | retry_count ≥ 5 |
| `failed` | DLQ | MoveToDLQ() (hourly cleanup) |

---

## 11.10. Вопросы для закрепления

### Вопрос 1: Dual Write
> Почему нельзя вызывать webhook **внутри** транзакции проведения документа?

<details>
<summary>Ответ</summary>

1. **Длинная TX:** HTTP-вызов (timeout 10s) держит DB connection, блокируя pool
2. **Partial failure:** Если TX откатится после успешного webhook → мерчант получил уведомление о несуществующем событии
3. **statement_timeout:** 30s лимит в production, а webhook с retry'ами может занять больше

</details>

### Вопрос 2: SKIP LOCKED
> Зачем `FOR UPDATE SKIP LOCKED` в relay, а не обычный `FOR UPDATE`?

<details>
<summary>Ответ</summary>

`FOR UPDATE` блокирует второй relay instance до commit первого → **sequential** processing. `SKIP LOCKED` позволяет второму relay instance взять **другие** pending messages → **parallel** processing без конфликтов. Это критично для горизонтального масштабирования worker'ов.

</details>

### Вопрос 3: Timestamp в HMAC
> Что произойдёт, если убрать timestamp из HMAC-подписи?

<details>
<summary>Ответ</summary>

Атакующий, перехвативший webhook, сможет **переиграть** его позже (replay attack). С timestamp мерчант проверяет `|now - timestamp| < 5 min` и отклоняет старые запросы. Без timestamp подпись идентична для одинаковых payload'ов.

</details>

### Вопрос 4: SSRF
> Мерчант указал `https://webhook.example.com` при регистрации. Через месяц DNS для `webhook.example.com` был изменён на `169.254.169.254`. Как Metapus защищается?

<details>
<summary>Ответ</summary>

**Defence-in-depth (3 уровня):**
1. DNS validation при создании мерчанта (ValidatePublicURL → LookupHost → assertPublicIP)
2. **Re-validation при каждом dispatch** — повторная DNS-проверка перед отправкой webhook
3. **CheckRedirect block** — даже если атакующий использует redirect chain, HTTP client отклонит

Уровень 2 — ключевой для этого сценария (DNS rebinding).

</details>

---

## 11.11. Диаграмма: End-to-End Flow

```
Merchant API                    Database                      Worker
     │                             │                            │
     │  POST /invoices             │                            │
     │ ──────────────────────────▶ │                            │
     │                             │ TX: INSERT invoice         │
     │                             │     INSERT sys_outbox      │
     │                             │     COMMIT                 │
     │  ◀── 201 Created ────────── │                            │
     │                             │                            │
     │                             │ ◀── 500ms poll ─────────── │
     │                             │                            │
     │                             │ CTE: claim pending msgs    │
     │                             │ ──────────────────────────▶ │
     │                             │                            │
     │                             │      Handler.Handle(msg)   │
     │                             │        ├─ Telegram Bot API │
     │                             │        ├─ SMTP Email       │
     │                             │        └─ Webhook HTTP     │
     │                             │                            │
     │  ◀──── X-Metapus-Event ──── │ ◀── mark published ────── │
     │  (invoice.confirmed)        │                            │
```
