# Правила именования (Naming Conventions)

> Единые правила именования для БД, Go-кода, REST API и файлов. Нарушение = отклонение.

---

## Общие принципы

| Правило | Пример | Причина |
|---------|--------|---------|
| Только латиница a-z, 0-9, _ | `cat_counterparties` | PostgreSQL case folding |
| snake_case в БД | `counterparty_id` | Соответствие БД ↔ Go теги |
| CamelCase в Go коде | `Counterparty`, `GoodsReceipt` | Go-конвенция |
| kebab-case в REST URL | `/counterparties`, `/goods-receipt` | Читаемость |
| Макс. длина имени таблицы — 58 символов | | Место под `_items`, `_balances` |

---

## Префиксы таблиц

| Префикс | Тип объекта | Примеры |
|---------|-------------|---------|
| `cat_` | Справочники | `cat_counterparties`, `cat_nomenclature`, `cat_warehouses`, `cat_vat_rates`, `cat_contracts` |
| `doc_` | Документы (шапка) | `doc_invoice`, `doc_goods_receipt` |
| `doc_` | Табличные части | `doc_invoice_items`, `doc_goods_receipt_goods` |
| `reg_` | Регистры накопления (движения) | `reg_stock_movements` |
| `reg_` | Регистры накопления (остатки) | `reg_stock_balances` |
| `reg_` | Регистры накопления (обороты) | `reg_stock_turnovers` |
| `reg_` | Регистры сведений | `reg_currency_rates_info`, `reg_barcodes_info` |
| `const_` | Константы | `const_company_settings` |
| `sys_` | Системные таблицы | `sys_sequences`, `sys_outbox`, `sys_audit`, `sys_sessions` |
| `auth_` | Аутентификация | `auth_users`, `auth_roles`, `auth_permissions`, `auth_refresh_tokens` |

---

## REST API маршруты

| Тип объекта | Базовый путь | Пример |
|-------------|--------------|--------|
| Справочники | `/api/v1/catalog/{name}` | `GET /api/v1/catalog/counterparties` |
| Документы | `/api/v1/document/{name}` | `POST /api/v1/document/goods-receipt` |
| Регистры накопления | `/api/v1/registers/{name}` | `GET /api/v1/registers/stock/movements` |
| Регистры сведений | `/api/v1/registers/{name}` | `GET /api/v1/registers/currency-rates/slices` |
| Константы | `/api/v1/constants/{name}` | `GET /api/v1/constants/company-settings` |
| Метаданные | `/api/v1/meta/{type}/{name}` | `GET /api/v1/meta/layouts/invoice` |

### Специальные операции документов

| Операция | Endpoint | Метод |
|----------|----------|-------|
| Проведение | `/api/v1/document/{name}/{id}/post` | POST |
| Отмена проведения | `/api/v1/document/{name}/{id}/unpost` | POST |
| Копирование | `/api/v1/document/{name}/{id}/copy` | POST |
| Печать | `/api/v1/document/{name}/{id}/print?form=...` | GET |

---

## Go-пакеты и файлы

| Элемент | Правило | Пример |
|---------|---------|--------|
| Пакет | snake_case | `bank_account`, `goods_receipt` |
| Файлы | snake_case.go | `model.go`, `service.go`, `repo.go` |
| Struct | CamelCase | `BankAccount`, `GoodsReceipt` |
| Поле struct | CamelCase | `AccountNumber`, `WarehouseID` |
| db тег | snake_case | `db:"account_number"` |
| json тег | camelCase | `json:"accountNumber"` |
| Интерфейс | CamelCase | `Repository`, `Generator` |
| Константа | CamelCase | `OperationTypeSale` |
| Переменная | camelCase | `counterpartyRepo` |

---

## Нумерация миграций

| Диапазон | Назначение |
|----------|-----------|
| `00001–00009` | Системные таблицы (`sys_*`), базовые функции |
| `00010–00010` | Аутентификация (`auth_*`, `users`, `roles`, `permissions`) |
| `00011–00018` | Справочники (`cat_*`) |
| `00020–00029` | Документы (`doc_*`) + seed-данные |
| `00030–00039` | Регистры (`reg_*`) + CDC |

---

## Permission strings

Формат: `{entity_type}:{entity_name}:{action}`

```
catalog:warehouse:read
catalog:counterparty:create
document:goods_receipt:post
register:stock:read
```

---

## Связанные документы

- [14-howto-new-entity.md](14-howto-new-entity.md) — применение конвенций при создании сущности
- [17-development-rules.md](17-development-rules.md) — правила миграций
