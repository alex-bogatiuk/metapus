# Database-per-Tenant

> **TL;DR:** Metapus физически изолирует данные клиентов. Каждый Tenant работает в своей собственной PostgreSQL базе данных. Общие таблицы (`tenant_id`) для бизнес-данных не используются.

> **Тип:** Concept
> **Аудитория:** Developer, Admin
> **Связанные:** [auth-security.md](auth-security.md)

---

## 1. Архитектура Meta-DB и Tenant-DB

В системе есть два уровня баз данных:

| Тип БД | Описание |
|--------|----------|
| **Meta Database** | Единая служебная БД. Сохраняет список всех тенантов, их статус, DSN подключения и миграции. |
| **Tenant Database** | Изолированная БД клиента (например, `mt_acme`). Содержит все справочники, документы и регистры. |

## 2. Идентификация тенанта

1. Клиент (браузер) прикрепляет заголовок `X-Tenant-ID` (UUID) к каждому API-запросу.
2. `Auth Middleware` читает JWT токен и сверяет `tid` claim (Tenant ID) с заголовком. Если не совпадают — 403 Forbidden. Это предотвращает доступ с чужим токеном к другой БД.
3. `TenantDB Middleware` извлекает `X-Tenant-ID` и получает пул соединений для этого тенанта.

## 3. Динамические пулы соединений (Connection Pools)

Сервер не может держать открытые соединения ко всем базам сразу (их могут быть тысячи). 
Управление пулами реализовано в `Manager.GetPool(tenantID)`:

1. **Fast path (Кэш):** Ищет пул в `sync.Map` в памяти. Если есть — обновляет время использования и возвращает. `O(1)`, lock-free.
2. **Slow path (Инициализация):** Если пула нет, делает запрос в Meta-DB. Проверяет статус тенанта (`active`). Собирает строку подключения и инициализирует новый `pgxpool.Pool`.
3. Фоновый процесс **Eviction Loop** каждые 15 минут закрывает соединения для пулов, к которым не было обращений (Idle Timeout).
4. Фоновый процесс **Health Check** пингует пулы раз в минуту.

## 4. Инъекция Transaction Manager

Бизнес-логика (слой Domain) и репозитории **не знают** о multi-tenancy.

Middleware кладёт объект `TxManager` (настроенный на базу конкретного тенанта) в `context.Context` запроса.
Все репозитории вызывают `tenant.MustGetTxManager(ctx)` для выполнения SQL-запросов. Код остаётся чистым, а SQL-запросы не содержат `WHERE tenant_id = ?`.

---

## Файловая карта
```path
internal/core/tenant/manager.go       — Управление пулами и Eviction Loop
internal/core/tenant/registry.go      — Общение с Meta-database
internal/infrastructure/http/v1/middleware/tenant.go — Перехват X-Tenant-ID
cmd/tenant/main.go                    — CLI для управления (миграции)
```

## Связанные документы
- [auth-security.md](auth-security.md) — как `X-Tenant-ID` валидируется через JWT токен.
