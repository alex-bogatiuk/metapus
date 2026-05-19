# Модуль 7: Posting Engine — Движения в регистрах

## Один платёж — три движения

Когда платёж на 100 USDT подтверждается, `PostingEngine.Post()` генерирует 3 движения:

```
CryptoPayment (100 USDT, fee=2%)
  │
  ├─→ reg_crypto_balance:          +100 USDT на кошелёк TXabc1  (приход)
  ├─→ reg_crypto_fee:              +2 USDT  комиссия платформы   (доход платформы)
  └─→ reg_crypto_merchant_balance: +98 USDT задолженность мерчанту (сколько мы ему должны)
```

## Как документ генерирует движения

`CryptoPayment` реализует **три** Source-интерфейса:

### 1. Баланс кошелька

```go
func (p *CryptoPayment) GenerateCryptoBalanceMovements(ctx context.Context) ([]entity.CryptoBalanceMovement, error) {
    movement := entity.NewCryptoBalanceMovement(
        p.ID,                      // document_id
        p.GetDocumentType(),       // "CryptoPayment"
        p.PostedVersion + 1,       // version
        p.Date,                    // дата
        entity.RecordTypeReceipt,  // ПРИХОД
        p.WalletID,                // на какой кошелёк
        p.TokenID,                 // какой токен
        p.Amount,                  // 100 USDT (полная сумма)
    )
    return []entity.CryptoBalanceMovement{movement}, nil
}
```

### 2. Комиссия платформы

```go
func (p *CryptoPayment) GenerateCryptoFeeMovements(ctx context.Context) ([]entity.CryptoFeeMovement, error) {
    feeAmount := p.FeeAmount()  // clamp(fixed + amount×percent/10000, min, max)
    if feeAmount.IsZero() { return nil, nil }
    // ... создаём движение на сумму feeAmount
}
```

### 3. Баланс мерчанта

```go
func (p *CryptoPayment) GenerateCryptoMerchantBalanceMovements(ctx context.Context) ([]entity.CryptoMerchantBalanceMovement, error) {
    netAmount := p.NetAmount()  // 100 - 2 = 98 USDT
    // ... создаём движение на сумму netAmount
}
```

## Формула комиссии

```go
func (p *CryptoPayment) FeeAmount() types.CryptoAmount {
    // Процентная часть: amount × percentBP / 10000  (200 б.п. = 2%)
    percentPart := p.Amount.MulDiv(int64(p.FeePercentBP), 10000)

    // Итого = фикс + процент
    total := p.FeeFixed.Add(percentPart)

    // Зажимаем в коридор [min, max]
    if p.FeeMin.IsPositive() && total.Cmp(p.FeeMin) < 0 { total = p.FeeMin }
    if p.FeeMax.IsPositive() && total.Cmp(p.FeeMax) > 0 { total = p.FeeMax }

    return total
}
```

**Пример:** 100 USDT, тариф `{fixed: 0.5, percent: 200 bp, min: 1, max: 50}`
- percentPart = 100 × 200 / 10000 = **2 USDT**
- total = 0.5 + 2 = **2.5 USDT**
- clamp(2.5, 1, 50) = **2.5 USDT** ✓

## Цепочка Visitor → Recorder

```go
// Visitors — СОБИРАЮТ движения из документа в MovementSet (в памяти)
postingEngine.AddVisitor(&posting.CryptoBalanceVisitor{})
postingEngine.AddVisitor(&posting.CryptoFeeVisitor{})
postingEngine.AddVisitor(&posting.CryptoMerchantBalanceVisitor{})

// Recorders — ЗАПИСЫВАЮТ движения из MovementSet в БД
postingEngine.AddRecorder(posting.NewCryptoBalanceRecorder(cryptoBalSvc))
postingEngine.AddRecorder(posting.NewCryptoFeeRecorder(cryptoFeeSvc))
postingEngine.AddRecorder(posting.NewCryptoMerchantBalanceRecorder(cryptoMerchantSvc))
```

## Compile-time проверки

```go
var _ posting.Postable = (*CryptoPayment)(nil)
var _ posting.CryptoBalanceMovementSource = (*CryptoPayment)(nil)
var _ posting.CryptoFeeMovementSource = (*CryptoPayment)(nil)
var _ posting.CryptoMerchantBalanceMovementSource = (*CryptoPayment)(nil)
```

Если удалить `GenerateCryptoFeeMovements` — проект **не скомпилируется**.

## Ключевые файлы

- [`internal/domain/posting/engine.go`](../../internal/domain/posting/engine.go) — `Post()`
- [`internal/domain/posting/crypto_visitor.go`](../../internal/domain/posting/crypto_visitor.go) — Visitors + Recorders
- [`internal/domain/documents/crypto_payment/model.go`](../../internal/domain/documents/crypto_payment/model.go) — `GenerateMovements()`

## Паттерны

- **Visitor** — сбор движений из документа
- **Recorder** — запись движений в регистр
- **Fee Formula** — clamp(fixed + amount × percent/10000, min, max)
- **Compile-time interface checks**
