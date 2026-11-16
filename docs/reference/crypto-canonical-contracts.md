# Канонические контракты криптопроцессинга

> **TL;DR:** В каждом chain family работает один декодер; версия evidence, подписываемые bytes и бизнес-политика имеют независимые контракты.
> **Тип:** Reference
> **Аудитория:** Developer, AI-Agent
> **Связанные:** [JSON-контракты](json-contracts.md), [TRON ingestion](../operations/tron-block-ingestion.md), [Merchant custody](../operations/crypto-merchant-custody.md)

## 1. Алгоритмы и сохраняемые форматы

Accounting identity использует отдельную schema accounting-effect.v1. Она не меняет исторические transfer/economics/signing bytes. Новые успешные доказательства TRON/EVM содержат optional accountingIdentity; исторические proofs остаются читаемыми без него.

Logical key состоит из network, family, canonical transaction hash, effect kind и transaction-local ordinal. Для внутренних TRON effects используется также полный trace hash. EVM block-global log.Index остаётся observation alias и не входит в logical key. Block hash и нормализованные economics образуют отдельную occurrence.

`reg_crypto_accounting_effects`, `reg_crypto_accounting_effect_occurrences` и `reg_crypto_accounting_effect_evidence` индексируют сохранённые proofs. Активную каноничность определяет семейство сети. Финансовое признание хранится отдельно в `reg_crypto_custody_effect_postings` и `reg_crypto_custody_effect_recognitions`. У одного эффекта один владелец признания и не более одной principal receipt и одной expense стороны, даже при catalog aliases. Смена block hash сама не создаёт второй principal.

`CryptoCustodyReceipt` и `CryptoWalletFunding` требуют persisted proof, audit и outbox. `CryptoPayment` распределяет receipt. Каждое новое поколение распределения требует полного сторно прежнего набора suspense/net/fee с сохранением economic/fee snapshot. Для EVM funding используется независимый verifier без signer/nonce dependencies. Display casing адреса EVM не меняет сохранённые proof bytes. [Входящий учёт](../crypto/crypto-inbound-provider-accounting-design.md)

Final native funding получает доказательство до финансовой транзакции и проводит две controlled стороны. Paused ingestion и kill switch блокируют новые отправки и сохраняют восстановление принятого перевода. Revert учитывает только доказанную комиссию.

Sweep признаёт principal каждой строки при final successful settlement. `VerifyExpectedTransfer` находит transfer по signed economics и возвращает observation index и transaction-local identity. Неоднозначные совпадения отклоняются. `VerifyTransfer` проверяет заданный index. Treasury применяет тот же strict proof для expense controlled source. Внешний recipient берётся из frozen approved document. Fee и principal учитываются по разным доказательствам. Дневной network fee budget учитывает только fee, JIT funding budget — principal и fee.

| Контракт | Текущее представление | Инвариант |
|---|---|---|
| TRON block decoding | Единственный `assembleBlock` с нормализацией receipts | RPC configuration не выбирает реализацию декодера |
| TRON transfer evidence | `TRONTransferEvidenceSchema = 2` | Число 2 и policy string `tron-transfer-v2` входят в существующий hash contract |
| EVM transfer evidence | `TransferEvidenceSchemaV1 = 1` | Схема 1 остаётся действующим EVM-форматом |
| Исторические events | Отсутствующий `decoderVersion` интерпретируется как схема 1 | Чтение старых bytes не повышает полноту evidence |
| Подписываемый unsigned fingerprint | Schema 1, `CanonicalizeLegacyV1` | Семейство, версия и точные исторические bytes связаны хешем |
| iTRX HMAC | `MarshalLegacyV1NoHTMLEscape` | Vendor body codec изолирован от product JSON |
| AML signing readiness | `aml_signing.v1` и `money_movement_intent.v2` | Совпадение поддерживаемых схем проверяется независимо от custody readiness |

Новый TRON capture всегда задаёт схему 2 явно. Значения 0, 1 и неизвестные значения отклоняются до сохранения событий. Fresh DDL фиксирует `decoder_version = 2`. Verifier, watcher, funding CLI и bootstrap проверяют тот же контракт.

Идентификаторы сохраняемых схем не перенумеровываются при переименовании Go-символов. Для нового byte-протокола требуется отдельная версия, спецификация и общие golden vectors производителей/потребителей.

## 2. Полнота AML coverage

`AMLCoverageIndex` получает все эффекты полного согласованного блока до фильтрации адресов и активов. `TransferCoverage` разделяет наблюдаемое количество и доказанность его полноты:

| Статус | Значение |
|---|---|
| `CoverageUnknown` | Полнота не доказана; в том числе неизвестная схема |
| `CoverageIncomplete` | У источника присутствуют исключённые traces |
| `CoverageComplete` | Для соответствующего вида эффектов доказан полный набор |

`ObservedEffects()` возвращает число наблюдаемых эффектов. `MatchingEffects()` возвращает число только при `CoverageComplete`, иначе 0. Таким образом сохраняется существующий wire-контракт proofs и AML requests. Отсутствие совпадений в полном блоке отличается от неизвестной полноты в доменной модели. Исключённые native traces не лишают полноты отдельно декодированные contract logs.

EVM verifier доказывает собственные условия полноты прямого native transfer и logs на согласованном final head; номер схемы сам по себе не является доказательством EVM coverage.

## 3. Авторизация и восстановление

AML scope является явным состоянием политики. Включённый scope требует точного зарегистрированного source plan. SQL error, отсутствие плана и несовпадение плана не превращаются в разрешение. Подпись и отправка дополнительно требуют durable dispatch permit.

`money_movement_intent.v1` используется действующим classless withdrawal при выключенном AML scope и treasury. `money_movement_intent.v2` связывает merchant withdrawal с risk class, explicit high/unknown acceptance и точными source lines. Производители указывают схему явно. Оба формата сохраняют исходные правила авторизации; treasury не получает искусственную AML-классификацию.

Допуск новой операции и наблюдение сохранённой попытки имеют разные обязанности. Сохранённые signed bytes, tx hash, reservations и authorizations используются при восстановлении без пересоздания идентичности. Неизвестный исход сохраняет резерв. Финансовые движения защищены от повторного применения; окончательное подтверждение обрабатывается независимо от текущей доступности нового funding source.

Domain проверяет значения и переходы. PostgreSQL обеспечивает конкурентную целостность и immutable evidence. Vault независимо проверяет подписанные полномочия и custody registry. Эти границы доверия сохраняют собственные проверки.

## 4. Источники native funding

`FeeFundingPolicy` хранит режим, лимиты и минимальные остатки. Глобального `feeWalletId` в HTTP/DTO, frontend и `sys_crypto_network_policies` нет.

Источник определяется по сети, неизменяемому custody owner и роли `network_fee`. Для merchant owner допустим только его fee wallet. Для платформенного кошелька при выключенном enforcement используется зарегистрированный платформенный fee wallet. При включённом enforcement непривязанный destination отклоняется. Ошибка точного поиска не запускает поиск другого owner.

Readiness проверяет native fee capital для каждого активного владельца. При резервировании минимальный остаток выбирается по роли заблокированного payer: `network_fee` использует `MinFeeWalletBalance`, остальные источники — `MinSourceWalletResidual`. Проверка применяется также при увеличении fee reservation.

## 5. Схема и операторские переходы

Правила изменения схемы описаны в [инструкции по миграциям](../../db/migrations/README.md). Для запуска нужна версия из [ExpectedSchemaVersion](../../internal/core/version/version.go). Disposable стенд создаётся штатным provisioning. Сохранение данных при переносе требует backup/restore и отдельной проверки.

`crypto-preflight` поддерживает read-only режимы `aml`, `balance`, `ingestion`, `rpc`, `evidence`. Shadow pause/resume, fencing leases, canonical header validation, financial adjustments и settlement используют сохранённые evidence. Proof bytes и audit records неизменяемы. Multi-payment policy сохраняется при включении single-payment для остальных инвойсов.

## 6. Проверки

Golden-тест `TestCanonicalTransferEvidenceGolden` фиксирует source/canonical digest и economics hashes native/internal transfers до изменения реализации. Отдельные golden vectors unsigned fingerprint и iTRX защищают их byte-контракты.

Unit tests покрывают явные coverage states, точный custody owner, отсутствие обходного источника, остатки payer и recovery при выключенной текущей политике. PostgreSQL integration suite проверяет fresh schema, `Up → Down → Up`, capture fencing, immutable evidence, частичное сторно, fee reservations, dispatch и settlement с race detector. Оба Vault plugin modules проверяются отдельно. HTTP declarations и Zod schemas генерируются из Go DTO.

## Merchant withdrawal recognition

Withdrawal использует тот же independent outgoing locator и строгий expense AccountingTransferBasis, что treasury. Proof получается до финансового transaction. Original WithdrawalRequest merchant debit не повторяется. Final principal owner, binding к исходному debit и authorization, actual fee/cost, normalized receipt, principal reserve outcome, AML fact и outbox атомарны. Failed v2 AML fact не имеет physical expense/refund; он подтверждает только точный merchant refund и освобождение резерва. Expected signed effect защищён от других legacy outflow writers. Изменение decoder/signing identity и новое accounting generation не добавлены.

## Custody loss recognition

Approval CryptoCustodyAdjustment использует strict expense AccountingTransferBasis по observed outflow: полный network/event/index/block/source/destination/asset/amount basis, независимый indexed canonical proof и общий owner. Матч signed expected effect требует исходного execution workflow. Detection/classification не заменяют proof. Loss cost отделён от network fee; merchant liability не меняется. Новые decoder/signing schemas не добавлены.

## Network fee proof и recognition

Fee role имеет независимый `network-fee.v1` key по network/family/canonical tx hash. NetworkFeeProof подтверждает controlled payer, canonical final outcome и точный total/components независимо от principal и transfer ordinal. TRON и Ethereum mainnet/Sepolia execution/blob используют два независимых источника/common head; неизвестный fee profile отклоняется. Fee reservations требуют proof/binding/exact financial set. Binding generation 1 не открывает автоматическую reorg-компенсацию.

## Expected-transfer routing

Expected matching использует полный economic tuple и сохранённый signed intent; tx hash и controlled source сами по себе недостаточны. Logical route требует independent indexed proof/current confirmations/MatchingEffects=1 и одного expected transfer на authorization. Chain-specific ordinal берётся из accounting identity, без переписывания legacy event index. Pending request/outbox не создаёт financial owner или signing permission; original settlement basis сохраняется после lease expiry и получает nudge через прежний poller. Legacy/ambiguous/unsupported intents требуют review, без external fallback.


## Manual correction basis

CryptoAdjustment фиксирует `basisKind` при proposal. `payment_attribution` (режим `payment_reversal`) сторнирует выбранные строки распределения текущего поколения платежа обратно в невыясненные receipt; custody не меняется, выбранные строки заморожены. `wallet_variance` (режим `wallet_balance`) не имеет chain recognition, даже если пользователь ввёл hash/proof ID в evidence. Корректировка не приобретает logical effect и не создаёт нового principal posting. DB проверяет точные противоположные движения и accounting outbox.
