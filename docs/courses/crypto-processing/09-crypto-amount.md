# Модуль 9: CryptoAmount — Типобезопасные деньги

## Почему нельзя использовать `float64`?

```go
fmt.Println(0.1 + 0.2)  // 0.30000000000000004  💥
```

Потеря точности при `float64` = потеря денег клиентов. В финансовых системах это **недопустимо**.

## Решение Metapus: `int64` с minor units

```go
type CryptoAmount int64  // Простой int64!
```

Храним всё в **минимальных единицах** (minor units):
- 1 USDT = 1,000,000 sun (6 decimals) → `CryptoAmount(1000000)`
- 1 BTC = 100,000,000 satoshi (8 decimals) → `CryptoAmount(100000000)`
- 1 ETH = 1,000,000,000 gwei (9 decimals) → `CryptoAmount(1000000000)`

`max(int64) = 9.2 × 10¹⁸`. Для USDT — **9.2 триллиона USDT**. Для ETH (gwei) — 9.2 миллиарда ETH. Более чем достаточно.

## `int64` vs `big.Int`

| Критерий | `big.Int` | `int64` (CryptoAmount) |
|----------|-----------|------------------------|
| Аллокации | Heap (GC нагрузка) | Zero allocs (value type) |
| Размер | 24+ байт + данные | 8 байт |
| PostgreSQL | NUMERIC (медленнее) | BIGINT (нативные индексы) |
| JSON | Строка `"1000000"` | Число `1000000` |
| Арифметика | Методы с аллокациями | Нативные CPU инструкции |

## Защита от переполнения (Overflow Detection)

Обычный Go `int64`: `math.MaxInt64 + 1 = math.MinInt64` (молчаливо). В финансах — катастрофа.

CryptoAmount **паникует** при переполнении:

```go
func (a CryptoAmount) Add(b CryptoAmount) CryptoAmount {
    result := int64(a) + int64(b)
    if (int64(b) > 0 && result < int64(a)) || (int64(b) < 0 && result > int64(a)) {
        panic(fmt.Sprintf("CryptoAmount overflow: %d + %d", a, b))
    }
    return CryptoAmount(result)
}
```

**Panic — осознанный выбор.** В финансах лучше **упасть**, чем молча записать неправильную сумму.

## MulDiv — безопасное вычисление процентов

```go
func (a CryptoAmount) MulDiv(numerator, denominator int64) CryptoAmount {
    product, ok := safeMultiply(int64(a), numerator)
    if !ok {
        panic(fmt.Sprintf("CryptoAmount overflow: %d * %d", a, numerator))
    }
    return CryptoAmount(product / denominator)
}

func safeMultiply(a, b int64) (int64, bool) {
    product := a * b
    if product/a != b {  // Обратное деление не совпадает → переполнение
        return 0, false
    }
    return product, true
}
```

## Сериализация: SQL + JSON

CryptoAmount реализует 4 интерфейса для "бесшовной" работы:

```go
// PostgreSQL BIGINT ↔ int64
func (a CryptoAmount) Value() (driver.Value, error) { return int64(a), nil }
func (a *CryptoAmount) Scan(src any) error { ... }

// JSON number
func (a CryptoAmount) MarshalJSON() ([]byte, error) { ... }
func (a *CryptoAmount) UnmarshalJSON(data []byte) error { ... }
```

`pgx` и `json.Marshal` автоматически знают, как конвертировать CryptoAmount.

## Ключевые файлы

- [`internal/core/types/crypto_amount.go`](../../internal/core/types/crypto_amount.go) — CryptoAmount

## Паттерны Go

- **Value type** (`type CryptoAmount int64`) — zero allocs, stack-allocated
- **Overflow detection** — panic при переполнении (финансовый инвариант)
- `driver.Valuer` / `sql.Scanner` — SQL-сериализация
- `json.Marshaler` / `json.Unmarshaler` — JSON-сериализация
- `strconv` вместо `fmt` для числовых конвертаций
