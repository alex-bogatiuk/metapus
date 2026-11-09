# Модуль 10. Паттерны Проектирования в Metapus

> Учебные фрагменты сокращены и показывают отдельный принцип; полный актуальный код открывайте по указанным исходникам. Примеры Item/Product и товарных документов не означают наличия этих модулей в текущем контенте.

> Мы подошли к финалу. В этом модуле мы разберём конкретные архитектурные паттерны, которые пронизывают всю кодовую базу Metapus. Понимание этих паттернов позволит вам не просто читать код, но и писать новый так, чтобы он идеально вписывался в архитектуру.

---

## 1. Repository Pattern (Репозиторий)

**Цель:** Изолировать бизнес-логику (domain) от базы данных (infrastructure).

В 1С вы пишете запросы прямо в модуле объекта: `Запрос.Текст = "ВЫБРАТЬ..."`. В Metapus бизнес-сервису **запрещено** знать про SQL.

Вместо этого сервис использует интерфейс репозитория:

```go
// internal/domain/repository.go
type CatalogRepository[T entity.CatalogEntity] interface {
    Create(ctx context.Context, entity T) error
    GetByID(ctx context.Context, id id.ID) (T, error)
    // ...
}
```

А сама реализация лежит в `infrastructure`:

```go
// internal/infrastructure/storage/postgres/catalog_repo/base.go
type BaseCatalogRepo[T any] struct {
    table string
}

func (r *BaseCatalogRepo[T]) Create(ctx context.Context, entity T) error {
    // Здесь реальный SQL (через squirrel)
}
```

**Плюс:** Если завтра мы решим кэшировать справочники в Redis, бизнес-логика (сервисы) не изменится вообще! Мы просто напишем `RedisCatalogRepo` и подсунем его в `main.go`.

---

## 2. Factory Pattern (Фабрика)

**Цель:** Обобщить создание объектов (в нашем случае — HTTP-хэндлеров и маршрутов).

Вместо того чтобы писать 20 одинаковых файлов для создания API Справочника Товаров, Справочника Клиентов и т.д., у нас есть **одна Фабрика**, которая генерирует стандартный CRUD (Create, Read, Update, Delete) для любой сущности.

Откройте `internal/infrastructure/http/v1/catalog_factory.go`: `CatalogRegistration` требует `RoutePrefix`, `Permission`, `EntityName`, `Build(deps CatalogDeps)`.

Конкретный `CounterpartyRegistration.Build` находится в `internal/content/catalog_registrations.go`: он собирает repository/service, `CatalogHandler` с mapper-функциями и общими deps. `routes_catalogs.go` обходит `FactoryRegistry` и вызывает `RegisterCatalogRoutes`.

Одна строка `RegisterCatalog` в `internal/content/register.go` подключает **уже реализованную** фабрику. Модель, SQL, DTO, permissions и HTTP route contracts добавляются отдельно; см. [рецепт](../howto/new-entity.md).

---

## 3. Functional Options (Функциональные опции)

**Цель:** Удобное создание объектов с большим количеством необязательных параметров.

В Go нет перегрузки функций (нельзя написать 3 разных конструктора `NewUser`). Если у объекта 10 параметров, конструктор становится огромным.

**Решение:** Функции высшего порядка!

```go
type Server struct {
    host string
    port int
    tls  bool
}

// Option — это функция, которая модифицирует Server
type Option func(*Server)

// Опция 1: Изменить порт
func WithPort(port int) Option {
    return func(s *Server) {
        s.port = port
    }
}

// Опция 2: Включить TLS
func WithTLS() Option {
    return func(s *Server) {
        s.tls = true
    }
}

// Конструктор принимает обязательные параметры + переменное число опций (...)
func NewServer(host string, opts ...Option) *Server {
    s := &Server{
        host: host,
        port: 8080, // значение по умолчанию
    }
    
    // Применяем все переданные опции
    for _, opt := range opts {
        opt(s)
    }
    return s
}
```

Использование:
```go
// Без опций (используются defaults)
srv1 := NewServer("localhost")

// С опциями
srv2 := NewServer("api.metapus.io", WithPort(443), WithTLS())
```

> [!NOTE]
> В Metapus мы используем Functional Options для новых конструкторов, где есть ≥3 необязательных параметров (Правило §3).

---

## 4. Strategy Pattern (Стратегия)

**Цель:** Выбор алгоритма поведения во время выполнения программы.

В Metapus примером выбора стратегии служит `internal/core/numerator/config.go`: `StrategyStrict` получает номер непосредственно из БД, `StrategyCached` резервирует диапазон. Реализация находится в `internal/infrastructure/numerator/service.go`; обе стратегии используют один domain-контракт `numerator.Generator`.

Валидация моделей реализуется их `Validate(ctx)`, а проверки проведения — source/recorder contracts и `PostingValidator`. Прежнего `internal/domain/validation.go` и `RunValidationRules` в проекте нет.

---

## 5. Visitor Pattern (Посетитель)

Мы подробно разбирали его в **Модуле 7**. Это ядро нашего **Движка Проведения (Posting Engine)**.

Кратко:
- **`Engine`** умеет обходить визиторов.
- **`RegisterVisitor`** (например, `CryptoBalanceVisitor`) знает, как извлечь движения из документа.
- **`Postable`** (документ) знает свои данные, но не знает, как устроен `Engine`.

Это позволяет нам добавлять новые регистры (CryptoBalance, CryptoFee, CryptoMerchantBalance, CryptoPlatformCost), **не меняя** ни код движка `Engine`, ни код старых документов!

---

## 6. Middleware Chain (Цепочка обязанностей)

Мы разбирали это в **Модуле 6**. Паттерн "Chain of Responsibility".

Каждый `gin.HandlerFunc` (middleware) решает:
1. Обработать запрос и передать дальше (`c.Next()`)
2. Либо прервать цепочку с ошибкой (`c.Abort()`)

Это идеальный способ вынести «сквозной функционал» (логирование, авторизацию, работу с транзакциями) за пределы бизнес-логики.

---

## Сводная таблица: 1С vs. Паттерны Metapus

| Механизм в 1С | Как решается в Metapus | Паттерн |
|--------------|------------------------|---------|
| Модуль Объекта / Менеджера | `internal/domain` (Сервисы) + `internal/infrastructure` (Хранилище) | Repository |
| Платформенный CRUD | Генерация эндпоинтов и хэндлеров | Factory |
| Галочки "Движения" (Регистры) | Документ реализует интерфейсы + Engine вызывает сборщиков | Visitor |
| ОбработкаПроверкиЗаполнения | `Validate(ctx)` модели и PostingValidator | Strategy |
| Подписки на события | Хуки (`BeforePost`, `AfterPost`) | Observer |
| Глобальные параметры сеанса | `context.Context` (прокидывается через каждый слой) | Context |
| Управляемые блокировки | `sync.Mutex` в памяти + Optimistic Locking в БД | Concurrency Control |

---

# 🎉 ПОЗДРАВЛЯЕМ! 🎉

Вы прошли весь курс. Теперь вы знаете:
- Базовый синтаксис Go (Модули 1-2).
- Архитектуру Clean Architecture и DI (Модуль 3).
- Многозадачность: горутины, каналы, мьютексы, пулы (Модули 4, 8).
- Стандартную библиотеку, SQL, работу с памятью (Модули 5, 9).
- Авторизацию, Middleware, JWT, RBAC, RLS (Модуль 6).
- Движок проведения и Паттерны проектирования (Модули 7, 10).

**Что делать дальше?**
Открывайте `cmd/server/main.go`, ставьте breakpoint и шагайте по коду. Теперь это не магия, а чёткий, структурированный и понятный код! Добро пожаловать в разработку ERP-систем на Go!
