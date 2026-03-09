# Процесс Posting в движке Metapus: Запись движений документов в регистры накопления

Карта показывает полный путь проведения документа от HTTP-запроса до записи в базу данных и автоматического обновления остатков через триггеры.

**Ключевые точки:**
- Вход через сервис документа (1a)
- Координация движком (2b)
- Генерация движений (3b)
- Проверка остатков (4c)
- Запись в БД (5d)
- Автоматическое обновление балансов (6b)

---

## Trace 1: Entry Point — Вызов проведения документа из сервиса

Начальная точка входа — метод `Post()` в сервисе документа (`goods_receipt`), который загружает документ и передает управление движку posting.

```
HTTP API Layer
└── GoodsReceiptService.Post() <-- service.go:245
    ├── GetByID() - загрузка документа <-- 1a
    │   ├── repo.GetByID() - header из БД <-- service.go:126
    │   └── repo.GetLines() - строки из БД <-- service.go:131
    ├── updateDoc callback создание <-- 1b
    │   └── func() { repo.Update(doc) } <-- service.go:252
    └── postingEngine.Post() вызов <-- 1c
        └── [переход в Trace 2: Engine.doPost]
```

| ID | Описание | Файл |
|----|----------|------|
| **1a** | Загрузка документа с табличной частью. Сервис получает полный документ со всеми строками перед проведением | `goods_receipt/service.go:246` |
| **1b** | Создание callback для сохранения документа. Функция обратного вызова будет выполнена движком внутри транзакции для обновления состояния документа | `goods_receipt/service.go:251` |
| **1c** | Передача управления движку posting. Сервис делегирует всю логику проведения централизованному движку | `goods_receipt/service.go:255` |

---

## Trace 2: Posting Engine — Координация процесса проведения

Центральный движок (`posting.Engine`) координирует весь процесс: валидация, генерация движений, проверка остатков, запись в регистры, обновление документа.

```
Posting Engine: Координация процесса проведения
│
├── Точка входа: Engine.Post() <-- engine.go:117
│   └── Engine.doPost() - главный алгоритм <-- engine.go:124
│       │
│       ├── Валидация документа <-- 2a
│       │   └── doc.CanPost(ctx) <-- engine.go:128
│       │
│       ├── Запуск транзакции <-- 2b
│       │   └── txm.RunInTransaction() <-- engine.go:144
│       │       │
│       │       ├── Реверс старых движений <-- 2c
│       │       │   └── stockService.ReverseMovements() <-- engine.go:148
│       │       │
│       │       ├── Генерация движений <-- 2d
│       │       │   └── doc.GenerateMovements() <-- engine.go:154
│       │       │
│       │       ├── Проверка остатков <-- 2e
│       │       │   └── validateStockAvailability() <-- engine.go:166
│       │       │
│       │       ├── Запись в регистры <-- 2f
│       │       │   └── recordMovements() <-- engine.go:171
│       │       │
│       │       ├── Маркировка документа <-- 2g
│       │       │   └── doc.MarkPosted() <-- engine.go:176
│       │       │
│       │       └── Сохранение документа <-- 2h
│       │           └── updateDoc(ctx) <-- engine.go:177
│       │
│       └── After-post hooks <-- engine.go:189
│           └── hook(ctx, doc) <-- engine.go:190
```

| ID | Описание | Файл |
|----|----------|------|
| **2a** | Валидация возможности проведения. Проверка бизнес-правил документа перед началом проведения | `posting/engine.go:128` |
| **2b** | Начало транзакции проведения. Все операции выполняются в единой атомарной транзакции | `posting/engine.go:144` |
| **2c** | Реверс старых движений при перепроведении. Если документ уже был проведен, сначала удаляются старые движения | `posting/engine.go:146` |
| **2d** | Генерация движений документом. Документ создает набор движений для регистров на основе своих данных | `posting/engine.go:154` |
| **2e** | Проверка наличия товара на складе. Для расходных движений проверяется достаточность остатков с пессимистической блокировкой | `posting/engine.go:166` |
| **2f** | Запись движений в регистры. Движения сохраняются в таблицы регистров накопления | `posting/engine.go:171` |
| **2g** | Маркировка документа как проведенного. Устанавливается флаг `Posted=true` и инкрементируется `PostedVersion` | `posting/engine.go:176` |
| **2h** | Сохранение обновленного документа. Вызов callback функции для записи изменений документа в БД | `posting/engine.go:177` |

---

## Trace 3: Document Layer — Генерация движений документом GoodsReceipt

Документ «Поступление товаров» создает приходные движения по регистру остатков для каждой строки табличной части.

```
GoodsReceipt.GenerateMovements() - Точка входа <-- model.go:249
├── Инициализация
│   └── movements = NewMovementSet() <-- 3a
├── Обработка строк документа
│   └── for _, line := range g.Lines <-- 3b
│       ├── Конвертация единиц измерения
│       │   └── baseQty = Quantity * Coefficient <-- 3c
│       ├── Создание движения
│       │   └── NewStockMovement(...) <-- 3d
│       │       ├── recorderID = g.ID <-- model.go:263
│       │       ├── recorderVersion = newVersion <-- model.go:265
│       │       ├── recordType = RecordTypeReceipt <-- model.go:267
│       │       ├── warehouseID, productID <-- model.go:268
│       │       └── quantity = baseQty <-- model.go:270
│       └── Добавление в набор
│           └── movements.AddStock(movement) <-- 3e
└── return movements <-- model.go:276
```

| ID | Описание | Файл |
|----|----------|------|
| **3a** | Создание пустого набора движений. Инициализация контейнера для всех движений документа | `goods_receipt/model.go:250` |
| **3b** | Итерация по строкам документа. Каждая строка табличной части создает одно движение по регистру | `goods_receipt/model.go:254` |
| **3c** | Конвертация количества в базовые единицы. Количество умножается на коэффициент пересчета (например, 1 коробка = 12 штук) | `goods_receipt/model.go:258` |
| **3d** | Создание движения прихода. Формируется структура движения с типом `RecordTypeReceipt` (приход) | `goods_receipt/model.go:262` |
| **3e** | Добавление движения в набор. Движение добавляется в коллекцию для последующей записи | `goods_receipt/model.go:273` |

---

## Trace 4: Stock Validation — Проверка доступности товара с блокировкой

Для расходных документов (`GoodsIssue`) движок проверяет наличие достаточного количества товара с пессимистической блокировкой строк балансов.

```
Процесс проверки доступности товара
└── Posting Engine (engine.go)
    ├── validateStockAvailability() <-- 4a
    │   ├── Сбор expense-движений по складам <-- engine.go:254
    │   ├── Группировка по warehouse+product (pointer map) <-- engine.go:260
    │   ├── Детерминированная сортировка <-- 4b
    │   └── CheckAndReserveStock() вызов <-- 4c
    │       └── Stock Service (service.go)
    │           └── CheckAndReserveStock() <-- service.go:74
    │               └── для каждого товара <-- service.go:75
    │                   ├── GetBalanceForUpdate() <-- 4d
    │                   │   └── Stock Repository
    │                   │       └── SQL: SELECT...FOR UPDATE <-- 4f
    │                   └── Проверка quantity >= required <-- 4e
    └── Если недостаточно → error <-- service.go:82
```

| ID | Описание | Файл |
|----|----------|------|
| **4a** | Сбор расходных движений. Фильтрация только expense-движений для проверки остатков. Группировка через pointer map (`map[string]*StockReservation`) для безопасной мутации | `posting/engine.go:254` |
| **4b** | Сортировка в детерминированном порядке. Предотвращение deadlock через упорядоченную блокировку строк (классическая проблема AB-BA) | `posting/engine.go:283` |
| **4c** | Вызов проверки остатков в сервисе. Передача управления сервису регистра для блокировки и проверки | `posting/engine.go:289` |
| **4d** | Получение баланса с блокировкой `FOR UPDATE`. Пессимистическая блокировка строки баланса до конца транзакции | `stock/service.go:76` |
| **4e** | Проверка достаточности остатка. Если товара недостаточно, возвращается ошибка `InsufficientStock` | `stock/service.go:81` |
| **4f** | SQL запрос с блокировкой. `SELECT ... FOR UPDATE` блокирует строку баланса для текущей транзакции | `register_repo/stock.go:184` |

---

## Trace 5: Recording Movements — Запись движений в таблицу регистра

Движения записываются в таблицу `reg_stock_movements` через batch insert (COPY), что запускает триггер обновления балансов.

```
posting.Engine.recordMovements() <-- engine.go:293
├── Проверка наличия движений <-- 5a
└── Вызов stockService.RecordMovements() <-- 5b
    │
    └── stock.Service.RecordMovements() <-- service.go:30
        ├── Валидация движений (quantity > 0) <-- service.go:36
        └── Вызов repo.CreateMovements() <-- 5c
            │
            └── StockRepo.CreateMovements() <-- stock.go:46
                ├── Получение TxManager из контекста <-- stock.go:52
                ├── Проверка активной транзакции <-- stock.go:53
                ├── Создание BatchInserter <-- stock.go:54
                ├── Формирование массива rows <-- 5e
                │   └── [line_id, recorder_id, period,
                │       warehouse_id, product_id, quantity]
                └── COPY в reg_stock_movements <-- 5d
                    │
                    └── PostgreSQL Trigger (автоматически) <-- 00014_reg_stock.sql:146
                        └── update_stock_balance() <-- 00014_reg_stock.sql:101
                            └── UPDATE reg_stock_balances <-- 00014_reg_stock.sql:132
```

| ID | Описание | Файл |
|----|----------|------|
| **5a** | Проверка наличия движений. Запись выполняется только если есть движения для записи | `posting/engine.go:295` |
| **5b** | Вызов сервиса для записи движений. Передача массива движений в сервис регистра | `posting/engine.go:296` |
| **5c** | Вызов репозитория для вставки. Делегирование физической записи в БД репозиторию | `stock/service.go:46` |
| **5d** | Batch insert через PostgreSQL COPY. Эффективная массовая вставка движений в таблицу `reg_stock_movements` | `register_repo/stock.go:68` |
| **5e** | Подготовка данных для вставки. Формирование массива значений: `line_id`, `recorder_id`, `period`, `warehouse_id`, `product_id`, `quantity` | `register_repo/stock.go:62` |

---

## Trace 6: Database Trigger — Автоматическое обновление балансов

PostgreSQL триггер автоматически обновляет таблицу балансов (`reg_stock_balances`) при каждой вставке или удалении движения.

```
Процесс обновления балансов через триггер БД
│
├── INSERT в reg_stock_movements <-- 5d
│   └── (batch insert через COPY)
│
├── TRIGGER: trg_stock_movements_balance <-- 6a
│   └── AFTER INSERT FOR EACH ROW <-- 00014_reg_stock.sql:147
│
└── FUNCTION: update_stock_balance() <-- 00014_reg_stock.sql:101
    ├── Определение знака движения <-- 6b
    │   ├── IF record_type = 'receipt'
    │   │   └── v_signed_qty := +quantity <-- 00014_reg_stock.sql:126
    │   └── ELSE (expense) <-- 00014_reg_stock.sql:127
    │       └── v_signed_qty := -quantity <-- 00014_reg_stock.sql:128
    │
    ├── UPSERT в reg_stock_balances <-- 6c
    │   ├── INSERT новой записи
    │   └── ON CONFLICT (warehouse_id, product_id) <-- 00014_reg_stock.sql:134
    │
    └── UPDATE существующего баланса <-- 6d
        └── quantity = quantity + v_signed_qty
```

| ID | Описание | Файл |
|----|----------|------|
| **6a** | Определение триггера на таблице движений. Триггер срабатывает `AFTER INSERT OR DELETE` на каждую строку | `00014_reg_stock.sql:146` |
| **6b** | Определение знака движения. Receipt (приход) увеличивает баланс, expense (расход) уменьшает | `00014_reg_stock.sql:125` |
| **6c** | Upsert баланса. Вставка нового баланса или обновление существующего через `ON CONFLICT` | `00014_reg_stock.sql:132` |
| **6d** | Инкрементальное обновление количества. Баланс обновляется добавлением signed quantity (положительной или отрицательной) | `00014_reg_stock.sql:135` |

---

## Trace 7: Unposting Flow — Отмена проведения документа

Процесс отмены проведения удаляет движения документа из регистра, триггер автоматически корректирует балансы.

```
Unpost Flow: Отмена проведения документа
│
├── Service Layer (goods_receipt/service.go)
│   └── Service.Unpost() вызывает движок <-- 7a
│
├── Posting Engine (posting/engine.go)
│   ├── Engine.Unpost() начинает транзакцию <-- 7b
│   │   ├── ReverseMovements() удаление <-- 7c
│   │   │   движений
│   │   ├── MarkUnposted() снятие флага <-- 7g
│   │   └── updateDoc() сохранение в БД <-- engine.go:231
│   │
│   └── txm.RunInTransaction() координация <-- engine.go:222
│
├── Stock Service (registers/stock/service.go)
│   └── Service.ReverseMovements() <-- 7d
│       └── repo.DeleteMovementsByRecorder() <-- service.go:60
│
├── Repository (storage/postgres/register_repo)
│   └── StockRepo.DeleteMovementsByRecorder() <-- stock.go:104
│       └── SQL DELETE WHERE recorder_id <-- 7e
│           AND recorder_version < N
│
└── Database Trigger (migrations/00014_reg_stock)
    └── trg_stock_movements_balance <-- 00014_reg_stock.sql:146
        └── AFTER DELETE срабатывает <-- 00014_reg_stock.sql:147
            └── update_stock_balance() <-- 7f
                └── Реверсирует изменение баланса <-- 00014_reg_stock.sql:109
                    (receipt → -qty, expense → +qty)
```

| ID | Описание | Файл |
|----|----------|------|
| **7a** | Вызов отмены проведения через движок. Сервис документа делегирует отмену проведения движку | `goods_receipt/service.go:269` |
| **7b** | Начало транзакции отмены. Отмена проведения выполняется в атомарной транзакции | `posting/engine.go:222` |
| **7c** | Удаление движений документа. Удаляются все движения с `recorder_id` документа и `recorder_version < текущей+1` | `posting/engine.go:225` |
| **7d** | Вызов репозитория для удаления. Передача команды удаления в слой хранения | `stock/service.go:60` |
| **7e** | SQL DELETE движений. Удаление по `recorder_id` и `recorder_version` для эффективной очистки | `register_repo/stock.go:105` |
| **7f** | Обработка DELETE в триггере. При удалении движения триггер применяет обратную операцию к балансу | `00014_reg_stock.sql:107` |
| **7g** | Снятие флага проведения. Документ помечается как непроведенный (`Posted=false`) | `posting/engine.go:230` |

---

## Trace 8: GoodsIssue — Генерация расходных движений

Документ «Расход товаров» создает expense-движения (в отличие от receipt у GoodsReceipt), что уменьшает остатки на складе.

```
GoodsIssue Document Posting Flow
├── GoodsIssue.GenerateMovements() <-- model.go:244
│   ├── Создание MovementSet <-- 8a
│   ├── Итерація по рядках документа <-- 8b
│   │   ├── Конвертація в базові одиниці <-- model.go:250
│   │   ├── NewStockMovement() з типом <-- model.go:254
│   │   │   └── RecordTypeExpense <-- 8c
│   │   └── movements.AddStock() <-- model.go:265
│   └── return movements <-- model.go:268
├── Service.Post() викликає рушій <-- service.go:226
│   └── postingEngine.Post() <-- 8d
│       ├── Engine.doPost() <-- engine.go:124
│       │   ├── doc.GenerateMovements() <-- engine.go:154
│       │   ├── validateStockAvailability() <-- engine.go:166
│       │   │   └── CheckAndReserveStock() <-- engine.go:289
│       │   │       └── GetBalanceForUpdate <-- service.go:76
│       │   ├── recordMovements() <-- engine.go:171
│       │   │   └── stockService.RecordMovements() <-- engine.go:296
│       │   │       └── repo.CreateMovements() <-- service.go:46
│       │   │           └── COPY в reg_stock_movements <-- stock.go:68
│       │   └── doc.MarkPosted() <-- engine.go:176
│       └── updateDoc() callback <-- engine.go:177
└── Database Trigger
    └── update_stock_balance() <-- 00014_reg_stock.sql:101
        └── quantity = quantity - expense <-- 00014_reg_stock.sql:135
```

| ID | Описание | Файл |
|----|----------|------|
| **8a** | Создание набора движений для расхода. Инициализация контейнера движений для документа расхода | `goods_issue/model.go:245` |
| **8b** | Итерация по строкам расходного документа. Каждая строка создает расходное движение | `goods_issue/model.go:248` |
| **8c** | Тип движения — **РАСХОД**. Критическое отличие: `RecordTypeExpense` вместо `RecordTypeReceipt` уменьшает остаток | `goods_issue/model.go:259` |
| **8d** | Проведение через тот же движок. GoodsIssue использует тот же `Engine.Post`, но с проверкой остатков для expense-движений | `goods_issue/service.go:236` |
