# Архитектура Metapus

> Clean Architecture с правилом зависимостей + metadata-driven подход, где Go-структуры являются единственным источником истины.

---

## Слои приложения

```
┌─────────────────────────────────────────────────┐
│  cmd/*          — Composition Root (DI, запуск) │
├─────────────────────────────────────────────────┤
│  infrastructure — Адаптеры (HTTP, Postgres,     │
│                   cache, worker)                 │
├─────────────────────────────────────────────────┤
│  domain         — Бизнес-логика и use cases     │
│                   (сервисы, интерфейсы репо)     │
├─────────────────────────────────────────────────┤
│  core           — Фундаментальные типы и        │
│                   политики (entity, ошибки, tx)  │
└─────────────────────────────────────────────────┘
```

**Правило зависимости:** внутренние слои **не импортируют** внешние. Domain ничего не знает о HTTP/Postgres.

### `internal/core` — Ядро
Фундаментальные типы и политики:
- **entity/** — BaseEntity, BaseCatalog, BaseDocument, HierarchicalCatalog, трейты (CurrencyAware)
- **apperror/** — структурированные ошибки (RFC 7807)
- **id/** — UUIDv7 генерация
- **tenant/** — типы и context-утилиты для multi-tenancy
- **tx/** — интерфейс Transaction Manager
- **security/** — AccessScope, JWT Claims
- **types/** — MinorUnits, Quantity, Money

### `internal/domain` — Бизнес-логика
Bounded contexts как подкаталоги:
- **catalogs/** — справочники (counterparty, nomenclature, warehouse, currency, unit, vat_rate, contract)
- **documents/** — документы (goods_receipt, invoice, stock_transfer)
- **registers/** — регистры накопления и сведений (stock, currency_rates, barcodes)
- **reports/** — отчёты (stock_balance, sales_turnover)
- **posting/** — движок проведения документов
- **workflow/** — бизнес-процессы

Каждый bounded context содержит: `model.go`, `repo.go`, `service.go`, опционально `hooks.go`.

### `internal/infrastructure` — Реализация
Адаптеры и драйверы:
- **http/v1/** — Gin router, handlers, DTOs, middleware
- **storage/postgres/** — pgx репозитории, TxManager, outbox
- **cache/** — in-memory кэш схем
- **worker/** — фоновые задачи (outbox relay, audit cleaner)

### `cmd/*` — Composition Root
Точки входа, DI руками, конфиг, graceful shutdown:
- **server/** — REST API сервер
- **worker/** — multi-tenant воркер фоновых задач
- **tenant/** — CLI управления тенантами
- **seed/** — seed данных для разработки

---

## Иерархия объектов метаданных

```
ГЛОБАЛЬНЫЕ НАСТРОЙКИ
├── Константы (Constants) — условно-постоянная информация (название орг., базовая валюта)
└── Перечисления (Enums) — закрытые списки значений (вид операции, юр. статус)
         │
         ▼
НОРМАТИВНО-СПРАВОЧНЫЕ ДАННЫЕ
├── Справочники (Catalogs) — сущности реального мира (номенклатура, контрагенты)
└── Планы видов характеристик — дополнительные реквизиты (EAV pattern)
         │
         ▼
ТРАНЗАКЦИОННЫЕ ДАННЫЕ
├── Документы (Documents) — факты хозяйственных операций (шапка + табличные части)
└── Журналы документов (Journals) — полиморфные списки (Views)
         │
         ▼
УЧЕТНЫЙ ДВИЖОК (STATE)
├── Регистры сведений (Information) — периодические/статические данные
├── Регистры накопления (Accumulation) — movements + balances + turnovers
└── Регистры бухгалтерии (Accounting) — план счетов, двойная запись, субконто
         │
         ▼
АНАЛИТИКА И АВТОМАТИЗАЦИЯ
├── Отчёты (Reports) — только чтение + экспорт
├── Обработки (Processors) — сценарии без сохранения
└── Бизнес-процессы (Workflows) — State Machine + Tasks
```

---

## Metadata-driven подход

- **«Code is metadata»** — метаданные системы выражаются компилируемыми Go-структурами
- Теги `db/json` — часть контракта, связывающего слой домена/DTO/хранилища
- Расширение под конкретный проект/клиента: хуки/плагины/интерфейсы и регистрационные механизмы, а не ветвления по флагам

---

## Типы справочников (Catalogs)

- **Плоский (Flat)** — без иерархии (контрагенты, валюты, единицы измерения)
- **Иерархический (Tree)** — папки и элементы с materialized path (номенклатура, подразделения)
- **Подчинённый (Subordinate)** — привязка к владельцу (договоры → контрагенты)

## Структура документа

- **Шапка (Header)** — ID, номер, дата, организация, контрагент, склад, posted, version, attributes
- **Табличные части (Table Parts)** — отдельные таблицы (`doc_xxx_goods`, `doc_xxx_services`)
- **Механизм проведения** — черновик (записан) → проведён (породил движения в регистрах)

## Регистры накопления

- **Таблица движений** (`reg_*_movements`) — immutable ledger, партиции по месяцам
- **Таблица остатков** (`reg_*_balances`) — горячий кэш, O(1) получение остатков, `CHECK (quantity >= 0)`
- **Delta-обновление** — при проведении/перепроведении считается дельта old/new

---

## Связанные документы

- [03-project-structure.md](03-project-structure.md) — файловое дерево проекта
- [04-core-layer.md](04-core-layer.md) — детали Core слоя
- [05-domain-layer.md](05-domain-layer.md) — детали Domain слоя
- [06-infrastructure-layer.md](06-infrastructure-layer.md) — детали Infrastructure слоя
