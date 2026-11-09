# Модуль 2. Основы 2: Интерфейсы, Ошибки и Generics

> Учебные фрагменты сокращены и показывают отдельный принцип; полный актуальный код открывайте по указанным исходникам. Примеры Item/Product и товарных документов не означают наличия этих модулей в текущем контенте.

> Этот модуль покрывает механизмы абстракции, которые делают Go по-настоящему мощным. Интерфейсы, обработка ошибок и Generics — три столпа, на которых стоит архитектура Metapus.

---

## 1. Интерфейсы

### Что такое интерфейс?

В Go интерфейс описывает **поведение** (набор методов), а не данные. 

В 1С мы проверяем тип объекта: `ТипЗнч(Объект) = Тип("СправочникСсылка.Номенклатура")`. В Go мы спрашиваем: «Умеет ли этот объект делать X?»

```go
// Интерфейс — контракт: «кто угодно, лишь бы умел Post() и Unpost()»
type Postable interface {
    Post() error
    Unpost() error
}
```

### Утиная типизация (Duck typing)

«Если что-то ходит как утка и крякает как утка — это утка.»

В Go не нужно писать `implements Postable`. Если у структуры есть нужные методы — она **автоматически** реализует интерфейс:

```go
type SalesInvoice struct {
    Number string
    Total  float64
}

// SalesInvoice реализует интерфейс Postable, потому что
// у неё есть оба метода: Post() и Unpost()
func (s *SalesInvoice) Post() error {
    fmt.Println("Проводим:", s.Number)
    return nil
}

func (s *SalesInvoice) Unpost() error {
    fmt.Println("Отменяем проведение:", s.Number)
    return nil
}

// Функция принимает ЛЮБОЙ тип, реализующий Postable
func processDocument(doc Postable) {
    doc.Post()
}

func main() {
    inv := &SalesInvoice{Number: "ТТН-001", Total: 15000}
    processDocument(inv) // SalesInvoice подходит, потому что реализует Postable
}
```

> [!TIP]
> **Где посмотреть в коде Metapus:**
> Откройте файл `internal/domain/posting/engine.go` — там определён интерфейс `Postable` (строки 24-45). Он описывает контракт: какие методы должен реализовать документ, чтобы его можно было провести. Любой документ (продажа, покупка, крипто-инвойс), реализующий эти методы, может быть проведён через один и тот же `Engine`.

### Композиция интерфейсов

Маленькие интерфейсы можно собирать в большие:

```go
type Reader interface {
    Read(p []byte) (n int, err error)
}

type Writer interface {
    Write(p []byte) (n int, err error)
}

// ReadWriter = Reader + Writer (композиция)
type ReadWriter interface {
    Reader
    Writer
}
```

Стандартная библиотека Go построена на маленьких интерфейсах: `io.Reader`, `io.Writer`, `io.Closer`. Это философия Go: **чем меньше интерфейс, тем полезнее**.

### Стандартные интерфейсы, которые нужно знать

| Интерфейс | Метод | Где используется |
|-----------|-------|------------------|
| `error` | `Error() string` | Возврат ошибок из любой функции |
| `fmt.Stringer` | `String() string` | Вывод объекта через `fmt.Println()` |
| `io.Reader` | `Read([]byte) (int, error)` | Чтение данных (файлы, сеть) |
| `io.Writer` | `Write([]byte) (int, error)` | Запись данных |
| `io.Closer` | `Close() error` | Закрытие ресурсов |

### Пустой интерфейс `any`

До Go 1.18 пустой интерфейс писался как `interface{}`. Теперь есть псевдоним `any` — они идентичны:

```go
func printAnything(val any) {
    fmt.Println(val)
}

printAnything(42)          // int
printAnything("hello")     // string
printAnything(true)        // bool
```

---

## 2. Приведение типов (Type Assertion)

Когда переменная имеет тип интерфейса, внутри неё скрывается **конкретный тип**. Чтобы его извлечь, используется приведение типа:

```go
var value any = "hello"

// Безопасное приведение (с проверкой)
str, ok := value.(string)
if ok {
    fmt.Println("Это строка:", str)
}

// Опасное приведение (без проверки) — паника, если тип не совпал
str2 := value.(string)  // OK
num := value.(int)       // ПАНИКА! value содержит string, а не int
```

### Type Switch — переключатель типов

```go
func describe(val any) string {
    switch v := val.(type) {
    case string:
        return fmt.Sprintf("строка длиной %d", len(v))
    case int:
        return fmt.Sprintf("число %d", v)
    case bool:
        if v {
            return "истина"
        }
        return "ложь"
    default:
        return fmt.Sprintf("неизвестный тип: %T", v)
    }
}
```

> [!TIP]
> **Где это в Metapus:**
> Откройте `internal/domain/posting/crypto_visitor.go`. Там `CryptoBalanceVisitor` проверяет: `src, ok := doc.(CryptoBalanceMovementSource)`. Если документ реализует интерфейс `CryptoBalanceMovementSource` — визитор собирает движения. Если нет — молча пропускает. Это **классический Go-паттерн** проверки через type assertion.

---

## 3. Интерфейсы и nil — ловушка для новичков

Внутри Go интерфейсная переменная — это пара `(тип, значение)`:

```go
var iface any        // тип = nil, значение = nil → iface == nil ✓

var s *string = nil  // указатель на строку, равен nil
iface = s            // тип = *string, значение = nil → iface != nil ✗ !!!
```

**Почему `iface != nil`?** Потому что Go проверяет nil по обоим полям. Раз тип известен (`*string`), переменная не считается nil, даже если значение — nil.

```go
fmt.Println(iface == nil) // false — вот это ловушка!
```

> [!WARNING]
> Это контринтуитивно и вызывает баги у всех новичков Go. Запомните: **если вы присвоили интерфейсной переменной типизированный nil-указатель — она перестаёт быть nil.**

---

## 4. Обработка Ошибок (Errors)

### Ошибки — это значения

В 1С ошибки перехватываются через `Попытка...Исключение`. В Go нет `try/catch`. Ошибки — это **обычные значения**, которые возвращаются из функций:

```go
func findUser(id string) (User, error) {
    if id == "" {
        return User{}, fmt.Errorf("id пользователя не может быть пустым")
    }
    // поиск в базе...
    return user, nil  // nil = «ошибки нет»
}
```

### Оборачивание ошибок (Wrapping) через `%w`

Когда ошибка пробрасывается вверх по стеку вызовов, каждый уровень **оборачивает** её, добавляя контекст:

```go
func getDocument(id string) (Document, error) {
    doc, err := repo.GetByID(ctx, id)
    if err != nil {
        // Оборачиваем ошибку: добавляем «что делали» → пробрасываем дальше
        return Document{}, fmt.Errorf("get document %s: %w", id, err)
    }
    return doc, nil
}
```

Цепочка оборачивания создаёт «трейс»:
```
get document abc-123: get by id: connection refused
```

### Проверка типа ошибки: `errors.Is()` и `errors.As()`

```go
import "errors"

// errors.Is — проверяет, есть ли конкретная ошибка в цепочке оборачивания
if errors.Is(err, sql.ErrNoRows) {
    fmt.Println("Запись не найдена")
}

// errors.As — извлекает конкретный тип ошибки из цепочки
var appErr *apperror.AppError
if errors.As(err, &appErr) {
    fmt.Println("Код ошибки:", appErr.Code)
    fmt.Println("HTTP статус:", appErr.HTTPStatus)
}
```

> [!IMPORTANT]
> **Правила Metapus (§2.6 и §2.11):**
> 1. Все бизнес-ошибки создаются через `apperror.AppError` (файл `internal/core/apperror/error.go`).
> 2. **Одноразовая обработка:** ошибку обрабатывают один раз. Либо `log.Error(err)` + проглотить, либо `return fmt.Errorf("action: %w", err)`. Никогда и то, и другое!
> 
> ```go
> // ❌ ЗАПРЕЩЕНО: двойная обработка
> log.Error("не удалось сохранить", err)
> return err
>
> // ✅ ПРАВИЛЬНО: оборачиваем и пробрасываем
> return fmt.Errorf("save document: %w", err)
> ```

### Реальный пример: AppError в Metapus

Откройте файл `internal/core/apperror/error.go`. Вот как выглядит наша структура ошибки:

```go
type AppError struct {
    Code       string         `json:"code"`              // Машинный код: "VALIDATION_ERROR"
    Message    string         `json:"message"`           // Человекочитаемое: "Сумма должна быть > 0"
    Details    map[string]any `json:"details,omitempty"` // Дополнительные данные
    HTTPStatus int            `json:"-"`                 // HTTP-статус (400, 404, 422...)
    Err        error          `json:"-"`                 // Оригинальная ошибка (не попадает в JSON!)
}
```

---

## 5. Panic и Recover

`panic` — это аварийная остановка программы (аналог необработанного исключения). `recover` — перехват паники.

```go
func riskyOperation() {
    panic("всё сломалось!")  // программа аварийно завершится
}

func safeWrapper() {
    defer func() {
        if r := recover(); r != nil {
            fmt.Println("Перехватили панику:", r)
        }
    }()
    riskyOperation()
}
```

> [!CAUTION]
> **В Metapus `panic` запрещён в бизнес-коде.** Паника допустима ТОЛЬКО при инициализации приложения (если невозможно продолжать работу без критического ресурса — например, без подключения к БД). Для бизнес-ошибок ВСЕГДА используйте `return error`.
>
> Посмотрите `cmd/server/main.go`: там `log.Fatalw(...)` вызывается, если не удалось подключиться к мета-базе — это завершение процесса через fatal log при невозможности запуска, а не вызов `panic`.

---

## 6. Generics (Обобщения)

### Проблема без Generics

В 1С платформа автоматически генерирует одинаковые методы для всех справочников. В Go до версии 1.18 приходилось дублировать код:

```go
// Без Generics — нужно писать отдельную функцию для каждого типа!
func filterStrings(items []string, predicate func(string) bool) []string { ... }
func filterInts(items []int, predicate func(int) bool) []int { ... }
// И так далее для КАЖДОГО типа...
```

### Решение: Generics

```go
// T — параметр типа. any = T может быть чем угодно
func filter[T any](items []T, predicate func(T) bool) []T {
    result := make([]T, 0, len(items)) // capacity hint!
    for _, item := range items {
        if predicate(item) {
            result = append(result, item)
        }
    }
    return result
}

// Одна функция работает с любым типом!
strings := filter([]string{"go", "rust", "python"}, func(s string) bool {
    return len(s) <= 4
})
// ["go", "rust"]

numbers := filter([]int{1, 2, 3, 4, 5}, func(n int) bool {
    return n > 3
})
// [4, 5]
```

### Constraints (Ограничения типов)

Иногда `any` слишком широко — нужно ограничить допустимые типы:

```go
// comparable — можно сравнивать через == и !=
func contains[T comparable](items []T, target T) bool {
    for _, item := range items {
        if item == target {
            return true
        }
    }
    return false
}

// Свой constraint (ограничение): только числовые типы
type Number interface {
    ~int | ~int64 | ~float64  // тильда ~ означает «и все типы, основанные на них»
}

func sum[T Number](items []T) T {
    var total T
    for _, item := range items {
        total += item
    }
    return total
}
```

> [!TIP]
> **Generics в Metapus — это фундамент архитектуры (Правило §4):**
>
> Откройте файл `internal/domain/service.go`. Вы увидите:
> ```go
> type CatalogService[T entity.CatalogEntity] struct {
>     repo   CatalogRepository[T]
>     // ...
> }
> ```
> `CatalogService[T]` обслуживает справочники, реализующие `entity.CatalogEntity`, например контрагентов и валюты. Constraint гарантирует методы `Validate(ctx)` и `GetID()`, а не наличие полей `Name`/`Code`. Пользователи обслуживаются отдельным auth-сервисом.
>
> **Запрещено** (Правило §6): писать `Create()/Update()/List()` с нуля для новой сущности. Вместо этого используйте `CatalogService[T]` или `BaseDocumentService[T, L]`.

---

## 7. Type Aliases vs Type Definitions

Два способа создать новое имя для типа. Они выглядят похоже, но ведут себя по-разному:

```go
// Type alias — просто синоним, полностью взаимозаменяем
type UserID = string  // UserID и string — один и тот же тип

// Type definition — НОВЫЙ тип на основе существующего
type OrderID string   // OrderID и string — РАЗНЫЕ типы!
```

```go
var a UserID = "user-1"
var b string = a          // OK: UserID и string — один тип

var c OrderID = "order-1"
var d string = c           // ОШИБКА КОМПИЛЯЦИИ: OrderID ≠ string
var e string = string(c)   // OK: явное преобразование
```

> [!NOTE]
> В Metapus `Quantity` — отдельный тип на основе int64, а `id.ID = uuid.UUID` — **alias**, не отдельный тип для каждой сущности. UUID пользователя и документа взаимозаменяемы на уровне компилятора; правильную связь проверяют domain/authorization/FK contracts.

---

## 8. Перечисления (Enums) и iota

В 1С есть объекты конфигурации `Перечисление`. В Go их делают через `const` + `iota`:

```go
type DocumentStatus int

const (
    _                  DocumentStatus = iota // 0 — пропускаем, неиспользуемое значение
    StatusDraft                              // 1
    StatusPosted                             // 2
    StatusDeleted                            // 3
)

// Человекочитаемое представление
func (s DocumentStatus) String() string {
    switch s {
    case StatusDraft:
        return "Черновик"
    case StatusPosted:
        return "Проведён"
    case StatusDeleted:
        return "Помечен на удаление"
    default:
        return fmt.Sprintf("Неизвестный(%d)", s)
    }
}
```

> [!WARNING]
> **Правило Metapus (§2.10):**
> Для бизнес-enum'ов `iota` начинается с `+1`. Нулевое значение (`0`) = «не задано». Это защита от ситуации, когда программист забыл установить статус.
>
> ```go
> // Наш формат:
> const (
>     StatusDraft   DocumentStatus = iota + 1 // 1
>     StatusPosted                             // 2
>     StatusDeleted                            // 3
> )
> ```
>
> Если у документа `Status == 0`, значит, кто-то забыл его заполнить — и мы это поймём.
>
> `iota` без `+1` допускается ТОЛЬКО для технических констант (ключи контекста, сентинели).

---

## Резюме модуля

| Концепция 1С | Аналог в Go |
|-------------|-------------|
| `ТипЗнч()` / `Тип()` | Type assertion: `val.(string)` |
| Общий модуль с экспортными процедурами | Интерфейс + реализация |
| `Попытка...Исключение` | `if err != nil { return err }` |
| `ВызватьИсключение` | `panic()` (запрещён в бизнес-логике!) |
| Перечисление | `const (...) = iota + 1` |
| Платформа генерирует CRUD | `CatalogService[T]` через Generics |

---

[Перейти к следующему модулю: Чистый код и Архитектура →](03-clean-code-and-arch.md)
