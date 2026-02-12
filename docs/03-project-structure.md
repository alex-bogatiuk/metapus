# Структура проекта Metapus

> Полное файловое дерево проекта с описанием назначения каждого каталога и файла.

---

## Верхнеуровневая структура

```
metapus/
├── cmd/                           # ТОЧКИ ВХОДА
├── configs/                       # Конфигурация
├── db/                            # Миграции и seed-данные
├── internal/                      # ПРИВАТНЫЙ КОД (основной)
├── pkg/                           # ПУБЛИЧНЫЕ УТИЛИТЫ
├── docs/                          # Документация
├── Dockerfile
├── docker-compose.yml
├── go.mod
└── go.sum
```

---

## cmd/ — Точки входа

```
cmd/
├── server/
│   └── main.go               # REST API сервер (DI, конфиг, graceful shutdown)
├── worker/
│   └── main.go               # Multi-tenant воркер фоновых задач
├── tenant/
│   └── main.go               # CLI управления тенантами (create, migrate, list)
└── seed/
    └── main.go               # Seed данных для разработки
```

Каждый `main.go` — composition root: собирает зависимости (DI руками), настраивает логирование/конфиг, поднимает HTTP/router/worker, делает graceful shutdown.

---

## configs/ — Конфигурация

```
configs/
├── .env.example              # Шаблон переменных окружения
└── config.yaml               # Примеры конфигурации
```

Ключевые переменные окружения для Database-per-Tenant:
- `META_DATABASE_URL` — подключение к meta-database
- `TENANT_DB_DEFAULT_HOST/PORT/USER/PASSWORD` — credentials для tenant-БД
- `TENANT_POOL_MAX_CONNS`, `TENANT_POOL_IDLE_TIMEOUT`, `TENANT_MAX_TOTAL_POOLS`

---

## db/ — Миграции

```
db/
├── meta/                     # Миграции Meta-database (реестр тенантов)
│   └── 00001_tenants.sql     # Схема для управления тенантами
├── migrations/               # SQL миграции для tenant databases (goose)
│   ├── 00001_init_extensions.sql          # Расширения (UUIDv7, pg_trgm)
│   ├── 00002_sys_sequences.sql            # Автонумерация
│   ├── 00003_sys_outbox.sql               # Transactional Outbox
│   ├── 00004_sys_idempotency.sql          # Идемпотентность
│   ├── 00005_sys_audit.sql                # Аудит
│   ├── 00006_sys_sessions.sql             # Сессии
│   ├── 00007_sys_custom_fields.sql        # Пользовательские поля
│   ├── 00008_sys_feature_flags.sql        # Feature flags
│   ├── 00009_base_indexes.sql             # Базовые индексы
│   ├── 00010_auth_users.sql               # Пользователи и аутентификация
│   ├── 00011_cat_currencies.sql           # Справочник валют
│   ├── 00012_cat_organizations.sql        # Справочник организаций
│   ├── 00013_cat_counterparties.sql       # Справочник контрагентов
│   ├── 00014_cat_units.sql                # Справочник единиц измерения
│   ├── 00015_cat_warehouses.sql           # Справочник складов
│   ├── 00016_cat_vat_rates.sql            # Справочник ставок НДС
│   ├── 00017_cat_nomenclature.sql         # Справочник номенклатуры
│   ├── 00018_cat_contracts.sql            # Справочник договоров
│   ├── 00020_doc_goods_receipt.sql        # Документ поступления товаров
│   ├── 00021_doc_goods_issue.sql          # Документ отгрузки товаров
│   ├── 00022_auth_seed_permissions.sql    # Seed: базовые разрешения
│   ├── 00023_auth_seed_roles.sql          # Seed: базовые роли
│   ├── 00024_auth_seed_document_permissions.sql  # Seed: разрешения документов
│   ├── 00025_auth_seed_report_permissions.sql    # Seed: разрешения отчётов
│   ├── 00026_auth_seed_docs_permissions.sql      # Seed: доп. разрешения документов
│   ├── 00027_seed_default_roles.sql       # Seed: роли по умолчанию
│   ├── 00028_reg_stock.sql                # Регистр товаров на складах
│   └── 00029_add_cdc_to_entities.sql      # CDC-колонки для сущностей
└── seeds/                    # Начальные данные
```

**Нумерация миграций:**
- `00001–00009` — системные таблицы (`sys_*`, расширения, индексы)
- `00010`       — аутентификация (`auth_users`)
- `00011–00018` — справочники (`cat_*`)
- `00020–00021` — документы (`doc_*`)
- `00022–00027` — seed-данные (разрешения, роли)
- `00028–00029` — регистры (`reg_*`) и CDC

---

## internal/ — Приватный код

### internal/core/ — Ядро

```
internal/core/
├── apperror/
│   └── error.go              # AppError (Code, Message, Details) — RFC 7807
├── context/
│   └── user_context.go       # Извлечение UserID из ctx
├── entity/
│   ├── base.go               # BaseEntity, BaseCatalog, BaseDocument
│   ├── catalog.go            # Catalog struct (NewCatalog)
│   ├── document.go           # Document struct (NewDocument)
│   └── register.go           # StockMovement, StockBalance
├── id/
│   └── uuid.go               # UUIDv7 генерация
├── instance/
│   └── isolation.go          # DedicatedIsolation (no-op для DB-per-Tenant)
├── tenant/
│   ├── types.go              # Tenant struct (ID, Slug, DBName, Status)
│   ├── context.go            # WithPool, WithTxManager, WithTenant
│   ├── registry.go           # PostgresRegistry для meta-database
│   └── manager.go            # MultiTenantManager (пулы соединений)
├── tx/
│   └── tx.go                 # Transaction Manager interface
├── security/
│   ├── scope.go              # AccessScope (UserID, Roles)
│   └── jwt.go                # JWT Claims
└── types/
    └── money.go              # MinorUnits, Quantity, Money (Decimal)
```

### internal/domain/ — Бизнес-логика

```
internal/domain/
├── catalogs/
│   ├── counterparty/         # model.go, repo.go, service.go, hooks.go
│   ├── nomenclature/         # model.go, repo.go, service.go
│   ├── warehouse/
│   ├── currency/
│   ├── unit/
│   ├── vat_rate/             # Ставки НДС
│   └── contract/             # Договоры контрагентов
├── documents/
│   ├── goods_receipt/        # model.go, repo.go, service/{crud.go, posting.go}
│   ├── invoice/              # model.go, repo.go, service/{crud.go, posting.go, stock_control.go}
│   └── stock_transfer/
├── registers/
│   ├── accumulation/
│   │   └── stock/            # model.go, repo.go, service.go
│   └── information/
│       ├── currency_rates/
│       └── barcodes/
├── reports/
│   ├── stock_balance/
│   └── sales_turnover/
├── posting/
│   └── engine.go             # Движок проведения документов
└── workflow/
    ├── engine.go
    └── tasks.go
```

**Каждый bounded context** содержит:
- `model.go` — Go struct + `Validate(ctx)` (бизнес-инварианты)
- `repo.go` — интерфейс репозитория (объявляется в domain)
- `service.go` — оркестрация use case, хуки, транзакции

### internal/infrastructure/ — Реализация

```
internal/infrastructure/
├── storage/
│   └── postgres/
│       ├── connection.go         # pgxpool setup
│       ├── tx_manager.go         # Transaction Manager implementation
│       ├── outbox.go             # Outbox Publisher
│       ├── idempotency.go        # Idempotency Store
│       ├── catalog_repo/         # Реализации для справочников
│       │   ├── base.go           # BaseCatalogRepo[T] (generic)
│       │   ├── counterparty.go
│       │   └── nomenclature.go
│       ├── document_repo/        # Реализации для документов
│       │   ├── goods_receipt.go
│       │   └── invoice.go
│       └── register_repo/
│           └── stock.go
├── http/
│   └── v1/
│       ├── router.go             # Gin router setup + wiring
│       ├── dto/                  # Request/Response DTOs
│       │   ├── catalog.go
│       │   └── document.go
│       ├── handlers/             # HTTP handlers
│       │   ├── catalog.go        # CatalogHandler[T] (generic)
│       │   ├── document.go       # BaseDocumentHandler[T] (generic)
│       │   └── health.go
│       └── middleware/
│           ├── recovery.go       # Panic recovery
│           ├── trace.go          # X-Request-ID, X-Trace-ID
│           ├── logger.go         # Structured logging
│           ├── error.go          # Единая обработка ошибок → JSON
│           ├── auth.go           # JWT validation + tenant match
│           ├── tenant.go         # TenantDB (DB-per-Tenant)
│           └── idempotency.go    # X-Idempotency-Key
├── cache/
│   ├── schema_cache.go           # In-Memory Schema Cache
│   └── feature_flags.go
└── worker/
    ├── base.go                   # BaseWorker (итерация по тенантам)
    ├── outbox_relay.go           # Multi-tenant Outbox → Kafka/NATS
    ├── audit_cleaner.go          # Удаление старых партиций
    └── handlers/
        └── month_closing.go      # Multi-tenant закрытие периода
```

---

## pkg/ — Публичные утилиты

```
pkg/
├── logger/
│   └── logger.go             # Zap wrapper
├── numerator/
│   └── service.go            # Автонумерация (Strict/Cached)
└── decimal/
    └── helpers.go
```

---

## Связанные документы

- [02-architecture.md](02-architecture.md) — архитектурные принципы и слои
- [04-core-layer.md](04-core-layer.md) — детали Core слоя
- [14-howto-new-entity.md](14-howto-new-entity.md) — куда класть новые файлы
