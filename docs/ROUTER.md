# Metapus Documentation Router

> **Этот файл — точка входа в документацию проекта.**
> Используйте его для навигации: найдите нужную тему и перейдите к соответствующему файлу.

---

## Навигация по документам

| # | Файл | Описание | Ключевые слова |
|---|------|----------|----------------|
| 01 | [01-overview.md](01-overview.md) | Обзор платформы, манифест, сравнение с аналогами | metapus, erp, 1с, odoo, golang, манифест, философия |
| 02 | [02-architecture.md](02-architecture.md) | Архитектура: слои, metadata hierarchy, Clean Architecture | clean architecture, слои, core, domain, infrastructure, metadata-driven |
| 03 | [03-project-structure.md](03-project-structure.md) | Структура репозитория, пакеты, файловое дерево | cmd, internal, pkg, db, configs, папки, layout |
| 04 | [04-core-layer.md](04-core-layer.md) | Core: BaseEntity, типы, ошибки, UUIDv7, валидация | entity, base, apperror, uuid, attributes, jsonb, validatable |
| 05 | [05-domain-layer.md](05-domain-layer.md) | Domain: справочники, документы, регистры, сервисы, хуки | catalog, document, register, service, hooks, validate, bounded context |
| 06 | [06-infrastructure-layer.md](06-infrastructure-layer.md) | Infrastructure: HTTP, Postgres, middleware, composition root | gin, pgx, handler, dto, router, middleware, graceful shutdown |
| 07 | [07-multi-tenancy.md](07-multi-tenancy.md) | Database-per-Tenant: Manager, пулы, context chain, eviction | tenant, pool, x-tenant-id, manager, eviction, health check, isolation |
| 08 | [08-auth-and-security.md](08-auth-and-security.md) | JWT, login, refresh, permissions, brute-force, AccessScope | jwt, login, bcrypt, refresh token, permission, role, access scope |
| 09 | [09-crud-pipeline.md](09-crud-pipeline.md) | Generic CRUD: handler → service → hooks → repo, wiring | generic, crud, cataloghandler, hookregistry, mapCreateDTO, wiring |
| 10 | [10-posting-engine.md](10-posting-engine.md) | Проведение документов, движения, остатки, триггеры | posting, post, unpost, movements, balances, trigger, stock, ledger |
| 11 | [11-transactions.md](11-transactions.md) | TxManager, вложенные tx, savepoints, optimistic locking | transaction, txmanager, savepoint, version, optimistic locking, rollback |
| 12 | [12-numerator.md](12-numerator.md) | Автонумерация: Strict/Cached стратегии, формат номера | numerator, code, sequence, strict, cached, sys_sequences, prefix |
| 13 | [13-request-lifecycle.md](13-request-lifecycle.md) | Полный путь HTTP-запроса через все слои | request, middleware, recovery, trace, logger, error handler, idempotency |
| 14 | [14-howto-new-entity.md](14-howto-new-entity.md) | Пошаговое руководство добавления новой сущности | howto, добавить, новый справочник, новый документ, пример, checklist |
| 15 | [15-naming-conventions.md](15-naming-conventions.md) | Правила именования: таблицы, Go, REST, префиксы | naming, cat_, doc_, reg_, sys_, snake_case, camelCase, kebab-case |
| 16 | [16-roadmap.md](16-roadmap.md) | Этапы разработки, метрики готовности | roadmap, этапы, mvp, beta, milestone |
| 17 | [17-development-rules.md](17-development-rules.md) | Правила разработки: стиль, ошибки, тестируемость, concurrency | rules, стиль, тесты, goroutine, context, миграции, cdc |
| 18 | [18-migration-status.md](18-migration-status.md) | Статус миграции DB-per-Tenant, env variables, CLI | migration, status, env, cli, tenant, goose, docker |

---

## Быстрый поиск по задаче

### Я хочу...

- **Понять, что такое Metapus** → [01-overview.md](01-overview.md)
- **Разобраться в архитектуре** → [02-architecture.md](02-architecture.md), затем [03-project-structure.md](03-project-structure.md)
- **Добавить новый справочник** → [14-howto-new-entity.md](14-howto-new-entity.md) (пошагово), [09-crud-pipeline.md](09-crud-pipeline.md) (как работает CRUD)
- **Добавить новый документ** → [14-howto-new-entity.md](14-howto-new-entity.md) (раздел "Документы"), [10-posting-engine.md](10-posting-engine.md) (проведение)
- **Разобраться в multi-tenancy** → [07-multi-tenancy.md](07-multi-tenancy.md)
- **Реализовать проведение документа** → [10-posting-engine.md](10-posting-engine.md), [11-transactions.md](11-transactions.md)
- **Добавить аутентификацию / права** → [08-auth-and-security.md](08-auth-and-security.md)
- **Написать миграцию БД** → [17-development-rules.md](17-development-rules.md) (раздел "Миграции"), [15-naming-conventions.md](15-naming-conventions.md)
- **Понять как работает запрос от HTTP до БД** → [13-request-lifecycle.md](13-request-lifecycle.md)
- **Настроить нумерацию кодов/номеров** → [12-numerator.md](12-numerator.md)
- **Разобраться с транзакциями и блокировками** → [11-transactions.md](11-transactions.md)
- **Понять базовые типы (Entity, ID, Errors)** → [04-core-layer.md](04-core-layer.md)
- **Узнать правила именования** → [15-naming-conventions.md](15-naming-conventions.md)
- **Посмотреть roadmap** → [16-roadmap.md](16-roadmap.md)
- **Настроить БД и окружение** → [18-migration-status.md](18-migration-status.md)

---

## Иерархия чтения (для новичков)

1. [01-overview.md](01-overview.md) — что за проект
2. [02-architecture.md](02-architecture.md) — как устроен
3. [03-project-structure.md](03-project-structure.md) — где что лежит
4. [04-core-layer.md](04-core-layer.md) → [05-domain-layer.md](05-domain-layer.md) → [06-infrastructure-layer.md](06-infrastructure-layer.md) — слои снизу вверх
5. [07-multi-tenancy.md](07-multi-tenancy.md) — ключевая особенность
6. [14-howto-new-entity.md](14-howto-new-entity.md) — практика
