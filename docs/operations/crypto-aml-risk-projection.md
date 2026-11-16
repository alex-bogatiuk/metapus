# AML: verified intake и проекция риска

Путь обработки: verified incoming → immutable effect → pinned AML job/report → decision → wallet/token risk projection → `wallet.risk_changed`. Контур включается явно для merchant/network/token. Владельцы существующих кошельков требуют подтверждённой привязки.

## Входящие средства и финансовый учёт

`BlockIngestionRepo.SaveVerification` сохраняет proof и наблюдение AML в одной SQL-транзакции. Владелец определяется по immutable custody owner получателя. Учитываются также hot/retired-адреса и поступления без подходящего invoice. Неизвестный owner не угадывается по текущему lease. Backlog подбирает последние canonical proofs, сохранённые до включения scope.

Effect привязан к network, wallet, token, tx, event kind/index, block hash, economics hash и сумме. SQL guards сверяют его с сохранённым canonical proof. Повтор того же доказательства не создаёт другую job; откат внешней транзакции откатывает proof, effect и job вместе.

AML не проводит `CryptoPayment`, не зачисляет merchant net и не исправляет финансовый ledger. Unmatched observation сама по себе не зачисляет деньги. Классификация и rescreen не создают второго receipt и не публикуют повторный `payment.confirmed`. Финальное финансовое признание выполняет [receipt intake](../crypto/crypto-inbound-provider-accounting-design.md).

## Что позволяет получить low/medium

Одного низкого score недостаточно. Для проекции нужны:

- Принятые mapping и risk policy текущего routing scope; свежий report с допустимыми signals, точной привязкой к subject и текущему поколению проверки.
- Однозначное chain coverage. Число подходящих effects считается по полному согласованному блоку до фильтрации наблюдаемых адресов. Несколько переводов одного актива на адрес в одной tx не становятся одним доказанным per-effect report.
- Явно зарегистрированный пустой checkpoint wallet/token и полное покрытие последующего физического inventory.

`RegisterAMLWalletBaseline` — команда доверенного control plane. Она принимает ссылку на свежую reconciliation check с chain=ledger=delta=0, block height/hash, evidence reference и проверившего оператора. Проверяются текущий нулевой ledger и отсутствие principal/fee reservations. Ссылка на checkpoint является доверенным доказательством оператора; текущая reconciliation check сама не подтверждает указанный block hash. Checkpoint и использованная check защищены от изменения. Автоматического объявления legacy balance чистым нет; UI/API регистрации ещё не реализованы.

Покрытие сверяется по идентичности каждого receipt после checkpoint и текущему физическому ledger. Для `CryptoPayment` нужны exact payment/proof/effect; для `CryptoSweep` — все проводимые lines и внутренние effects; для `CryptoNativeFunding` — settled basis и внутренний effect. Совпадение общей суммы проверенных переводов с несвязанными старыми или ручными receipt не даёт покрытия. Отсутствующий checkpoint, расхождение ledger и неразобранный receipt оставляют `unknown`.

TRON contract logs имеют счётчик coverage; native transfers требуют decoder V2 с полной поддерживаемой структурой блока. Legacy proof с неизвестным числом effects финансово остаётся валидным, но не даёт AML-разрешения. Internal trace без принятого coverage и EVM без необходимого verified proof не объявляются low. Если поздний proof обнаружил неоднозначность, coverage закрывается немедленно и автоматически обратно не повышается; отдельный reviewed resolution ещё предстоит реализовать.

## Пересмотр и распространение риска

Проекция относится ко всему fungible inventory wallet/token. Классы не усредняются. Missing/expired report и pending generation дают `unknown`; известный `high` сохраняет ограничение даже при истёкшем report или неполном inventory. Пустой кошелёк без оценённых contributions не становится low автоматически.

Изменение chain proof, финансовых movements или прямой ремонт физической balance projection закрывают прежнее покрытие в той же транзакции. Reorg помечает AML effect orphaned и ставит пересчёт; финансовые компенсации выполняет существующий chain accounting. Для low/medium в очереди сохраняется срок ближайшего истечения оценки. Worker обновляет состояние по очереди; при будущем расходовании всё равно потребуется проверять `expires_at` в момент dispatch, поскольку выполнение worker может задерживаться.

Контролируемый внутренний перевод определяется exact signed expected effect и тем же custody owner. Он наследует риск исходного inventory без новой платной проверки. Зависимости проходят также через сохранённые signed sweep authorizations: поздний high источника ограничивает оставшиеся средства его downstream collectors. Граница merchant/network/token не пересекается. Историческая подпись рассматривается консервативно даже после неуспешного attempt; снятие такой зависимости и ротация batch требуют будущего reviewed workflow.

`RequestAMLRescreen` — явная команда с effect ID, ожидаемым поколением, причиной и оператором. Она атомарно повышает generation, создаёт новую pinned job и закрывает старый low. Это новая намеренная платная проверка в пределах admission budget; retry старой job её не заменяет. Запоздавший low старого поколения не разрешает актуальное pending. Любой новый сохранённый report закрывает старый low до оценки; валидный поздний high сохраняет ограничение и после более нового low. Автоматического снятия исторического high нет. API/UI rescreen и reviewed resolution пока отсутствуют.

Пороги и signals policy версионируются неизменяемо. Смена policy требует остановленной сети, немедленно инвалидирует scope и ставит уже сохранённые reports в очередь локальной переоценки без нового paid submit. Смена mapping также инвалидирует scope; report, закреплённый за старой mapping, не получает новое разрешение. Перенос subjects на другую mapping требует отдельного workflow. Семантическое изменение token identity при включённом scope запрещено.

## Risk events

`reg_crypto_aml_risk_changes` хранит immutable revisions wallet/token. Отдельный publisher атомарно создаёт merchant event и webhook outbox с causation key `aml-risk:<change-id>`. Повторный запуск publisher не дублирует событие. Это событие о версии проекции: оно может отражать закрытие покрытия или изменение evidence без смены текстового класса.

Payload `wallet.risk_changed` содержит `walletId`, `tokenId`, `networkId`, `previousRiskClass`, `riskClass`, `riskRevision`, `scopeVersion`, `reason`, `inventoryCovered`, `expiresAt`. Подписка выбирается в существующих настройках webhook endpoints; ранее выбранные события не расширяются автоматически. Доставка использует обычный outbox и может повторяться, поэтому consumer использует event ID/revision для идемпотентности и порядка.

## Граница текущей реализации

Эта проекция описывает физический inventory. Поверх неё подключён [Portal net snapshot баланса по риску](crypto-aml-risk-balances.md): он использует [durable payment/token-sweep allocations](crypto-aml-inventory-allocations.md) и учитывает debit-first commitments ровно один раз. Неподтверждённая attribution удерживается технически. Native operating capital и final fee/funding подключены через [resource journal](crypto-aml-resources.md). Activation и withdrawal allocations/точные компенсации lines, high/unknown export, автоматический baseline и permits подключены в [новом outbound контуре](crypto-aml-outbound.md). General reorg corrections и reviewed high resolution остаются отдельными этапами. [Batch planning/capacity](crypto-aml-batches.md) уже подключены.

Для включённого scope новый classless withdrawal request отклоняется SQL guard. Все новые отправки этого актива через прежний signing/dispatch контракт закрыты; низкий score сам по себе их не разрешает. Подбор новых legacy sweeps исключает такой актив и не выкачивает native gas из origin с заблокированным AML token. Исторические signed/broadcast операции остаются доступны для recovery; новая подпись/повторная отправка не получают обхода gate.

Новый outbound контракт проверен в изолированной среде; публичная testnet/mainnet приёмка остаётся отдельным этапом. Рабочие БД не обновлялись, реальных AML-вызовов и chain broadcasts при разработке нет. При данных этого контура rollback миграции запрещён; используется forward migration.

## Проверка

`aml_routing_integration_test.go` использует отдельную временную PostgreSQL БД на сценарий. Проверены replay/rollback, baseline и неподкреплённые receipt, ложное vendor coverage, chain coverage regression, TTL, reorg, поколения rescreen, поздний high и изоляция другого мерчанта, смена policy без платной проверки, несовместимость mapping, прямой ремонт ledger, immutable evidence и risk outbox. Vendor fixtures синтетические; Mainnet acceptance ими не подтверждается.
