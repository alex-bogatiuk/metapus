# Metapus Database Migrations

> Все SQL-миграции для tenant-баз данных. Управляются [goose](https://github.com/pressly/goose).

---

## Конвенция нумерации

| Диапазон | Назначение | Пример |
|----------|------------|--------|
| `00001–09999` | **Core** — миграции ядра Metapus | `00001_extensions_and_functions.sql`, `00034_tron_wallet_pre_activation.sql` |
| `10000+` | **Extensions** — миграции расширений клиента | `10001_cat_custom_catalog.sql` |

### Почему это важно

- Goose использует **единую таблицу** `goose_db_version` для всех миграций
- Core и extension миграции применяются отдельными `goose.Provider`, но в **общую таблицу**
- Непересекающиеся диапазоны гарантируют отсутствие конфликтов версий

---

## Правила написания миграций

### 1. Формат файла

```sql
-- +goose Up
CREATE TABLE cat_new_entity (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    -- ... columns ...
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ
);

-- +goose Down
DROP TABLE cat_new_entity;
```

Для сериализации DDL используется transaction-scoped lock (присутствует в миграциях с этим guard; не все исторические исходные файлы содержат его):

```sql
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
```

Ручной `pg_advisory_unlock` не нужен: PostgreSQL освобождает lock при commit или
rollback, поэтому ошибка внутри migration не оставляет соединение заблокированным.

### 2. CDC и тип таблицы

Mutable catalog/document/configuration tables содержат `version`, `_txid`,
`deletion_mark`, `_deleted_at`, `created_at`, `updated_at` и соответствующие
triggers. Application update выполняется с optimistic lock по `version`.

Append-only event, movement и delivery audit tables не получают искусственный
soft delete: их контракт — immutable rows и reversal/compensating records.
Перестраиваемые projections/balances могут не иметь CDC, если source register
однозначно восстанавливает их.

Служебное состояние (auth sessions/refresh tokens, MFA enrollment, signing
authorizations, nonce reservations, worker leases) использует собственные
условные переходы, версии полномочий, сроки действия и retention. Полный CRUD/CDC
набор к нему автоматически не добавляется. В криптографических артефактах нельзя
подменять отзыв разрешения или завершение settlement через soft delete.

В tenant DB `tenant_id` сохранён ровно в двух защитных контрактах:
`reg_crypto_signing_authorizations` связывает неизменяемое разрешение подписи
с конкретным tenant, а `auth_step_up_confirmations` связывает подтверждение
с tenant, пользователем, сессией и действием. Эти поля предотвращают перенос
авторизации между базами и не используются как замена Database-per-Tenant.
Fresh-schema integration test проверяет обе обязательные колонки и неизменяемость
signing identity. Meta DB имеет собственные tenant-ссылки по своему назначению.

### 3. Именование таблиц

| Тип | Префикс | Пример |
|-----|---------|--------|
| Справочник | `cat_` | `cat_contractors`, `cat_items` |
| Документ | `doc_` | `doc_crypto_invoices` |
| Регистр | `reg_` | `reg_crypto_balance_movements` |
| Системная | `sys_` | `sys_sequences` |
| Auth/RBAC/RLS/FLS/CEL/MFA | `auth_` | `auth_users`, `auth_security_profiles`, `auth_user_mfa_totp` |

Preferences хранятся в `sys_user_preferences`. Все новые UUID defaults используют
`gen_random_uuid_v7()` (helper установлен отдельно в tenant и meta DB и работает
на PostgreSQL 16/17). Go `id.New()` также генерирует только UUIDv7: отказ источника
случайности прекращает выделение ID, без fallback на UUIDv4. Фиксированные seed IDs
не регенерируются. До production изменения вносятся в исходные миграции и
проверяются на отдельных свежих БД; пользовательские базы автоматически не пересоздаются.

### 4. `NO TRANSACTION` для индексов CONCURRENTLY

```sql
-- +goose Up
-- +goose NO TRANSACTION
CREATE INDEX CONCURRENTLY idx_cat_items_code ON cat_items (code);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY idx_cat_items_code;
```

> ⚠️ `CREATE INDEX CONCURRENTLY` нельзя выполнять внутри транзакции. Goose поддерживает `-- +goose NO TRANSACTION` для таких миграций.

### 5. Baseline до и после production

- Пока production-инсталляций нет, финальная схема переносится в migration,
  владеющую таблицей; переходные `ALTER/UPDATE/backfill` удаляются.
- После первого production release checksums опубликованных migrations
  неизменяемы; любые изменения выполняются новой forward-only migration.
- Тестовый стенд всегда создаётся с нуля и обязан пройти полный `Up → Down → Up`.

### 6. Строгий DDL

`Up` использует строгий DDL без `IF NOT EXISTS`: частично применённая или
загрязнённая schema должна завершить migration ошибкой, а не маскировать drift.
`ON CONFLICT` допустим только для явно идемпотентного catalog seed с проверенным
business key. Extensions могут использовать `IF NOT EXISTS`, поскольку ими
управляет уровень PostgreSQL, а не tenant schema.

Core migrations не создают пользователей и не содержат credentials. Первичный
администратор создаётся явным запуском `cmd/seed` с `ADMIN_PASSWORD` или
`ADMIN_PASSWORD_FILE`; пустой secret считается ошибкой.

---

## Как применить миграции

### Из CLI (dev/ops)

```bash
# Все тенанты
go run cmd/tenant/main.go migrate --all

# Конкретный тенант
go run cmd/tenant/main.go migrate --id <tenant-uuid>
```

Миграции запускаются оператором после остановки server и worker. Команда обрабатывает активные и приостановленные tenant-БД и возвращает ненулевой код, если хотя бы одна операция завершилась ошибкой.

### Внутренний runner

```go
import "metapus/internal/infrastructure/storage/postgres/migration"

output, err := migration.RunAll(dsn)
```

Runner используется tenant CLI и provisioning-контуром. HTTP-endpoint и UI-действия для запуска миграций намеренно отсутствуют.

---

## Связанные документы

- [naming-conventions.md](../../docs/reference/naming-conventions.md) — правила именования
- [development-rules.md](../../docs/reference/development-rules.md) — правила разработки
- [operational-modes.md](../../docs/systems/operational-modes.md) — процедура развёртывания и обновления
