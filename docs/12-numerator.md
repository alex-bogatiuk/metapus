# Автонумерация (Numerator)

> Генерация номеров документов и кодов справочников. Две стратегии: Strict (без пропусков) и Cached (быстрее, но gaps возможны). Tenant-aware кеширование.

---

## Интерфейс Generator

Domain зависит от интерфейса, реализация — в `pkg/numerator`:

```go
// core/numerator/generator.go
type Generator interface {
    GetNextNumber(ctx, cfg Config, opts *Options, period time.Time) (string, error)
    SetNextNumber(ctx, cfg Config, period time.Time, value int64) error
}
```

### Config — формат номера

```go
type Config struct {
    Prefix      string   // "INV", "GR", "CP"
    IncludeYear bool     // true → "INV-2024-00001"
    PadWidth    int      // 5 → "00001"
    ResetPeriod string   // "year" | "month" | "never"
}
```

`DefaultConfig("CP")` → `{Prefix:"CP", IncludeYear:true, PadWidth:5, ResetPeriod:"year"}`

### Options — стратегия

```go
type Strategy int
const (
    StrategyStrict  // DB UPSERT на каждый номер (без пропусков)
    StrategyCached  // Резервирование диапазонов (быстрее, но gaps)
)

type Options struct {
    Strategy  Strategy
    RangeSize int64    // для Cached: количество номеров в блоке (default 50)
}
```

---

## Strict Strategy — UPSERT на каждый номер

Каждый вызов → один атомарный SQL. Гарантирует **последовательные номера без пропусков**.

```sql
INSERT INTO sys_sequences (key, current_val)
VALUES ('CP_2024', 1)
ON CONFLICT (key)
DO UPDATE SET current_val = sys_sequences.current_val + 1
RETURNING current_val
```

- PostgreSQL row-level lock на конфликтующей строке → конкурентные запросы **гарантированно** получат разные номера
- Ключ зависит от `ResetPeriod`: `"CP_2024"` (year), `"CP_2024_01"` (month), `"CP"` (never)
- Нумератор использует **pool напрямую** (не транзакцию) — выполняется в before-create hook, до бизнес-tx

**Когда использовать:** счета, накладные, бухгалтерские документы

---

## Cached Strategy — резервирование диапазонов

Выделяет блок номеров за один SQL, затем раздаёт из памяти. **1 SQL на 50 номеров** вместо 50.

```
GetNextNumber(ctx, cfg, opts{Cached, RangeSize:50}, time.Now())
│
├── cacheKey = "tenant-uuid:ORD_2024" (multi-tenant)
├── cacheMu.Lock()
├── rng := ranges[cacheKey]
│   ├── rng.current < rng.max → номер из памяти (fast path, без SQL)
│   └── rng.current >= rng.max → DB fetch:
│       └── UPSERT +50 → current_val=100, range=[51..100]
├── rng.current++ → return
└── cacheMu.Unlock()
```

**Tenant-aware:** к cache key добавляется `tenantID:` — предотвращает коллизии между тенантами в одном процессе.

**Когда использовать:** внутренние заказы, массовый импорт, seed данных

**Недостаток:** при рестарте приложения неиспользованные номера из кэша **теряются** (gaps).

---

## Формат номера — PREFIX-YEAR-XXXXX

```
cfg={Prefix:"GR", IncludeYear:true, PadWidth:5}, num=42
→ "GR-2024-00042"

cfg={Prefix:"GR", IncludeYear:false, PadWidth:5}, num=42
→ "GR-00042"
```

**Примеры по типам:**

| Сущность | Prefix | Пример |
|----------|--------|--------|
| Counterparty | CP | CP-2024-00001 |
| GoodsReceipt | GR | GR-2024-00001 |
| GoodsIssue | GI | GI-2024-00001 |
| Nomenclature | NOM | NOM-2024-00001 |
| Invoice | INV | INV-2024-00001 |

---

## SetNextNumber — ручная установка

Для миграций данных из 1С — установка стартового значения:

```go
Service.SetNextNumber(ctx, cfg, period, value=1000)
// → UPSERT с фиксированным значением
// → Инвалидация кэша (delete(ranges, cacheKey))
```

---

## Интеграция — вызов из Hook сервиса

Нумерация вызывается в `OnBeforeCreate` hook, **ДО бизнес-транзакции**:

```go
func NewService(repo Repository, numerator *numerator.Service) *Service {
    base := domain.NewCatalogService(config)
    svc := &Service{CatalogService: base}
    base.Hooks().OnBeforeCreate(svc.prepareForCreate)
    return svc
}

func (s *Service) prepareForCreate(ctx context.Context, cp *Counterparty) error {
    if cp.Code == "" {
        cfg := numerator.DefaultConfig("CP")
        code, err := s.numerator.GetNextNumber(ctx, cfg, nil, time.Now())
        if err != nil { return err }
        cp.Code = code  // "CP-2024-00042"
    }
    return nil
}
```

**Условная генерация:** номер генерируется только если `Code` пуст — позволяет задать вручную.

**Вне транзакции:** нумератор использует pool напрямую (не tx). При rollback бизнес-tx номер "сгорает" — допустимо для Strict.

---

## Таблица sys_sequences

```
sys_sequences (PostgreSQL, в БД каждого тенанта)
┌────────────────┬───────────────┬──────┬────────────┐
│ key            │ sequence_type │ year │ current_val│
├────────────────┼───────────────┼──────┼────────────┤
│ CP_2024        │ CP            │ 2024 │ 42         │
│ GR_2024        │ GR            │ 2024 │ 15         │
│ ORD_2024       │ ORD           │ 2024 │ 1050       │ ← cached
└────────────────┴───────────────┴──────┴────────────┘
```

---

## Связанные документы

- [09-crud-pipeline.md](09-crud-pipeline.md) — место нумератора в hook pipeline
- [05-domain-layer.md](05-domain-layer.md) — hooks в сервисах
- [14-howto-new-entity.md](14-howto-new-entity.md) — добавление нумерации к новой сущности
