# Движок проведения

> **TL;DR:** Документ предоставляет движения через source-интерфейсы; Posting Engine координирует блокировки, validators, recorders и сохранение статуса в общей транзакции.
> **Тип:** Concept
> **Аудитория:** Developer
> **Связанные:** [Транзакции](transactions.md), [Новый документ](../howto/new-document.md)

## 1. Visitor и recorder

`posting.Postable` определяет ID, тип, posted version, проверки и переходы состояния. `RegisterVisitor.CollectMovements` извлекает движения только у поддерживаемого source-интерфейса. Базовый `MovementSet` содержит settlement slice и `Extensions`; crypto visitors хранят в Extensions типизированные slices.

Действующие crypto sources: CryptoBalance, CryptoMerchantBalance, CryptoFee, CryptoPlatformCost, CryptoProviderBalance, CryptoReceiptSuspense. StockVisitor/StockMovementSource в текущем контенте отсутствуют. Новый регистр подключается через `AddVisitor` и `AddRecorder`.

## 2. Транзакционная граница

1. `CanPost` и before-post hooks выполняются перед локальным `RunInTransaction`; вызывающий service уже может держать внешнюю транзакцию. В before hooks нельзя помещать внешние side effects, которые невозможно откатить.
2. Engine получает `tx.Manager` из context, открывает/переиспользует транзакцию и берёт document advisory lock.
3. `PostingResourceLocker` recorders выполняется до evidence validators и reversal. CryptoBalanceRecorder через storage port блокирует объединение исходных и новых wallets; PostgreSQL adapter схемы 89 берёт все owner/network AML scopes по merchant/network/token, затем catalog wallets по ID. Source generators вызываются для планирования ресурсов без записи и должны быть чистыми. После этого `ValidateInTransaction` проверяет durable evidence.
4. При repost вызываются `ReverseMovements` всех recorders; visitors собирают новый набор.
5. `PrepareMovementsInTransaction` получает собранный `MovementSet` до проверки и записи recorders. CryptoPayment recognition выделяет physical line ID в общем slice, связывает его с registry и сохраняет snapshot/outbox в этой же транзакции. Ошибка откатывает также предшествующее сторно.
6. `PostingValidator.ValidateBeforePost` проверяет balance constraints. Crypto register repositories сортируют ключи и берут resource locks; crypto balance — wallet/token, merchant balance — merchant/token.
7. Recorders сохраняют движения; `MarkPosted` увеличивает posted version, callback repository сохраняет документ. Ошибка откатывает всю операцию.
8. After-post hooks регистрируются через `tx.OnCommit`, выполняются после внешнего коммита и логируют ошибки. Обязательный аудит/outbox должен сохраняться до коммита; after-post hook не обеспечивает его атомарность с ledger.

Default PostgreSQL TxManager использует **read committed**; repeatable read/serializable задаются явно соответствующим scenario. Вложенный вызов не может незаметно ослабить обязательную изоляцию. RPC, Vault signing, broadcast и доставка выполняются вне короткой DB-транзакции.

## 3. Неизменяемый crypto ledger

Финансовые crypto movements запрещают **UPDATE и DELETE**. Register service reversal вызывает `AppendReversalsByRecorder`: добавляет противоположные движения, сохраняя исходную историю. Repost затем добавляет новое поколение. SQL guards и triggers поддерживают этот контракт и агрегированные balances.

Legacy settlement register имеет отдельный `ReverseMovements` с удалением строк; его поведение не описывает crypto ledger. `UnpostGuard.CanUnpost` может вообще запретить reversal конкретного settled-документа. Коррекция внешнего факта тогда выполняется новым документом; blockchain не откатывается.

CryptoCustodyReceipt схемы 78 — системный неизменяемый документ, доступный только внутреннему maintenance intake. Он генерирует ровно один physical receipt и один suspense receipt с заранее выделенными стабильными line IDs. Recognition, audit и posted outbox сохраняет application service в той же транзакции. Deferred SQL constraints отклоняют неполный набор. Generic modify/repost/unpost запрещены. Suspense recorder при reversal постороннего документа ничего не меняет, а при наличии собственных receipt movements требует будущего explicit compensation workflow.

CryptoPayment не двигает custody: проведение распределяет его receipt (невыясненные −gross, мерчант +net, комиссия +fee по замороженному снимку тарифа). Следующее поколение сохраняет economics/fee и допускается только после полного сторно всех трёх регистров. Неполный unpost, relabel recorder и неверные reversal dimensions запрещены SQL.

CryptoSweep схемы 82: на broadcast не генерируют CryptoBalance movements. PostingEngine фиксирует snapshot ресурсных расходов; `posted` не означает final principal. Final settlement записывает по две exact principal lines на строку через [SweepCustodyRepo](../../internal/infrastructure/storage/postgres/crypto_repo/sweep_recognition.go), с одним shared registry owner, immutable binding, normalized fee/cost и outbox в общей транзакции. Source attempt/wallet завершаются только после этого. Generic reversal/repost и новые поколения запрещены SQL до explicit recovery workflow. При revert создаётся только фактический network fee, без principal и storno.

TreasuryWithdrawal схемы 83: не генерирует physical principal и platform revenue expense на broadcast. PostingEngine сохраняет состояние; wallet/revenue reservations остаются active. Final settlement выполняет exact expense set через [TreasuryCustodyRepo](../../internal/infrastructure/storage/postgres/crypto_repo/treasury_recognition.go) вместе с registry, fee/cost, binding/outbox и consumption резервов. Revert освобождает principal/revenue и учитывает actual fee. Generic repost/unpost/новый generation закрыты SQL.

CryptoWithdrawal схемы 84: на broadcast не создаёт physical principal или fee income, principal reserve остаётся active. WithdrawalRequest сохраняет единственный original debit-first расход. Final proof и [WithdrawalCustodyRepo](../../internal/infrastructure/storage/postgres/crypto_repo/withdrawal_recognition.go) обеспечивают principal/fee/reserve/outbox set и binding к merchant debit. Revert возвращает merchant amount без physical principal/storno; AML refund ограничен отказавшей строкой. Generic reversal/repost закрыт.

## 4. Batch operations

Generic handler поддерживает batch-action и batch-action-by-filter только при соответствующих capabilities. Выбранные ID ограничены 500, virtual selection — 100000. Каждый документ выполняется отдельно; ошибка одного не откатывает остальные. `batchConcurrency` читается из tenant settings и ограничивается размером pool. При `Accept: text/event-stream` прогресс передаётся через SSE; один writer отправляет события.

## 5. Debit-first и исходящие операции

`WithdrawalRequest` списывает merchant balance при создании, до approval/chain execution. Так параллельные заявки не расходуют одно обязательство дважды. Исполняющий `CryptoWithdrawal` не создаёт второго merchant debit. CryptoWithdrawal фиксирует physical principal только при final independent proof, actual network fee — через ChainSettlement; доход комиссии вывода не создаётся.

Отклонение/доказанный окончательный отказ компенсирует debit по workflow. Неопределённый chain outcome не означает безопасную отмену. AML withdrawals дополнительно хранят class-specific source lines, risk acceptance, partial settlement и cancellation facts; см. [AML outbound](../operations/crypto-aml-outbound.md).

ID документа выделяется Go-кодом до проведения; движения используют тот же recorder ID. Обход repository не разрешает генерировать другой UUID в INSERT.

Processing fee — выручка; `chain_resource_rent` и `chain_network_fee` — platform costs. Resource spend/JIT principal не превращаются в merchant debit или fee income. Типы и связь с settlement задаёт конкретный документ.

## 6. CryptoNetworkOperation и ручная учётная коррекция

`CryptoNetworkOperation` проводится только после financially final receipt и
реализует два источника движений:

| Регистр | Движение `wallet_activation` |
|---|---|
| `reg_crypto_balance_movements` | расход native token по `sourceWalletId` |
| `reg_crypto_platform_cost_movements` | расход `chain_network_fee`, `purpose=activation`, `provider=chain`, `external_order_id=txHash` |

При нулевой фактической комиссии документ проводится без движений. Reverted
транзакция тоже проводится на фактически списанную сетью комиссию.

Документ проводится один раз координатором активации в период финальности
вместе с признанной сетевой комиссией. Пользовательского изменения,
распроведения и перепроведения нет; `CanUnpost` всегда отклоняет команду.

---

## Файловая карта
```path
internal/domain/posting/engine.go   — Координатор транзакции проведения
internal/domain/posting/visitor.go  — Сбор движений из документов
internal/domain/posting/recorder.go — Запись движений в регистры
internal/domain/registers/cryptoposting/ — Visitor/Recorder криптовых регистров (конфигурация, подключается при сборке движка)
internal/infrastructure/http/v1/handlers/document.go — SSE Batch processing
internal/domain/registers/crypto_merchant_balance/service.go — CheckAndReserveMerchantBalance + StornoMovements
internal/domain/registers/crypto_balance/service.go — CheckAndReserveCryptoBalance
internal/domain/registers/crypto_platform_cost/service.go — Запись platform expense movements
internal/domain/documents/crypto_network_operation/model.go — источники движений активации
internal/domain/strict_document.go — защита ручной коррекции
internal/application/outboundapp/ — сценарии outbound, confirmation/replacement и ресурсов
internal/infrastructure/storage/postgres/outbound_repo/ — резервы hot wallet, claims и атомарный settlement
internal/infrastructure/crypto_worker/ — планирование и сборка зависимостей
```

## Связанные документы
- [transactions.md](transactions.md) — детализация механизмов блокировок (Resource Ordering)
- [crypto-processing.md](crypto-processing.md) — §3.5 WithdrawalRequest (debit-first use case)

## Custody loss (85)

CryptoCustodyAdjustment сохраняет maker/checker и PostingEngine visitors, но PrepareCustodyAdjustmentForPosting заранее закрепляет proven outgoing effect. Оба генератора возвращают стабильные отдельные IDs с одним admission period. SQL требует полный principal/loss-cost/audit/reconciliation/outbox set в одном transaction. Без независимого proof, с managed recipient, ожидаемой signed execution либо prior owner новое проведение закрыто. Generic reversal principal и loss cost запрещён.

## Network fee recognition схемы 86

При независимом fee recognition CryptoNetworkOperation использует stable expense/cost IDs, общий FinalizedAt и reservation reference. FeeBudget сохраняет proof/binding/normalized components/outbox в transaction координатора; PostingEngine записывает единственную пару native expense/platform cost. При нуле денежных строк нет. Generic unpost и reversal признанной комиссии закрыты до отдельной compensation-команды. Legacy документы без fee reservation reference сохраняют прежние generators и workflow.
