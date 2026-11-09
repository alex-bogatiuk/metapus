# AML: baseline, dispatch и выводы по классам

Миграции 00070–00073 добавляют outbound-контур; актуальная schema version — 84 с отдельным [testnet AML emulator](crypto-aml-testnet-emulator.md). Контур реализован для зарегистрированных token sweeps, собственного native operating funding, TRON activation и выводов по risk-классам. Разработка и проверки используют одноразовые PostgreSQL базы, synthetic Vault enrollment и RPC/AML fixtures. Рабочие DB/Vault, платные AML calls и реальные chain broadcasts не выполнялись.

## Автоматический baseline и activation

В merchant pool policy включается `automaticBaseline=true` через существующий control-plane manifest. Значение по умолчанию — false, включая уже созданные policies. Provisioning по-прежнему выполняет отдельный доверенный consumer с enrollment базы и адреса в owner registry обоих signer plugins. Обычный worker не получает права назначать owner.

Processor создаёт leased preflight jobs только для опубликованных автоматических адресов и enabled scopes. Два RPC из разных trust groups должны подтвердить один пустой финальный checkpoint: TRON читает solidified balance между одинаковыми solidified headers; EVM читает native/ERC20 balance на конкретном finalized block с повторной проверкой его canonical hash. Single-provider fallback отсутствует. Чужой asset/address, устаревший lease, изменение policy/scope или разные ответы не создают baseline.

Baseline требует доказанного начала наблюдения адреса. Для TRON используется watch target; для EVM каждый native/token watcher сохраняет собственную immutable границу coverage после обновления списка адресов. SQL принимает её только от текущего владельца живого watcher lease с точным fencing token; отставшая реплика не подтверждает coverage. Все merchant-owned адреса продолжают наблюдаться после деактивации или retirement. Она находится после уже полученного окна блоков/logs. Finalized checkpoint должен достичь этой границы; задержка финальности не подменяется latest balance. Пустая SQL сверка сама по себе не доказывает checkpoint.

Jobs повторяются с backoff 30 секунд и пределом 120 попыток. Nonzero balance или обнаруженная история inventory переводят задачу в `needs_attention` и удерживают адрес. Такие средства не объявляются чистыми, не игнорируются и не порождают бесконечный выпуск замен: lifetime budgets сохраняются. RPC proof и observation append-only. Для collector нужны token и native gas baselines, для fee wallet — native baseline.

После baseline и сверки снимается только baseline hold. TRON activation ставится в отдельную существующую очередь. Источник — собственный активный network_fee wallet с подтверждённым operating capital; max fee и фактический cost учитываются отдельно от principal. Начальный действующий fee source и его средства требуется подготовить при enrollment. Provisioning не создаёт gas и merchant credit. Партия открывается только после необходимых baselines, chain activation, свежей сверки и снятия остальных holds.

## Durable permits

Перед обращением к control signer, family signer и broadcast вызывается `BeginAMLDispatch` с фазой `authorize`, `sign` или `broadcast`. Отдельная SQL транзакция блокирует scopes в том же порядке, что risk invalidation, проверяет состояние и коммитит immutable dispatch start до внешнего вызова. Внешний RPC не удерживает SQL locks.

Проверяются exact registered plan, owner/key/network/token/amount/destination, source risk и scope versions, operating fee budget, enabled network policy, отсутствие открытого network hold, живой watcher lease, свежие heartbeat и chain timestamps. Relevant inbox и due risk/decision backlog должны быть обработаны. Запланированный будущий TTL rebuild не блокирует актуальный риск. Native и token watchers проверяются отдельно.

Каждая граница требует новой проверки. Если risk change принят раньше start, старое разрешение не проходит. Start означает потенциальный внешний эффект, включая timeout; процесс не может доказать отзыв уже выданной подписи. При первом broadcast сохраняются immutable source/risk/evidence/resource snapshots и watermarks. Повторные starts не переписывают этот snapshot.

Recovery наблюдает outcome сохранённой подписи. AML может повторить только те же bytes и tx hash через свежий broadcast permit, включая истёкшую authorization TTL. При новой revision того же класса повторно проверяются текущие coverage/scope/TTL; новую подпись старая revision не разрешает. Смена класса блокирует повторную отправку. Подписанный/неизвестный attempt сохраняет reserves до доказанного outcome; новый nonce, generation или альтернативный source автоматически не создаются.

## Вывод по классу

`POST /portal/v1/withdrawal-requests?merchant_id=<uuid>` принимает `tokenId`, `amount`, `addressId`, а при enabled AML scope обязательно `riskClass` (`low|medium|high|unknown`). Для `high|unknown` требуется `riskAccepted=true`. Поля входят в неизменяемый intent V2 вместе с конкретными source lines. Для legacy scope сохраняется прежний контракт. Это Portal API; отдельная команда withdrawal в публичном Merchant API этим изменением не добавлена.

Одна заявка использует один класс principal собственного мерчанта. Сервер выбирает и резервирует до 1000 точных sources; нехватка выбранного класса не дополняется другим классом. Unknown export требует доказанной физической суммы и owner, acceptance не снимает технические ограничения. Destinations, известные системе как управляемые кошельки, включая retired, запрещены.

На parent выполняется один merchant debit. Каждая line получает собственный финансовый basis, CW документ, gas budget и signer authorization. Оба Vault plugins проверяют сумму всех lines, уникальность, owner, source address/key/amount, class/acceptance и membership конкретной line. Для AML line разрешены только attempt/generation 1; альтернативный approval не позволяет повторно потребить ту же line. Публичные DTO не раскрывают Vault key IDs.

Final success уменьшает principal ровно на line amount. Final revert подтверждается normalized settlement и physical CW expense/refund, возвращает только principal этой line мерчанту и сохраняет фактическую стоимость сети в operating journal. Неизвестный outcome не считается failure. Повторная обработка того же факта идемпотентна; несовпадающее evidence отклоняется.

Мерчант может отменить неподписанный остаток approved/signing/broadcast/held заявки. Отменяются только lines без любой signing authorization, освобождаются их reserves и возвращаются их точные суммы. Уже подписанная line остаётся в recovery. Parent получает `partially_completed`, если часть суммы выплачена, а остальная безопасно возвращена; полный storno после частичного успеха запрещён.

В форме доступен выбор класса и отдельное подтверждение high/unknown. История показывает requested class, sources, класс при первом dispatch, текущий класс и tx hashes. Последующая оценка не переписывает класс состоявшегося вывода. Терминальные события `withdrawal.confirmed`, `withdrawal.failed`, `withdrawal.partially_completed` содержат неизменяемый снимок, completedAmount/refundedAmount и sources. Полностью отклонённая заявка публикует `withdrawal.failed` со status `rejected`. Outbox публикует событие один раз. Последующие risk changes публикуются отдельно.

## Порядок включения

1. Проверить backup/restore и legacy inventory: существующие shared balances, pending, signed и broadcast attempts не переносятся автоматически в новые source lines.
2. Обновить оба Vault plugins с V2/source-line enforcement и owner registries, control-plane consumer, API и workers. Привести схему к версии текущего бинарника (76). До активации маршрутов выполнить [AML configuration preflight](crypto-aml-preflight.md): worker требует точный `aml_signing.v1`, intent V2 и source-line basis от обоих family plugins; новый контракт нельзя направлять в старый signer.
3. При остановленной network policy выполнить trusted enrollment и настроить accepted provider mappings, risk policies, account budget и scopes. Подготовить собственный operating fee source без использования merchant principal.
4. Настроить merchant pool policy с явным `automaticBaseline=true`, проверить независимые RPC trust groups, ingestion и native/token watch coverage. Дождаться baselines, сверок, activation и готовых batches.
5. Повторить `crypto-preflight -mode aml`, затем ingestion preflight и Nile/Sepolia rehearsal: class-specific success, отказ по stale/high, поздняя переоценка между фазами, потеря ACK, повтор saved bytes, partial cancellation и final revert. Сверить ledger, principal, operating cost и webhook snapshot.
6. Включать merchant/network/token поэтапно после приёмки. При регрессии останавливать новые dispatch через network policy/hold, сохраняя ingestion и receipt recovery. Enabled AML scope нельзя выключить для возврата к shared legacy spending.

Автоматический общий rollout enrollment, native principal consolidation, resource-aware replacements, reviewed high resolution, general reorg corrections и живой баланс пакета AML-провайдера остаются отдельными задачами. Миграции с сохранёнными AML evidence/dispatch/withdrawal history не откатываются удалением данных: требуется forward migration.


## Проверено локально

- Go `test -race ./...`, сборка и golangci-lint: успешно.
- PostgreSQL AML integration: источник principal, отдельный operating gas, low/high/unknown authorize/sign/broadcast, immutable snapshot, late-risk, retry, partial success/revert/refund, конкурирующие requests и отмена неподписанного остатка.
- Baseline: независимый quorum, nonzero hold, policy/lease fencing, EVM asset-specific coverage и отставшая replica; деактивация merchant address не прекращает наблюдение.
- Crypto-worker integration/race на чистой базе: успешно, включая legacy signed recovery и запрет внешнего signer call внутри незакоммиченной транзакции.
- Migration integration/race: fresh install, пустой Down/Up, conventions и schema 60 → TRON upgrade → AML 73.
- Оба Vault plugin test suites, Go/TypeScript/Python SDK tests и 428 HTTP contracts: успешно.
- TypeScript/ESLint и 13 Chromium сценариев формы, включая 320 px, сброс acceptance при смене класса и точный high/acceptance payload: успешно.

Эти проверки подтверждают локальную реализацию. Реальные vendor fixtures и Nile/Sepolia rollout rehearsal выполняются отдельно по порядку включения выше.

Автоматический планировщик: после обычного sweep evaluation `PlanNextAMLBatchSweep` резервирует подходящий source и партию low/medium одной транзакцией. До этого зарегистрированные token sweeps могли исполняться через существующий executor, но автоматический выбор партии из `sweep_pending` отсутствовал. Планировщик сохраняет retain amount, отсекает чужие, held, high/unknown и stale sources; деньги не уходят до действующих authorize/sign/broadcast permits.

TRON checkpoint: стабильное наблюдение одного RPC можно сохранить на время ограниченного ожидания отстающего второго. Принимаются по-прежнему два независимых ответа на одной высоте с одинаковыми hash и balance. Изменение головы внутри чтения, разные balances и невозможность достичь общего checkpoint не создают baseline.


## Однократный повтор истёкшей неподписанной AML-авторизации

В схеме 76 оператор может разрешить повтор только сохранённой sweep-транзакции: `aml-pool-controller -tenant-id <tenant> -renew-exact-sweep <signing-record-id> -actor <operator> -reason <reason>`. Это отдельное явное решение после устранения технической причины отказа. Команда не меняет исходную authorization, unsigned envelope, fingerprint, сумму, адрес или nonce; новая control-подпись и десятиминутный срок записываются в immutable sidecar. Разрешение создаётся только один раз для истёкшей, ещё неподписанной попытки с сохранённой исходной подписью control plane, прежним fee reserve и актуальным low/medium inventory. До подписи повторно выполняются все risk, custody, batch, ingestion и resource gates. Изменившийся риск блокирует повтор. Уже подписанные транзакции восстанавливаются по сохранённым bytes без нового разрешения или замены nonce.

Схема 76 также учитывает неизменяемый testnet opening operating balance при применении последующих resource costs и допускает штатный статус `signed` внутреннего native funding в проверках точного плана. Merchant principal не становится источником gas. При временной ошибке активации коллектор остаётся pending в provisioning policy, чтобы контроллер не расходовал lifetime quota на новые адреса.


Финальные внутренние переводы проходят отдельный proof recovery: provisional watcher event связывается с подписанным expected effect и успешным chain settlement того же блока, после чего семейный verifier независимо проверяет фактический перевод. В EVM обязательны два RPC/trust group, общий canonical head и требуемые подтверждения. Это восстанавливает operating JIT и principal allocation в AML inventory без повторного зачисления ledger. Ни settlement сам по себе, ни запись в watcher inbox не заменяют независимое доказательство.


Каждая новая попытка автоматического свипа получает отдельный execution document. Это сохраняет независимость ожидания gas, отправки и финансового проведения разных sources. При росте gas quote уже исполненное JIT-пополнение не считается вечным доказательством достаточности operating balance: допустимо дополнительное пополнение точного дефицита (до трёх раундов на parent, со всеми прежними лимитами). Старые signed envelopes и nonce не переписываются.

## Финальный учёт выводов (84)

Новые CryptoWithdrawal используют model 2: broadcast сохраняет active principal reserve, без physical expense. Final independent proof закрепляет один outgoing owner за исходным merchant debit. При failed chain result расходуется только actual network fee, резерв освобождается, мерчанту возвращается сумма отказавшей строки. Failed AML v2 fact имеет physical expense/refund refs NULL; legacy model 1 сохраняет прежние факты. Частично успешный запрос остаётся partially_completed; успешный principal не восстанавливается. Поздний risk hold/paused intake ограничивает новый dispatch, сохраняя учёт уже принятого final transfer. При missing proof/quorum резерв удерживается. Не освобождайте его вручную и не исправляйте outcome/generation через generic posting: требуется отдельный recovery workflow. Детали: [контракт и locks](../reference/crypto-custody-writers.md#12-merchant-withdrawal-схемы-84).
