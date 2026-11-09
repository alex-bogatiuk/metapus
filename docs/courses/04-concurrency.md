# Модуль 4. Многозадачность: Горутины, Каналы и Контекст

> Учебные фрагменты сокращены и показывают отдельный принцип; полный актуальный код открывайте по указанным исходникам. Примеры Item/Product и товарных документов не означают наличия этих модулей в текущем контенте.

> В 1С для асинхронной работы есть Фоновые Задания. В Go асинхронность встроена в язык на уровне runtime. Этот модуль покрывает всё, что нужно для уверенного чтения конкурентного кода Metapus.

---

## 1. Горутины (Goroutines)

Горутина — это легковесный «поток» выполнения. Она в тысячи раз дешевле потока ОС (стартовый размер стека — 2 КБ vs. 1 МБ у потока ОС). В Go-программе могут одновременно работать миллионы горутин.

```go
func processData(name string) {
    time.Sleep(time.Second)
    fmt.Println("Обработано:", name)
}

func main() {
    // Запускаем 3 горутины параллельно
    go processData("документ-1")
    go processData("документ-2")
    go processData("документ-3")

    // Без ожидания программа завершится ДО того,
    // как горутины закончат работу!
    time.Sleep(2 * time.Second) // грубый способ дождаться
}
```

> [!WARNING]
> **Проблема:** `time.Sleep` — это «костыль». Мы не знаем, сколько точно ждать. Ниже мы разберём правильные способы ожидания: `sync.WaitGroup` и каналы.

---

## 2. sync.WaitGroup — ожидание группы горутин

`WaitGroup` — счётчик активных горутин. Вы увеличиваете его перед запуском горутины и уменьшаете по завершении:

```go
import "sync"

func main() {
    var wg sync.WaitGroup

    names := []string{"документ-1", "документ-2", "документ-3"}

    for _, name := range names {
        wg.Add(1)  // +1 к счётчику

        go func(n string) {
            defer wg.Done()  // -1 к счётчику при выходе из функции
            processData(n)
        }(name)
    }

    wg.Wait()  // Блокирует, пока счётчик не станет 0
    fmt.Println("Все горутины завершились!")
}
```

### `defer` — отложенный вызов

Ключевое слово `defer` откладывает выполнение функции до выхода из текущей функции. Это критично для закрытия ресурсов:

```go
func readFile(path string) error {
    f, err := os.Open(path)
    if err != nil {
        return err
    }
    defer f.Close()  // Закроет файл при ЛЮБОМ выходе из функции (даже при ошибке)

    // ... работаем с файлом
    return nil
}
```

`defer` выполняется в порядке LIFO (последний добавленный — первый выполненный):

```go
defer fmt.Println("первый")
defer fmt.Println("второй")
defer fmt.Println("третий")
// Выведет: третий, второй, первый
```

---

## 3. Каналы (Channels)

Каналы — это типизированные очереди для передачи данных между горутинами. Это основной механизм коммуникации в Go.

### Небуферизированный канал

```go
ch := make(chan string)  // канал для передачи строк

go func() {
    ch <- "Привет из горутины!"  // отправляем в канал (БЛОКИРУЕТ, пока кто-то не прочитает)
}()

msg := <-ch  // читаем из канала (БЛОКИРУЕТ, пока кто-то не отправит)
fmt.Println(msg)
```

Небуферизированный канал — это «рукопожатие»: отправитель ждёт, пока получатель прочитает.

### Буферизированный канал

```go
ch := make(chan string, 3)  // буфер на 3 сообщения

ch <- "первое"   // не блокирует — есть место в буфере
ch <- "второе"   // не блокирует
ch <- "третье"   // не блокирует
// ch <- "четвёртое"  // ЗАБЛОКИРУЕТ — буфер полон!

fmt.Println(<-ch) // первое
```

### Закрытие канала и range

```go
ch := make(chan int, 5)

go func() {
    for i := 0; i < 5; i++ {
        ch <- i
    }
    close(ch)  // Сигнал: «больше данных не будет»
}()

// range по каналу читает, пока канал не закрыт
for val := range ch {
    fmt.Println(val) // 0, 1, 2, 3, 4
}
```

---

## 4. Select — переключатель каналов

`select` позволяет ждать данные из **нескольких** каналов одновременно. Выполнится первый `case`, в котором придут данные:

```go
ch1 := make(chan string)
ch2 := make(chan string)

go func() {
    time.Sleep(1 * time.Second)
    ch1 <- "результат от сервиса 1"
}()

go func() {
    time.Sleep(500 * time.Millisecond)
    ch2 <- "результат от сервиса 2"
}()

select {
case msg := <-ch1:
    fmt.Println("Получили от сервиса 1:", msg)
case msg := <-ch2:
    fmt.Println("Получили от сервиса 2:", msg) // Выполнится первым (500ms < 1s)
}
```

### Select с таймаутом

```go
select {
case result := <-ch:
    fmt.Println("Результат:", result)
case <-time.After(5 * time.Second):
    fmt.Println("Таймаут! Ответ не пришёл за 5 секунд.")
}
```

### Select с `default` — неблокирующее чтение

```go
select {
case msg := <-ch:
    fmt.Println("Получили:", msg)
default:
    fmt.Println("Канал пуст, идём дальше")  // не ждём
}
```

---

## 5. Context (context.Context) — самая важная сущность Go-бэкенда

### Зачем нужен контекст?

Контекст решает две задачи:

**1. Отмена (Cancellation).** Пользователь закрыл браузер → HTTP-запрос отменён → все операции (к БД, к внешним API) должны прекратиться:

```go
// Контекст с таймаутом: через 5 секунд всё отменяется
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()  // ОБЯЗАТЕЛЬНО вызвать cancel, иначе утечка ресурсов

result, err := slowOperation(ctx)
if err != nil {
    if errors.Is(err, context.DeadlineExceeded) {
        fmt.Println("Операция не уложилась в 5 секунд!")
    }
}
```

**2. Передача scoped-данных (запросо-зависимых):**

```go
// Контекст с ручной отменой
ctx, cancel := context.WithCancel(context.Background())

go func() {
    // Ждём сигнал отмены
    <-ctx.Done()
    fmt.Println("Получен сигнал остановки:", ctx.Err())
}()

// ... через некоторое время
cancel()  // отменяем контекст → горутина получит сигнал
```

### Как горутина слушает отмену контекста

```go
func worker(ctx context.Context, id int) {
    for {
        select {
        case <-ctx.Done():
            // Контекст отменён — выходим чисто
            fmt.Printf("Воркер %d остановлен: %v\n", id, ctx.Err())
            return
        default:
            // Делаем полезную работу
            fmt.Printf("Воркер %d работает...\n", id)
            time.Sleep(500 * time.Millisecond)
        }
    }
}
```

> [!WARNING]
> **Правило Metapus (Anti-pattern §6):**
> Каждая `go func()` должна иметь владельца и слушать `ctx.Done()`. Без этого горутина «утечёт» — будет крутиться вечно, потребляя память и CPU.
>
> ```go
> // ❌ ЗАПРЕЩЕНО: горутина без контекста
> go func() {
>     for { doWork() }
> }()
>
> // ✅ ПРАВИЛЬНО: горутина слушает отмену
> go func() {
>     for {
>         select {
>         case <-ctx.Done():
>             return
>         default:
>             doWork()
>         }
>     }
> }()
> ```

### Контекст в Metapus: Database-per-Tenant

> [!IMPORTANT]
> **Правило §2.2:** В бизнес-таблицах Metapus **НЕТ** колонки `tenant_id`. У каждого клиента своя база данных (Database-per-Tenant).
>
> Как система знает, к какой базе обращаться? `tenant_id` и `TxManager` лежат внутри `context.Context`!
>
> Цепочка:
> 1. Auth Middleware извлекает `tenant_id` из JWT-токена
> 2. Tenant Middleware находит пул коннектов для этого тенанта
> 3. Кладёт `TxManager` в `ctx`
> 4. Репозитории извлекают `TxManager` из `ctx` для выполнения SQL
>
> Поставьте breakpoint в `internal/domain/service.go` на методе `getTxManager()`:
> ```go
> func (s *CatalogService[T]) getTxManager(ctx context.Context) (tx.Manager, error) {
>     if s.txManager != nil {
>         return s.txManager, nil
>     }
>     return tenant.GetTxManager(ctx)  // ← из контекста!
> }
> ```

---

## 6. time.Ticker — периодические задачи

В 1С для периодических задач используются Регламентные задания. В Go — `time.Ticker`:

```go
func periodicTask(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()  // ОБЯЗАТЕЛЬНО! Иначе утечка таймера

    for {
        select {
        case <-ctx.Done():
            fmt.Println("Задача остановлена")
            return
        case <-ticker.C:  // Сработал каждые 30 секунд
            fmt.Println("Выполняю периодическую задачу...")
            doPeriodicWork()
        }
    }
}
```

> [!IMPORTANT]
> **Правило Metapus:** `defer ticker.Stop()` — обязательно. Без этого таймер продолжит работать даже после выхода из функции.

---

## Резюме модуля

| Концепция 1С | Аналог в Go |
|-------------|-------------|
| Фоновое задание | `go func() { ... }()` (горутина) |
| `ОжидатьЗавершения()` | `sync.WaitGroup` |
| — | Каналы (`chan T`) для передачи данных |
| — | `select` для ожидания нескольких каналов |
| Регламентное задание | `time.Ticker` |
| Отмена фонового задания | `context.WithCancel()` / `ctx.Done()` |
| Параметры сеанса | `context.Value()` (scoped данные в контексте) |

---

[Перейти к следующему модулю: Стандартная библиотека и БД →](05-stdlib-and-db.md)
