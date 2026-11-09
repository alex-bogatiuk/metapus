# Безопасность и архитектурное обоснование криптопроцессинга Metapus

> **TL;DR:** Архитектура использует Vault-side policy enforcement, immutable
> money intents, независимые ledger и reconciliation. Прежние программные
> blockers закрыты; production всё ещё требует внешний hardened Vault,
> backup/restore, alerting, formal finality и Mainnet operational gate.
>
> **Тип:** Concept
>
> **Связанные:** [crypto-processing-architecture.md](crypto-processing-architecture.md), [crypto-environment-setup.md](crypto-environment-setup.md), [crypto-processing-test-scenarios.md](crypto-processing-test-scenarios.md)
>
> Документ сознательно разделяет реализованные гарантии, операционные предпосылки и незакрытые риски.

---

Текущий custody/AML-контракт дополняет описанные ниже общие проверки: immutable merchant owner, verified inventory, class-specific sources, operating gas и permits проверяются независимо. [AML outbound](../operations/crypto-aml-outbound.md) и [custody runbook](../operations/crypto-merchant-custody.md) задают условия включённого scope. Наличие этих механизмов в коде не подтверждает production rollout; release gates остаются обязательными.

## 1. Краткий ответ

Сильная сторона Metapus — не отдельный криптокошелёк, а сквозная модель контроля движения денег:

- приём, обязательство перед мерчантом, комиссия и расходы платформы учитываются раздельно;
- приватные ключи управляемых кошельков находятся в Vault, а не в БД приложения;
- authorization private keys находятся в per-tenant Vault Transit и не попадают
  в worker;
- исходящий перевод требует совпадения документа, immutable intent, policy, approvals и параметров транзакции;
- sweep разрешён только на derived address из tenant-scoped Vault allowlist;
- approval intent употребляется один раз, а exact retry возвращает прежнюю
  подписанную транзакцию;
- повторная обработка защищена idempotency keys, уникальными индексами, reservations и сохранением подписанной транзакции;
- pool-адрес с расхождением blockchain/ledger автоматически исключается из выдачи;
- вывод мерчанта и вывод treasury имеют разные источники средств и разные allowlist;
- чувствительные действия требуют MFA/step-up и ролевой модели.

На уровне source/regression controls закрыты прежние программные blockers:
authorization signer, lifecycle webhook outbox, failed-withdrawal compensation,
fee fail-closed, token isolation, dynamic watcher refresh, multi-contract
loading, exact external-effect matching и merchant CIDR policies. Это не означает,
что текущий candidate уже прошёл Nile/Sepolia: execution verdict хранится только
в acceptance evidence и по умолчанию равен `NOT_RUN`.

При этом текущую версию нельзя называть полностью production-ready. Открыт
отдельный Mainnet operational gate: внешний hardened Vault, PostgreSQL/Vault
backup и restore drills, key rotation, P1 alerting/on-call, formal TRON
finality/reorg policy, provider-outage/load/chaos E2E, treasury acceptance и
proof-of-assets-and-liabilities procedure.

Закрытые свойства должны оставаться обязательными regression gates каждого
release; они не заменяют операционную готовность production-инфраструктуры.

## 2. Какие активы защищаем

| Актив | Основная угроза | Основной контроль |
|---|---|---|
| HD seed/private keys | кража и произвольная подпись | хранение и derivation внутри Vault plugin |
| USDT/TRX на hot wallet | несанкционированный вывод | policy, approvals, authorization JWS, Vault validation |
| Pool balances | потеря входящих денег | watcher checkpoints, idempotency, reconciliation, allocation hold |
| Баланс мерчанта | двойное списание/зачисление | immutable movement registers, debit-first, unique effects |
| Доход платформы | смешение с обязательствами | отдельный fee register и treasury workflow |
| Расходы на Energy/TRX | скрытые потери | отдельный platform cost register и provider reconciliation |
| Внешнее native funding | неучтённый приход/ручная корректировка | chain-verified immutable funding receipt и физический wallet movement |
| Merchant API credentials | утечка/повторное использование | plaintext показывается один раз, хранится SHA-256 hash, scopes/expiry/revoke |
| Webhook secret | подмена событий | HMAC, step-up на создание/rotate с однократным показом, delivery audit |
| Merchant network boundary | угон portal/API credential с чужой сети | Portal/API/per-key CIDR intersection, trusted proxies, audit и recovery |
| Audit trail | удаление следов | документы, movement registers, event logs, immutable observations |

## 3. Почему Vault действительно уменьшает риск

### В приложении нет seed

Tenant DB хранит:

- адрес;
- `vault_key_id`;
- документ и бизнес-параметры;
- unsigned/signed transaction JSON;
- подписанное разрешение.

HD seed и производные private keys находятся в Vault. Derivation идёт по:

```text
m/44'/195'/{account}'/0/{index}
```

Приложение получает адрес или подписанную транзакцию, но не приватный ключ.

### Vault проверяет не только «какой ключ»

Перед подписью custom TRON plugin проверяет:

- tenant binding;
- authorization signature и expiry;
- purpose;
- source и `vault_key_id`;
- destination;
- token contract;
- amount;
- fee limit;
- basis document;
- txID/raw data;
- policy digest;
- approvals для withdrawal/treasury.

Это защищает от класса атак «скомпрометировали worker и попросили Vault подписать перевод злоумышленнику». Для успешной атаки потребуется одновременно сформировать корректный signed authorization и удовлетворить policy/approval constraints.

### Что всё ещё требуется от эксплуатации

Vault не является магией. В production необходимы:

- HA/storage backend и disaster recovery;
- TLS и network isolation;
- short-lived auth вместо статического root token;
- отдельные policies для server, worker и оператора;
- audit device;
- seal/unseal или auto-unseal политика;
- ротация authorization/policy/approval keys;
- мониторинг denied signing requests;
- backup и проверенные restore drills.

Текущий local setup использует dev Vault token и может сохранять mnemonic в `.env.crypto.local`; это допустимо только для disposable dev-среды.

## 4. Почему двойная выплата маловероятна

Одна и та же withdrawal защищена несколькими независимыми слоями:

1. Merchant API/portal idempotency и уникальные business keys.
2. Debit-first списание merchant balance при создании заявки.
3. Frozen Money Movement Intent, digest которого зависит от суммы, адреса, токена и назначения.
4. Unique intent на basis document.
5. Role-separated approvals, привязанные к intent digest и policy version.
6. Worker claim с lease/locking.
7. Резерв token balance hot wallet.
8. Один `CryptoWithdrawal` на `WithdrawalRequest`.
9. Одна signing authorization на basis/attempt.
10. Signed transaction сохраняется и переиспользуется при неопределённом broadcast.
11. Settlement имеет собственный claim/repair lifecycle.

Нельзя полагаться только на HTTP idempotency: process может упасть после отправки в blockchain, но до ответа API. Поэтому важнейшая гарантия — сохранённая подписанная транзакция с детерминированным txID и запрет создания «новой попытки вслепую».

## 5. Почему бухгалтерский учёт не смешивает деньги

Metapus ведёт несколько независимых ledger dimensions:

| Остаток | Экономический смысл |
|---|---|
| Managed wallet token balance | сколько токенов система считает находящимися на конкретном адресе |
| Merchant balance | сколько платформа должна конкретному мерчанту |
| Processing fee balance | накопленный доход платформы |
| Platform cost | затраты на активацию, Energy и network fee |
| Provider balance | предоплата и списания у iTRX |

Пример платежа 100 USDT при комиссии 1 USDT:

- pool wallet: +100 USDT;
- merchant payable: +99 USDT;
- processing revenue: +1 USDT.

Свип 100 USDT:

- pool wallet: −100 USDT;
- hot wallet: +100 USDT;
- merchant payable не меняется.

Withdrawal 20 USDT:

- при заявке merchant payable: −20 USDT;
- при broadcast hot wallet: −20 USDT;
- повторного списания merchant payable нет.

Energy и TRX network fee относятся к platform cost. Они не уменьшают merchant balance, если бизнес-тариф явно не предусматривает отдельную merchant fee.

Внешний native TRX перевод на hot/pool wallet отражается только после
chain-verification командой `crypto-record-wallet-funding`: immutable receipt
и физический приход создаются атомарно, а `tx_hash` уникален. Активация account
создаёт обратный физический расход hot wallet и `chain_network_fee` platform
cost с `purpose=activation`. Прямые SQL-корректировки ledger не являются
допустимым операторским процессом.

## 6. Как система обнаруживает потерю синхронизации

Watcher и ledger — не единственный источник истины. Отдельная reconciliation сравнивает:

- TRC-20 balance адреса в ledger и blockchain;
- TRX balance адреса;
- известные исходящие транзакции;
- неизвестные external outflow;
- внутренний provider ledger и баланс iTRX.

Результат хранится в трёх уровнях:

- run — запуск проверки;
- incident — текущее расхождение;
- immutable observation — исторический снимок.

При balance mismatch pool-адрес ставится на allocation hold и перестаёт выдаваться новым инвойсам. Это важнее обычного alert: система автоматически ограничивает дальнейший ущерб.

Unknown external outflow проходит append-only classification/resolution workflow.
Exact match требует полного expected chain effect; подтверждённый custody loss
создаёт maker-checker `CryptoCustodyAdjustment`, не затрагивающий merchant
balance. Adjustment проводится общим `posting.Engine`, а deferred DB constraint
не позволяет зафиксировать `posted` без точного wallet/platform movement set или
впоследствии удалить эти движения. Reconciliation для resolution должна быть
завершена уже после posting decision. Независимые owner-bound holds снимаются
только workflow своего incident.
Для production остаются SLA разрешения incidents, alert routing и исполненный
reconciliation/custody drill на конкретном candidate.

## 7. Как защищён приём платежа

- Адрес выдаётся только после TRON account activation и clean reconciliation.
- Выбор адреса транзакционный и конкурентно-безопасный.
- Tx hash уникален.
- Инвойс блокируется при применении платежа.
- Суммы хранятся целыми минорными единицами, без floating point.
- Комиссия фиксируется snapshot-ом в момент платежа.
- Число confirmations настраивается на уровне сети.
- Checkpoint watcher сохраняется в БД.
- После restart используется lookback.
- Внутренние managed transfers не признаются входящими платежами.
- Неизвестные исходящие операции сохраняются для расследования.

Ограничения:

- finality/reorg model должна пройти отдельный Nile/Sepolia conformance и chaos gate;
- новые адреса подхватываются с периодом до 5 минут, а не мгновенно;
- ordering lifecycle webhook между разными outbox events не гарантируется при
  retry одного из них.

## 8. Как защищён вывод

### Merchant withdrawal

- Адрес заранее внесён Owner в whitelist.
- Добавление адреса требует свежего step-up.
- Создание заявки требует роли Manager и step-up.
- Средства списываются debit-first.
- Risk/policy artifacts подписаны и неизменяемы.
- Approval привязан к конкретному intent digest.
- Admin approval требует object-bound step-up.
- Worker повторно сверяет документ с intent перед подписью.
- Vault повторно сверяет транзакцию с authorization.
- Hot balance резервируется.
- Неопределённый broadcast переводится в repair/held, а не в новый перевод.

### Treasury withdrawal

- Используется отдельный allowlist.
- Источник — processing fee balance.
- Действует maker-checker.
- Применяются policy, approvals, Vault signing и settlement.

### On-chain failure

Если уже отправленная withdrawal-транзакция завершилась on-chain failure,
settlement в одной транзакции восстанавливает managed hot-wallet ledger,
сторнирует debit merchant balance и переводит withdrawal/request в `failed`.
Settlement row lock и детерминированные movement IDs делают exact retry
идемпотентным. Автоматическая новая выплата по старому approval intent остаётся
запрещённой.

## 9. Webhook security и фактическая готовность

Dispatcher реализует хороший outbound SSRF/TLS baseline:

- только HTTPS;
- DNS resolution и connection pinning;
- запрет private/reserved ranges;
- без redirects;
- TLS 1.2+;
- HMAC подпись `timestamp.body`;
- стабильный ID бизнес-события во всех delivery attempts;
- журнал попыток.

Secret показывается только при создании или ротации endpoint; повторное раскрытие невозможно. Эти операции доступны только Owner со step-up. Есть test webhook.

Lifecycle dispatch подключён к `invoice.paid`, `invoice.confirmed`,
`invoice.expired`, `invoice.late_payment`, `withdrawal.confirmed` и `withdrawal.failed`. Событие
записывается в deduplicated outbox в той же транзакции, что и business state;
предусмотрены десять delivery attempts до 72 часов и ручной redelivery.

Мерчант обязан дедуплицировать запросы по стабильному `event.id` /
`Metapus-Event-Id`; `Metapus-Delivery-Id` уникален для каждой попытки. Строгий
ordering разных событий при retry не гарантируется, поэтому receiver должен
сравнивать status/timestamp либо перечитывать актуальный объект через API.

## 10. Ответы на сложные вопросы

### «Что произойдёт, если worker упадёт после подписи?»

Signed transaction хранится в БД. После restart процесс должен продолжить работу с тем же txID. Создавать новую транзакцию до выяснения судьбы старой нельзя.

### «Что произойдёт, если TRON RPC ответил timeout при broadcast?»

Ответ считается неопределённым. Система проверяет существующую транзакцию и может перевести операцию в `held`. Повтор с новым nonce/tx запрещён.

### «Может ли DBA украсть деньги?»

Доступ только к PostgreSQL недостаточен для получения private key. Но DBA может менять документы и authorization payload, поэтому защита опирается на signatures, Vault-side validation, separation of duties и audit. Если DBA одновременно контролирует signing keys/policy keys или Vault token, граница нарушается. Эти полномочия должны принадлежать разным security domains.

### «Может ли разработчик изменить адрес получателя после одобрения?»

Изменение сломает intent digest и проверку worker/Vault. Важно, чтобы production approval/private signing keys были недоступны разработчикам и CI.

### «Почему используется hot wallet?»

Hot wallet нужен для оперативных выплат и активаций. Риск ограничивается:

- минимальным необходимым остатком;
- лимитами policy;
- allowlist;
- reservations;
- Vault signing;
- reconciliation;
- treasury separation.

Для крупных объёмов требуется cold/warm treasury и автоматизированный refill policy, которых текущий контур не заменяет.

### «Что если pool-адрес ранее использовался?»

On-chain состояние переживает сброс локальной БД. Setup выполняет preflight и ставит generation на hold при найденном балансе/истории. Для чистого E2E выбирается новый BIP-44 account index.

### «Что произойдёт, если на адрес USDT-инвойса отправить TRX?»

Token всегда разрешается по immutable паре `(network, contract)`, где пустой
contract означает native asset. Если token события не совпадает с token
активного invoice, payment и проводки не создаются; перевод остаётся предметом
reconciliation, но не может быть ошибочно учтён как USDT.

### «Кто платит за TRX и Energy?»

Активация account, аренда Energy и фактическая network fee учитываются как platform costs. Это позволяет CFO видеть unit economics отдельно от merchant payable и processing revenue.

### «Можно ли доказать полный баланс?»

Да, но не одной цифрой. Нужен отчёт:

```text
on-chain managed assets
vs
merchant liabilities + platform-owned token balances + explained operational deltas
```

Плюс отдельная сверка TRX и provider prepayment. Текущая схема хранит необходимые движения и observations, но управленческий proof-of-reserves/liabilities отчёт должен быть формализован отдельно.

### «Как откатывается blockchain-транзакция?»

Никак. Отменяется или сторнируется только внутренний учёт. Поэтому контроль до подписи важнее обычного post-factum rollback, а неопределённый broadcast всегда требует reconciliation.

### «Можно ли вручную исправить проведённую blockchain-операцию?»

Только системный администратор (`isAdmin` или роль `admin`) с отдельным
permission и fresh MFA step-up может разблокировать финальный
`CryptoNetworkOperation`. `security_admin` этого права не имеет. После
разблокировки разрешены только источник учётного расхода, cost token, сумма и
описание; network, target, address, TX, settlement и finality неизменяемы.

Распроведение, если разрешено lifecycle, добавляет компенсирующие crypto movements и не меняет blockchain или
оперативный статус кошелька. Повторное проведение и пометка на удаление
автоматически блокируют документ. `sys_event_log` хранит факт команды и
исполнителя; отдельной истории значений до/после в MVP нет. Это осознанный
аварийный контур, а не штатный способ коррекции.

### «Что будет при ошибке тарифа?»

Infrastructure error при чтении fee schedule откатывает создание payment и
всех проводок. После восстановления repository тот же chain event можно
обработать с одним согласованным fee snapshot. Отсутствие настроенного тарифа,
в отличие от ошибки чтения, остаётся явной бесплатной конфигурацией.

## 11. Merchant CIDR policies: текущее состояние

### Что уже есть

Для системных `admin` и `security_admin` реализован глобальный allowlist:

```dotenv
ADMIN_SECURITY_MODE=off|warn|enforce
ADMIN_ALLOWED_CIDRS=127.0.0.1/32,::1/128
TRUSTED_PROXIES=
```

Проверка выполняется до bcrypt на login и повторяется middleware на защищённых маршрутах. Решение зависит от корректной настройки `TRUSTED_PROXIES`.

### Merchant-specific enforcement

В tenant DB реализованы нормализованные сущности:

```text
auth_merchant_network_policies
  merchant_id
  surface            portal | api
  api_key_id nullable
  mode               off | warn | enforce
  version
  updated_by/at

auth_merchant_allowed_networks
  policy_id
  network            PostgreSQL CIDR
  label
  active/revoked metadata

auth_merchant_network_policy_audit
  actor/action/decision
  old/new snapshot
  request metadata without credentials
```

Portal и API имеют разные baseline policies. API-key override пересекается с
baseline; любой enforce-deny запрещает запрос. Portal policy не может ссылаться
на API key, а enforce нельзя включить без active CIDR. IPv4-mapped IPv6
нормализуется. Невалидный `TRUSTED_PROXIES` завершает startup ошибкой; forwarding
headers от недоверенного remote address игнорируются.

Policy проверяется на каждом business request. Ошибка read/audit в enforce path
fail-closed. После успешной mutation локальный cache инвалидируется немедленно;
другие процессы перечитывают policy не позднее жёсткого TTL в пять секунд.
Migration также публикует audit notification для внешних listeners.
`/portal/v1/merchants` возвращает
`networkAccess`, но это только UX projection: маршруты всегда проверяют policy
повторно.

### Управление и recovery

Owner управляет Portal/API policy и per-key overrides через portal Security UI.
Mutations требуют durable `Idempotency-Key`, fresh object-bound step-up, причину
и staged `off -> warn -> enforce`. Включение enforce проверяет доступность
текущего IP либо recovery CIDR.

System-admin recovery вынесен из merchant policy enforcement, но остаётся за
admin CIDR/MFA/RBAC, fresh step-up и обязательной причиной. Merchant
`security_admin` не может использовать recovery endpoint. Изменения аудируются
и уведомляют остальных Owners.

### Что ещё нужно доказать до release

Source unit/UI contracts не заменяют full-stack security-negative gate. На
disposable PostgreSQL/backend необходимо доказать Portal/API baseline,
per-key intersection, IPv4/IPv6, cache invalidation не позднее пяти секунд,
read/audit failure, spoofed forwarding headers, self-lockout guard и recovery.
До появления такого evidence candidate остаётся `NOT_RUN`, а launch — `NO-GO`.

Для dashboard IP allowlist лучше делать enterprise-опцией, а не единственным механизмом. У сотрудников могут быть динамические адреса; SSO, phishing-resistant MFA/passkeys, device posture и корпоративный VPN часто дают лучшую безопасность.

## 13. Как это делают Stripe и PayPal

Практика индустрии разделяет human dashboard и machine API.

### Stripe

Stripe позволяет ограничивать secret/restricted API keys конкретными IPv4/CIDR и рекомендует IP restrictions для live-mode keys: [Stripe API keys](https://docs.stripe.com/keys).

Для доступа людей Stripe делает упор на granular roles, обязательную 2FA/passkeys, security history и enterprise SAML SSO/SCIM:

- [Team and roles](https://docs.stripe.com/get-started/account/orgs/team)
- [SAML SSO](https://docs.stripe.com/get-started/account/sso)
- [Account security](https://docs.stripe.com/security)

То есть IP restriction в публичной модели Stripe прежде всего относится к server API key, а dashboard защищается identity controls.

### PayPal

PayPal REST API использует OAuth client ID/secret для получения access token:

- [PayPal REST API](https://developer.paypal.com/api/rest/)
- [REST authentication and requests](https://developer.paypal.com/api/rest/requests/)

Для Business account используются secondary users/privileges, approvals, 2-step verification и passkeys:

- [Manage users](https://www.paypal.com/uk/cshelp/article/how-do-i-manage-users-on-my-business-account-help274)
- [Approvals](https://www.paypal.com/us/cshelp/article/what-are-approvals-and-how-do-they-work-help575)
- [2-step verification](https://www.paypal.com/us/cshelp/article/what-is-2-step-verification-help167)
- [Passkeys](https://www.paypal.com/us/cshelp/article/what-is-a-passkey-and-how-do-i-use-it-to-log-in-to-my-paypal-account-help997)

В проверенной публичной документации основного PayPal Business не описан общий self-service allowlist IP для входа в dashboard. При этом Braintree, продукт PayPal, поддерживает отдельные allowlist для Control Panel и API, включая CIDR/hostnames: [Braintree allowlisting](https://developer.paypal.com/braintree/articles/risk-and-security/allowlisting).

Рекомендуемый для Metapus результат:

- per-API-key CIDR restrictions как базовая server-to-server защита;
- merchant portal CIDR policy как enterprise-опция;
- MFA/passkeys/SSO/roles/session audit как основной human-access control.

## 14. Release gates

### Закрыты и обязательны как regression gates

- per-tenant Transit и tokenless Vault Proxy;
- merchant lifecycle webhook outbox/retry/replay;
- merchant и managed-wallet compensation при on-chain failure;
- fail-closed fee resolution;
- token isolation между blockchain event и invoice;
- dynamic watcher address refresh;
- отдельный watcher/checkpoint для всех активных TRC-20 contracts;
- CNO finality и recovery без duplicate settlement/movements;
- strict-document guard, system-admin boundary и MFA step-up для unlock.

### Обязательные до production/Mainnet

- внешний hardened Vault вместо test-only Compose profile;
- PostgreSQL/Vault backup/restore и key rotation drills;
- P1 alerting, on-call и SLA на held/failed/reconciliation incidents;
- formal finality/reorg policy;
- treasury happy path и maker-checker;
- нагрузочный, concurrency и chaos E2E для duplicate/timeout/restart и
  provider outage;
- proof-of-assets-and-liabilities процедура и утверждённые wallet limits.

### Обязательные, если заявлены в продукте

- merchant IP allowlist и UI;
- proof-of-assets/liabilities dashboard для клиента.

## 15. Итоговая оценка

Архитектура Metapus построена на правильных принципах платёжной системы: separation of duties, immutable intent, Vault-side policy enforcement, idempotency, independent ledgers и reconciliation.

Главный риск не в выбранной архитектуре, а в преждевременном заявлении о
production readiness. Реализованная Vault boundary снимает хранение
authorization key у worker, ограничивает sweep destination и блокирует replay
approval intent. Программные blockers закрыты; после выполнения Mainnet
operational gate контур можно защищать перед техническим и финансовым аудитом
как контролируемую, восстанавливаемую и проверяемую систему.

## 16. Файловая карта

```text
internal/domain/crypto/money_movement_approval.go       — intent, policy и approvals
internal/domain/crypto/money_movement_signer.go         — JWS artifacts
internal/domain/crypto/signing_authorization.go          — authorization contract
internal/infrastructure/crypto_worker/signing_authorization_service.go — authorization lifecycle
internal/infrastructure/crypto/authorization_signer     — Local/Transit control-plane signing
internal/infrastructure/crypto/vault_signer             — family signer adapter
vault-sign-plugins/tron                                 — Vault-side validation и подпись
internal/infrastructure/http/v1/middleware/admin_security.go — admin CIDR/MFA/step-up
internal/domain/strict_document.go                     — защита ручной коррекции
internal/domain/documents/crypto_network_operation/    — сетевой факт и accounting movements
internal/infrastructure/crypto_worker/crypto_network_operation_coordinator.go — atomic activation finality
internal/infrastructure/http/v1/middleware/merchant_portal.go — merchant roles
internal/domain/crypto/webhook.go                       — HTTPS/HMAC/SSRF-safe dispatcher
internal/infrastructure/crypto_worker/reconciliation.go — blockchain/ledger incidents
internal/infrastructure/blockchain/tron/funding/record.go — audited operator funding verification
internal/infrastructure/crypto_worker/wallet_pool_controller.go — bounded async provisioning
db/migrations/00038_money_movement_approval_policy.sql — approval persistence
db/migrations/00041_multichain_crypto_core.sql         — chain-agnostic network/resource policy
db/migrations/00042_ethereum_sepolia_core.sql          — EVM nonce/finality/network holds
db/migrations/00043_crypto_address_reuse_cooldown.sql  — allocation history и inbound inbox
db/migrations/00044_merchant_webhooks_production.sql  — current schema end
```

## Связанные документы

- [crypto-processing-architecture.md](crypto-processing-architecture.md) — фактические процессы и состояния.
- [crypto-environment-setup.md](crypto-environment-setup.md) — bootstrap PostgreSQL, Vault и сервисов.
- [crypto-processing-test-scenarios.md](crypto-processing-test-scenarios.md) — доказательная проверка гарантий и gates.
