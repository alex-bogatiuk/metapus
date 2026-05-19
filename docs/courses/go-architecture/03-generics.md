# Модуль 3: Сила Generics в Go 1.24

## Проблема без Generics

До Go 1.18, если нужен CRUD для `User` — пишешь `CreateUser`, `GetUserByID`. Появляется `Product` — пишешь `CreateProduct`, `GetProductByID`. Тонны одинакового (boilerplate) кода.

С **Generics** мы написали это **один раз для всех**!

## BaseCatalogRepo[T] — один репозиторий для всех справочников

```go
type BaseCatalogRepo[T entity.CatalogEntity] struct {
    tableName string
    pool      Pool
}

// Один метод GetByID работает для ЛЮБОГО справочника
func (r *BaseCatalogRepo[T]) GetByID(ctx context.Context, id uuid.UUID) (T, error) {
    var entity T = new(T)  // ← Создаём пустой экземпляр нужного типа
    query := fmt.Sprintf("SELECT * FROM %s WHERE id = $1", r.tableName)
    err := r.pool.QueryRow(ctx, query, id).Scan(entity)
    return entity, err
}
```

Теперь создать репозиторий для нового справочника — **одна строка**:

```go
// Вместо 200 строк boilerplate:
currencyRepo := NewBaseCatalogRepo[*currency.Currency]("cat_currencies", pool)
warehouseRepo := NewBaseCatalogRepo[*warehouse.Warehouse]("cat_warehouses", pool)
unitRepo := NewBaseCatalogRepo[*unit.Unit]("cat_units", pool)
```

## Как Go работает с указателями в Generics

В нашем коде `T` — почти всегда **указатель** (`*catalog.Currency`). Указатель — адрес в памяти, где лежат реальные данные.

### Что будет, если написать `var entity T`?

У всех типов Go есть нулевое значение:
- `int` → `0`
- `string` → `""`
- `*Currency` → `nil` (указатель в никуда!)

```go
var entity T          // entity = nil (пустой указатель)
entity.Name = "USD"   // 💥 ПАНИКА! Пишем в nil-указатель
```

### Решение: `new(T)`

```go
entity := new(T)      // Выделяем память, entity указывает на пустую структуру
entity.Name = "USD"   // ✅ Работает!
```

## Type Constraints (Ограничения типов)

```go
type CatalogEntity interface {
    GetID() uuid.UUID
    GetName() string
    SetDeletionMark(bool)
}
```

`BaseCatalogRepo[T CatalogEntity]` означает: "T может быть чем угодно, **но только если у него есть** методы `GetID()`, `GetName()` и `SetDeletionMark()`". Это защита на уровне компилятора.

## Композиция — Go-способ "наследования"

Go не имеет наследования классов. Вместо этого — **встраивание (embedding)**:

```go
// Базовая "модель" для всех справочников
type CatalogItem struct {
    ID           uuid.UUID
    Name         string
    Code         string
    DeletionMark bool
}

// Валюта = CatalogItem + свои поля
type Currency struct {
    entity.CatalogItem          // ← Встроили! Все поля и методы CatalogItem доступны
    DecimalPlaces int           // ← Собственное поле
    Symbol        string
}

// Можно вызывать методы CatalogItem напрямую:
currency.GetID()   // ← Метод из CatalogItem
currency.Symbol    // ← Собственное поле
```

## Ключевые файлы

- [`internal/infrastructure/storage/postgres/catalog_repo/base.go`](../../internal/infrastructure/storage/postgres/catalog_repo/base.go) — `BaseCatalogRepo[T]`
- [`internal/core/entity/catalog.go`](../../internal/core/entity/catalog.go) — `CatalogItem`, `CatalogEntity`

## Паттерны Go

- **Generics** с Type Constraints
- **Embedding** (композиция вместо наследования)
- `new(T)` для выделения памяти под generic-тип
