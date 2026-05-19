# Модуль 6: Обработка Ошибок и Валидация

## Ошибки как значения

В Go нет исключений (`try/catch`). Ошибки — обычные значения, которые **возвращаются** из функций:

```go
user, err := repo.GetByID(ctx, id)
if err != nil {
    return nil, fmt.Errorf("get user: %w", err)
}
```

## 1. Оборачивание ошибок (Wrapping)

Когда ошибка поднимается по стеку вызовов, каждый уровень добавляет контекст:

```go
// Репозиторий: "sql: no rows in result set"
// Сервис: "get user: sql: no rows in result set"
// Handler: "process request: get user: sql: no rows in result set"

return nil, fmt.Errorf("get user: %w", err)  // %w = wrap (обернуть)
```

Оператор `%w` позволяет потом "развернуть" ошибку:
```go
if errors.Is(err, sql.ErrNoRows) {
    // Даже обёрнутая в 3 слоя, мы распознаём оригинал
}
```

## 2. AppError — единая система ошибок

В Metapus все бизнес-ошибки создаются через `apperror`:

```go
return apperror.NewValidation("invoice is required").
    WithDetail("field", "invoiceId")

return apperror.NewNotFound("user", id)

return apperror.NewForbidden("insufficient permissions")
```

Каждый тип ошибки автоматически маппится на HTTP-статус:
- `Validation` → **400** Bad Request
- `NotFound` → **404** Not Found
- `Forbidden` → **403** Forbidden
- Неизвестная ошибка → **500** Internal Server Error

## 3. Handler — тонкий адаптер (§2.5)

Handler **никогда** не формирует ответ об ошибке сам:

```go
// ❌ ЗАПРЕЩЕНО
func (h *Handler) GetByID(c *gin.Context) {
    user, err := h.service.GetByID(ctx, id)
    if err != nil {
        c.JSON(400, gin.H{"error": err.Error()})  // ← Нарушение!
        return
    }
}

// ✅ ПРАВИЛЬНО
func (h *Handler) GetByID(c *gin.Context) {
    user, err := h.service.GetByID(ctx, id)
    if err != nil {
        _ = c.Error(err)   // ← Передаём ошибку в Gin
        c.Abort()           // ← Прерываем обработку
        return
    }
}
```

Ошибку перехватывает **единый** `ErrorHandler` middleware, который:
1. Определяет тип ошибки (`AppError`? `sql.ErrNoRows`?)
2. Формирует JSON-ответ с правильным HTTP-статусом
3. Записывает в лог (для 500-х ошибок)

## 4. Золотое правило: Ошибка обрабатывается ОДИН раз (§2.11)

```go
// ❌ ЗАПРЕЩЕНО: двойная обработка
func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
    user, err := s.repo.GetByID(ctx, id)
    if err != nil {
        log.Error("failed to get user", "error", err)  // ← Логируем
        return nil, err                                  // ← И возвращаем!
    }
}
```

Если каждый уровень будет и логировать, и возвращать — одна ошибка превратится в **10 одинаковых записей** в логах. Найти реальную причину станет невозможно.

Правило: **либо `log` + swallow, либо `wrap` + return**. В 99% случаев — оборачиваем и возвращаем наверх.

## 5. Валидация — чистая функция (§2.7)

Метод `Validate(ctx)` — это **чистая функция**: без обращений к БД, без side effects.

```go
func (p *CryptoPayment) Validate(ctx context.Context) error {
    if id.IsNil(p.InvoiceID) {
        return apperror.NewValidation("invoice is required").
            WithDetail("field", "invoiceId")
    }
    if !p.Amount.IsPositive() {
        return apperror.NewValidation("amount must be positive").
            WithDetail("field", "amount")
    }
    return nil
}
```

Почему **чистая**? Потому что валидация должна быть молниеносной и тестируемой. Если `Validate()` лезет в БД — тест невозможно запустить без Docker.

## Ключевые файлы

- [`internal/core/apperror/`](../../internal/core/apperror/) — `AppError`, `NewValidation`, `NewNotFound`
- [`internal/infrastructure/http/middleware/error_handler.go`](../../internal/infrastructure/http/middleware/error_handler.go) — единый обработчик ошибок

## Паттерны Go

- **Error wrapping** — `fmt.Errorf("context: %w", err)`
- **Error unwrapping** — `errors.Is()`, `errors.As()`
- **Single point of error handling** — `ErrorHandler` middleware
- **Pure validation** — `Validate()` без side effects
- **Handle once** — либо log+swallow, либо wrap+return
