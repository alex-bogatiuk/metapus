# План тестирования: Threshold Sweep

## Обзор

Тестирование охватывает 4 уровня: unit-тесты (чистая логика), integration-тесты (БД), E2E-сценарии (полный поток), edge cases.

> [!IMPORTANT]
> В проекте пока нет тестов для crypto-пакетов. Это первые тесты — они зададут паттерн для всех последующих.

---

## 1. Unit-тесты (без БД)

### 1.1. `crypto/sweep_config_test.go` — SweepConfig

Table-driven тесты value-object'а.

| # | Test Case | give | want |
|---|-----------|------|------|
| 1 | `IsZeroThreshold` — zero amount | `Threshold: CryptoAmount(0)` | `true` |
| 2 | `IsZeroThreshold` — positive | `Threshold: CryptoAmount(1000)` | `false` |
| 3 | `IsZeroThreshold` — nil (zero-value) | `SweepConfig{}` | `true` |

---

### 1.2. `crypto/sweep_resolver_test.go` — SweepConfigResolver

Mock-зависимости: `MerchantTokenConfigRepository` + `token.Repository`.

| # | Test Case | give | want |
|---|-----------|------|------|
| 1 | Token defaults only (no override) | Token: thresh=10M, age=24h; Override: nil | `{10M, 24}` |
| 2 | Full merchant override | Token: thresh=10M, age=24h; Override: thresh=5M, age=1h | `{5M, 1}` |
| 3 | Partial override (threshold only) | Token: thresh=10M, age=24h; Override: thresh=20M, age=nil | `{20M, 24}` |
| 4 | Partial override (maxAge only) | Token: thresh=10M, age=24h; Override: thresh=nil, age=48h | `{10M, 48}` |
| 5 | Nil merchantID → skip override | `merchantID = id.Nil()` | Token defaults |
| 6 | Override repo error → fallback to token | Override repo returns error | Token defaults |
| 7 | Token repo error → propagate | Token repo returns error | Error returned |

**Mock-интерфейсы:**

```go
// internal/domain/crypto/sweep_resolver_test.go

type mockMerchantConfigRepo struct {
    cfg *MerchantTokenConfig
    err error
}

func (m *mockMerchantConfigRepo) Get(ctx context.Context, merchantID, tokenID id.ID) (*MerchantTokenConfig, error) {
    return m.cfg, m.err
}

func (m *mockMerchantConfigRepo) Upsert(ctx context.Context, cfg *MerchantTokenConfig) error { return nil }

type mockTokenRepo struct {
    tok *token.Token
    err error
}

func (m *mockTokenRepo) GetByID(ctx context.Context, id id.ID) (*token.Token, error) {
    return m.tok, m.err
}
// ... остальные методы token.Repository → panic("not implemented")
```

---

### 1.3. `catalogs/wallet/model_test.go` — Wallet Model

| # | Test Case | give | want |
|---|-----------|------|------|
| 1 | `IsTransient` — transient | `AllocationMode: transient` | `true` |
| 2 | `IsTransient` — persistent | `AllocationMode: persistent` | `false` |
| 3 | `IsPersistent` — persistent | `AllocationMode: persistent` | `true` |
| 4 | `Release` clears lease | leased wallet | `Status=free, LeasedForID=nil` |
| 5 | `MarkSweepPending` | leased wallet | `Status=sweep_pending, LeasedForID=nil` |
| 6 | Validate — persistent without CustomerRef | `AllocationMode: persistent, CustomerRef: ""` | Validation error |
| 7 | Validate — persistent with CustomerRef | `AllocationMode: persistent, CustomerRef: "CUST-001"` | OK |
| 8 | Validate — transient without CustomerRef | `AllocationMode: transient, CustomerRef: ""` | OK |
| 9 | Validate — invalid AllocationMode | `AllocationMode: "invalid"` | Validation error |

---

### 1.4. `catalogs/token/model_test.go` — Token Sweep Validation

| # | Test Case | give | want |
|---|-----------|------|------|
| 1 | Negative sweep threshold | `SweepThreshold: CryptoAmount(-1)` | Validation error |
| 2 | Zero sweep threshold (legacy) | `SweepThreshold: CryptoAmount(0)` | OK |
| 3 | Positive sweep threshold | `SweepThreshold: CryptoAmount(10M)` | OK |
| 4 | Negative max age | `SweepMaxAgeHours: -1` | Validation error |
| 5 | Zero max age (disabled) | `SweepMaxAgeHours: 0` | OK |

---

### 1.5. `crypto/event_processor_test.go` — handleWalletAfterConfirm

> [!NOTE]
> Требует mock `walletSvc`, `sweepResolver`. EventProcessor инициализируется с mock-зависимостями.

| # | Test Case | give | want |
|---|-----------|------|------|
| 1 | Nil resolver → legacy sweep | `sweepResolver = nil` | `MarkSweepPending()` вызван |
| 2 | Resolver error → legacy fallback | resolver returns error | `MarkSweepPending()` вызван |
| 3 | Zero threshold → immediate sweep | cfg: `{Threshold: 0}` | `MarkSweepPending()` вызван |
| 4 | Positive threshold + transient → release | cfg: `{Threshold: 10M}`, wallet: transient | `Release()` + `Update()` вызваны |
| 5 | Positive threshold + persistent → no-op | cfg: `{Threshold: 10M}`, wallet: persistent | Ни `MarkSweepPending`, ни `Release` |

---

## 2. Integration-тесты (с БД)

> [!IMPORTANT]
> Требуют testcontainers или тестовую БД PostgreSQL. Используем паттерн `TestMain` + `testcontainers-go`.

### 2.1. `crypto_repo/merchant_token_config_test.go`

| # | Test Case | Описание |
|---|-----------|----------|
| 1 | `Get` — record exists | Insert → Get → verify fields |
| 2 | `Get` — record not found | Get non-existent pair → nil, nil |
| 3 | `Upsert` — insert | Upsert new record → verify in DB |
| 4 | `Upsert` — update | Insert → Upsert with new values → verify updated |
| 5 | `Upsert` — partial NULL | Upsert with threshold=nil, maxAge=48 → verify NULL preserved |
| 6 | Unique constraint | Two records for same merchant+token → last wins (upsert) |

---

### 2.2. `catalog_repo/wallet_test.go` — LeaseForInvoice

| # | Test Case | Описание |
|---|-----------|----------|
| 1 | Lease transient wallet | 2 free transient wallets → lease → one becomes leased |
| 2 | Skip persistent wallets | 1 persistent assigned + 1 transient free → lease → transient leased |
| 3 | No free wallets | 0 free wallets → error `NOT_FOUND` |
| 4 | Skip frozen wallets | 1 frozen + 1 free → lease → free one leased |
| 5 | Concurrent lease (FOR UPDATE SKIP LOCKED) | 2 goroutines lease simultaneously → each gets different wallet |

---

### 2.3. `crypto_worker/processor_integration_test.go` — evaluateSweeps

| # | Test Case | Описание |
|---|-----------|----------|
| 1 | Balance below threshold | Wallet with 5M balance, threshold=10M → NOT marked |
| 2 | Balance equals threshold | Wallet with 10M balance, threshold=10M → marked sweep_pending |
| 3 | Balance above threshold | Wallet with 15M balance, threshold=10M → marked sweep_pending |
| 4 | Max age exceeded | Wallet with 1M balance, last_swept_at=25h ago, maxAge=24h → marked |
| 5 | Never swept + max age | Wallet: last_swept_at=NULL, maxAge=24h → marked (force sweep) |
| 6 | Zero threshold → skipped | Wallet with threshold=0 → skipped (handled by EventProcessor) |
| 7 | Multiple tokens per wallet | Wallet with USDT + ETH payments → independent evaluation per token |
| 8 | Already sweep_pending | Wallet already sweep_pending → NOT re-processed (query filters it) |

---

## 3. E2E-сценарии (API → Worker → DB)

> [!NOTE]
> Проверяют полный поток: создание инвойса → имитация платежа → проверка состояния кошелька.

### Сценарий 1: Legacy mode (threshold = 0)

```
Setup:  Token.SweepThreshold = 0, Wallet = transient
Steps:
  1. POST /document/crypto-invoice → invoice created, wallet leased
  2. Simulate confirmed payment event → EventProcessor processes
  3. Assert: wallet.status = sweep_pending (legacy behavior)
  4. Assert: invoice.status = confirmed
```

### Сценарий 2: Threshold mode — balance below threshold

```
Setup:  Token.SweepThreshold = 10_000_000 (10 USDT), Wallet = transient
Steps:
  1. POST /document/crypto-invoice (expectedAmount = 5_000_000) → wallet leased
  2. Simulate confirmed payment (5 USDT)
  3. Assert: wallet.status = free (released back to pool)
  4. Wait for sweep evaluation loop tick (60s)
  5. Assert: wallet.status = free (still free, balance < threshold)
```

### Сценарий 3: Threshold mode — balance reaches threshold

```
Setup:  Token.SweepThreshold = 10_000_000, Wallet = transient
Steps:
  1. Invoice 1: 5 USDT → wallet leased → confirmed → released
  2. Invoice 2: 6 USDT → same wallet leased → confirmed → released
  3. Wait for sweep evaluation loop
  4. Assert: wallet.status = sweep_pending (balance 11M ≥ threshold 10M)
```

### Сценарий 4: Merchant override

```
Setup:  Token.SweepThreshold = 10_000_000
        MerchantTokenConfig: threshold = 3_000_000 (lower override)
Steps:
  1. Invoice: 4 USDT → confirmed → released
  2. Sweep evaluation
  3. Assert: wallet.status = sweep_pending (4M ≥ merchant threshold 3M)
```

### Сценарий 5: Persistent wallet (future)

```
Setup:  Wallet.AllocationMode = persistent, CustomerRef = "CUST-001"
Steps:
  1. Payment confirmed on persistent wallet
  2. Assert: wallet.status = assigned (NOT released, NOT sweep_pending)
  3. Sweep evaluation runs, balance ≥ threshold
  4. Assert: wallet.status = sweep_pending
```

---

## 4. Edge Cases

| # | Case | Expected |
|---|------|----------|
| 1 | Sweep eval with zero candidates | Loop runs silently, no errors |
| 2 | SweepConfigResolver — token not found | Error propagated, wallet skipped |
| 3 | MarkSweepPending fails | Error logged, other wallets still evaluated |
| 4 | Concurrent sweep eval + EventProcessor | No race: EventProcessor releases in tx, eval queries later |
| 5 | Worker restart mid-evaluation | Idempotent: re-query finds same candidates |
| 6 | Multiple confirmed payments same wallet | SUM(amount) correctly aggregates |
| 7 | Payment confirmed_at = NULL | Excluded by `p.confirmed_at > COALESCE(...)` filter |

---

## 5. Приоритизация

| Приоритет | Тесты | Обоснование |
|-----------|-------|-------------|
| 🔴 P0 | 1.2 (Resolver), 1.5 (handleWallet) | Ядро бизнес-логики, ошибки = потеря средств |
| 🟠 P1 | 1.1 (SweepConfig), 1.3 (Wallet model), 1.4 (Token validation) | Domain invariants |
| 🟡 P2 | 2.1 (MerchantConfigRepo), 2.2 (LeaseForInvoice) | Data integrity |
| 🟢 P3 | 2.3 (evaluateSweeps), E2E сценарии | Integration correctness |

---

## 6. Файловая карта тестов

```
internal/domain/crypto/
├── sweep_config_test.go              — SweepConfig value object tests (§1.1)
├── sweep_resolver_test.go            — SweepConfigResolver NULL-coalescing (§1.2)
└── event_processor_test.go           — handleWalletAfterConfirm branching (§1.5)

internal/domain/catalogs/wallet/
└── model_test.go                     — Wallet allocation/status/validation (§1.3)

internal/domain/catalogs/token/
└── model_test.go                     — Token sweep field validation (§1.4)

internal/infrastructure/storage/postgres/crypto_repo/
└── merchant_token_config_test.go     — Get/Upsert persistence (§2.1)

internal/infrastructure/storage/postgres/catalog_repo/
└── wallet_test.go                    — LeaseForInvoice allocation filter (§2.2)

internal/infrastructure/crypto_worker/
└── processor_test.go                 — evaluateSweeps integration (§2.3)
```

## Open Questions

> [!IMPORTANT]
> **Testcontainers:** использовать `testcontainers-go` с PostgreSQL для integration-тестов (§2), или запускать на уже поднятом Docker контейнере из `docker-compose`?

> [!NOTE]
> **Мок-стратегия:** для unit-тестов (§1) нужны mock-реализации `token.Repository`, `wallet.Service`, `MerchantTokenConfigRepository`. Создавать руками или использовать `go.uber.org/mock` (mockgen)?
