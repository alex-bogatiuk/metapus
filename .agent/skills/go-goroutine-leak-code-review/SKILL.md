---
name: go-goroutine-leak-code-review
description: Проверяй написанный код на утечки горутин (goroutine leaks) и связанные проблемы:неправильный lifecycle (нет отмены context / нет Stop), незакрытые/неправильно используемые каналы,"вечные" ticker/таймеры, горутины без стратегии завершения, блокирующие send/recv (partial deadlocks),отсутствие лимитов параллелизма и слабая наблюдаемость (NumGoroutine/pprof/goleak).Use this when reviewing Go concurrency, WebSocket/streaming handlers, pub-sub subscribers, background workers.
---

# Go Goroutine Leak Code Review (Checklist Skill)

## Goal
Найти и предотвратить утечки горутин и ресурсоёмкие “подвисания” в Go-коде до продакшена.
Результат — перечень конкретных находок + точечные рекомендации и патчи/диффы.

## What I need from the user
- PR/дифф, ссылки на файлы или вставка кода.
- Контекст исполнения: HTTP/WS/gRPC, фоновые воркеры, шедулеры, подписки (pub/sub), shutdown-путь.
- Ожидаемая семантика: кто владелец жизненного цикла, кто закрывает каналы, когда происходит stop/unsubscribe.

## Review protocol (follow strictly)

### 1) Inventory goroutines
1. Собери список всех мест, где запускаются горутины (`go ...`), а также косвенных запусков (через библиотеки, подписки, обработчики).
2. Для каждой горутины зафиксируй:
   - owner (кто обязан остановить),
   - stop-signal (ctx.Done()/done-chan/close),
   - условия завершения (какие события гарантируют exit).

**Red flags**
- “fire-and-forget” без stop-сигнала.
- горутина в обработчике запроса/соединения без привязки к закрытию соединения.

### 2) Context lifecycle (mandatory)
1. Если создаётся `context.WithCancel/WithTimeout/WithDeadline`:
   - найди владельца `cancel()`;
   - проверь, что `cancel()` вызывается гарантированно (defer или в явном `Stop/Unsubscribe/Close`).
2. Любой цикл/селект внутри горутины обязан иметь ветку остановки:
   - `case <-ctx.Done(): return` (или эквивалент done-chan).

**Red flags**
- `cancel` сохраняется, но нигде не вызывается.
- `select` без `ctx.Done()` в долгоживущей горутине.

### 3) Channel contracts (ownership + backpressure)
1. Для каждого канала определить:
   - кто producer, кто consumer,
   - кто **единственный** владелец `close(ch)` (owner closes).
2. Проверить send/recv на возможность вечной блокировки:
   - Любой `ch <- x` в потенциально долгоживущем коде должен иметь план B:
     `select { case ch<-x: ...; case <-ctx.Done(): ... }`
   - Аналогично для recv при необходимости.

3. Очереди должны быть ограничены:
   - буферизация оправдана только при определённой политике переполнения (drop/backpressure/disconnect).

**Red flags**
- unbuffered channel в месте, где читатель может исчезнуть (клиент отвалился, reader остановился).
- send без `select`/таймаута/ctx — риск partial deadlock.
- канал не закрывается, но на него продолжают писать после “логического закрытия” потребителя.

### 4) Timers / Tickers hygiene
1. Любой `time.NewTicker(...)` → **обязателен** `defer ticker.Stop()`.
2. Любой `time.NewTimer(...)` → остановка/дренаж при досрочном выходе (если применимо).
3. Если тикер используется в горутине — убедиться, что stop-сигнал приводит к exit + stop тикера.

**Red flags**
- `NewTicker` без `Stop`.
- таймер/тикер в бесконечном цикле без выхода по ctx.Done.

### 5) I/O + error paths (especially WS/streams)
1. Любая запись в сеть/WS/стрим обязана:
   - проверять ошибки,
   - на ошибке инициировать cleanup (cancel/unsubscribe/close).
2. Где уместно — установить дедлайны/таймауты на I/O.
3. Для “Subscribe/stream per connection”:
   - должен быть симметричный `Unsubscribe/Close/Stop`,
   - cleanup должен быть привязан к закрытию соединения (close handler / монитор-пинг / read-loop exit).

**Red flags**
- ошибка write/read логируется, но lifecycle не завершается.
- “подписка” переживает соединение.

### 6) Cleanup order (must unblock + release)
В `Stop/Unsubscribe/Close` использовать порядок:
1) `cancel()` (остановить горутины),
2) `close(ch)` (разблокировать ожидания),
3) удалить ссылки из map/slice/registry (чтобы GC мог собрать).

**Red flags**
- закрытие канала не происходит никогда.
- cancel/close делаются, но объект остаётся в registry и удерживается ссылками.

### 7) Concurrency limits (DoS resistance)
1. Там, где входной поток может порождать горутины (handlers, subscriptions, fan-out), проверить:
   - есть ли лимитер (semaphore/worker pool),
   - есть ли bounded очередь,
   - что происходит при перегрузе (отказ/дроп/замедление).

**Red flags**
- каждый входной message → новая горутина без лимитов.

### 8) Observability + testing gates
1. Рекомендовать метрику `runtime.NumGoroutine()` и алерты на тренд.
2. Для тестов — `go.uber.org/goleak` как “защитный барьер” в suite.
3. Рекомендовать pprof goroutine dump для расследований (если речь о прод-симптомах).

## Output format (use exactly)
### A) Executive summary
- 3–7 bullets: основные риски и их impact (memory growth, latency, stuck writers).

### B) Findings (table)
| ID | Severity (P0/P1/P2) | Location | Symptom | Root cause | Fix sketch |
|---:|---|---|---|---|---|

Severity guide:
- P0: бесконечная горутина/тикер или гарантированный deadlock/leak при обычном сценарии
- P1: утечка на error-path/edge case, но вероятная в проде
- P2: потенциально опасный паттерн без доказанного триггера

### C) Checklist (pass/fail)
- [ ] У каждой горутины есть stop-сигнал и owner, вызывающий stop.
- [ ] Все созданные cancel() гарантированно вызываются.
- [ ] Все `NewTicker`/таймеры корректно останавливаются.
- [ ] Каналы: определён owner close; send/recv не блокируются навсегда.
- [ ] Subscribe имеет Unsubscribe/Close и cleanup привязан к disconnect.
- [ ] Ошибки I/O приводят к завершению lifecycle.
- [ ] Есть лимиты на порождение горутин/очереди bounded.
- [ ] Есть goleak-тесты/NumGoroutine мониторинг (или конкретный план добавить).

### D) Patch suggestions
Если уместно — предложи точечные изменения кода (минимальные, локальные), сохраняя исходные контракты.

## Examples

### Example 1 — missing cancel + ticker.Stop
Input (snippet):
```go
ctx, cancel := context.WithCancel(context.Background())
sub := NewSubscriber(ctx)
go func() {
  t := time.NewTicker(time.Second)
  for {
    select {
    case <-t.C:
      sub.Ping()
    }
  }
}()
// cancel is never called; ticker is never stopped