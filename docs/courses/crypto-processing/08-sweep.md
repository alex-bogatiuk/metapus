# Модуль 8: Sweep — Сбор средств с пул-кошельков

## Зачем нужен Sweep?

Деньги лежат "разбросанными" по пул-кошелькам. Sweep — сбор средств на **один горячий кошелёк** (Hot Wallet).

**Экономическая ловушка:** TRC-20 перевод стоит ~$2. Если клиент заплатил $5 и свипнуть сразу — комиссия съест **40%** суммы!

## Два режима Sweep

### Legacy (threshold = 0)

Свип сразу после каждого подтверждённого платежа:
```go
if sweepCfg.IsZeroThreshold() {
    return p.walletSvc.MarkSweepPending(ctx, payment.WalletID)
}
```

### Threshold mode (threshold > 0)

Копим платежи, пока баланс не достигнет порога. Один свип вместо десяти — **экономия 90%**.

## Двухуровневая конфигурация (NULL-coalescing)

```
Приоритет 1: Мерчант override → reg_merchant_token_config (nullable)
Приоритет 2: Token default    → cat_tokens (обязательный)
```

```go
func (r *SweepConfigResolver) Resolve(ctx, merchantID, tokenID) (SweepConfig, error) {
    // 1. Всегда берём дефолт из Token
    tok, _ := r.tokenRepo.GetByID(ctx, tokenID)
    cfg := SweepConfig{Threshold: tok.SweepThreshold, MaxAgeHours: tok.SweepMaxAgeHours}

    // 2. Пробуем мерчантский override
    override, _ := r.merchantConfigRepo.Get(ctx, merchantID, tokenID)
    if override != nil {
        if override.SweepThreshold != nil {      // NULL = "использовать дефолт"
            cfg.Threshold = *override.SweepThreshold
        }
        if override.SweepMaxAgeHours != nil {
            cfg.MaxAgeHours = *override.SweepMaxAgeHours
        }
    }
    return cfg, nil
}
```

## Sweep Evaluation Loop

Каждые 60 секунд CryptoProcessor запускает оценку:

```
1. SQL-запрос: найди кошельки с подтверждёнными, не свипнутыми платежами
2. Суммируй баланс (GROUP BY wallet_id)
3. Резолви конфигурацию (Merchant override → Token default)
4. Проверь: баланс ≥ threshold ИЛИ время > maxAgeHours?
5. Да → MarkSweepPending(walletID)
```

SQL-запрос:
```sql
SELECT w.id, w.merchant_id, w.last_swept_at, p.token_id,
       COALESCE(SUM(p.amount), 0) AS balance,
       MIN(p.confirmed_at) AS oldest_payment_at
FROM cat_wallets w
INNER JOIN doc_crypto_payments p ON p.wallet_id = w.id
WHERE w.tier = 'pool'
  AND w.status IN ('free', 'assigned')
  AND p.status = 'confirmed'
  AND p.confirmed_at > COALESCE(w.last_swept_at, '1970-01-01')
GROUP BY w.id, w.merchant_id, w.last_swept_at, p.token_id
HAVING COALESCE(SUM(p.amount), 0) > 0
```

Два триггера:
```go
thresholdMet := c.balance.Cmp(cfg.Threshold) >= 0  // Баланс >= порог?
ageMet := time.Since(*c.lastSweptAt) > maxAge       // Слишком давно не свипили?

if thresholdMet || ageMet {
    p.walletSvc.MarkSweepPending(ctx, c.walletID)
}
```

## Полная картина жизни кошелька

```
[Free] ──Lease──→ [Leased] ──Payment Confirmed──→ ?
                                │
                    threshold=0 │ threshold>0
                                │
                    [SweepPending]  [Free] (вернули в пул)
                        │              │
                     Sweep TX     Eval Loop: баланс >= threshold?
                        │              │ Да
                     [Free]       [SweepPending]
                                       │
                                    Sweep TX → [Free]
```

## Ключевые файлы

- [`internal/infrastructure/crypto_worker/processor.go`](../../internal/infrastructure/crypto_worker/processor.go) — `evaluateSweeps()`
- [`internal/domain/crypto/sweep_resolver.go`](../../internal/domain/crypto/sweep_resolver.go) — `SweepConfigResolver`
- [`internal/domain/crypto/sweep_config.go`](../../internal/domain/crypto/sweep_config.go) — `SweepConfig`

## Паттерны

- **NULL-coalescing** — Merchant override → Token default
- **Nullable fields** — `*int` (`nil` = "не задано")
- **Threshold-based batching** — экономия на комиссиях
- **Evaluation Loop** — периодическая оценка с GROUP BY + HAVING
