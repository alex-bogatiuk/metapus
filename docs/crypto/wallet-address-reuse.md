# Повторное использование адресов и поздние платежи

> **TL;DR:** Transient lease сохраняется в истории; cooldown ограничивает повторную выдачу, а blockchain timestamp определяет инвойс. Watcher lag блокирует lease, сохраняя outbound recovery.
> **Тип:** Concept
> **Аудитория:** Developer, Finance, Security
> **Статус:** Реализовано; сверено с кодом 2026-11-02.
> **Связанные:** [Wallet pool](../operations/crypto-wallet-pool.md), [Lifecycle и webhooks](../reference/merchant-public-api.md), [AML и ротация](../operations/crypto-deposit-address-rotation.md)

## Модель

`cat_wallets.cooldown_until` — временное ограничение выдачи, а не отдельный occupancy-статус. `free`, `leased`, `assigned`, `sweep_pending`, `sweeping`, `frozen` сохраняют своё значение.

`reg_crypto_wallet_allocations` хранит одну аллокацию transient-адреса на invoice: wallet, invoice, merchant, token, network, время lease, закрытия, cooldown и причину. `WalletAllocationRepo.Open` вызывается при выдаче адреса. `CloseTransientLease` под wallet lock закрывает текущую аллокацию, очищает lease и переводит кошелёк в следующее occupancy-состояние в той же транзакции вызывающего сценария.

Допустимые close reasons в домене: `expired`, `cancelled`, `confirmed`, `orphaned_lease`. Причина `confirmed` относится к завершению lease после подтверждения платежа; статус инвойса имеет отдельный lifecycle (`confirmed` и `completed` различаются).

Cooldown читается из network wallet-pool policy. Фактические параметры берите из сохранённой политики: значения раннего проекта 1800/900 или 1200/600 не являются универсальным runtime default.

## Атрибуция по истории

`WalletAllocation.Covers` и SQL `FindCovering` используют одинаковые границы:

- Открытая аллокация: `[leased_at, +∞)`.
- Закрытая: `[leased_at, cooldown_until]`, обе границы включены.
- Timestamp до `leased_at` не получает скрытого допуска на рассинхронизацию часов.

`DecideMatch` выбирает среди покрывающих аллокаций по token:

| Условие | Результат |
|---|---|
| Одна покрывающая аллокация нужного token | matched |
| Несколько покрывающих аллокаций нужного token | needs_review / overlapping_windows |
| Окно есть только для другого token | unmatched / wrong_token |
| Timestamp раньше открытого lease | needs_review / timestamp_before_lease |
| Подходящего окна нет | unmatched / no_allocation |

Нельзя выбирать владельца платежа только по текущему `leased_for_id`. Cooldown уменьшает риск повторной выдачи, но не заменяет историю и chain evidence.

## Durable inbox и late payment

Входящие transfer-события сохраняются в `reg_crypto_inbound_chain_events` с network/transaction/event identity до бизнес-сопоставления. Повтор не должен создавать второй платёж. Inbox хранит match status, reason, invoice/payment/allocation references и retry state.

Текущие inbox states: `received`, `matched`, `unmatched`, `needs_review`, `resolved`, `ignored`, `poison`. Ошибки токена, dust и неоднозначные окна сохраняются явно. `ChainEventInbox` предоставляет отдельные received retry и unmatched replay операции; обычный повтор watcher event не разрешает произвольно перепривязать уже разобранный платёж.

`InvoiceIsLateTerminal` считает поздними платежи к `expired`, `cancelled`, `underpaid`. Такие поступления не открывают инвойс заново. После финальности/проводки late path публикует `invoice.late_payment`; выполняемый заказ всё равно требует проверки canonical `completed` по Merchant API, а не одного факта прихода денег. Single-payment review и retired persistent addresses имеют дополнительные gates; см. связанные руководства.

## Выдача и watcher lag

Transient lease и выдача persistent address используют ready/admission checks: active wallet, разрешённая роль/owner, отсутствие cooldown, holds и reconciliation incidents. Проверки network policy выполняются под policy lock, выбор свободного wallet — через `FOR UPDATE SKIP LOCKED`.

`lease_blocked_reason='watcher_lag'` и `lease_blocked_at` находятся в `sys_crypto_wallet_pool_policies`. Они блокируют выдачу адресов и refill, но не создают `reg_crypto_network_holds` и не останавливают outbound. Physical ready inventory и admission block отображаются отдельно.

`WatcherCheckpointLag` берёт максимум возраста checkpoint update и chain timestamp с учётом объявленных driver единиц и indexing lag. `WatcherStreamDescriptor` задаёт ожидаемые streams: отсутствие checkpoint не означает готовность. Причина lag снимается после восстановления требуемых streams.

Persistent `assigned` адреса не получают transient lease history как новую occupancy-модель. Ротация и retired intake используют отдельный неизменяемый журнал и сохраняют прежнего владельца.

## Проверка и источники

Unit-тесты покрывают границы окна, token mismatch, overlap и lag. PostgreSQL integration tests проверяют allocation/inbox, lease predicate, retry, lifecycle и восстановление; внешний Nile/Sepolia acceptance проводится отдельно.

```path
internal/domain/crypto/wallet_allocation.go
internal/domain/crypto/wallet_allocation_test.go
internal/domain/crypto/watcher_lag.go
internal/domain/crypto/watcher_lag_test.go
internal/domain/crypto/event_processor.go
internal/infrastructure/storage/postgres/crypto_repo/wallet_allocation.go
internal/infrastructure/storage/postgres/crypto_repo/chain_event_inbox.go
internal/infrastructure/storage/postgres/catalog_repo/wallet.go
internal/infrastructure/crypto_worker/lease_block.go
```
