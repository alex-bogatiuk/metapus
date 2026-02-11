# Posting Engine — Проведение документов

> Полный путь проведения документа: от HTTP-запроса до записи движений в регистры и автоматического обновления остатков через триггеры.

---

## Общая схема проведения

```
Service.Post(docID)
├── repo.GetByID() — загрузка документа + строки
├── updateDoc callback — func() { repo.Update(doc) }
└── postingEngine.Post(ctx, doc, updateDoc)
    └── Engine.doPost()
        ├── doc.CanPost(ctx) — валидация
        └── txm.RunInTransaction()
            ├── stockService.ReverseMovements() — реверс старых (если перепроведение)
            ├── doc.GenerateMovements() — генерация новых
            ├── validateStockAvailability() — проверка остатков (для расхода)
            ├── recordMovements() — запись в reg_stock_movements (COPY)
            │   └── PostgreSQL Trigger → UPDATE reg_stock_balances
            ├── doc.MarkPosted() — Posted=true, PostedVersion++
            └── updateDoc(ctx) — сохранение документа
```

---

## Шаг 1: Entry Point — Сервис документа

Сервис загружает документ (шапку + строки) и передаёт управление движку:

```go
func (s *Service) Post(ctx context.Context, docID id.ID) error {
    doc, _ := s.repo.GetByID(ctx, docID)
    lines, _ := s.repo.GetLines(ctx, docID)
    doc.Lines = lines
    
    updateDoc := func(ctx context.Context) error { return s.repo.Update(ctx, doc) }
    return s.postingEngine.Post(ctx, doc, updateDoc)
}
```

---

## Шаг 2: Engine.doPost — координация

Централизованный движок (`posting.Engine`) координирует весь процесс **в единой атомарной транзакции**:

1. **Валидация** — `doc.CanPost(ctx)`
2. **Начало транзакции** — `txm.RunInTransaction()`
3. **Реверс старых движений** — если документ уже был проведён (перепроведение)
4. **Генерация движений** — `doc.GenerateMovements()`
5. **Проверка остатков** — для расходных движений, с пессимистической блокировкой
6. **Запись движений** — batch insert через COPY
7. **Маркировка документа** — `Posted=true`, `PostedVersion++`
8. **Сохранение документа** — вызов callback
9. **After-post hooks**

---

## Шаг 3: Генерация движений документом

Документ сам создаёт движения. Это **детерминированная функция** — легко тестируется.

### GoodsReceipt (приход)
```go
func (g *GoodsReceipt) GenerateMovements() *MovementSet {
    movements := NewMovementSet()
    for _, line := range g.Lines {
        baseQty := line.Quantity * line.Coefficient  // конвертация единиц
        movements.AddStock(NewStockMovement(
            recorderID:  g.ID,
            recordType:  RecordTypeReceipt,  // ПРИХОД
            warehouseID: g.WarehouseID,
            productID:   line.ProductID,
            quantity:    baseQty,
        ))
    }
    return movements
}
```

### GoodsIssue (расход)
Аналогично, но с `RecordTypeExpense` — **уменьшает** остаток.

---

## Шаг 4: Проверка остатков (Stock Validation)

Для расходных документов движок проверяет наличие товара с **пессимистической блокировкой**:

```
validateStockAvailability()
├── Сбор expense-движений
├── Группировка по warehouse+product (pointer map)
├── Детерминированная сортировка ← предотвращение deadlock (resource ordering)
└── CheckAndReserveStock()
    └── для каждого товара:
        ├── GetBalanceForUpdate() — SELECT ... FOR UPDATE
        └── if quantity < required → InsufficientStock error
```

**Resource Ordering:** сортировка по ключам измерений (warehouseID + productID) **до** блокировок. Предотвращает deadlock AB-BA.

---

## Шаг 5: Запись движений

Движения записываются через **batch insert (COPY)** — эффективная массовая вставка:

```go
func (r *StockRepo) CreateMovements(ctx context.Context, movements []StockMovement) error {
    txm := postgres.MustGetTxManager(ctx)
    inserter := txm.NewBatchInserter(...)
    for _, m := range movements {
        inserter.Add(m.LineID, m.RecorderID, m.Period, m.WarehouseID, m.ProductID, m.Quantity)
    }
    return inserter.Flush()  // COPY в reg_stock_movements
}
```

---

## Шаг 6: Триггер обновления балансов

PostgreSQL триггер **автоматически** обновляет `reg_stock_balances` при каждой вставке/удалении движения:

```sql
CREATE TRIGGER trg_stock_movements_balance
    AFTER INSERT OR DELETE ON reg_stock_movements
    FOR EACH ROW EXECUTE FUNCTION update_stock_balance();

-- Функция триггера:
-- receipt → +quantity
-- expense → -quantity
-- UPSERT в reg_stock_balances через ON CONFLICT
```

**Immutable Ledger:** движения **никогда не обновляются** (UPDATE). При перепроведении — старые удаляются, новые вставляются.

---

## Unpost — отмена проведения

```
Service.Unpost(docID)
└── postingEngine.Unpost(ctx, doc, updateDoc)
    └── txm.RunInTransaction()
        ├── stockService.ReverseMovements(recorderID)
        │   └── DELETE FROM reg_stock_movements
        │       WHERE recorder_id = $1 AND recorder_version < $2
        │       └── Trigger AFTER DELETE → реверс баланса
        ├── doc.MarkUnposted() — Posted=false
        └── updateDoc(ctx) — сохранение
```

При DELETE триггер применяет **обратную операцию** к балансу (receipt → -qty, expense → +qty).

---

## Перепроведение

При повторном проведении уже проведённого документа:

1. Удаление старых движений (`ReverseMovements`) → триггер откатывает балансы
2. Генерация новых движений (`GenerateMovements`) с новой версией
3. Проверка остатков (для расхода)
4. Запись новых движений → триггер обновляет балансы
5. Обновление `PostedVersion`

Всё в **одной транзакции** — атомарно.

---

## Алгоритм проведения (полный)

```
1. IDEMPOTENCY CHECK
   └── Если ключ уже обработан → return cached_result

2. ПРЕДВАРИТЕЛЬНАЯ ВАЛИДАЦИЯ (БЕЗ транзакции)
   └── Парсинг, обязательные поля, типы данных

3. BEGIN TRANSACTION (REPEATABLE READ)
   └── SET LOCAL statement_timeout = '30s'

4. СОРТИРОВКА РЕСУРСОВ (Resource Ordering)
   └── ORDER BY product_id ASC — предотвращение Deadlock

5. ЧТЕНИЕ СТАРЫХ ДВИЖЕНИЙ (если перепроведение)

6. ГЕНЕРАЦИЯ НОВЫХ ДВИЖЕНИЙ

7. ПРОВЕРКА ОСТАТКОВ
   └── UPDATE reg_stock_balances SET quantity = quantity + $delta
       WHERE (quantity + $delta) >= 0
       └── RowsAffected = 0 → ROLLBACK + ErrInsufficientStock

8. ЗАПИСЬ ДВИЖЕНИЙ (pgx.CopyFrom)

9. COMMIT + UPDATE IDEMPOTENCY STATUS
```

---

## Правила

- **NO** `UPDATE` на `reg_*_movements` таблицах (Immutable Ledger)
- **NO** глобальных блокировок — только row-level locking
- **Всегда** сортировать ресурсы перед блокировкой (resource ordering)
- **Всегда** обрабатывать `CONCURRENT_MODIFICATION` ошибки
- Движения регистров **версионируются** (`recorder_version`)

---

## Связанные документы

- [05-domain-layer.md](05-domain-layer.md) — GenerateMovements в моделях документов
- [11-transactions.md](11-transactions.md) — TxManager и транзакции
- [04-core-layer.md](04-core-layer.md) — StockMovement, StockBalance типы
- [09-crud-pipeline.md](09-crud-pipeline.md) — общий CRUD pipeline
