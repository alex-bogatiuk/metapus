# Модуль 2: Clean Architecture на практике

## Правило зависимостей

Суть Clean Architecture (Чистой Архитектуры) Роберта Мартина — **"Код, который отвечает за бизнес-логику, ничего не должен знать про базу данных, фреймворки и интернет"**.

В Metapus это реализовано через три слоя:

```
internal/
├── core/           ← Ядро: типы, ID, ошибки (знает только себя)
├── domain/         ← Бизнес-логика: сервисы, модели (знает core)
└── infrastructure/ ← Техника: PostgreSQL, HTTP, TRON (знает всё)
```

### Правило зависимостей (стрелки только вниз → вглубь):

```
infrastructure → domain → core
     ✓              ✓        ✓

core → domain       ✗  (ЗАПРЕЩЕНО!)
domain → infrastructure  ✗  (ЗАПРЕЩЕНО!)
```

## Как domain работает с БД, не зная о ней?

Через **интерфейсы**. Domain определяет *"Мне нужен кто-то, кто умеет сохранять валюту"*:

```go
// internal/domain/catalogs/currency/repository.go
type Repository interface {
    GetByID(ctx context.Context, id uuid.UUID) (*Currency, error)
    Create(ctx context.Context, c *Currency) error
}
```

А infrastructure реализует это конкретно для PostgreSQL:

```go
// internal/infrastructure/storage/postgres/catalog_repo/currency.go
type CurrencyRepo struct {
    base BaseCatalogRepo[*currency.Currency]
}

func (r *CurrencyRepo) GetByID(ctx context.Context, id uuid.UUID) (*currency.Currency, error) {
    return r.base.GetByID(ctx, id)  // SQL-запрос
}
```

## Зачем это нужно?

1. **Тестируемость** — в тестах подставляем фейковый репозиторий (in-memory map), не нужна БД
2. **Заменяемость** — хотим перейти с PostgreSQL на MySQL? Меняем только infrastructure
3. **Фокус** — бизнес-программист не отвлекается на детали SQL

## Куда класть новый код?

| Вопрос | Слой |
|--------|------|
| "Отправить email после создания заказа" | `infrastructure` (техническая реализация доставки) |
| "Скидка 10% если сумма > 1000" | `domain` (бизнес-правило) |
| "Сохранить курс валюты в Redis" | `infrastructure` (хранилище) |
| "Валюта должна иметь код из 3 букв" | `domain` (валидация модели) |

## Ключевые файлы

- [`internal/core/`](../../internal/core/) — типы, ID, ошибки, entity
- [`internal/domain/`](../../internal/domain/) — бизнес-логика, интерфейсы
- [`internal/infrastructure/`](../../internal/infrastructure/) — PostgreSQL, HTTP, блокчейн

## Паттерны

- **Dependency Inversion** — domain определяет интерфейс, infrastructure реализует
- **Duck Typing** — Go-интерфейсы реализуются неявно (не нужно `implements`)
- **Hexagonal Architecture** — domain как ядро, порты (интерфейсы), адаптеры (реализации)
