# Модуль 4: Базы Данных, Multi-Tenancy и Транзакции

## 1. Multi-Tenancy: Database-per-Tenant

Metapus — SaaS-сервис для многих компаний (тенантов). Каждый тенант получает **свою собственную базу данных**.

```
Meta DB (tenants)          ← Хранит список тенантов и подключения
  ├── mt_company_a         ← БД компании А
  ├── mt_company_b         ← БД компании Б
  └── mt_company_c         ← БД компании В
```

### Почему не одна БД с колонкой `tenant_id`?

| Подход | Плюсы | Минусы |
|--------|-------|--------|
| Shared DB (`tenant_id`) | Проще управлять | Один баг = утечка данных. Забыл WHERE → видишь чужие данные |
| **Database-per-Tenant** | Физическая изоляция. Невозможно увидеть чужие данные | Сложнее управлять миграциями |

Metapus выбирает **безопасность**. Нет `tenant_id` в таблицах — нет возможности его забыть.

### Как сервер определяет, какую БД использовать?

Middleware `TenantDB` — первый в цепочке:

```go
func TenantDB(poolManager *tenant.Manager) gin.HandlerFunc {
    return func(c *gin.Context) {
        tenantID := c.GetHeader("X-Tenant-ID")        // 1. Из HTTP-заголовка
        pool, _ := poolManager.GetPool(ctx, tenantID)  // 2. Получаем пул к БД тенанта
        ctx = tenant.WithPool(ctx, pool)               // 3. Кладём в "рюкзак" (context)
        c.Next()                                       // 4. Следующий middleware
    }
}
```

После этого **весь код ниже** работает с правильной БД, не зная об этом.

## 2. Транзакции и `TxManager`

Транзакция — атомарная операция: либо **всё** выполнится, либо **ничего**.

```go
txManager.RunInTransaction(ctx, func(ctx context.Context) error {
    // Всё внутри — одна транзакция
    invoice.Status = "paid"
    invoiceRepo.Update(ctx, invoice)   // UPDATE 1

    payment.Status = "confirmed"
    paymentRepo.Update(ctx, payment)   // UPDATE 2

    return nil  // COMMIT (оба UPDATE сохранены)
    // Если return err → ROLLBACK (оба UPDATE отменены)
})
```

`TxManager` тоже кладётся в контекст: `ctx = tenant.WithTxManager(ctx, txManager)`.

## 3. Optimistic Locking vs Pessimistic Locking

### Pessimistic Locking (`SELECT FOR UPDATE`)

```sql
SELECT * FROM products WHERE id = 1 FOR UPDATE;
-- Строка ЗАБЛОКИРОВАНА. Другие ждут.
UPDATE products SET stock = stock - 1 WHERE id = 1;
COMMIT;
-- Строка разблокирована.
```

Проблема: если два запроса блокируют строки в разном порядке → **deadlock**.

### Optimistic Locking (поле `version`)

```sql
-- Читаем с version
SELECT *, version FROM products WHERE id = 1;  -- version = 5

-- Обновляем с проверкой version
UPDATE products SET stock = 99, version = 6 WHERE id = 1 AND version = 5;
-- affected_rows = 1? ✓ Успех!
-- affected_rows = 0? ✗ Кто-то уже изменил! → повторить
```

Metapus по умолчанию использует **Optimistic Locking**. `SELECT FOR UPDATE` — только для крипто-остатков, где гонки критичны.

## 4. Регистры и DELETE + INSERT

В Metapus таблицы с префиксом `reg_` — это регистры (регистры движений, остатков). При **перепроведении** документа используется стратегия `DELETE + INSERT`:

```
Документ отредактирован → удаляем старые движения → вставляем новые
```

### Почему DELETE + INSERT, а не UPDATE?

1. **Количество строк может измениться.** Было 3 товара → стало 4. UPDATE не умеет "добавлять строки".
2. **Простота.** Не нужно сравнивать "что изменилось" — просто удалил всё старое и вставил новое.
3. **Идемпотентность.** `DELETE WHERE doc_id = X AND version < N` + `INSERT` всегда даёт верный результат.
4. **PostgreSQL MVCC.** DELETE не удаляет данные физически (просто помечает как invisible), а INSERT пишет в конец. По производительности это сравнимо с UPDATE.

## Ключевые файлы

- [`internal/core/tenant/`](../../internal/core/tenant/) — `Manager`, `WithPool`, `WithTxManager`
- [`internal/infrastructure/storage/postgres/tx_manager.go`](../../internal/infrastructure/storage/postgres/tx_manager.go) — `TxManager`
- [`docs/systems/posting-engine.md`](../../docs/systems/posting-engine.md) — стратегия проведения

## Паттерны

- **Database-per-Tenant** — физическая изоляция
- **Context-carried dependencies** — пул БД и TxManager в `context.Context`
- **Optimistic Locking** — поле `version` вместо блокировок
- **DELETE + INSERT** — стратегия перепроведения регистров
