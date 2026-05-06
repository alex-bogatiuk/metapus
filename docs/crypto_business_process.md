# Бизнес-процесс криптопроцессинга Metapus

## Общая архитектура

Криптопроцессинг Metapus — это **acquiring-сервис** для приёма криптовалютных платежей. Мерчант интегрируется через REST API, получает адрес для оплаты, а система автоматически отслеживает блокчейн и подтверждает получение средств.

## Полный цикл приёма платежа

```mermaid
sequenceDiagram
    participant Client as Клиент мерчанта
    participant Merchant as Мерчант (Backend)
    participant API as Metapus API
    participant DB as Database
    participant Worker as CryptoProcessor
    participant Chain as Blockchain (TRON)

    Note over Merchant,API: 1. Создание инвойса
    Merchant->>API: POST /document/crypto-invoice
    API->>DB: INSERT doc_crypto_invoices (status='created')
    API->>DB: LeaseForInvoice: SELECT pool wallet FOR UPDATE SKIP LOCKED
    API->>DB: UPDATE wallet SET status='leased', leased_for_id=invoiceID
    API-->>Merchant: {invoiceId, walletAddress, expectedAmount, expiresAt}
    Merchant-->>Client: Показать QR / адрес кошелька

    Note over Client,Chain: 2. Оплата
    Client->>Chain: Перевод USDT на walletAddress

    Note over Worker,Chain: 3. Обнаружение
    loop Каждые 3 сек (polling)
        Worker->>Chain: GET /v1/contracts/{contract}/events?min_timestamp=...
        Chain-->>Worker: TRC-20 Transfer events
    end
    Worker->>DB: FindByAddress → match wallet
    Worker->>DB: wallet.LeasedForID → find invoice
    Worker->>DB: INSERT doc_crypto_payments (status='detected')
    Worker->>DB: UPDATE invoice SET received_amount += amount

    Note over Worker,Chain: 4. Подтверждения
    Worker->>Chain: GET confirmations для tx_hash
    Worker->>DB: FSM: detected → confirming (confs ≥ 1)
    Worker->>DB: FSM: confirming → confirmed (confs ≥ required)

    Note over Worker,DB: 5. Завершение
    Worker->>DB: UPDATE invoice SET status='confirmed'
    Worker->>DB: UPDATE wallet SET status='sweep_pending'
    Worker->>DB: INSERT reg_crypto_balance_movements
    Worker->>Merchant: POST callbackUrl {status: 'confirmed', txHash, amount}
```

## Ключевые сущности

### Справочники
| Сущность | Назначение |
|----------|-----------|
| **Merchant** | Мерчант-клиент сервиса. Имеет `kybStatus` (pending/approved/rejected), `commissionRate` |
| **Wallet** | Крипто-кошелёк. Tier: pool/hot/warm/cold. Status: free/leased/sweep_pending/frozen |
| **Token** | Криптовалюта (USDT-TRC20). Хранит `contract_address`, `decimal_places` |
| **BlockchainNetwork** | Сеть (TRON, ETH). Хранит `confirmations_needed` |

### Документы
| Документ | Назначение |
|----------|-----------|
| **CryptoInvoice** | Счёт на оплату. FSM: created → partially_paid → paid → confirmed → expired/cancelled |
| **CryptoPayment** | Зафиксированная транзакция. FSM: detected → confirming → confirmed → settled / reorged |
| **CryptoWithdrawal** | Вывод средств мерчантом. FSM: created → signed → broadcast → confirmed / failed |
| **CryptoSweep** | Консолидация средств из pool → hot wallet. Системный документ |

## FSM статусы инвойса

```mermaid
stateDiagram-v2
    [*] --> created: POST /crypto-invoice
    created --> partially_paid: Получен частичный платёж
    created --> expired: ExpiresAt < NOW()
    created --> cancelled: Ручная отмена
    partially_paid --> paid: received ≥ expected
    paid --> confirmed: Все confirmations получены
    confirmed --> [*]: Webhook → мерчант
```

## FSM статусы платежа

```mermaid
stateDiagram-v2
    [*] --> detected: TX в mempool/block
    detected --> confirming: confs ≥ 1
    confirming --> confirmed: confs ≥ required
    confirming --> reorged: Chain reorg
    confirmed --> settled: Funds settled
    reorged --> detected: TX re-detected
```

## Wallet Leasing (Пул адресов)

Ключевой механизм — **pool wallets**:

1. При создании инвойса система атомарно «арендует» свободный pool-кошелёк:
   ```sql
   SELECT id FROM cat_wallets
   WHERE network_id = $1 AND status = 'free' AND tier = 'pool'
   FOR UPDATE SKIP LOCKED  -- lock-free concurrency
   LIMIT 1
   ```
2. Кошелёк переходит в `status = 'leased'`, `leased_for_id = invoiceID`
3. Клиент мерчанта видит **уникальный адрес** для оплаты
4. После подтверждения: `status = 'sweep_pending'` → CryptoSweep перемещает средства в hot wallet
5. После sweep: wallet возвращается в `status = 'free'`

## Мониторинг блокчейна

[CryptoProcessor](file:///c:/Users/user/go/src/metapus/internal/infrastructure/crypto_worker/processor.go) запускается **per-tenant** в Worker:

- **TRON Watcher** — polling TronGrid API каждые 3 сек
- **Adaptive polling** — ускоряется при обнаружении событий, замедляется при простое
- **Checkpoint** — состояние сохраняется в `sys_watcher_state` (crash recovery)
- **EventProcessor** — chain-agnostic бизнес-логика (можно добавить ETH, TON)

## Текущий статус реализации

> [!WARNING]
> ### Wallet Leasing НЕ подключен к CreateInvoice
> 
> `LeaseForInvoice()` **реализован** в repo и service, но **не вызывается** при создании инвойса.
> В `CryptoInvoiceRegistration.Build()` нет hook'а `OnBeforeCreate` для автоматического lease'а.
> 
> **Без этого цикл не работает**: созданный инвойс не имеет wallet'а → watcher не может связать входящую транзакцию с инвойсом.
> 
> Это критический gap — нужно решить, подключать ли lease автоматически через hook или через отдельный API endpoint.

## Можно ли протестировать полный цикл?

**Нет, полноценно в текущем состоянии нельзя**, по нескольким причинам:

1. **Wallet leasing не подключен** — инвойс создаётся без привязки к кошельку
2. **TRON Shasta testnet** — нужен реальный перевод USDT на тестовом контракте
3. **Worker должен быть запущен** с правильным `TRON_RPC_URL` и `TRON_API_KEY`

### Что можно проверить через API:
- ✅ CRUD инвойсов, мерчантов, кошельков
- ✅ Фильтры с enum-dropdown'ами (результат текущей миграции)
- ✅ Metadata inspector возвращает `enumValues` для статусов/tier'ов
