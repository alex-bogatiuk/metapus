# Модуль 2: Go-конкурентность на практике

## Три столпа конкурентности Go

В Go конкурентность — базовый инструмент. Разбираем на реальном коде `CryptoProcessor.Start()`.

## 1. Горутины (Goroutines) — лёгкие потоки

Горутина — функция, которая выполняется **параллельно** с остальным кодом. В отличие от потоков ОС (~1 МБ), горутина занимает ~2 КБ. Можно запустить тысячи без проблем.

```go
// Обычный вызов — код ЖДЁТ, пока функция закончит
scheduler.Start(ctx)

// Горутина — код ПРОДОЛЖАЕТ РАБОТУ, а Start() работает параллельно
go scheduler.Start(ctx)
```

`CryptoProcessor.Start()` запускает **5 горутин** для каждого тенанта:

| # | Горутина | Назначение |
|---|----------|------------|
| 1 | TRON Watcher | Опрос блокчейна (каждые 3 сек) |
| 2 | Event Consumer | Обработка событий из канала |
| 3 | Expiration Loop | Проверка просроченных инвойсов (каждые 60 сек) |
| 4 | Confirmation Loop | Перепроверка подтверждений (каждые 10 сек) |
| 5 | Sweep Evaluation | Проверка порогов для свипа (каждые 60 сек) |

## 2. Каналы (Channels) — безопасные трубы

Горутины не должны общаться через общую память (race conditions). Вместо этого Go использует **каналы** — безопасные трубы для передачи данных.

Аналогия: пневматическая почта в старых банках.

```go
// Создаем "трубу" с буфером на 100 сообщений
events := make(chan crypto.BlockchainEvent, 100)
```

**Производитель (TRON Watcher)** кладёт события:
```go
events <- BlockchainEvent{TxHash: "abc123", Amount: ...}
```

**Потребитель (Event Consumer)** достаёт и обрабатывает:
```go
func (p *CryptoProcessor) consumeEvents(ctx context.Context, events <-chan crypto.BlockchainEvent) {
    for {
        select {
        case <-ctx.Done():
            return
        case event, ok := <-events:
            if !ok {
                return  // Канал закрыт — все Watcher'ы завершились
            }
            p.eventProcessor.ProcessEvent(ctx, event)
        }
    }
}
```

**Буфер 100:** Если Watcher нашёл сразу 50 транзакций, он быстро кладёт их все в канал, не дожидаясь Consumer. Если буфер заполнится — Watcher **заблокируется** (backpressure).

## 3. Select — мультиплексор каналов

`select` — как `switch`, но для каналов. Ждёт, пока один из каналов будет готов:

```go
for {
    select {
    case <-ctx.Done():
        return           // Ctrl+C → уходим

    case <-ticker.C:
        p.pollConfirmations(ctx)  // Прошло 10 сек → проверяем
    }
}
```

Аналогия: сидишь в комнате с двумя телефонами. Левый (ctx.Done) звонит — уходи домой. Правый (ticker) — сделай работу.

## 4. Graceful Shutdown — координация остановки

Самая красивая часть `CryptoProcessor.Start()`:

```go
var watcherWg sync.WaitGroup   // Группа ожидания для Watcher'ов
var consumerWg sync.WaitGroup  // Группа ожидания для Consumer'ов

// Запускаем Watcher
watcherWg.Add(1)
go func() { defer watcherWg.Done(); p.runTRONWatcher(ctx, ..., events) }()

// Закрываем канал ПОСЛЕ того, как все Watcher'ы завершились
go func() {
    watcherWg.Wait()  // Ждём Watcher
    close(events)     // Закрываем трубу → Consumer получит "ok = false"
}()

// Запускаем Consumer
consumerWg.Add(1)
go func() { defer consumerWg.Done(); p.consumeEvents(ctx, events) }()

consumerWg.Wait()  // Ждём Consumer
```

Цепочка остановки работает как домино:
```
Ctrl+C → ctx.Done()
  → Watcher заканчивает текущий poll, выходит
    → watcherWg.Wait() разблокируется
      → close(events) — канал закрыт
        → Consumer видит "ok = false", выходит
          → consumerWg.Wait() разблокируется
            → Start() завершается
```

## Что будет, если не проверить `ok`?

```go
// ❌ ПЛОХО
case event := <-events:
    p.eventProcessor.ProcessEvent(ctx, event)
```

При закрытом канале Go возвращает **zero value** типа. Для `BlockchainEvent` это пустая структура (`TxHash = ""`, `Amount = 0`). Без проверки `ok`:
1. **Бесконечный цикл** — `select` мгновенно получает zero value снова и снова
2. **100% CPU** — тысячи "пустых" событий в секунду
3. **Горутина никогда не завершится** → `consumerWg.Wait()` зависнет навечно

## Ключевые файлы

- [`internal/infrastructure/crypto_worker/processor.go`](../../internal/infrastructure/crypto_worker/processor.go) — `CryptoProcessor.Start()`

## Паттерны Go

- Паттерн "Producer → Channel → Consumer"
- Буферизованные каналы для backpressure
- `select` для мультиплексирования каналов
- `sync.WaitGroup` + `close(channel)` для Graceful Shutdown
- Проверка `ok` при чтении из канала
