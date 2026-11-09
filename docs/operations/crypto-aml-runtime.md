# AML runtime Getblock.net

Runtime связывает verified incoming effects с AML jobs, сохраняет reports, применяет версионированную risk policy, пересчитывает риск wallet/token и публикует `wallet.risk_changed`. Одна leased-операция выполняет один платный submit или один free poll. Классификация не меняет финансовые проводки. [Portal баланс по риску](crypto-aml-risk-balances.md) читает отдельный согласованный net snapshot; вывод по выбранному классу подключён через [новый outbound контур](crypto-aml-outbound.md).

Условия полноты покрытия, повторная проверка и ограничения старого вывода описаны в [проекции риска](crypto-aml-risk-projection.md).

Для Tron Nile и Ethereum Sepolia доступен отдельный [testnet AML emulator](crypto-aml-testnet-emulator.md), миграция 00074. Он не использует paid admission и не подменяет live Getblock.net jobs.

## Профиль и версия контракта

- `getblock` — прежний RPC-профиль, отображаемое название Getblock.io, slots `fullnode/solidity` сохранены.
- `getblock_net` — новый AML/KYT-профиль с одним bearer token в slot `default`. Секрет хранится в зашифрованных credentials integration account и не включается в reports/logs.
- `sys_crypto_aml_provider_mappings` содержит immutable версии network/token/contract/decimals, provider currency/token ID, принятую версию контракта, pending/failure statuses и snapshot currencylist. Для capability acceptance требуется ссылка на доказательство. Пустая или неподтверждённая mapping не допускает paid submit.
- Resolver читает mapping версии, записанной в job. Новая версия не подменяет контракт уже созданного ticket. Одна generation закреплена за одним аккаунтом; повтор enqueue с другим account ID отклоняется. Credential rotation не меняет provider account ID и не переносит job в другой аккаунт.

Значения provider token IDs и списков статусов не зашиты для Mainnet. Синтетические integration fixtures не являются приёмкой реального vendor contract. Текущий HTTP endpoint фиксирован адаптером; generic RPC transport с автоматическим failover не используется.

## Локальный бюджет платных запросов

`QuotaPackage` не имеет месячного/суточного периода. Статус такого аккаунта возвращает `nextResetAt=null`; generic RPC selection не применим к этому профилю.

Runtime использует отдельный **локальный лимит разрешённых платных запросов**:

- `reg_crypto_aml_package_credits` — append-only grants: аккаунт, число проверок, уникальная ссылка на приобретённый/выделенный пакет и кто разрешил использование. Повтор той же ссылки не создаёт новый бюджет.
- `reg_crypto_aml_paid_admissions` — append-only запись расходования одной проверки для job. Доступный локальный бюджет равен сумме grants минус числу admissions этого аккаунта.
- Перед HTTP runtime блокирует account/policy, проверяет актуальную submit lease и остаток, затем фиксирует admission. Конкурирующие worker не могут потратить одну единицу дважды. Смена ключей, ручное возобновление RPC-пула и новая календарная дата не добавляют grants.
- Admission не возвращается автоматически при неизвестном результате, ошибке или падении процесса перед HTTP. Это консервативный лимит расходов. Неопределённый submit становится `submission_unknown`; повторный платный вызов для той же job запрещён.

Этот счётчик **не является live балансом пакета у провайдера**. `cheks_left/cheks_used` ещё не синхронизируются; число локальных admissions также не является подтверждённым vendor billing. Получение package observations и сверка с grant/admission ledger требуют отдельной реализации. Доступ к автоматическому grant по календарю отсутствует.

Если бюджет кончился или аккаунт/accepted mapping не готовы, pending submit не выбирается worker. При конкуренции нескольких уже выданных leases последний admission может получить отказ без HTTP и перейти в `needs_attention`. Дедлайн jobs сохраняется. Free polling уже полученных tickets не зависит от остатка бюджета и `allow_submissions`.

## Включение

Миграции 00063–00064 не создают принятых mappings, grants, включённых аккаунтов или routing scopes. Рабочие базы в рамках разработки не обновляются.

Для использования нужны все условия:

1. Активный integration account `getblock_net` с отдельным зашифрованным bearer token.
2. Реальная приёмка vendor contract/currencylist и новая immutable mapping для точной network/token identity. SQL проверяет network family, native/contract scope и decimals при создании версии.
3. Явно выделенный локальный бюджет grants и `sys_crypto_aml_account_policies.allow_submissions=true`.
4. Для автоматического intake — явно включённый `sys_crypto_aml_routing_scopes` по merchant/network/token с принятой mapping, immutable risk policy и provider account. Его включение/изменение требует остановленного network policy и enforced merchant custody. Включённый scope нельзя удалить или выключить для возврата к старому пути расходования.
5. Job с доказанным custody owner и chain evidence, закреплённым account ID и mapping version. Verified ingestion создаёт её атомарно с effect; backlog подбирает подходящие proofs, сохранённые до включения scope. Replay не создаёт новую платную проверку.

CryptoProcessor запускает loop в своём lifecycle с остановкой по context. Каждые 5 секунд он обрабатывает ограниченные порции backlog, локальной [inventory allocation queue](crypto-aml-inventory-allocations.md), submit/poll, решений, проекций и risk events. SQL leases/admissions коммитятся до сетевого вызова; HTTP выполняется с коротким timeout. Сохранение входящего effect не требует активного аккаунта или доступного бюджета: недоступность AML-провайдера оставляет риск неизвестным, но не откатывает chain receipt. Внешний HTTP выполняется только после runtime admission.

Включённый routing scope сейчас закрывает все новые отправки соответствующего актива через прежний контракт: он ещё не содержит risk class, source revisions и dispatch permit. Старые signed/broadcast attempts остаются доступны для чтения chain history и recovery. Новый [AML outbound](crypto-aml-outbound.md) уже использует risk class, source revisions и dispatch permits. Его production-включение требует отдельной приёмки; старый путь закрыт.

Административные команды для приёмки mapping и выделения бюджета ещё не вынесены в UI/API; эти настройки относятся к доверенному control plane. Миграция, обновление бинарников и включение требуют согласованного rollout. Rollback с сохранёнными runtime evidence/новыми профилями блокируется; используется forward migration.

## Проверка

Integration tests используют отдельную временную БД и синтетический HTTP transport. Проверены restart submit/poll, запрет SQL-locks во время HTTP, потеря ACK без второго paid request, конкурентный admission, отсутствие заимствования чужого бюджета, immutable mapping и продолжение poll без новых submit. Отдельные routing tests проверяют verified intake, late high, поколения rescreen, смену policy/mapping, reorg, полноту inventory и идемпотентный risk outbox. Реальных платных запросов при этих проверках нет.
