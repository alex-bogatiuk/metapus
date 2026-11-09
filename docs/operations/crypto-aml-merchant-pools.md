# AML: автоматическое пополнение merchant pools

Миграция 00069 добавляет opt-in политики адресной ёмкости, durable intents, отдельный control-plane consumer и открытие подготовленных collector batches. Она не включает signing/broadcast нового AML executor и не выделяет средства на gas.

## Политика и конечный бюджет

`ConfigureMerchantPoolPolicy` сохраняет версионируемую политику с immutable owner/network/role. Для deposit и fee wallets scope сетевой; collector имеет дополнительный token и один low/medium class. Изменение scope и удаление политики запрещены. Каждая версия watermarks/enabled/limits/actor сохраняется в неизменяемой истории.

Настройки:

- `lowWatermark` — порог запуска пополнения.
- `targetReady` — желаемое число готовых адресов или открытых collector batches.
- `maxTotal` — конечный lifetime бюджет адресов этого scope, включая занятые, retired и закрытые batches. Ротация не сбрасывает его. Увеличение — явное изменение версии политики.
- `maxProvisionBatch` — ограничение числа новых intents за один проход, от 1 до 50.

Для fee role target равен одному активному адресу; второй активный fee wallet не создаётся. Если старый адрес ещё существует и ожидает активации/preflight, новая попытка пополнения адресов не решает отсутствие gas. Retired fee wallet сохраняет историю, а замена возможна только в рамках оставшегося lifetime бюджета.

Принятие включённой политики требует enrolled database/Vault namespace, enforced custody, готовый HD cursor и включённую network wallet-pool policy. Collector требует AML scope и batch policy; deposit требует хотя бы один enabled AML asset scope. Runtime учитывает network holds, watcher lag, review flags и активность сети. Обычный shared warm-pool в enforced сетях остаётся выключенным.

## Подсчёт и очередь

`fn_crypto_aml_pool_counts` различает `ready`, `pending`, `queued`, `total` и `started`:

- Ready deposit address свободен, active, transient, вне cooldown и без holds/reconciliation incidents. Ready collector — открытая партия нужного класса с ёмкостью и актуальным покрытием inventory. Fee ready означает готовность адреса, не положительный gas balance.
- Pending включает незавершённый provisioning и опубликованные адреса, ожидающие нормального первичного preflight/активации/baseline. Просроченный risk snapshot открытой партии также не является поводом создавать новый адрес при каждом цикле.
- Queued — неизменяемые заявки, для которых HD path ещё не выделен.
- Total учитывает lifetime scope budget; изменение role или retirement опубликованного автоматического адреса не возвращают лимит.

При `ready <= lowWatermark` планируется минимум из дефицита `targetReady - ready - pending - queued`, свободного scope budget, `maxProvisionBatch` и свободного network budget. Общий network budget включает manual provisioning, старые незавершённые shared jobs и очереди всех мерчантов. Преобразование queued intent в HD reservation не учитывает его второй раз.

Планировщики сериализуются network custody lock, затем берут routing scopes в фиксированном порядке и нужную policy. SQL guards повторяют cap/identity checks. Конкурентные циклы не создают двойной дефицит. Pending preflight и недоступный RPC не приводят к бесконечной генерации адресов.

`CancelMerchantPoolIntent` допускает аудитируемую отмену только до появления derivation reservation. После неё path сохраняется, включая потерянный ответ Vault. Отключение политики останавливает новые claim/open операции, сохраняет queued и уже начатую историю. Уже выполнявшийся внешний enrollment может завершить SQL publication. Уменьшение target/cap не создаёт дополнительные starts: старые ещё не начатые intents ждут доступной ёмкости либо явной отмены. Отмена intent не выключает саму политику.

## Доверенный consumer

`cmd/aml-pool-controller` использует отдельный Vault Proxy с enrollment ACL. Он не создаёт binding базы сам и сверяет сохранённый namespace, имя базы и schema version. Обычный CryptoProcessor только планирует локальные intents, снимает доказанные baseline holds и открывает подготовленные batches. Его Vault ACL не расширяется.

Для настройки одной политики используется локальный JSON manifest; неизвестные поля отклоняются:

```json
{
  "id": "11111111-1111-4111-8111-111111111111",
  "merchantId": "22222222-2222-4222-8222-222222222222",
  "networkId": "33333333-3333-4333-8333-333333333333",
  "role": "merchant_pool",
  "enabled": true,
  "automaticBaseline": true,
  "expectedVersion": 0,
  "lowWatermark": 1,
  "targetReady": 3,
  "maxTotal": 30,
  "maxProvisionBatch": 2,
  "actor": "control-plane operator"
}
```

Это placeholders: используются реальные IDs подготовленного контура. Для collector добавляются `tokenId` и `riskClass`, role равен `liquidity_hot`. При обновлении `expectedVersion` равен текущей версии. Применение конфигурации не выполняет Vault calls:

```sh
go run ./cmd/aml-pool-controller -tenant-id '<enrolled tenant UUID>' -configure '<policy.json>'
```

Нужен `AML_CONTROL_DATABASE_URL`; для consumer дополнительно `AML_CONTROL_VAULT_ADDR`, указывающий на отдельный control-plane proxy:

```sh
go run ./cmd/aml-pool-controller -tenant-id '<enrolled tenant UUID>' -once
go run ./cmd/aml-pool-controller -tenant-id '<enrolled tenant UUID>'
```

`-once` выполняет ограниченный цикл и печатает счётчики. Постоянный процесс повторяет его каждые 5 секунд и останавливается по interrupt/SIGTERM. Планирование, baseline release и opening ограничены десятью операциями за цикл; provisioning — десятью claim, с остановкой серии после трёх последовательных ошибок. Внешние calls идут после коммита lease. Потерянный ACK повторяет тот же request/path; ошибка даёт durable backoff 30 секунд, а другие заявки продолжают обрабатываться.

## Preflight до intake

Автоматически созданные deposit и collector wallets получают отдельный `aml_baseline_pending`. Обычная сверка не снимает его. При `automaticBaseline=true` processor сам получает пустые baselines по двум независимым RPC после подтверждения watcher coverage (миграция 00070). При выключенной опции нужны независимо зарегистрированные пустые baselines по всем enabled активам deposit scope либо по token и native gas scopes collector; проверяются component inventory и chain/physical coverage. Существующие operating native funds не считаются merchant principal.

`ReleaseNextMerchantPoolBaselineHold` повторно проверяет все текущие scope baselines под блокировками и снимает только baseline holds. Reconciliation/activation holds и high-risk holds сохраняются. Сверка с нулевым SQL balance, сам факт derivation, истечение времени и достижение возраста batch не создают baseline. Отдельный loop исключает потерянное пробуждение при конкурентной регистрации разных baselines.

Включение нового asset scope снова удерживает автоматические deposit addresses без соответствующего baseline. Такой адрес не выдаётся новому invoice; существующие поступления продолжают наблюдаться и учитываться. High-risk вклад также ставит отдельный hold на повторную выдачу автоматического deposit address. Риск не понижается сменой wallet status, и финансовый credit не исчезает. Эти quarantined адреса не маскируют дефицит готовой ёмкости; создание замены ограничено обоими lifetime budgets.

`OpenNextReadyMerchantPoolBatch` открывает партию один раз, используя wallet intent ID как стабильный batch ID, текущие batch policy/scope versions и уже доказанный пустой baseline. Новые деньги до открытия, inactive account, hold или неподходящий inventory не проходят guard. Ошибка opening откатывается до savepoint и получает backoff. После закрытия партии её адрес сохраняется; планировщик готовит следующий собственный collector, если бюджет допускает.

## Границы

Независимые checkpoints/baselines и resource-aware activation автоматизированы при явном opt-in; старые policies сохраняют `automaticBaseline=false`. Подробности, fresh permits и выводы по классам описаны в [outbound runbook](crypto-aml-outbound.md). Административный UI и production rollout остаются отдельными этапами. Рабочие DB/Vault в рамках разработки не меняются, consumer с настоящим Proxy не запускается. Проверки используют isolated PostgreSQL и synthetic enrollment, без подписей, broadcast и paid AML calls. Откат с policy/intent history запрещён.

## Управляющие команды партий и повтор исходной проверки

Контроллер поддерживает `AML_CONTROL_DATABASE_URL_FILE` для tenant DSN. `-configure-batch <manifest>` сохраняет одну batch policy с CAS expectedVersion, exact `maxGross` в minor units, `maxAgeSeconds` и actor; сеть должна быть приостановлена. Этот режим не обращается к Vault. Для runtime есть отдельная Docker target `aml-pool-controller` с nonroot user; provisioning proxy и его ACL изолируются от обычного worker.

Исчерпавшийся из-за RPC preflight можно расследовать и затем повторить командой:

```sh
./aml-pool-controller -tenant-id '<tenant UUID>' -retry-preflight '<job UUID>' \
  -actor '<operator>' -reason '<что исправлено и почему повтор допустим>'
```

Команда даёт один новый конечный цикл 120 чтений и сохраняет предыдущий счётчик/причину в журнале. Она не снимает holds, не изменяет observations/baselines, не принимает nonzero evidence и не повторяет уже завершённую задачу. При обнаруженном inventory нужен отдельный разбор средств, а не повтор пустого baseline.

После sweep evaluation worker теперь вызывает `PlanNextAMLBatchSweep`: он резервирует конкретную собственную партию и source revisions, после чего обычный executor получает зарегистрированную AML-попытку. Выбор класса и capacity выполняется под scope lock, concurrent workers не создают дубликаты. Legacy outbound не обслуживает enabled AML scopes.
