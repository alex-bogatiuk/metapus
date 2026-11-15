# Модуль 7. Движок проведения

> Учебный маршрут по действующему crypto ledger. [Полный контракт Posting Engine](../systems/posting-engine.md).

## 1. Аналогия с 1С

| 1С | Metapus | Исходник |
|---|---|---|
| ДокументОбъект | `posting.Postable` | `internal/domain/posting/engine.go` |
| ОбработкаПроведения | Метод source-интерфейса, например `GenerateCryptoBalanceMovements` | `internal/domain/registers/cryptoposting/balance.go` |
| НаборЗаписей | `MovementSet`; crypto slices в Extensions | `internal/domain/posting/engine.go` |
| Запись регистра | `RegisterRecorder` | `internal/domain/posting/recorder.go` |
| Проверка остатков | `PostingValidator.ValidateBeforePost` | `internal/domain/registers/cryptoposting/balance.go` |

StockVisitor, GoodsIssue и товарных регистров в текущем контенте нет. Для практики используйте CryptoPayment, CryptoSweep и CryptoNetworkOperation.

## 2. Visitor

`CryptoBalanceVisitor` проверяет, реализует ли документ `CryptoBalanceMovementSource`. Если нет — пропускает его. Если да — вызывает генерацию движений и помещает типизированный срез в `MovementSet.Extensions`. Соответствующий recorder извлекает этот срез и передаёт register service.

Так engine не зависит от структуры каждого документа. Новый регистр требует visitor и recorder, подключённых через `AddVisitor`/`AddRecorder`; сам документ реализует только нужные source contracts.

## 3. Транзакция

В `Engine.doPost`:

1. `CanPost` и before-post hooks проверяют допустимость до локального transaction boundary. Внешний service уже может держать транзакцию.
2. Из context получается `tx.Manager`; внутри `RunInTransaction` берётся document lock.
3. `ValidateInTransaction` проверяет durable evidence перед reversal/записью.
4. При перепроведении вызываются recorders reversal; затем visitors собирают новый набор.
5. `PostingValidator` проверяет остатки под детерминированными блокировками.
6. Recorders записывают движения; `MarkPosted` и callback repository сохраняют состояние документа в той же транзакции.
7. After-post hooks регистрируются в `tx.OnCommit` и выполняются после **внешнего** коммита. Это не автоматические асинхронные задачи; durable внешняя доставка использует outbox.

Ошибка до коммита откатывает движения и статус вместе. RPC, подпись и отправка выполняются отдельным durable workflow вне DB-транзакции.

## 4. Отмена без потери истории

Crypto register services вызывают `AppendReversalsByRecorder`: добавляются противоположные движения, оригинал сохраняется. UPDATE/DELETE финансовых crypto movements запрещены SQL guards. Ledger не пересоздаётся удалением старых строк.

Unpost разрешён только lifecycle конкретного документа. Settled-факт может запрещать его через `CanUnpost`; финансовая коррекция тогда создаёт новый compensating document. Legacy settlement register имеет другой repository-контракт, который нельзя переносить на crypto.

`WithdrawalRequest` использует debit-first: обязательство списывается при создании. Исполнительный CryptoWithdrawal не списывает merchant balance второй раз. Отклонение или окончательный отказ компенсируют исходное списание согласно workflow.

## 5. Практика

Откройте `internal/domain/documents/crypto_payment/model.go`, source interfaces и `Engine.doPost`. Затем проследите `crypto_balance.Service.ReverseMovements` до PostgreSQL `AppendReversalsByRecorder`.

Проверьте связь document ID и recorder ID, posted version, детерминированность сумм, атомарный rollback и отсутствие повторного merchant debit. Chain finality и merchant credit — отдельные факты: invoice `confirmed` не заменяет `completed`.

[Модуль 8: продвинутая многозадачность](08-advanced-concurrency.md)
