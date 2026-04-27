# Слой Infrastructure (Реализация)

> **TL;DR:** Адаптеры к внешнему миру. Здесь живут HTTP-хэндлеры (Gin), PostgreSQL репозитории (`pgx`, `squirrel`), фоновые worker-ы и сборка зависимостей (Composition Root).

> **Тип:** Concept
> **Аудитория:** Developer
> **Связанные:** [architecture.md](../guide/02-architecture.md), [request-lifecycle.md](../guide/04-request-lifecycle.md)

---

## 1. HTTP Слой (Gin Router)

### Middleware Chain
Порядок middleware жёстко зафиксирован: 
`Recovery` → `Trace` → `Logger` → `ErrorHandler` → `TenantDB` → `Auth` → `UserContext` → `Idempotency` → `Permission`.

### Handlers
Хэндлеры максимально "тонкие":
1. Принимают HTTP-запрос (Bind DTO).
2. Маппят DTO в доменную сущность (mapper func).
3. Вызывают сервис из Domain.
4. Маппят ответ сервиса обратно в DTO и отдают 200/201.
- **Хэндлер НЕ знает** про JSON-ответы при ошибках, это делает глобальный `ErrorHandler`.

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
- **Никаких ORM** (GORM и прочие запрещены для сохранения контроля над SQL и производительностью).

## 3. Фоновые задачи (Workers)

Отдельное приложение (`cmd/worker/main.go`), которое обрабатывает фоновые процессы:
- **Outbox Relay**: гарантированная доставка событий из `sys_outbox` во внешние системы.
- **Audit Cleaner**: очистка старых логов.
- Воркеры циклично обходят базы данных всех активных тенантов.

## 4. Composition Root (Сборка)

В Metapus **нет** "магических" фреймворков для внедрения зависимостей (DI).
Сборка происходит вручную в файлах `cmd/*/main.go`:
1. Чтение конфигурации из ENV.
2. Подключение к Meta-DB.
3. Инициализация TenantManager.
4. Инициализация Router и запуск HTTP-сервера.
5. Подписка на сигналы ОС (SIGTERM, SIGINT) для **Graceful Shutdown** (корректного завершения активных соединений).
