# Database-per-Tenant — текущая модель изоляции

В Metapus поддержка нескольких клиентов реализована по схеме **Database-per-Tenant**:
каждый tenant работает в **своей отдельной PostgreSQL базе**.

## Идентификация tenant

- **HTTP заголовок**: `X-Tenant-ID`
- **Формат**: UUID (значение из meta-database `tenants.id`)

## Meta-database (реестр tenant)

- **Схема/миграция**: `db/meta/00001_tenants.sql`
- **Назначение**: хранит метаданные для подключения к tenant-БД (db_name/host/port/status/…)

## Request flow (HTTP)

1. Клиент отправляет запрос с `X-Tenant-ID: <tenant-uuid>`.
2. Middleware `TenantDB`:
   - валидирует заголовок
   - получает tenant metadata из meta-database
   - получает/создаёт pool для tenant-БД (через `internal/core/tenant.Manager`)
   - создаёт request-scoped `TxManager`
   - кладёт `Pool`, `TxManager`, `Tenant` в `context.Context`
3. Дальше handlers/services/repos берут соединение/транзакцию из `context.Context`.

## Важные следствия

- **Нет “shared DB” режима**: данные разных клиентов не смешиваются в одной tenant-БД.
- **Запросы не содержат фильтрации по tenant**: изоляция обеспечивается выбором базы, а не `WHERE ...`.
- **JWT содержит tenant ID** и middleware проверяет совпадение tenant из токена с tenant из `X-Tenant-ID`.

## CLI

Управление tenant (создание, миграции, статус) — через `cmd/tenant`.

