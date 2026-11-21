# Добавление провайдера внешней аренды ресурсов

> **TL;DR:** Новый вендор Energy/Bandwidth (TronSave, TronZap, …) — это адаптер
> в `internal/configuration/crypto/infrastructure/crypto/resources/<code>/` с файлами `client.go`,
> `adapter.go`, `adapter_test.go`. Оркестрация аренды в `crypto_worker` не
> импортирует пакет вендора. Failover действует только на **новый** заказ.

> **Тип:** How-To
> **Аудитория:** Developer
> **Связанные:** [add-blockchain-network.md](add-blockchain-network.md),
> [crypto-processing.md](../systems/crypto-processing.md)

## Граница

`NetworkDriver` не арендует Energy. Family объявляет `ExternalResourceMode`.
Поставщик аренды — отдельный registry, как RPC endpoint не является chain family.

| Слой | Ответственность |
|---|---|
| `domain/crypto.ExternalResourceProvider` | Place / Query / Reclaim / Quote |
| `crypto_worker.SharedResourcePreparer` | FSM, capacity, ledger, router |
| `crypto/resources/<vendor>` | HTTP и маппинг статусов вендора |
| `cmd/worker` | сборка адаптера из секретов |

In-flight заказ (`ordering` / `held` / `reclaiming`) закрывает только свой
`rental.Provider`. Ambiguous Place не создаёт заказ у следующего вендора.

## Шаблон пакета

```text
internal/configuration/crypto/infrastructure/crypto/resources/tronsave/
├── client.go         — HTTP, подпись, errno вендора
├── adapter.go        — crypto.ExternalResourceProvider
└── adapter_test.go   — HMAC/JSON, sentinel errors, идемпотентный Place
```

Скопируйте `resources/itrx/` и замените протокол. `adapter.go` обязан:

- объявить `Capabilities()` (family, reusable lease, query-by-client-id, reclaim);
- принимать наш `ClientOrderID` (`out_trade_no`);
- наружу отдавать `ResourceOrderStatus`, не errno;
- мапить HTTP 401/403 → `ErrResourceProviderUnauthorized`, 429/5xx → `ErrResourceProviderTransient`.

Без `ReusableFixedLease` оркестратор не шарит hourly pool.

## Сборка

В `cmd/worker` добавьте новый код провайдера в `buildAccountProviderFactory`.
Фабрика получает credentials из зашифрованной записи аккаунта.
`crypto_worker` не импортирует `crypto/resources/*` — это ловит `architecture_test.go`.

Создайте `cat_crypto_provider_accounts` через админский UI и сохраните credentials.
Base URL и callback URL входят в credentials аккаунта.

## Policy

`sys_crypto_external_resource_policies.provider_account_ids` — упорядоченный
список UUID аккаунтов. Для `prefer_rental` / `rental_required` список непустой.
Ручной switch: убрать аккаунт из списка или установить `is_active=false`.
Живые аренды обслуживаются через аккаунт, на котором были созданы.

## Проверки

```bash
go test ./internal/configuration/crypto/domain/crypto/ ./internal/configuration/crypto/infrastructure/crypto/resources/... ./internal/configuration/crypto/infrastructure/crypto_worker/ -count=1
```
