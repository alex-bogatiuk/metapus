# Модуль 6: FSM — Конечный автомат платежей

## Зачем FSM в финансовой системе?

Без FSM программист может случайно написать `payment.Status = "confirmed"`, когда платёж ещё в `Detected`. С FSM это **физически невозможно**.

## Матрица переходов (compile-time)

```go
var _allowedTransitions = map[PaymentStatus][]PaymentStatus{
    Detected:   {Confirming},              // Detected → только Confirming
    Confirming: {Confirmed, Reorged},      // Confirming → Confirmed ИЛИ Reorged
    Confirmed:  {Settled},                 // Confirmed → только Settled
    Reorged:    {Detected},                // Reorged → обратно в Detected
}
```

Визуально:
```
Detected ──→ Confirming ──→ Confirmed ──→ Settled
                  │
                  └──→ Reorged ──→ Detected (повторный цикл)
```

## Метод `Transition` — 4 атомарных шага

```go
func (fsm *PaymentFSM) Transition(ctx, payment, newStatus, eventType, metadata) error {
    // 1. ПРОВЕРКА: разрешён ли переход?
    if !fsm.isAllowed(payment.Status, newStatus) {
        return apperror.NewValidation("transition Detected → Settled is not allowed")
    }

    oldStatus := payment.Status

    // 2. ПРИМЕНЕНИЕ: меняем статус в памяти
    payment.Status = newStatus
    if newStatus == Confirmed {
        now := time.Now().UTC()
        payment.ConfirmedAt = &now  // Фиксируем момент подтверждения
    }

    // 3. СОХРАНЕНИЕ: UPDATE в базу данных
    fsm.paymentRepo.Update(ctx, payment)

    // 4. АУДИТ: записываем событие перехода
    event := &PaymentEvent{
        PaymentID:  payment.ID,
        FromStatus: oldStatus,       // "Detected"
        ToStatus:   newStatus,        // "Confirming"
        EventType:  eventType,        // "first_confirmation"
        Metadata:   metadata,         // {Confirmations: 5, BlockNumber: 123456}
    }
    fsm.eventRepo.Create(ctx, event)  // INSERT в reg_crypto_payment_events
}
```

## Audit Trail — журнал переходов

Каждый переход записывается в `reg_crypto_payment_events`. Обязательно для финансовой системы:

| Время | Из | В | Событие | Подтверждений |
|-------|-----|---|---------|---------------|
| 10:00:01 | Detected | Confirming | first_confirmation | 5 |
| 10:00:31 | Confirming | Confirmed | confirmed | 20 |
| 10:05:00 | Confirmed | Settled | settlement_complete | 20 |

**Если запись события не удалась — вся транзакция откатывается.** Платёж без audit trail — юридически несуществующий.

## Типобезопасные метаданные (§2.4)

```go
type TransitionMetadata struct {
    Confirmations int   `json:"confirmations,omitempty"`
    RequiredConfs int   `json:"requiredConfs,omitempty"`
    BlockNumber   int64 `json:"blockNumber,omitempty"`
    TxHash        string `json:"txHash,omitempty"`
}
```

Вместо `map[string]interface{}` — строгая структура. `metatada.Confrmations` не скомпилируется.

## Ключевые файлы

- [`internal/domain/crypto/payment_fsm.go`](../../internal/domain/crypto/payment_fsm.go) — `PaymentFSM`
- [`internal/domain/documents/crypto_payment/model.go`](../../internal/domain/documents/crypto_payment/model.go) — `PaymentStatus` enum

## Паттерны Go

- `iota + 1` для enum (нулевое значение = "не задано")
- Таблица переходов как `map[Status][]Status`
- Audit Trail через FSM Events
