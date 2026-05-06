# Как добавить новую сущность

> **TL;DR:** Пошаговая инструкция по добавлению нового справочника. Включает миграцию, модель, сервис и регистрацию. Фронтенд-интерфейс сгенерируется автоматически.

> **Тип:** How-To
> **Аудитория:** Developer

---

## Шаг 1: Миграция БД (Таблица)

Создайте файл `db/migrations/NNNNN_cat_vehicles.sql`.
Таблица должна содержать стандартные поля и CDC триггеры.

```sql
CREATE TABLE cat_vehicles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version INT NOT NULL DEFAULT 1,
    attributes JSONB DEFAULT '{}',
    
    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(),
    
    code VARCHAR(50) NOT NULL,
    name VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_vehicles(id),
    is_folder BOOLEAN NOT NULL DEFAULT FALSE,
    
    plate_number VARCHAR(20) NOT NULL -- Ваше кастомное поле
);

-- Индексы и триггеры
CREATE UNIQUE INDEX idx_cat_vehicles_code ON cat_vehicles (code) WHERE deletion_mark = FALSE;
CREATE TRIGGER trg_cat_vehicles_txid BEFORE UPDATE ON cat_vehicles FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_cat_vehicles_soft_delete BEFORE UPDATE OF deletion_mark ON cat_vehicles FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
```

## Шаг 2: Доменная модель

Создайте пакет `internal/content/catalogs/vehicle/model.go`.
Встройте `entity.Catalog`.

```go
type Vehicle struct {
    entity.Catalog
    PlateNumber  string `db:"plate_number"  json:"plateNumber"  meta:"label:Гос. номер"`
    WarehouseID  *id.ID `db:"warehouse_id"  json:"warehouseId,omitempty" meta:"label:Гараж,ref:warehouse"`
}

func (Vehicle) TableName() string { return "cat_vehicles" }
```

> [!IMPORTANT]
> **Каждое поле** типа `id.ID` / `*id.ID`, ссылающееся на другую сущность, **обязано** иметь `ref:<refType>` в `meta`-теге.
> Без него AutoForm отрисует UUID как текстовое поле вместо combobox-пикера.
> Подробнее — в секции [Ссылочные поля](#ссылочные-поля-meta-ref-теги).

## Шаг 3: DTO и Validation

Определите DTO для запросов (`CreateDTO`, `UpdateDTO`) и напишите метод `Validate(ctx context.Context) error`.

## Шаг 4: Service и Repo

Так как это справочник, используйте базовые абстракции из ядра. Вам не нужно писать CRUD-запросы руками.

```go
type Repository struct {
    *repo.BaseCatalogRepo[Vehicle]
}

type Service struct {
    *service.CatalogService[Vehicle]
}
```

## Шаг 5: Регистрация (Composition Root)

В пакете `vehicle` создайте `registration.go`, реализующий интерфейс `v1.CatalogRegistration`:

```go
type Registration struct{}

func (r *Registration) RoutePrefix() string { return "vehicles" }
func (r *Registration) Permission() string { return "catalog:vehicle" }
func (r *Registration) EntityName() string { return "Vehicle" }

func (r *Registration) Build(deps v1.CatalogDeps) v1.CatalogRouteHandler {
    // Инициализация Repo, Service и Handler
    repo := NewRepository(...)
    svc := NewService(...)
    return v1.NewCatalogHandler(svc)
}
```

В файле `main.go` зарегистрируйте сущность:
```go
factoryReg.RegisterCatalog(&vehicle.Registration{})
```

**Готово!**
Фронтенд автоматически увидит `Vehicle` через эндпоинт метаданных и сгенерирует страницу списка, форму добавления и элементы бокового меню.

---

## Ссылочные поля (meta ref-теги)

Каждое поле модели, ссылающееся на другую сущность (`id.ID` или `*id.ID` с FK), **обязано** содержать `ref:<refType>` в struct-теге `meta`. Это критически важно — без `ref:` тега:

- **AutoForm** рендерит UUID как текстовый `<Input>` вместо `<ReferenceField>` (combobox с поиском)
- **Списки** не резолвят имя связанной сущности (показывают UUID или пустое поле)
- **FilterSidebar** не сможет предложить фильтр по ссылочному полю

### Формат meta-тега

```
meta:"label:<Отображаемое имя>,ref:<referenceType>"
```

| Часть | Описание | Пример |
|-------|----------|--------|
| `label:` | Название поля в UI | `label:Поставщик` |
| `ref:` | Ключ регистрации (`ReferenceTypes()`) | `ref:supplier` |

### Как определить правильный `ref:<refType>`

`refType` — это строка, возвращаемая методом `ReferenceTypes()` регистрации целевого справочника.
Найдите в `internal/content/catalog_registrations.go`:

```go
func (r *CounterpartyRegistration) ReferenceTypes() []string { return []string{"supplier", "customer"} }
func (r *WarehouseRegistration)    ReferenceTypes() []string { return []string{"warehouse"} }
func (r *NomenclatureRegistration) ReferenceTypes() []string { return []string{"product"} }
func (r *UnitRegistration)         ReferenceTypes() []string { return []string{"unit"} }
func (r *CurrencyRegistration)     ReferenceTypes() []string { return []string{"currency"} }
func (r *TokenRegistration)        ReferenceTypes() []string { return []string{"token"} }
func (r *BlockchainNetworkRegistration) ReferenceTypes() []string { return []string{"blockchain_network"} }
func (r *MerchantRegistration)     ReferenceTypes() []string { return []string{"merchant"} }
func (r *WalletRegistration)       ReferenceTypes() []string { return []string{"wallet"} }
```

### Примеры

```go
// ✅ Правильно — ref: тег указан
SupplierID  id.ID  `db:"supplier_id"  json:"supplierId"  meta:"label:Поставщик,ref:supplier"`
WarehouseID id.ID  `db:"warehouse_id" json:"warehouseId" meta:"label:Склад,ref:warehouse"`
ContractID  *id.ID `db:"contract_id"  json:"contractId,omitempty" meta:"label:Договор,ref:contract"`
NetworkID   id.ID  `db:"network_id"   json:"networkId"   meta:"label:Сеть,ref:blockchain_network"`

// ❌ Неправильно — AutoForm покажет UUID как текст
SupplierID  id.ID  `db:"supplier_id"  json:"supplierId"  meta:"label:Поставщик"`
NetworkID   id.ID  `db:"network_id"   json:"networkId"   meta:"label:Сеть"`
```

> [!CAUTION]
> **Пропуск `ref:` — частая ошибка.** Данные в API и БД будут корректны, но UI будет показывать сырые UUID вместо человекочитаемых имён. Ошибка часто обнаруживается поздно — только при тестировании форм.

### Как работает цепочка

```
Go: meta:"ref:warehouse"  →  Metadata API: {type: "reference", referenceType: "warehouse"}
  →  Frontend AutoForm: resolveRefEndpoint("warehouse")
    →  useMetadataStore.byKey["warehouse"] → routePrefix: "warehouses"
      →  ReferenceField apiEndpoint="/catalog/warehouses"
```

---

## Чеклист

- [ ] Миграция: таблица `cat_*` с CDC-колонками и триггерами
- [ ] Модель: `entity.Catalog` embed, **все FK-поля имеют `ref:` в `meta`-теге**
- [ ] Validate: чистая функция без обращений к БД
- [ ] Service / Repo: embed `BaseCatalogRepo[T]`, `CatalogService[T]`
- [ ] Регистрация: `ReferenceTypes()` возвращает корректные ключи
- [ ] Проверка: `go build ./... && golangci-lint run ./...`
- [ ] Проверка: открыть форму в UI — ссылочные поля отображаются как combobox, не как текст
