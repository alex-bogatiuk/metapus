# Модуль 1: Архитектура Worker'а (Фоновые задания)

## Что такое Worker?

У Metapus есть **два отдельных процесса**:
- `cmd/server/main.go` — принимает HTTP-запросы от пользователей (дневная смена)
- `cmd/worker/main.go` — выполняет фоновую работу (ночная смена)

Worker работает параллельно с сервером, но **не принимает HTTP-запросов**. Его задачи:
1. **Опрашивает блокчейн** (TRON Watcher) — "Пришли ли новые платежи?"
2. **Протухшие инвойсы** — "Прошло 30 минут, а никто не заплатил? → `Expired`"
3. **Обрабатывает автоматизации** (Outbox Relay) — "Отправить email / Telegram"
4. **Загружает курсы валют** (Rate Feed) — "Какой курс BTC/USD?"
5. **Уборка мусора** (Cleanup) — Удаление просроченных сессий

## Как Worker управляет тенантами?

Сервер получает `X-Tenant-ID` из HTTP-запроса. Но Worker не получает HTTP-запросов — он сам по расписанию определяет, для каких тенантов работать.

### `MultiTenantWorker.Run()` — главный цикл

```go
func (w *MultiTenantWorker) Run(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Minute)  // Каждую минуту...
    defer ticker.Stop()

    tenantContexts := make(map[string]context.CancelFunc) // "Реестр работающих тенантов"

    w.refreshTenants(ctx, ...)  // При старте: загрузить всех тенантов

    for {
        select {
        case <-ctx.Done():          // Сигнал на выключение → останавливаем всех
            for _, cancel := range tenantContexts {
                cancel()
            }
            return
        case <-ticker.C:            // Каждую минуту → обновляем список
            w.refreshTenants(ctx, ...)
        }
    }
}
```

Каждую минуту Worker спрашивает мета-базу: *"Какие тенанты сейчас активны?"* Если появился новый — запускает для него горутину. Если тенант удалился — вызывает `cancel()`.

## `runTenantWorker` — сердце фоновой обработки

Для каждого тенанта Worker запускает отдельную горутину:

```go
func (w *MultiTenantWorker) runTenantWorker(ctx context.Context, t *tenant.Tenant) {
    // 1. Получаем пул соединений к БД этого тенанта
    mp, _ := w.manager.GetPool(ctx, t.ID)
    mp.AcquireRef()          // "Не закрывай мой пул, я его использую!"
    defer mp.ReleaseRef()    // "Можешь закрывать, я закончил"

    // 2. Создаем TxManager и кладем в контекст (как в Middleware у сервера!)
    txManager := postgres.NewTxManagerFromRawPool(mp.Pool())
    ctx = tenant.WithPool(ctx, mp.Pool())
    ctx = tenant.WithTxManager(ctx, txManager)

    // 3. Запускаем подсистемы (каждая — отдельная горутина)
    go scheduler.Start(ctx)           // Автоматизации по расписанию
    go cryptoProcessor.Start(ctx)     // Крипто-процессинг
    go rateFeedWorker.Start(ctx)      // Курсы валют

    // 4. Главный цикл: обработка Outbox + очистка мусора
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C:   relay.ProcessBatch(ctx) // Outbox каждые 500ms
        case <-cleanupTicker.C:  // Cleanup каждый час
        }
    }
}
```

Строка `ctx = tenant.WithTxManager(ctx, txManager)` — тот же трюк, что делает Middleware `TenantDB` на сервере: кладёт подключение к БД в "рюкзак" (контекст).

## Паттерн `AcquireRef` / `ReleaseRef` (Reference Counting)

`TenantManager` умеет закрывать (evict) неиспользуемые пулы соединений. Но если Worker прямо сейчас использует пул тенанта, а Manager его закроет — Worker упадёт!

- `mp.AcquireRef()` — *"Я использую этот пул, не трогай!"*
- `defer mp.ReleaseRef()` — *"Можешь закрывать"*

Тот же принцип, что `shared_ptr` в C++ и ARC в Swift.

## Зачем `subsWg.Wait()` перед return?

```go
case <-ctx.Done():
    subsWg.Wait()  // Ждём завершения ВСЕХ подсистем
    return         // Только потом выходим (и defer ReleaseRef() срабатывает)
```

Без `subsWg.Wait()` функция завершится → `defer mp.ReleaseRef()` отпустит пул → Manager может закрыть соединения. Но горутины (`cryptoProcessor`, `scheduler`) **ещё живы** и пытаются обратиться к БД → **Use-After-Free** (паника или потеря данных).

```
БЕЗ Wait:                          С Wait:
ctx.Done() →                        ctx.Done() →
  return →                            subsWg.Wait() →
    ReleaseRef() →                      goroutine 1: Done() ✓
      Pool CLOSED ←── 💥               goroutine 2: Done() ✓
        goroutine: Query() → PANIC    return →
                                        ReleaseRef() →
                                          Pool CLOSED ← всё чисто ✓
```

## Ключевые файлы

- [`cmd/worker/main.go`](../cmd/worker/main.go) — точка входа Worker

## Паттерны Go

- `context.WithCancel` — правильная остановка горутин
- `sync.WaitGroup` — ожидание завершения параллельных задач
- `defer ticker.Stop()` — предотвращение утечки ресурсов
- Reference Counting (`AcquireRef`/`ReleaseRef`)
