# Metapus Platform — Полное Руководство по Проектированию платформы для построения бизнес приложений на golang.

# Часть I. Философия и Архитектурные Принципы

## 1. Введение

### 1.1. Назначение документа

Данный документ является **единственным источником истины** для проектирования и разработки платформы Metapus — современной альтернативы классическим ERP-системам (1С:Предприятие, Odoo, ERPNext), работающей исключительно в веб-браузере.

### 1.2. Целевая аудитория

- **Архитекторы:** Принятие стратегических решений о структуре системы
- **Backend-разработчики:** Реализация бизнес-логики на Go
- **Frontend-разработчики:** Построение UI на основе метаданных
- **DevOps-инженеры:** Развертывание и эксплуатация

### 1.3. Ключевые характеристики платформы

| Характеристика | Значение |
|----------------|----------|
| Backend | Go (Golang) + Gin Framework |
| Frontend | Next.js + React + TypeScript + shadcn/ui |
| База данных | PostgreSQL 16+ с отдельными таблицами для каждого типа |
| API | RESTful с версионированием (/api/v1, /api/v2) |
| Архитектура | Clean Architecture + Vertical Slices |
| Multi-tenancy | **Database-per-Tenant** (отдельная БД для каждого клиента) |
| Аутентификация | JWT (Access + Refresh tokens) |

---

## 2. Манифест Платформы

### 2.1. Основные принципы

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        METAPUS MANIFESTO                                 │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  1. CODE IS METADATA                                                     │
│     Структура данных (struct) — единственный источник истины.           │
│     Мы НЕ храним описание полей в JSON в базе данных.                   │
│                                                                          │
│  2. EXPLICIT OVER IMPLICIT                                               │
│     Все связи, блокировки и транзакции управляются явно в коде.         │
│     Никакой "магии" ORM.                                                │
│                                                                          │
│  3. PERFORMANCE FIRST                                                    │
│     Нативные драйверы (pgx), пулы соединений, MinorUnits/Quantity для точности. │
│                                                                          │
│  4. LAYERED ISOLATION                                                    │
│     Domain ничего не знает о Infrastructure и Presentation.              │
│                                                                          │
│  5. IMMUTABLE LEDGER                                                     │
│     Мы НИКОГДА не делаем UPDATE движений регистров.                     │
│     При изменении документа — новая версия движений.                    │
│                                                                          │
│  6. DATABASE-PER-TENANT                                                  │
│     Multi-tenancy через отдельные PostgreSQL базы данных.               │
│     Физическая изоляция, без фильтрации по tenant в запросах.           │
│                                                                          │
│  7. NAMING IS CONTRACT                                                   │
│     Все имена подчиняются единому стандарту. Нарушение = отклонение.   │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### 2.2. Сравнение с аналогами

| Характеристика | 1С:Предприятие | Odoo | ERPNext | **Metapus** |
|----------------|----------------|------|---------|-------------|
| Язык бэкенда | 1С (проприетарный) | Python | Python | **Go** |
| Типизация | Слабая | Динамическая | Динамическая | **Статическая строгая** |
| Метаданные | Runtime Reflection | ORM (Odoo) | DocType (Frappe) | **Compile-time structs** |
| Производительность | Средняя | Средняя | Средняя | **Высокая** |
| Multi-tenancy | Отдельные базы | Одна общая БД | Отдельные сайты | **Database-per-Tenant** |
| Блокировки | Managed Locks | ORM locks | DB-level | **Explicit FOR UPDATE** |
| Транзакции | Автоматические | Декоратор @api.multi | Неявные | **Явный Transaction Manager** |

---

## 3. Архитектура Метаданных

### 3.1. Иерархия объектов метаданных

```
┌─────────────────────────────────────────────────────────────────────────┐
│                      METADATA OBJECT HIERARCHY                           │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                    ГЛОБАЛЬНЫЕ НАСТРОЙКИ                          │    │
│  │  ┌──────────────────┐    ┌──────────────────────────────────┐   │    │
│  │  │   КОНСТАНТЫ      │    │         ПЕРЕЧИСЛЕНИЯ             │   │    │
│  │  │   (Constants)    │    │         (Enums)                  │   │    │
│  │  │                  │    │                                  │   │    │
│  │  │  • Название орг. │    │  • Вид операции                 │   │    │
│  │  │  • Базовая валюта│    │  • Юрид. статус                 │   │    │
│  │  │  • Учет. политика│    │  • Пол                          │   │    │
│  │  └──────────────────┘    └──────────────────────────────────┘   │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                 НОРМАТИВНО-СПРАВОЧНЫЕ ДАННЫЕ                     │    │
│  │  ┌──────────────────┐    ┌──────────────────────────────────┐   │    │
│  │  │   СПРАВОЧНИКИ    │    │   ПЛАНЫ ВИДОВ ХАРАКТЕРИСТИК      │   │    │
│  │  │   (Catalogs)     │    │   (Characteristic Types)         │   │    │
│  │  │                  │    │                                  │   │    │
│  │  │  • Номенклатура  │    │  Механизм дополнительных         │   │    │
│  │  │  • Контрагенты   │    │  реквизитов (EAV pattern)        │   │    │
│  │  │  • Склады        │    │                                  │   │    │
│  │  │  • Сотрудники    │    │                                  │   │    │
│  │  └──────────────────┘    └──────────────────────────────────┘   │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                    ТРАНЗАКЦИОННЫЕ ДАННЫЕ                         │    │
│  │  ┌──────────────────┐    ┌──────────────────────────────────┐   │    │
│  │  │    ДОКУМЕНТЫ     │    │      ЖУРНАЛЫ ДОКУМЕНТОВ          │   │    │
│  │  │   (Documents)    │    │      (Journals)                  │   │    │
│  │  │                  │    │                                  │   │    │
│  │  │  • Шапка         │    │  Полиморфные списки              │   │    │
│  │  │  • Табл. части   │    │  документов разных типов         │   │    │
│  │  │  • Проведение    │    │  (Views, не хранят данные)       │   │    │
│  │  └──────────────────┘    └──────────────────────────────────┘   │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                    УЧЕТНЫЙ ДВИЖОК (STATE)                        │    │
│  │                                                                   │    │
│  │  ┌────────────────┐ ┌────────────────┐ ┌────────────────────┐   │    │
│  │  │ РЕГ. СВЕДЕНИЙ  │ │ РЕГ. НАКОПЛ.   │ │ РЕГ. БУХГАЛТЕРИИ   │   │    │
│  │  │ (Information)  │ │ (Accumulation) │ │ (Accounting)       │   │    │
│  │  │                │ │                │ │                    │   │    │
│  │  │ • Периодич.    │ │ • Movements    │ │ • План счетов      │   │    │
│  │  │ • Статич.      │ │ • Balances     │ │ • Двойная запись   │   │    │
│  │  │                │ │ • Turnovers    │ │ • Субконто         │   │    │
│  │  └────────────────┘ └────────────────┘ └────────────────────┘   │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                  АНАЛИТИКА И АВТОМАТИЗАЦИЯ                       │    │
│  │  ┌────────────────┐ ┌────────────────┐ ┌────────────────────┐   │    │
│  │  │    ОТЧЕТЫ      │ │   ОБРАБОТКИ    │ │  БИЗНЕС-ПРОЦЕССЫ   │   │    │
│  │  │   (Reports)    │ │ (Processors)   │ │   (Workflows)      │   │    │
│  │  │                │ │                │ │                    │   │    │
│  │  │ Только чтение  │ │ Сценарии без   │ │ State Machine      │   │    │
│  │  │ + экспорт      │ │ сохранения     │ │ + Tasks            │   │    │
│  │  └────────────────┘ └────────────────┘ └────────────────────┘   │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### 3.2. Детальное описание объектов метаданных

#### 3.2.1. Константы (Constants / Singleton Models)

**Назначение:** Хранение условно-постоянной информации, существующей в системе в единственном экземпляре.

**Примеры:**
- Название организации
- Базовая валюта
- Учетная политика
- Разрешить отрицательные остатки

**Реализация в БД:**
```sql
CREATE TABLE const_company_settings (
    key VARCHAR(100) PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Или одна строка:
CREATE TABLE const_company_settings (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id = TRUE), -- Гарантия одной записи
    company_name TEXT NOT NULL,
    base_currency_id UUID REFERENCES cat_currencies(id),
    allow_negative_stock BOOLEAN DEFAULT FALSE,
    ...
);
```

**Архитектурная особенность:** Кэшируются в памяти приложения (In-Memory Cache с TTL 5-10 минут + NOTIFY/LISTEN для инвалидации).

#### 3.2.2. Перечисления (Enums)

**Назначение:** Закрытые списки значений, задаваемые на этапе разработки.

**Примеры:**
- Вид операции (Покупка/Продажа)
- Юридический статус (Физлицо/Юрлицо)
- Тип движения регистра (Приход/Расход)

**Реализация в Go:**
```go
// internal/domain/enums/operation_type.go
type OperationType int

const (
    OperationTypePurchase OperationType = iota + 1
    OperationTypeSale
    OperationTypeReturn
)

func (o OperationType) String() string {
    return [...]string{"", "Purchase", "Sale", "Return"}[o]
}

func (o OperationType) MarshalJSON() ([]byte, error) {
    return json.Marshal(o.String())
}
```

#### 3.2.3. Справочники (Catalogs)

**Назначение:** Основной разрез аналитического учета. Хранят сущности реального мира.

**Свойства:**
- Уникальный идентификатор (UUIDv7)
- Код (автонумерация)
- Наименование (Name)
- Иерархия (опционально)
- Подчинение (опционально)
- Кастомные поля (JSONB Attributes)

**Типы иерархии:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│                        ТИПЫ ИЕРАРХИИ СПРАВОЧНИКОВ                        │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  1. ПЛОСКИЙ (Flat)                                                       │
│     ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐                                    │
│     │ A   │ │ B   │ │ C   │ │ D   │                                    │
│     └─────┘ └─────┘ └─────┘ └─────┘                                    │
│     Контрагенты, Валюты, Единицы измерения                              │
│                                                                          │
│  2. ИЕРАРХИЧЕСКИЙ (Tree)                                                 │
│           ┌─────────┐                                                    │
│           │ Товары  │ (папка)                                           │
│           └────┬────┘                                                    │
│        ┌───────┴───────┐                                                 │
│     ┌──┴──┐         ┌──┴──┐                                            │
│     │Еда  │ (папка) │Одежда│ (папка)                                    │
│     └──┬──┘         └──┬──┘                                            │
│   ┌────┴────┐      ┌───┴───┐                                           │
│ ┌─┴─┐   ┌─┴─┐   ┌─┴─┐   ┌─┴─┐                                         │
│ │Яблоко │Хлеб│   │Джинсы │Куртка│ (элементы)                            │
│ └───┘   └───┘   └─────┘ └──────┘                                        │
│     Номенклатура, Подразделения                                         │
│                                                                          │
│  3. ПОДЧИНЕННЫЙ (Subordinate)                                            │
│           ┌───────────────┐                                              │
│           │  Контрагент   │ (владелец)                                  │
│           └───────┬───────┘                                              │
│        ┌──────────┼──────────┐                                          │
│     ┌──┴──┐   ┌───┴───┐  ┌──┴──┐                                       │
│     │Договор1│ │Договор2│ │Договор3│ (подчиненные)                      │
│     └─────┘   └───────┘  └─────┘                                       │
│     Договоры (подчинены Контрагентам)                                   │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

#### 3.2.4. Документы (Documents)

**Назначение:** Фиксация факта совершения хозяйственной операции во времени.

**Структура:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│                          СТРУКТУРА ДОКУМЕНТА                             │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                         ШАПКА (Header)                           │    │
│  │  ┌────────────────────────────────────────────────────────────┐ │    │
│  │  │  ID (UUIDv7)           │  Номер (Автонумерация)            │ │    │
│  │  │  Дата                  │  Организация (Ref)                │ │    │
│  │  │  Контрагент (Ref)      │  Склад (Ref)                      │ │    │
│  │  │  Проведен (Boolean)    │  Версия (Int, Optimistic Lock)    │ │    │
│  │  │  Attributes (JSONB)    │  [Кастомные поля]                 │ │    │
│  │  └────────────────────────────────────────────────────────────┘ │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                  ТАБЛИЧНЫЕ ЧАСТИ (Table Parts)                   │    │
│  │                                                                   │    │
│  │  ┌───────────────────────────────────────────────────────────┐  │    │
│  │  │ Табличная часть "Товары" (doc_xxx_goods)                   │  │    │
│  │  │ ┌─────┬────────────┬──────────┬───────┬─────────┬───────┐ │  │    │
│  │  │ │LineID│ LineNumber │ProductID │  Qty  │  Price  │ Total │ │  │    │
│  │  │ ├─────┼────────────┼──────────┼───────┼─────────┼───────┤ │  │    │
│  │  │ │UUID │     1      │   ...    │  10   │  100.00 │1000.00│ │  │    │
│  │  │ │UUID │     2      │   ...    │   5   │  200.00 │1000.00│ │  │    │
│  │  │ │UUID │     3      │   ...    │   2   │  500.00 │1000.00│ │  │    │
│  │  │ └─────┴────────────┴──────────┴───────┴─────────┴───────┘ │  │    │
│  │  └───────────────────────────────────────────────────────────┘  │    │
│  │                                                                   │    │
│  │  ┌───────────────────────────────────────────────────────────┐  │    │
│  │  │ Табличная часть "Услуги" (doc_xxx_services)                │  │    │
│  │  │ ...                                                        │  │    │
│  │  └───────────────────────────────────────────────────────────┘  │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

**Механизм проведения (Posting):**
- **Черновик:** Документ просто сохранен(аналог записан в 1С) (как справочник)
- **Проведен:** Документ породил записи в Регистрах

#### 3.2.5. Регистры накопления (Accumulation Registers)

**Назначение:** Основа количественно-суммового учета. Хранение остатков и оборотов.

**Архитектура:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│                     РЕГИСТР НАКОПЛЕНИЯ "ТОВАРЫ НА СКЛАДАХ"              │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │              ТАБЛИЦА ДВИЖЕНИЙ (reg_stock_movements)              │    │
│  │                   Partitioned by RANGE (period)                   │    │
│  │ ┌──────────┬───────────┬──────────┬──────────┬────────┬────────┐│    │
│  │ │  Period  │ Warehouse │ Product  │ RecType  │  Qty   │RecordID││    │
│  │ ├──────────┼───────────┼──────────┼──────────┼────────┼────────┤│    │
│  │ │2024-01-15│  WH-001   │ PRD-001  │ RECEIPT  │ +100   │Doc-123 ││    │
│  │ │2024-01-16│  WH-001   │ PRD-001  │ EXPENSE  │  -30   │Doc-124 ││    │
│  │ │2024-01-17│  WH-002   │ PRD-002  │ RECEIPT  │  +50   │Doc-125 ││    │
│  │ └──────────┴───────────┴──────────┴──────────┴────────┴────────┘│    │
│  │  • Неизменяемый журнал (Immutable Ledger)                        │    │
│  │  • recorder_version для версионирования                          │    │
│  │  • Партиции по месяцам                                           │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    │ Триггер/Сервис                      │
│                                    ▼ (Delta Update)                      │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │               ТАБЛИЦА ОСТАТКОВ (reg_stock_balances)              │    │
│  │                      "Горячий кэш" остатков                       │    │
│  │ ┌───────────┬──────────┬──────────┬──────────┐                   │    │
│  │ │ Warehouse │ Product  │ Quantity │ Reserved │                   │    │
│  │ ├───────────┼──────────┼──────────┼──────────┤                   │    │
│  │ │  WH-001   │ PRD-001  │    70    │    10    │                   │    │
│  │ │  WH-002   │ PRD-002  │    50    │     0    │                   │    │
│  │ └───────────┴──────────┴──────────┴──────────┘                   │    │
│  │  • Моментальное получение остатков O(1)                          │    │
│  │  • CHECK (quantity >= 0) для контроля отрицательных              │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

#### 3.2.6. Регистры сведений (Information Registers)

**Периодические:** Хранят историю изменения значения во времени.
```sql
-- Курсы валют
CREATE TABLE reg_info_currency_rates (
    period      DATE NOT NULL,
    currency_id UUID NOT NULL,
    rate        BIGINT NOT NULL, -- scaled x10000 (Quantity)
    PRIMARY KEY (currency_id, period)
);

-- Получение среза последних (Slice Last) через DISTINCT ON:
SELECT DISTINCT ON (currency_id)
    currency_id, rate, period
FROM reg_info_currency_rates
WHERE period <= $1 -- TargetDate
ORDER BY currency_id, period DESC;
```

**Непериодические (Статические):** Текущее состояние без истории.
```sql
-- Штрихкоды товаров
CREATE TABLE reg_info_barcodes (
    barcode     VARCHAR(50) PRIMARY KEY,
    product_id  UUID NOT NULL REFERENCES cat_nomenclature(id)
);
```

---

# Часть II. Структура Репозитория и Слои Приложения

## 4. Полная Структура Проекта

```
metapus/
├── cmd/                           # ТОЧКИ ВХОДА
│   ├── server/
│   │   └── main.go               # REST API Сервер
│   ├── worker/
│   │   └── main.go               # Multi-tenant воркер фоновых задач
│   ├── tenant/
│   │   └── main.go               # CLI управления тенантами
│   └── seed/
│       └── main.go               # Seed данных для разработки
│
├── configs/
│   ├── .env.example              # Шаблон переменных окружения
│   └── config.yaml               # Примеры конфигурации
│
│   # Переменные окружения для Database-per-Tenant:
│   # META_DATABASE_URL=postgres://user:pass@localhost:5432/metapus_meta
│   # TENANT_DB_DEFAULT_HOST=localhost
│   # TENANT_DB_DEFAULT_PORT=5432
│   # TENANT_DB_DEFAULT_USER=metapus
│   # TENANT_DB_DEFAULT_PASSWORD=secret
│   # TENANT_POOL_MAX_CONNS=10
│   # TENANT_POOL_IDLE_TIMEOUT=30m
│   # TENANT_MAX_TOTAL_POOLS=100
│
├── db/
│   ├── meta/                     # Миграции Meta-database (tenants)
│   │   └── 00001_tenants.sql     # Схема для управления тенантами
│   ├── migrations/               # SQL миграции для tenant databases (goose)
│   │   ├── 00001_init_extensions.sql
│   │   ├── 00002_sys_sequences.sql
│   │   ├── 00003_sys_outbox.sql
│   │   ├── 00004_sys_audit.sql
│   │   ├── 00005_sys_sessions.sql
│   │   ├── 00010_cat_counterparties.sql
│   │   ├── 00011_cat_nomenclature.sql
│   │   ├── 00012_cat_warehouses.sql
│   │   ├── 00020_doc_goods_receipt.sql
│   │   ├── 00021_doc_invoice.sql
│   │   ├── 00030_reg_stock.sql
│   │   └── ...
│   └── seeds/                    # Начальные данные
│
├── internal/                     # ПРИВАТНЫЙ КОД
│   ├── core/                     # ═══ ЯДРО ═══
│   │   ├── apperror/
│   │   │   └── error.go          # AppError (Code, Message, Details)
│   │   ├── context/
│   │   │   └── user_context.go   # Извлечение UserID из ctx
│   │   ├── entity/
│   │   │   ├── base.go           # BaseEntity, BaseCatalog, BaseDocument
│   │   │   ├── catalog.go        # Catalog struct (NewCatalog без tenantID)
│   │   │   ├── document.go       # Document struct (NewDocument без tenantID)
│   │   │   └── register.go       # StockMovement, StockBalance
│   │   ├── id/
│   │   │   └── uuid.go           # UUIDv7 генерация
│   │   ├── instance/
│   │   │   └── isolation.go      # DedicatedIsolation (no-op для DB-per-Tenant)
│   │   ├── tenant/               # ═══ MULTI-TENANCY ═══
│   │   │   ├── types.go          # Tenant struct (ID, Slug, DBName, Status)
│   │   │   ├── context.go        # WithPool, WithTxManager, WithTenant
│   │   │   ├── registry.go       # PostgresRegistry для meta-database
│   │   │   └── manager.go        # MultiTenantManager (пулы соединений)
│   │   ├── tx/
│   │   │   └── tx.go             # Transaction Manager interface
│   │   ├── security/
│   │   │   ├── scope.go          # AccessScope (UserID, Roles)
│   │   │   └── jwt.go            # JWT Claims
│   │   └── types/
│   │       └── money.go          # MinorUnits, Quantity, Money (Decimal)
│   │
│   ├── domain/                   # ═══ БИЗНЕС-ЛОГИКА ═══
│   │   ├── catalogs/
│   │   │   ├── counterparty/
│   │   │   │   ├── model.go      # Структура Counterparty
│   │   │   │   ├── repo.go       # Интерфейс Repository
│   │   │   │   ├── service.go    # Бизнес-логика
│   │   │   │   └── hooks.go      # BeforeSave, AfterSave hooks
│   │   │   ├── nomenclature/
│   │   │   │   ├── model.go
│   │   │   │   ├── repo.go
│   │   │   │   └── service.go
│   │   │   ├── warehouse/
│   │   │   ├── currency/
│   │   │   └── unit/
│   │   │
│   │   ├── documents/
│   │   │   ├── goods_receipt/
│   │   │   │   ├── model.go      # Шапка + табличные части
│   │   │   │   ├── repo.go
│   │   │   │   ├── service/
│   │   │   │   │   ├── crud.go   # Create, Update, Delete
│   │   │   │   │   └── posting.go # Post, Unpost логика
│   │   │   │   └── hooks.go
│   │   │   ├── invoice/
│   │   │   │   ├── model.go
│   │   │   │   ├── repo.go
│   │   │   │   ├── service/
│   │   │   │   │   ├── crud.go
│   │   │   │   │   ├── posting.go
│   │   │   │   │   └── stock_control.go  # Контроль остатков
│   │   │   │   └── hooks.go
│   │   │   └── stock_transfer/
│   │   │
│   │   ├── registers/
│   │   │   ├── accumulation/
│   │   │   │   └── stock/
│   │   │   │       ├── model.go  # Movement, Balance structs
│   │   │   │       ├── repo.go
│   │   │   │       └── service.go
│   │   │   └── information/
│   │   │       ├── currency_rates/
│   │   │       └── barcodes/
│   │   │
│   │   ├── reports/
│   │   │   ├── stock_balance/
│   │   │   └── sales_turnover/
│   │   │
│   │   └── workflow/
│   │       ├── engine.go
│   │       └── tasks.go
│   │
│   └── infrastructure/           # ═══ РЕАЛИЗАЦИЯ ═══
│       ├── storage/
│       │   └── postgres/
│       │       ├── connection.go     # pgxpool setup
│       │       ├── tx_manager.go     # Transaction Manager
│       │       ├── outbox.go         # Outbox Publisher
│       │       ├── idempotency.go    # Idempotency Store
│       │       ├── catalog_repo/
│       │       │   ├── counterparty.go
│       │       │   └── nomenclature.go
│       │       ├── document_repo/
│       │       │   ├── goods_receipt.go
│       │       │   └── invoice.go
│       │       └── register_repo/
│       │           └── stock.go
│       │
│       ├── http/
│       │   └── v1/
│       │       ├── router.go         # Gin router setup
│       │       ├── dto/
│       │       │   ├── catalog.go    # Request/Response DTOs
│       │       │   └── document.go
│       │       ├── handlers/
│       │       │   ├── catalog_handler.go
│       │       │   ├── document_handler.go
│       │       │   └── health_handler.go
│       │       └── middleware/
│       │           ├── recovery.go
│       │           ├── trace.go
│       │           ├── logger.go
│       │           ├── error.go
│       │           ├── auth.go
│       │           ├── tenant.go        # TenantDB middleware (DB-per-Tenant)
│       │           └── idempotency.go
│       │
│       ├── cache/
│       │   ├── schema_cache.go       # In-Memory Schema Cache
│       │   └── feature_flags.go
│       │
│       └── worker/
│           ├── base.go               # BaseWorker (итерация по тенантам)
│           ├── outbox_relay.go       # Multi-tenant Outbox -> Kafka/NATS
│           ├── audit_cleaner.go      # Удаление старых партиций
│           └── handlers/
│               └── month_closing.go  # Multi-tenant закрытие периода
│
├── pkg/                          # ПУБЛИЧНЫЕ УТИЛИТЫ
│   ├── logger/
│   │   └── logger.go             # Zap wrapper
│   ├── numerator/
│   │   └── service.go            # Автонумерация
│   └── decimal/
│       └── helpers.go
│
├── Dockerfile
├── Makefile
├── go.mod
└── go.sum
```

---

## 5. Слой Core (Ядро)

### 5.1. Базовые сущности (internal/core/entity)

```go
// base.go
package entity

import (
    "context"
    "time"
    "github.com/shopspring/decimal"
)

// Attributes — типобезопасный JSONB с кастомным Scanner
type Attributes map[string]any

// Scan реализует sql.Scanner для корректной работы с числами
func (a *Attributes) Scan(value interface{}) error {
    bytes, ok := value.([]byte)
    if !ok {
        return nil
    }

    decoder := json.NewDecoder(bytes.NewReader(bytes))
    decoder.UseNumber() // Критично: числа как json.Number, не float64

    var m map[string]any
    if err := decoder.Decode(&m); err != nil {
        return err
    }
    *a = m
    return nil
}

// Типизированные геттеры
func (a Attributes) GetDecimal(key string) decimal.Decimal {
    if a == nil {
        return decimal.Zero
    }
    switch v := a[key].(type) {
    case json.Number:
        d, _ := decimal.NewFromString(v.String())
        return d
    case string:
        d, _ := decimal.NewFromString(v)
        return d
    case float64:
        return decimal.NewFromFloat(v)
    }
    return decimal.Zero
}

func (a Attributes) GetString(key string) string {
    if v, ok := a[key].(string); ok {
        return v
    }
    return ""
}

// BaseEntity — фундамент для всех объектов (Catalogs, Documents, etc.)
// ВАЖНО (Database-per-Tenant): tenant discriminator в бизнес-таблицах НЕ нужен.
// Tenant/tx/pool доступны через context.Context.
type BaseEntity struct {
    ID           id.ID      `db:"id" json:"id"`
    DeletionMark bool       `db:"deletion_mark" json:"deletionMark"`
    Version      int        `db:"version" json:"version"` // Optimistic Locking
    Attributes   Attributes `db:"attributes" json:"attributes,omitempty"` // JSONB custom fields
}

// BaseDocument — стандарт для документов (audit-поля нужны, т.к. документ фиксирует факт хозяйственной операции)
type BaseDocument struct {
    BaseEntity
    CreatedAt time.Time `db:"created_at" json:"createdAt"`
    UpdatedAt time.Time `db:"updated_at" json:"updatedAt"`
    CreatedBy string    `db:"created_by" json:"createdBy,omitempty"`
    UpdatedBy string    `db:"updated_by" json:"updatedBy,omitempty"`
}

// BaseCatalog — стандарт для справочников (audit-поля избыточны и НЕ являются контрактом)
// Если нужно "кто менял номенклатуру" — смотрим системный аудит/логи (например sys_audit / централизованные логи),
// а не усложняем схему справочников.
type BaseCatalog struct {
    BaseEntity
    Code string `db:"code" json:"code"`
    Name string `db:"name" json:"name"`
}

// Catalog — стандарт для справочников
type Catalog struct {
    BaseCatalog
}

// HierarchicalCatalog — иерархический справочник
type HierarchicalCatalog struct {
    BaseCatalog
    ParentID  *id.ID `db:"parent_id" json:"parentId,omitempty"`
    Level     int    `db:"level" json:"level"`
    Path      string `db:"path" json:"path"` // Materialized path
    IsFolder  bool   `db:"is_folder" json:"isFolder"`
    SortOrder int    `db:"sort_order" json:"sortOrder"`
}

// Document — технический стандарт для всех бизнес-документов.
type Document struct {
    BaseDocument
    Number         string    `db:"number" json:"number"`
    Date           time.Time `db:"date" json:"date"`
    OrganizationID id.ID     `db:"organization_id" json:"organizationId"`
    Posted         bool      `db:"posted" json:"posted"`
    PostedVersion  int       `db:"posted_version" json:"postedVersion"`
}

// 3.2.4.1. Стандартные Трейты (Traits / Mixins)
// Мы используем композицию для добавления стандартных измерений там, где они нужны.

// CurrencyAware добавляет поддержку валюты (для финансовых документов).
type CurrencyAware struct {
    CurrencyID id.ID `db:"currency_id" json:"currencyId"`
}

// Пример сборки документа:
type GoodsReceipt struct {
    Document      // Включает BaseDocument
    CurrencyAware // Трейт валюты
    // ... поля документа
}
```

### 5.2. Интерфейс валидации

```go
// validatable.go
package entity

import "context"

// Validatable — интерфейс проверки бизнес-инвариантов
type Validatable interface {
    // Validate проверяет внутреннюю согласованность сущности
    // БЕЗ обращения к внешним сервисам и БД
    // Примеры: EndDate >= StartDate, Amount > 0
    Validate(ctx context.Context) error
}
```

### 5.3. UUIDv7 Генерация

```go
// id/uuid.go
package id

import (
    "github.com/google/uuid"
)

type ID = uuid.UUID

// New генерирует UUIDv7 (сортируемый по времени)
func New() ID {
    return uuid.Must(uuid.NewV7())
}

// Parse парсит строку в UUID
func Parse(s string) (ID, error) {
    return uuid.Parse(s)
}

// IsZero проверяет на нулевой UUID
func IsZero(id ID) bool {
    return id == uuid.Nil
}
```

### 5.4. Структура ошибок (RFC 7807)

```go
// apperror/error.go
package apperror

import "fmt"

// Коды ошибок
const (
    CodeValidation           = "VALIDATION_ERROR"
    CodeNotFound             = "NOT_FOUND"
    CodeConflict             = "CONFLICT"
    CodeInsufficientStock    = "INSUFFICIENT_STOCK"
    CodeClosedPeriod         = "CLOSED_PERIOD"
    CodeConcurrentModification = "CONCURRENT_MODIFICATION"
    CodeUnauthorized         = "UNAUTHORIZED"
    CodeForbidden            = "FORBIDDEN"
    CodeInternal             = "INTERNAL_ERROR"
)

// AppError — структурированная бизнес-ошибка
type AppError struct {
    Code    string                 `json:"code"`
    Message string                 `json:"message"`
    Details map[string]interface{} `json:"details,omitempty"`
    Err     error                  `json:"-"` // Внутренняя ошибка (для логов)
}

func (e AppError) Error() string {
    return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e AppError) Unwrap() error {
    return e.Err
}

// Конструкторы
func NewValidationError(message string, details map[string]interface{}) AppError {
    return AppError{Code: CodeValidation, Message: message, Details: details}
}

func NewNotFoundError(entity, id string) AppError {
    return AppError{
        Code:    CodeNotFound,
        Message: fmt.Sprintf("%s with id %s not found", entity, id),
        Details: map[string]interface{}{"entity": entity, "id": id},
    }
}

func NewInsufficientStockError(productID string, requested, available types.Quantity) AppError {
    return AppError{
        Code:    CodeInsufficientStock,
        Message: "Недостаточно товара на складе",
        Details: map[string]interface{}{
            "product_id": productID,
            "requested":  requested,
            "available":  available,
        },
    }
}

func NewConcurrentModificationError() AppError {
    return AppError{
        Code:    CodeConcurrentModification,
        Message: "Данные были изменены другим пользователем. Обновите страницу.",
    }
}
```

---

## 6. Слой Domain (Бизнес-логика)

### 6.1. Пример справочника "Контрагенты"

```go
// domain/catalogs/counterparty/model.go
package counterparty

import (
    "context"
    "errors"
    "metapus/internal/core/entity"
)

type Counterparty struct {
    entity.Catalog

    INN           string `db:"inn" json:"inn"`
    LegalStatus   int    `db:"legal_status" json:"legalStatus"` // Enum: 1=Физлицо, 2=Юрлицо
    ContactPerson string `db:"contact_person" json:"contactPerson,omitempty"`
    Phone         string `db:"phone" json:"phone,omitempty"`
    Email         string `db:"email" json:"email,omitempty"`
}

// Validate реализует entity.Validatable
func (c *Counterparty) Validate(ctx context.Context) error {
    if c.Name == "" {
        return errors.New("наименование обязательно")
    }
    if c.LegalStatus == 2 && c.INN == "" {
        return errors.New("ИНН обязателен для юридических лиц")
    }
    if c.INN != "" && !isValidINN(c.INN) {
        return errors.New("некорректный формат ИНН")
    }
    return nil
}

func isValidINN(inn string) bool {
    // Проверка контрольной суммы ИНН
    // ...
    return len(inn) == 10 || len(inn) == 12
}
```

```go
// domain/catalogs/counterparty/repo.go
package counterparty

import (
    "context"
    "metapus/internal/core/id"
)

type Repository interface {
    GetByID(ctx context.Context, id id.ID) (*Counterparty, error)
    GetByINN(ctx context.Context, inn string) (*Counterparty, error)
    List(ctx context.Context, filter ListFilter) ([]Counterparty, int, error)
    Create(ctx context.Context, c *Counterparty) error
    Update(ctx context.Context, c *Counterparty) error
    Delete(ctx context.Context, id id.ID) error
    Lock(ctx context.Context, id id.ID) error // FOR UPDATE
}

type ListFilter struct {
    Search       string
    LegalStatus  *int
    DeletionMark *bool
    Limit        int
    Offset       int
}
```

```go
// domain/catalogs/counterparty/service.go
package counterparty

import (
    "context"
    "metapus/internal/core/apperror"
    "metapus/internal/core/id"
    "metapus/pkg/numerator"
)

type Service struct {
    repo       Repository
    numerator  *numerator.Service
    hooks      []BeforeSaveHook
}

func NewService(repo Repository, num *numerator.Service) *Service {
    return &Service{repo: repo, numerator: num}
}

func (s *Service) RegisterHook(hook BeforeSaveHook) {
    s.hooks = append(s.hooks, hook)
}

func (s *Service) Create(ctx context.Context, c *Counterparty) error {
    // 1. Валидация (Domain Invariants)
    if err := c.Validate(ctx); err != nil {
        return apperror.NewValidationError(err.Error(), nil)
    }

    // 2. Автонумерация
    code, err := s.numerator.GetNextNumber(ctx, "counterparty", nil)
    if err != nil {
        return err
    }
    c.Code = code
    c.ID = id.New().String()

    // 3. Хуки (расширяемость)
    for _, hook := range s.hooks {
        if err := hook.Execute(ctx, c); err != nil {
            return err
        }
    }

    // 4. Сохранение
    return s.repo.Create(ctx, c)
}

func (s *Service) Update(ctx context.Context, c *Counterparty) error {
    // 1. Валидация
    if err := c.Validate(ctx); err != nil {
        return apperror.NewValidationError(err.Error(), nil)
    }

    // 2. Хуки
    for _, hook := range s.hooks {
        if err := hook.Execute(ctx, c); err != nil {
            return err
        }
    }

    // 3. Обновление с Optimistic Locking
    return s.repo.Update(ctx, c)
}
```

```go
// domain/catalogs/counterparty/hooks.go
package counterparty

import "context"

// BeforeSaveHook — интерфейс для расширения логики сохранения
type BeforeSaveHook interface {
    Execute(ctx context.Context, c *Counterparty) error
}

// Пример: Проверка ИНН через DaData
type DaDataValidationHook struct {
    apiKey string
}

func (h *DaDataValidationHook) Execute(ctx context.Context, c *Counterparty) error {
    // Вызов внешнего API для валидации ИНН
    // ...
    return nil
}
```

### 6.2. Пример документа "Реализация" с проведением

```go
// domain/documents/invoice/model.go
package invoice

import (
    "context"
    "errors"
    "time"
    "github.com/shopspring/decimal"
    "metapus/internal/core/entity"
    "metapus/internal/core/id"
)

type Invoice struct {
    entity.Document

    CounterpartyID string    `db:"counterparty_id" json:"counterpartyId"`
    WarehouseID    string    `db:"warehouse_id" json:"warehouseId"`
    OrganizationID string    `db:"organization_id" json:"organizationId"`
    CurrencyID     string    `db:"currency_id" json:"currencyId"`
    TotalAmount    types.MinorUnits `db:"total_amount" json:"totalAmount"`

    // Табличные части (не маппятся напрямую)
    Items []InvoiceItem `db:"-" json:"items"`
}

type InvoiceItem struct {
    LineID     string          `db:"line_id" json:"lineId"`
    LineNumber int             `db:"line_number" json:"lineNumber"`
    ProductID  string          `db:"product_id" json:"productId"`
    Quantity   types.Quantity  `db:"quantity" json:"quantity"`
    Price      types.MinorUnits `db:"price" json:"price"`
    Amount     types.MinorUnits `db:"amount" json:"amount"`
}

func (i *Invoice) Validate(ctx context.Context) error {
    if i.CounterpartyID == "" {
        return errors.New("контрагент обязателен")
    }
    if i.WarehouseID == "" {
        return errors.New("склад обязателен")
    }
    if len(i.Items) == 0 {
        return errors.New("документ не может быть пустым")
    }
    for idx, item := range i.Items {
        if item.Quantity.LessThanOrEqual(decimal.Zero) {
            return fmt.Errorf("строка %d: количество должно быть положительным", idx+1)
        }
    }
    return nil
}

// Calculate пересчитывает суммы
func (i *Invoice) Calculate() {
    var total types.MinorUnits
    for idx := range i.Items {
        // Integer arithmetic: (QuantityScaled * Price) / 10000
        i.Items[idx].Amount = types.MinorUnits((i.Items[idx].Quantity.Int64Scaled() * int64(i.Items[idx].Price)) / 10000)
        total += i.Items[idx].Amount
    }
    i.TotalAmount = total
}

// GenerateMovements формирует движения для регистра
func (i *Invoice) GenerateMovements(version int) []StockMovement {
    movements := make([]StockMovement, len(i.Items))
    for idx, item := range i.Items {
        movements[idx] = StockMovement{
            Period:          i.Date,
            WarehouseID:     i.WarehouseID,
            ProductID:       item.ProductID,
            RecorderID:      i.ID,
            RecorderVersion: version,
            LineID:          item.LineID,
            RecordType:      RecordTypeExpense,
            Quantity:        item.Quantity.Neg(), // Отрицательное для расхода
        }
    }
    return movements
}
```

```go
// domain/documents/invoice/service/posting.go
package service

import (
    "context"
    "sort"
    "metapus/internal/core/apperror"
    "metapus/internal/domain/documents/invoice"
    "metapus/internal/domain/registers/accumulation/stock"
)

type PostingService struct {
    docRepo     invoice.Repository
    stockRepo   stock.Repository
    periodCheck PeriodChecker
    // ВАЖНО: НЕТ txManager в структуре! Получаем из контекста.
}

func (s *PostingService) Post(ctx context.Context, docID string) error {
    // TxManager получаем из контекста (Database-per-Tenant)
    txm := tenant.MustGetTxManager(ctx)
    return txm.RunInTransaction(ctx, func(ctx context.Context) error {
        // 1. Блокировка документа
        doc, err := s.docRepo.GetForUpdate(ctx, docID)
        if err != nil {
            return err
        }

        if doc.Posted {
            return nil // Уже проведен (идемпотентность)
        }

        // 2. Проверка закрытого периода
        if err := s.periodCheck.Validate(ctx, doc.Date); err != nil {
            return err
        }

        // 3. Сортировка строк по ProductID для предотвращения Deadlock
        items := make([]invoice.InvoiceItem, len(doc.Items))
        copy(items, doc.Items)
        sort.Slice(items, func(i, j int) bool {
            return items[i].ProductID < items[j].ProductID
        })

        // 4. Получение старых движений (если перепроведение)
        oldMoves, currentVersion := s.stockRepo.GetActiveByRecorder(ctx, docID)
        newVersion := currentVersion + 1

        // 5. Генерация новых движений
        newMoves := doc.GenerateMovements(newVersion)

        // 6. Расчет дельты
        deltas := s.calculateDeltas(oldMoves, newMoves)

        // 7. Атомарное применение дельт к остаткам
        for key, delta := range deltas {
            if delta.IsNegative() {
                // Списание — нужна проверка остатка
                affected, err := s.stockRepo.UpdateBalanceWithCheck(
                    ctx,
                    key.WarehouseID,
                    key.ProductID,
                    delta,
                )
                if err != nil {
                    return err
                }
                if affected == 0 {
                    balance, _ := s.stockRepo.GetBalance(ctx, key.WarehouseID, key.ProductID)
                    return apperror.NewInsufficientStockError(
                        key.ProductID,
                        delta.Abs(),
                        balance,
                    )
                }
            } else {
                // Приход — просто обновляем
                if err := s.stockRepo.UpdateBalance(ctx, key.WarehouseID, key.ProductID, delta); err != nil {
                    return err
                }
            }
        }

        // 8. Запись движений (Immutable Ledger)
        if err := s.stockRepo.BulkInsertMovements(ctx, newMoves); err != nil {
            return err
        }

        // 9. Пометка документа как проведенного
        return s.docRepo.SetPosted(ctx, docID, true)
    })
}

// calculateDeltas вычисляет разницу между старыми и новыми движениями
func (s *PostingService) calculateDeltas(old, new []stock.Movement) map[DimensionKey]decimal.Decimal {
    deltas := make(map[DimensionKey]decimal.Decimal)

    // Вычитаем старые
    for _, m := range old {
        key := DimensionKey{WarehouseID: m.WarehouseID, ProductID: m.ProductID}
        deltas[key] = deltas[key].Sub(m.Quantity)
    }

    // Добавляем новые
    for _, m := range new {
        key := DimensionKey{WarehouseID: m.WarehouseID, ProductID: m.ProductID}
        deltas[key] = deltas[key].Add(m.Quantity)
    }

    return deltas
}

type DimensionKey struct {
    WarehouseID string
    ProductID   string
}
```

---

## 7. Слой Infrastructure

### 7.1. Transaction Manager

В архитектуре Database-per-Tenant, TxManager создаётся динамически для каждого 
запроса и инжектируется в контекст через middleware TenantDB.

```go
// infrastructure/storage/postgres/tx_manager.go
package postgres

import (
    "context"
    "fmt"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
)

type ctxKey string
const txKey ctxKey = "pg_tx"

type TxManager struct {
    pool *pgxpool.Pool
}

// NewTxManager создаёт TxManager для конкретного пула (используется в middleware)
func NewTxManager(pool *pgxpool.Pool) *TxManager {
    return &TxManager{pool: pool}
}

// NewTxManagerFromRawPool создаёт TxManager для raw pool (Database-per-Tenant)
func NewTxManagerFromRawPool(pool *pgxpool.Pool) *TxManager {
    return &TxManager{pool: pool}
}

// RunInTransaction выполняет функцию в транзакции
func (m *TxManager) RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
    // Проверяем, есть ли уже транзакция
    if tx := m.getTxFromContext(ctx); tx != nil {
        // Вложенная транзакция — просто выполняем без нового BEGIN
        return fn(ctx)
    }

    // Начинаем транзакцию
    tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{
        IsoLevel: pgx.RepeatableRead,
    })
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }

    // Устанавливаем таймаут на statement
    _, _ = tx.Exec(ctx, "SET LOCAL statement_timeout = '30s'")

    // Кладем транзакцию в контекст
    ctx = context.WithValue(ctx, txKey, tx)

    // Выполняем бизнес-логику
    if err := fn(ctx); err != nil {
        _ = tx.Rollback(ctx)
        return err
    }

    // Фиксируем
    return tx.Commit(ctx)
}

// RunInTransactionWithSavepoint — с точкой сохранения для изоляции ошибок
func (m *TxManager) RunInTransactionWithSavepoint(ctx context.Context, fn func(ctx context.Context) error) error {
    tx := m.getTxFromContext(ctx)
    if tx == nil {
        return m.RunInTransaction(ctx, fn)
    }

    // Создаем savepoint
    savepointName := fmt.Sprintf("sp_%d", time.Now().UnixNano())
    _, err := tx.Exec(ctx, "SAVEPOINT "+savepointName)
    if err != nil {
        return err
    }

    if err := fn(ctx); err != nil {
        _, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepointName)
        return err
    }

    _, _ = tx.Exec(ctx, "RELEASE SAVEPOINT "+savepointName)
    return nil
}

func (m *TxManager) getTxFromContext(ctx context.Context) pgx.Tx {
    if tx, ok := ctx.Value(txKey).(pgx.Tx); ok {
        return tx
    }
    return nil
}

// GetConn возвращает соединение (транзакцию или пул)
func (m *TxManager) GetConn(ctx context.Context) Queryable {
    if tx := m.getTxFromContext(ctx); tx != nil {
        return tx
    }
    return m.pool
}

type Queryable interface {
    Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
```

### 7.2. Репозиторий справочника

В архитектуре Database-per-Tenant, репозитории получают TxManager из контекста,
а не хранят его в структуре. Это позволяет работать с разными базами данных
в зависимости от текущего тенанта.

```go
// infrastructure/storage/postgres/catalog_repo/counterparty.go
package catalog_repo

import (
    "context"
    "github.com/Masterminds/squirrel"
    "metapus/internal/core/apperror"
    "metapus/internal/core/tenant"
    "metapus/internal/domain/catalogs/counterparty"
    "metapus/internal/infrastructure/storage/postgres"
)

type CounterpartyRepo struct {
    builder squirrel.StatementBuilderType
    // ВАЖНО: НЕТ txManager в структуре! Получаем из контекста.
}

func NewCounterpartyRepo() *CounterpartyRepo {
    return &CounterpartyRepo{
        builder: squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar),
    }
}

// getTxManager получает TxManager из контекста (Database-per-Tenant)
func (r *CounterpartyRepo) getTxManager(ctx context.Context) *postgres.TxManager {
    return tenant.MustGetTxManager(ctx)
}

func (r *CounterpartyRepo) GetByID(ctx context.Context, id string) (*counterparty.Counterparty, error) {
    conn := r.getTxManager(ctx).GetQuerier(ctx)

    sql, args, _ := r.builder.
        Select("id", "code", "name", "inn", "legal_status", "deletion_mark",
               "version", "attributes", "created_at", "updated_at").
        From("cat_counterparties").
        Where(squirrel.Eq{"id": id}).
        ToSql()

    var c counterparty.Counterparty
    err := conn.QueryRow(ctx, sql, args...).Scan(
        &c.ID, &c.Code, &c.Name, &c.INN, &c.LegalStatus, &c.DeletionMark,
        &c.Version, &c.Attributes, &c.CreatedAt, &c.UpdatedAt,
    )
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, apperror.NewNotFoundError("Counterparty", id)
        }
        return nil, err
    }

    return &c, nil
}

func (r *CounterpartyRepo) Create(ctx context.Context, c *counterparty.Counterparty) error {
    conn := r.getTxManager(ctx).GetQuerier(ctx)

    // ВАЖНО: INSERT без фильтрации/привязки к tenant (изоляция через БД)
    sql, args, _ := r.builder.
        Insert("cat_counterparties").
        Columns("id", "code", "name", "inn", "legal_status").
        Values(c.ID, c.Code, c.Name, c.INN, c.LegalStatus).
        ToSql()

    _, err := conn.Exec(ctx, sql, args...)
    return err
}

func (r *CounterpartyRepo) Update(ctx context.Context, c *counterparty.Counterparty) error {
    conn := r.getTxManager(ctx).GetQuerier(ctx)

    // Optimistic Locking: version = version + 1 WHERE version = $old
    sql, args, _ := r.builder.
        Update("cat_counterparties").
        Set("name", c.Name).
        Set("inn", c.INN).
        Set("legal_status", c.LegalStatus).
        Set("attributes", c.Attributes).
        Set("version", squirrel.Expr("version + 1")).
        Set("updated_at", squirrel.Expr("NOW()")).
        Where(squirrel.Eq{"id": c.ID, "version": c.Version}).
        ToSql()

    result, err := conn.Exec(ctx, sql, args...)
    if err != nil {
        return err
    }

    if result.RowsAffected() == 0 {
        return apperror.NewConcurrentModificationError()
    }

    return nil
}

func (r *CounterpartyRepo) List(ctx context.Context, filter counterparty.ListFilter) ([]counterparty.Counterparty, int, error) {
    conn := r.getTxManager(ctx).GetQuerier(ctx)

    // ВАЖНО: без фильтрации по tenant в WHERE (изоляция через БД)
    query := r.builder.
        Select("id", "code", "name", "inn", "legal_status", "deletion_mark", "version").
        From("cat_counterparties")

    // Динамические фильтры
    if filter.Search != "" {
        query = query.Where(squirrel.Or{
            squirrel.ILike{"name": "%" + filter.Search + "%"},
            squirrel.ILike{"code": "%" + filter.Search + "%"},
            squirrel.ILike{"inn": "%" + filter.Search + "%"},
        })
    }
    if filter.LegalStatus != nil {
        query = query.Where(squirrel.Eq{"legal_status": *filter.LegalStatus})
    }
    if filter.DeletionMark != nil {
        query = query.Where(squirrel.Eq{"deletion_mark": *filter.DeletionMark})
    }

    // Подсчет общего количества
    countQuery := query.RemoveColumns().Columns("COUNT(*)")
    countSQL, countArgs, _ := countQuery.ToSql()

    var total int
    if err := conn.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
        return nil, 0, err
    }

    // Пагинация
    query = query.OrderBy("code ASC").Limit(uint64(filter.Limit)).Offset(uint64(filter.Offset))

    sql, args, _ := query.ToSql()
    rows, err := conn.Query(ctx, sql, args...)
    if err != nil {
        return nil, 0, err
    }
    defer rows.Close()

    var result []counterparty.Counterparty
    for rows.Next() {
        var c counterparty.Counterparty
        if err := rows.Scan(&c.ID, &c.Code, &c.Name, &c.INN, &c.LegalStatus, &c.DeletionMark, &c.Version); err != nil {
            return nil, 0, err
        }
        result = append(result, c)
    }

    return result, total, nil
}
```

### 7.3. Outbox Publisher (Transactional Outbox)

```go
// infrastructure/storage/postgres/outbox.go
package postgres

import (
    "context"
    "encoding/json"
    "github.com/jackc/pgx/v5"
)

type OutboxPublisher struct {
    // ВАЖНО: НЕТ txManager в структуре! Получаем из контекста (Database-per-Tenant)
}

type DomainEvent struct {
    AggregateType string      `json:"aggregateType"`
    AggregateID   string      `json:"aggregateId"`
    EventType     string      `json:"eventType"`
    Payload       interface{} `json:"payload"`
}

func (p *OutboxPublisher) Publish(ctx context.Context, event DomainEvent) error {
    txm := tenant.MustGetTxManager(ctx)
    conn := txm.GetQuerier(ctx)

    payloadBytes, err := json.Marshal(event.Payload)
    if err != nil {
        return err
    }

    _, err = conn.Exec(ctx, `
        INSERT INTO sys_outbox (aggregate_type, aggregate_id, event_type, payload)
        VALUES ($1, $2, $3, $4)
    `, event.AggregateType, event.AggregateID, event.EventType, payloadBytes)

    return err
}
```

---

# Часть III. Правила Именования

## 8. Единые правила именования (Naming Conventions)

### 8.1. Общие принципы

| Правило | Пример | Причина |
|---------|--------|---------|
| Только латиница a-z, 0-9, _ | `cat_counterparties`, `doc_invoice` | PostgreSQL case folding, миграции |
| snake_case в БД | `counterparty_id` | Соответствие БД ↔ Go |
| CamelCase в Go коде | `Counterparty`, `GoodsReceipt` | Go-конвенция |
| kebab-case в REST URL | `/counterparties`, `/goods-receipt` | Читаемость, SEO |
| Макс. длина имени таблицы — 58 символов | Оставляем место под `_items`, `_balances` | PostgreSQL ограничения |

### 8.2. Префиксы таблиц

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           ПРЕФИКСЫ ТАБЛИЦ                                │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌──────────────────────────────────────────────────────────────────┐   │
│  │ cat_    │ Справочники           │ cat_counterparties             │   │
│  │         │                       │ cat_nomenclature               │   │
│  │         │                       │ cat_warehouses                 │   │
│  ├─────────┼───────────────────────┼────────────────────────────────┤   │
│  │ doc_    │ Документы (шапка)     │ doc_invoice                    │   │
│  │         │                       │ doc_goods_receipt              │   │
│  │         │ Табличные части       │ doc_invoice_items              │   │
│  │         │                       │ doc_goods_receipt_goods        │   │
│  ├─────────┼───────────────────────┼────────────────────────────────┤   │
│  │ reg_    │ Регистры накопления   │ reg_stock_movements            │   │
│  │         │   (движения)          │ reg_stock_balances             │   │
│  │         │   (остатки)           │ reg_stock_turnovers            │   │
│  │         │   (обороты)           │                                │   │
│  │         │ Регистры сведений     │ reg_currency_rates_info        │   │
│  │         │                       │ reg_barcodes_info              │   │
│  ├─────────┼───────────────────────┼────────────────────────────────┤   │
│  │ const_  │ Константы             │ const_company_settings         │   │
│  ├─────────┼───────────────────────┼────────────────────────────────┤   │
│  │ sys_    │ Системные таблицы     │ sys_sequences                  │   │
│  │         │                       │ sys_outbox                     │   │
│  │         │                       │ sys_audit                      │   │
│  │         │                       │ sys_sessions                   │   │
│  └─────────┴───────────────────────┴────────────────────────────────┘   │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### 8.3. REST API маршруты

| Тип объекта | Базовый путь | Пример |
|-------------|--------------|--------|
| Справочники | `/api/v1/catalog/{name}` | `GET /api/v1/catalog/counterparties` |
| Документы | `/api/v1/document/{name}` | `POST /api/v1/document/goods-receipt` |
| Регистры накопления | `/api/v1/registers/{name}` | `GET /api/v1/registers/stock/movements` |
| Регистры сведений | `/api/v1/registers/{name}` | `GET /api/v1/registers/currency-rates/slices` |
| Константы | `/api/v1/constants/{name}` | `GET /api/v1/constants/company-settings` |
| Метаданные | `/api/v1/meta/{type}/{name}` | `GET /api/v1/meta/layouts/invoice` |

**Специальные операции документов:**

| Операция | Endpoint | Метод |
|----------|----------|-------|
| Проведение | `/api/v1/document/{name}/{id}/post` | POST |
| Отмена проведения | `/api/v1/document/{name}/{id}/unpost` | POST |
| Копирование | `/api/v1/document/{name}/{id}/copy` | POST |
| Печать | `/api/v1/document/{name}/{id}/print?form=...` | GET |

---

# Часть IV. Roadmap и Этапы Разработки

## 9. Этапы реализации

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          ROADMAP РАЗРАБОТКИ                              │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ЭТАП 0: ФУНДАМЕНТ (Core & Infrastructure)                              │
│  ════════════════════════════════════════                               │
│  □ Структура проекта и go.mod                                           │
│  □ Base entities (ID, Timestamps, Version, Attributes)                  │
│  □ UUIDv7 генерация                                                     │
│  □ AppError структура ошибок                                            │
│  □ PostgreSQL connection pool (pgxpool)                                 │
│  □ Transaction Manager                                                   │
│  □ Миграции системных таблиц (sequences, outbox, audit)                 │
│  □ Graceful Shutdown                                                    │
│  □ Structured Logger + Trace ID                                         │
│  □ testcontainers-go для интеграционных тестов                          │
│                                         │                                │
│                                         ▼                                │
│  ЭТАП 1: СПРАВОЧНИКИ (Catalogs)                                         │
│  ═══════════════════════════════                                        │
│  □ Numerator сервис (автонумерация)                                     │
│  □ JWT Middleware + AccessScope                                         │
│  □ Справочник "Контрагенты" (CRUD + Hooks)                              │
│  □ Справочник "Номенклатура" (+ иерархия)                               │
│  □ Справочник "Склады"                                                  │
│  □ Справочник "Единицы измерения"                                       │
│  □ Справочник "Валюты"                                                  │
│                                         │                                │
│                                         ▼                                │
│  ЭТАП 2: ДОКУМЕНТЫ И ПРОВЕДЕНИЕ (Documents & Posting)                   │
│  ════════════════════════════════════════════════════                   │
│  □ Документ "Поступление товаров"                                       │
│  □ Регистр "Товары на складах" (movements + balances)                   │
│  □ Документ "Реализация" + контроль остатков                            │
│  □ Immutable Ledger + версионирование движений                          │
│  □ Delta-обновление остатков                                            │
│  □ Idempotency для проведения                                           │
│  □ Resource Ordering (предотвращение Deadlock)                          │
│                                         │                                │
│                                         ▼                                │
│  ЭТАП 3: АСИНХРОННОСТЬ (Background Jobs)                                │
│  ═══════════════════════════════════════                                │
│  □ Outbox Worker (Polling → Kafka/NATS)                                 │
│  □ Базовые отчеты (SQL-first)                                           │
│  □ Закрытие периода (Soft Close)                                        │
│                                         │                                │
│                                         ▼                                │
│  ЭТАП 4: РАСШИРЯЕМОСТЬ (Customization)                                  │
│  ═════════════════════════════════════                                  │
│  □ sys_custom_field_schemas                                             │
│  □ Schema Cache (In-Memory + NOTIFY/LISTEN)                             │
│  □ Dependency Checker (JSONB ссылки)                                    │
│  □ UI Hints endpoint (/meta/layouts)                                    │
│                                         │                                │
│                                         ▼                                │
│  ЭТАП 5: PRODUCTION READINESS                                           │
│  ═════════════════════════════                                          │
│  □ AuthService (Refresh tokens, Revoke)                                 │
│  □ RBAC (Roles + Permissions)                                           │
│  □ Audit с партиционированием                                           │
│  □ Prometheus метрики                                                   │
│  □ Healthcheck endpoints                                                │
│  □ OpenTelemetry tracing                                                │
│                                         │                                │
│                                         ▼                                │
│  ЭТАП 6: ИНТЕГРАЦИИ                                                     │
│  ═════════════════                                                      │
│  □ Event Bus (Kafka/NATS)                                               │
│  □ Webhooks                                                             │
│  □ Integration API                                                      │
│  □ Import/Export (Excel, CSV)                                           │
│                                         │                                │
│                                         ▼                                │
│  ЭТАП 7: UI (Web & Mobile)                                              │
│  ═════════════════════════                                              │
│  □ Next.js Admin Panel                                                  │
│  □ Metadata-driven forms                                                │
│  □ Data tables (AG Grid / TanStack)                                     │
│  □ Mobile App (React Native / Flutter)                                  │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

## 10. Метрики готовности

| Milestone | Критерии | Срок |
|-----------|----------|------|
| **MVP** | 5 справочников, 3 документа, Stock register, Auth | +6 недель |
| **Beta** | Все справочники, Workflow, Интеграции | +12 недель |
| **RC** | UI, Reports, Mobile | +18 недель |
| **GA** | Production-ready, Documentation | +22 недели |

---

# Часть V. Технические Паттерны

## 11. Проведение документов (Posting Engine)

### 11.1. Алгоритм проведения с контролем остатков

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    АЛГОРИТМ ПРОВЕДЕНИЯ ДОКУМЕНТА                         │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  1. IDEMPOTENCY CHECK                                                    │
│     ┌──────────────────────────────────────────────────────────────┐    │
│     │  IF idempotency_key EXISTS AND status = 'Success'            │    │
│     │      RETURN cached_result                                     │    │
│     │  IF idempotency_key EXISTS AND status = 'Pending'            │    │
│     │      AND created_at < NOW() - 1 minute                        │    │
│     │      ALLOW takeover (server crashed)                          │    │
│     └──────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  2. ПРЕДВАРИТЕЛЬНАЯ ВАЛИДАЦИЯ (БЕЗ транзакции)                          │
│     ┌──────────────────────────────────────────────────────────────┐    │
│     │  • Парсинг JSON                                               │    │
│     │  • Проверка обязательных полей                                │    │
│     │  • Проверка типов данных                                      │    │
│     │  → НЕ держим соединение с БД занятым                          │    │
│     └──────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  3. BEGIN TRANSACTION (REPEATABLE READ)                                 │
│     ┌──────────────────────────────────────────────────────────────┐    │
│     │  SET LOCAL statement_timeout = '30s';                         │    │
│     └──────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  4. СОРТИРОВКА РЕСУРСОВ (Resource Ordering)                             │
│     ┌──────────────────────────────────────────────────────────────┐    │
│     │  ORDER BY product_id ASC                                      │    │
│     │  → Предотвращение Deadlock при параллельных транзакциях       │    │
│     └──────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  5. ЧТЕНИЕ СТАРЫХ ДВИЖЕНИЙ                                              │
│     ┌──────────────────────────────────────────────────────────────┐    │
│     │  SELECT * FROM reg_stock_movements                            │    │
│     │  WHERE recorder_id = $1                                       │    │
│     │  AND recorder_version = (SELECT MAX(recorder_version) ...)    │    │
│     └──────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  6. РАСЧЕТ ДЕЛЬТЫ (In-Memory)                                           │
│     ┌──────────────────────────────────────────────────────────────┐    │
│     │  Delta[warehouse, product] = NewQty - OldQty                  │    │
│     └──────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  7. АТОМАРНОЕ ОБНОВЛЕНИЕ ОСТАТКОВ                                       │
│     ┌──────────────────────────────────────────────────────────────┐    │
│     │  UPDATE reg_stock_balances                                    │    │
│     │  SET quantity = quantity + $delta                             │    │
│     │  WHERE warehouse_id = $1 AND product_id = $2                  │    │
│     │        AND (quantity + $delta) >= 0  -- CHECK!                │    │
│     │  RETURNING quantity;                                          │    │
│     │                                                                │    │
│     │  IF RowsAffected = 0 → ROLLBACK + ErrInsufficientStock        │    │
│     └──────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  8. ЗАПИСЬ НОВЫХ ДВИЖЕНИЙ (pgx.CopyFrom)                                │
│     ┌──────────────────────────────────────────────────────────────┐    │
│     │  INSERT INTO reg_stock_movements                              │    │
│     │  (period, warehouse_id, product_id, recorder_id,              │    │
│     │   recorder_version, line_id, record_type, quantity)           │    │
│     │  VALUES (...), (...), ...                                     │    │
│     └──────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  9. COMMIT + UPDATE IDEMPOTENCY STATUS                                  │
│     ┌──────────────────────────────────────────────────────────────┐    │
│     │  UPDATE doc_xxx SET posted = true WHERE id = $1               │    │
│     │  UPDATE sys_idempotency SET status = 'Success' ...            │    │
│     └──────────────────────────────────────────────────────────────┘    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

## 12. Transactional Outbox Pattern

```
┌─────────────────────────────────────────────────────────────────────────┐
│                      TRANSACTIONAL OUTBOX PATTERN                        │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌─────────────────┐                                                    │
│  │   Application   │                                                    │
│  │                 │                                                    │
│  │  ┌───────────┐  │                                                    │
│  │  │ Service   │──┼──────────────────────────────────────────────┐    │
│  │  └───────────┘  │                                               │    │
│  │                 │                                               │    │
│  └────────┬────────┘                                               │    │
│           │                                                         │    │
│           │ ONE TRANSACTION                                         │    │
│           ▼                                                         │    │
│  ┌─────────────────────────────────────────────────────────────┐   │    │
│  │                     PostgreSQL                               │   │    │
│  │                                                               │   │    │
│  │  ┌─────────────────┐      ┌─────────────────┐                │   │    │
│  │  │  Business Data  │      │   sys_outbox    │                │   │    │
│  │  │  (doc_invoice)  │      │                 │                │   │    │
│  │  │                 │      │ id              │                │   │    │
│  │  │  UPDATE posted  │──────▶ aggregate_type  │                │   │    │
│  │  │  = true         │      │ aggregate_id    │                │   │    │
│  │  │                 │      │ event_type      │                │   │    │
│  │  └─────────────────┘      │ payload (JSONB) │                │   │    │
│  │                           │ is_published    │                │   │    │
│  │                           └─────────────────┘                │   │    │
│  │                                    ▲                          │   │    │
│  └────────────────────────────────────┼──────────────────────────┘   │    │
│                                       │                               │    │
│                                       │ POLLING (every 500ms)         │    │
│                                       │ or DEBEZIUM (CDC)             │    │
│                                       │                               │    │
│  ┌────────────────────────────────────┼──────────────────────────┐   │    │
│  │                    Outbox Worker   │                          │   │    │
│  │                                    │                          │   │    │
│  │  1. SELECT * FROM sys_outbox       │                          │   │    │
│  │     WHERE is_published = false     │                          │   │    │
│  │     LIMIT 100                      │                          │   │    │
│  │     FOR UPDATE SKIP LOCKED         │                          │   │    │
│  │                                    │                          │   │    │
│  │  2. PUBLISH to Kafka/NATS ─────────┼──────────────────────┐   │   │    │
│  │                                    │                      │   │   │    │
│  │  3. DELETE FROM sys_outbox         │                      │   │   │    │
│  │     WHERE id IN (...)              │                      │   │   │    │
│  │                                    │                      │   │   │    │
│  └────────────────────────────────────┘                      │   │   │    │
│                                                               │   │   │    │
│                                                               ▼   │   │    │
│                                              ┌──────────────────┐│   │    │
│                                              │  Message Broker  ││   │    │
│                                              │  (Kafka/NATS)    ││   │    │
│                                              │                  ││   │    │
│                                              │  Topic:          ││   │    │
│                                              │  invoice.posted  ││   │    │
│                                              └──────────────────┘│   │    │
│                                                                   │   │    │
│  ГАРАНТИЯ: At-least-once delivery                                │   │    │
│  События никогда не теряются, но могут дублироваться             │   │    │
│  → Consumers должны быть идемпотентными                          │   │    │
│                                                                   │   │    │
└───────────────────────────────────────────────────────────────────┘   │    │
```

## 13. Multi-Tenancy через Database-per-Tenant

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    MULTI-TENANCY: DATABASE-PER-TENANT                    │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                   PostgreSQL (Кластер)                          │    │
│  │                                                                   │    │
│  │  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐     │    │
│  │  │   Database:    │  │   Database:    │  │   Database:    │     │    │
│  │  │   tenant_acme  │  │  tenant_globex │  │ tenant_initech │     │    │
│  │  │                │  │                │  │                │     │    │
│  │  │ cat_counterp.. │  │ cat_counterp.. │  │ cat_counterp.. │     │    │
│  │  │ doc_invoice    │  │ doc_invoice    │  │ doc_invoice    │     │    │
│  │  │ reg_stock_...  │  │ reg_stock_...  │  │ reg_stock_...  │     │    │
│  │  │ users, roles   │  │ users, roles   │  │ users, roles   │     │    │
│  │  └────────────────┘  └────────────────┘  └────────────────┘     │    │
│  │                                                                   │    │
│  │  ┌─────────────────────────────────────────────────────────┐    │    │
│  │  │  Meta-Database: metapus_meta                            │    │    │
│  │  │                                                          │    │    │
│  │  │  tenants (slug, db_name, db_host, status, plan)         │    │    │
│  │  │  tenant_migrations, tenant_audit                         │    │    │
│  │  └─────────────────────────────────────────────────────────┘    │    │
│  │                                                                   │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                                                          │
│  АРХИТЕКТУРА:                                                           │
│  ═══════════                                                            │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────┐    │
│  │  internal/core/tenant/                                         │    │
│  │  ├── types.go      # Tenant struct (ID, Slug, DBName, Status)  │    │
│  │  ├── context.go    # WithPool, WithTxManager, WithTenant       │    │
│  │  ├── registry.go   # PostgresRegistry для meta-database        │    │
│  │  └── manager.go    # MultiTenantManager (пулы соединений)      │    │
│  └────────────────────────────────────────────────────────────────┘    │
│                                                                          │
│  МЕХАНИЗМ ИЗОЛЯЦИИ:                                                     │
│  ══════════════════                                                     │
│                                                                          │
│  1. HTTP Request приходит с заголовком X-Tenant-ID                      │
│                                                                          │
│  2. Middleware TenantDB:                                                │
│     a) Получает Tenant из Registry по tenantID (UUID)                   │
│     b) Получает/создаёт Pool из MultiTenantManager                     │
│     c) Создаёт TxManager для данного запроса                           │
│     d) Инжектирует Pool, TxManager, Tenant в context                   │
│                                                                          │
│  3. Repositories получают TxManager из контекста:                       │
│     txm := tenant.MustGetTxManager(ctx)                                 │
│                                                                          │
│  4. Все SQL запросы идут в базу данных конкретного тенанта             │
│     БЕЗ фильтрации по tenant (физическая изоляция)                     │
│                                                                          │
│  MULTITENANTMANAGER:                                                    │
│  ═══════════════════                                                    │
│  • Ленивая загрузка пулов соединений                                   │
│  • Eviction неиспользуемых пулов (PoolIdleTimeout)                     │
│  • Health checks для активных пулов                                     │
│  • Reference counting для graceful shutdown                            │
│  • Лимит на общее количество пулов (MaxTotalPools)                     │
│                                                                          │
│  ПРЕИМУЩЕСТВА:                                                          │
│  • Полная физическая изоляция данных                                   │
│  • Простые бэкапы и восстановление отдельных клиентов                  │
│  • Возможность размещения на разных серверах                           │
│  • Независимые миграции для каждого тенанта                            │
│  • Нет фильтрации по tenant в запросах                                 │
│  • Проще масштабирование (шардинг по тенантам)                         │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### 13.1. Пример Middleware TenantDB

```go
// infrastructure/http/v1/middleware/tenant.go
func TenantDB(manager *tenant.Manager) gin.HandlerFunc {
    return func(c *gin.Context) {
        ctx := c.Request.Context()
        
        // 1. Извлекаем tenantID (UUID) из заголовка
        tenantID := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
        if tenantID == "" {
            c.AbortWithStatusJSON(401, gin.H{"error": "X-Tenant-ID required"})
            return
        }
        
        // 2. Получаем пул из менеджера
        managedPool, err := manager.GetPool(ctx, tenantID)
        if err != nil {
            c.AbortWithStatusJSON(404, gin.H{"error": "Tenant not found"})
            return
        }
        
        // 3. Reference counting для graceful shutdown
        managedPool.AcquireRef()
        defer managedPool.ReleaseRef()
        
        // 4. Создаём TxManager для запроса
        txManager := postgres.NewTxManagerFromRawPool(managedPool.Pool())
        
        // 5. Инжектируем в контекст
        ctx = tenant.WithPool(ctx, managedPool.Pool())
        ctx = tenant.WithTxManager(ctx, txManager)
        ctx = tenant.WithTenant(ctx, managedPool.Tenant())
        c.Request = c.Request.WithContext(ctx)
        
        c.Next()
    }
}
```

### 13.2. Пример получения TxManager в репозитории

```go
// infrastructure/storage/postgres/catalog_repo/base.go
type BaseCatalogRepo[T any] struct {
    tableName  string
    selectCols []string
}

// getTxManager получает TxManager из контекста (Database-per-Tenant)
func (r *BaseCatalogRepo[T]) getTxManager(ctx context.Context) *postgres.TxManager {
    return tenant.MustGetTxManager(ctx)
}

func (r *BaseCatalogRepo[T]) GetByID(ctx context.Context, entityID id.ID) (T, error) {
    // TxManager из контекста, изоляция обеспечивается выбором БД
    querier := r.getTxManager(ctx).GetQuerier(ctx)
    // ... SQL без фильтрации по tenant ...
}
```

### 13.3. CLI для управления тенантами

```bash
# Создание нового тенанта
go run cmd/tenant/main.go create --slug=acme --name="ACME Corp"

# Список тенантов
go run cmd/tenant/main.go list

# Миграции для всех тенантов
go run cmd/tenant/main.go migrate

# Приостановка тенанта
go run cmd/tenant/main.go suspend --slug=acme

# Активация тенанта
go run cmd/tenant/main.go activate --slug=acme
```

### 13.4. Background Workers в Multi-Tenant среде

```go
// cmd/worker/main.go
// Воркер итерирует по всем активным тенантам

type MultiTenantWorker struct {
    registry tenant.Registry
    manager  *tenant.Manager
}

func (w *MultiTenantWorker) Run(ctx context.Context) error {
    // Получаем список активных тенантов
    tenants, err := w.registry.ListActive(ctx)
    if err != nil {
        return err
    }
    
    for _, t := range tenants {
        // Получаем пул для тенанта
        pool, err := w.manager.GetPool(ctx, t.Slug)
        if err != nil {
            log.Error("failed to get pool", "tenant", t.Slug, "error", err)
            continue
        }
        
        // Создаём контекст с TxManager для тенанта
        txm := postgres.NewTxManagerFromRawPool(pool.Pool())
        tenantCtx := tenant.WithTxManager(ctx, txm)
        tenantCtx = tenant.WithTenant(tenantCtx, &t)
        
        // Выполняем работу для тенанта
        if err := w.processOutbox(tenantCtx); err != nil {
            log.Error("outbox processing failed", "tenant", t.Slug, "error", err)
        }
    }
    
    return nil
}
```

---

# Часть VI. Observability и Production

## 14. Метрики и Мониторинг

### 14.1. Обязательные Prometheus метрики

```yaml
# Runtime
- go_goroutines
- go_memstats_alloc_bytes
- go_gc_duration_seconds

# HTTP
- http_requests_total{method, path, status}
- http_request_duration_seconds{method, path}

# Database
- db_pool_acquired_conns
- db_pool_idle_conns
- db_query_duration_seconds{query_type}

# Business
- posting_operations_total{document_type, status}
- outbox_messages_published_total
- outbox_messages_failed_total

# Multi-tenancy (Database-per-Tenant)
- tenant_pool_count                    # Количество активных пулов
- tenant_pool_connections{tenant_slug} # Соединения по тенанту
- tenant_request_duration_seconds{tenant_slug}
- tenant_worker_iterations_total{worker, tenant_slug}
```

### 14.2. Healthcheck Endpoints

```
GET /live    → Приложение запущено (для Kubernetes liveness probe)
GET /ready   → Приложение готово принимать запросы (readiness probe)
              Проверяет: DB connection, Redis, etc.
```

## 15. Аудит и Ретенция

### 15.1. Таблица аудита с партиционированием

```sql
CREATE TABLE sys_audit (
    id UUID DEFAULT gen_random_uuid_v7(),
    entity_type VARCHAR(50) NOT NULL,
    entity_id UUID NOT NULL,
    action VARCHAR(10) NOT NULL, -- CREATE, UPDATE, DELETE
    user_id UUID NOT NULL,
    changes_compressed BYTEA, -- ZSTD compressed JSON diff
    compression_method VARCHAR(10) DEFAULT 'zstd',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- Партиции создаются автоматически воркером
CREATE TABLE sys_audit_y2024m01 PARTITION OF sys_audit
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
```

### 15.2. Audit Cleaner Worker

```go
// infrastructure/worker/audit_cleaner.go
func (w *AuditCleaner) Run(ctx context.Context) error {
    retentionDays := 90
    cutoffDate := time.Now().AddDate(0, 0, -retentionDays)

    // Находим старые партиции
    partitions, _ := w.getOldPartitions(ctx, cutoffDate)

    for _, partition := range partitions {
        // Мгновенная операция O(1)
        _, err := w.db.Exec(ctx, fmt.Sprintf("DROP TABLE %s", partition))
        if err != nil {
            return err
        }
    }

    // Создаем партиции на будущее
    return w.ensureFuturePartitions(ctx, 3) // На 3 месяца вперед
}
```

---

# Часть VII. Frontend и UI

## 16. Metadata-Driven UI

### 16.1. Endpoint для подсказок UI

```
GET /api/v1/meta/layouts/{entity}
```

**Response:**
```json
{
  "entity": "Invoice",
  "type": "document",
  "layout": {
    "groups": [
      {
        "title": "Основное",
        "fields": ["number", "date", "counterparty_id"]
      },
      {
        "title": "Склад",
        "fields": ["warehouse_id"]
      }
    ]
  },
  "fields": {
    "counterparty_id": {
      "widget": "ReferenceSelect",
      "refType": "Counterparty",
      "displayField": "name",
      "required": true
    },
    "date": {
      "widget": "DatePicker",
      "format": "DD.MM.YYYY"
    }
  },
  "tableParts": [
    {
      "name": "items",
      "title": "Товары",
      "columns": [
        {"field": "product_id", "widget": "ReferenceSelect", "width": 300},
        {"field": "quantity", "widget": "NumberInput", "width": 100},
        {"field": "price", "widget": "MoneyInput", "width": 120},
        {"field": "amount", "widget": "MoneyDisplay", "width": 120, "readonly": true}
      ]
    }
  ]
}
```

### 16.2. Структура Web-приложения

```
/web
├── app/
│   ├── (auth)/
│   │   ├── login/page.tsx
│   │   └── register/page.tsx
│   ├── (dashboard)/
│   │   ├── layout.tsx
│   │   ├── page.tsx                  # Dashboard
│   │   ├── catalogs/
│   │   │   ├── counterparties/
│   │   │   │   ├── page.tsx          # Список
│   │   │   │   └── [id]/page.tsx     # Форма
│   │   │   └── nomenclature/
│   │   ├── documents/
│   │   │   ├── goods-receipt/
│   │   │   └── invoice/
│   │   └── reports/
│   └── api/                          # API Routes (proxy)
├── components/
│   ├── ui/                           # shadcn/ui
│   ├── forms/
│   │   ├── generic-form.tsx          # Metadata-driven form
│   │   ├── reference-select.tsx
│   │   └── table-part.tsx
│   └── tables/
│       └── data-table.tsx            # TanStack Table
└── lib/
    ├── api-client.ts
    └── hooks/
```

---

# Часть VIII. Словарь Терминов

## 17. Mapping концепций

| Концепция | Реализация в Go | Расположение |
|-----------|-----------------|--------------|
| **Объект** | `struct` с тегами `db` | `domain/{type}/{name}/model.go` |
| **Табличная часть** | `[]Struct` (слайс) | Внутри структуры документа |
| **Ссылка (Ref)** | `string` (UUID) | Поля `SomeID` |
| **Менеджер** | `Repository Interface` | `domain/{type}/{name}/repo.go` |
| **Модуль объекта** | Методы структуры | `model.go` |
| **Проведение** | `Post()` в Service | `domain/documents/{name}/service/posting.go` |
| **Подписка** | `EventSubscriber` | `infrastructure/events/` |
| **Регламентное задание** | Worker Job | `infrastructure/worker/handlers/` |

---

# Часть IX. Чеклисты

## 18. Чеклист добавления нового справочника

- [ ] Создать миграцию `db/migrations/XXX_cat_{name}.sql`
- [ ] Создать `internal/domain/catalogs/{name}/model.go`
- [ ] Реализовать `Validate()` метод
- [ ] Создать `internal/domain/catalogs/{name}/repo.go` (интерфейс)
- [ ] Создать `internal/domain/catalogs/{name}/service.go`
- [ ] Реализовать репозиторий в `infrastructure/storage/postgres/catalog_repo/{name}.go`
- [ ] Создать DTO в `infrastructure/http/v1/dto/catalog.go`
- [ ] Добавить handler в `infrastructure/http/v1/handlers/catalog_handler.go`
- [ ] Зарегистрировать маршруты в `router.go`
- [ ] Написать интеграционные тесты

## 19. Чеклист добавления нового документа

- [ ] Создать миграции: шапка + табличные части
- [ ] Создать `model.go` с шапкой и табличными частями
- [ ] Реализовать `Validate()`, `Calculate()`, `GenerateMovements()`
- [ ] Создать интерфейс репозитория
- [ ] Реализовать `service/crud.go` (Create, Update, Delete)
- [ ] Реализовать `service/posting.go` (Post, Unpost)
- [ ] Добавить записи в регистры при проведении
- [ ] Реализовать контроль остатков (если расходный документ)
- [ ] Добавить идемпотентность
- [ ] Добавить события в Outbox
- [ ] Написать интеграционные тесты

---

# Часть X. Заключение

Данный документ представляет собой полное руководство по проектированию и разработке платформы Metapus. Он объединяет:

1. **Философию и принципы** — чёткое понимание "почему" мы делаем так, а не иначе
2. **Архитектуру метаданных** — полное описание всех типов объектов системы
3. **Структуру проекта** — детальную организацию кода по слоям Clean Architecture
4. **Правила именования** — единый стандарт для БД, Go-кода и API
5. **Технические паттерны** — Immutable Ledger, Transactional Outbox, Database-per-Tenant
6. **Roadmap** — поэтапный план реализации с метриками готовности
7. **Чеклисты** — практические руководства для разработчиков

Любое архитектурное решение должно проверяться на соответствие принципам, изложенным в этом документе.