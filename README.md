<p align="center">
  <h1 align="center">Metapus</h1>
  <p align="center">
    <strong>ERP-платформа на Go и Next.js</strong>
  </p>
  <p align="center">
    <a href="#быстрый-старт">Быстрый старт</a> ·
    <a href="#архитектура">Архитектура</a> ·
    <a href="#документация">Документация</a> ·
    <a href="#разработка">Разработка</a>
  </p>
  <p align="center">
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0-blue.svg" alt="License: AGPL-3.0"></a>
    <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white" alt="Go 1.25"></a>
    <a href="https://nextjs.org/"><img src="https://img.shields.io/badge/Next.js-16-000000?logo=next.js&logoColor=white" alt="Next.js 16"></a>
    <a href="https://www.postgresql.org/"><img src="https://img.shields.io/badge/PostgreSQL-17-4169E1?logo=postgresql&logoColor=white" alt="PostgreSQL 17"></a>
  </p>
</p>

---

Metapus — open-source ERP-платформа для построения корпоративных систем. Базируется на принципах Clean Architecture, строгой типизации и физической изоляции данных.

## Архитектурные решения

| Подход | Реализация в Metapus |
|---|---|
| Метаданные | **Code is Metadata**: Go-структуры выступают единым источником истины. |
| Типизация | **Go Generics**: Проверка типов на этапе компиляции, отказ от runtime-рефлексии. |
| Транзакции | **Resource ordering**: Advisory locks предотвращают взаимные блокировки. |
| Регистры | **Immutable Ledger**: Обновление остатков через триггеры БД. `UPDATE` движений запрещён. |
| Изоляция | **Database-per-Tenant**: Раздельные базы данных для клиентов. Фильтрация по `tenant_id` отсутствует. |

## Характеристики

### Backend
- **Стек**: Go 1.25, pgx, squirrel, Gin.
- **Изоляция слоёв**: Core → Domain → Infrastructure. Внутренние слои не импортируют внешние.
- **Проведение документов**: Паттерн Visitor, детерминированный сбор движений, batch-запись.
- **Безопасность**: JWT, RBAC, RLS (строки), FLS (поля), CEL-политики.
- **Расширяемость**: Регистрация справочников, документов и регистров через API расширений без форков ядра.

### Frontend
- **Стек**: Next.js 16 (App Router), React 19, TypeScript, Tailwind CSS, shadcn/ui.
- **UI-генерация**: Списки, формы и фильтры строятся на основе метаданных API бэкенда.
- **Состояние**: URL-driven (навигация, фильтры), Zustand (авторизация, метаданные).

### Инфраструктура
- **Развёртывание**: Docker, docker-compose, GHCR.
- **База данных**: PostgreSQL 17, UUIDv7, goose.
- **Мониторинг**: Zap (структурированное логирование), X-Request-ID.

---

## Быстрый старт

### Требования
- Go 1.25+
- Node.js 20+
- PostgreSQL 17+ (или Docker)

### Инициализация и запуск

```bash
# Клонирование репозитория
git clone https://github.com/alex-bogatiuk/metapus.git
cd metapus

# Запуск СУБД (при использовании Docker)
docker compose up postgres -d

# Настройка окружения
export META_DATABASE_URL="postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable"
export TENANT_DB_USER="metapus"
export TENANT_DB_PASSWORD="metapus"
export JWT_SECRET="dev-secret"

# Создание тенанта и применение миграций
go run cmd/tenant/main.go create --slug=default --name="Dev"
go run cmd/tenant/main.go migrate

# Запуск API-сервера
go run ./cmd/server

# Запуск фронтенда (в отдельном терминале)
cd frontend
npm install
npm run dev
```

- API: `http://localhost:8080`
- Интерфейс: `http://localhost:3000`

---

## Структура проекта

```text
metapus/
├── cmd/               # Точки входа (server, worker, tenant, seed)
├── internal/
│   ├── core/          # Базовые типы, ошибки, политики
│   ├── domain/        # Бизнес-логика, сервисы, модели
│   └── infrastructure/# Адаптеры (HTTP, Postgres)
├── pkg/               # Утилиты (logger, numerator, decimal)
├── db/                # Миграции баз данных
├── extensions/        # Пользовательские модули
├── frontend/          # Next.js приложение
└── docs/              # Техническая документация
```

## Документация

Полная техническая документация находится в директории [`docs/`](docs/).
Точка входа: [Навигатор документации](docs/ROUTER.md).

## Разработка

Основные команды сборки и проверки (`Makefile`):

- `make build` — Сборка бинарных файлов (server, worker).
- `make server` — Запуск бэкенда.
- `make frontend` — Запуск фронтенда.
- `make test` — Запуск юнит-тестов.
- `make lint` — Проверка кода (Go, TypeScript).
- `make check` — Полная CI-проверка (build, lint, typecheck, test).

## 🤝 Участие в разработке

Мы приветствуем участие в проекте! Ознакомьтесь с [CONTRIBUTING.md](CONTRIBUTING.md):

- Инструкции по настройке окружения
- Гайдлайны кода (Go и TypeScript)
- Процесс создания PR и формат коммитов
- Процедура подписания CLA

**Перед первым PR** прочтите и примите [Лицензионное соглашение участника](CLA.md).

---

## Лицензия

- **Community Edition**: [AGPL-3.0](LICENSE).
- **Коммерческая лицензия**: Доступна по запросу.

Copyright © 2026-present [Aleksandr Bogatiuk](mailto:alex.bogatiuk@gmail.com)
