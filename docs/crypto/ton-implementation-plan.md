# План реализации поддержки TON

Дата: 2026-10-08. Статус: проект реализации по результатам чтения текущего кода и первичной документации TON. Код адаптера, миграции и сетевые операции в рамках составления плана не выполнялись. Все перечисленные приёмочные сценарии имеют статус `NOT_RUN` до появления проверяемых результатов.

## 1. Результат и границы первой версии

Цель — включить TON в существующий процессинг: выдача адреса инвойсу → обнаружение и финализация платежа → проводки → sweep → выплаты и treasury → сверка и восстановление после сбоев.

Рекомендуемый объём первой версии:

- TON testnet, затем отдельное включение mainnet; разные `network_id`, ключевые пространства, настройки и подтверждённые профили сети.
- Управляемые кошельки одного фиксированного профиля Wallet V5R1, workchain 0. Одна исходящая transfer-action на подписанное внешнее сообщение.
- Приём нативной монеты и явно разрешённых Jetton; сумма всегда в целых minor units, decimals берутся из каталога токенов.
- Существующая адресная модель инвойсов, pool/hot/treasury, политики аренды и повторной выдачи адресов. Небольшой заранее развёрнутый пул с пополнением по спросу.
- Native/Jetton sweep, withdrawal, treasury withdrawal, служебное пополнение нативной монетой, учёт комиссий и excess/refund.
- Подпись Ed25519 только в отдельном Vault-плагине; application получает публичные данные и подписанный envelope.

Highload wallets, batching, gasless, произвольные вызовы контрактов, NFT, shared-address + обязательный comment и TON Connect — отдельные последующие задачи. TON Connect может улучшить checkout, но не является условием серверного процессинга.

Решение о V5R1 — рекомендация для реализации, а не существующая возможность системы. Версию SDK, wallet code hash, derivation scheme, wallet ID и правила send mode нужно зафиксировать до создания первого постоянного адреса.

## 2. Что уже есть и какие пробелы подтверждены кодом

| Участок | Текущее состояние | Необходимая работа |
| --- | --- | --- |
| [Домен сетей и драйверов](../../internal/domain/crypto/chain.go) | Семейство `ton` уже объявлено; есть builder, watcher, finality, balance, wallet и lifecycle-интерфейсы | Реализовать драйвер и расширить описание доказанного результата операции |
| [Capabilities](../../internal/domain/crypto/capabilities.go) | Есть account sequence и derived-per-asset token accounts; provisioning описывает derivation/activation | Выделить deployment кошелька, добавить исполняемые возможности TON |
| [Composition root](../../internal/infrastructure/blockchain/bootstrap/builder.go) | Связаны только TRON/EVM | Зарегистрировать TON factory, capabilities, preflight и startup reconciliation |
| [Chain config](../../internal/infrastructure/blockchain/chainconfig/config.go) | Настройки TRON/EVM, HTTP(S) endpoints, provider ID и trust group | Добавить TON settings и проверку идентичности сети; ADNL требует отдельного транспорта |
| [Custody registry](../../internal/infrastructure/crypto/custody/registry.go) | Подключены TRON/EVM signers и derivers | Добавить TON signer/deriver и Vault-плагин |
| [Signing authorization](../../internal/domain/crypto/signing_authorization.go) | Есть привязка к intent, sequence, сумме, адресу, fee limit и fingerprint | Определить подписываемые ограничения TON и совместимость схем авторизации |
| [Адресная политика](../../internal/infrastructure/storage/postgres/crypto_repo/chain_address_policy.go) | Поддерживает TRON/EVM; из каталога читает только family | Добавить canonical raw address, flags и проверку выбранной сети |
| [Каталог токенов](../../internal/domain/catalogs/token/model.go) | `Jetton` уже предусмотрен | Определить master address как контракт актива и хранить owner → Jetton wallet mapping |
| [Wallet pool](../../internal/infrastructure/storage/postgres/crypto_repo/wallet_pool.go) | Путь строится как namespace/index; отдельного TON contract profile нет | Версионировать derivation descriptor и сохранить неизменяемые параметры контракта |
| [Activation worker](../../internal/infrastructure/crypto_worker/account_activation.go) | Activation строится от hot wallet к новому адресу | Ввести workflow funding → self-signed deployment → проверка readiness |
| [Входящие события](../../internal/domain/crypto/blockchain_event.go) | Виды native value / contract log / internal value; один block number/hash | Описать TON effects, account transaction и masterchain/shard evidence |
| [Processor](../../internal/infrastructure/crypto_worker/processor.go) и [canonicality audit](../../internal/infrastructure/crypto_worker/canonicality_audit.go) | Есть generic orchestration, но источник `ExpectedTransfer` связан с текущим block ingestion repo | Вынести family-neutral evidence repository; TON не подключать к TRON-схеме формально |
| [Outbound confirmation](../../internal/infrastructure/crypto_worker/outbound_confirmation.go), [settlement](../../internal/infrastructure/storage/postgres/outbound_repo/outbound_settlement.go) | Поиск по одному `TxHash`; `ReceiptReverted` приводит к компенсации основной суммы | Разделить принятие запроса кошельком, доставку актива и окончательный денежный результат |
| [Broadcast safety](../../internal/infrastructure/crypto_worker/broadcast_safety.go) | Возвращённый hash сравнивается с signed hash через `EqualFold` | Ввести тип hash/reference, сравнивать декодированные байты |
| [JIT funding](../../internal/infrastructure/crypto_worker/jit_funding.go) | Required balance рассчитывается из max fee и остатка | Учесть отдельно прикладываемую стоимость сообщений, deployment и forward value |
| [Vault image](../../deployments/vault-test/Dockerfile) | Собирает два плагина | Добавить сборку, установку, policies и readiness TON-плагина |

`TokenAccounts()` уже объявлен и проверяется при регистрации драйвера, но рабочего orchestration для него сейчас нет. Одной реализации этого метода для Jetton будет недостаточно.

Финансовый подход: API-команда сохраняет прежнюю idempotency identity; chain effects имеют отдельную идемпотентность; ledger использует существующие проводки и компенсации; ключи остаются в Vault; webhook создаётся атомарно с финансовым переходом через существующий outbox. Это соответствует принципам Stripe для повторов API, Uber/RBKmoney для money movement, Google для custody и PayPal для удержания неоднозначных операций.

## 3. Архитектурные решения и обязательные инварианты

### 3.1. Операция, сообщение и транзакция — разные сущности

TON-операция образует причинно связанную последовательность сообщений и account transactions. Поэтому receipt исходного кошелька не описывает весь результат выплаты. Основание: [TON traces](https://docs.ton.org/foundations/traces).

Добавить family-neutral `ExecutionRef` и evidence для результата перевода. Для TON хранить отдельно:

1. Внутренний operation/attempt/generation ID и unsigned fingerprint.
2. Digest сохранённого signed BoC — контроль неизменности артефакта.
3. Normalized external message hash — поиск отправленного запроса.
4. Account address + account transaction hash + `lt` — конкретное исполнение.
5. Internal message hashes и связанные recipient transactions — доказательство движения актива.
6. Provider trace ID как вспомогательную ссылку, без использования в качестве бизнес-идентичности.

Normalized hash по TEP-467 не включает `StateInit`. Он пригоден для tracking, но не заменяет fingerprint полного разрешённого payload. Уникальность ограничивать сетью и профилем кошелька; запретить удаление и повторное развёртывание управляемого кошелька. Основание: [TEP-467](https://github.com/ton-blockchain/TEPs/blob/master/text/0467-normalized-message-hash.md).

Совместимость: текущий `TxHash` продолжает обозначать transaction hash для TRON/EVM. Для TON использовать новый reference с явным типом; не записывать message hash под старым названием ради прохождения проверок. Можно добавить отдельную capability tracking, сохранив `FinalityReader` для прежних драйверов.

### 3.2. Три независимых результата

| Измерение | Предлагаемые состояния | Что разрешает |
| --- | --- | --- |
| Запрос к кошельку | prepared → signed → submitted/unknown → accepted либо expired-unaccepted/held | Управляет seqno и безопасностью повторной отправки |
| Перевод актива | pending → delivered либо failed-proven/refund-pending/held | Управляет выплатой, sweep и обязательством по основной сумме |
| Денежный расчёт | reserved → fees-observed → settled; excess/refund учитываются по мере доказательства | Управляет расходами, резервами и возвратами нативной монеты |

Названия состояний окончательно согласовать с существующей FSM. Важно сохранить смысл разделения, а не создавать вторую независимую финансовую FSM.

У V5 внешняя send-action должна иметь бит `+2` ignore-errors. Следовательно, успешное исполнение корневого кошелька и увеличение seqno могут сопровождаться пропущенной отправкой. Для v1 предлагается фиксированный mode `3` (`+1` separate fees, `+2` ignore errors), проверяемый signer; его денежный эффект проверить fixtures и эмуляцией. Основание требования `+2`: [Wallet V5](https://docs.ton.org/contracts/standard/wallets/v5).

`ReceiptReverted` для общей оркестрации допустим только после доказательства экономического исхода. Bounce уведомления получателю Jetton сам по себе не доказывает возврат токенов. Unknown и lag сохраняют pending/held; они не запускают новую выплату или компенсацию.

### 3.3. Финансовые и эксплуатационные инварианты

- Один API intent создаёт не более одного успешно доставленного перевода основной суммы. Повторный broadcast использует тот же сохранённый BoC.
- Один доказанный chain effect создаёт не более одного финансового зачисления, независимо от провайдера, повторного чтения и порядка прихода observations.
- Финансовая финализация требует canonical account transaction и подтверждённой привязки её shard block к masterchain; число прошедших секунд не заменяет evidence. Основание: [TON payment processing](https://docs.ton.org/applications/payments/overview).
- Funding, deployment, sweep, excess, bounce и Jetton carrier value связаны со служебной операцией и не создают новый клиентский депозит автоматически.
- Приватный ключ/seed не покидает Vault. Авторизация ограничивает реальное содержимое подписываемого Cell, а не только поля JSON.
- Все RPC/Vault вызовы выполняются вне финансовой DB-транзакции. До вызова сохраняется intent; после — идемпотентная observation/result-команда с fencing.
- Суммы используют `CryptoAmount`/`big.Int`. `lt` и Jetton query ID не помещаются в signed `int64` без проверки; API передаёт их строками.
- Остановка новых подписей не останавливает наблюдение, подтверждение и settlement ранее отправленных операций.
- Неизвестное списание, расхождение профиля кошелька или providers переводит затронутый кошелёк/сеть в hold с сохранением evidence.

## 4. План по этапам и критерии завершения

### Этап 0. ADR, прототип чтения и воспроизводимые fixtures

1. Зафиксировать ADR: wallet profile, derivation, canonical addresses, execution references, delivery semantics, fee policy и доверие к RPC.
2. Выбрать SDK после проверки Cells/BoC, Ed25519, V5R1, TL-B, поддержки pinned block reads и воспроизводимых тестов. Кандидаты из [официального списка SDK](https://docs.ton.org/applications/sdks) — tonutils-go/tongo; закрепить конкретную версию. HTTP integration и ADNL не считать взаимозаменяемыми.
3. На публичных read-only данных проверить получение account history с пагинацией, raw transaction/message data, masterchain/shard linkage и поиск запроса по normalized message hash.
4. Рекомендуемая топология v1: HTTP indexer для discovery + независимые источники account/block evidence. Проверка двух провайдеров является моделью доверия, а не криптографическим доказательством. Для production выбрать собственный проверяющий узел/проверяемые proofs либо явно утверждённую модель доверия; при её нарушении финансовая финализация блокируется.
5. Собрать неизменяемые fixtures native/Jetton success, skipped action, bounce, zero-notification, deployment, expiry и excess. Привязать их к блокам, raw data и версии decoder.
6. Измерить throughput одного кошелька, lag индексатора, RPC budget и стоимость поддержания пула; определить SLO, размер пула, fee safety margin и лимиты canary по этим измерениям.

**Готово, когда:** ADR перечисляет выбранные значения и доказательства; прототип восстанавливает нужный transfer effect без доверия к одной готовой provider action; fixtures независимы от live API.

### Этап 1. Совместимое расширение доменных контрактов

Изменения: `internal/domain/crypto`, соответствующие application ports и DTO.

1. Добавить типизированные execution references, transfer outcome/evidence и отдельный budget для прикладываемой нативной стоимости. Определить legacy adaptation для TRON/EVM.
2. Расширить finality evidence: account transaction, workchain/shard block ID и masterchain anchor. Не перегружать `BlockNumber` значением `lt` или shard ID.
3. Определить TON event kinds/semantic effect key. Jetton не маскировать под EVM contract log; сохранить детерминированный replay прежних событий.
4. Выделить wallet deployment lifecycle либо расширить существующий lifecycle с явным actor/source/target. Capabilities TON: account sequence, seconds для timestamp, derived-per-asset token accounts, без fee replacement и TRON resource rental.
5. Вынести общий evidence reader/repository из текущего `ExpectedTransfer` пути, чтобы family adapter сохранял собственную структуру доказательств.
6. Для unsigned payload выбрать TON typed schema-v1 либо новую схему с отдельной canonicalization. Текущая `CanonicalUnsignedFingerprint` принимает только version 1; старые байты fingerprint нельзя менять.
7. Зафиксировать migration path подписываемых authorization schemas и `ton-v1` policy. Поля, влияющие на native value, bounce, send mode, recipient owner и StateInit, должны проверяться signer независимо от прикреплённого JSON.

Отдельно определить signing purpose для deployment. В текущем [SigningPurpose](../../internal/domain/crypto/signing_purpose.go) такого значения нет, а нулевая сумма в `SigningAuthorization.Validate` разрешена только для activation. Предпочтительно добавить явный `wallet_deployment` с собственными domain/SQL/plugin ограничениями и шаблоном без transfer-actions; не расширять разрешение на нулевые money-movement команды вообще.

**Готово, когда:** контракты позволяют выразить все три результата из §3.2; прежние envelopes и authorizations воспроизводятся; architecture/conformance tests TRON/EVM проходят.

### Этап 2. PostgreSQL и долговечные идентификаторы

Добавить новые forward migrations, не редактируя исторические. Номера выбрать по актуальному состоянию репозитория при реализации. Имена ниже концептуальные; окончательные названия следуют соглашениям существующей схемы.

| Данные | Что хранить и ограничить |
| --- | --- |
| TON network profile | Каталожный UUID, global ID, genesis/zerostate identity, workchain policy, finality/trust policy |
| Wallet contract profile | Version/code hash, public key, wallet ID, workchain, derivation scheme/path, StateInit hash, профиль сети; immutable после выдачи адреса |
| Owner/Jetton mapping | `network_id + master + owner → Jetton wallet`, evidence derivation, code compatibility и статус readiness |
| Seqno ownership | Wallet identity, chain seqno, reserved seqno, operation generation, lease owner/version, expiry; уникальность активного reservation |
| Execution artifacts | Authorization/fingerprint, signed BoC, digest, normalized hash, `valid_until`, submission results и связи account transactions/messages |
| Transfer evidence | Versioned semantic effect key, raw evidence digest, source IDs, recipient effect и статус верификации |
| Watcher cursors | Account address, `lt` как uint64-compatible representation, transaction hash, MC anchor, backfill range и fencing |
| Fee/value settlement | Зарезервированная прикладываемая стоимость, фактические расходы, ожидаемые/полученные excess/refund и causal IDs |

Уникальность effects строить из canonical on-chain идентичности, например network/account transaction/in-message/asset/effect kind/детерминированный ordinal, а не provider trace ID или hash всего JSON. Versioned key должен давать одинаковый результат для двух провайдеров.

Проверить все SQL paths, а не только inserts: address-key trigger в [миграции 00006](../../db/migrations/00006_crypto_core.sql), case-insensitive contract index в [миграции 00006](../../db/migrations/00006_crypto_core.sql), signing authorization CHECK, event-kind CHECK и `LOWER(contract_address)` lookup при signing. Для TON записывать canonical raw master/address key и исключить lowercasing friendly base64.

Реестры watch addresses/token contracts из [миграции 00006](../../db/migrations/00006_crypto_core.sql) имеют TRON-специфичные triggers. Создать TON registry или совместимое расширение с отдельными правилами family, а не считать текущие triggers универсальными.

**Готово, когда:** миграции проходят на чистой и заполненной тестовой БД; параллельные reservations/effects блокируются constraints; новый бинарник читает legacy data; сеть TON остаётся выключенной. Индексы/backfill не требуют долгой блокировки финансовых таблиц.

### Этап 3. Read-only TON driver, адреса и RPC

Создать `internal/infrastructure/blockchain/ton/` с factory/driver, codecs, address policy, RPC pool, balance reader, account/token readers и verifier.

1. Распарсить raw/friendly base64/base64url, проверить checksum, сохранить canonical `workchain:lowercase-hex` и отдельно delivery flags. Test-only flag отклонять при mainnet; сам флаг не является доказательством идентичности сети. Основание форматов: [TON addresses](https://docs.ton.org/foundations/addresses/formats).
2. Проверять global ID и pinned network identity в preflight. Не переносить EVM-ограничение «идентификатор сети положительный» на TON.
3. Добавить `TONSettings`: endpoints/roles, trust groups, deadlines, пагинация, overlap/lookback, limits BoC/trace depth и свежести доказательств. Секреты — через существующие secret files.
4. Реализовать чтение native/Jetton balances, account status, seqno, wallet profile и разрешённых token masters на закреплённом блоке. Nonexistent, uninit, active и frozen не сводить к одному boolean без evidence.
5. Account history читать с курсором `lt + transaction hash`; проверять полноту страниц, отсутствие циклов, смену источника и позднюю индексацию. Перекрывающийся replay безопасен благодаря effect key.
6. Для block/finality verifier учесть shard identity, split/merge и masterchain anchor. Существующий плоский `CanonicalBlock(height)` не переносить буквально.
7. Подключить capability registry, bootstrap, preflight и startup reconciliation. Неисправная TON-сеть не должна отключать здоровые TRON/EVM-сети.

**Готово, когда:** public fixtures воспроизводятся через два источника, неверная сеть и неполные evidence блокируются, пагинация не теряет events, decoder работает в заданных resource limits.

### Этап 4. Vault-плагин и политика подписи

Создать `vault-sign-plugins/ton/` и `internal/infrastructure/crypto/ton_vault_signer/`; зарегистрировать их в custody.

1. Выбрать и версионировать Ed25519 derivation. Если используется hardened SLIP-0010, изменить allocator descriptor: текущий namespace/index не должен молча означать другой путь. Проверить независимые derivation/address vectors и восстановление из backup.
2. Формировать address из зафиксированного StateInit. Сохранить contract profile до включения кошелька в pool; повторная derivation должна возвращать те же публичные данные.
3. Signer самостоятельно разбирает BoC/TL-B и принимает только утверждённые шаблоны: wallet deployment, native transfer, Jetton transfer. Проверяет tenant/network/key, source profile, seqno, срок, destination/owner/master, amount, attached value, send mode, bounce, response destination и forward payload.
4. Запретить extensions, отключение signature auth, лишние actions, произвольный StateInit/code/body, send-all/carry-all/destroy и неподдерживаемые адресные формы. Разрешение конкретного StateInit для deployment не означает разрешение произвольного контракта.
5. Привязать policy authorization к полному unsigned payload; после подписи проверять неизменность и сохранять exact broadcast artifact. Normalized message hash используется отдельно для поиска.
6. Ограничить число Cells, refs, глубину, длину body/comment и время разбора. Проверить trailing/unparsed data, необычные Cell types и несовпадение JSON с декодированными полями.
7. Добавить сборку plugin binary, SHA-256 registration, Vault ACL/mount/readiness, тестовый image и production deployment path. Plugin health должен подтверждать ожидаемую версию policy, а не только доступность Vault.

Signing request должен быть воспроизводим по authorization ID + fingerprint. При потере ответа Vault вернуть тот же результат для immutable payload либо безопасно восстановить его; новый payload под прежней authorization запрещён. Signer не выполняет broadcast.

**Готово, когда:** независимый decoder подтверждает подпись и payload; все mutation fixtures отвергаются; ключи не попадают в application/logs; backup restore воспроизводит адреса; тестируется реальный plugin API.

### Этап 5. Развёртывание кошельков и readiness пула

1. Реализовать durable workflow: derived → funding-pending → funded → deployment-signed/submitted → deployed-and-verified → ready либо held.
2. Funding приходит из разрешённого network-fee/hot wallet отдельной операцией. Deployment подписывается ключом целевого TON-кошелька с разрешённым StateInit; это не TRON activation от hot wallet с нулевой суммой.
3. До выдачи адреса инвойсу проверить код, public key, wallet ID, signature auth, отсутствие extensions, баланс/остаток для эксплуатации и актуальный статус аккаунта.
4. При повторном запуске сначала восстановить funding/deployment по сохранённым references; не создавать повторное пополнение только из-за RPC timeout.
5. Поддержать низкий запас готовых адресов, storage reserve и hold замороженных/неожиданно изменившихся аккаунтов. События funding/deployment должны классифицироваться до invoice matching.
6. Jetton wallet создаётся в соответствии с конкретным master/реализацией, обычно при первом переводе. Не требовать отдельный predeployment всех Jetton wallets без необходимости; различать «адрес вычислен», «аккаунт существует» и «готов выполнить нужную операцию».

**Готово, когда:** сбой на каждом шаге не создаёт повторного движения денег; неготовый адрес не выдаётся; после восстановления pool не теряет ни profile, ни наблюдаемые операции.

### Этап 6. Входящие native payments

1. Watcher получает account transactions управляемых адресов, сохраняет raw observations до ack и строит semantic incoming effect.
2. Отделяет клиентские сообщения от внутренних funding/sweep, bounce/refund, excess и carrier native value в token protocol. Неизвестное сообщение сохраняется для review; экономический смысл не определяется одним положительным balance delta.
3. Сопоставляет effect с существующей арендой адреса и invoice rules по chain time и immutable evidence. Поздняя индексация, expired invoice, under/overpayment, one-payment mode и cooldown адреса сохраняют действующие бизнес-правила.
4. Фиксирует gross inbound value и расходы раздельно. Рекомендуемая политика: основная сумма инвойса соответствует доказанному inbound value, processing fee — существующей политике, account/storage/compute costs — отдельным расходам платформы. Утвердить это в ADR и сверить с текущим native posting.
5. Финализирует платёж только по проверенному recipient effect и MC anchor. Payment, ledger и webhook/outbox изменяются атомарно; replay не создаёт повторное зачисление.
6. Проверяет восстановление с сохранённого cursor, повторный backfill, переключение RPC и повторную выдачу адреса без ошибочного переноса платежа на новый invoice.

**Готово, когда:** native invoice проходит полный путь до проводок и webhook; все служебные переводы исключены из автоматических клиентских депозитов; replay и reordering не меняют результат.

### Этап 7. Seqno, исходящие native операции и recovery

1. Создать PostgreSQL seqno manager с уникальным reservation, generation и fencing на wallet/network. Резервировать по согласованному chain state; не копировать EVM replacement/nonces без проверки семантики.
2. В v1 разрешать один неподтверждённый запрос к одному кошельку. После доказанного root acceptance следующий seqno может использоваться, даже если доставка предыдущего перевода ещё pending; budgets обоих intent остаются раздельными.
3. Путь: reserve budget/seqno → build + fingerprint → authorize → sign → persist signed artifact → broadcast → resolve root → resolve recipient effect → settle. RPC и signing находятся вне DB locks.
4. На timeout/ambiguous broadcast искать сохранённый запрос и повторять тот же BoC в пределах valid-until. Не создавать новый signed attempt на основании `not found` одного indexer.
5. Повторное построение после expiry допускается только когда доказано, что старый запрос больше не может быть принят и ранее не исполнился. Проверить chain time, непрерывную account history/seqno и freshness источников. При неполной истории — hold.
6. Если seqno занят неизвестной операцией — hold и reconciliation, без автоматического пропуска. Unsigned reservation освобождается только при отсутствии подписи/возможной отправки.
7. Подключить withdrawals, treasury, native sweep и funding к одному общему tracking/settlement port. Существующие hold, approval и idempotency правила сохранить.
8. Не использовать EVM fee replacement. Для native sweep оставлять рассчитанный остаток вместо send-all; долгосрочные расходы кошелька учитываются отдельно.

Fencing защищает DB-переходы, но не отзывает уже выданную подпись. Lease takeover не разрешает новый конфликтующий generation, пока прежнее сообщение может исполниться. Эта граница должна быть проверена отдельным тестом со старым worker, продолжающим работу после потери lease.

**Готово, когда:** crash/timeout/restart/concurrent worker сценарии дают максимум одну доставку; skipped action не считается выплатой; неизвестное исполнение не компенсируется и не отправляется заново.

### Этап 8. Jetton: master, owner, wallet и эффект перевода

1. В `AssetRef.ContractAddress` хранить canonical master address. Identity актива — network + master; name/symbol/icon и присланный sender не являются доказательством.
2. Для каждого управляемого owner получить ожидаемый Jetton wallet, проверить owner/master и совместимость реализации на закреплённом состоянии. Каталог допускает только исследованные masters/варианты, включая их upgrade/admin риски.
3. Приём подтверждать исполнением в ожидаемом recipient Jetton wallet; incoming notification используется как сигнал и связанное evidence. `query_id` коррелирует сообщения, но не заменяет semantic effect identity.
4. Переводы без notification должны проходить отдельную верификацию recipient transaction/state transition; один текущий balance delta не доказывает конкретную оплату. При недостаточных evidence — review/held.
5. Для исходящего transfer бизнес-получателем является owner, transport destination — source Jetton wallet. Signer проверяет master/source mapping, owner, amount, response destination, forward amount/payload и attached native budget.
6. Подключить token account lifecycle в orchestration: prepare mapping/readiness, оценить lazy deployment получателя и стоимость, затем signing. Сам по себе `TokenAccounts()` ничего не запускает.
7. Settlement проверяет фактический recipient token effect. Если токены зачислены, а последующее owner notification bounced, основная сумма не компенсируется. Если transfer не состоялся, отдельно доказать rollback/возврат principal и учесть native fee.

Стандарт прямо допускает `forward_ton_amount = 0`, при котором notification не отправляется. Поэтому отсутствие notification нельзя приравнивать к неуспеху. Адреса сообщений, operation codes и payload проверяются по [TEP-74](https://github.com/ton-blockchain/TEPs/blob/master/text/0074-jettons-standard.md), а поля transfer — по [Jetton API](https://docs.ton.org/contracts/standard/tokens/jettons/api).

**Готово, когда:** incoming, sweep и payout разрешённого Jetton проходят с deployment/nondeployment получателя, zero/nonzero forward amount, fake notification и bounce-after-delivery; лишняя компенсация principal исключена.

### Этап 9. Комиссии, attached value, excess и сверка

1. Расширить fee quote/budget: principal, attached native value, верхняя граница фактических расходов, forward value, deployment allowance, residual/storage reserve. Резервировать весь необходимый cash; категории не должны пересекаться и суммироваться дважды.
2. Обновить JIT funding: считать native requirement из выбранного шаблона и quote, а не только `MaxFee + minResidual`. Повторный funding защищён отдельным operation ID и causal link.
3. Использовать оценки по актуальной конфигурации/эмуляции с TTL и margin, проверять limit перед подписью. Не фиксировать навсегда одну сумму комиссии для всех Jetton и состояний аккаунта.
4. Сохранять source-paid fees и связанные расходы по account transactions. Переданное attached value не объявлять целиком network fee; расходы сторонних участников trace не списывать с платформы повторно.
5. Excess/refund признавать только по доказанному сообщению и causal chain. Возврат native excess не означает возврат Jetton principal. Поздний excess создаёт отдельное идемпотентное движение, не переписывает прошлую проводку.
6. После доказанной доставки principal можно завершить бизнес-выплату, сохраняя отдельное ожидание fee/refund reconciliation и соответствующие резервы. Не ждать завершения произвольного стороннего trace бесконечно.
7. Сверять ledger, бюджеты и on-chain остатки на общей MC boundary; включить pool funding, storage costs, sweep, pending executions и refunds. Не исправлять расхождения прямым UPDATE balance.

Протокол отдельно учитывает storage/compute/forwarding и другие компоненты расходов; их параметры зависят от состояния и конфигурации сети. Основание: [TON fees](https://docs.ton.org/foundations/fees).

**Готово, когда:** native и Jetton операции сходятся по principal, фактическим расходам, возвратам и остаткам; повторные/задержавшиеся excess не меняют баланс дважды.

### Этап 10. API, checkout, эксплуатация и включение mainnet

1. Каталоги/API/SDK: разрешить существующие TON/Jetton enum в реально поддержанных сценариях; добавить typed execution reference и backward-compatible поля статуса. До реализации runtime не показывать TON как доступную для оплаты.
2. Checkout: адрес selected network, корректный friendly format/QR, decimals из каталога, понятные pending/held состояния. Если поддержан outgoing comment для биржевого получателя, включить его в immutable intent и policy; ограничить размер, не помещать PII.
3. Explorer: [текущий helper](../../frontend/lib/blockchain.ts) понимает только `{txHash}`. Добавить ссылку по типу reference и показывать account transaction после его обнаружения; исключить подстановку BoC digest в transaction URL.
4. Обновить `cmd/crypto-preflight`, `cmd/public-test-bootstrap`, deployment samples и readiness. При отключённом Vault read-only health не означает готовность к выплатам.
5. Метрики: masterchain/indexer lag, cursor/backfill gap, unresolved root/delivery age, skipped actions, seqno holds, provider divergence, pool readiness, native reserves, refund age и reconciliation drift. Ограничить cardinality; адреса/operation IDs — в защищённом audit.
6. Runbooks: ambiguous broadcast, expiry, frozen wallet, unexpected seqno, fake Jetton, stalled excess, смена RPC, Vault restore, stop-new-signing/drain-pending. Не рекомендовать оператору «просто повторить выплату» при неизвестном исходе.
7. Расширить существующую [матрицу сценариев](../../docs/crypto/crypto-processing-test-scenarios.md) и [mainnet go/no-go](../../docs/operations/crypto-mainnet-go-no-go.md). Evidence привязать к candidate commit/image digest, plugin hash, schema, network/token profile и provider topology.
8. Включать по очереди: testnet native → testnet allowlisted Jetton → public testnet recovery/soak → mainnet read-only → ограниченный native canary → ограниченный Jetton canary → увеличение лимитов. Пороговые значения и длительность определяет этап 0 по измерениям.

Порядок rollout: совместимые миграции → новые binaries/plugin при выключенной TON → preflight/backfill → включение выбранных возможностей. После появления TON attempts откат на бинарник, который не умеет их наблюдать, недопустим: остановить новые подписи и оставить совместимый confirmation/recovery worker. Миграционный rollback не должен удалять evidence или незавершённые операции.

**Готово, когда:** требуемые сценарии имеют evidence/PASS, критичные unknown устранены, restore проверен, наблюдаемость и остановка подписей работают, canary сверяется без необъяснённого drift.

## 5. Детали, которым уделить особое внимание

| Приоритет | Ошибка, которую нужно исключить | Правильное поведение |
| --- | --- | --- |
| P0 | «Root tx успешна → выплатить/закрыть» | Проверить ожидаемую отправку и recipient asset effect |
| P0 | «Уведомление Jetton не пришло/отскочило → вернуть principal» | Проверить recipient token transaction; токены могли быть доставлены |
| P0 | «RPC timeout/not found → новая подпись» | Восстановить прежний запрос, повторять exact BoC, hold при неоднозначности |
| P0 | Normalized message hash используется как signing fingerprint | StateInit и полный Cell payload привязаны отдельной авторизацией |
| P0 | Fake Jetton с тем же символом или fake notification | Identity по network/master и проверенному recipient wallet |
| P0 | Смешаны fee, attached value, excess и principal | Отдельные budgets/effects; причинные связи и double-entry settlement |
| P0 | Два worker подписали один seqno/разные поколения | DB reservation, fencing, immutable signed artifact и recovery |
| P0 | Funding/excess/carrier value попали на клиентский invoice | Служебная классификация до invoice matching; unknown в review |
| P1 | Friendly адрес/hash приведён к lowercase | Decode → canonical bytes/raw address, flags сохраняются отдельно |
| P1 | `lt`/query ID прошёл через int64/JavaScript number | Проверенные uint64-compatible типы и строковые API поля |
| P1 | Новый SDK/wallet ID изменил прежние адреса | Immutable profile, pinned dependency, derivation/state-init vectors |
| P1 | Lag/split/merge потеряли платеж или приписали новой аренде | Account cursor, MC evidence, overlap/backfill и старые lease rules |
| P1 | Успешные native тесты считаются готовностью Jetton | Отдельная вертикальная приёмка master/mapping/recipient effect/fees |

## 6. Обязательная матрица проверки

Все строки ниже — планируемые проверки, статус `NOT_RUN`. Unit mocks недостаточны для Cells/signing/delivery: использовать raw fixtures, эмуляцию/локальную сеть, PostgreSQL integration и public testnet по назначению.

| Группа | Сценарий | Ожидаемый результат |
| --- | --- | --- |
| Address/profile | raw/friendly/base64url, плохой checksum, test-only в mainnet, другой workchain | Одна canonical identity либо явный отказ |
| Custody | Изменены amount/dest/master/mode/StateInit/forward payload после authorizing | Signer отказывает; legacy vectors сохраняются |
| Custody | Backup restore, SDK upgrade, malformed/oversized/deep BoC | Те же адреса; bounded parsing; unsupported payload отвергнут |
| Deploy | Crash после funding, после signing, после broadcast и до DB ack | Одна служебная операция, readiness только после проверки |
| Incoming | Один effect от двух providers, replay, out-of-order pages | Одно зачисление и одна финансовая transition/outbox запись |
| Incoming | Поздняя оплата после expiry/reuse, one-payment invoice, недоплата/переплата | Существующие lease/invoice/review правила |
| Incoming | Funding, Jetton carrier, excess, bounced return на leased address | Нет автоматического клиентского зачисления |
| Finality | Нет MC anchor, provider divergence, lag, shard split/merge | Нет преждевременной финализации; восстановление без пропусков |
| Native outgoing | Root accepted и seqno consumed, send-action skipped | Выплата не считается доставленной |
| Native outgoing | Recipient success, recipient bounce и частичный native refund | Правильный principal outcome; расходы/возвраты отдельно |
| Jetton | Fake master/wallet/notification с правильным symbol/opcode | Нет зачисления или подтверждения выплаты |
| Jetton | `forward_ton_amount=0`, notification отсутствует | Эффект определяется recipient wallet evidence |
| Jetton | Токены доставлены, owner notification bounced | Нет компенсации уже доставленного principal |
| Jetton | Недостаточный attached value, lazy deployment, excess поздно/дважды | Нет ложного success; expenses/refund сходятся один раз |
| Seqno | Два процесса/lease takeover, unknown external seqno | Один разрешённый активный generation на reservation; не более одной доставки intent; hold для unknown |
| Recovery | Broadcast accepted, ответ потерян; повтор после restart | Поиск прежнего запроса и безопасный повтор exact BoC |
| Recovery | Expired request и отстающий indexer | Новая отправка только после доказанного неисполнения |
| Recovery | Подпись получена, но artifact не сохранён | Broadcast не выполняется; reservation не освобождается без reconciliation |
| Settlement | Crash между effect, ledger и webhook/outbox | Атомарность финансового перехода, идемпотентный replay |
| Accounting | Несколько fees/refunds, storage расходы, pending balances | Нулевой необъяснённый reconciliation drift |
| Isolation | TON RPC/Vault неисправен при работающих TRON/EVM | Здоровые сети продолжают работу |
| Regression | Старые auth/envelopes/receipts, TRON activation, EVM nonce/replacement | Семантика прежних сетей сохранена |

Планируемые проверки кода: targeted `go test` для domain/crypto, chainconfig/bootstrap/conformance, TON driver, custody, worker и crypto_repo; race tests seqno/watcher; отдельные тесты каждого Vault Go module; миграции на disposable PostgreSQL; API/SDK/frontend contracts. Точные integration команды брать из актуальных test harness и package scripts на момент реализации. Live выплаты не запускать в общем CI.

## 7. Разбиение на поставляемые изменения

| Поставка | Содержание | Зависимости / результат для ревью |
| --- | --- | --- |
| PR-1 | ADR, provider read prototype, protocol fixtures, SLO/economics | Выбор решений этапа 0; TON ещё недоступна |
| PR-2 | Domain ports, execution/effect/budget contracts и legacy adapters | PR-1; доказанная совместимость TRON/EVM |
| PR-3 | Миграции, canonical keys, profiles, cursors и reservations | PR-2; integration на чистой/заполненной БД |
| PR-4 | Read-only driver, config, verifier, preflight и bootstrap | PR-2/3; независимое чтение и проверка evidence |
| PR-5 | Vault plugin, derivation/signing policy, images и restore vectors | PR-1/2/3; безопасная подпись утверждённых шаблонов |
| PR-6 | Wallet funding/deployment и pool readiness | PR-4/5; адрес выдаётся после проверенного deployment |
| PR-7 | Native incoming вертикально до ledger/webhook | PR-4/6; приём в testnet и replay/finality проверки |
| PR-8 | Seqno, native outbound/sweep/treasury, общий tracking/recovery | PR-3/4/5/6; завершённая native testnet вертикаль |
| PR-9 | Jetton mapping/watch/build/verify/settle | PR-7/8; testnet allowlisted Jetton вертикаль |
| PR-10 | Полная fee/refund reconciliation, JIT, API/explorer и operations | Budget/accounting минимум входит уже в PR-7/8/9; здесь завершение всех случаев |
| PR-11 | Candidate evidence, regression/restore/soak и canary configuration | Все предыдущие; отдельное решение о mainnet readiness |

PR-4 и PR-5 можно разрабатывать независимо после фиксации общих контрактов. Базовый fee settlement и recovery обязательны уже для первой native вертикали; их нельзя отложить до PR-10. Каждую поставку делить дополнительно, если независимый reviewer не может проверить её финансовый эффект.

## 8. Итоговые критерии реализации

- TON — полноценный driver/custody path, а не только enum/catalog entry; generic workers не импортируют TON adapter напрямую. Это проверяет существующий [architecture test](../../internal/infrastructure/crypto_worker/architecture_test.go).
- Native и каждый разрешённый Jetton проходят invoice → finality → ledger/webhook → sweep → payout/treasury → reconciliation.
- Ни timeout, ни crash, ни повторный event не создают дополнительную доставку или зачисление; неизвестный исход удерживается с evidence.
- Подтверждение основано на recipient effect, а компенсация — на доказанном результате principal; source receipt/notification сами по себе недостаточны.
- Полный payload ограничен Vault policy; адреса и ключи восстанавливаются; бюджет, реальные расходы и возвраты сходятся с on-chain активами.
- TRON/EVM regression пройдена, требуемая acceptance evidence относится к точному release candidate, эксплуатационные лимиты проверены на canary.

До выполнения этих критериев наличие TON в справочнике, успешная одиночная отправка или зелёный RPC healthcheck не означает готовность сети для финансового процессинга.
