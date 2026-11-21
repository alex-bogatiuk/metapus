# Metapus Database Migrations

> SQL-миграции tenant-баз данных: платформенные — в этом каталоге, миграции конфигураций — в `db/configuration/<имя>/` (криптопроцессинг: [`db/configuration/crypto/`](../configuration/crypto/)), миграции модулей расширений — в [`db/extensions/<модуль>/`](../extensions/README.md). Управляются [goose](https://github.com/pressly/goose).

---

## Три истории версий

Tenant-база получает миграции из трёх источников. Все они встроены в бинарники (`dbassets`), и у каждого своя история goose и своя нумерация.

| Источник | Каталог | История goose | Нумерация |
|----------|---------|---------------|-----------|
| Платформа | `db/migrations/` | `goose_db_version_platform` | `00001…` |
| Конфигурация | `db/configuration/<имя>/` | `goose_db_version_<имя>` | своя, ниже `10000` |
| Модули расширений | `db/extensions/<модуль>/` | `goose_db_version_extensions` | `10000+`, общая для всех модулей |

`migration.Run` применяет их по порядку: платформа, конфигурация (`migration.WithConfiguration`), расширения. Затем он синхронизирует права и коды справочников. Процессы получают все опции из `configuration.MigrationOptions(cfg, extensions...)`.

- **Тенант принадлежит одной конфигурации навсегда.** Её имя записано в meta-базе (`tenants.configuration`), а в tenant-базе есть таблица её истории. Команда тенантов другой конфигурации отказывает (`tenant.ErrConfigurationMismatch`), и `migration.Run` тоже отказывает, если в базе есть история чужой конфигурации. Конфигурация без миграций, например `none`, всё равно создаёт пустую историю: так база помечена как её.
- **Версия схемы — это две цифры.** `tenants.schema_version` хранит версию платформы, она должна совпадать с `version.ExpectedSchemaVersion`. `tenants.configuration_schema_version` хранит версию конфигурации, её ожидаемое значение — старшая миграция конфигурации (`configuration.ExpectedSchema`).
- **Перенос старой истории.** До разделения все миграции писались в одну таблицу `goose_db_version`. При первом запуске новый `migration.Run` переносит её одной транзакцией: версии `≤ 00005` уходят в историю платформы, версии `10000+` — в историю расширений, остальные — в историю конфигурации, затем старая таблица удаляется. Каждая версия конфигурации из старой истории должна быть миграцией той конфигурации, с которой запущен перенос, иначе он отказывает. Миграции при переносе повторно не применяются.

### Правила

- **Платформенные миграции заморожены для конфигураций.** Платформа не знает о конфигурациях: её миграции не называют таблиц конфигураций (проверяет `internal/architecture`). Изменение платформенной схемы — это новая миграция в последовательности платформы. Применённую миграцию никто не правит: goose не перезапускает её на существующих тенантах, а эти тенанты принадлежат любым конфигурациям.
- **Конфигурация строится поверх платформы.** Её миграции могут ссылаться на таблицы платформы и добавлять к ним внешние ключи и `CHECK`-ограничения своих правил (например, список провайдеров интеграций в `00006`). Её версии идут от `00001` (у крипто — с `00006`, это наследие общей истории) и не доходят до `10000`. Проверяет `configurationtest.Check`.
- **Модули расширений применяются к тенанту любой конфигурации.** `cmd/scaffold` выбирает следующую свободную версию `10000+` по всем модулям. Модули добавляются в любом порядке, поэтому их история допускает версии не по порядку.

---

## Состав tenant-миграций

| Файл | Домен |
|------|-------|
| `00001_core.sql` | Расширения, общие функции, outbox, idempotency, audit, sequences, event log |
| `00002_auth.sql` | Пользователи, роли, права платформы и их seed, сессии, MFA, RLS/FLS/CEL. Права зарегистрированных сущностей создаёт `migration.SyncPermissions` после миграций |
| `00003_catalogs.sql` | Валюты, организации, контрагенты, договоры |
| `00004_settlement.sql` | Регистр взаиморасчётов |
| `00005_platform.sql` | Настройки, сервисные и интеграционные аккаунты (провайдер — идентификатор каталога, список задаёт конфигурация), курсы валют (источники, соответствия, котировки, состояние загрузки), автоматизация, уведомления, варианты отчётов, list views, worker jobs |
| `configuration/crypto/00006_crypto_core.sql` | Сети, токены, кошельки, инвойсы, платежи, payment links, watcher, наблюдения, funding, wallet pool, вебхуки мерчантов, финансовые регистры; права и FLS криптосущностей; провайдеры интеграций конфигурации |
| `configuration/crypto/00007_crypto_outbound.sql` | Выводы, sweep, treasury, signing, approvals и signing jobs, native funding, аренда ресурсов, сверка и инциденты |
| `configuration/crypto/00008_crypto_aml.sql` | AML |
| `configuration/crypto/00009_crypto_custody.sql` | Custody-учёт: identity, receipts и suspense, реестр признания, сетевые комиссии, expected routing |

Таблица целиком, с колонками, ограничениями, индексами и комментариями, объявлена в миграции своего домена. Функция и триггер, связывающие таблицу с более поздним доменом, объявлены в этом более позднем домене; там же внешний ключ на его таблицу и проверка его правил (например, список провайдеров интеграций конфигурации в `00006`) добавляются однострочным `ALTER TABLE ... ADD CONSTRAINT ... FOREIGN KEY|CHECK`. В каждом файле Up и Down выполняются одним goose statement: функции, затем таблицы, внешние ключи между доменами, представления, триггеры и seed. Down откатывает только свой домен и отказывает, если в нём есть записанные финансовые или AML-доказательства.

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

Для сериализации DDL каждая миграция начинает Up и Down с transaction-scoped lock:

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
| Табличная часть | `<шапка>__<ключ>` | `doc_crypto_sweeps__lines` |

Разделитель `__` зарезервирован для физических табличных частей. Имя шапки
берётся из `entity.Definition.Table`, ключ части задаётся явно в
`entity.Definition.Parts` и не выводится из имени поля Go или JSON. Слова в
имени шапки и в ключе разделяются одним `_`, полное имя не длиннее 63 байт.
Таблица части имеет `document_id UUID NOT NULL` с внешним ключом на `id` шапки.
Коллекции модели без своей таблицы перечисляются в `DerivedParts`; регистрация
метаданных отказывает, если коллекция не объявлена ни там, ни там.
Integration-тест `TestDeclaredTablePartsMatchSchema` сверяет объявления со схемой.

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
администратор создаётся явным запуском команды сида конфигурации (`cmd/seed`,
`cmd/crypto-seed`; в образе `/bin/seed`) с `ADMIN_PASSWORD` или
`ADMIN_PASSWORD_FILE`; пустой secret считается ошибкой. С `SEED_DEMO_DATA=true`
платформа добавляет свои демо-данные, затем конфигурация — свои
(`Configuration.Seed`).

---

## Как применить миграции

### Из CLI (dev/ops)

Команда тенантов своя у каждой конфигурации: `cmd/tenant` работает с пустой конфигурацией, `cmd/crypto-tenant` — с крипто. В Docker-образе `/bin/tenant` — это крипто-версия.

```bash
# Meta-база: схема реестра тенантов (идемпотентно)
go run ./cmd/crypto-tenant init-meta

# Все тенанты этой конфигурации; тенанты других конфигураций пропускаются
go run ./cmd/crypto-tenant migrate --all

# Конкретный тенант
go run ./cmd/crypto-tenant migrate --id <tenant-uuid>
```

Миграции запускаются оператором после остановки server и worker. Команда обрабатывает активные и приостановленные tenant-БД и возвращает ненулевой код, если хотя бы одна операция завершилась ошибкой. Тенант без записанной конфигурации (создан до её учёта) получает конфигурацию команды при первой миграции.

### Внутренний runner

```go
import "metapus/internal/infrastructure/storage/postgres/migration"

result, err := migration.Run(dsn, configuration.MigrationOptions(cfg, extensions.Register)...)
// result.Schema — конфигурация и версии платформы и конфигурации
```

Runner используется tenant CLI и provisioning-контуром. HTTP-endpoint и UI-действия для запуска миграций намеренно отсутствуют.

---

## Связанные документы

- [naming-conventions.md](../../docs/reference/naming-conventions.md) — правила именования
- [development-rules.md](../../docs/reference/development-rules.md) — правила разработки
- [operational-modes.md](../../docs/systems/operational-modes.md) — процедура развёртывания и обновления
