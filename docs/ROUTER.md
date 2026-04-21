# Metapus Documentation Router

> **Этот файл — точка входа в документацию проекта.**
> Используйте его для навигации: найдите нужную тему и перейдите к соответствующему файлу.

---

## Навигация по документам

| #  | Файл                                                     | Описание | Ключевые слова |
|----|----------------------------------------------------------|----------|----------------|
| 01 | [01-overview.md](01-overview.md)                         | Обзор платформы, манифест, сравнение с аналогами | metapus, erp, 1с, odoo, golang, манифест, философия |
| 02 | [02-architecture.md](02-architecture.md)                 | Архитектура: слои, metadata hierarchy, Clean Architecture | clean architecture, слои, core, domain, infrastructure, metadata-driven |
| 03 | [03-project-structure.md](03-project-structure.md)       | Структура репозитория, пакеты, файловое дерево | cmd, internal, pkg, db, configs, папки, layout |
| 04 | [04-core-layer.md](04-core-layer.md)                     | Core: BaseEntity, типы, ошибки, UUIDv7, валидация | entity, base, apperror, uuid, attributes, jsonb, validatable |
| 05 | [05-domain-layer.md](05-domain-layer.md)                 | Domain: справочники, документы, регистры, сервисы, хуки | catalog, document, register, service, hooks, validate, bounded context |
| 06 | [06-infrastructure-layer.md](06-infrastructure-layer.md) | Infrastructure: HTTP, Postgres, middleware, composition root | gin, pgx, handler, dto, router, middleware, graceful shutdown |
| 07 | [07-multi-tenancy.md](07-multi-tenancy.md)               | Database-per-Tenant: Manager, пулы, context chain, eviction | tenant, pool, x-tenant-id, manager, eviction, health check, isolation |
| 08 | [08-auth-and-security.md](08-auth-and-security.md)       | JWT, login, refresh, permissions, brute-force, AccessScope | jwt, login, bcrypt, refresh token, permission, role, access scope |
| 09 | [09-crud-pipeline.md](09-crud-pipeline.md)               | Generic CRUD: handler → service → hooks → repo, FactoryRegistry wiring | generic, crud, cataloghandler, hookregistry, mapCreateDTO, wiring, factoryregistry |
| 10 | [10-posting-engine.md](10-posting-engine.md)             | Проведение документов, движения, остатки, RegisterRecorder, триггеры | posting, post, unpost, movements, balances, trigger, stock, ledger, recorder |
| 11 | [11-transactions.md](11-transactions.md)                 | TxManager, вложенные tx, savepoints, optimistic locking | transaction, txmanager, savepoint, version, optimistic locking, rollback |
| 12 | [12-numerator.md](12-numerator.md)                       | Автонумерация: Strict/Cached стратегии, формат номера | numerator, code, sequence, strict, cached, sys_sequences, prefix |
| 13 | [13-request-lifecycle.md](13-request-lifecycle.md)       | Полный путь HTTP-запроса через все слои | request, middleware, recovery, trace, logger, error handler, idempotency |
| 14 | [14-howto-new-entity.md](14-howto-new-entity.md)         | Пошаговое руководство добавления новой сущности | howto, добавить, новый справочник, новый документ, пример, checklist |
| 15 | [15-naming-conventions.md](15-naming-conventions.md)     | Правила именования: таблицы, Go, REST, префиксы | naming, cat_, doc_, reg_, sys_, snake_case, camelCase, kebab-case |
| 16 | [16-development-rules.md](16-development-rules.md)       | Правила разработки: стиль, ошибки, тестируемость, concurrency | rules, стиль, тесты, goroutine, context, миграции, cdc |
| 17 | [17-migration-status.md](17-migration-status.md)         | Статус миграции DB-per-Tenant, env variables, CLI | migration, status, env, cli, tenant, goose, docker |
| 18 | [18-filtering.md](18-filtering.md)                       | Система фильтрации: metadata → frontend → SQL, масштабирование, табличные части | filter, отбор, advanced filter, money, quantity, exists, metadata-driven, scale |
| 19 | [19-dashboard-widgets.md](19-dashboard-widgets.md)       | Dashboard виджеты: создание, реестр, рендерер, конфиг, galley | widget, dashboard, виджет, реестр, renderer, useWidgetData, WidgetRenderProps |
| 20 | [20-related-documents.md](20-related-documents.md)       | Связанные документы: дерево подчинённости, Find References | related, subordination, tree, find references, RefResolverRepo |
| 21 | [21-extension-api.md](21-extension-api.md)               | Extension API: расширение Metapus, FactoryRegistry, HookRegistry, UIRegistry | extension, plugin, расширение, кастомизация, FactoryRegistry, HookRegistry, UIRegistry, client-ext |
| 22 | [22-cloud-deployment.md](22-cloud-deployment.md)         | Cloud Deployment: multi-version, nginx routing, version groups | cloud, saas, deployment, nginx, version group, routing, docker, multi-version |
| 23 | [23-operational-modes.md](23-operational-modes.md)       | Операционные режимы (SaaS, Standalone Multi-Tenant, Dedicated) | mode, isolation, saas, cloud, on-premise, dedicated, shared DB |
| 24 | [24-updater-agent.md](24-updater-agent.md)               | Updater Agent: обновление из UI, Docker orchestration, state machine | updater, sidecar, docker, blue-green, rollback, state machine, WAL |
| 25 | [25-realtime-notifications.md](25-realtime-notifications.md) | Real-time notifications: SSE, EventStream, client hooks | sse, event, notification, real-time, stream, push |
| 26 | [26-automation-engine.md](26-automation-engine.md)       | Automation Engine: triggers, actions, schedules | automation, trigger, action, schedule, workflow |
| 27 | [27-printing-system.md](27-printing-system.md)           | Система печатных форм: макеты, реестр, рендеринг | print, печать, шаблон, gohtml, pdf, docx, registry |
| — | [UPGRADE.md](UPGRADE.md)                                | Гайд по обновлению: совместимость, breaking changes, миграция | upgrade, обновление, совместимость, breaking change, semver |

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
- **Написать миграцию БД** → [16-development-rules.md](16-development-rules.md) (раздел "Миграции"), [15-naming-conventions.md](15-naming-conventions.md)
- **Понять как работает запрос от HTTP до БД** → [13-request-lifecycle.md](13-request-lifecycle.md)
- **Настроить нумерацию кодов/номеров** → [12-numerator.md](12-numerator.md)
- **Разобраться с транзакциями и блокировками** → [11-transactions.md](11-transactions.md)
- **Понять базовые типы (Entity, ID, Errors)** → [04-core-layer.md](04-core-layer.md)
- **Узнать правила именования** → [15-naming-conventions.md](15-naming-conventions.md)
- **Настроить БД и окружение** → [17-migration-status.md](17-migration-status.md)
- **Понять систему фильтрации списков** → [18-filtering.md](18-filtering.md)
- **Создать виджет дашборда** → [19-dashboard-widgets.md](19-dashboard-widgets.md)
- **Добавить печатную форму** → [27-printing-system.md](27-printing-system.md)
- **Расширить систему для клиента (extension)** → [21-extension-api.md](21-extension-api.md), [UPGRADE.md](UPGRADE.md)
- **Добавить новый регистр накопления** → [21-extension-api.md](21-extension-api.md) (Visitor+Recorder), [10-posting-engine.md](10-posting-engine.md)
- **Обновиться на новую версию ядра** → [UPGRADE.md](UPGRADE.md)
- **Развернуть Cloud/SaaS (multi-version)** → [22-cloud-deployment.md](22-cloud-deployment.md)
- **Изучить варианты развертывания (On-Premise, SaaS)** → [23-operational-modes.md](23-operational-modes.md)
- **Настроить обновление из UI (Updater Agent)** → [24-updater-agent.md](24-updater-agent.md)

---

## Иерархия чтения (для новичков)

1. [01-overview.md](01-overview.md) — что за проект
2. [02-architecture.md](02-architecture.md) — как устроен
3. [03-project-structure.md](03-project-structure.md) — где что лежит
4. [04-core-layer.md](04-core-layer.md) → [05-domain-layer.md](05-domain-layer.md) → [06-infrastructure-layer.md](06-infrastructure-layer.md) — слои снизу вверх
5. [07-multi-tenancy.md](07-multi-tenancy.md) — ключевая особенность
6. [14-howto-new-entity.md](14-howto-new-entity.md) — практика
