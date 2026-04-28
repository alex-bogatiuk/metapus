# Навигатор документации Metapus

> **TL;DR:** Точка входа для разработчиков и ИИ-агентов. Найдите нужную тему по директории или задаче.

> **Тип:** Reference
> **Аудитория:** All
> **Связанные:** [meta/00-doc-standard.md](meta/00-doc-standard.md)

---

## 1. Путеводитель (Guide)
Читается по порядку для погружения в проект.

| Файл | Описание | Ключевые слова |
|------|----------|----------------|
| [guide/01-overview.md](guide/01-overview.md) | Обзор платформы, стек, манифест | metapus, erp, обзор, манифест, стек |
| [guide/02-architecture.md](guide/02-architecture.md) | Clean Architecture, слои, Code is Metadata | clean architecture, слои, core, domain, infrastructure |
| [guide/03-project-structure.md](guide/03-project-structure.md) | Структура директорий проекта | cmd, internal, pkg, папки, структура |
| [guide/04-request-lifecycle.md](guide/04-request-lifecycle.md) | Жизненный цикл HTTP-запроса, middleware | request, middleware, error handler, lifecycle |

## 2. Глубокое погружение (Systems)
Архитектура подсистем. Читается по мере необходимости.

| Файл | Описание | Ключевые слова |
|------|----------|----------------|
| [systems/auth-security.md](systems/auth-security.md) | Аутентификация, JWT, RBAC, RLS, FLS | jwt, rbac, rls, fls, security, permissions, roles |
| [systems/automation-engine.md](systems/automation-engine.md) | Движок автоматизации, события, правила | automation, engine, rules, events |
| [systems/core-layer.md](systems/core-layer.md) | Базовые примитивы, BaseEntity, BaseCatalog | core, entity, base, catalog, document |
| [systems/crud-pipeline.md](systems/crud-pipeline.md) | Generic CRUD pipeline, обработка запросов | crud, generic, pipeline, factory, handler |
| [systems/dashboard-widgets.md](systems/dashboard-widgets.md) | Дашборды, виджеты, layout пользователя | dashboard, widgets, UI, preferences |
| [systems/domain-layer.md](systems/domain-layer.md) | Бизнес-логика, сервисы, хуки | domain, services, hooks, logic, repository |
| [systems/extension-api.md](systems/extension-api.md) | Плагины, кастомные расширения, модули | extensions, plugins, api, modules, custom |
| [systems/filtering.md](systems/filtering.md) | Система фильтрации, SQL-генерация, типы | filtering, filters, sql, exists, ui |
| [systems/infrastructure-layer.md](systems/infrastructure-layer.md) | Инфраструктура, PostgreSQL, HTTP сервер | infrastructure, postgres, http, gin |
| [systems/multi-tenancy.md](systems/multi-tenancy.md) | Физическая изоляция Database-per-Tenant | multi-tenant, tenant, isolation, database |
| [systems/numerator.md](systems/numerator.md) | Автонумерация, генерация кодов и номеров | numerator, codes, auto-increment, numbering |
| [systems/operational-modes.md](systems/operational-modes.md) | SaaS, Self-Hosted, Dedicated режимы | saas, dedicated, self-hosted, modes, deployment |
| [systems/posting-engine.md](systems/posting-engine.md) | Движок проведения, Visitor pattern | posting, engine, visitor, stock, balances, movements |
| [systems/printing-system.md](systems/printing-system.md) | Печатные формы, шаблоны HTML/PDF | printing, templates, pdf, gohtml, export |
| [systems/realtime-notifications.md](systems/realtime-notifications.md) | Push-уведомления, WebSockets | realtime, websockets, notifications, sse, push |
| [systems/related-documents.md](systems/related-documents.md) | Дерево подчиненности, превью карточек | related, documents, tree, links, subordination |
| [systems/reporting-system.md](systems/reporting-system.md) | Query Engine, сборка отчетов, СКД | reporting, query engine, xlsx, variants |
| [systems/settings-system.md](systems/settings-system.md) | Системные и пользовательские настройки | settings, configuration, ui, metadata |
| [systems/smart-data-entry.md](systems/smart-data-entry.md) | Каскадное автозаполнение, UX-фокус | smart entry, autofill, ux, focus, cascade |
| [systems/transactions.md](systems/transactions.md) | Управление транзакциями, блокировки | transactions, lock, pessimistic, optimistic, txmanager |
| [systems/updater-agent.md](systems/updater-agent.md) | Механизм обновления (Blue-Green) | updater, sidecar, docker, rollback, update |

## 3. Рецепты (How-To)
Пошаговые инструкции для частых задач.

| Файл | Описание | Ключевые слова |
|------|----------|----------------|
| [howto/cloud-deployment.md](howto/cloud-deployment.md) | Облачное SaaS-развертывание (Multi-Version) | cloud, deploy, saas, nginx, router, version |
| [howto/new-document.md](howto/new-document.md) | Инструкция по добавлению нового документа | new, document, migration, posting, validation, M15 |
| [howto/new-entity.md](howto/new-entity.md) | Инструкция по добавлению справочника/документа | new, entity, catalog, document, tutorial |

## 4. Справочник (Reference)
Таблицы, API, строгие правила.

| Файл | Описание | Ключевые слова |
|------|----------|----------------|
| [reference/development-rules.md](reference/development-rules.md) | Строгие архитектурные правила и запреты | rules, development, invariants, limits |
| [reference/frontend-guidelines.md](reference/frontend-guidelines.md) | Правила фронтенда, Next.js, shadcn/ui | frontend, guidelines, next.js, shadcn, react |
| [reference/keyboard-shortcuts.md](reference/keyboard-shortcuts.md) | Глобальные горячие клавиши, useShortcut | shortcuts, keyboard, hotkeys, bindings |
| [reference/naming-conventions.md](reference/naming-conventions.md) | Стандарты именования (БД, Go, API) | naming, conventions, database, api, snake_case |

## 5. Мета-документация
Правила работы с проектом и документацией.

| Файл | Описание | Ключевые слова |
|------|----------|----------------|
| [meta/00-doc-standard.md](meta/00-doc-standard.md) | Стандарт написания документации | документация, стандарт, шаблон, стиль |

---

## Связанные документы
- [meta/00-doc-standard.md](meta/00-doc-standard.md) — правила, по которым написан этот навигатор и все остальные файлы
