# Модуль 3: Merchant API — Создание Инвойса

## Как мерчант создаёт инвойс?

Мерчант (интернет-магазин) отправляет HTTP-запрос:

```
POST /merchant/v1/invoices
X-Tenant-ID: 5cfe45cb-...
X-Api-Key: mk_live_abc123...

{
  "token_id": "uuid-of-usdt",
  "expected_amount": "10000000",     ← 10 USDT (6 decimals: 10 × 10⁶)
  "callback_url": "https://shop.com/webhook",
  "external_id": "order-12345"
}
```

**Никакого JWT-токена!** Merchant API использует API-ключ (`X-Api-Key`), потому что мерчант — это внешняя система (сервер магазина), у которой нет "пользователя" с логином/паролем.

## Hook (Хук) — перехватчик событий

При создании инвойса срабатывает хук `OnBeforeCreate`:

```go
merchantInvoiceSvc.Hooks().OnBeforeCreate(func(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
    // 1. Узнаём, в какой сети работает токен
    tok, err := merchantTokenRepo.GetByID(ctx, doc.TokenID)
    
    // 2. Берём свободный кошелёк из пула для этой сети
    w, err := merchantWalletSvc.LeaseForInvoice(ctx, doc.ID, tok.NetworkID)
    
    // 3. Привязываем кошелёк к инвойсу
    doc.WalletID = &w.ID
    return nil
})
```

Цепочка вызовов:
```
Handler.Create()
  → Service.Create()
    → Hooks.OnBeforeCreate()    ← Арендуем кошелёк (WalletID заполняется)
      → Validate()              ← Проверяем обязательные поля
        → Numerator.Generate()  ← Генерируем номер "CI-00001"
          → Repository.Create() ← INSERT в PostgreSQL
```

## Wallet Pool — паттерн Lease/Release (Аренда/Возврат)

Аналогия: гардероб в театре. Зритель приходит (инвойс создан) — ему дают свободную вешалку (кошелёк). Уходит (платёж подтверждён) — вешалка возвращается.

```
[Wallet Pool]
┌─────────┬─────────┬─────────┬─────────┐
│ Free ✓  │ Leased  │ Free ✓  │ Leased  │
│ TXabc1  │ TXdef2  │ TXghi3  │ TXjkl4  │
└─────────┴─────────┴─────────┴─────────┘
     ↑
     └── LeaseForInvoice() берёт первый свободный
```

**Зачем пул?** У каждого инвойса должен быть **уникальный** блокчейн-адрес. Иначе, если два клиента отправят на один адрес, мы не поймём, кто за что заплатил. Но генерировать новый адрес на каждый платёж дорого (запрос в Vault). Поэтому мы **переиспользуем** адреса.

## Ответ мерчанту

```json
{
  "id": "uuid-of-invoice",
  "wallet_address": "TXabc1...",
  "expected_amount": "10000000",
  "status": "created",
  "expires_at": "2026-05-19T10:55:00Z"
}
```

Мерчант показывает `wallet_address` клиенту на странице оплаты. С этого момента мяч переходит на сторону **Worker'а** — сервер больше не участвует.

## Ключевые файлы

- [`cmd/server/main.go`](../../cmd/server/main.go) — Merchant API setup (строки 179–210)
- [`internal/infrastructure/http/v1/router.go`](../../internal/infrastructure/http/v1/router.go) — `registerMerchantPublicRoutes`

## Паттерны

- **Hook** (OnBeforeCreate) — callback для расширения lifecycle
- **Lease/Release** — управление пулом ресурсов
- **API Key Auth** — отдельная аутентификация для внешних систем
