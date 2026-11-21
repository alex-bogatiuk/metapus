# TRON: быстрый observer и последовательное подтверждение

Тип: Runbook. Схема tenant БД: 76. TRON runtime использует единственный канонический декодер.
Сохраняемый формат evidence имеет версию 2; `sys_crypto_block_ingestion.decoder_version`
и fresh DDL фиксируют это значение. Версия не выбирает алгоритм.

## Финансовые границы

`tron:latest` читает FullNode и сохраняет предварительные входящие переводы.
`tron:solidified` читает строго следующую высоту у двух Solidity источников и
сверяет весь блок и полный набор receipts в канонической проекции декодера. Только согласованное доказательство
конкретного перевода разрешает зачисление. Отсутствие кворума означает ожидание;
обычный сетевой сбой не расходует бизнес-лимит ошибок inbox.

Поддержаны прямой TRX, положительный исходный `call_value`, внутренний TRX и
TRC20 `Transfer` из любых вызывающих контрактов. Эмитент TRC20 должен находиться
в реестре поддерживаемых активов. TRC10, rejected traces и нулевые значения
не становятся платежами. Неизвестный execution status останавливает чтение.
Допустимый native receipt без `receipt.result` проверяется через `contractRet`.

Идентичность документа: `network_id + tx_hash + event_kind + event_index`.
Для `internal_value` v2 логическая идентичность — `network_id + tx_hash +
trace_hash + call_value_index`. Canonical event index вычисляется до фильтрации
нулевых/rejected/TRC10 значений, после исключения проверенных неплатёжных notes.
Raw trace index относится к первому источнику proof и не входит в economics hash.

| event_kind | event_index |
|---|---|
| native_value | Позиция исходного контракта транзакции |
| contract_log | Позиция лога в полном массиве receipt logs |
| internal_value | Сквозная позиция всех callValueInfo до фильтрации |

Для внутренних переводов сохраняются trace hash, позиция trace и позиция
callValueInfo. `logIndex` Merchant API сохраняет совместимость и обозначает
индекс события; `eventKind` различает три вида. Суммы остаются целыми minor units.

Позднее provisional событие не понижает уже подтверждённый платёж. Для
проведённого TRON-платежа согласованное исчезновение сохраняет absence proof,
security incident и network hold; автоматической компенсации, unpost или
перезаписи ledger нет. Финансовая корректировка требует существующего
операторского процесса. EVM сохраняет свой текущий механизм.

## Режимы и независимость источников

В `networks.<code>.tron` задаётся `ingestionMode` (по умолчанию `hybrid`):

| Режим | Действие |
|---|---|
| shadow | Сравнение блоков; audit observations сразу processed, proofs не создаются |
| solidified | Последовательный финансовый scanner без latest observer |
| hybrid | Solidified scanner и latest observer |
| paused | Финансовая обработка ожидает, checkpoints/backlog сохраняются |

Timestamp watchers, account/events pagination и режим `indexer` удалены.
`legacyIndexer` больше не принимается в файле конфигурации. Старые checkpoints,
observations и proofs остаются в БД как история. Поддержка чтения v1 evidence
сохранена; активный TRON bootstrap требует decoder policy v2.

Каждый endpoint FullNode/Solidity имеет `trustGroup`. Одинаковый backend под
разными URL/именами относится к одной группе. У FullNode и Solidity одного
источника группа должна совпадать. Назначать разные группы можно только после
подтверждения независимого управления backend. Разные URL такого подтверждения
не дают. Пример для **уже проверенных независимых** источников:

```json
{
  "roles": {
    "fullnode": [
      {"providerId":"source-a","trustGroup":"source-a","url":"https://source-a.example"},
      {"providerId":"source-b","trustGroup":"source-b","url":"https://source-b.example"}
    ],
    "solidity": [
      {"providerId":"source-a","trustGroup":"source-a","url":"https://source-a.example"},
      {"providerId":"source-b","trustGroup":"source-b","url":"https://source-b.example"}
    ]
  },
  "tron": {"ingestionMode":"hybrid"}
}
```

В shadow допускаются неназванные группы для проверки форматов и совпадения
данных, без финансового доверия. RPC CLI при этом честно возвращает
`ready=false` для независимого покрытия. Один доступный RPC позволяет latest
обнаружение; финансовая проверка продолжает ждать второго.

## Чтение, replay и адреса

Polling: 2 секунды у согласованного head. Solidified prefetch у head — четыре
высоты; при отставании больше четырёх блоков окно растёт до
`tron.solidifiedCatchUpBlocks` (по умолчанию 16, допустимо 4–64), и следующее
окно читается без паузы polling. Fetch workers — `tron.backfillConcurrency`
(по умолчанию 4); commit последовательный. Окно четырёх высот раз в 2 секунды
коммитило медленнее, чем TRON производит блоки, и после простоя не догоняло
сеть. Источники закреплены на окно, согласованные
heads читаются один раз на окно. Уже согласованные данные сохраняются в том же
ограниченном prefetch до успешного DB commit: временный пропуск более ранней
высоты не вызывает повторного чтения остальных блоков окна. После новой lease
или рестарта память очищается. Подтверждения используют head исходного чтения
и при необходимости обновляются verifier. Данные блока и receipts каждого
источника всегда читаются отдельно. Пропуск, дубль, посторонний receipt, неверная высота,
HTTP 200 Error и некорректное исполнение не продвигают checkpoint.

В режиме догоняния окно может чередовать пары источников по высотам. После
согласования основной парой solidified и latest heads к окну присоединяются до
двух дополнительных независимых источников. У каждого своя группа доверия,
provider и backend URL, нет статуса lagging, открытых head/block circuits и
unsupported. Источник присоединяется, только если его solidity endpoint вернул
тот же блок на согласованной solidified высоте, а full-node endpoint той же
группы — тот же блок на latest высоте. Высоту `h` читает пара `h mod N` из всех
N пар участников окна. Каждый блок по-прежнему сравнивают два независимых
источника, и у evidence окна один общий head. При трёх участниках каждый читает
две трети высот, при четырёх — половину. Workers окна —
`backfillConcurrency × участники / 2`, поэтому на источник приходится прежняя
параллельность. Источник, который не ответил или вернул другой блок, в окно не
входит; расхождение пишется в журнал, evidence от него не используется. Ошибка
участника при чтении блока, как и прежде, исключает его группу, и эта высота
читается в новом окне. Shadow, tip-режим и отдельный `ReadAgreedBlock`
используют одну пару.

Провайдеры должны возвращать совместимый полный профиль платёжных внутренних
операций. V2 исключает проверенные неплатёжные resource/vote notes, поэтому
различие `vm.saveFeaturedInternalTx` само по себе не блокирует Chainstack.
Одинаковое число receipts не доказывает полноту traces; пропуск обычного transfer
или неизвестная семантика блокируют проверку. Расхождение не обходится третьим
источником или сбросом checkpoint.

Основной путь — `gettransactioninfobyblocknum`. При unsupported/oversize
receipts читаются по transaction IDs с теми же admission/circuit/response
лимитами. RPC выполняется до DB-транзакции. Commit проверяет live lease,
owner, fencing token, ожидаемые height/hash и parent hash; observations,
proofs, block history и checkpoint фиксируются атомарно.

Latest перечитывает окно 128 блоков после рестарта, новые высоты имеют
приоритет. Неполный блок повторяется через пять секунд и не останавливает
чтение новых высот. Watermark latest не означает финансовую полноту. При outage
длиннее окна полноту восстанавливает solidified scanner.

Реестр `sys_crypto_watch_targets` сохраняет происхождение и начальную высоту.
Регистрация managed адреса и активного TRC20 происходит DB trigger до commit
выдачи клиенту. Регистрация и block commit блокируют одну запись сети, matcher
читает актуальный реестр после RPC. Истечение invoice и деактивация/удаление
постоянного кошелька не удаляют выданный адрес из наблюдения.

## Каноническая нормализация receipts

Канонический декодер одинаково для всех провайдеров исключает только проверенные операции
ресурсов/голосования из платёжной проекции. Список и допустимая форма полей
зафиксированы в [protocol audit](../../internal/configuration/crypto/infrastructure/blockchain/tron/testdata/receipt_profiles/protocol-audit.md).
Финансовый `withdrawExpireUnfreezeWhileSuiciding` остаётся обязательным.
Неизвестный note, некорректная форма, отсутствие обычного transfer или
расхождение обязательных данных останавливают проверку; третий источник
не используется для обхода семантической ошибки.

`freezeBalanceV2ForTronPower` и `unfreezeBalanceV2ForTronPower` проверяются по тем же правилам
собственных ресурсов, что Energy/Bandwidth. Внутренняя отклонённая попытка
может присутствовать при успешном внешнем вызове. `rejected=true` сохраняет
проверку note и формы. При обновлении согласуйте версию runtime consumers и
продолжайте сканирование от сохранённого checkpoint.

Proof v2 содержит общую версию, canonical digest, отдельный `sourceDigest`
каждого источника и типизированные счётчики исключений. Старые proofs v1
сохраняются. Native/TRC20 economics hash совместим с v1; internal v2 дополнительно
привязан к trace hash/callValueInfo index.

После обновления проверить прежнюю пару RPC, затем новые provider bindings. До
финансовой активации использовать shadow. Проверки ротации и исчерпания аккаунтов
выполняются на изолированном стенде. Для проверки evidence:

```sh
crypto-preflight --mode rpc --samples 2 --historical-block 71963230 \
  --internal-transfer-tx dc0586ee1dfa586a9ebe8264926a9b905db1f269c517cea5d1274c59138a9675
```

Preflight сравнивает digests независимых источников на одних высотах для обеих
ролей и требует положительный внутренний TRX sample у готовых провайдеров.
`positive_sample_missing` означает, что capability не подтверждена выборкой;
одинаковых пустых массивов недостаточно. Отказ резервного RPC отражается в его
операциях; readiness требует две доступные независимые группы на каждую роль.
Реальные расхождения или неизвестная семантика не скрываются третьим RPC.

Первичный откат — отключить Chainstack bindings, сохранив v2 и прежнюю пару.
Возврат v1/binary требует отдельной проверки отсутствия financial internal v2 и
несовместимых pending payloads, остановки writers и согласованной смены версии.
Не сбрасывать checkpoint и не восстанавливать старую полную БД поверх ledger.

## Переключение

1. Сохранить tenant backup и проверить восстановление. Подготовить API, worker,
   monitor и операторы одного release с decoder v2.
2. Для существующего shadow с legacy сначала проверить переход на свежей копии
   tenant БД. Перепроверить существующие платежи независимым v2 verifier без
   изменения документов и ledger.
3. Остановить worker и дождаться освобождения всех сетевых leases. Сохранить
   timestamp курсоры `native_trx` и `trc20:*`; отсутствие курсора или устаревшая
   история требуют отдельного разбора.
4. Выбрать согласованную границу H с timestamp не позже самого раннего legacy
   курсора, с небольшим перекрытием. Проверить полный блок и receipts двумя
   независимыми группами. Выбор последнего solidified блока может пропустить
   непрочитанную историю старого индексатора и потому не подходит для перехода.
5. Одной короткой транзакцией активировать hybrid, сохранить H/hash, создать
   `tron:solidified`/`tron:latest`, отключить indexer bindings через каталог,
   удалить `legacyIndexer` из настроек и записать аудит. RPC выполняется до tx;
   leases, settings version, старые курсоры и платежи проверяются повторно.
6. Запустить новый runtime, проверить продвижение обоих потоков и отсутствие
   legacy запросов. Scanner начинает с H+1; перекрытие проходит существующую
   защиту от повторного финансового эффекта. Исторические checkpoints сохраняются.

Для текущего стенда подготовлена проверяемая операторская команда и артефакты:
архивный tron-legacy-retirement-20261021 (`db/backup/tron-legacy-retirement-20261021/REPORT.md`, вне текущего checkout).
Первый запуск новой сети сразу использует block v2; legacy переход нужен только
при обновлении существующего старого runtime.

Граница H после активации неизменна. Полная историческая сверка до H не входит
в этот rollout. Откат после активации: hybrid → solidified либо paused; нельзя
возвращать indexer/shadow или бинарник без internal_value/evidence guard.

## Диагностика и alerts

```sh
docker compose ... run --rm --no-deps crypto-preflight -mode ingestion
docker compose ... run --rm --no-deps crypto-preflight -mode rpc -samples 2 -historical-block 71963230 -internal-transfer-tx dc0586ee1dfa586a9ebe8264926a9b905db1f269c517cea5d1274c59138a9675
docker compose ... run --rm --no-deps crypto-preflight -mode evidence -tx <64-hex-transaction-hash>
```

Ingestion JSON показывает отдельные heads, optional/required streams, lag,
progress, cutover H и last error. Evidence показывает identity, статус, posting,
confirmation policy, verification ID, sources/digest и причину ожидания.
Latest/shadow optional: их отказ не маскирует financial readiness solidified.
Неизвестный lag обозначается −1. Readiness TRON требует FullNode и Solidity.

Durable monitor сохраняет alerts и доставку в существующее ERP хранилище/outbox.
Startup grace: 90 секунд; отсутствие кворума или progress при продвижении head
дольше 60 секунд вызывает alert. Потоки latest/solidified измеряются отдельно.

Метрики `crypto.ingestion.*`: quorum_available, lag_blocks, lag_seconds,
last_progress_age_seconds, lease_active, inbox_pending, inbox_processing,
inbox_needs_review, inbox_oldest_age_seconds, alerts_active. Дополнительно
`crypto.tron.block_commits`, `decoded_events`, `read_failures` с error.category
и `poll_seconds`. Скорость backlog оценивается по росту checkpoint и block_commits
относительно роста соответствующего head. Ошибки incomplete_receipts,
provider_disagreement и quorum_unavailable различаются.

## Приёмка и предел гарантии

На прогретом стенде измерить p95 обнаружения ≤5 секунд от доступности блока и
receipt, checkout ≤10 секунд, зачисления ≤10 секунд после согласованных
solidified данных и confirmation policy. Сетевую solidification задержку
измерять отдельно. Нужны fault проверки одного RPC/429/длительного outage,
восстановление быстрее роста цепочки и двухчасовой active прогон без пропусков
или повторных финансовых эффектов. Shadow прогон этих измерений не заменяет.

Гарантия ограничена поддерживаемыми переводами с H+1, сохранностью БД и
eventual availability корректных полных данных двух независимых RPC. Источники
обязаны сохранять internal traces. Предварительное обнаружение не разрешает
выдачу товара и не увеличивает доступный баланс.

Семантика сети: [подтверждения TRON](https://developers.tron.network/docs/confirmation-semantics),
[receipts целого solidified блока](https://developers.tron.network/reference/gettransactioninfobyblocknum-1),
[платёжная интеграция и internal traces](https://developers.tron.network/docs/exchangewallet-integrate-with-the-tron-network).

## Управление теневой проверкой через UI

В «Администрирование → Криптовалютные сети», `/admin/crypto-networks`, карточка
TRON содержит блок «Теневая проверка блоков». Он показывает фактический статус,
проверенную высоту и время последнего прогресса. Команды «Приостановить проверку»
и «Возобновить проверку» доступны системному администратору с
`crypto_network:update` только для настроенного `shadow` без `activated_at`.
Нужна причина из 3–500 символов; автор, причина и версии записываются в
`crypto.ingestion.shadow_control_changed` той же транзакцией.

Панель не переводит сеть между финансовыми режимами. После финансовой активации
отображается объяснение ограничения; сервер также запрещает команду, даже если
она подготовлена до активации или отправлена напрямую. `security_admin` и
оператор с одним разрешением update не получают этот контроль.

`GET /api/v1/admin/crypto/networks/:networkId/shadow` возвращает состояние.
`POST` принимает `{version, paused, reason}` с `Idempotency-Key` и существующей
защитой чувствительных действий. HTTP 202 подтверждает принятие команды;
«Приостанавливается»/«Запускается» сменяются фактическим состоянием по lease.
Версия защищает от конкурентных действий; при 409 клиент перечитывает состояние.

Пауза хранится отдельно в `sys_crypto_block_ingestion.shadow_paused`, версия —
`shadow_control_version`. `ingestionMode`, decoder и финансовая граница не
меняются. Worker замечает флаг между проходами, освобождает lease и остаётся
в управляемом ожидании. Resume заново получает lease и продолжает с сохранённого
checkpoint; накопленная история не пропускается. Команда и `CaptureBlock`
блокируют одну строку: после commit паузы поздний shadow capture отклоняется.
Контейнеры для последующих команд UI не перезапускаются.

Ingestion monitor показывает явную паузу, не читает блоки и не запускает recovery
probe для приостановленной проверки. Открытые shadow-инциденты закрываются с
`resolution=operator_paused`, информационным сообщением о ручной остановке,
без утверждения о восстановлении. Inbox и финансовые проверки сохраняются.

Fresh schema уже содержит поля shadow control. Порядок подготовки сохраняемого
стенда и удалённые переходные CLI описаны в [канонических контрактах](../reference/crypto-canonical-contracts.md#5-схема-и-операторские-переходы).

## Перенос точки неактивированного shadow

Перенос точки неактивированного shadow требует явного решения оператора и
независимой проверки целевого блока и receipts. Зафиксируйте в аудите
пропущенный интервал. Финансовая граница сохраняется, каждый последующий блок
проходит обычную проверку. При расхождении источников или активной ошибке
ожидайте результата диагностики.

## Отставание и восстановление RPC

Рабочая пара исключает endpoints со статусом lagging/invalid. Каждое чтение head
проверяет время блока: FullNode не старше 30 с, Solidity не старше 90 с, будущее
не более 15 с. Значительный откат (>20 блоков относительно наблюдавшейся высоты)
немедленно помещает endpoint в lag quarantine. Небольшие колебания sampled tips
допустимы, включая завершение параллельных запросов в другом порядке.

При большом разбросе высот с формально свежими timestamps дополнительный
независимый источник должен подтвердить верхний диапазон на общей высоте/hash,
прежде чем исключается нижний источник. Один завышенный head не выключает
исправное большинство. Периодический Probe также использует две независимые
группы для относительного порога >20, а не одиночный максимальный head.

Подтверждённое отставание вызывает выбор новой независимой пары и повторное
чтение всего блока/receipts в общем read budget. Timeout/429/5xx и недоступность
account pool сохраняют прежние operation circuits и backoff. Несовпадение полных
данных, неизвестные traces и decoder conflict не разрешают подбор другой пары.
Недостающий receipt сохраняет VerificationPending. Без двух готовых независимых
групп финансовый checkpoint ожидает без пропуска блока.

Возврат lagging endpoint требует двух здоровых наблюдений с интервалом не менее
5 с и ростом head, затем успешного полного чтения блока и receipts той же роли.
Неуспех сбрасывает восстановление. Один успешный head не закрывает другие
operation circuits. Все проверки используют общие admission/quota limits;
финансовые proofs по-прежнему выдаются только после сравнения двух полных источников.
