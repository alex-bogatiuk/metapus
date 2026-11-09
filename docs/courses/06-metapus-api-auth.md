# Модуль 6. API и авторизация

> Учебный маршрут по действующим исходникам. Полный контракт: [Auth/Security](../systems/auth-security.md), [HTTP lifecycle](../guide/04-request-lifecycle.md).

## 1. Путь запроса

Откройте `internal/infrastructure/http/v1/router.go`. Глобальная цепочка: CORS → Recovery → Trace → Logger → ErrorHandler. ERP protected group добавляет TenantDB → Auth → AdminGuard → SecurityContext и authorized read service. Permission, step-up и command idempotency назначаются конкретным маршрутам.

Для отладки используйте существующий `POST /api/v1/catalog/counterparties`, а не отсутствующий `/catalogs/items`. Handler выполняет транспортную проверку DTO, domain/application — бизнес-сценарий, PostgreSQL adapter — SQL.

Merchant Public API (`/merchant/v1`) использует API key; Portal (`/portal/v1`) — JWT и merchant scope. Checkout разрешает tenant по opaque public ref. Эти контуры не проходят одну универсальную auth-цепочку.

## 2. TenantDB

`internal/infrastructure/http/v1/middleware/tenant.go` выбирает tenant по `X-Tenant-ID`, получает `tenantdb.Manager.GetPool`, удерживает reference на время запроса и помещает pool, tenant и TxManager в context. TenantDB выполняется **до** Auth; Auth сверяет выбранный tenant с токеном.

В domain доступен абстрактный `tenant.GetTxManager(ctx)`. PostgreSQL-репозиторий получает конкретный querier через `postgres.MustGetTxManager(ctx)`. Бизнес-SQL не разделяет клиентов условием `WHERE tenant_id=…`.

## 3. JWT и состояние сессии

JWT подписан HS256; payload кодирован, **не зашифрован**. Точные claims смотрите в `internal/domain/auth/jwt.go`: tenant, роли/permissions, `sid`, `uv`, `pv`, а для Portal — merchant IDs и роли по каждому merchant.

Подписи и срока действия недостаточно: middleware проверяет `auth_sessions`, версии пользователя и tenant policy через auth-state cache. Изменение прав делает старый token stale; logout отзывает сессии. Access token живёт 15 минут, refresh — 7 дней по стандартной конфигурации; refresh rotation хранит SHA-256 hash и обнаруживает повтор отозванного token.

Для admin/security_admin применяются CIDR, MFA и sensitive step-up. Object-bound approval включает digest денежного intent. Одно подтверждение не разрешает произвольные выплаты.

Для WebSocket клиент получает одноразовый ticket через `POST /api/v1/auth/ws-ticket`, затем подключается с `ticket` и `tenant` query. Access JWT не передаётся в URL.

## 4. RBAC, RLS, FLS и CEL

- RBAC допускает endpoint по permission, например `catalog:counterparty:read`.
- RLS ограничивает строки и point reads по разрешённым измерениям.
- FLS ограничивает читаемые/изменяемые поля; связь не раскрывается только потому, что доступен исходный документ.
- CEL добавляет пользовательские условия операций.

`SecurityContext` передаёт политики текущего пользователя. Frontend скрывает недоступные команды, но backend повторно проверяет права при каждом запросе.

## 5. DTO, валидация и ошибки

`BindJSON` использует strict facade: unknown/duplicate fields, неверный регистр и дополнительные JSON values отклоняются. Generated contracts синхронизируют Go DTO, TypeScript и Zod; API consumer валидирует payload до запроса и response до публикации в store.

`Counterparty.Validate(ctx)` в `internal/domain/catalogs/counterparty/model.go` проверяет данные в памяти. Уникальность ИНН проверяет service hook в транзакции. Прежнего общего `ValidateDocumentLines` в `internal/domain/validation.go` нет; конкретная модель определяет собственные инварианты.

Handler передаёт ошибку в `c.Error(err)` и вызывает `c.Abort()`. ErrorHandler возвращает `{code, message, details}` с нужным HTTP status; это собственный формат, не RFC 7807. Внутренние SQL/cause не возвращаются клиенту.

## Практика

1. Поставьте breakpoint в `TenantDB`, затем `Auth` и `CatalogHandler.Create`.
2. Проследите один запрос и доступ к tenant TxManager.
3. Сравните валидный запрос с неверным tenant, запрещённым permission и неизвестным DTO-полем.
4. Посмотрите `CatalogService.Create`, `TransactionCreate` audit и `tx.OnCommit` в [CRUD pipeline](../systems/crud-pipeline.md).

[Модуль 7: движок проведения](07-metapus-posting-engine.md)
