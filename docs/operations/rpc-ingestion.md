# RPC ingestion: настройка и проверка

Тип: Runbook. Область: EVM/TRON ingestion и family-neutral readiness.

Конфигурация может храниться в БД тенанта: [аккаунты интеграций и перенос](integration-accounts.md). Режим `database` сохраняет RPC endpoints без ключей и подхватывает изменения через polling; файловый режим остаётся для ещё не перенесённых сетей.

EVM topology принимает optional `evm` объект рядом с `roles`. Пример находится в
`deployments/crypto/chains.example.json`. Старые конфигурации получают defaults.
Circuit threshold и cooldown читаются из `sys_crypto_network_policies`.

| Настройка | Default | Назначение |
|---|---:|---|
| maxHeadAgeSeconds | 30 | Максимальный возраст latest head; future timestamp более 5 s отклоняется |
| attemptTimeoutMs | 2000 | Admission, HTTP body и decode одной попытки |
| readBudgetMs | 5000 | Общий budget чтения, включая failover |
| logsTimeoutMs | 5000 | Budget одной range-logs попытки |
| maxAttempts | 2 | Максимум разных providers на чтение |
| providerConcurrency | 8 | Общая process-wide capacity endpoint URL |
| backfillConcurrency | 4 | Часть capacity для full block/logs; остальные slots зарезервированы |
| requestsPerSecond | 20 | Общая process-wide частота endpoint URL |
| nativeConcurrency | 4 | Fetch workers одного native watcher |
| prefetchBlocks | 32 | Максимальное окно высот |
| maxResponseBytes | 8388608 | Максимум wire response, включая chunked HTTP |
| maxBufferedBytes | 268435456 | Ограничение окна по максимальному response size |

`MaxBufferedBytes/MaxResponseBytes` дополнительно ограничивает window. Это bound
для принятых ответов и retained snapshots, а не универсальная квота Go heap:
декодирование JSON/RLP и служебные объекты требуют дополнительной памяти.
Провайдеры выбираются по priority при первом чтении, затем сохраняется последний
успешный provider каждой операции. Единственная пробная попытка после cooldown
проверяет именно отказавшую операцию. Header/runtime probe не лечит full block.
429 учитывает Retry-After и короткий jitter; shared admission приостанавливает
запросы к этому endpoint на всех ролях. Ожидание входит в attempt budget, поэтому
чтение может перейти к другому endpoint. Parent cancellation не открывает circuit.
Известный upstream ответ `block range extends beyond current head block`
классифицируется как `head_behind`, допускающий failover: provider может
отставать от head другого endpoint. Прочие invalid params остаются terminal.
Unsupported capability учитывается по методу, а не отключает весь state API.

При нескольких истёкших circuits впереди здоровых/untried endpoints допускается
одна recovery-попытка; выбор между recovering providers циклический. Повторные
отказы первых двух providers не должны постоянно расходовать весь `maxAttempts`
и блокировать третий endpoint. При `maxAttempts=1` recovery чередуется с healthy
provider. Общие deadline и предел числа попыток сохраняются.

Явный ответ Alchemy `eth_getLogs requests with up to a N block range` с кодом
`-32600` (HTTP 400 либо JSON-RPC error при HTTP 200) обучает конкретный endpoint
допустимому диапазону. Чтение ограниченного числового окна повторяется частями,
включая исходные границы и без изменения address/topics; последующие чтения
сразу используют сохранённый лимит. Каждая часть учитывает общую endpoint quota
и throttle, все части ограничены исходным attempt timeout/read budget. Ошибка
любой части отбрасывает уже прочитанный префикс; token watcher не продвигает
checkpoint. Произвольный HTTP 400 или invalid params не вызывают дробление.

Native consumer принимает высоты последовательно. Сравнивает body roots,
height, chain IDs, parent hash, receipt hash/height/tx hash. Все observations
сохраняются через существующий durable inbox; затем checkpoint и block history
атомарно сохраняются с expected previous height/hash и fencing. Ошибка или
перезапуск повторяет незавершённую высоту. Lease renew выполняется независимо
от fetch window. ERC-20 сохраняет range logs с дроблением range при response
limit/timeout и сохраняет checkpoint/history той же атомарной операцией.

## EVM: свежесть, заголовки и восстановление после Amsterdam

EVM использует `go-ethereum v1.17.7`, включая `blockAccessListHash` (EIP-7928)
в RLP/hash заголовка. При каждом header read вычисленный hash сравнивается
с hash в исходном RPC-ответе; неверная высота/hash даёт `invalid_header`.
Это предотвращает незаметное сохранение неправильного fingerprint при новом
неподдерживаемом поле протокола. Для `latest` дополнительно проверяется timestamp:
устаревший или будущий head даёт `stale_head`, учитывается circuit и вызывает
failover. Historical/finalized headers не обязаны иметь свежий timestamp.

Canonical header требует два согласованных ответа при нескольких configured
providers. Чтения выполняются с fan-out до четырёх workers в общем read budget;
задержавшийся первый endpoint не отнимает последовательные попытки у остальных.
Любое наблюдаемое расхождение успешных ответов остаётся ошибкой. Обычный режим
с одним provider поддерживается для development.

Для EVM endpoint можно задать `minRequestIntervalMs` (100–30000 ms; по умолчанию
0). Это общий для pools/tenants данного процесса интервал без burst. Такие
providers используются в canonical quorum как резерв, когда основных
согласованных ответов меньше двух. Например, фактический anonymous limit
OnFinality 10 requests/min требует интервала не менее 6000 ms с запасом.
Учитывайте суммарный трафик других процессов с того же IP. Для capability probe
attempt/logs timeout должен покрывать ожидание интервала и сам RPC; рабочее
чтение с меньшим budget может пропустить занятый резерв и повториться позднее.
Это не снижает требуемый кворум. Для существующего checkpoint требуется канонический
hash поддерживаемого release. Переходный CLI старого client header encoding удалён;
сохранённые данные требуют проверенной подготовки до обновления runtime, см.
[канонические контракты](../reference/crypto-canonical-contracts.md#5-схема-и-операторские-переходы).

После восстановления запустить worker, дождаться catch-up и повторить
`-mode ingestion`, `-mode rpc` с historical sample, а также проверить payment
attribution/finality/posting и дедупликацию при restart. Снижение кворума и
перестановка checkpoint на текущий head не заменяют восстановление.

## TRON и общие компоненты

Для TRON block scanner и evidence guard используйте
[TRON block ingestion runbook](tron-block-ingestion.md). Ниже описан прежний
timestamp/indexer режим и общая transport policy. Его двухминутная задержка
не применяется к `tron:latest` и `tron:solidified`.

`rpcpolicy` содержит operation circuit, process-wide endpoint admission,
Retry-After/jitter и ограничение HTTP response. `watcherlease` содержит независимый
heartbeat с cancel-on-failure и join при остановке. Оба адаптера используют эти
механизмы; chain identity, block validation, confirmations/finality, форматы RPC
и broadcast uncertainty остаются в адаптерах.

TRON topology принимает optional объект `tron` рядом с `roles`:

```json
{
  "attemptTimeoutMs": 5000,
  "readBudgetMs": 12000,
  "maxAttempts": 2,
  "providerConcurrency": 8,
  "backfillConcurrency": 4,
  "requestsPerSecond": 10,
  "maxResponseBytes": 10485760
}
```

Это defaults, а не измеренный production SLO. Admission общий для URL во всех
ролях и tenant pools одного процесса. Для одного URL настройки capacity должны
совпадать; разные worker/monitor процессы имеют самостоятельные квоты.
Bulk — blocks/events/account pages, urgent — head/state/receipt.
Circuit и sticky provider разделены по `(provider, role, operation)`.
Работающий head не лечит events/receipts. После cooldown допускается один
half-open read именно отказавшей операции; cancellation/admission wait и
истёкший fingerprint не считаются отказами provider. 404/405 и auth failures
отключают capability операции до перезагрузки конфигурации.

TRC20 использует документированные `min_timestamp`/`max_timestamp`,
`only_confirmed=true`, URL-encoded fingerprint и неизменные параметры всех
страниц. Fingerprint привязан к выдавшему его provider и окну. Смена provider,
ошибка страницы или повтор cursor возвращают ошибку и запускают replay окна;
cursor продвигается только после durable capture всех страниц. Native timestamp
stream не отбрасывает поздно индексированную транзакцию только из-за старого
block height. Inbox identity устраняет дубли при replay.

Сохранение timestamp window проверяет expected block/timestamp и fencing token
одним SQL UPDATE, после внешних API и capture. `updated_at` обновляется только
при продвижении checkpoint. Unchanged Save, Acquire и heartbeat не освежают
progress. На этапе этой transport-доработки новая миграция не требовалась (историческая schema 60). Текущий release ожидает schema 76; это не инструкция оставить tenant на 60.
Индексатор по-прежнему имеет safety delay 2 минуты и restart lookback 5 минут.
Fingerprint ограничен 16 KiB, окно — одним часом, pagination — 1000 страницами;
исчерпание лимита не продвигает cursor. Для высоконагруженного global contract
нужны меньшие окна/отдельный scanner; текущая схема не обещает EVM latency для TRON.

Read-only monitor проверяет Solidity head и доступность account/events API
активных contracts. Successful empty ответ подтверждает доступность endpoint,
а положительные данные и pagination проверяются отдельным RPC check ниже.

Контракт пагинации: [TRON event API](https://developers.tron.network/reference/get-events-by-contract-address).

## Ingestion preflight

Обычный `crypto-preflight` проверяет балансы и требует остановить worker.
Режим `crypto-preflight -mode ingestion` выполняет только чтения при живом
worker. Он не запускает nonce/startup reconciliation и не занимает финансовую
reconciliation lease. Сеть выбирается `CRYPTO_PREFLIGHT_NETWORK_CODE`;
пустое значение проверяет все активные сети.

Для public-test Compose используйте тот же env/project/три compose файла,
что применялись при запуске стенда:

```sh
docker compose ... --profile operator run --rm --no-deps \
  -e CRYPTO_NETWORK_CODE=ETHEREUM-SEPOLIA \
  -e CRYPTO_PREFLIGHT_NETWORK_CODE=ETHEREUM-SEPOLIA \
  crypto-preflight -mode ingestion
```

JSON содержит head, все expected streams, checkpoint, активность lease,
lag, возраст фактического progress и operation RPC statuses. Exit 0 означает
`ready`; `catching_up` и `degraded` возвращают exit 1. Missing native не скрывается
работающим token watcher. EVM lag включает высоты; TRON использует timestamp
cursor и indexing lag hints. Отсутствующий head/неизвестный lag не равен нулю.
Возраст head ограничен family capability (EVM 30 секунд, TRON минимум 180),
lag tolerance берётся из существующей wallet pool policy. Inactive lease и отсутствие progress 60 секунд при
backlog дают `degraded`. Большой backlog со свежим progress — `catching_up`.
API database readiness остаётся отдельной проверкой. При одновременном чтении
RPC head и DB checkpoint допускается skew в один блок; больший head regression
и устаревший timestamp по-прежнему дают degraded. Проверки canonicality/commit
watcher не используют это operational послабление.

## Короткая проверка возможностей RPC

`crypto-preflight -mode rpc -samples 3 -historical-block 11815160` проверяет
каждый EVM endpoint напрямую: header, полный блок, receipt первой транзакции
непустого блока и logs активных token contracts. Проверка использует три
недавние высоты (head−2 и ниже) и три указанные исторические высоты. Допустимо
1–10 samples; historical block необязателен. Запускать через тот же operator
service и network env, что ingestion preflight, последовательно с другими
operator commands из-за статического IP контейнера.

Ответы проверяются относительно reference block hash/height; полный блок также
проверяется по roots/chain ID. Ошибка endpoint не скрывается успешным fallback.
JSON содержит attempted/successful, категории ошибок и p50/p95 только успешных
чтений. Empty logs допустимы; они не доказывают полноту индексации. Для проверки
положительного события выбирайте historical block с известной token оплатой.
При пустых блоках receipt не запрашивается; непроверенная операция не считается
успешной. Exit 0 требует двух endpoints, успешно выполнивших все четыре операции
по всей выборке. Это короткий capability check, а не нагрузочный тест или
статистически устойчивый SLO benchmark. Между запросами есть пауза 200 ms.

Для TRON проверяются block/receipt API двух независимых источников:

```sh
docker compose ... --profile operator run --rm --no-deps \
  -e CRYPTO_NETWORK_CODE=TRON-NILE \
  -e CRYPTO_PREFLIGHT_NETWORK_CODE=TRON-NILE \
  crypto-preflight -mode rpc -samples 3 -historical-block 71963230 \
  -internal-transfer-tx dc0586ee1dfa586a9ebe8264926a9b905db1f269c517cea5d1274c59138a9675
```

FullNode и Solidity проверяются напрямую: свежий head, recent/historical blocks,
полный набор receipts, согласованный v2 digest и положительный internal TRX sample.
JSON содержит `roleCoverage` и сравнения источников; readiness требует две
независимые именованные группы в каждой роли. Пустые receipts не доказывают
поддержку internal transfers. Account/events indexer и его пагинация удалены.
Подробности: [TRON block ingestion](tron-block-ingestion.md).

## Бюджеты переключения и многошаговой проверки

TRON quorum выполняет одну wire-попытку на выбранный endpoint; повтором владеет
операция выбора другой независимой пары. `maxAttempts` продолжает ограничивать
обычные failover reads, но не умножает ожидание одного узла внутри quorum.
Одна ведущая recovery-попытка чередует истёкшие circuits, оставляя место
здоровому резерву. Семантическое несовпадение данных не обходит проверку через
другого provider.

EVM `ReadIngestionHead` делит один `readBudgetMs` между header и полным блоком.
`VerifyTransfer` также ограничен общим read budget. Перед первым запросом
проверяется, укладывается ли минимальный интервал всех пяти/шести шагов
проверки в оставшийся deadline. Непригодный маршрут возвращает причину
`budget_unavailable`, без расхода его квоты. Например, endpoint с интервалом
30 s не может завершить такую проверку в budget 25 s; он может оставаться
резервом для одиночного чтения. Ошибки сохраняются по каждому provider/шагу,
а не заменяются последней причиной. Подтверждённый mismatch останавливает
проверку и не подменяется ответом третьего источника.

При невозможности выполнить chain-ID + head probe в бюджете low-quota endpoint
проходит только проверку идентичности. Такой probe не закрывает operation
circuits и не доказывает наличие свежего head. `admission_busy` не увеличивает
счётчик отказов upstream.

## Измерения и alerts

Метрики `crypto.evm.rpc.*`: duration, errors/result class, failover, circuit_open,
in_flight, admission_wait, response_bytes. Watcher: lag_blocks, committed_blocks,
prefetch_bytes, checkpoint_rewinds. `created_at` history для текущего checkpoint
отделяет progress от Acquire/heartbeat. Для durable inbox проверяйте также
`reg_crypto_chain_observations`: pending/processing age и needs_review.

Независимый `crypto-ingestion-monitor` читает активные сети, свежий head с
full-block coverage, expected streams/checkpoints/leases и durable inbox каждые
30 секунд. До четырёх сетей проверяются независимо; TRON latest/solidified
head reads также независимы. Адаптеры применяют настроенные RPC budgets без
дополнительного жёсткого предела 12 s. `CRYPTO_INGESTION_MONITOR_TIMEOUT`
ограничивает весь sample: default 75 s, допустимо 5 s–3 min. При настройке
этого значения учитывайте максимальный RPC read budget, DB и recovery probe;
короткий sample может сам обрезать разрешённый failover.
Monitor продолжает проверку при остановленном worker. У процесса нет
custody/signing credentials или подключения к Vault; финансовые записи,
checkpoints и leases он не меняет. Существующие ERP notifications записываются
короткой транзакцией в tenant DB для активных system admins.

| Условие | Уведомление после устойчивого состояния |
|---|---|
| Обязательные streams: измеренный lag > 60 s при свежем progress | warning, 60 s |
| Optional `tron:latest`: diagnostic backlog | warning, 60 s |
| `tron:shadow`: lag > 60 s при продолжающемся progress | warning, 180 s |
| Progress age > 60 s при backlog и свежем head | error, ещё 60 s debounce; для `tron:*` без повторного debounce |
| Missing checkpoint / inactive watcher lease | error, 60 s |
| Fresh head/full-block read недоступен или head устарел | error, 60 s |
| Pending/processing oldest age > 60 s или needs_review > 0 | error, 60 s |
| DB/config read не позволяет выполнить проверку | error, 60 s; доставка после восстановления DB |
| Обязательные streams: все известные проверки в допуске; backlog < 30 s | success, 120 s |
| Inbox/monitor/optional latest восстановлены | success, 60 s |
| `tron:shadow`: lag < 30 s после активного backlog, при свежем head и активной lease | success, 180 s |

Диагностический `catching_up` и метрики сохраняются при отставании stream более
двух блоков. Это состояние само по себе не создаёт уведомление: alert policy
использует отставание по времени. В диапазоне 30–60 s включительно активный
сетевой или shadow backlog остаётся открытым, а таймеры открытия/восстановления прерываются.
Небольшая задержка при продолжающемся прогрессе остаётся видимой в `/status`.
Неизвестный lag, отсутствие свежих данных и разрыв наблюдений не считаются
восстановлением. Остановка checkpoint, RPC coverage, quorum и несовпадение
данных по-прежнему считаются ошибками. Финансовая readiness и dispatch gates
используют прежние строгие правила, независимо от debounce уведомлений.

Backlog notifications содержат измеренные `lagSeconds` и
`progressAgeSeconds`; сообщение различает продолжающуюся обработку и остановку.
При обновлении monitor старый кандидат без измеренного lag не засчитывается
как доказательство превышения нового порога. Существующий активный инцидент
сохраняется и закрывается после устойчивого восстановления; pending event IDs
и дедупликация доставки сохраняются.

Ручная пауза shadow из UI закрывает его инциденты отдельно, с
`resolution=operator_paused`; это информационное уведомление, а не success о
восстановлении. Повторные paused observations не создают новых сообщений.
После resume сохраняются обычные условия длительности нового отставания.
См. [управление теневой проверкой](tron-block-ingestion.md#управление-теневой-проверкой-через-ui).

Первый запуск имеет grace 90 s для уведомлений. Он не меняет финансовую
readiness и не превращает missing stream в ready. Неполные наблюдения не
закрывают активный alert. Разрыв более `max(3×interval, 2×sampleTimeout)` прерывает накопление debounce.
Coverage, block evidence и обязательные streams объединены в один инцидент
на сеть. Смена причины при той же severity обновляет evidence; изменение
severity проходит debounce. Восстановление quorum при большом backlog создаёт
сообщение о продолжающейся обработке с отставанием, а не success всей сети.
Optional shadow/latest и inbox сохраняют отдельные scopes. При обновлении
старые активные scopes объединяются без сброса pending event IDs; старые
таймеры частичного восстановления не доказывают здоровье всей сети. При стандартном периоде обычное уведомление обычно
сохраняется через 60–90 s после обнаружения устойчивого условия. Notification
bell использует существующий polling раз в 60 s; немедленный межпроцессный WS
push и внешние каналы не добавлены.

State/pending event IDs атомарно сохраняются до доставки на Docker volume
`ingestion-monitor-data`, путь `/var/lib/metapus/ingestion-monitor`. Advisory
transaction lock и `ingestionEventId` в существующих notifications исключают
дубликаты при повторе после crash, включая прочитанные/soft-deleted сообщения.
Одновременно volume может владеть только один monitor. Повреждённый state или
другой tenant останавливает запуск; нельзя удалять state как способ retry.

Operational snapshots сохраняются в JSONL: до 7 дней, максимум восемь сегментов
по 8 MiB (64 MiB); state отдельно ограничен 1 MiB. В историю включены безопасный
`rpcErrorCode`, `elapsedMs` и operation snapshots с provider IDs, причинами,
latency и retry time. Транспортные URL/ответы не сохраняются. Это история состояния
ingestion, без tx hashes, адресов, RPC payloads и секретов. Текущий metrics sink
по-прежнему только подтверждает OTLP batches. Общая история RPC time series и
исполнение rules в metrics backend не добавлены: alerts вычисляет monitor.

Дополнительные OTLP gauges `crypto.ingestion.*`: `read_ok`, `rpc_coverage`,
`lag_blocks`, `lag_seconds`, `last_progress_age_seconds`, `lease_active`,
`inbox_pending`, `inbox_processing`, `inbox_needs_review`,
`inbox_oldest_age_seconds`, `alerts_active`. Unknown имеет значение −1, а не 0;
после обработки inbox счётчики возвращаются в 0. Labels — network code и
watcher key. RPC coverage означает успешное текущее full-block чтение через
обычный bounded routing, а не проверку всех endpoints на каждом tick.

Compose public-test запускает monitor автоматически; обычный single-tenant
Compose использует `--profile crypto-monitor`. `TENANT_ID`, tenant DSN и chain
topology должны совпадать с worker. Изменения RPC из БД подхватываются monitor через штатную смену generation.
После изменения topology в файле перезапустите monitor. `CRYPTO_INGESTION_MONITOR_INTERVAL` принимает 5 s–5 min, default 30 s.
Private HTTP port 8092 не опубликован наружу: `/status` — последний snapshot,
`/health/live` — процесс, `/health/ready` — свежая завершённая проверка и отсутствие
недоставленных переходов. Monitor может быть ready и при degraded processing:
финансовый deployment gate — отдельный `-mode ingestion`.

## Настройки через интерфейс сети

В «Управление криптосетями → RPC-подключения → Настроить RPC» доступны:

- таймаут одной попытки и общий бюджет операции с резервными источниками, в мс;
- допустимое отставание, длительность проблемы, порог и длительность восстановления, в секундах;
- в дополнительных настройках: попытки чтения, частота и параллельность запросов,
  параллельность истории, EVM logs timeout/head age, предзагрузка, размер ответа и память;
- минимальный интервал запросов каждого подключения, в мс. Квоты аккаунтов/пулов
  продолжают действовать независимо от сетевого лимита.

Нулевое числовое значение означает автоматический выбор. API возвращает отдельно
сохранённые `settings`, текущие `effectiveSettings` и `defaultSettings`, которые
показываются рядом с автоматическими полями. Открытие формы не материализует
значения по умолчанию в БД. Изменение только порогов уведомлений сохраняет
прежнюю RPC-конфигурацию, включая отсутствующий settings object.

Четыре порога уведомлений по умолчанию: `maxLagSeconds=60`,
`fireAfterSeconds=60`, `recoveryLagSeconds=30`, `recoverAfterSeconds=120`.
Допустимое отставание: 10–3600 с; восстановление: 1 с до строго меньшего
значения; обе длительности: 30–3600 с. Уведомление об отставании открывается
при lag **больше** допустимого, восстановление — при lag **меньше** порога.
Значения в средней полосе прерывают оба таймера и сохраняют активный инцидент.
Пороги действуют для обязательного сетевого backlog; задержки обычных ошибок
и inbox также используют длительности этой сети. Для отдельного shadow backlog
сохранена политика 180/180 с. Необязательный `tron:latest` использует
настраиваемые сетевые пороги lag и времени: диагностический `catching_up`
на 3–9 с сам по себе не открывает уведомление. Его ready-наблюдения также
сохраняют измеренный lag, чтобы соблюдать выбранный порог восстановления. Сигнал `checkpoint stalled` уже содержит минимум
60 с без прогресса; дополнительный debounce равен `max(0, fireAfterSeconds−60)`.
Наблюдения дискретны: переход происходит на первой проверке после накопления
нужного интервала, с сохранением startup grace и правил неизвестного состояния.

Сервер и UI проверяют: бюджет операции ≥ двух таймаутов попытки, logs timeout ≤
бюджета; запросы истории оставляют параллельность для актуальных данных; окно
предзагрузки вмещает загрузчики, а память — их максимальные ответы. Quorum,
контроль данных, ограничения финансовой readiness и режим активации TRON
через эту форму не изменяются.

Сохранение настроек и изменений endpoint выполняется одной транзакцией через
существующий `PATCH /admin/crypto/networks/:networkId/rpc-configuration`.
`expectedRevision`, права `catalog:crypto_rpc_endpoint:update`, step-up,
идемпотентность, FLS/RLS/CEL и аудит сохраняются. В ответе административная
`revision` охватывает и alert policy; `effectiveRevision` относится к RPC.
Индикатор воркера подтверждает только RPC этой наблюдаемой generation.

RPC перечитываются при следующем обращении, с интервалом проверки 10 с.
Текущие операции заканчиваются на закреплённой generation. EVM watcher
закрепляет клиентов и лимиты на одно ограниченное окно обработки, поэтому
новый размер ответа не попадает в старое окно памяти. Новые параметры
предзагрузки применяются к следующему окну. Monitor перечитывает пороги
на каждом tick и сохраняет применённые `alertSettings` в `/status` и JSONL.
При изменении порогов кандидат и таймер восстановления начинают отсчёт
заново; активные инциденты и pending IDs сохраняются.

Для существующих tenant перед запуском новых binaries выполнить штатный
`integration-config -schema-only` сначала без `-apply`, затем с `-apply`.
Он аддитивно добавляет `sys_crypto_rpc_network_settings.ingestion_alert_settings`
с `{}` по умолчанию, не меняя RPC JSON, checkpoint и историю goose. Новый tenant
получает колонку из owning baseline migration. Откат binaries не требует
удаления этой колонки. Frontend, API, worker и monitor выпускаются совместно.

`CRYPTO_INGESTION_MONITOR_INTERVAL` и `CRYPTO_INGESTION_MONITOR_TIMEOUT` остаются
настройками развёртывания. При изменении timeout он должен вмещать RPC budget
плюс чтение БД и recovery probe; штатные 75 с вмещают максимальный RPC budget
60 с. Ограничение памяти истории и минимальная независимость источников
остаются защитными инвариантами.

## Rollout и rollback

1. Сохранить исходники/checksums, текущие image tags, runtime topology и штатный
   согласованный DB snapshot. Не выводить URL secrets и env contents в logs.
2. Измерить historical и свежие полные блоки из контейнерной сети. Выбрать
   primary по результатам; независимый secondary обязателен для production.
3. Собрать worker/app/operator из одного release. Остановить worker, убедиться
   в освобождении leases. Balance preflight при остановленном worker может
   показать существующую необработанную оплату: расследовать, не reset balances.
4. Запустить nativeConcurrency=1, затем 4/window=32. Сохранить checkpoint/history;
   не перепрыгивать backlog и не создавать payment вручную.
5. Проверить durable inbox, payment attribution, finality/posting и deduplication.
   Пройти read-only ingestion preflight после catch-up, проверить restart/lease.
6. При rollback вернуть topology и image tags, перезапустить app/worker.
   Checkpoint/history format совместим: миграция не требуется. Снимок БД
   восстанавливать только при самостоятельной необходимости восстановления
   данных, а не для отката binaries.

При rollout мониторинга собрать app/worker/operator/monitor из одного release,
запустить `crypto-ingestion-monitor` с `--no-deps`, затем обновить app/worker и
повторить ingestion/RPC checks. Проверить history volume, delivery, отсутствие
дубликатов и восстановление после restart. Для базового мониторинга миграция не нужна; настраиваемые пороги требуют аддитивного schema upgrade, описанного выше.
При откате этого этапа остановить monitor, вернуть app/worker/operator images
и сохранённый runtime env. Volume со state/history сохранять; DB rollback и
удаление notifications не требуются.

Реализация базового этапа не включает WS, JSON-RPC batch или отдельный fresh-head
stream с собственной persisted coverage.
