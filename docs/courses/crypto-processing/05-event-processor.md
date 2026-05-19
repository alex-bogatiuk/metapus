# Модуль 5: EventProcessor — Мозг Криптопроцессинга

## Главный принцип: Всё в одной транзакции

```go
func (p *EventProcessor) ProcessEvent(ctx context.Context, event BlockchainEvent) error {
    return p.txManager.RunInTransaction(ctx, func(ctx context.Context) error {
        return p.processEventInTx(ctx, event)
    })
}
```

Все шаги выполняются **атомарно**. Если любой шаг упадёт — всё откатится.

## 7 шагов обработки события

```
Шаг 1: GUARD        → Сумма > 0? Не пыль (dust)?
Шаг 2: IDEMPOTENCY  → Мы уже видели эту транзакцию?
Шаг 3: MATCH WALLET → Чей это кошелёк?
Шаг 4: FIND INVOICE → Какой инвойс ждёт оплату?
Шаг 5: CREATE       → Создаём документ CryptoPayment
Шаг 6: UPDATE       → Обновляем сумму в инвойсе
Шаг 7: CONFIRM      → FSM-переходы + Проведение + Управление кошельком
```

### Шаг 1: Guard (Защита от мусора)

```go
// Отклоняем нулевые и отрицательные суммы
if event.EventType != EventTypeReorg && !event.Amount.IsPositive() {
    return nil
}

// Отклоняем "пыль" — микро-транзакции < 0.001 USDT
if event.Amount.Cmp(p.dustThreshold) < 0 {
    return nil  // Атакующий может спамить тысячами мусорных транзакций
}
```

### Шаг 2: Idempotency (Защита от дублей)

```go
existing, err := p.paymentRepo.FindByTxHash(ctx, event.TxHash)
if err == nil && existing != nil {
    return p.handleConfirmationUpdate(ctx, existing, event) // Только обновляем confs
}
```

Одна транзакция может прийти несколько раз (watcher poll, confirmation loop). Без этой проверки — дубликаты.

### Шаги 3-4: Match Wallet → Find Invoice

```go
w, _ := p.walletSvc.FindByAddress(ctx, event.NetworkID, event.ToAddress)
invoice, _ := p.findActiveInvoice(ctx, w) // wallet.LeasedForID → invoice
```

Цепочка: `Адрес в блокчейне → Наш Кошелёк → Инвойс`.

Если кошелёк **persistent** и нет активного инвойса — создаём **Top-Up инвойс на лету**.

### Шаг 5: Create Payment + Fee Snapshot

```go
payment := crypto_payment.NewCryptoPayment(invoice.ID, ..., event.TxHash, event.Amount, ...)

// "Фотографируем" текущий тариф комиссии в документ
fee, _ := p.feeResolver.Resolve(ctx, invoice.MerchantID, invoice.TokenID, FeeDirectionProcessing)
payment.SetFeeConfig(fee.FixedFee, fee.PercentBP, fee.MinFee, fee.MaxFee)
```

**Fee Snapshot:** если завтра тариф изменится с 1% на 2%, старые платежи считаются по старому тарифу.

### Шаг 6: Update Invoice Amount

```go
invoice.ReceivedAmount = invoice.ReceivedAmount.Add(event.Amount)

switch {
case excess.IsPositive():  → InvoiceStatusOverpaid      (переплата)
case excess.IsZero():      → InvoiceStatusPaid           (точно)
default:                   → InvoiceStatusPartiallyPaid   (недоплата)
}
```

### Шаг 7: processConfirmations

**Последовательные if** (не switch!) — потому что Watcher может увидеть транзакцию сразу с 20+ подтверждениями:

```go
// 7a: Detected → Confirming (первое подтверждение)
if payment.Status == Detected && event.Confirmations >= 1 {
    p.fsm.Transition(ctx, payment, Confirming, "first_confirmation", ...)
}

// 7b: Confirming → Confirmed (набрали нужное число подтверждений)
if payment.Status == Confirming && event.Confirmations >= payment.RequiredConfs {
    p.postingEngine.Post(ctx, payment, func(ctx) error {
        return p.fsm.Transition(ctx, payment, Confirmed, "confirmed", ...)
    })

    invoiceConfirmed, _ := p.confirmInvoice(ctx, payment.InvoiceID)
    if invoiceConfirmed {
        p.handleWalletAfterConfirm(ctx, payment)
    }
}
```

### Compose Writes: FSM-переход внутри Post()

FSM-переход передаётся как callback в `Post()`, а не вызывается отдельно:

```go
// ✅ Один UPDATE: Posted=true, PostedVersion=1, Status=Confirmed
p.postingEngine.Post(ctx, payment, func(ctx) error {
    return p.fsm.Transition(ctx, payment, Confirmed, ...)
})

// ❌ Два UPDATE → Optimistic Locking конфликт!
p.postingEngine.Post(ctx, payment, ...)
p.fsm.Transition(ctx, payment, Confirmed, ...)
```

## Clean Architecture

`EventProcessor` лежит в `internal/domain/crypto/`. Он **не знает** про TRON, PostgreSQL или HTTP. Работает с интерфейсами: `InvoiceAccessor`, `TokenResolver`, `tx.Manager`.

## Ключевые файлы

- [`internal/domain/crypto/event_processor.go`](../../internal/domain/crypto/event_processor.go) — `ProcessEvent()`
- [`internal/domain/crypto/event_processor_test.go`](../../internal/domain/crypto/event_processor_test.go)

## Паттерны

- **Idempotency** через `FindByTxHash`
- **Fee Snapshot** — заморозка тарифа в документе
- **Compose Writes** — объединение записей в один UPDATE
- **Dust Guard** — защита от спам-атак
- **Sequential if** вместо switch для multi-step FSM transitions
