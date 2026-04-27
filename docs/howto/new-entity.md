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
    PlateNumber string `db:"plate_number" json:"plateNumber"`
}

func (Vehicle) TableName() string { return "cat_vehicles" }
```

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
