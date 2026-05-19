# Модуль 5: Дизайн-паттерны (Visitor, Factory)

## Паттерн Visitor (Посетитель) — Posting Engine

Самый красивый паттерн в Metapus. Используется в Движке Проведения (Posting Engine).

### Проблема

Один документ может создать движения в **разных** регистрах:
- Приходная накладная → движения по складу + движения по взаиморасчётам
- Крипто-платёж → баланс кошелька + комиссия + баланс мерчанта

Если писать `if documentType == "Invoice" { ... } else if documentType == "Payment" { ... }` — получится спагетти-код, который невозможно расширять.

### Решение: Visitor

```go
// Visitor — "посетитель", который умеет собирать движения определённого типа
type RegisterVisitor interface {
    Name() string
    CollectMovements(ctx context.Context, doc Postable, set *MovementSet) error
}
```

Каждый регистр — отдельный Visitor:
```go
type StockVisitor struct{}      // Знает как собрать складские движения
type SettlementVisitor struct{}  // Знает как собрать движения по взаиморасчётам
type CryptoBalanceVisitor struct{} // Знает как собрать крипто-баланс
```

### Как работает Post()

```go
func (e *Engine) Post(ctx context.Context, doc Postable, updateDoc func(ctx) error) error {
    set := NewMovementSet()

    // Фаза 1: СБОР — каждый Visitor "обходит" документ
    for _, visitor := range e.visitors {
        visitor.CollectMovements(ctx, doc, set)  // Складывает в "корзину"
    }

    // Фаза 2: ПРОВЕРКА — можно ли провести? (остатки, лимиты)
    // ... валидация ...

    // Фаза 3: ЗАПИСЬ — каждый Recorder пишет свою часть
    for _, recorder := range e.recorders {
        recorder.RecordFromSet(ctx, set)
    }

    // Фаза 4: ОБНОВЛЕНИЕ документа
    doc.MarkPosted()
    updateDoc(ctx)
}
```

### Зачем две фазы (Collect → Record)?

Запись движений не может быть частичной. Если сделать запись **в момент обхода** и на третьем регистре возникнет ошибка (не хватает товара), то первые два регистра уже записаны — их придётся откатывать.

Разделяя на фазы: сначала **собираем всё** в памяти, проверяем, и только потом **пишем всё** за один раз. Если проверка не прошла — ничего не записано.

## Паттерн Factory (Фабрика)

Используется для создания объектов с настройкой. Вместо прямого вызова `NewService(param1, param2, param3, ...)` — фабрика скрывает сложность:

```go
// API-фабрика — одна строка на сущность
currencyApi := createCatalogApi[*currency.Currency](currencyRepo, currencySvc)
warehouseApi := createCatalogApi[*warehouse.Warehouse](warehouseRepo, warehouseSvc)
```

## Паттерн Functional Options

Для конструкторов с множеством необязательных параметров:

```go
func NewServer(addr string, opts ...Option) *Server {
    s := &Server{addr: addr, timeout: 30 * time.Second}  // дефолты
    for _, opt := range opts {
        opt(s)  // применяем опции
    }
    return s
}

// Использование:
server := NewServer(":8080",
    WithTimeout(60 * time.Second),
    WithMaxConns(100),
)
```

## Ключевые файлы

- [`internal/domain/posting/engine.go`](../../internal/domain/posting/engine.go) — `Engine.Post()`
- [`internal/domain/posting/visitor.go`](../../internal/domain/posting/visitor.go) — интерфейсы Visitor + Recorder

## Паттерны

- **Visitor** — обход документа для сбора движений (Open/Closed Principle)
- **Factory** — скрытие сложности создания объектов
- **Functional Options** — гибкие конструкторы
- **Two-phase commit** — Collect → Validate → Record
