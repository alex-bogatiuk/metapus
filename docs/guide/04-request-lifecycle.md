# 04. Жизненный цикл HTTP-запроса

> **TL;DR:** Router выбирает контур авторизации, handler проверяет DTO, application/domain выполняют сценарий, а repository использует tenant-транзакцию из context.
> **Тип:** Concept
> **Аудитория:** Developer
> **Связанные:** [Архитектура](02-architecture.md), [CRUD](../systems/crud-pipeline.md)

## Middleware

Глобальная цепочка в `internal/infrastructure/http/v1/router.go`:
`CORS → Recovery → Trace → Logger → ErrorHandler`.

У защищённого ERP API `/api/v1` далее выполняются `TenantDB → Auth → AdminGuard → SecurityContext`, затем инъекция authorized read service. Permission и sensitive-action guards назначаются маршрутам. Auth и checkout имеют собственные группы; Merchant Public API использует API key, Portal — JWT и merchant scope. WebSocket получает одноразовый ticket.

`TenantDB` выбирает пул по `X-Tenant-ID` и помещает `TxManager` в context. `Auth` проверяет JWT, tenant binding и серверное состояние сессии. `Trace` устанавливает `X-Request-ID`.

## Пример: `POST /api/v1/catalog/counterparties`

1. `CatalogHandler` выполняет строгий `BindJSON`: неизвестные поля, неверный регистр, повторные ключи и дополнительные JSON-значения отклоняются. DTO преобразуется в `counterparty.Counterparty`.
2. `CatalogService.Create` нормализует и проверяет вход до БД. В транзакции (или savepoint) проверяются RLS/CEL, повторная валидация, `BeforeCreate`, иерархия; затем вызывается `repo.Create`.
3. `BaseCatalogRepo` преобразует `db`-теги в map через reflection, строит INSERT через squirrel и выполняет его через `GetQuerier(ctx)`.
4. Обязательные `TransactionCreate`-хуки, в том числе аудит, выполняются до коммита. Их ошибка откатывает запись. `AfterCreate` ставится в `tx.OnCommit` и выполняется после внешнего коммита; его ошибка логируется.
5. Handler строит разрешённое представление ответа. Для catalog create/update запись и presentation входят в одну транзакционную границу: ошибка проекции не оставляет сохранённую запись с ошибочным HTTP-ответом.

## Ошибки

Handler передаёт ошибку в `c.Error(err)` и вызывает `c.Abort()`. `ErrorHandler` извлекает `apperror.AppError` и возвращает `code`, `message`, `details` с соответствующим HTTP status. Неизвестная ошибка возвращает 500 с общим сообщением; причина остаётся в серверном логе. Это собственный формат, не RFC 7807.

## Идемпотентность команд

`CommandIdempotency` назначается конкретным маршрутам, а не всему API. Catalog create/update допускают необязательный ключ; защищённые денежные команды могут требовать его. Точный охват задают `route_helpers.go` и регистрации специализированных маршрутов.

При `Idempotency-Key` middleware строит fingerprint из пути, нормализованного query и canonical JSON body. `application/command.Executor` сохраняет команду и результат в `sys_idempotency` в одной границе с бизнес-изменением. Повтор с другим fingerprint или незавершённая команда возвращают 409. При воспроизведении устанавливается `Idempotent-Replayed: true`; catalog replay заново проверяет доступ и строит разрешённое представление. Стабильные бизнес-ошибки могут сохраняться как результат; ошибки авторизации и временные серверные ошибки не кешируются таким способом.

Handler не вызывает `CompleteIdempotency`: ответ буферизуется и фиксируется middleware/executor.

## Файловая карта

```path
internal/infrastructure/http/v1/router.go
internal/infrastructure/http/v1/route_helpers.go
internal/infrastructure/http/v1/middleware/idempotency.go
internal/application/command/executor.go
internal/infrastructure/storage/postgres/command_repository.go
internal/domain/service.go
internal/domain/catalog_eventlog.go
```
