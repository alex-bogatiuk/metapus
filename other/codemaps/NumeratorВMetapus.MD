# Автонумерация в Metapus: Генерация номеров документов и кодов справочников

Карта показывает полный путь автонумерации: от вызова в hook сервиса до UPSERT в `sys_sequences`, две стратегии (Strict и Cached), формат номера, и tenant-aware кеширование.

**Ключевые точки:**
- Интерфейс `numerator.Generator` в domain layer (1a)
- Strict стратегия: UPSERT на каждый номер (2a)
- Cached стратегия: резервирование диапазонов в памяти (3a)
- Формат номера: PREFIX-YEAR-XXXXX (4a)
- Tenant-aware cache keys (5a)
- Вызов из hook сервиса (6a)

---

## Trace 1: Generator Interface — Контракт для domain layer

Domain layer зависит от интерфейса `Generator`, реализация — в `pkg/numerator`.

```
Domain Contract
│
├── core/numerator/generator.go <-- 1a
│   └── type Generator interface {
│       ├── GetNextNumber(ctx, cfg Config, opts *Options, period time.Time) (string, error)
│       └── SetNextNumber(ctx, cfg Config, period time.Time, value int64) error
│   }
│
├── core/numerator/config.go <-- 1b
│   ├── type Config struct {
│   │   ├── Prefix      string   // "INV", "GR", "CP"
│   │   ├── IncludeYear bool     // true → "INV-2024-00001"
│   │   ├── PadWidth    int      // 5 → "00001"
│   │   └── ResetPeriod string   // "year" | "month" | "never"
│   │   }
│   └── DefaultConfig("CP") → {Prefix:"CP", IncludeYear:true, PadWidth:5, ResetPeriod:"year"}
│
├── core/numerator/config.go <-- 1c
│   ├── type Strategy int
│   │   ├── StrategyStrict  — DB UPSERT на каждый номер (без пропусков)
│   │   └── StrategyCached  — резервирование диапазонов (быстрее, но gaps возможны)
│   └── type Options struct {
│       ├── Strategy  Strategy
│       └── RangeSize int64    // для Cached: количество номеров в блоке (default 50)
│       }
│
└── Реализация: pkg/numerator/service.go <-- 1d
    ├── New(querier) — single-tenant mode
    └── NewFromContext() — multi-tenant (querier из context)
```

| ID | Описание | Файл |
|----|----------|------|
| **1a** | Generator interface. Два метода: `GetNextNumber` для генерации и `SetNextNumber` для миграции/ручной установки | `core/numerator/generator.go:15` |
| **1b** | Config. Определяет формат номера: префикс, год, ширина, период сброса. `DefaultConfig("CP")` → `CP-2024-00001` | `core/numerator/config.go:36` |
| **1c** | Strategy. `Strict` — гарантия без пропусков (для бухгалтерских документов). `Cached` — быстрее, но при рестарте могут быть gaps | `core/numerator/config.go:7` |
| **1d** | Service. `NewFromContext()` для Database-per-Tenant — получает querier из tenant context | `pkg/numerator/service.go:105` |

---

## Trace 2: Strict Strategy — UPSERT на каждый номер

Каждый вызов выполняет атомарный UPSERT в `sys_sequences`. Гарантирует последовательные номера без пропусков.

```
Service.GetNextNumber(ctx, cfg, nil, time.Now()) [Strategy: Strict]
│
├── buildKey(cfg, period) <-- 2a
│   ├── ResetPeriod = "year"  → "CP_2024"
│   ├── ResetPeriod = "month" → "CP_2024_01"
│   └── ResetPeriod = "never" → "CP"
│
├── getNextStrict(ctx, prefix, key, period) <-- 2b
│   │
│   ├── getQuerier(ctx) <-- 2c
│   │   ├── useContext=true → tenant.MustGetPool(ctx)
│   │   │   └── Pool тенанта (НЕ транзакция — вне бизнес-tx)
│   │   └── useContext=false → staticQuerier
│   │
│   └── UPSERT в sys_sequences <-- 2d
│       └── SQL:
│           INSERT INTO sys_sequences (key, current_val)
│           VALUES ('CP_2024', 1)
│           ON CONFLICT (key)
│           DO UPDATE SET current_val = sys_sequences.current_val + 1
│           RETURNING current_val
│       │
│       ├── Первый вызов: INSERT → current_val = 1
│       ├── Второй вызов: UPDATE → current_val = 2
│       └── N-й вызов:    UPDATE → current_val = N
│       │
│       └── Атомарность: ON CONFLICT гарантирует <-- 2e
│           └── Конкурентные запросы НЕ получат одинаковый номер
│               (PostgreSQL row-level lock на конфликтующей строке)
│
└── formatNumber(cfg, period, num) <-- 2f
    └── "CP-2024-00001"
```

| ID | Описание | Файл |
|----|----------|------|
| **2a** | Build key. Ключ последовательности зависит от `ResetPeriod`. При `year` — нумерация сбрасывается каждый год | `pkg/numerator/service.go:257` |
| **2b** | Strict entry. Один SQL-запрос на каждый номер (key-based UPSERT). Надёжно, но медленнее Cached | `pkg/numerator/service.go:170` |
| **2c** | Querier selection. В multi-tenant используется `tenant.MustGetPool(ctx)` — pool тенанта напрямую, НЕ через транзакцию (нумерация выполняется в before-create hook, до бизнес-транзакции) | `pkg/numerator/service.go:92` |
| **2d** | Atomic UPSERT. `INSERT INTO sys_sequences (key, ...) ON CONFLICT (key) DO UPDATE ... RETURNING` — атомарная операция. PostgreSQL ставит row-level lock на конфликтующую строку | `pkg/numerator/service.go:174` |
| **2e** | Конкурентная безопасность. Два параллельных запроса на один ключ гарантированно получат разные номера (serialized через row lock) | `pkg/numerator/service.go:177` |
| **2f** | Форматирование. `PREFIX-YEAR-XXXXX` с нулевым заполнением (pad width). Настраивается через Config | `pkg/numerator/service.go:269` |

---

## Trace 3: Cached Strategy — Резервирование диапазонов в памяти

Выделяет блок номеров из БД за один SQL-запрос, затем раздаёт из памяти. Значительно быстрее для массовых операций.

```
Service.GetNextNumber(ctx, cfg, opts, time.Now()) [Strategy: Cached, RangeSize: 50]
│
├── buildKey + tenant-aware cacheKey <-- 3a
│   ├── dbKey = "ORD_2024"
│   └── cacheKey = "tenant-uuid:ORD_2024" (multi-tenant)
│
├── getNextCached(ctx, dbKey, cacheKey, opts) <-- 3b
│   │
│   ├── cacheMu.Lock() — глобальный mutex <-- 3c
│   │
│   ├── Проверка диапазона в памяти <-- 3d
│   │   └── rng := ranges[cacheKey]
│   │       ├── rng.current < rng.max → номер из памяти (fast path)
│   │       └── rng.current >= rng.max → нужен DB fetch
│   │
│   ├── DB fetch: резервирование блока <-- 3e
│   │   └── SQL:
│   │       INSERT INTO sys_sequences (key, current_val)
│   │       VALUES ('ORD_2024', 50)
│   │       ON CONFLICT (key)
│   │       DO UPDATE SET current_val = sys_sequences.current_val + 50
│   │       RETURNING current_val
│   │   │
│   │   └── Пример:
│   │       ├── Первый fetch: INSERT → current_val=50, range=[1..50]
│   │       ├── Второй fetch: UPDATE → current_val=100, range=[51..100]
│   │       └── rng.current = newMax - 50 = 0
│   │           rng.max = newMax = 50
│   │
│   ├── Выдача номера из кэша <-- 3f
│   │   └── rng.current++ → return rng.current
│   │       └── 1, 2, 3, ..., 50 (без обращения к БД)
│   │
│   └── cacheMu.Unlock()
│
├── Преимущества Cached:
│   ├── 1 SQL-запрос на 50 номеров (vs 50 SQL в Strict) <-- 3g
│   └── В памяти: O(1) с mutex lock
│
└── Недостатки Cached:
    └── При рестарте приложения: gaps в нумерации <-- 3h
        └── Пример: выдано 23 из 50 → номера 24-50 потеряны
```

| ID | Описание | Файл |
|----|----------|------|
| **3a** | Tenant-aware key. В multi-tenant режиме к cache key добавляется `tenantID:` — предотвращает коллизии между тенантами в одном процессе | `pkg/numerator/service.go:147` |
| **3b** | Cached entry. Проверяет in-memory cache, при исчерпании — fetch из БД | `pkg/numerator/service.go:188` |
| **3c** | Global mutex. `sync.Mutex` защищает map `ranges`. Всё под одним lock — простота важнее throughput (нумерация — не hot path) | `pkg/numerator/service.go:189` |
| **3d** | Range check. Если `current < max` — номер берётся из памяти без SQL. Fast path | `pkg/numerator/service.go:199` |
| **3e** | Range allocation. UPSERT инкрементирует `current_val` на `RangeSize` (default 50). Возвращает верхнюю границу диапазона | `pkg/numerator/service.go:211` |
| **3f** | In-memory increment. `rng.current++` — атомарно под mutex. Без SQL | `pkg/numerator/service.go:225` |
| **3g** | Performance. 50x меньше SQL-запросов для массовых операций (импорт, seed) | — |
| **3h** | Gaps при рестарте. Не использованные номера из кэша теряются. Неприемлемо для бухгалтерских документов | — |

---

## Trace 4: Формат номера — PREFIX-YEAR-XXXXX

```
formatNumber(cfg, period, num) <-- service.go:269
│
├── Вход: cfg={Prefix:"GR", IncludeYear:true, PadWidth:5}, period=2024-03-15, num=42
│
├── IncludeYear = true <-- 4a
│   └── fmt.Sprintf("%s-%s-%0*d", "GR", "2024", 5, 42)
│       └── "GR-2024-00042"
│
├── IncludeYear = false <-- 4b
│   └── fmt.Sprintf("%s-%0*d", "GR", 5, 42)
│       └── "GR-00042"
│
└── Примеры по типам сущностей: <-- 4c
    ├── Counterparty:    "CP-2024-00001"   (prefix="CP")
    ├── GoodsReceipt:    "GR-2024-00001"   (prefix="GR")
    ├── GoodsIssue:      "GI-2024-00001"   (prefix="GI")
    ├── Nomenclature:    "NOM-2024-00001"  (prefix="NOM")
    └── Invoice:         "INV-2024-00001"  (prefix="INV")
```

| ID | Описание | Файл |
|----|----------|------|
| **4a** | С годом. Стандартный формат для документов: `PREFIX-YEAR-XXXXX`. Нумерация сбрасывается ежегодно | `pkg/numerator/service.go:276` |
| **4b** | Без года. Для сущностей с вечной нумерацией (`ResetPeriod: "never"`) | `pkg/numerator/service.go:278` |
| **4c** | Примеры. Каждая сущность имеет свой префикс, задаваемый через `DefaultConfig(prefix)` | — |

---

## Trace 5: SetNextNumber — Ручная установка (миграция)

Для миграций данных из 1С: установка стартового значения нумерации.

```
Service.SetNextNumber(ctx, cfg, period, value=1000) <-- service.go:230
│
├── buildKey(cfg, period) → "INV_2024" <-- 5a
│
├── UPSERT с фиксированным значением <-- 5b
│   └── SQL:
│       INSERT INTO sys_sequences (key, current_val)
│       VALUES ('INV_2024', 1000)
│       ON CONFLICT (key) DO UPDATE SET current_val = 1000
│       RETURNING current_val
│
└── Инвалидация кэша <-- 5c
    └── cacheMu.Lock()
        └── delete(ranges, cacheKey)
            └── Следующий GetNextNumber выполнит fresh DB fetch
```

| ID | Описание | Файл |
|----|----------|------|
| **5a** | Key для set. Тот же формат ключа, что и для GetNextNumber | `pkg/numerator/service.go:297` |
| **5b** | Force set. `ON CONFLICT DO UPDATE SET current_val = $value` — перезаписывает текущее значение | `pkg/numerator/service.go:301` |
| **5c** | Cache invalidation. При ручной установке кэш очищается — предотвращает выдачу старых номеров из памяти | `pkg/numerator/service.go:309` |

---

## Trace 6: Интеграция — Вызов из Hook сервиса

Нумерация вызывается в `OnBeforeCreate` hook, ДО бизнес-транзакции.

```
Интеграция нумератора с сервисом
│
├── Регистрация hook <-- 6a
│   └── counterparty/service.go:
│       └── base.Hooks().OnBeforeCreate(svc.prepareForCreate)
│
├── Вызов при создании сущности <-- 6b
│   └── CatalogService.Create(ctx, entity)
│       ├── 1. entity.Validate(ctx)
│       ├── 2. hooks.RunBeforeCreate(ctx, entity) ◄── ЗДЕСЬ
│       │   └── prepareForCreate(ctx, cp)
│       │       └── if cp.Code == "" <-- 6c
│       │           ├── cfg := numerator.DefaultConfig("CP")
│       │           ├── code, err := s.numerator.GetNextNumber(ctx, cfg, nil, time.Now())
│       │           └── cp.Code = code  // "CP-2024-00042"
│       ├── 3. txm.RunInTransaction { repo.Create }
│       └── 4. hooks.RunAfterCreate
│
├── Важно: нумерация ВНЕ бизнес-транзакции <-- 6d
│   └── getQuerier(ctx) → tenant.MustGetPool(ctx) (НЕ tx)
│       └── Причина: если бизнес-tx откатится, номер
│           "сгорит" — это допустимо (gaps) для Strict
│       └── Альтернатива: вызывать внутри tx → но это
│           создаёт contention на sys_sequences row lock
│
└── Документы: аналогично через document service hooks <-- 6e
    └── goods_receipt/service.go:
        └── OnBeforeCreate → numerator.GetNextNumber(cfg("GR"), ...)
```

| ID | Описание | Файл |
|----|----------|------|
| **6a** | Hook registration. Нумерация регистрируется как `OnBeforeCreate` hook при создании сервиса | `counterparty/service.go:43` |
| **6b** | Порядок выполнения. Hook выполняется ПОСЛЕ валидации, но ДО транзакции | `domain/service.go:95` |
| **6c** | Условная генерация. Номер генерируется только если `Code` пуст — позволяет задать номер вручную | `counterparty/service.go:52` |
| **6d** | Вне транзакции. Нумератор использует pool напрямую (не tx). При rollback бизнес-tx номер "сгорает", но это допустимо | `pkg/numerator/service.go:117` |
| **6e** | Документы. Аналогичный паттерн для GoodsReceipt, GoodsIssue и других документов | — |

---

## Trace 7: Сводная диаграмма — Таблица sys_sequences и стратегии

```
┌─────────────────────────────────────────────────────────────────────────┐
│               АВТОНУМЕРАЦИЯ: СТРАТЕГИИ И ТАБЛИЦА                         │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  sys_sequences (PostgreSQL, в БД каждого тенанта)                       │
│  ┌────────────────────────────────────────────────────┐                │
│  │  key            │ sequence_type │ year │ current_val│                │
│  ├────────────────────────────────────────────────────┤                │
│  │  CP_2024        │ CP            │ 2024 │ 42         │                │
│  │  GR_2024        │ GR            │ 2024 │ 15         │                │
│  │  GI_2024        │ GI            │ 2024 │ 7          │                │
│  │  NOM_2024       │ NOM           │ 2024 │ 230        │                │
│  │  ORD_2024       │ ORD           │ 2024 │ 1050       │  ← cached     │
│  └────────────────────────────────────────────────────┘                │
│                                                                          │
│  Strict Strategy (default):                                              │
│  ┌─────────────────────────────────────────────┐                       │
│  │  Запрос #1: UPSERT → current_val=1 → "CP-2024-00001"│              │
│  │  Запрос #2: UPSERT → current_val=2 → "CP-2024-00002"│              │
│  │  Запрос #3: UPSERT → current_val=3 → "CP-2024-00003"│              │
│  │  ...                                          │                       │
│  │  ✅ Без пропусков  ⚠️ 1 SQL на номер         │                       │
│  └─────────────────────────────────────────────┘                       │
│                                                                          │
│  Cached Strategy (RangeSize=50):                                        │
│  ┌─────────────────────────────────────────────┐                       │
│  │  DB fetch #1: UPSERT +50 → current_val=50   │                       │
│  │    Memory: [1,2,3,...,50] ← без SQL           │                       │
│  │  DB fetch #2: UPSERT +50 → current_val=100  │                       │
│  │    Memory: [51,52,...,100] ← без SQL          │                       │
│  │  ...                                          │                       │
│  │  ✅ 1 SQL на 50 номеров  ⚠️ Gaps при restart │                       │
│  └─────────────────────────────────────────────┘                       │
│                                                                          │
│  Когда какую стратегию:                                                 │
│  ├── Strict: счета, накладные, бухгалтерские документы                 │
│  └── Cached: внутренние заказы, массовый импорт, seed                  │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```
