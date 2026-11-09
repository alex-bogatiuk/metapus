# Модуль 5. Стандартная библиотека, БД и Рутины

> Учебные фрагменты сокращены и показывают отдельный принцип; полный актуальный код открывайте по указанным исходникам. Примеры Item/Product и товарных документов не означают наличия этих модулей в текущем контенте.

> Go имеет одну из лучших стандартных библиотек среди языков программирования. В этом модуле — SQL, HTTP, JSON, работа с датами и строками. Всё, что нужно для повседневной работы.

---

## 1. defer — гарантированная очистка ресурсов

В 1С мы вручную закрываем соединения и файлы. В Go есть `defer` — отложенный вызов функции, который гарантированно выполнится при выходе:

```go
func processFile(path string) error {
    f, err := os.Open(path)
    if err != nil {
        return fmt.Errorf("open file: %w", err)
    }
    defer f.Close()  // ← выполнится при ЛЮБОМ выходе из функции

    // ... работаем с файлом
    // Даже если здесь произойдёт ошибка — f.Close() всё равно вызовется
    return nil
}
```

В Metapus `defer` используется повсеместно:
- `defer rows.Close()` — закрытие результата SQL-запроса
- `defer ticker.Stop()` — остановка таймера
- `defer metaPool.Close()` — закрытие пула соединений (в `cmd/server/main.go`)
- `defer cancel()` — отмена контекста

---

## 2. Работа со строками и `strconv`

### Строки

```go
import "strings"

s := "Hello, Metapus!"

strings.Contains(s, "Meta")        // true
strings.HasPrefix(s, "Hello")      // true
strings.ToUpper(s)                 // "HELLO, METAPUS!"
strings.ReplaceAll(s, "!", "?")    // "Hello, Metapus?"
strings.Split("a,b,c", ",")       // ["a", "b", "c"]
strings.TrimSpace("  hello  ")    // "hello"
```

### Конвертация: `strconv` vs. `fmt.Sprintf`

```go
import "strconv"

// Число → Строка
s := strconv.Itoa(42)           // "42"   — быстро
s := fmt.Sprintf("%d", 42)      // "42"   — медленнее (парсит шаблон)

// Строка → Число
n, err := strconv.Atoi("42")    // 42, nil
n, err := strconv.Atoi("abc")   // 0, error

// Float
f := strconv.FormatFloat(3.14, 'f', 2, 64) // "3.14"
```

> [!IMPORTANT]
> **Правило Metapus (§3):** В hot paths (часто вызываемый код) используйте `strconv.Itoa()` вместо `fmt.Sprintf("%d", n)`. `strconv` работает в разы быстрее, потому что не парсит строку-шаблон.

---

## 3. Дата и время (`time`)

В 1С: `ТекущаяДата()`, `ДобавитьМесяц()`, `Формат()`. В Go — пакет `time`:

```go
import "time"

// Текущее время
now := time.Now()

// Создание конкретной даты
birthday := time.Date(1990, time.March, 15, 0, 0, 0, 0, time.UTC)

// Арифметика
tomorrow := now.Add(24 * time.Hour)
diff := tomorrow.Sub(now)  // time.Duration

// Форматирование (Go использует ОСОБУЮ дату-образец: Mon Jan 2 15:04:05 MST 2006)
formatted := now.Format("2006-01-02 15:04:05")
// Пример: "2026-08-09 10:30:00"

// Парсинг строки в дату
t, err := time.Parse("2006-01-02", "2026-08-09")
```

> [!CAUTION]
> **Магическая дата Go:** Для форматирования используется не `YYYY-MM-DD`, а конкретная дата: `2006-01-02 15:04:05`. Запомните: 1-2-3-4-5-6-7 (январь=1, день=2, час=15(3pm), минуты=04, секунды=05, год=2006, часовой пояс=7(MST)).

### TIMESTAMPTZ в базе данных

В Metapus все даты в PostgreSQL хранятся как `TIMESTAMPTZ` (с часовым поясом). В Go они маппятся на `time.Time`.

---

## 4. Struct Tags (Теги структур)

В 1С вы настраиваете отображение реквизитов в формах через конфигуратор. В Go для этого используются **теги** — метаданные, привязанные к полям структуры:

```go
type CreateUserRequest struct {
    Email    string `json:"email"    db:"email"    validate:"required,email"`
    Name     string `json:"name"     db:"full_name"`
    Age      int    `json:"age"      db:"age"      validate:"gte=0,lte=150"`
    Password string `json:"password" db:"-"`
}
```

| Тег | Библиотека | Назначение |
|-----|-----------|------------|
| `json:"email"` | `encoding/json` | Имя поля при сериализации в JSON |
| `json:"-"` | `encoding/json` | Поле НЕ попадает в JSON |
| `json:",omitempty"` | `internal/core/json` (v2) | Поле пропускается при пустом JSON-представлении; false/0 остаются |
| `json:",omitzero"` | `internal/core/json` (v2) | Поле пропускается при нулевом Go-значении |
| `db:"full_name"` | `pgx` / `sqlx` | Имя колонки в БД |
| `validate:"required"` | `validator` | Правила валидации |

---

## 5. JSON

В приложении используется `internal/core/json`, а не прямой `encoding/json`. HTTP binding работает в strict profile: unknown/duplicate fields, неверный регистр и trailing values отклоняются. [Полный контракт](../reference/json-contracts.md). Float `Price` в примере ниже демонстрирует сериализацию; реальные деньги используют `MinorUnits`/`CryptoAmount`.

### Маршалинг (Go → JSON)

```go
import "metapus/internal/core/json"

type Item struct {
    ID   string  `json:"id"`
    Name string  `json:"name"`
    Price float64 `json:"price"`
}

item := Item{ID: "1", Name: "Молоток", Price: 350}

// Структура → JSON-строка
data, err := json.Marshal(item)
// {"id":"1","name":"Молоток","price":350}

// Красивый JSON (с отступами)
data, err := json.MarshalIndent(item, "", "  ")
```

### Анмаршалинг (JSON → Go)

```go
jsonStr := `{"id":"1","name":"Молоток","price":350}`

var item Item
err := json.Unmarshal([]byte(jsonStr), &item)
// item.Name == "Молоток"
```

В Gin (наш HTTP-фреймворк) это делается ещё проще:

```go
func CreateHandler(c *gin.Context) {
    var req CreateUserRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        // Gin автоматически прочитал Body и заполнил req
    }
}
```

---

## 6. Базы данных: PostgreSQL, pgx, squirrel

### Connection Pool (Пул соединений)

В 1С платформа сама управляет соединениями с СУБД. В Go мы делаем это явно через **пул**:

```go
// Создаём пул соединений (один раз при старте приложения!)
pool, err := pgxpool.New(ctx, "postgres://user:pass@localhost:5432/mydb")
defer pool.Close()

// pool.Query() берёт свободное соединение, выполняет запрос и возвращает его в пул
rows, err := pool.Query(ctx, "SELECT id, name FROM items")
```

Пул решает проблему: открывать новое TCP-соединение к базе на каждый запрос — дорого (100+ мс). Пул держит N готовых соединений и переиспользует их.

### Squirrel — Query Builder

Вместо клейки SQL-строк мы используем строитель запросов:

```go
import sq "github.com/Masterminds/squirrel"

// Аналог: ВЫБРАТЬ Код, Наименование ИЗ Справочник.Номенклатура ГДЕ ПометкаУдаления = ЛОЖЬ
query, args, err := sq.
    Select("code", "name").
    From("cat_items").
    Where(sq.Eq{"is_deleted": false}).
    OrderBy("name ASC").
    Limit(50).
    PlaceholderFormat(sq.Dollar).  // PostgreSQL использует $1, $2 вместо ?
    ToSql()

// query = "SELECT code, name FROM cat_items WHERE is_deleted = $1 ORDER BY name ASC LIMIT 50"
// args  = [false]
```

### Выполнение и сканирование

```go
rows, err := pool.Query(ctx, query, args...)
if err != nil {
    return fmt.Errorf("query items: %w", err)
}
defer rows.Close()  // ← ОБЯЗАТЕЛЬНО через defer!

items := make([]Item, 0, 50)  // capacity hint
for rows.Next() {
    var item Item
    if err := rows.Scan(&item.Code, &item.Name); err != nil {
        return fmt.Errorf("scan item: %w", err)
    }
    items = append(items, item)
}

// Проверка ошибок после цикла
if err := rows.Err(); err != nil {
    return fmt.Errorf("rows iteration: %w", err)
}
```

> [!TIP]
> **Где посмотреть в коде:**
> Откройте любой файл `*_repo.go` в `internal/infrastructure/storage/postgres/`. Найдите метод `List()` — там вы увидите: `sq.Select(...)`, `ToSql()`, `pool.Query()`, `rows.Scan()`. Поставьте breakpoint на `ToSql()` и посмотрите, какой SQL генерируется!

---

## 7. Миграции базы данных

В 1С при добавлении реквизита платформа сама обновляет таблицы. В Go мы создаём **миграции** — SQL-файлы, которые выполняются по порядку:

```
migrations/
├── 001_cat_items.up.sql          ← создание таблицы справочника
├── 001_cat_items.down.sql        ← откат (удаление таблицы)
├── 002_doc_sales_invoice.up.sql  ← создание таблицы документа
└── 002_doc_sales_invoice.down.sql
```

### Конвенция именования таблиц в Metapus

| Префикс | Тип объекта | Аналог в 1С |
|---------|-------------|-------------|
| `cat_` | Справочник (Catalog) | `Справочник` |
| `doc_` | Документ | `Документ` |
| `reg_` | Регистр | `РегистрНакопления` / `РегистрСведений` |
| `sys_` | Системная таблица | Служебные таблицы платформы |
| `auth_` | Авторизация | Пользователи ИБ |

> [!IMPORTANT]
> **Правило Metapus:** на раннем этапе разработки мы **редактируем существующую миграцию**, а не создаём новую. Новые миграции — только после релиза в продакшен. Новые сущности получают новую исходную миграцию. Mutable business tables используют `_txid`, `_deleted_at`, version и предусмотренные моделью audit triggers; append-only ledger и auth/runtime state имеют отдельные контракты. См. [правила миграций](../../db/migrations/README.md).

---

## Резюме модуля

| Концепция 1С | Аналог в Go |
|-------------|-------------|
| `ЗакрытьФайл()` | `defer f.Close()` |
| `Строка()`, `Число()` | `strconv.Itoa()`, `strconv.Atoi()` |
| `Формат(Дата, "ДФ=...")` | `time.Format("2006-01-02")` |
| Язык запросов 1С | `squirrel` (Query Builder) + SQL |
| Реквизиты (в конфигураторе) | Struct Tags (`json:"..."`, `db:"..."`) |
| Обновление конфигурации БД | Миграции (SQL-файлы) |
| `ЧтениеJSON` / `ЗаписьJSON` | `json.Marshal()` / `json.Unmarshal()` |

---

[Перейти к следующему модулю: Устройство API и Авторизация →](06-metapus-api-auth.md)
