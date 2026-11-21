# Слой Infrastructure (Реализация)

> **TL;DR:** Адаптеры к внешнему миру. Здесь живут HTTP-хэндлеры (Gin), PostgreSQL репозитории (`pgx`, `squirrel`), фоновые worker-ы и сборка зависимостей (Composition Root).

> **Тип:** Concept
> **Аудитория:** Developer
> **Связанные:** [architecture.md](../guide/02-architecture.md), [request-lifecycle.md](../guide/04-request-lifecycle.md)

---

## 1. HTTP Слой (Gin Router)

### Middleware Chain
Порядок middleware жёстко зафиксирован: 
Глобально: `CORS → Recovery → Trace → Logger → ErrorHandler`. Для ERP: `TenantDB → Auth → AdminGuard → SecurityContext → authorized read service`; permission, step-up и command idempotency назначаются маршрутам. Merchant API, Portal, auth, checkout и WebSocket имеют отдельные группы. См. [жизненный цикл](../guide/04-request-lifecycle.md).

### Handlers
Хэндлеры максимально "тонкие":
1. Принимают HTTP-запрос (Bind DTO).
2. Маппят DTO в доменную сущность (mapper func).
3. Вызывают сервис из Domain.
4. Маппят ответ сервиса обратно в DTO и отдают 200/201.
- **Хэндлер НЕ знает** про JSON-ответы при ошибках, это делает глобальный `ErrorHandler`.
- Gin использует общий `internal/core/json` facade через
  `internal/infrastructure/http/jsoncodec`. Входной JSON — закрытый контракт:
  неизвестные поля, неверный регистр имени, дубликаты ключей, невалидный UTF-8
  и дополнительные top-level значения отклоняются.

## 2. PostgreSQL Слой (Storage)

### Transaction Manager (TxManager)
Объект, управляющий транзакциями для конкретного тенанта (БД). 
- Создаётся для каждого HTTP-запроса в `TenantDB` middleware.
- Кладётся в `context.Context`.
- Сервисы (Domain) вызывают `txm.RunInTransaction(ctx, ...)`.

### Generic Репозитории
Все базовые CRUD операции реализованы через `BaseCatalogRepo[T]` и `BaseDocumentRepo[T]`.
- Репозиторий использует Go-рефлексию (`db` теги) для маппинга структур в SQL.
- Запросы строятся через query builder `squirrel`.
- Драйвер: `jackc/pgx/v5` (нативный, высокопроизводительный).
- Каждый product pool конфигурируется через `postgres.ConfigurePool` или
  создаётся `postgres.OpenPool`: JSON/JSONB codecs обязаны использовать тот же
  JSON v2 facade, что HTTP и application-код.
- **Никаких ORM** (GORM и прочие запрещены для сохранения контроля над SQL и производительностью).

## 3. Фоновые задачи (Workers)

Отдельное приложение (`internal/bootstrap/worker`, точки входа `cmd/worker` и `cmd/crypto-worker`), которое обрабатывает фоновые процессы:
- **Outbox Relay**: гарантированная доставка событий из `sys_outbox` во внешние системы.
- **Cleanup loops**: истёкшие сессии, idempotency, automation history/files, notifications, worker jobs и retired webhook secrets. Универсального очистителя финансового audit ledger нет.
- Воркеры циклично обходят базы данных всех активных тенантов.

В `crypto_worker` нет прямых SQL-вызовов. Сценарии исполнения и подтверждения
собраны в `application/outboundapp`, а PostgreSQL-реализации их портов — в
`storage/postgres/outbound_repo`. Каталог сетей, reconciliation, очереди webhook
и runtime status обслуживает `crypto_repo.WorkerRuntimeRepo`; публикация состояния
approval job находится в `money_movement_repo`.

Worker сохраняет таймеры, остановку по context, создание конкретных клиентов и
телеметрию. `FeeBudgetObserver` передаёт события резерва и settlement внешнему
адаптеру; SQL-реализация не импортирует SDK телеметрии. Репозитории получают
транзакцию и tenant-подключение из context. Эта граница закреплена в
`internal/architecture/crypto_storage_test.go` и проверками реализации application-портов.

## 4. Composition Root (Сборка)

В Metapus **нет** "магических" фреймворков для внедрения зависимостей (DI).
Сборка происходит вручную в файлах `cmd/*/main.go`:
1. Чтение конфигурации из ENV.
2. Подключение к Meta-DB.
3. Инициализация TenantManager.
4. Инициализация Router и запуск HTTP-сервера.
5. Подписка на сигналы ОС (SIGTERM, SIGINT) для **Graceful Shutdown** (корректного завершения активных соединений).
