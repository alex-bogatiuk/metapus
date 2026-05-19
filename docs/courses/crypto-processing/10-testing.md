# Модуль 10: Тестирование крипто-пайплайна

## 1. Table-Driven Tests — стандарт Metapus

Конвенция: `give` (что даём), `want` (что ожидаем), переменная `tt`, один `t.Run` на сценарий.

```go
func TestEffectiveFee_Calculate(t *testing.T) {
    tests := []struct {
        give   string        // Описание сценария (человекочитаемое)
        fee    EffectiveFee  // Входные данные
        amount int64
        want   int64         // Ожидаемый результат
    }{
        {
            give:   "zero config → zero fee",
            fee:    EffectiveFee{},
            amount: 1_000_000,
            want:   0,
        },
        {
            give: "percent only: 1% of 100 USDT",
            fee:  EffectiveFee{PercentBP: 100},
            amount: 100_000_000,
            want:   1_000_000,
        },
        {
            give: "min floor applies: calculated < minFee",
            fee:  EffectiveFee{FixedFee: 1_000_000, PercentBP: 100, MinFee: 5_000_000},
            amount: 50_000_000,   // 50 USDT → 1 + 0.5 = 1.5 < 5 min
            want:   5_000_000,    // min kicks in
        },
        // ... ещё 10 сценариев
    }

    for _, tt := range tests {
        t.Run(tt.give, func(t *testing.T) {
            got := tt.fee.Calculate(types.NewCryptoAmountFromInt64(tt.amount))
            want := types.NewCryptoAmountFromInt64(tt.want)
            if got.Cmp(want) != 0 {
                t.Errorf("Calculate(%d) = %s, want %s", tt.amount, got.String(), want.String())
            }
        })
    }
}
```

При падении Go покажет **какой именно** сценарий сломался (по имени из `give`).

## 2. Mock-объекты — фейковые зависимости

`EventProcessor` зависит от 6+ компонентов. Вместо настоящей БД — **in-memory mock'и**:

```go
// Фейковая БД платежей — просто map в памяти
type memPaymentRepo struct {
    payments map[id.ID]*crypto_payment.CryptoPayment
    byTxHash map[string]*crypto_payment.CryptoPayment
}

func (r *memPaymentRepo) Create(_ context.Context, doc *crypto_payment.CryptoPayment) error {
    r.payments[doc.ID] = doc
    r.byTxHash[doc.TxHash] = doc
    return nil
}

func (r *memPaymentRepo) FindByTxHash(_ context.Context, txHash string) (*crypto_payment.CryptoPayment, error) {
    p, ok := r.byTxHash[txHash]
    if !ok {
        return nil, fmt.Errorf("payment for tx %s not found", txHash)
    }
    return p, nil
}
```

```go
// Фейковый TxManager — просто вызывает функцию без реальной транзакции
type noopTxManager struct{}

func (n *noopTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
    return fn(ctx)  // Без BEGIN/COMMIT
}
```

Тесты запускаются за **миллисекунды** без Docker, PostgreSQL или сети.

## 3. Test Fixture — подготовка окружения

```go
func setupTestFixture(t *testing.T, sweepThreshold int64) *testFixture {
    t.Helper()  // При падении Go покажет строку ВЫЗОВА, а не строку внутри fixture

    // Создаём: кошелёк (Leased) + инвойс (Created) + пустые repos
    // Собираем: FSM + WalletService + SweepResolver + EventProcessor
    // Возвращаем: всё в одной структуре

    return &testFixture{
        processor:   processor,
        walletRepo:  walletRepo,
        invoiceRepo: invoiceRepo,
        paymentRepo: paymentRepo,
        eventRepo:   eventRepo,
        // ... seeded entity IDs
    }
}
```

## 4. E2E Test: Полный цикл платежа

`TestPaymentCycle_FullFlow` проходит весь путь:

```
Step 1: Transfer (0 confs)  → Payment: Detected,   Invoice: Paid
Step 2: Confirmation (1/19) → Payment: Confirming
Step 3: Confirmation (10/19) → Payment: Confirming  (без изменений)
Step 4: Confirmation (19/19) → Payment: Confirmed + Posted
                             → Invoice: Confirmed
                             → Wallet: Free (released)
                             → FSM Audit: 2 события
```

Каждый шаг проверяет состояние **всех** связанных сущностей:

```go
func TestPaymentCycle_FullFlow(t *testing.T) {
    ctx := context.Background()
    ctx = tenant.WithTxManager(ctx, &noopTxManager{})
    f := setupTestFixture(t, 10_000_000)

    // ── Step 1: Transfer detected (0 confirmations) ──
    event := f.makeTransferEvent(5_000_000) // 5 USDT
    if err := f.processor.ProcessEvent(ctx, event); err != nil {
        t.Fatalf("Step 1: %v", err)
    }
    payment, _ := f.paymentRepo.FindByTxHash(ctx, event.TxHash)
    if payment.Status != crypto_payment.PaymentStatusDetected {
        t.Errorf("status = %q, want detected", payment.Status)
    }

    // ── Step 4: Final confirmation (19/19) ──
    confEvent := f.makeConfirmationEvent(txHash, 19)
    f.processor.ProcessEvent(ctx, confEvent)

    payment, _ = f.paymentRepo.FindByTxHash(ctx, txHash)
    if payment.Status != crypto_payment.PaymentStatusConfirmed { t.Error("...") }
    if !payment.Posted { t.Error("should be Posted") }

    // Verify: wallet released
    w, _ := f.walletRepo.GetByID(ctx, f.walletID)
    if w.Status != wallet.WalletStatusFree { t.Error("...") }

    // Verify: FSM audit trail
    events, _ := f.eventRepo.GetByPaymentID(ctx, payment.ID)
    if len(events) != 2 { t.Fatalf("expected 2 FSM events, got %d", len(events)) }
}
```

## 5. Граничные случаи (Edge Cases)

| Тест | Что проверяет |
|------|---------------|
| `FullFlow` | Полный цикл: Transfer → Confirmed → Wallet Released |
| `ZeroThreshold_ImmediateSweep` | threshold=0 → wallet SweepPending (не Free) |
| `DustRejection` | 999 sun < 1000 порог → платёж НЕ создаётся |
| `Idempotency` | Одна транзакция дважды → один платёж, confs обновились |
| `ExpiredInvoice` | Инвойс протух → платёж НЕ создаётся, инвойс = Expired |
| `UnknownWallet` | Чужой адрес → молча пропускаем |

## Ключевые приёмы

### `t.Helper()` — правильные ошибки

```go
func setupTestFixture(t *testing.T, ...) *testFixture {
    t.Helper()  // Без этого: ошибка покажет строку ВНУТРИ fixture
                // С этим: ошибка покажет строку ВЫЗОВА fixture
}
```

### `t.Fatalf` vs `t.Errorf`

- `t.Fatalf` — тест **останавливается** (данные невалидны, продолжать бессмысленно)
- `t.Errorf` — тест **продолжается** (собираем все ошибки разом)

### Compile-time interface checks

```go
var _ posting.Postable = (*CryptoPayment)(nil)
```

Не скомпилируется, если интерфейс не реализован. Ошибка за 0 секунд, а не в production.

### Helper-методы для создания тестовых событий

```go
func (f *testFixture) makeTransferEvent(amount int64) BlockchainEvent {
    return BlockchainEvent{
        EventType:     EventTypeTransfer,
        NetworkID:     f.networkID,
        TxHash:        "0xtesthash_" + id.New().String()[:8],
        ToAddress:     f.walletAddr,
        Amount:        types.NewCryptoAmountFromInt64(amount),
        Confirmations: 0,
        RequiredConfs: 19,
    }
}
```

## Ключевые файлы

- [`internal/domain/crypto/payment_cycle_test.go`](../../internal/domain/crypto/payment_cycle_test.go) — E2E тест полного цикла
- [`internal/domain/crypto/fee_config_test.go`](../../internal/domain/crypto/fee_config_test.go) — Table-driven тесты
- [`internal/domain/crypto/webhook_test.go`](../../internal/domain/crypto/webhook_test.go) — Тесты SSRF-защиты

## Паттерны

- **Table-Driven Tests** — `[]struct{give, want}` + `t.Run`
- **In-memory mocks** — `map`-based fakes вместо реальной БД
- **Test Fixture** — `setupTestFixture()` + `t.Helper()`
- **E2E сценарии** — пошаговая верификация всех связанных сущностей
- **Compile-time checks** — `var _ Interface = (*Struct)(nil)`
