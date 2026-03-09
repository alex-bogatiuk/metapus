# Roadmap — Этапы разработки

> План развития платформы Metapus от фундамента до production-ready состояния.

---

## Этапы

### Этап 0: Фундамент (Core & Infrastructure)
- Структура проекта и `go.mod`
- Base entities (ID, Timestamps, Version, Attributes)
- UUIDv7 генерация
- AppError структура ошибок
- PostgreSQL connection pool (pgxpool)
- Transaction Manager
- Миграции системных таблиц (sequences, outbox, audit)
- Graceful Shutdown
- Structured Logger + Trace ID
- testcontainers-go для интеграционных тестов

### Этап 1: Справочники (Catalogs)
- Numerator сервис (автонумерация)
- JWT Middleware + AccessScope
- Справочник "Контрагенты" (CRUD + Hooks)
- Справочник "Номенклатура" (+ иерархия)
- Справочник "Склады"
- Справочник "Единицы измерения"
- Справочник "Валюты"

### Этап 2: Документы и Проведение (Documents & Posting)
- Документ "Поступление товаров"
- Регистр "Товары на складах" (movements + balances)
- Документ "Реализация" + контроль остатков
- Immutable Ledger + версионирование движений
- Delta-обновление остатков
- Idempotency для проведения
- Resource Ordering (предотвращение Deadlock)

### Этап 3: Асинхронность (Background Jobs)
- Outbox Worker (Polling → Kafka/NATS)
- Базовые отчёты (SQL-first)
- Закрытие периода (Soft Close)

### Этап 4: Расширяемость (Customization)
- `sys_custom_field_schemas`
- Schema Cache (In-Memory + NOTIFY/LISTEN)
- Dependency Checker (JSONB ссылки)
- UI Hints endpoint (`/meta/layouts`)

### Этап 5: Production Readiness
- AuthService (Refresh tokens, Revoke)
- RBAC (Roles + Permissions)
- Audit с партиционированием
- Prometheus метрики
- Healthcheck endpoints
- OpenTelemetry tracing

### Этап 6: Интеграции
- Event Bus (Kafka/NATS)
- Webhooks
- Integration API
- Import/Export (Excel, CSV)

### Этап 7: UI (Web & Mobile)
- Next.js Admin Panel
- Metadata-driven forms
- Data tables (AG Grid / TanStack)
- Mobile App (React Native / Flutter)

---

## Метрики готовности

| Milestone | Критерии | Срок |
|-----------|----------|------|
| **MVP** | 5 справочников, 3 документа, Stock register, Auth | +6 недель |
| **Beta** | Все справочники, Workflow, Интеграции | +12 недель |
| **RC** | UI, Reports, Mobile | +18 недель |
| **GA** | Production-ready, Documentation | +22 недели |

---

## Связанные документы

- [01-overview.md](01-overview.md) — обзор платформы
- [02-architecture.md](02-architecture.md) — архитектурные решения
