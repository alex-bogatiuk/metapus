# Правила именования (Naming Conventions)

> **TL;DR:** Единый стандарт именования объектов БД, API-маршрутов и Go-кода. Нарушение стандарта = отклонение PR.

> **Тип:** Reference
> **Аудитория:** Developer

---

## 1. Базовые принципы

| Слой | Стиль | Пример | Причина |
|------|-------|--------|---------|
| **База Данных** | `snake_case` | `cat_counterparties` | PostgreSQL case folding |
| **Go Code** | `CamelCase` | `Counterparty` | Go convention |
| **Go JSON Tags** | `camelCase` | `counterpartyId` | JSON standard |
| **REST URL** | `kebab-case` | `/goods-receipt` | Удобочитаемость URL |

> [!WARNING]
> Максимальная длина имени таблицы в PostgreSQL — 63 символа. Мы используем ограничение в **58 символов**, чтобы оставить место для системных суффиксов (например, `_balances` или `_movements`).

## 2. База данных: Префиксы

Все таблицы обязаны иметь префикс, указывающий на их подсистему.

| Префикс | Подсистема | Пример |
|---------|------------|--------|
| `cat_` | Справочники (Catalogs) | `cat_counterparties`, `cat_warehouses` |
| `doc_` | Документы (Шапка) | `doc_invoice`, `doc_goods_receipt` |
| `doc_*_items` | Табличные части | `doc_invoice_items` |
| `reg_*_movements` | Движения регистров | `reg_stock_movements` |
| `reg_*_balances` | Остатки регистров | `reg_stock_balances` |
| `reg_*_info` | Регистры сведений | `reg_currency_rates_info` |
| `sys_` | Системные таблицы | `sys_outbox`, `sys_idempotency` |
| `auth_` | Безопасность | `auth_users`, `auth_roles` |

## 3. REST API маршруты

| Тип объекта | Базовый путь | Пример |
|-------------|--------------|--------|
| Справочник | `/api/v1/catalog/{name}` | `GET /api/v1/catalog/counterparties` |
| Документ | `/api/v1/document/{name}` | `POST /api/v1/document/goods-receipt` |
| Движения регистра| `/api/v1/registers/{name}`| `GET /api/v1/registers/stock/movements` |
| Срезы сведений | `/api/v1/registers/{name}`| `GET /api/v1/registers/currency-rates/slices` |
| Метаданные | `/api/v1/meta/{type}/{name}`| `GET /api/v1/meta/layouts/invoice` |

### Специальные операции (RPC-style)
Для не-CRUD операций документов используем суффиксы-глаголы.

| Действие | Маршрут | Метод HTTP |
|----------|---------|------------|
| Проведение | `.../{id}/post` | `POST` |
| Отмена проведения | `.../{id}/unpost` | `POST` |
| Копирование | `.../{id}/copy` | `POST` |

## 4. Go Файлы

| Тип файла | Правило именования | Пример |
|-----------|--------------------|--------|
| Структура таблицы | `model.go` | `counterparty/model.go` |
| Репозиторий | `repo.go` | `counterparty/repo.go` |
| Бизнес-логика | `service.go` | `counterparty/service.go` |
| Тесты | `*_test.go` | `service_test.go` |

## 5. Внешние ключи (FK) — строгое правило

**Принцип:** FK-поле обязано называться по имени таблицы, на которую ссылается. Отображаемое имя (label) — задача UI-слоя (`meta:"label:..."`, фронтенд), а не базы данных.

| Правило | Пример | Пояснение |
|---------|--------|-----------|
| FK = имя таблицы | `nomenclature_id` → `cat_nomenclatures` | Однозначная связь |
| Композитное имя при нескольких ролях | `sender_counterparty_id`, `receiver_counterparty_id` | Суффикс `_counterparty_id` сохраняет связь с таблицей |
| REFERENCES обязателен | `counterparty_id UUID NOT NULL REFERENCES cat_counterparties(id)` | DDL-уровень гарантии |
| Label — это UI, не БД | DB: `counterparty_id`, Go: `meta:"label:Поставщик"` | Роль определяется типом документа |

> [!CAUTION]
> Запрещены «бытовые» синонимы, не совпадающие с именем таблицы:
> - ❌ `product_id` → `cat_nomenclatures` (справочник называется «Номенклатура», не «Продукты»)
> - ❌ `supplier_id` → `cat_counterparties` (используйте `counterparty_id`)
> - ❌ `customer_id` → `cat_counterparties` (используйте `counterparty_id`)

## 6. Именование переменных и полей (Go Variables & Fields)

**Принцип:** Имя переменной должно быть настолько коротким, насколько позволяет её область видимости (scope).

| Сущность | Тип / Интерфейс | Поле структуры (DI) | Локальная переменная | Пример (Неправильно) |
|----------|-----------------|---------------------|----------------------|----------------------|
| **Сервис** | `Service` | `...Svc` | `svc` | `authService`, `srv` |
| **Репозиторий** | `Repository` | `...Repo` | `repo` | `walletRepository` |
| **Транзакции** | `tx.Manager` | `txManager` | `txm` | `transactionManager` |
| **Конфигурация** | `Config` | `...Cfg` | `cfg` | `configuration`, `conf` |

**Базовые примитивы (Универсальный стандарт):**
- `context.Context` → **`ctx`**
- `error` → **`err`**
- `sync.Mutex` → **`mu`**
- `sync.WaitGroup` → **`wg`**
- `http.Request`, `http.ResponseWriter` → **`req`** (или `r`), **`w`**
- `*gin.Context` → **`c`**

**Методы-ресиверы (Receivers):**
Никогда не используйте `this`, `self` или `me`. Используйте 1-2 буквы, производные от типа:
```go
// ПРАВИЛЬНО
func (ep *EventProcessor) Process(...) 

// НЕПРАВИЛЬНО
func (this *EventProcessor) Process(...)
```
