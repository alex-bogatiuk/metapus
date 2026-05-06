# Криптопроцессинг

> **TL;DR:** Описание подсистемы приёма и обработки криптоплатежей: архитектура, модели, FSM, chain watchers, event processing, compliance и settlement.

> **Тип:** System Documentation
> **Аудитория:** Developer
> **Связанные:** [posting-engine.md](posting-engine.md), [multi-tenancy.md](multi-tenancy.md), [new-document.md](../howto/new-document.md)

---

## 1. Обзор архитектуры

Криптопроцессинг Metapus — это подсистема приёма, подтверждения и учёта криптоплатежей, интегрированная в существующий Clean Architecture стек. Ключевой принцип — **максимальное переиспользование** существующих абстракций (PostingEngine, Worker, BaseDocumentService) вместо создания параллельной инфраструктуры.

### Архитектурные слои

```mermaid
graph TB
    subgraph "Merchant API"
        API["REST API<br/>(Gin handlers)"]
        WH["Webhook Dispatcher<br/>(HMAC-SHA256)"]
    end

    subgraph "Domain — Business Logic"
        EP["EventProcessor<br/>(chain-agnostic)"]
        FSM["PaymentFSM<br/>(transition matrix)"]
        WP["WalletPool<br/>(lease/release)"]
        CE["ComplianceEngine<br/>(AML/KYT)"]
        SE["SettlementService<br/>(crypto-to-crypto)"]
    end

    subgraph "Worker — Background Processing"
        CP["CryptoProcessor<br/>(per-tenant)"]
        TW["TRON Watcher<br/>(TronGrid API)"]
        EL["Expiration Loop<br/>(invoice TTL)"]
    end

    subgraph "Registers (Immutable Ledger)"
        CB["reg_crypto_balance"]
        CF["reg_crypto_fee"]
    end

    subgraph "External"
        TG["TronGrid / Nodes"]
        VS["HashiCorp Vault"]
    end

    API --> EP
    TW --> |BlockchainEvent| EP
    EP --> FSM
    EP --> WP
    EP --> CB
    FSM --> WH
    SE --> CF
    CP --> TW
    CP --> EL
    TW --> TG
    WP --> VS
```

### Что переиспользуется

| Существующий компонент | Роль в криптопроцессинге |
|---|---|
| `PostingEngine` + Visitor + Recorder | Крипто-регистры: `CryptoBalanceMovement`, `CryptoFeeMovement` |
| `BaseDocumentService[T, L]` | Базовый сервис для `CryptoInvoice`, `CryptoPayment` |
| `CatalogService[T]` | Справочники: `BlockchainNetwork`, `Token`, `Wallet`, `Merchant` |
| `types.CryptoAmount` (big.Int) | Суммы для 18+ decimals (ETH wei, TRON sun) |
| `Worker` (per-tenant goroutines) | Хост для `CryptoProcessor` + `ChainWatcher` |
| `entity.MovementBase` | Immutable ledger для крипто-остатков |

---

## 2. Справочники (Catalogs)

### 2.1. BlockchainNetwork

Определяет поддерживаемые блокчейн-сети и их параметры.

| Поле | Тип | Описание |
|------|-----|----------|
| `ChainID` | string | Уникальный идентификатор сети (`"tron"`, `"ethereum"`) |
| `NativeTokenSymbol` | string | Символ нативной монеты (`"TRX"`, `"ETH"`) |
| `NativeDecimals` | int | Количество десятичных знаков нативного токена |
| `ConfirmationsNeeded` | int | Минимальное число подтверждений для финальности |
| `BlockTimeSeconds` | int | Среднее время блока (для UI-отображения) |
| `RPCEndpoint` | string | URL ноды/API (зашифрован AES) |
| `ExplorerURL` | string | Шаблон URL обозревателя блоков |

**Ключевой инвариант:** `ConfirmationsNeeded` — единственный источник требуемых подтверждений. Никогда не хардкодить.

### 2.2. Token

Криптовалюты и токены, привязанные к сети.

| Поле | Тип | Описание |
|------|-----|----------|
| `NetworkID` | FK → BlockchainNetwork | Сеть, в которой выпущен токен |
| `ContractAddress` | string | Адрес смарт-контракта (`""` для нативных) |
| `Symbol` | string | `"USDT"`, `"ETH"`, `"BTC"` |
| `DecimalPlaces` | int | **Никогда не хардкодить!** (6 для USDT-TRC20, 18 для ETH) |
| `TokenStandard` | string | `"TRC-20"`, `"ERC-20"`, `"native"` |
| `SweepThreshold` | CryptoAmount | Мин. баланс для свипа (minor units). 0 = свип после каждого платежа |
| `SweepMaxAgeHours` | int | Макс. часов до принудительного свипа. 0 = отключено |

> [!NOTE]
> `SweepThreshold` и `SweepMaxAgeHours` — **дефолтные** значения. Мерчант может переопределить их через `reg_merchant_token_config`.

### 2.3. Wallet

Блокчейн-адреса под управлением платформы. Организованы по уровням (tier), режимам аллокации (allocation mode) и состояниям (status).

**Уровни (Tier):**

| Tier | Назначение |
|------|-----------|
| `Pool` | Клиентские адреса (HD-derived). Арендуются инвойсами для приёма платежей |
| `Hot` | Целевой кошелёк для sweep. Высокочастотные операции |
| `Warm` | Буфер для settlement. Средняя ликвидность |
| `Cold` | Холодное хранилище. Долгосрочное хранение |

**Режим аллокации (Allocation Mode):**

| Mode | Описание |
|------|-----------|
| `transient` | Кошелёк арендуется под конкретный инвойс, возвращается после оплаты. **По умолчанию.** |
| `persistent` | Кошелёк привязан к клиенту (`CustomerRef`) на постоянной основе. Не возвращается в пул. |

**Состояния (Status):**

```mermaid
stateDiagram-v2
    [*] --> Free
    Free --> Leased: Lease(invoiceID) [transient]
    Free --> Assigned: AssignToCustomer() [persistent]
    Leased --> Free: Release [threshold > 0]
    Leased --> SweepPending: Sweep [threshold = 0]
    Assigned --> SweepPending: Sweep Evaluation
    SweepPending --> Free: Sweep Completed [transient]
    SweepPending --> Assigned: Sweep Completed [persistent]
    Leased --> Free: Invoice Expired
    Free --> Frozen: Compliance Hold
    Frozen --> Free: Manual Release
```

**Новые поля:**

| Поле | Тип | Описание |
|------|-----|----------|
| `AllocationMode` | string | `transient` или `persistent` |
| `CustomerRef` | string | Внешний ID клиента (для persistent) |
| `LastSweptAt` | *time.Time | Время последнего свипа |

**Lease-механизм:** при создании `CryptoInvoice` свободный `Pool`-кошелёк с `allocation_mode = 'transient'` арендуется (`Leased`) на время TTL инвойса. При threshold > 0 кошелёк **возвращается в пул** после подтверждения, а свип выполняется фоновым job'ом.

### 2.4. Merchant

Мерчанты — бизнес-клиенты, принимающие платежи. Содержат API-ключи, тарифы комиссий и webhook-настройки.

### 2.5. MerchantTokenConfig (Регистр сведений)

Таблица `reg_merchant_token_config` — **регистр сведений** для per-merchant переопределения крипто-настроек. `NULL` = использовать дефолт из `cat_tokens`.

| Поле | Тип | Описание |
|------|-----|----------|
| `MerchantID` | FK → Merchant | Мерчант (часть PK) |
| `TokenID` | FK → Token | Токен (часть PK) |
| `SweepThreshold` | NUMERIC (nullable) | Переопределение порога свипа. NULL = дефолт токена |
| `SweepMaxAgeHours` | INT (nullable) | Переопределение макс. возраста. NULL = дефолт токена |

**Резолвинг (NULL-coalescing):** `SweepConfigResolver` применяет приоритет:

```
Мерчант override (reg_merchant_token_config) → Token default (cat_tokens)
```

---

## 3. Документы (Documents)

### 3.1. CryptoInvoice — Запрос на оплату

Создаётся мерчантом через API. Представляет запрос на оплату с ожидаемой суммой.

**Жизненный цикл:**

```mermaid
stateDiagram-v2
    [*] --> Created: Мерчант создаёт инвойс
    Created --> PartiallyPaid: Получена частичная оплата
    Created --> Paid: Получена полная сумма
    Created --> Expired: TTL истёк
    Created --> Cancelled: Отмена мерчантом
    PartiallyPaid --> Paid: Получена оставшаяся сумма
    Paid --> Confirmed: Достигнуты подтверждения
    Expired --> [*]
    Cancelled --> [*]
    Confirmed --> [*]
```

**Ключевые поля:**

| Поле | Тип | Описание |
|------|-----|----------|
| `MerchantID` | FK | Мерчант-создатель |
| `TokenID` | FK | Токен для оплаты |
| `WalletID` | FK (nullable) | Арендованный кошелёк |
| `ExpectedAmount` | CryptoAmount | Ожидаемая сумма (minor units) |
| `ReceivedAmount` | CryptoAmount | Фактически полученная сумма |
| `Status` | InvoiceStatus | Текущий статус FSM |
| `ExpiresAt` | time.Time | Время истечения (default 30 min) |
| `CallbackURL` | string | Webhook URL для уведомлений |
| `ExternalID` | string | Idempotency key мерчанта |

**Связь с Posting Engine:** при подтверждении (`Confirmed`) инвойс проводится — записывается `CryptoBalanceMovement` (приход на кошелёк).

### 3.2. CryptoPayment — Зафиксированный платёж

Создаётся **автоматически** chain watcher'ом при обнаружении входящей транзакции. Не редактируется вручную.

**FSM:**

```mermaid
stateDiagram-v2
    [*] --> Detected: Транзакция обнаружена в блокчейне
    Detected --> Confirming: ≥1 подтверждение
    Confirming --> Confirmed: ≥ RequiredConfs подтверждений
    Confirming --> Reorged: Реорганизация цепи
    Confirmed --> Settled: Средства переведены мерчанту
    Reorged --> Detected: Повторное обнаружение
```

**Матрица переходов (compile-time):**

```go
var _allowedTransitions = map[PaymentStatus][]PaymentStatus{
    PaymentStatusDetected:   {PaymentStatusConfirming},
    PaymentStatusConfirming: {PaymentStatusConfirmed, PaymentStatusReorged},
    PaymentStatusConfirmed:  {PaymentStatusSettled},
    PaymentStatusReorged:    {PaymentStatusDetected},
}
```

**Каждый переход записывается** в `reg_crypto_payment_events` — полный audit trail для финансовой трассируемости.

### 3.3. CryptoSweep — Сбор средств

Sweep переводит средства с пул-кошельков на горячий кошелёк. Создаётся автоматически после подтверждения платежа.

### 3.4. CryptoWithdrawal — Вывод средств

Вывод средств мерчантом с платформы. Инициируется через API.

---

## 4. Обработка событий блокчейна

### 4.1. BlockchainEvent

Нормализованная структура события от любого chain watcher'а. **Chain-agnostic** — EventProcessor не знает о конкретной сети.

```go
type BlockchainEvent struct {
    Network       string            // "tron_mainnet", "ethereum"
    NetworkID     id.ID             // resolved UUID
    TxHash        string            // blockchain tx hash
    FromAddress   string            // sender
    ToAddress     string            // recipient (matched against wallets)
    TokenContract string            // token contract ("" for native)
    Amount        types.CryptoAmount // minor units
    BlockNumber   int64
    Confirmations int
    RequiredConfs int               // from BlockchainNetwork
    EventType     EventType         // Transfer | Confirmation | Reorg
    Timestamp     time.Time
}
```

### 4.2. ChainWatcher (Interface)

Адаптер для конкретной блокчейн-сети. Каждая сеть реализует свой watcher.

```go
type ChainWatcher interface {
    NetworkCode() string
    Start(ctx context.Context, addresses []string, events chan<- BlockchainEvent) error
    GetConfirmations(ctx context.Context, txHash string) (int, error)
}
```

**Текущие реализации:** TRON (через TronGrid API).

### 4.3. TRON Watcher

Реализация `ChainWatcher` для TRON/Shasta.

**Механизм polling:**

1. Загружает checkpoint из `reg_crypto_watcher_state` (last_block, fingerprint)
2. Запрашивает TRC-20 Transfer events через TronGrid API
3. Фильтрует: только транзакции к нашим мониторимым кошелькам
4. Для каждого события запрашивает текущее число подтверждений
5. Эмитирует `BlockchainEvent` в канал
6. Сохраняет checkpoint

**Adaptive polling:**
- Base interval: 3s (время блока TRON)
- При обнаружении событий: сброс к base
- При idle: постепенное замедление до 30s
- При ошибках: экспоненциальный backoff (×2, ceiling 30s)

**Retry:** 3 попытки с экспоненциальным backoff (500ms → 1s → 2s).

### 4.4. EventProcessor

Центральный компонент бизнес-логики — **chain-agnostic**. Оркестрирует полный цикл обработки события.

**Алгоритм `ProcessEvent()`:**

```
1. Guard         → Reject non-positive amounts (zero-value events)
2. Dust Guard    → Reject amounts below dust threshold (default 1000 minor units)
3. Match         → event.ToAddress → find Wallet → find active CryptoInvoice
4. Idempotency   → check CryptoPayment.TxHash — if exists → update confirmations
5. Reorg         → if EventTypeReorg → FSM transition to Reorged
6. Create        → new CryptoPayment(Detected)
7. Invoice       → update ReceivedAmount, recalculate Status
8. Confirmations → Detected→Confirming (≥1), Confirming→Confirmed (≥required)
9. Post          → on Confirmed: posting engine records register movements
10. Wallet       → on Confirmed: handleWalletAfterConfirm()
                    threshold=0 → MarkSweepPending (legacy)
                    threshold>0, transient → Release (back to pool)
                    persistent → no change (stays assigned)
11. Invoice      → on Confirmed: update invoice status to Confirmed
```

**Каждый шаг выполняется внутри транзакции** (`txManager.RunInTransaction`).

**Защита от дублей:** если `CryptoPayment` с таким `TxHash` уже существует — обновляем только `Confirmations` и проверяем FSM-переходы.

---

## 5. Worker Integration

### CryptoProcessor

Per-tenant фоновый процессор, запускаемый внутри Worker'а. Управляет:

- Загрузкой blockchain networks и wallet addresses из БД
- Запуском ChainWatcher goroutine для каждой сети
- Потреблением `BlockchainEvent` из канала → `EventProcessor`
- Invoice expiration ticker (каждые 60 секунд)
- Confirmation poll loop (каждые 10 секунд)
- **Sweep evaluation loop** (каждые 60 секунд)

**Lifecycle:**

```mermaid
sequenceDiagram
    participant W as Worker
    participant CP as CryptoProcessor
    participant TW as TRON Watcher
    participant EP as EventProcessor
    participant SE as Sweep Evaluator

    W->>CP: Start(ctx)
    CP->>CP: loadNetworks()
    CP->>TW: go runTRONWatcher()
    CP->>CP: go consumeEvents()
    CP->>CP: go runExpirationLoop()
    CP->>CP: go runConfirmationLoop()
    CP->>SE: go runSweepEvaluationLoop()
    TW-->>EP: BlockchainEvent (via channel)
    EP->>EP: processEventInTx()
    Note over EP: Match → Create → FSM → Post → Wallet
    SE->>SE: evaluateSweeps() every 1m
    Note over SE: Query balances → Resolve config → MarkSweepPending
    W->>CP: ctx.Done()
    CP->>TW: stop (ctx cancelled)
    TW->>TW: close(events)
    CP->>CP: consumerWg.Wait()
```

**Конфигурация (env vars):**

| Переменная | Описание |
|-----------|----------|
| `TRON_RPC_URL` | TronGrid API endpoint (e.g., `https://api.shasta.trongrid.io`) |
| `TRON_API_KEY` | API key для повышенных rate limits |

---

## 6. PaymentFSM — Конечный автомат платежей

FSM обеспечивает строгую валидацию переходов между статусами платежа. Каждый переход атомарен и записывается в audit trail.

### Переходы

```
Detected ──→ Confirming     (first_confirmation, confs ≥ 1)
Confirming ──→ Confirmed    (confirmed, confs ≥ RequiredConfs)
Confirming ──→ Reorged      (chain_reorg)
Confirmed ──→ Settled       (settlement_complete)
Reorged ──→ Detected        (re-detect after reorg)
```

### Audit Trail

Каждый переход создаёт запись `PaymentEvent`:

```go
type PaymentEvent struct {
    ID         id.ID
    PaymentID  id.ID
    FromStatus PaymentStatus
    ToStatus   PaymentStatus
    EventType  string              // "first_confirmation", "confirmed", "chain_reorg"
    Metadata   TransitionMetadata  // {Confirmations, RequiredConfs, BlockNumber, TxHash}
    CreatedAt  time.Time
}
```

**Если запись FSM-события не удалась — вся транзакция откатывается.** Audit trail обязателен для финансовой трассируемости.

---

## 7. Webhooks

Уведомления мерчанту о событиях инвойса. Каждый webhook подписан HMAC-SHA256.

### Типы событий

| Event | Когда |
|-------|-------|
| `invoice.paid` | Получена полная оплата (ожидает подтверждений) |
| `invoice.confirmed` | Платёж подтверждён (finalised) |
| `invoice.expired` | TTL инвойса истёк без оплаты |
| `withdrawal.confirmed` | Вывод средств подтверждён |

### Формат запроса

```
POST {callbackURL}
Content-Type: application/json
X-Metapus-Event: invoice.confirmed
X-Metapus-Signature: HMAC-SHA256(body, webhookSecret)
X-Metapus-Timestamp: 2026-05-04T10:00:00Z
X-Metapus-Delivery-ID: unique-uuid
```

### SSRF-защита

`ValidateWebhookURL()` применяется при создании мерчанта **и** при каждой отправке (defence-in-depth):
- Только HTTPS
- Блокировка приватных IP (10.x, 172.16.x, 192.168.x)
- Блокировка loopback (127.0.0.1, ::1)
- Блокировка cloud metadata (169.254.169.254)
- Блокировка `*.internal`, `localhost`
- **Блокировка всех HTTP-редиректов** (`CheckRedirect → http.ErrUseLastResponse`) — предотвращает SSRF bypass через redirect chains

---

## 8. Compliance (AML/KYT)

Интерфейс `ComplianceEngine` предоставляет скрининг адресов и транзакций.

```go
type ComplianceEngine interface {
    ScreenAddress(ctx context.Context, address string) (RiskScore, error)
    ScreenTransaction(ctx context.Context, txHash string) (RiskScore, error)
}
```

**Текущая реализация:** `NoopComplianceEngine` — всегда возвращает `low risk`. Для продакшена необходимо подключить Chainalysis, Elliptic или Crystal.

**Risk Levels:** `low` (0–25) → `medium` (26–50) → `high` (51–75) → `critical` (76–100).

---

## 9. Settlement

Механизм расчётов с мерчантами.

**v1 — Crypto-to-Crypto:** средства переводятся в том же токене. `CryptoWithdrawal` = settlement документ.

**Будущее — Crypto-to-Fiat:** интеграция с OTC desk / биржей для конвертации в фиат.

```go
type SettlementStrategy interface {
    Settle(ctx context.Context, merchantID id.ID, amount CryptoAmount, tokenID id.ID) error
}
```

---

## 10. Threshold Sweep

Механизм накопительного свипа — переводит средства с пул-кошельков на горячий кошелёк при достижении порогового баланса, экономя на комиссиях за транзакции.

### Мотивация

При каждом платеже делать sweep невыгодно — комиссия за перевод TRC-20 составляет ~14 TRX (~$2). При мелких платежах (например, $5) комиссия съедает 40% суммы. Threshold sweep позволяет накопить баланс и выполнить один sweep вместо множества мелких.

### Конфигурация

Двухуровневая система настроек:

```
 Приоритет                           Хранилище
┌────────────────────────────────┐   ┌─────────────────────────────┐
│ 1. Merchant override (nullable)│ → │ reg_merchant_token_config   │
│ 2. Token default (required)    │ → │ cat_tokens                  │
└────────────────────────────────┘   └─────────────────────────────┘
```

**SweepConfig** — результат резолвинга:

| Поле | Описание | Пример |
|------|----------|--------|
| `Threshold` | Мин. баланс для свипа (minor units) | 10000000 = 10 USDT |
| `MaxAgeHours` | Макс. время без свипа (принудительный) | 24 = 1 день |

**Особые случаи:**
- `Threshold = 0` → **Legacy mode**: sweep сразу после подтверждения платежа (backward compatible)
- `MaxAgeHours = 0` → Нет принудительного свипа по времени, только по порогу

### Поток обработки

```mermaid
flowchart TD
    A["Payment Confirmed"] --> B{"SweepConfig.Threshold = 0?"}
    B -->|Yes| C["MarkSweepPending<br/>(legacy, immediate)"]
    B -->|No| D{"Wallet AllocationMode?"}
    D -->|transient| E["Release<br/>(back to pool)"]
    D -->|persistent| F["No change<br/>(stays assigned)"]
    
    G["Sweep Evaluation Loop<br/>(every 1 min)"] --> H["Query pool wallets with<br/>confirmed payments since last sweep"]
    H --> I["Resolve SweepConfig<br/>(merchant → token)"]
    I --> J{"Balance ≥ Threshold<br/>OR Age > MaxAgeHours?"}
    J -->|Yes| K["MarkSweepPending"]
    J -->|No| L["Skip"]
```

### Sweep Evaluation Loop

Фоновый тикер в `CryptoProcessor`, запускается каждые 60 секунд:

1. **Query** — находит pool-кошельки (`free`/`assigned`) с непросвипленными confirmed-платежами
2. **Aggregate** — суммирует `amount` из `doc_crypto_payments` с `confirmed_at > last_swept_at`
3. **Resolve** — для каждого кошелька резолвит `SweepConfig` через `SweepConfigResolver`
4. **Evaluate** — проверяет: `balance ≥ threshold` ИЛИ `time.Since(lastSweptAt) > maxAgeHours`
5. **Mark** — вызывает `walletSvc.MarkSweepPending()` для кандидатов

```sql
-- Ключевой запрос evaluation loop
SELECT w.id, w.merchant_id, w.last_swept_at, p.token_id,
       COALESCE(SUM(p.amount), 0) AS balance
FROM cat_wallets w
INNER JOIN doc_crypto_payments p ON p.wallet_id = w.id
WHERE w.tier = 'pool'
  AND w.status IN ('free', 'assigned')
  AND p.status = 'confirmed'
  AND p.confirmed_at > COALESCE(w.last_swept_at, '1970-01-01'::timestamptz)
GROUP BY w.id, w.merchant_id, w.last_swept_at, p.token_id
HAVING COALESCE(SUM(p.amount), 0) > 0
```

### SweepConfigResolver

```go
// NULL-coalescing: merchant override → token default
func (r *SweepConfigResolver) Resolve(ctx, merchantID, tokenID) SweepConfig {
    // 1. Get token defaults (always required)
    tok := r.tokenRepo.GetByID(ctx, tokenID)
    cfg := SweepConfig{Threshold: tok.SweepThreshold, MaxAgeHours: tok.SweepMaxAgeHours}
    
    // 2. Try merchant override (optional)
    override := r.merchantConfigRepo.Get(ctx, merchantID, tokenID)
    if override != nil {
        if override.SweepThreshold != nil { cfg.Threshold = *override.SweepThreshold }
        if override.SweepMaxAgeHours != nil { cfg.MaxAgeHours = *override.SweepMaxAgeHours }
    }
    return cfg
}
```

---

## 11. Типы данных

### CryptoAmount

`int64` (`MinorUnits`) **не подходит** для крипто: ETH имеет 18 decimals, max `int64` = 9.2 × 10¹⁸ = ~9.2 ETH в wei.

**Решение:** обёртка над `math/big.Int`:

```go
type CryptoAmount struct {
    val *big.Int // minor units (satoshi, wei, sun, lamport)
}
```

- Сериализация в JSON: строка (`"1000000"`)
- Хранение в Postgres: `NUMERIC` (arbitrary precision)
- Defensive copy при создании и извлечении
- Arithmetics: `Add()`, `Sub()`, `Cmp()`, `IsZero()`, `IsPositive()`

---

## 12. Файловая карта

```
Backend:
  internal/core/types/crypto_amount.go               — CryptoAmount (big.Int wrapper)
  internal/core/entity/crypto_register.go             — CryptoBalanceMovement, CryptoFeeMovement
  internal/domain/crypto/
  ├── blockchain_event.go                             — BlockchainEvent, ChainWatcher interface
  ├── event_processor.go                              — EventProcessor + handleWalletAfterConfirm
  ├── payment_fsm.go                                  — PaymentFSM + PaymentEvent
  ├── sweep_config.go                                 — SweepConfig, MerchantTokenConfig
  ├── sweep_resolver.go                               — SweepConfigResolver (NULL-coalescing)
  ├── webhook.go                                      — WebhookDispatcher + SSRF protection
  ├── compliance.go                                   — ComplianceEngine + NoopComplianceEngine
  ├── settlement.go                                   — SettlementStrategy + SettlementService
  └── signer.go                                       — VaultSigner interface
  internal/domain/catalogs/
  ├── blockchain_network/model.go                     — BlockchainNetwork
  ├── token/model.go                                  — Token (+SweepThreshold, +SweepMaxAgeHours)
  ├── wallet/model.go                                 — Wallet (+AllocationMode, +CustomerRef)
  └── merchant/model.go                               — Merchant
  internal/domain/documents/
  ├── crypto_invoice/model.go                         — CryptoInvoice
  ├── crypto_payment/model.go                         — CryptoPayment (FSM-driven)
  ├── crypto_sweep/model.go                           — CryptoSweep
  └── crypto_withdrawal/model.go                      — CryptoWithdrawal
  internal/infrastructure/blockchain/tron/
  ├── client.go                                       — TronGrid HTTP client (retry, backoff)
  └── watcher.go                                      — TRON ChainWatcher (polling, checkpoint)
  internal/infrastructure/crypto_worker/
  └── processor.go                                    — CryptoProcessor + Sweep Evaluation Loop
  internal/infrastructure/storage/postgres/crypto_repo/
  ├── merchant_token_config.go                        — MerchantTokenConfig repo (Get, Upsert)
  ├── payment_event_repo.go                           — PaymentEvent persistence
  └── watcher_state_repo.go                           — Watcher checkpoint persistence
```

---

## 13. Сквозной поток: от транзакции до подтверждения

```mermaid
sequenceDiagram
    participant C as Customer
    participant BC as TRON Blockchain
    participant TW as TRON Watcher
    participant EP as EventProcessor
    participant FSM as PaymentFSM
    participant DB as PostgreSQL

    C->>BC: Transfer USDT → Pool Wallet
    TW->>BC: Poll TRC-20 events
    BC-->>TW: Transfer event (tx_hash, amount)
    TW->>TW: Filter by monitored addresses
    TW->>BC: GetConfirmations(tx_hash)
    BC-->>TW: confs = 0

    TW->>EP: BlockchainEvent (Detected, 0 confs)
    EP->>DB: FindByTxHash → not found
    EP->>DB: FindActiveInvoice(wallet)
    EP->>DB: Create CryptoPayment (Detected)
    EP->>DB: Update Invoice.ReceivedAmount
    Note over EP: confs < 1 → no FSM transition yet

    loop Каждые 3 секунды
        TW->>BC: Poll for confirmation updates
        BC-->>TW: confs = 5
        TW->>EP: BlockchainEvent (Confirmation, 5 confs)
        EP->>DB: FindByTxHash → found
        EP->>FSM: Transition(Detected→Confirming)
        FSM->>DB: Update payment.status
        FSM->>DB: Insert PaymentEvent (audit)
    end

    TW->>BC: Poll
    BC-->>TW: confs = 20 (≥ RequiredConfs)
    TW->>EP: BlockchainEvent (Confirmation, 20 confs)
    EP->>FSM: Transition(Confirming→Confirmed)
    FSM->>DB: Update payment.status + confirmedAt
    FSM->>DB: Insert PaymentEvent (audit)
    EP->>DB: Post payment (register movements)
    EP->>DB: handleWalletAfterConfirm()
    Note over EP: threshold=0 → SweepPending<br/>threshold>0 → Release
    EP->>DB: Update Invoice → Confirmed
```

---

## 14. Архитектурные решения и trade-offs

| Решение | Альтернатива | Обоснование |
|---------|-------------|-------------|
| **Монолит** (не микросервисы) | Hellgate/Fistful/Shumway отдельно | Ранний этап. Monolith-first. Микросервисы при >100k tx/day |
| **`big.Int`** для сумм | `decimal.Decimal` / `int64` | int64 переполняется при 18 decimals. Decimal медленнее для pure integer ops |
| **NUMERIC** в Postgres | BIGINT | Arbitrary precision, native support для big.Int |
| **Worker** как watcher host | Отдельный процесс (NBXplorer) | Переиспользуем tenant lifecycle, pool management, ctx.Done() |
| **Polling** (не WebSocket) | Node WebSocket subscription | TronGrid не поддерживает WS для events. Polling + adaptive interval |
| **Event log** (не event sourcing) | Full event sourcing + replay | Достаточно для audit. Full ES — при необходимости replay |
| **Vault** для ключей | In-process crypto libs | Ключи не покидают Vault. Zero-knowledge signing |
| **Fingerprint pagination** | Offset-based | TronGrid API uses fingerprint. Immutable — safe for concurrent polling |

---

## 15. Конфигурация и запуск

### Environment Variables

```bash
# Worker (cmd/worker)
TRON_RPC_URL=https://api.shasta.trongrid.io   # Shasta testnet
TRON_API_KEY=c9c9646e-0626-4035-857b-911c6aba25cc  # TronGrid API key

# Server (cmd/server)
AUTOMATION_ENCRYPTION_KEY=test-encryption-key-32chars!!!!!  # AES key для RPC endpoints
```

### Seed Data

Крипто-данные сидируются через `cmd/seed/main.go` → `seedCryptoData()` (requires `SEED_DEMO_DATA=true`):
- 1 blockchain network (TRON Shasta Testnet)
- 1 token (USDT TRC-20, sweep_threshold=10 USDT, max_age=1h)
- 1 merchant (Demo Merchant)
- 2 pool wallets (transient) + 1 hot wallet

### Проверка работоспособности

```bash
# Backend
go build ./... && golangci-lint run ./...

# Frontend
cd frontend && npx tsc --noEmit && npm run lint

# Worker logs — должны появиться:
# "crypto processor started" networks=1
# "sweep evaluation loop started"
# "starting TRON watcher" addresses=2
```
