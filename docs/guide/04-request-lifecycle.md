# 04. Жизненный цикл запроса

> **TL;DR:** Запрос проходит строгую цепочку: глобальные Middleware (Trace, Auth, TenantDB, Idempotency) → тонкий Handler → оркеструющий Service (транзакция + хуки) → Repository (Postgres). 

> **Тип:** Concept
> **Аудитория:** Developer
> **Связанные:** [03-project-structure.md](03-project-structure.md)

---

## 1. Middleware Chain (Конвейер)

Каждый запрос к защищённому API (`/api/v1/`) проходит фиксированную цепочку Middleware.

| Middleware | Действие |
|------------|----------|
| **Recovery** | Ловит `panic` и отдаёт 500 ошибку. |
| **Trace** | Добавляет `X-Request-ID` в контекст и заголовки ответа. |
| **Logger** | Засекает время старта, по завершении логирует метод, путь, статус и latency. |
| **ErrorHandler** | Преобразует `c.Errors` в стандартизированный JSON RFC 7807 (см. §3). |
| **TenantDB** | Читает `X-Tenant-ID`, достаёт нужный пул БД, кладёт `TxManager` в контекст. |
| **Auth** | Проверяет JWT Bearer token, кладёт `UserContext` в контекст запроса. |
| **Idempotency** | Проверяет `X-Idempotency-Key` для мутирующих запросов (POST/PUT/PATCH). |
| **Permission**| Проверяет наличие прав у пользователя на выполнение конкретного маршрута. |

## 2. Путь на примере `POST /catalog/counterparties`

1. **HTTP Handler (`internal/infrastructure/http/v1/handlers/`)**
   - Биндинг JSON в DTO (`BindJSON`).
   - Маппинг DTO → `*entity.Counterparty`.
   - Вызов `service.Create(ctx, entity)`.
   - Ответ клиенту (201 Created).

2. **Domain Service (`internal/domain/catalogs/counterparty/`)**
   - Вызов `entity.Validate(ctx)` (без обращений к БД).
   - Вызов хуков `RunBeforeCreate` (например, генерация кода или проверка уникальности ИНН).
   - Извлечение `TxManager` из `ctx` и старт транзакции.
   - Внутри транзакции: `repo.Create(ctx, entity)`.
   - Вызов хуков `RunAfterCreate`.

3. **Repository (`internal/infrastructure/storage/postgres/`)**
   - Рефлексия Go-структуры в Map по `db` тегам.
   - Сборка SQL через `squirrel` (`INSERT INTO cat_counterparties`).
   - Получение `pgx.Tx` (если в транзакции) или `pgxpool.Pool` из `ctx`.
   - Выполнение запроса (`Exec`).

## 3. Обработка ошибок (Error Flow)

> [!IMPORTANT]
> Handler никогда не формирует JSON с ошибкой напрямую (`c.JSON(400, ...)` запрещено). 

Все ошибки всплывают наверх и обрабатываются централизованно.

1. Любой слой возвращает типизированную `apperror.AppError` (например, `apperror.NewValidation()`).
2. В хэндлере: `c.Error(err)` и `c.Abort()`.
3. Срабатывает глобальная `ErrorHandler` middleware (она срабатывает после того, как `c.Next()` завершается с ошибкой).
4. `ErrorHandler` маппит `AppError` в JSON `{"code": "...", "message": "..."}` с нужным HTTP статусом.

## 4. Идемпотентность

Защита от дублей при сетевых сбоях.

1. Если в запросе (POST/PUT/PATCH) есть заголовок `X-Idempotency-Key`, Idempotency Middleware считает хеш от тела запроса.
2. Проверяет ключ в базе `sys_idempotency`.
3. Если ключ найден и статус `Success` — Middleware возвращает сохранённый ответ **без вызова бизнес-логики**.
4. В хэндлере после успешного выполнения вызывается `CompleteIdempotency`, чтобы сохранить ответ для будущих повторов.

---

## Связанные документы
- [03-project-structure.md](03-project-structure.md) — где физически лежат описанные файлы (handlers, repos, middleware).
