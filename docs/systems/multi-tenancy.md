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
| **Meta Database** | Единая служебная БД. Сохраняет список всех тенантов, их статус, параметры подключения и текущую версию схемы. |
| **Tenant Database** | Изолированная БД клиента (например, `mt_0195d5a8f32a7bc8a9011234567890ab`). Содержит все справочники, документы и регистры. |

`slug` является публичным URL-идентификатором и не используется как SQL-идентификатор. Физическое `db_name` строится из UUID tenant и остается неизменным при переименовании slug.

## 2. Provisioning tenant

Создание tenant выполняется как идемпотентная saga, поскольку PostgreSQL запрещает `CREATE DATABASE` внутри обычной транзакции:

1. В Meta Database резервируется tenant со статусом `provisioning`.
2. Создается физическая БД с безопасно quoted identifier.
3. Применяются все tenant-миграции.
4. Только после успешных миграций tenant атомарно переводится в `active`.

При ошибке сохраняются `provision_failed`, стадия и текст ошибки. Повторный `tenant create` с тем же slug продолжает существующую попытку. Параллельные попытки сериализуются PostgreSQL advisory lock по canonical slug.

## 3. Идентификация тенанта

1. Клиент (браузер) прикрепляет заголовок `X-Tenant-ID` (UUID) к каждому API-запросу.
2. `TenantDB Middleware` сначала извлекает `X-Tenant-ID` и получает пул соединений для этого tenant. Для WebSocket допускается query `tenant`.
3. `Auth Middleware` проверяет JWT и сверяет `tid` с выбранным tenant. Несовпадение отклоняется до handler; затем проверяется серверное состояние сессии.

## 4. Динамические пулы соединений (Connection Pools)

Сервер не может держать открытые соединения ко всем базам сразу (их могут быть тысячи). 
Управление пулами реализовано в `tenantdb.Manager.GetPool(tenantID)`:

1. **Fast path (Кэш):** Ищет пул в `sync.Map` в памяти. Если есть — обновляет время использования и возвращает. `O(1)`; lifecycle lock согласует выдачу с shutdown.
2. **Slow path (Инициализация):** Если пула нет, делает запрос в Meta-DB. Проверяет статус тенанта (`active`). Собирает строку подключения и инициализирует новый `pgxpool.Pool`.
3. Фоновый процесс **Eviction Loop** каждые 15 минут закрывает соединения для пулов, к которым не было обращений (Idle Timeout).
4. Фоновый процесс **Health Check** пингует пулы раз в минуту.

## 5. Инъекция Transaction Manager

Бизнес-логика (слой Domain) и репозитории **не знают** о multi-tenancy.

Middleware кладёт объект `TxManager` (настроенный на базу конкретного тенанта) в `context.Context` запроса.
Domain получает абстрактный manager через `tenant.GetTxManager(ctx)`; PostgreSQL-репозитории получают конкретный querier через `postgres.MustGetTxManager(ctx)`. Код остаётся чистым, а SQL-запросы не содержат `WHERE tenant_id = ?`.

---

## Файловая карта
```path
internal/infrastructure/tenantdb/manager.go — Пулы, eviction, health checks и shutdown
internal/core/tenant/registry.go — Абстрактный Registry и provisioning-порты
internal/infrastructure/tenantdb/registry.go — PostgreSQL Registry для Meta-database
internal/infrastructure/tenantdb/context.go — Raw pool context (только инфраструктура)
internal/infrastructure/http/v1/middleware/tenant.go — Перехват X-Tenant-ID
internal/bootstrap/tenant/            — CLI для управления (cmd/tenant, cmd/crypto-tenant)
```

## Связанные документы
- [auth-security.md](auth-security.md) — как `X-Tenant-ID` валидируется через JWT токен.
