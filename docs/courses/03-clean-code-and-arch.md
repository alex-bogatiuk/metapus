# Модуль 3. Чистый код и Архитектура

> Учебные фрагменты сокращены и показывают отдельный принцип; полный актуальный код открывайте по указанным исходникам. Примеры Item/Product и товарных документов не означают наличия этих модулей в текущем контенте.

> Архитектура 1С диктуется платформой: метаданные, модули объектов, общие модули. В Go разработчик **сам** проектирует структуру. В Metapus мы используем строгую Clean Architecture. Этот модуль объясняет, зачем она нужна и как работает.

---

## 1. Пакеты и Модули

### Что такое пакет (Package)?

В 1С код организуется в «Общие модули» и «Модули объектов». В Go код организуется в **пакеты**. Каждая папка — это пакет. Каждый файл в папке начинается с `package имяпакета`.

```
internal/
├── core/
│   └── apperror/          ← пакет "apperror"
│       └── error.go       ← package apperror
├── domain/
│   └── posting/           ← пакет "posting"
│       ├── engine.go      ← package posting
│       └── visitor.go     ← package posting
└── infrastructure/
    └── storage/
        └── postgres/      ← пакет "postgres"
            └── user_repo.go
```

Важно: **все файлы в одной папке** должны иметь одинаковое `package`. Нельзя положить `package posting` и `package engine` в одну папку.

### Что такое модуль (Module)?

Модуль — это корень проекта, определённый файлом `go.mod`:

```
module metapus              ← имя нашего модуля

go 1.27                     ← версия Go

require (
    github.com/gin-gonic/gin v1.10.0    ← внешние зависимости
    github.com/jackc/pgx/v5  v5.7.0
)
```

### Импорты: три группы

В Metapus мы строго группируем импорты (Правило §3):

```go
import (
    // 1. Стандартная библиотека Go
    "context"
    "fmt"
    "time"

    // 2. Внешние зависимости (скачанные из интернета)
    "github.com/jackc/pgx/v5/pgxpool"

    // 3. Наши внутренние пакеты
    "metapus/internal/core/apperror"
    "metapus/internal/domain/posting"
)
```

### Папка `internal/` — защита от внешнего мира

Go имеет встроенный механизм: если пакет лежит в папке `internal/`, его **невозможно** импортировать из другого проекта. Это наша защита от случайного использования внутреннего кода.

---

## 2. Clean Architecture — Почему слои?

### Проблема без архитектуры

Представьте: у вас обработчик HTTP-запроса, который сам парсит JSON, сам лезет в базу данных, сам считает скидки и сам отправляет email. Если завтра нужно заменить PostgreSQL на MongoDB — придётся переписать ВСЁ.

### Решение: разделение на слои

```
┌──────────────────────────────────────────────────┐
│             infrastructure (внешний)              │
│  HTTP-handlers, PostgreSQL repos, внешние API     │
│                                                   │
│    ┌──────────────────────────────────────────┐   │
│    │           domain (средний)                │   │
│    │  Бизнес-логика, сервисы, валидация        │   │
│    │                                          │   │
│    │    ┌──────────────────────────────────┐   │   │
│    │    │         core (центр)             │   │   │
│    │    │  Базовые типы, интерфейсы,       │   │   │
│    │    │  AppError, BaseEntity            │   │   │
│    │    └──────────────────────────────────┘   │   │
│    └──────────────────────────────────────────┘   │
└──────────────────────────────────────────────────┘
```

> [!WARNING]
> **Архитектурный инвариант (Правило §2.1):**
> Импорты: `infrastructure` → `application`/`domain` → `core`. Стрелки направлены **внутрь**. Внутренние слои **НИКОГДА** не импортируют внешние!
>
> - ✅ `infrastructure` импортирует `domain` и `core`
> - ✅ `domain` импортирует `core`
> - ❌ `domain` НЕ импортирует `infrastructure`
> - ❌ `core` НЕ импортирует `domain`

### Слой `core` — Базовые примитивы

Самый центр. Здесь лежат типы, от которых зависят ВСЕ:
- `apperror.AppError` — структура ошибки
- `entity.BaseEntity` — базовые поля сущности (ID, DeletionMark, Version, Attributes, CDC); timestamps документа добавляет BaseDocument
- `id.ID` — тип идентификатора
- `tx.Manager` — интерфейс менеджера транзакций

### Слой `domain` — Бизнес-логика

**Аналог в 1С:** «Модуль Объекта» + «Модуль Менеджера».

Здесь описаны:
- **Модели** (struct) — бизнес-сущности
- **Сервисы** (Service) — бизнес-процессы
- **Интерфейсы репозиториев** — абстракции для работы с данными

**Слой `domain` ничего не знает про базу данных или HTTP!** Он общается с хранилищем данных только через интерфейсы:

```go
// domain/repository.go — ИНТЕРФЕЙС (контракт)
type CatalogRepository[T entity.CatalogEntity] interface {
    Create(ctx context.Context, entity T) error
    GetByID(ctx context.Context, id id.ID) (T, error)
    Update(ctx context.Context, entity T) error
    List(ctx context.Context, filter ListFilter) (CursorListResult[T], error)
}
```

Домен говорит: «Мне нужен объект, умеющий Create/GetByID/Update/List». А **кто** именно это делает (PostgreSQL, MongoDB, файл) — домену всё равно.

### Слой `infrastructure` — Реализация

Здесь лежат:
- **Repositories (Репозитории):** конкретная реализация интерфейсов из `domain` для PostgreSQL
- **Handlers (Обработчики HTTP):** парсинг запросов, вызов сервисов, формирование ответов
- **Внешние API клиенты:** TRON blockchain, Vault и т.д.

---

## 3. Dependency Injection (Внедрение зависимостей)

### Как слои связываются?

Если `domain` не знает про `infrastructure`, кто их соединяет? Ответ: **`main.go`** — он единственный файл, который знает обо ВСЁМ.

Откройте `cmd/server/main.go` и посмотрите, как собирается приложение:

```go
func main() {
    // 1. Создаём инфраструктуру (конкретные реализации)
    userRepo := auth_repo.NewUserRepo()        // PostgreSQL-репозиторий
    roleRepo := auth_repo.NewRoleRepo()

    // 2. Передаём их в сервис (domain) через интерфейсы
    authSvc := auth.NewService(
        userRepo,   // передаём репозиторий
        roleRepo,   // передаём репозиторий
        jwtSvc,     // передаём JWT-сервис
        // ...
    )

    // 3. Передаём сервис в HTTP-handler (infrastructure)
    // handler вызывает authSvc.Login(), authSvc.Register() и т.д.
}
```

Это и есть **Dependency Injection**: зависимости не создаются внутри объекта, а **передаются снаружи**. В 1С аналог — когда вы передаёте параметры в Общий Модуль через параметры процедуры, а не обращаетесь к глобальным переменным.

> [!TIP]
> **Дебаг на практике:**
> Поставьте breakpoint на строке `authSvc := auth.NewService(...)` в `cmd/server/main.go`. При запуске F5 вы увидите, как каждая зависимость передаётся в конструктор. Это и есть «корень композиции» (Composition Root).

---

## 4. Тонкие Handlers

**Правило §2.5:** Handler — это тонкий адаптер. Его задачи:

1. Принять HTTP-запрос
2. Распарсить JSON в DTO-структуру
3. Вызвать сервис из `domain`
4. При ошибке: `c.Error(err)` + `c.Abort()`

```go
// ✅ ПРАВИЛЬНО: handler — тонкий адаптер
func (h *ItemHandler) Create(c *gin.Context) {
    var req CreateItemRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.Error(apperror.NewValidation("некорректный JSON"))
        c.Abort()
        return
    }

    item, err := h.service.Create(c.Request.Context(), req)
    if err != nil {
        c.Error(err)  // ошибку обработает middleware ErrorHandler
        c.Abort()
        return
    }

    c.JSON(http.StatusCreated, item)
}

// ❌ ЗАПРЕЩЕНО: бизнес-логика в handler
func (h *ItemHandler) Create(c *gin.Context) {
    var req CreateItemRequest
    c.ShouldBindJSON(&req)
    
    // Это бизнес-логика — должна быть в service!
    if req.Price < 0 {
        c.JSON(400, gin.H{"error": "цена не может быть отрицательной"})
        return
    }
    if req.Name == "" {
        c.JSON(400, gin.H{"error": "название обязательно"})
        return
    }
    // ...
}
```

---

## 5. Тестирование

### Файлы тестов

В Go тесты лежат рядом с кодом, в файлах с суффиксом `_test.go`:

```
internal/core/apperror/
├── error.go          ← код
└── error_test.go     ← тесты
```

### Простейший тест

```go
package apperror_test

import (
    "testing"

    "metapus/internal/core/apperror"
)

func TestNewValidation(t *testing.T) {
    err := apperror.NewValidation("price must be positive")

    if err.Code != apperror.CodeValidation {
        t.Errorf("got code %q, want %q", err.Code, apperror.CodeValidation)
    }
    if err.HTTPStatus != 400 {
        t.Errorf("got status %d, want 400", err.HTTPStatus)
    }
}
```

### Table-driven тесты — наш стандарт

```go
func TestAppErrorPreservesFixedPointQuantities(t *testing.T) {
    tests := []struct {
        give    *apperror.AppError  // входные данные
        want    string              // ожидаемый результат
    }{
        {
            give: apperror.NewValidation("bad input"),
            want: "VALIDATION_ERROR: bad input",
        },
        {
            give: apperror.NewNotFound("user", "abc"),
            want: "NOT_FOUND: user not found",
        },
    }

    for _, tt := range tests {
        t.Run(tt.want, func(t *testing.T) {
            got := tt.give.Error()
            if got != tt.want {
                t.Errorf("Error() = %q, want %q", got, tt.want)
            }
        })
    }
}
```

### Запуск тестов

```bash
# Все тесты проекта
go test ./...

# Тесты конкретного пакета
go test ./internal/core/apperror/

# С подробным выводом
go test -v ./internal/core/apperror/

# Конкретный тест по имени
go test -v -run TestAppErrorPreservesFixedPointQuantities ./internal/core/apperror/
```

> [!TIP]
> В Metapus мы используем конвенцию: переменная цикла — `tt`, поля структуры — `give` (входные) и `want` (ожидаемые). Ищите файлы `*_test.go` через Ctrl+Shift+F.

---

## Резюме модуля

| Концепция 1С | Аналог в Go/Metapus |
|-------------|---------------------|
| Общий модуль | Пакет (`package`) |
| Конфигурация | `go.mod` |
| Метаданные → Модуль Объекта → БД | `core` → `domain` → `infrastructure` |
| Глобальные переменные модуля | Dependency Injection через конструктор |
| Vanessa / Тестирование | `go test`, Table-driven тесты |

---

[Перейти к следующему модулю: Многозадачность →](04-concurrency.md)
