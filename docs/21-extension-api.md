# Справочник API Расширений (v1.0.0)

> **Версия API:** `internal/platform.ExtensionAPIVersion = "1.0.0"`
> Версионирование следует семантическому версионированию (semver). Мажорный релиз = изменение обязательного интерфейса. Минорный релиз = добавлен новый опциональный интерфейс.

---

## Обзор

Metapus использует **Трехслойную Модель Расширений** с проверкой безопасности во время компиляции Go:

| Слой | Назначение | Кто пишет | Расположение |
|-------|---------|-----------|----------|
| **Core Платформы** | Фреймворк: дженерики, CRUD-пайплайн, движок проводок, безопасность | Команда Metapus | `internal/core/`, `internal/domain/`, `internal/infrastructure/` |
| **Бизнес-контент** | Встроенные сущности (Контрагент, Поступление товаров и т.д.) | Команда Metapus | `internal/content/` |
| **Клиентские расширения** | Кастомные сущности, хуки, регистры для каждого клиента | Интегратор | `extensions/` или внешний Go-модуль |

---

## Точки расширения

### 1. Новая сущность Catalog (Справочник)

Реализуйте `v1.CatalogRegistration` (обязательно) + опциональные интерфейсы из `internal/platform/`:

```go
// Обязательные (ДОЛЖНЫ быть реализованы — контракт мажорной версии)
type CatalogRegistration interface {
    RoutePrefix() string                      // Путь URL: "vehicles"
    Permission() string                       // Префикс прав: "catalog:vehicle"
    EntityName() string                       // Имя для метаданных: "Vehicle"
    Build(deps CatalogDeps) CatalogRouteHandler
}

// Опциональные (реализуйте для более богатых метаданных — расширение минорной версии)
type Presentable interface {
    EntityPresentation() metadata.Presentation // UI метки
}
type Inspectable interface {
    EntityStruct() interface{}                 // Нулевое значение для metadata.Inspect()
}
type Labeled interface {
    EntityLabel() string                       // Метка для боковой панели
}
type ReferenceProvider interface {
    ReferenceTypes() []string                  // Например, ["vehicle"]
}
```

**Регистрация:**
```go
factoryReg := v1.NewFactoryRegistry()
content.RegisterDefaults(factoryReg)
factoryReg.RegisterCatalog(&VehicleRegistration{})
```

### 2. Новая сущность Document (Документ)

Реализуйте `v1.DocumentRegistration` + опциональные интерфейсы:

```go
type DocumentRegistration interface {
    RoutePrefix() string
    Permission() string
    EntityName() string
    Build(deps DocumentDeps) DocumentRouteHandler
}
```

**Регистрация:**
```go
factoryReg.RegisterDocument(&WaybillRegistration{})
```

### 3. Хуки жизненного цикла на существующих сущностях

Используйте `domain.HookRegistry[T]` для инъекции бизнес-логики в стандартные сущности:

```go
// Доступ через service.Hooks()
goodsReceiptService.Hooks().OnBeforeCreate(func(ctx context.Context, doc *goods_receipt.GoodsReceipt) error {
    // Проверка кредитного лимита
    return nil
})

// С указанием приоритета (меньший выполняется раньше)
goodsReceiptService.Hooks().OnWithPriority(domain.BeforeCreate, 10, "credit-check", func(ctx context.Context, doc *goods_receipt.GoodsReceipt) error {
    return nil
})
```

**События хуков:** `BeforeCreate`, `AfterCreate`, `BeforeUpdate`, `AfterUpdate`, `BeforeDelete`, `AfterDelete`

### 4. Кастомные регистры (Посетители и Регистраторы проводок)

Расширяйте движок проводок новыми типами регистров:

```go
// Посетитель (Visitor): проверяет документ, генерирует движения
type RegisterVisitor interface {
    Name() string
    CollectMovements(ctx context.Context, doc Postable, set *MovementSet) error
}

// Регистратор (Recorder): сохраняет/отменяет движения для регистра
type RegisterRecorder interface {
    Name() string
    Record(ctx context.Context, docID id.ID, movements *MovementSet) error
    Reverse(ctx context.Context, docID id.ID) error
}

// Регистрация:
engine.AddVisitor(&FuelVisitor{})
engine.AddRecorder(&FuelRecorder{repo})
```

### 5. Дополнительные реквизиты (No Code — JSONB)

Добавляйте пользовательские поля в любую сущность через таблицу `sys_custom_field_schemas` (в базе данных каждого арендатора):

| Колонка | Описание |
|--------|-------------|
| `entity_type` | Например, "Counterparty" |
| `field_name` | Например, "credit_limit" |
| `field_type` | string, integer, decimal, boolean, date, reference, enum |
| `is_required` | Обязательность значения |
| `validation_rules` | JSONB с правилами валидации |

Кастомные поля хранятся в столбце JSONB `attributes` и автоматически объединяются с метаданными через `SchemaCache` и механизм инвалидации LISTEN/NOTIFY.

### 6. Правила Политик (CEL)

Бизнес-правила через CEL выражения (перекомпиляция не требуется):

```
// Заблокировать проведение документов свыше 1 миллиона
doc.totalAmount > 1000000 && action == "post"
```

### 7. Виджеты Дашборда (Frontend)

```typescript
import { registerWidget } from "@/lib/widget-registry"

registerWidget({
    type: "fuel-chart",
    label: "Расход топлива",
    icon: Fuel,
    allowedSizes: ["4x2", "4x3"],
    defaultSize: "4x3",
    component: lazy(() => import("./fuel-chart-renderer")),
    // ...
})
```

---

## Scaffold CLI

Генерация нового расширения за считанные секунды:

```bash
# Сущность Справочник (Catalog)
go run cmd/scaffold/main.go --name employee --type catalog

# Сущность Документ (Document)
go run cmd/scaffold/main.go --name waybill --type document

# Кастомное имя таблицы БД
go run cmd/scaffold/main.go --name payment_order --type document --table doc_payment_orders
```

Весь сгенерированный код сразу компилируется — настройте поля и бизнес-логику так, как вам нужно.

## Конвенция миграций

- **Миграции ядра:** `db/migrations/00001-09999_*.sql`
- **Номера миграций расширений:** `extensions/<name>/migrations/10001+_*.sql`
- Автоматически находятся командой `tenant migrate --all` (сканирующей папки `extensions/*/migrations/`)
- Предотвращайте конфликты номеров, используя диапазон 10000+ для всех клиентских расширений.

## Шаблон Расширения

См. пример в папке `extensions/vehicle/`. Ключевые файлы:

| Файл | Назначение |
|------|---------|
| `model.go` | Доменная модель (встраивает (embed) `platform.Catalog` из `internal/platform/`) |
| `service.go` | Бизнес-логика (использует API: `platform.Generator`, `platform.NewConflict` и т.д.) |
| `dto.go` | DTO Запросов/Ответов (использует API: `platform.ID`, `platform.Attributes`, `platform.ParseID`) |
| `registration.go` | Реализует `v1.CatalogRegistration` + опционально `platform.Presentable` |
| `register.go` | Точка входа: `Register(reg, cfg)` |
| `repo.go` | Интерфейс репозитория (встраивает `domain.CatalogRepository`) |
| `migrations/` | SQL-миграции расширения (10000+) |

### Конвенция импортов

Расширения должны импортировать базовые интерфейсы и типы данных из `internal/platform/` для стабильности:

```go
import "metapus/internal/platform"

// platform.Catalog       — базовый тип сущности
// platform.ID            — идентификатор UUIDv7
// platform.ParseID()     — конвертация строки в ID
// platform.NewCatalog()  — конструктор сущности
// platform.NewValidation(), NewConflict(), NewNotFound() — ошибки
// platform.Generator     — интерфейс нумератора
// platform.DefaultNumeratorConfig() — конфиг нумератора
```

Пакеты инфраструктуры (`v1`, `handlers`, `domain`) также считаются стабильными для расширений внутри (in-repo) проекта.

### Подключение в main.go

```go
import (
    "metapus/extensions/vehicle"
    "metapus/internal/platform"
)
// ...
vehicle.Register(factoryReg, platform.ExtensionConfig{
    PostingEngine: postingEngine, // nil для расширений без документов
})
```

---

## Frontend Расширения

### Автоматическое обнаружение (Auto-Discovery)

Сущности расширений **автоматически обнаруживаются** через API-запрос к эндпоинту `/api/v1/meta/entities`.
Для базовых страниц списков и форм переопределение на фронтенде не требуется.

Боковая панель (Sidebar) автоматически показывает сущности расширений в разделе «Расширения».
Ссылки на сущности из расширений используют специальный catch-all маршрут `/ext/{entityType}/{routePrefix}`.

### Кастомные Компоненты (Опционально)

Для того чтобы переопределить авто-генерируемый UI на кастомные компоненты:

```typescript
import { entityRegistry } from "@/lib/entity-registry"

entityRegistry.registerCatalog({
    entityType: "catalog",
    entityName: "Vehicle",
    routePrefix: "vehicles",
    listColumns: [...],
    formComponent: lazy(() => import("./vehicle-form")),
})
```

### Динамическая Маршрутизация

- **Список:** `/ext/catalog/vehicles` → Компонент `AutoList` (metadata-driven)
- **Форма:** `/ext/catalog/vehicles/new` или `/ext/catalog/vehicles/:id` → Компонент `AutoForm` (metadata-driven)

### Фолбэк на основе Метаданных (Metadata Fallback)

Если кастомные компоненты (`listComponent`/`formComponent`) не зарегистрированы, система автоматически сгенерирует UI, опираясь на метаданные, полученные из эндпоинта `/api/v1/meta/:name`.

---

## Правила Совместимости

| Тип изменения | Semver | Сломает ли клиента? |
|-------------|--------|---------------|
| Новое поле в `entity.Catalog` | minor | Нет |
| Новый опциональный интерфейс (`Presentable`) | minor | Нет |
| Новый базовый метод в `CatalogRegistration` | **major** | **Да** |
| Изменена сигнатура hook-обработчика | **major** | **Да** |
| Добавлен Visitor/Recorder | minor | Нет |
| Новое middleware | minor | Нет |

**Проверка совместимости:**
```bash
make check-extensions   # собирает расширения extensions/ против текущего ядра
```
