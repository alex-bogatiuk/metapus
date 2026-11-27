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
    <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white" alt="Go 1.27"></a>
    <a href="https://nextjs.org/"><img src="https://img.shields.io/badge/Next.js-16-000000?logo=next.js&logoColor=white" alt="Next.js 16"></a>
    <a href="https://www.postgresql.org/"><img src="https://img.shields.io/badge/PostgreSQL-17-4169E1?logo=postgresql&logoColor=white" alt="PostgreSQL 17"></a>
  </p>
</p>

---

Metapus — open-source ERP-платформа для построения корпоративных систем. Базируется на принципах Clean Architecture, строгой типизации и физической изоляции данных.

## Платформа и конфигурации

Репозиторий содержит платформу и отраслевые конфигурации поверх неё. Конфигурация добавляет свои справочники, документы, регистры, миграции и страницы фронтенда. У каждой есть свои команды `cmd/<имя>-server`, `-worker`, `-tenant` и `-seed`.

| Конфигурация | Что это | Код | Документация |
|---|---|---|---|
| `none` | Платформа без отраслевой конфигурации, плюс клиентские расширения | [`internal/configuration/none`](internal/configuration/none), `cmd/server` | — |
| `crypto` | Криптопроцессинг: приём платежей, учёт, свипы, вывод, AML, подписание через Vault | [`internal/configuration/crypto`](internal/configuration/crypto), `cmd/crypto-*` | [docs/crypto](docs/crypto), [обзор подсистемы](docs/systems/crypto-processing.md) |
| `trade` | Базовая конфигурация «Торговля и сервис»: номенклатура, склады, заказы с резервами, накладные, цены, себестоимость, закрытие месяца | [`configurations/trade`](configurations/trade), `cmd/trade-*` | [docs/trade](docs/trade/README.md) |
| `autoparts` | Надстройка «Автозапчасти» над `trade` | [`configurations/autoparts`](configurations/autoparts), `cmd/autoparts-*` | [docs/autoparts](docs/autoparts/README.md) |

Приложение — три слоя: платформа, базовая конфигурация и необязательная надстройка над ней ([слои приложения](docs/systems/configuration-layers.md)). Конфигурации не зависят друг от друга, надстройка видит только свою базу, и все видят платформу только через публичный API [`pkg/sdk`](docs/sdk/README.md). Эти границы проверяют тесты в [`internal/architecture`](internal/architecture).

## Архитектура

| Подход | Реализация в Metapus |
|---|---|
| Метаданные | **Code is Metadata**: Go-структуры выступают единым источником истины. |
| Типизация | **Go Generics**: Типизированные CRUD-компоненты; рефлексия используется для метаданных и маппинга `db`-тегов. |
| Транзакции | **Блокировки**: проведение берёт advisory lock на документ; репозитории блокируют строки в детерминированном порядке, чтобы избежать взаимных блокировок. |
| Регистры | **Immutable Ledger**: Обновление остатков через триггеры БД. Финансовые crypto movements запрещают `UPDATE`/`DELETE`; исправления добавляют компенсирующие записи. |
| Изоляция | **Database-per-Tenant**: Раздельные базы данных для клиентов. Фильтрация по `tenant_id` отсутствует. |

## Характеристики

### Backend
- **Стек**: Go 1.27, pgx, squirrel, Gin.
- **Слои**: `core` → `domain` → `application` → `infrastructure`; `bootstrap` собирает их в процессы. Внутренние слои не импортируют внешние.
- **Проведение документов**: Паттерн Visitor, детерминированный сбор движений, batch-запись.
- **Безопасность**: JWT, MFA, RBAC, RLS (строки), FLS (поля), CEL-политики.
- **Расширяемость**: Конфигурации и клиентские расширения регистрируют справочники, документы и регистры через `pkg/sdk` без форков ядра.

### Frontend
- **Стек**: Next.js 16 (App Router), React 19, TypeScript, Tailwind CSS, shadcn/ui.
- **Конфигурации**: `npm run configure -- <имя>` собирает `app/` из платформы и страниц выбранной конфигурации (`frontend/configuration/<имя>`).
- **UI-генерация**: Списки, формы и фильтры строятся на основе метаданных API бэкенда.
- **Состояние**: URL-driven (навигация, фильтры), Zustand (авторизация, метаданные).

### Инфраструктура
- **Развёртывание**: Docker, docker-compose, GHCR ([`deployments/`](deployments), [`single-tenant-deployment/`](single-tenant-deployment)).
- **База данных**: PostgreSQL 17, UUIDv7, goose.
- **Секреты и подписание**: HashiCorp Vault с плагинами подписи TRON и EVM ([`vault-sign-plugins/`](vault-sign-plugins)).
- **Мониторинг**: Zap (структурированное логирование), X-Request-ID.

---

## Быстрый старт

Ниже — локальный запуск конфигурации `crypto`: на неё настроены bootstrap-скрипты и цели `make server`/`make frontend`.

### Требования
- Go 1.27+
- Node.js 20.9+ и npm
- PowerShell 7 (`pwsh`) — скрипты `scripts/dev` описаны для Windows
- Docker: PostgreSQL поднимается из `docker-compose.yml`, Vault — отдельным контейнером `metapus-vault`
- Свободные порты 5432, 8080, 8200 и 3000
- Тестовые данные сети TRON Nile: отдельная mnemonic, внешний адрес получателя, желательно API key TronGrid. Не используйте mainnet mnemonic.

### Инициализация и запуск

```bash
git clone https://github.com/alex-bogatiuk/metapus.git
cd metapus
```

Mnemonic передайте параметром `-Mnemonic` или положите в `.env.crypto.local` (файл игнорируется git):

```dotenv
TRON_DEV_MNEMONIC=<NILE TEST MNEMONIC>
TRON_TEST_API_KEY=<OPTIONAL>
TRON_WITHDRAWAL_TEST_ADDRESS=<EXTERNAL NILE ADDRESS>
```

```bash
# PostgreSQL, Vault с плагином, tenant, миграции, seed, ключи и политики подписания.
# Пишет .env.crypto.local и frontend/.env.local.
pwsh ./scripts/dev/crypto-setup.ps1

# API-сервер
pwsh ./scripts/dev/crypto-run-server.ps1

# Воркер: watchers, свипы, вывод (в отдельном терминале)
pwsh ./scripts/dev/crypto-run-worker.ps1

# Фронтенд (в отдельном терминале)
cd frontend
npm install
npm run configure -- crypto
npm run dev
```

- API: `http://localhost:8080`
- Интерфейс: `http://localhost:3000`
- Vault: `http://127.0.0.1:8200`

Полная инструкция — финансирование hot wallet, первая авторизация и MFA, активация пула — в [docs/crypto/crypto-environment-setup.md](docs/crypto/crypto-environment-setup.md).

### Другие конфигурации

Платформа без конфигурации (`cmd/server`, `cmd/tenant`, `cmd/seed`), `trade` (`cmd/trade-*`) и `trade` с надстройкой `autoparts` (`cmd/autoparts-*`) запускаются напрямую через `go run`. Им нужны переменные `META_DATABASE_URL`, `TENANT_DB_USER`, `TENANT_DB_PASSWORD` и `JWT_SECRET`. Порядок команд — в [docs/trade/README.md](docs/trade/README.md#запуск) и [docs/autoparts/README.md](docs/autoparts/README.md#запуск).

---

## Структура проекта

```text
metapus/
├── cmd/                    # Точки входа: server/worker/tenant/seed платформы и каждой конфигурации,
│                           # а также служебные (integration-test, http-contracts, pdf-renderer, scaffold, AML…)
├── internal/
│   ├── core/               # Базовые типы, ошибки, политики безопасности (CEL)
│   ├── domain/             # Бизнес-логика платформы, движок проведения
│   ├── application/        # Сценарии: автоматизация, команды, интеграции
│   ├── infrastructure/     # Адаптеры (HTTP, Postgres)
│   ├── platform/           # Контракты платформы для конфигураций
│   ├── metadata/           # Реестр и инспектор метаданных
│   ├── bootstrap/          # Сборка server, worker и tenant-команд
│   ├── configuration/      # Контракт конфигураций, none и crypto
│   └── architecture/       # Тесты архитектурных границ
├── configurations/         # Базовые конфигурации и надстройки, которые могут импортировать другие репозитории: trade, autoparts
├── pkg/
│   ├── sdk/                # Публичный API платформы для конфигураций и расширений
│   └── logger/
├── db/
│   ├── migrations/         # Миграции платформы
│   ├── configuration/      # Миграции конфигураций
│   └── extensions/         # Миграции клиентских расширений
├── extensions/             # Точка регистрации клиентских модулей (пока пустая)
├── sdk/                    # Клиентские SDK мерчантов: Go, Python, TypeScript
├── api/                    # OpenAPI публичного API мерчантов и фикстуры webhooks
├── frontend/               # Next.js приложение; frontend/configuration — страницы конфигураций
├── vault-sign-plugins/     # Исходники плагинов подписи Vault (TRON, EVM); сборка — в vault-plugins/
├── deployments/, single-tenant-deployment/  # Compose-развёртывания и тестовые стенды
├── scripts/                # dev-скрипты и CI
└── docs/                   # Техническая документация
```

## Документация

Полная техническая документация находится в директории [`docs/`](docs/).
Точка входа: [Навигатор документации](docs/ROUTER.md).

## Разработка

Основные команды (`Makefile`):

| Команда | Что делает |
|---|---|
| `make build` | Собирает в `bin/` server, worker, tenant и seed платформы, `crypto`, `trade` и `autoparts` |
| `make server` | Запускает API-сервер `crypto` (`go run ./cmd/crypto-server`) |
| `make frontend` | Собирает фронтенд для `crypto` и запускает dev-сервер |
| `make migrate` / `make seed` | Миграции всех тенантов `crypto` / seed `crypto` |
| `make test` | Юнит-тесты (`go test -short -race ./...`) |
| `make test-integration` | Интеграционные тесты в новой базе явно заданного тестового кластера PostgreSQL |
| `make lint` | golangci-lint и ESLint |
| `make check` | build, lint, typecheck, юнит-тесты и проверка сгенерированных HTTP-контрактов |
| `make check-configurations` | Сборка Go-команд и фронтенда для каждой конфигурации (`none`, `crypto`, `trade`, `autoparts`) |
| `make check-all` | `check`, проверка расширений и `check-configurations` |
| `make generate-http-contracts` | Перегенерирует HTTP-контракты |

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
