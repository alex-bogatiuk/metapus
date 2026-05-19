# Модуль 4: ChainWatcher — Наблюдатель блокчейна

## Зачем нужен Watcher?

Клиент перевёл USDT на адрес кошелька. Блокчейн не пришлёт push-уведомление — нам нужно **самим спрашивать**: *"Были ли новые транзакции?"*

## Интерфейс `ChainWatcher` (Clean Architecture)

Domain-слой **не знает** ничего про TRON:

```go
// internal/domain/crypto/blockchain_event.go
type ChainWatcher interface {
    NetworkCode() string
    Start(ctx context.Context, addresses []string, events chan<- BlockchainEvent) error
    GetConfirmations(ctx context.Context, txHash string) (int, error)
}
```

Конкретная реализация — в `internal/infrastructure/blockchain/tron/watcher.go`. Завтра добавим Ethereum — напишем новый файл, не трогая domain.

## Polling (Опрос) — основной цикл

```go
func (w *Watcher) Start(ctx context.Context, addresses []string, events chan<- BlockchainEvent) error {
    // 1. Загружаем checkpoint: "Где мы остановились в прошлый раз?"
    state, _ := w.stateRepo.Get(ctx, w.cfg.NetworkID)

    pollTimer := time.NewTimer(pollInterval)
    defer pollTimer.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()  // Graceful shutdown

        case <-pollTimer.C:
            eventsFound, err := w.poll(ctx, state, events)
            // ... adaptive polling, save checkpoint ...
            pollTimer.Reset(pollInterval)
        }
    }
}
```

## Метод `poll()` — одна итерация

```
1. Вычислить окно: от last_timestamp до last_timestamp + 1 час (но не дальше now)
2. HTTP-запрос к TronGrid: "Дай все TRC-20 Transfer события в этом окне"
3. Для каждого события:
   a. Адрес получателя (toAddr) есть в наших кошельках? Если нет → пропускаем
   b. Блок уже обработан? (idempotency) Если да → пропускаем
   c. Запрашиваем количество подтверждений (GetConfirmations)
   d. Отправляем BlockchainEvent в канал → events <- blockchainEvent
4. Сохраняем checkpoint (last_block, last_timestamp)
```

## Adaptive Polling — адаптивный интервал

Watcher подстраивает скорость опроса:

```go
if state.LastTimestamp < catchUpThreshold {
    // CATCH-UP MODE: Отстали > 1 минуты → Турбо! (500ms)
    pollInterval = 500 * time.Millisecond
} else if eventsFound > 0 {
    // Есть события → Базовый интервал (3 сек)
    pollInterval = w.cfg.PollInterval
} else {
    // Ничего нет → Замедляемся (×1.2) до потолка 30 сек
    pollInterval = time.Duration(float64(pollInterval) * 1.2)
    if pollInterval > _maxPollInterval {
        pollInterval = _maxPollInterval
    }
}
```

| Ситуация | Интервал | Зачем |
|----------|----------|-------|
| Worker только запустился, отстал на 2 часа | 500ms | Быстро нагнать |
| Есть новые транзакции | 3 сек | Блокчейн активен |
| Ничего не происходит | 3s → 3.6s → 4.3s → ... → 30s | Экономия API-лимитов |

## Checkpoint — защита от потери данных

После каждого успешного poll:
```go
state.UpdatedAt = time.Now().UTC()
w.stateRepo.Save(ctx, state)  // last_block, last_timestamp → reg_crypto_watcher_state
```

При перезапуске Worker продолжит **с того же места**, не пропустив транзакций.

## Отправка события в канал (с защитой)

```go
select {
case events <- blockchainEvent:   // Попытка положить в канал
    eventsFound++
case <-ctx.Done():                // Если пришёл сигнал остановки
    return eventsFound, ctx.Err() // Не зависаем!
}
```

Без `select` простая запись `events <- event` заблокируется навечно, если канал полон и пришёл Ctrl+C.

## Exponential Backoff — обработка ошибок

```go
func (w *Watcher) backoff(current time.Duration, errorCount int) time.Duration {
    next := current * 2                // Удваиваем интервал
    if next > _maxPollInterval {
        next = _maxPollInterval         // Не больше 30 сек
    }
    return next
}
```

1-я ошибка → 6 сек. 2-я → 12 сек. 3-я → 24 сек. 4-я → 30 сек (потолок). Успешный запрос → сброс на 3 сек.

## Ключевые файлы

- [`internal/domain/crypto/blockchain_event.go`](../../internal/domain/crypto/blockchain_event.go) — интерфейс `ChainWatcher`
- [`internal/infrastructure/blockchain/tron/watcher.go`](../../internal/infrastructure/blockchain/tron/watcher.go) — TRON-реализация
- [`internal/infrastructure/blockchain/tron/client.go`](../../internal/infrastructure/blockchain/tron/client.go) — HTTP-клиент к TronGrid

## Паттерны Go

- `time.NewTimer` + `defer timer.Stop()` (вместо `time.After` — не утекает)
- HTTP-клиент с retry и Exponential Backoff
- `select` для неблокирующей записи в канал
- Checkpoint/Resume для надёжного опроса
