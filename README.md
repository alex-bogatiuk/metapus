<p align="center">
  <h1 align="center">Metapus</h1>
  <p align="center">
    <strong>Open-Source ERP платформа</strong> — современная альтернатива 1С:Предприятие, Odoo, ERPNext
  </p>
  <p align="center">
    <a href="#-быстрый-старт">Быстрый старт</a> ·
    <a href="#-архитектура">Архитектура</a> ·
    <a href="#-документация">Документация</a> ·
    <a href="#-расширения">Расширения</a> ·
    <a href="CONTRIBUTING.md">Участие в разработке</a>
  </p>
  <p align="center">
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0-blue.svg" alt="License: AGPL-3.0"></a>
    <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white" alt="Go 1.25"></a>
    <a href="https://nextjs.org/"><img src="https://img.shields.io/badge/Next.js-16-000000?logo=next.js&logoColor=white" alt="Next.js 16"></a>
    <a href="https://www.postgresql.org/"><img src="https://img.shields.io/badge/PostgreSQL-17-4169E1?logo=postgresql&logoColor=white" alt="PostgreSQL 17"></a>
  </p>
</p>

---

## Что такое Metapus?

**Metapus** — современная веб-платформа для построения бизнес-приложений, спроектированная с нуля на Go, Next.js и PostgreSQL. Она объединяет лучшие практики корпоративных систем (1С:Предприятие, SAP, ERPNext, Odoo) с современной инженерией: Go generics, Clean Architecture, строгая типизация и неизменяемый регистр (Immutable Ledger).

### Почему Metapus?

| Проблема традиционных ERP | Решение Metapus |
|--------------------------|-----------------|
| Runtime-рефлексия и duck typing | **Go generics** — безопасность на этапе компиляции |
| Монолитные типы документов (200+ полей) | **Композиция**: Document + Lines + CurrencyAware трейты |
| Метаданные хранятся в базе данных | **Code is Metadata** — Go-структуры как единственный источник истины |
| Глобальные блокировки при проведении | **Resource ordering** + advisory locks (без deadlock-ов) |
| Мутабельный регистр с UPDATE на движениях | **Immutable Ledger** — обновление остатков через триггеры |
| Shared-DB мультитенантность с фильтрацией по `tenant_id` | **Database-per-Tenant** — физическая изоляция |

---

## ✨ Возможности

### Ядро платформы
- 🏗️ **Clean Architecture** — строгая изоляция слоёв (Core → Domain → Infrastructure)
- 🏢 **Database-per-Tenant** — полная изоляция данных для каждого клиента, без колонок `tenant_id`
- 📄 **Движок проведения документов** — проведение/отмена/перепроведение с автоматической записью движений в регистры
- 📊 **Регистры накопления** — Товары на складах, Себестоимость, Взаиморасчёты с триггерными остатками
- 🔢 **Автонумерация** — настраиваемый нумератор со стратегиями Strict/Cached
- 🔒 **Корпоративная безопасность** — JWT-аутентификация, RBAC, RLS (строковый уровень), FLS (полевой уровень), CEL-движок политик
- 🧩 **API расширений** — добавление пользовательских сущностей, регистров, хуков и виджетов дашборда
- 🔄 **Агент обновлений** — Docker-обновления с применением из UI (blue-green развёртывание)

### Встроенные сущности

| Справочники (НСИ) | Документы (транзакции) | Регистры |
|-------------------|----------------------|----------|
| Контрагенты | Поступление товаров | Товары на складах (склад × номенклатура) |
| Номенклатура (иерархический) | Отгрузка товаров | Себестоимость (склад × номенклатура × валюта) |
| Склады | Перемещение товаров | Взаиморасчёты (контрагент × договор × валюта) |
| Валюты | | |
| Единицы измерения | | |
| Ставки НДС | | |
| Договоры (подчинённый) | | |

### Фронтенд
- ⚡ **Next.js 16** с React 19 и Turbopack
- 🎨 **shadcn/ui** + Tailwind CSS дизайн-система
- 📋 **Generic-компоненты списков и форм** — metadata-driven, без boilerplate для каждой сущности
- 📊 **Настраиваемый дашборд** — drag-and-drop сетка виджетов (Recharts)
- 🔍 **Расширенная фильтрация** — metadata-driven, автоматически генерируется из бэкенда
- 📝 **Черновики форм** — локальное сохранение черновиков, отслеживание dirty-state

---

## 🛠️ Стек технологий

| Слой | Технология |
|------|-----------|
| **Бэкенд** | Go 1.25 · Gin · pgx (нативный драйвер PostgreSQL) · squirrel (query builder) |
| **Фронтенд** | Next.js 16 · React 19 · TypeScript · Tailwind CSS · shadcn/ui · Zustand · Zod · React Hook Form |
| **База данных** | PostgreSQL 17 · UUIDv7 · goose (миграции) · pg_trgm |
| **Наблюдаемость** | Zap (структурированное логирование) · OpenTelemetry · X-Request-ID трассировка |
| **Инфраструктура** | Docker · docker compose · GHCR · distroless runtime |

---

## 🚀 Быстрый старт

### Требования

- **Go** 1.25+
- **Node.js** 20+ и **npm** (или **pnpm**)
- **PostgreSQL** 17+ (или Docker)
- **Docker** (опционально, для контейнеризированного запуска)

### Вариант A: Локальная разработка

```bash
# Клонирование репозитория
git clone https://github.com/alex-bogatiuk/metapus.git
cd metapus

# Запуск PostgreSQL (если используете Docker)
docker run -d --name metapus-pg \
  -e POSTGRES_USER=metapus \
  -e POSTGRES_PASSWORD=metapus \
  -e POSTGRES_DB=tenants \
  -p 5432:5432 postgres:17-alpine

# Установка переменных окружения
export META_DATABASE_URL=postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable
export TENANT_DB_USER=metapus
export TENANT_DB_PASSWORD=metapus
export JWT_SECRET=your-dev-secret

# Создание тенанта и запуск миграций
go run cmd/tenant/main.go create --slug=default --name="Моя компания"
go run cmd/tenant/main.go migrate

# Запуск бэкенд-сервера
go run ./cmd/server

# В отдельном терминале — запуск фронтенда
cd frontend
npm install
npm run dev
```

API доступен по адресу `http://localhost:8080`, веб-интерфейс — `http://localhost:3000`.

### Вариант B: Docker Compose

```bash
# Клонирование и настройка
git clone https://github.com/alex-bogatiuk/metapus.git
cd metapus

# Создание файла .env
cat > .env <<EOF
JWT_SECRET=change-me-to-a-strong-secret
POSTGRES_PASSWORD=metapus
TENANT_ID=00000000-0000-0000-0000-000000000001
EOF

# Запуск всех сервисов
docker compose up -d

# Начальная настройка (только при первом запуске)
docker compose exec metapus-app /tenant create --slug=default --name="Моя компания"
docker compose exec metapus-app /tenant migrate
```

---

## 🏛️ Архитектура

```
┌─────────────────────────────────────────────────┐
│  cmd/*          — Composition Root (DI, запуск)  │
├─────────────────────────────────────────────────┤
│  infrastructure — Адаптеры (HTTP, Postgres,      │
│                   кэш, воркеры)                   │
├─────────────────────────────────────────────────┤
│  domain         — Бизнес-логика и use cases      │
│                   (сервисы, интерфейсы репо)       │
├─────────────────────────────────────────────────┤
│  core           — Фундаментальные типы и         │
│                   политики (entity, ошибки, tx)    │
└─────────────────────────────────────────────────┘
```

**Правило зависимостей:** внутренние слои **никогда не импортируют** внешние.

### Семь принципов

1. **CODE IS METADATA** — Go-структуры как единственный источник истины
2. **EXPLICIT OVER IMPLICIT** — никакой магии ORM, явные транзакции и блокировки
3. **PERFORMANCE FIRST** — нативный драйвер pgx, пулы соединений, MinorUnits для точности
4. **LAYERED ISOLATION** — Domain ничего не знает о HTTP и Postgres
5. **IMMUTABLE LEDGER** — движения регистров никогда не обновляются (UPDATE)
6. **DATABASE-PER-TENANT** — физическая изоляция БД для каждого клиента
7. **NAMING IS CONTRACT** — единые соглашения об именовании на всех слоях

### Процесс проведения документа

```
Service.Post(docID)
├── Загрузка документа + строки
└── PostingEngine.Post(ctx, doc)
    └── RunInTransaction()
        ├── Реверс старых движений (если перепроведение)
        ├── Сбор движений через Visitor Pattern
        │   ├── StockVisitor   → doc.(StockMovementSource)
        │   ├── CostVisitor    → doc.(CostMovementSource)
        │   └── SettlementVisitor → doc.(SettlementMovementSource)
        ├── Проверка остатков (resource ordering + FOR UPDATE)
        ├── Запись движений (batch COPY)
        ├── Триггеры автоматически обновляют остатки
        └── Пометка документа как проведённого
```

---

## 📁 Структура проекта

```
metapus/
├── cmd/                           # Точки входа
│   ├── server/                    # REST API сервер
│   ├── worker/                    # Воркер фоновых задач
│   ├── tenant/                    # CLI управления тенантами
│   └── seed/                     # Начальные данные для разработки
├── internal/
│   ├── core/                      # Фундаментальные типы (entity, ошибки, types, security)
│   ├── domain/                    # Бизнес-логика (справочники, документы, регистры, проведение)
│   └── infrastructure/            # Адаптеры (HTTP-хендлеры, PostgreSQL-репозитории, middleware)
├── pkg/                           # Публичные утилиты (logger, numerator, decimal)
├── db/
│   ├── meta/                      # Миграции мета-базы (реестр тенантов)
│   └── migrations/                # Миграции баз данных тенантов (goose)
├── extensions/                    # Пользовательские расширения сущностей
├── frontend/                      # Веб-приложение Next.js
│   ├── app/                       # Страницы Next.js App Router
│   ├── components/                # shadcn/ui + пользовательские компоненты
│   ├── hooks/                     # React-хуки (слой оркестрации)
│   ├── lib/                       # Утилиты, API-клиенты, общая логика
│   ├── stores/                    # Управление состоянием (Zustand)
│   └── types/                     # Определения типов TypeScript
├── docs/                          # Документация платформы (24 статьи)
├── Dockerfile                     # Многоэтапная продакшн-сборка
├── docker-compose.yml             # Развёртывание для одного тенанта
└── Makefile                       # Цели: сборка, линтер, тесты, запуск
```

---

## 🧩 Расширения

Metapus поддерживает **трёхуровневую модель расширений** для добавления пользовательских сущностей, хуков и регистров без модификации ядра.

### Создание справочника (Scaffold)

```bash
go run cmd/scaffold/main.go --name employee --type catalog
```

### Регистрация в `main.go`

```go
import "metapus/extensions/employee"

employee.Register(factoryReg, platform.ExtensionConfig{})
```

### Точки расширения

| Точка расширения | Описание |
|-----------------|----------|
| `CatalogRegistration` | Новая сущность нормативно-справочных данных (справочник) |
| `DocumentRegistration` | Новый транзакционный документ |
| `HookRegistry[T]` | Хуки жизненного цикла для существующих сущностей (before/after create, update, delete) |
| `RegisterVisitor` + `RegisterRecorder` | Пользовательские регистры накопления для движка проведения |
| Пользовательские поля (JSONB) | No-code дополнительные реквизиты через `sys_custom_field_schemas` |
| CEL-правила политик | Тонкая авторизация на основе выражений |
| Виджеты дашборда | Пользовательские рендереры виджетов для сетки дашборда |

### Автообнаружение на фронтенде

Сущности расширений **автоматически обнаруживаются** через `/api/v1/meta/entities`. Боковая панель, страницы списков и формы генерируются из метаданных — для базового CRUD фронтенд-код не требуется.

Подробнее: [Справочник API расширений](docs/21-extension-api.md).

---

## 🚢 Режимы развёртывания

| Режим | Описание | Сценарий использования |
|-------|---------|----------------------|
| **Cloud SaaS** | Мультиверсионный, маршрутизация через Nginx, канареечные развёртывания | SaaS-провайдер |
| **Self-Hosted** | Одна версия, несколько тенантов | Холдинг с дочерними компаниями |
| **Dedicated** | Один тенант, один экземпляр | Отдельная компания |

Все режимы используют изоляцию **Database-per-Tenant**. См. [Операционные режимы](docs/23-operational-modes.md) и [Руководство по облачному развёртыванию](docs/22-cloud-deployment.md).

---

## 📖 Документация

Полная документация доступна в директории [`docs/`](docs/):

| # | Документ | Тема |
|---|----------|------|
| 01 | [Обзор](docs/01-overview.md) | Манифест платформы и сравнение |
| 02 | [Архитектура](docs/02-architecture.md) | Clean Architecture, слои, иерархия метаданных |
| 03 | [Структура проекта](docs/03-project-structure.md) | Структура репозитория и пакетов |
| 04–06 | [Core](docs/04-core-layer.md) · [Domain](docs/05-domain-layer.md) · [Infrastructure](docs/06-infrastructure-layer.md) | Послойное погружение |
| 07 | [Мультитенантность](docs/07-multi-tenancy.md) | Реализация Database-per-Tenant |
| 08 | [Аутентификация и безопасность](docs/08-auth-and-security.md) | JWT, RBAC, RLS, FLS, CEL-политики |
| 09 | [CRUD-пайплайн](docs/09-crud-pipeline.md) | Generic handler → service → repo пайплайн |
| 10 | [Движок проведения](docs/10-posting-engine.md) | Проведение документов, движения, регистры |
| 11 | [Транзакции](docs/11-transactions.md) | TxManager, savepoints, оптимистическая блокировка |
| 12 | [Нумератор](docs/12-numerator.md) | Стратегии автонумерации |
| 14 | [Как добавить сущность](docs/14-howto-new-entity.md) | Пошаговое руководство для новых справочников/документов |
| 18 | [Фильтрация](docs/18-filtering.md) | Metadata-driven фильтрация списков |
| 21 | [API расширений](docs/21-extension-api.md) | Справочник модели расширений |

Начните здесь → [Навигатор документации](docs/ROUTER.md)

---

## 🧪 Разработка

### Цели Makefile

```bash
make build            # Сборка бинарников server + worker
make server           # Запуск бэкенд dev-сервера
make frontend         # Запуск фронтенд dev-сервера (Next.js + Turbopack)
make test             # Запуск юнит-тестов
make lint             # Запуск линтеров Go + фронтенд
make typecheck        # TypeScript проверка типов (npx tsc --noEmit)
make migrate          # Запуск миграций БД для всех тенантов
make seed             # Заполнение начальными данными для разработки
make check            # Полная CI-проверка: build + lint + typecheck + test
make check-all        # Полная проверка включая расширения
```

### CLI управления тенантами

```bash
go run cmd/tenant/main.go create --slug=acme --name="ACME Corp"
go run cmd/tenant/main.go list
go run cmd/tenant/main.go migrate              # все тенанты
go run cmd/tenant/main.go migrate --id <uuid>  # один тенант
go run cmd/tenant/main.go suspend <uuid>
go run cmd/tenant/main.go activate <uuid>
```

---

## 🤝 Участие в разработке

Мы приветствуем участие в проекте! Ознакомьтесь с [CONTRIBUTING.md](CONTRIBUTING.md):

- Инструкции по настройке окружения
- Гайдлайны кода (Go и TypeScript)
- Процесс создания PR и формат коммитов
- Процедура подписания CLA

**Перед первым PR** прочтите и примите [Лицензионное соглашение участника](CLA.md).

---

## 📄 Лицензия

Metapus использует двойную лицензию:

- **Community Edition** — [GNU Affero General Public License v3.0](LICENSE) (AGPL-3.0)
- **Коммерческая лицензия** — доступна для организаций, которые не могут соблюдать условия AGPL

Copyright © 2025-present [Aleksandr Bogatiuk](mailto:alex.bogatiuk@gmail.com)
