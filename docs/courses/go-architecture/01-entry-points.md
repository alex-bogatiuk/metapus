# Модуль 1: Точки входа и Жизненный цикл

## Две точки входа

Каждый Go-проект начинается с папки `cmd/`. В ней лежат функции `main()` — точки входа.
В Metapus их **две**:

1. **`cmd/server/main.go`** — HTTP-сервер, обрабатывающий запросы от фронтенда (REST API)
2. **`cmd/worker/main.go`** — фоновый воркер для задач, которые нельзя делать в HTTP-запросе

## Dependency Injection (DI) — Внедрение зависимостей

Представь, что наш код — это **ресторан**.

### Репозиторий (Repository) — это Кладовщик

Кладовщик умеет одно — достать ингредиенты со склада (базы данных) и положить обратно. Он ничего не готовит.

```go
type UserRepository interface {
    GetByID(ctx context.Context, id uuid.UUID) (*User, error)
    Create(ctx context.Context, user *User) error
    Update(ctx context.Context, user *User) error
    Delete(ctx context.Context, id uuid.UUID) error
}
```

### Сервис (Service) — это Шеф-повар

Шеф-повар содержит **бизнес-логику**: "Чтобы приготовить борщ, нужно взять свёклу (у кладовщика), нарезать, сварить". Шеф не ходит на склад сам — он говорит кладовщику: "Принеси свёклу".

```go
type UserService struct {
    repo UserRepository  // ← Шеф знает кладовщика
}

func (s *UserService) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
    user, err := s.repo.GetByID(ctx, id)  // "Кладовщик, дай мне пользователя!"
    if err != nil {
        return nil, fmt.Errorf("get user: %w", err)
    }
    // ... бизнес-логика ...
    return user, nil
}
```

### Внедрение зависимостей — Директор ресторана

Директор (функция `main()`) в самом начале говорит: "Кладовщик — это ты (PostgresUserRepo), Шеф — это ты (UserService), и вот твой кладовщик". Это и есть DI.

```go
func main() {
    // 1. Создаём "кладовщика"
    userRepo := postgres.NewUserRepo(dbPool)

    // 2. Создаём "шефа" и говорим ему, кто его кладовщик
    userService := domain.NewUserService(userRepo)

    // 3. Создаём "официанта" (Handler) и даём ему шефа
    userHandler := handler.NewUserHandler(userService)

    // 4. Запускаем ресторан
    router.GET("/users/:id", userHandler.GetByID)
}
```

## Middleware — полоса препятствий

Когда пользователь отправляет HTTP-запрос `GET /api/v1/catalog/users`, он проходит через цепочку Middleware:

```go
protected := router.Group("/api/v1")
protected.Use(
    middleware.TenantDB(poolManager),  // 1. "Какой тенант?" → подключаем его БД
    middleware.Auth(jwtService),        // 2. "Кто ты?" → проверяем JWT токен
    middleware.DataScope(roleSvc),      // 3. "Что тебе можно?" → проверяем права
)
```

### Порядок важен!

Чтобы проверить JWT-токен, нужно знать, в **какой базе** искать пользователя. Поэтому `TenantDB` должен быть **до** `Auth`. Если поменять местами — система сломается.

```
Запрос →  TenantDB  →  Auth  →  DataScope  →  Handler
          (какая БД?)  (кто?)   (что можно?)   (бизнес-логика)
```

## Ключевые файлы

- [`cmd/server/main.go`](../../cmd/server/main.go) — точка входа сервера, DI
- [`cmd/worker/main.go`](../../cmd/worker/main.go) — точка входа воркера
- [`internal/infrastructure/http/v1/router.go`](../../internal/infrastructure/http/v1/router.go) — маршруты и middleware

## Паттерны

- **Dependency Injection** — передача зависимостей через конструкторы
- **Middleware Chain** — последовательная обработка запроса
- **Interface Segregation** — репозиторий определяется интерфейсом в domain
