# Изоляция кошельков мерчантов

Реализованы owner-aware allocation, выбор коллектора, резерв principal для выплаты, выбор gas wallet и проверка владельца перед подписью/отправкой. Поверх него подключена [проекция AML-риска wallet/token](crypto-aml-risk-projection.md). [Batch planning/capacity и trusted merchant provisioning](crypto-aml-batches.md) подключены; [автоматическое пополнение merchant pools](crypto-aml-merchant-pools.md) подключено, [class-specific вывод и dispatch permits](crypto-aml-outbound.md) подключены. Включённый AML scope закрывает новые отправки своего актива через прежний контракт; новый путь требует точного risk/source plan.

## Состояние и гарантии

- `cat_wallets.custody_merchant_id` — постоянный владелец. Transient lease, освобождение адреса и persistent assignment его не меняют. История текущего `merchant_id` не является основанием для автоматического назначения owner.
- `sys_crypto_custody_policies` включает изоляцию по сети. Отсутствие строки/`enforced=false` сохраняет legacy-поведение для непривязанных кошельков. Кошельки с заданным owner уже фильтруются по владельцу.
- При `enforced=true` аллокатор использует только заранее привязанные адреса нужного мерчанта. Автоматического заимствования общего адреса нет.
- Свип соединяет `merchant_pool` и `liquidity_hot` одного owner. Выплата резервирует principal своего мерчанта; недостаток средств не компенсируется чужими кошельками. Существующий некорректный резерв останавливает выполнение и остаётся видимым.
- JIT funding использует активный `network_fee` нужного мерчанта. В одной сети допустим один активный fee wallet каждого мерчанта и отдельный платформенный fee wallet. Общий fee wallet не является fallback для мерчантского контура. Выбор источника принадлежит custody registry; сетевая политика хранит только лимиты и минимальные остатки. Readiness проверяет наличие и собственный native balance fee source для каждого активного владельца. Для платформенных кошельков при выключенной изоляции выбирается зарегистрированный платформенный `network_fee`.
- Платформенный treasury не расходует merchant principal. Managed destination для merchant withdrawal запрещён, включая адреса с deletion mark при финальной проверке авторизации.
- Проверки выполняются до ресурсных операций, подписи и новой отправки. Это касается и EVM replacement/повторной отправки сохранённых bytes.

## Независимая проверка Vault

Оба плагина поддерживают `merchant_custody.v1`. Registry хранится в Vault отдельно от SQL. Сам факт derivation в tenant не доказывает владельца: требуется отдельная неизменяемая привязка. Worker ACL не имеет права создавать её или менять policy.

Контрольный endpoint привязки:

```text
POST /v1/{tron|evm}/tenants/{tenant_id}/wallets/custody
network: tron_nile | tron_mainnet | 11155111 (соответственно плагину)
address: адрес, ранее выведенный этим tenant в этой сети
merchant_id: canonical UUID владельца, либо пустая строка для платформенного кошелька
role: merchant_pool | liquidity_hot | network_fee | treasury
```

Повтор идентичной привязки допустим. Изменение owner, network, address или role существующей записи запрещено. Привязать произвольный внешний адрес или адрес другого tenant нельзя. EVM-адрес нормализуется плагином; TRON-адрес сохраняет регистр.

Контрольный endpoint включения:

```json
{
  "schema_version": "merchant_custody.v1",
  "revision": 1,
  "enforced_networks": ["tron_nile"]
}
```

Запрос отправляется на `POST /v1/{tron|evm}/tenants/{tenant_id}/config/merchant-custody`. Следующая revision равна предыдущей + 1. Удаление уже включённой сети запрещено. `GET .../config/readiness` публикует capability, schema, revision и список включённых сетей без раскрытия ключей.

Перед новой подписью Vault сверяет merchant из подписанной авторизации с registry source; для внутреннего перевода также сверяет destination и роли. Проверка выполняется и перед возвратом кешированной подписи. При включённой SQL policy backend требует соответствующую включённую policy в Vault: старый плагин или неполная конфигурация останавливают отправку.

## Переключение существующей сети

Доступен отдельный [trusted provisioning request](crypto-aml-batches.md) с Vault binding до SQL publication. Автоматические merchant watermarks подключены через [отдельные политики и control-plane queue](crypto-aml-merchant-pools.md); общий переключатель ещё не реализован. Миграции 00061–00062 сами не включают изоляцию и не назначают legacy owners.

Порядок подготовки согласованного rollout:

1. Остановить новые операции сети. Перед включением SQL policy сеть должна быть выключена в `sys_crypto_network_policies`; миграционный guard блокирует включение на работающей сети.
2. Проверить историю и остатки адресов, зафиксировать доказанного владельца. Общий адрес с principal нескольких мерчантов не получает owner по текущему lease или последнему платежу.
3. Через доверенный control plane привязать проверенные адреса в Vault и SQL; подготовить собственные депозитные, консолидационные и gas wallets мерчантов. Для нового адреса сделать это до выдачи/финансирования.
4. Включить сеть в policy Vault, проверить readiness. Затем включить SQL custody policy и выполнить сверку. Обратное отключение enforcement запрещено в обоих хранилищах.
5. Возобновить новые операции после проверки готовности. Совместное включение старого worker и новых policies не допускается.

Привязку адресов, владельцы которых неизвестны, необходимо отложить. Недостаточная ёмкость собственного пула приводит к отказу аллокации, а не к использованию адреса другого мерчанта. До реализации provisioning для merchant scope автоматический общий pool controller не обеспечивает такую ёмкость.

## Сохранённые операции

Чтение существующей signing authorization не фильтруется по новой policy. Merchant withdrawal восстанавливается через существующий recovery path. Для свипа и treasury matching financially-final receipt позволяет восстановить локальный broadcast/accounting без новой подписи или отправки, даже если source больше не подходит для новой операции. Hash, network и старый merchant snapshot не переписываются.

Signed-задачи с hold выбираются только для чтения chain history; сам claim не даёт разрешения на повторную отправку. JIT funding с известным broadcast также продолжает confirmation независимо от актуального fee source. Резервы сохраняются до доказанного исхода.

## Проверка

Интеграционные тесты создают отдельную БД для каждого сценария через `METAPUS_TEST_ADMIN_DSN` и удаляют её после завершения. Проверены чужой/общий источник при выплате, чужой collector/gas wallet, повтор запроса, неизменяемость owner, изоляция адресов, guard переключения и восстановление исторического signed sweep. В обоих Vault-плагинах проверены пути подписи, включая блокировку cached foreign signature после включения enforcement. Реальные Mainnet acceptance и rollout остаются отдельными этапами.


Схема 83: новый platform TreasuryWithdrawal model 2 сохраняет wallet/revenue reservations после broadcast. Final independent outgoing proof создаёт один expense owner, exact revenue expense и actual network fee/cost атомарно с consumption резервов. При revert principal/revenue освобождаются без storno. Native fee budget учитывает active principal reservations. Существующие treasury model 1 и их история сохраняют прежний механизм; Merchant withdrawal model 2 схемы 84 сохраняет debit-first и active principal reserve до final outgoing proof, с exact per-line AML success/refund. Его механизм описан в [§12](../reference/crypto-custody-writers.md#12-merchant-withdrawal-схемы-84). Детали: [custody writers, §11](../reference/crypto-custody-writers.md#11-treasury-схемы-83).

Новые fee reservations model 2 (схема 86) признают сетевую комиссию через independent fee proof, отдельный owner и атомарный expense/platform-cost/binding/outbox набор. Это применяется также к sweep/treasury/withdrawal и activation; merchant liability не списывается повторно. Legacy fee model 1 сохраняет drain. Подробности: [network fee recognition](../reference/crypto-custody-writers.md#14-независимое-признание-network-fee-схемы-86).

Схема 87 добавляет exact expected-transfer handoff перед payment matching и internal exits. Signed sweep/treasury/withdrawal/AML-line transfer остаётся у исходного settlement basis после истечения lease; nudge не меняет резервов, held state или денег. Неоднозначный или legacy proof не создаёт external receipt. [Pending handoff и ограничения](../reference/crypto-custody-writers.md#15-маршрутизация-ожидаемых-переводов-схемы-87).
