# План реализации учёта криптопоступлений и ресурсных провайдеров

> **TL;DR:** Сначала внедрить общую идентичность и защиту custody effects, затем согласованно переключить receipt/payment/AML; возвраты, казначейские основания и provider funding подключить через отдельные доказанные workflow.
> **Тип:** How-To
> **Аудитория:** Developer, AI-Agent, Admin
> **Статус:** реализация начата 05.11.2026. Схемы 77–104 добавляют identity/evidence index, внутренний receipt + suspense и общее признание principal для funding CLI, CryptoPayment v1, JIT native funding и финальных sweep/treasury/merchant withdrawal v2 и подтверждённой custody loss v2, а также отдельное признание network fee для новых резервов версии 2 и exact expected-transfer routing к исходному settlement owner и source-bound manual corrections; схема 89 вводит общий early resource preflight и исправляет пересечения proof/capture/policy/wallet locks. Схема 90 дополняет withdrawal/sweep selectors и activation coordinator единым порядком advisories/scopes/wallets до workflow rows. Этап 91 переносит native-funding operational updates/handoff и sweep bulk maintenance на early resource preflight с фиксированными кандидатами. Этап 92 выравнивает replacement/authorization publication и проверяет полный expected-effect replay без новой миграции. Этап 93 фиксирует требования snapshot для queue capture и полный semantic replay authorization/fee settlement; схема остаётся 91. Этап 94 (схема 92) защищает reconciliation identity/source target и исходное основание ручной AML cancellation. Этап 95 дополняет batch cancellation/expiry проверкой payload и полным AML request resource prefix; схема остаётся 92. Этап 96 исправляет cumulative rental refund и проверяет полную charge/refund history; схема 92. Этап 97 проверяет legacy compensation/recorded replay и resource reservation/broadcast facts; схема 92. Этап 98 (схема 93) выравнивает activation selectors/state, замораживает task identity и проверяет resource release replay. Этап 99 (схема 94) вводит fencing rental maintenance claims и защищает close/refund от запоздалого worker. Этап 100 (схема 95) защищает activation cost-repair token каждой попытки и guarded completion до fee writes, сохраняя authoritative finality. Этап 101 (схема 96) передаёт основной activation claim token через actor commands и guarded signing dispatch; late signed/broadcast evidence сохраняет исходный owner без снятия новой lease. Этап 102 выравнивает CryptoNetworkOperation metadata/protection/posting prefix и читает manual-edit permission после ожидания. Этап 103 (схема 97) сохраняет основания повторного финансового сторно и проверяет одинаковые network incidents без переписывания истории. Этап 104 проверяет полный replay общих команд постановки/снятия запретов и сохраняет initial observation как их основание; схема 97. Этап 105 (схема 98) защищает прямые SQL hold writers и разделяет новые pool/preflight lifecycle по wallet/source без переписывания legacy history. Этап 106 (схема 99) проверяет исходные причину и ответственного отмены заявки на кошелёк, сохраняет первое подтверждение наблюдения за адресом и отвергает истёкшие полномочия исполнителя после ожидания блокировки. Этап 107 (схема 100) сохраняет время первого решения AML и проверяет его результат при повторе, разделяет текущую сверку счёта и неизменяемую историю отдельных инцидентов. Этап 108 (схема 101) сохраняет исходную привязку preflight job и отдельный неизменяемый snapshot каждой попытки, проверяет callbacks по нему и выбирает готовый fixed target активации без блокирования другой сети неподготовленным кандидатом. Этап 109 (схема 102) сохраняет отдельные основания регистрации и финансовой активации существующего TRON ingestion, защищает first watch owner/boundary и повторяет только временно занятую регистрацию. Этап 110 (схема 103) сохраняет первое основание внешнего списания и связь наблюдений с расследованием, согласованно обновляет block evidence, сохраняет прежних owners после retirement и откладывает busy без исчерпания попыток. Этап 111 (схема 104) сохраняет основания классификации, approval/rejection и закрытия, атомарный audit и точный replay решений; остаток P3 зафиксирован пакетами R1–R8 и поимёнными индексами. Автоматический intake, attribution, остальные исходящие writers и cutover новой учётной модели ещё не подключены.
> **Связанные:** [Архитектурное решение](../crypto/crypto-inbound-provider-accounting-design.md), [Тестовые сценарии](../crypto/crypto-processing-test-scenarios.md), [Добавление документа](new-document.md), [Миграции и данные](../reference/development-rules.md).

## 1. Результат и границы работ

Состояние на языке хозяйственных операций: код уже защищает многие действующие документы от повторных движений и ошибочного сторно, но полный новый учёт неопознанных поступлений и авансов провайдерам ещё не включён. Номера 89–111 обозначают технические проходы разработки, а не количество завершённых крупных разделов плана. Остаток нельзя оценивать по этим номерам или по числу отметок в checklist.

| Хозяйственный результат | Раздел | Текущее состояние |
|---|---|---|
| Один физический перевод — один регистратор, безопасные повторы и сторно | P1–P3 | Большая часть общего механизма реализована в коде; аудит оставшихся путей записи и повторов продолжается |
| Автоматический приход на кошелёк и учёт до выяснения, затем зачисление мерчанту без второго прихода | P4 | Есть внутренние документы/регистры, основной автоматический поток не подключён |
| Невыясненные, рискованные и зарезервированные суммы нельзя расходовать как свободные | P5 | Требуется подключение новой учётной модели ко всем режимам расходования |
| Инвентаризация на одну дату/момент и рабочее место расследования | P6–P7 | Целевые возможности не завершены |
| Правильные казначейские основания и возврат неопознанного поступления | P8–P9 | Целевые процессы не завершены |
| Аванс провайдеру, услуга, деньги в пути, возвраты и автопополнение | P10–P11 | Отдельные действующие расчёты исправлены; полный целевой цикл не завершён |
| Перенос истории без повторных движений и переключение рабочей системы | P12 | Не выполнен |
| Итоговая приёмка всех включаемых хозяйственных сценариев | P13 | Предстоит; уже выполненные технические тесты не заменяют эту приёмку |

В P0–P2 также остаются незакрытые правила и структуры данных; они являются зависимостями соответствующих функций. Рабочая БД в рамках этих проходов разработки не обновлялась.

Изменения 09.11.2026: система отдельно сохраняет, с какого блока началась пробная проверка и с какого включён действующий учёт. Перезапуск или пауза не меняют уже принятую рабочую границу. Адрес и сеть наблюдаемого кошелька, контракт и точность токена защищены от изменения задним числом, поскольку с ними связана история операций. Первоначальные сведения внешних списаний и согласованные сведения о блоке дополнены этапом 110. Повторы решений защищены этапом 111. Следующий участок — новые эпизоды расследований внешних списаний; остальные крупные результаты перечислены в таблице выше.

Текущий прогресс: [карта writers и реализованные механизмы](../reference/crypto-custody-writers.md). Migration 77 создаёт immutable identity/occurrence/proof index. Migration 78 добавляет CryptoCustodyReceipt, suspense, posting ownership и recognition links к physical movement. Migration 79 подключает funding CLI к тому же registry. Migration 80 подключает новые indexed CryptoPayment v1: custody gross, merchant net, processing fee, recognition, неизменяемый экономический snapshot и outbox обязательны в одном коммите. Сервисы используют общие CustodyRecognitionRepo, CustodyEffectGuard и AccountingTransferBasis.

Для текущего проведённого legacy payment добавлен внутренний explicit adoption на отключённой и освобождённой от незавершённых отправок сети. Он связывает независимый proof с исходной physical line, сохраняет actor/reference и не создаёт денег или новых дат. Массовый backfill и закрытые исторические поколения остаются P12. Indexed payment требует registry даже до ingestion cutover; unindexed legacy workflow сохраняется. Перепроведение признанного payment допускается только с полным сторно custody/net/fee предыдущего поколения и тем же экономическим snapshot. Комиссия выше gross отклоняется. Доказанный orphan создаёт incident и сетевой hold без автоматической компенсации; отдельный recovery workflow остаётся открытым.

Схема 81 подключает CryptoNativeFunding: независимый indexed native transfer proof связывает два controlled wallet одного custody owner с одним posting owner. Principal expense/receipt, fee settlement/cost, неизменяемый binding и outbox коммитятся вместе. Proof получается вне финансовой транзакции; сетевой kill switch, paused ingestion, истёкший parent или поздний High не отменяют учёт уже принятого перевода. Missing/contradictory proof оставляет funding незавершённым и повторяемым. Reverted tx создаёт только фактический fee, cap violation удерживает reservation. Исторические settled fundings не переписываются; незавершённым до обновления нужен новый proof. Generic reversal и новое поколение признанного native funding закрыты до отдельного recovery workflow.

Проверки схемы 81: concurrent JIT settlement, оба principal links, rollback missing components/outbox/proof и orphan, запрет раннего fee settlement с освобождением principal (включая fee=0), immutable basis, payment aliases, Up/Down/Up и сохранение settled history 80 → 81. AML operating capital/late High/paused recovery и прежние worker/payment/funding/read-model suites проверены отдельно. Независимое доказательство fee и его собственная accounting identity подключены схемой 86; cap violation сохраняет hold/review workflow только после проверки доказательства и плательщика.

Схема 82 меняет механизм CryptoSweep для новых документов (`accountingModelVersion=2`). Broadcast больше не записывает principal: PostingEngine сохраняет состояние и snapshot ресурсных расходов. После независимого indexed proof каждая успешная строка отдельно создаёт source expense и collector receipt с одним registry owner, immutable binding и outbox. Principal, фактический network fee/cost, normalized settlement, итог строки и завершение attempt коммитятся атомарно. До этого кошелёк остаётся `sweeping`, AML allocation и native principal reservation сохраняют резерв. Reverted строка создаёт только фактический fee; фиктивного principal и его сторно нет. Подписанные attempts помечаются failed, AML allocation отменяется с аудитом, кошелёк возвращается в очередь. Частично завершённый пакет переносит точный сохранённый principal/fee split по каждой подтверждённой строке, без ожидания общего документа.

Существующие документы получают model 1 и завершаются прежним механизмом; migration не переписывает history и не присваивает ей recognition. Accounting model, подписанный basis и финальные строки защищены SQL. Generic repost/unpost, новое поколение и автоматический reorg recovery sweep v2 закрыты до отдельного workflow. Locator TRON/EVM находит signed expected effect независимо от observation index и отклоняет неоднозначные одинаковые effects. Indexed transaction нельзя повторно принять legacy payment через другой observation alias. Собственное независимое доказательство network fee и fee accounting identity остаются открытыми.

Проверки схемы 82: concurrent per-line settlement/replay и запоздавший broadcaster после success/revert, native principal + fee, rejected early fee settlement при fee=0 и fee>0, rollback missing claim/outbox/writer, reverted outcome без principal, immutable signed/final basis и запрет generic reversal, exact-line AML coverage до завершения всего пакета, сохранение финансовой истории 81 → 82 и Up/Down/Up неиспользованной схемы, запрет Down после использования v2. UI/API показывают версию модели и учёт по подтверждённым переводам.

Схема 83 подключает TreasuryWithdrawal model 2. Новые документы на broadcast сохраняют состояние без physical principal и расхода platform revenue. Существующие wallet/revenue reservations остаются active. После independent indexed proof исходящего перевода strict guard проверяет controlled source и внешний destination, общий registry закрепляет expense principal за документом. Principal, точный расход зарезервированных fee partitions, actual network fee/cost, normalized receipt, immutable binding, consumption резервов и outbox коммитятся атомарно. Reverted tx создаёт только actual network fee и освобождает principal/revenue; фиктивного расхода и его сторно нет. Запоздавший broadcaster не переподписывает завершённый документ. Старые model 1 сохраняют broadcast/drain без переписывания history и automatic adoption.

Изменение reserve-механизма: native fee budget учитывает активные wallet principal reservations. Native sweep может отдельно резервировать principal, но эта сумма не считается network fee и не расходует дневной лимит комиссии; JIT funding сохраняет прежний principal + fee budget. Legacy sweep model 1 не получает новый резерв при восстановлении. SQL сериализует новые treasury reservations по реальным balance resources, защищает basis/terminal status и запрещает раннее освобождение подписанного резерва при неизвестном исходе. Signed expected treasury нельзя повторно списать как custody loss либо поздним legacy withdrawal/treasury; уже учтённый legacy withdrawal/treasury/adjustment требует explicit adoption/reconciliation. Generic reversal, поколение 2+, treasury reorg recovery и собственная fee accounting identity остаются открытыми.

Проверки схемы 83: конкурентный final settlement/replay, точные fee partitions без merchant debit, zero/nonzero network fee, native principal reserve и запрет расходования его на gas, concurrent wallet/revenue reservations, replacement fee delta/lock order, rollback missing claim/outbox/writer, missing quorum/wrong amount, signed/final immutability, legacy history 82 → 83 и rollback guard. API возвращает accountingModelVersion; UI объясняет сохранение резерва до подтверждения сети.

Funding CLI теперь принимает только внешний перевод на platform-owned wallet. EVM funding получает двух независимых провайдеров через общий read-only verifier и сохраняет proof; один RPC больше не разрешает проводку. Managed sender/expected internal transfer требует settlement owner; merchant-owned funding использует AML operating workflow. Новая дедупликация допускает разные доказанные effects одной транзакции. SQL guards защищают от второго principal через payment/operating receipt. Положительная ручная correction без recognition требует adoption; расхождение projection/movements требует сверки. Исторические funding rows/proofs не переписываются: их replay требует будущего explicit adoption, а re-inclusion уже признанного effect не создаёт новое поколение автоматически. Общий переход остальных legacy writers ещё открыт.

Схема 84 подключает merchant CryptoWithdrawal model 2, включая составные AML-выводы. Merchant ledger сохраняет единственный исходный debit-first расход WithdrawalRequest. Broadcast фиксирует состояние без physical principal и дохода комиссии; principal reservation остаётся active. При final успешном independent outgoing proof expense owner, original merchant debit binding, principal, actual network fee/cost, normalized receipt, reserve consumption и outbox коммитятся атомарно. Revert возвращает только соответствующую сумму мерчанту, освобождает резерв и учитывает actual fee без фиктивного physical principal/storno. У failed AML v2 fact оба physical movement references равны NULL; успешная строка ссылается на proven registry expense. Старые model 1 и их факты сохраняют прежний drain/history.

Изменены существующие механизмы: момент физического списания и consumption резерва перенесён с broadcast на final settlement; AML valid-fact view поддерживает обе модели. API возвращает accountingModelVersion, портал поясняет удержание резерва. SQL замораживает финансовое основание, terminal outcome и reservations, запрещает ранний fee-only/release и конкурирующий legacy owner. Повторный запуск после revert восстанавливает сохранённую signed attempt через released reservation завершённого v2, не начинает новый вывод.

Проверки схемы 84: concurrent final settlement/replay, полный merchant refund без physical storno, native principal и zero/nonzero actual fee, gas reserve admission, rollback missing claim/outbox/writer, missing quorum/wrong amount, immutable basis/final history, expected-owner fence, mixed AML success/revert и отмена неподписанного остатка с точным principal/merchant balance, late High/paused recovery, legacy 83 → 84, Up/Down/Up и rollback guard. Независимые fee identity/proof, historical adoption и reorg/re-inclusion остаются открытыми.

Схема 85 подключает CryptoCustodyAdjustment model 2. Detection и классификация остаются неденежными. Maker/checker-проведение требует сохранённого independent indexed proof именно observed event/occurrence: controlled source, внешний destination, сеть/asset, сумма, block и quorum finality. В одном коммите сохраняются outgoing principal owner, immutable maker/checker binding, physical expense, равный platform custody-loss cost, action audit, reconciliation request и outbox. Merchant balance, provider balance и processing revenue не меняются.

Изменён механизм подтверждения новой корректировки: статуса final у инцидента и решения checker больше недостаточно для списания. Пока proof отсутствует, устарел или противоречив, документ остаётся pending_approval. Управляемый recipient, ожидаемая подписанная отправка или прежний учёт того же principal требуют reconciliation/matched-basis workflow. Общий lock по нормализованной экономике сериализует новые и legacy expenses через aliases. Generic reversal/repost новых principal и loss cost закрыты. Исторические документы помечаются model 1 без изменений денег; Down после появления model 2 закрыт. API возвращает accountingModelVersion, UI поясняет требование подтверждения сети.

Проверки 85: concurrent approval, missing/unindexed/wrong proof, orphan и re-inclusion, finality floor, maker/checker, managed destination, rollback principal/cost/audit/reconciliation/outbox, immutable history, race с legacy withdrawal через retired token alias, TRON token/internal value workflow, upgrade 84 → 85 и Up/Down/Up/rollback guard. Fee identity, reorg compensation и historical adoption остаются открытыми.

Внутренний receipt service принимает только доказанный внешний приход на явно отключённой сети без незавершённых fee reservations и неизвестных исходов подписанных отправок. Он не подключён к watcher, HTTP, CLI или generic CRUD. После первого receipt включение сети и legacy movements по его wallet/token запрещены SQL до следующей миграции availability/reorg/cutover. Это ограниченный этап подготовки, не рабочий режим приёма платежей. Остальные outbound/fee writers ещё требуют P3; поколения после первого и компенсация receipt/funding ещё не реализованы.

Проверки: сборка, Go tests с race detector, production/integration lint, 436 HTTP contracts; PostgreSQL integration для identity, конкурентного receipt/funding/payment replay, полной атомарности financial set/recognition/audit/outbox, полного и частичного сторно, неизменяемого fee snapshot, adoption 79 → 80 без денег, stale reorg, proof/hold до cutover, scope lock order и migration rollback guards. Прежние worker financial adjustment/reorg/withdrawal и exchange-rate integration tests проходят. Проверки проводятся в отдельном временном PostgreSQL 17; действующий стенд не обновляется.

Результат считается достигнутым, когда любой доказанный поддерживаемый внешний приход отражён один раз, его экономическое назначение разрешается без повторного custody, а свип/вывод не расходуют нераспределённые или запрещённые средства. Депозит провайдеру отделён от оказанной услуги; расхождение ни с chain, ни с провайдером не превращается автоматически в доход/расход.

В план входят backend, schema, worker, Vault capabilities, ERP UI, Portal/API projections, миграция истории и приёмка Nile/Sepolia/реального resource-provider account. Новый production send не является частью подготовки этого плана.

Начальные рабочие решения:

| Вопрос | Решение для реализации |
|---|---|
| Общий receipt или отдельный путь только для unmatched | Общий внешний CryptoCustodyReceipt; двойная архитектура v1/v2 нужна только для перехода |
| Автоматическое merchant credit | Разрешено по доказанному matching; receipt и allocation проводят в одном коммите при готовом локальном evidence |
| Неизвестный риск | Не мешает отражению установленного обязательства; новые расходы закрыты policy |
| Dust | Поддерживаемый актив учитывается полностью; matching/screening имеют отдельные budgets |
| Неподдерживаемые активы | Технический журнал; финансовое admission после проверки актива и исходного effect |
| Возврат | Отдельный refund payable/outbound basis; начальная поддержка platform-paid network fee |
| Невостребованные средства | Только review по возрасту; money recognition выключено без применимого правового основания |
| iTRX funding evidence | Сначала проверить доступные API/statement; при отсутствии API — verified import с независимым решением |
| Автопополнение провайдера | Выключено до подтверждения credit evidence, destinations и лимитов |
| Расход фиксированной краткой аренды | Признание при доказанной готовности услуги; аналитическое распределение между операциями отдельно |
| Частичная merchant attribution | Backend/schema готовятся сразу; публичное включение после готовности DTO/SDK и invoice aggregates |
| Переделка общего GL | Не требуется; добавить типизированные operational counterpart registers и инварианты документов |

Юрисдикция, legal entity, правила unclaimed property, допустимость fee withholding и межвладельческих возвратов фиксируются в policy до включения соответствующих денежных действий. Их отсутствие оставляет средства suspense и не блокирует корректный физический учёт.

Схема 86 меняет расчёт новых FeeReservation (`accountingModelVersion=2`): одного final receipt больше недостаточно. Нужен отдельный NetworkFeeProof от двух независимых provider/trust groups, подтверждающий плательщика, итог сети, canonical block и фактическую комиссию. Fee role определяется сетью и tx hash независимо от transfer ordinal и token alias. Успешный и reverted исходы могут иметь положительный либо нулевой fee; ноль сохраняет факт и outbox без денежных движений. Activation оставляет единственного владельца CryptoNetworkOperation; остальные цели — ChainSettlement. Неполный набор expense/cost/components/owner/outbox не коммитится. Terminal replay сверяет сохранённый proof. Replacement требует сохранённой подписанной попытки и immutable transition event в той же транзакции. Исторические fee reservations получают model 1 без перепроводок и продолжают прежний workflow. Поддержаны TRON total fee (energy/bandwidth/остаток других chain fees) и Ethereum mainnet/Sepolia execution/blob receipt; неизвестные fee-поля требуют нового профиля. Автоматический adoption, reorg compensation и новые поколения fee не добавлены. [Контракт, блокировки и проверки](../reference/crypto-custody-writers.md#14-независимое-признание-network-fee-схемы-86).

Схема 87 подключает expected routing до direction exits и payment matching. Incoming/internal/outgoing observation сверяется с полной сетью/tx/kind/from/to/asset/amount и сохранённым подписанным намерением. Совпадение hash или managed source само по себе не создаёт route. Pending request и outbox атомарны; однозначный независимый indexed proof добавляет immutable logical route и evidence links. Routing и re-inclusion не создают денег, нового posting generation либо разрешения подписи. Worker ускоряет проверку исходной операции через существующие pollers, не снимая чужой lease/hold. SQL защищает новые payment receipts и shared postings от занятия expected principal другим owner. Старые expected/match rows и финансовая история не переписываются. [Контракт и ограничения](../reference/crypto-custody-writers.md#15-маршрутизация-ожидаемых-переводов-схемы-87).

## 2. Обязательные изменения действующего поведения

| Область | Изменение, требующее внимания при review |
|---|---|
| CryptoPayment | V2 перестаёт создавать физический приход и начинает распределять ранее учтённое поступление до выяснения; сумма частичной оплаты — распределённая часть поступления |
| CryptoPayment v1, схема 80 | Indexed principal требует registry и точного gross/net/fee; признанный basis и fee snapshot нельзя редактировать, перепроведение требует полной компенсации без переоценки; исторический приход подключается отдельным adoption |
| JIT native funding, схема 81 | Для нового principal недостаточно финального transaction receipt: нужен независимый indexed transfer proof; обе controlled стороны признаются одним owner, immutable intent нельзя заменить при replay |
| Начало чтения TRON и наблюдаемые справочники, схема 102 | Перезапуск сохраняет принятую рабочую границу. Адрес/сеть кошелька и контракт/точность токена, уже связанные с историей наблюдения, нельзя редактировать. Финансовый режим требует времени активации; прямые записи используют read committed. Противоречивые старые данные требуют разбора до установки миграции |
| Внешние списания, схема 103 | Первое основание не переписывается. Старое событие сохраняет прежний wallet/token после деактивации каталога; новый блок не наследует подтверждения другого блока. Занятый ресурс требует повтора всей команды; worker откладывает её без исчерпания попыток. После новых source facts обычный Down закрыт |
| Решения по внешним списаниям, схема 104 | Повтор по тому же основанию возвращает сохранённое решение без второго проведения. Ответственный, причина, доказательства и время защищены от редактирования. После отказа допускается одна новая классификация; дальнейшие изменения требуют отдельного нового случая. Прямой SQL требует decision seed, audit создаётся DB |
| Sweep, схема 82 | Новые документы учитывают principal только при final settlement каждой строки; broadcast фиксирует snapshot расходов. Старые model 1 завершаются прежним механизмом без переписывания истории |
| Custody loss, схема 85 | Новые корректировки требуют independent indexed proof и общего owner; классификация не списывает деньги. Старые model 1 сохраняются |
| Merchant withdrawal, схема 84 | Debit-first мерчанта сохраняется; physical principal и consume резерва только на final proof. AML success/revert учитываются по каждой строке. Model 1 history/drain сохраняется |
| Treasury, схема 83 | Новые документы проводят principal и расход дохода платформы при final settlement; до этого wallet/revenue reservations остаются active. Model 1 сохраняет старый broadcast/drain |
| FinancialGuard | Проверка полного chain receipt отделяется от проверки частичной allocation |
| AML inventory | Удаляется обязательная привязка physical recorder к CryptoPayment; добавляются unattributed/treasury позиции |
| Invoice/single-payment/webhooks | Receipt не означает merchant credit; aggregation и selection работают с receipt identity |
| Reorg/adjustment | Новые цепочки receipt/attribution компенсируются совместно; старые payment reversal не применяются к v2 без адаптации |
| Все custody writers | Участвуют в общем recognition registry, включая direct SQL settlement и CLI |
| Signing/Vault | Новые refund/provider purposes и basis должны поддерживаться обоими plugins; старые cached signatures не обходят новую policy |
| Rental settlement | Нельзя оставить одновременные финансовые writers в model methods и SQL Activate/Close |
| Reconciliation | Amount-only comparison заменяется сопоставимым chain/accounting checkpoint |
| Review UI | Текстовая HTTPS reference больше не подтверждает зачисление/возврат; исход выводится из связанного проведённого документа |

## 3. Зависимости и релизы

Номера будущих migrations выделяются по фактической вершине db/migrations в момент реализации. Исходный baseline — 76; добавлены 77–85. Существующие migrations 51/60/61–76 не редактировать для доставки этой функции.

~~~mermaid
flowchart TD
    P0[P0: policy и baseline] --> P1[P1: identity и evidence]
    P1 --> P2[P2: расширение схемы]
    P2 --> P3[P3: recognition всех writers]
    P3 --> P4[P4: receipt и payment v2]
    P3 --> P5[P5: AML и availability]
    P1 --> P6[P6: сопоставимая сверка]
    P4 --> P7[P7: review и attribution UI/API]
    P5 --> P7
    P7 --> P8[P8: treasury classification]
    P7 --> P9[P9: refund и Vault]
    P0 --> P10[P10: provider capabilities и rental]
    P10 --> P11[P11: funding и cash refund]
    P3 --> P11
    P5 --> P11
    P4 --> P12[P12: backfill и cutover]
    P5 --> P12
    P6 --> P12
    P7 --> P12
    P12 --> P13[P13: приёмка и rollout]
    P8 --> P13
    P9 --> P13
    P11 --> P13
~~~

Минимальный релиз физического учёта требует P0–P7 и безопасного P12. Treasury/refund/provider funding могут оставаться выключенными отдельными capabilities, пока их P8–P11 не приняты. Полное покрытие исходных сценариев требует всех этапов.

Expand schema и shadow computation не создают новых денежных движений. V2 intake включается атомарно с его financial guard, inventory/availability и reorg поддержкой. Старый binary после активного v2 ledger не является допустимым rollback.

## 4. P0 — baseline, контракты и правила учёта

Изменяемые места: architecture proposal, docs/crypto/crypto-processing-test-scenarios.md, docs/reference/crypto-canonical-contracts.md.

- [ ] Зафиксировать разрешённые network/token/effect types, asset scale и token mechanics; отдельный список unsupported cases.
- [x] Составить карту production custody writers, включая generic posting/reversal, прямые SQL, CLI, operating receipts и recovery; результат — reference/crypto-custody-writers.md. Provider writers отмечены в архитектурной инвентаризации; test fixtures не являются production writers.
- [ ] Для каждого writer указать evidence owner, posting owner, idempotency key, lock order, возможный второй путь к тому же effect.
- [ ] Снять manifest текущей схемы, capabilities server/worker/Vault, enabled custody/AML policies и финансовых read models.
- [ ] Зафиксировать fee assessment scope, normal/late/manual attribution policy, terminal invoice и single-payment правила.
- [ ] Определить роли treasury/reviewer/approver, суммы и лимиты, object-bound step-up и независимое подтверждение.
- [ ] Разделить ledger recognition, ownership, risk, execution и review states в domain enums.
- [ ] Подготовить sample manifests: normal payment, unmatched, dust, manual credit, old reorg, internal transfer, provider opening.
- [ ] Подтвердить policy признания услуги и перечень treasury counterpart categories; unclaimed recognition по умолчанию отключён.

Критерий завершения: для каждого исходного сценария определены документ, регистры, встречная сторона, basis, сумма, финальность и разрешающий actor. Нет варианта «прибавить кошелёк и потом решить, куда отнести».

## 5. P1 — accounting identity и универсальный evidence guard

Новые предлагаемые пакеты: internal/domain/crypto/custody_effect.go, internal/application/cryptoapp/custody_effect_service.go. Существующие: blockchain_event.go, block_ingestion.go, TRON/EVM decoders/verifiers, reference/canonical contracts.

- [x] Ввести AccountingEffectIdentity.Key с отдельной стабильной версией и golden vectors Go/SQL.
- [x] EVM: доказать transaction-local ordinal по receipt; сохранять alias старого block-global event index в persisted verification evidence.
- [x] TRON: доказать mapping native/log ordinal и полный internal trace/call-value identity; сохранить действующие decoder/economics bytes.
- [ ] Описать occurrence, canonical generation, economics conflict и re-inclusion после orphan.
- [x] Ввести CustodyEffectGuard → VerifiedCustodyEffect и AccountingTransferBasis, не зависящие от CryptoPayment. Strict path всегда требует persisted proof/index, current network confirmations и DB-транзакцию; это проверка evidence, не выбор canonical generation или разрешение dispatch.
- [x] Разделить proof успешного principal и proof фактически списанного fee: схема 86, независимый NetworkFeeProof TRON/Ethereum execution+blob. Success/revert/zero подтверждаются отдельно; failed transfer не создаёт principal. Дополнительные L2 fee profiles, reorg и adoption остаются отдельными этапами.
- [ ] Распространить guard на receipt, internal settlement, native funding, provider funding/refund и refund execution.
- [ ] Для противоречивых payload сохранять incident и ограничение новых расходов; повторная доставка не превращается во второй effect.
- [x] Сохранять исходные evidence versions; optional accountingIdentity не меняет EconomicsHash и не переписывает historical proofs.

Приёмка: два logs в tx различаются; один effect из двух RPC совпадает; re-inclusion EVM с другим log.Index не создаёт второй актив; missing traces/ordinal не принимаются по догадке.

## 6. P2 — расширение схемы и типизированные регистры

Migration группы поставляются последовательно: identity/recognitions → receipt/suspense/reservations → ownership/AML compatibility → counterpart/refund → provider transit/service facts → reconciliation checkpoints. Точные номера выделяются при реализации.

- [x] Добавить logical effect/occurrence/alias tables и immutable recognition links к movement line IDs. Схема 80 допускает следующие поколения CryptoPayment только после полной компенсации custody/net/fee и с тем же owner/economics/snapshot; receipt/funding остаются generation 1.
- [x] Добавить doc_crypto_custody_receipts, reg_crypto_receipt_suspense_movements и агрегированные balances. Пока разрешён только начальный receipt, без расходования suspense.
- [ ] Добавить decision reservation/application/release facts; не полагаться на mutable boolean reserved.
- [ ] Добавить accounting_model_version, receipt/allocation/fee-assessment refs в payment; ограничения v1 и v2 раздельны.
- [ ] Разрешить несколько v2 attribution документов для одного receipt в пределах его gross; новый UNIQUE по allocation identity.
- [ ] Добавить отдельные refund payable/reservations, external positions/treasury sources/other income и provider transit/service-pending.
- [ ] Добавить технические observations неподдерживаемых активов с chain identity; не создавать cat_token автоматически по ticker.
- [ ] Все суммы и mappings связать с token/network, proof/generation, custody scope и неизменяемым basis.
- [x] Подключить receipt suspense movement source/visitor/recorder к PostingEngine в server и worker. Остальные новые регистры подключаются на соответствующих этапах.
- [ ] Добавить append-only/reversal guards, FK, limit compensation и deferred constraints для точных наборов движений.
- [ ] Защитить отсутствие отрицательной available allocation/refund capacity и точный fee/net split; signed deficit вынести в отдельные incident positions.
- [ ] Обязательные audit/outbox и financial state сохранять атомарно; generic Post/Unpost денежных документов закрыть.
- [ ] Добавить миграционные compatibility views для legacy v1, не генерирующие денежных движений.

Приёмка: fresh schema и upgrade с заполненной схемы 76; fault injection между любыми компонентами документа не оставляет частичного проведения. Прямой SQL INSERT неполного financial set отклоняется при commit.

## 7. P3 — общий recognition registry для существующих writers

Конечная приёмка и остаток P3 зафиксированы в [пакетах P3-R1–R8](../reference/crypto-p3-acceptance.md), [индексе Go](../reference/crypto-p3-go-paths.csv), [индексе фактических SQL routines/triggers](../reference/crypto-p3-sql-paths.csv) и [журнале проверок с hash](../reference/crypto-p3-review.csv). Открытые общие требования ниже закрываются только после проверки всех применимых строк этих пакетов; число технических проходов не определяет готовность P3.

Изменяемые места: crypto_repo/block_ingestion.go, crypto_repo/financial_adjustment.go, outbound_repo settlement/native_funding, register_repo/crypto_balance.go, CLI funding, application orchestration.

- [x] Подключить registry к CryptoPayment v1. Схема 80: новые indexed postings и explicit adoption текущего проведённого canonical legacy payment без новых денег. Закрытые поколения и массовый historical backfill остаются P12.
- [x] Подключить native funding principal обеих controlled сторон: схема 81, strict independent proof, один owner, точный principal/fee set, binding/outbox, immutable intent и replay. Старые settled rows требуют отдельного backfill; отдельная fee role добавлена схемой 86.
- [x] Подключить обе controlled стороны sweep: схема 82, model 2, final per-line principal и exact AML coverage. Broadcast principal исключён; модель 1 сохраняется для history/drain. Adoption истории и reorg/re-inclusion остаются отдельными зависимостями; fee identity добавлена схемой 86.
- [x] Подключить treasury principal: схема 83, independent outgoing proof, один expense owner, final principal/revenue/fee/reservation/outbox set. Model 1 history/drain сохранён; adoption и reorg recovery отдельно.
- [x] Подключить withdrawal principal: схема 84, final expense owner и original merchant debit binding без второго списания; active principal reserve до результата, exact partial AML success/refund. Model 1 history/drain сохранён; adoption/reorg отдельно.
- [x] Подключить подтверждённый external-outflow adjustment: схема 85, independent indexed proof, один outgoing owner, maker/checker binding, exact physical/loss-cost/audit/reconciliation/outbox set; old model 1 history/drain сохранён. Managed destination, known signed execution и prior owner требуют своего workflow.
- [x] Подключить FeeBudget/CryptoNetworkOperation network fee: схема 86, новые FeeReservation model 2, отдельные proof/identity/immutable binding, exact native expense + platform cost + normalized components + outbox. Activation использует единственного document owner. Старые model 1 history/drain, автоматический adoption и reorg отдельно.
- [x] Подключить crypto-record-wallet-funding; убрать возможность конкурировать с новым receipt intake. Схема 79: общий registry, strict indexed proof, EVM quorum, atomic audit/outbox; legacy adoption и дальнейшие поколения отдельно.
- [x] Ввести exact expected-effect routing до внешнего receipt admission: схема 87, полная экономика и persisted signed envelope, один ожидаемый transfer на авторизацию. Независимый proof с accounting identity и MatchingEffects=1 привязывает logical effect; ambiguous/legacy/unsupported intent остаётся pending без внешнего прихода. Multi-output и будущие provider/refund writers требуют отдельных профилей.
- [x] Pending expected transfer передавать исходному settlement owner: immutable request/outbox, отдельная delivery projection и existing funding/sweep/treasury/withdrawal pollers. Истёкший parent/signing label не меняет owner; lease/hold не снимаются, подпись/broadcast/financial posting из handoff запрещены. Нет готового owner document — durable retry, без fallback.
- [x] Сопоставить manual corrections с chain effects только при доказанном basis, не занимать effect из пользовательской строки tx hash. Схема 88: новые CryptoAdjustment model 2 фиксируют исходный custody posting текущего поколения; indexed legacy payment без recognition требует adoption. Самостоятельная wallet variance не создаёт chain owner. Historical model 1 сохраняется, миграция ручных начислений и исправление пропущенного effect требуют P12/отдельного доказанного workflow.
- [ ] Свести новые resource lock orders с действующими scope/wallet/advisory locks; устранить обратные порядки у всех участвующих writers.

Этап 89 этого пункта реализован: общий SQL helper берёт все scopes владельца/сети в порядке merchant/network/token, затем полный набор catalog wallets по ID. Proof observation и batch capture берут wallets до checkpoint/balance/effect; batch заранее собирает объединение получателей всего блока. Native funding и sweep передают обе principal стороны и fee payer одним набором. Fee settle model 1/2 берёт payer до reservation row. Maintenance intake/adoption берёт policy до wallet, treasury — wallet до revenue balance и document/balance reservation. PostingEngine добавляет resource preflight до validators/сторно: union исходных и новых wallets для repost, исходные wallets для unpost. Тарифы, движения, posted generations и recognition owners не меняются. На этапе 89 ещё оставались legacy selectors/reservation entry points и activation coordinator; их изменения описаны в этапе 90 ниже.

Проверки 89: build, полный Go race suite, production/integration lint, 436 HTTP contracts, TypeScript и ESLint; полный crypto_repo/funding integration suite и worker suite на разных свежих БД. Отдельные PostgreSQL wait-edge tests проверяют proof wallet/balance, capture wallet/checkpoint, maintenance policy/wallet, полный operation/native scope set, wallet без преждевременного lock при ожидании scope, порядок/дедупликацию нескольких wallets и old/new preflight до validators. Reorg/сторно также берут полный исходный wallet footprint. Up/Down/Up 88↔89 сохраняет financial snapshot. Все эти проверки прошли; рабочая БД не обновлялась. Общий репозиторный integration gate с ранее отмеченным нестабильным тестом автоматизаций повторно не объявляется зелёным по одному crypto suite.

- [ ] Для каждого ON CONFLICT определить неизменяемое основание либо правило обновления проекции. Повтор команды сверяет сохранённое основание; сохраняемое первое наблюдение не переписывается при штатном обновлении. Любой конфликт сам по себе не означает успешное выполнение.
- [x] Защитить rental maintenance от старого владельца lease: этап 99, схема 94; token каждой попытки, проверка после locks, guarded renewal/state/close/release.
- [x] Передавать activation execution claim token в task/commands и проверять ownership после общего prefix до нового budget/signing dispatch и actor state/link writes. Сохранение уже полученного signed envelope/известного broadcast отделить как durable evidence recovery исходной authorization, без нового dispatch; сохранить отдельный authoritative finality/reconciliation путь. Этап 101, схема 96.
- [x] Добавить отдельный token activation cost-repair попытки и fencing held/completion, чтобы поздний RPC не снимал claim новой попытки; этап 100, схема 95. Финансовый replay сохраняет первоначальные owner/fee facts; authoritative finality остаётся отдельным путём.
- [x] Получать общий activation prefix до metadata/protection/row locks и post/unpost/repost существующего CryptoNetworkOperation; проверять manual-edit permission после ожидания, захватывать union текущих/предлагаемых/исторических wallets. Этап 102, схема 96.
- [x] Проверять исходное основание повторного финансового сторно платежа, сохранять его для новых записей и принимать повтор только после полного сторно трёх регистров. Одинаковые сообщения о проблеме сети не создают дубли из-за порядка JSON-полей; ссылка инцидента на платёж проверяется в обоих действующих путях. Этап 103, схема 97.
- [x] Проверять полный payload общих команд постановки и снятия operational holds, сохранять первоначальное основание и resolution actor/reason, снимать набор по сети/кошелькам атомарно и не добавлять новые targets завершённому owner. Новые наблюдения сверки сохранять отдельно от первоначального hold basis. Этап 104, схема 97.
- [x] Защитить прямые SQL-пути hold insert/update/delete: wallet provisioning, AML merchant publication/preflight, scope/high pool triggers и baseline release. Новые pool/preflight owner identities привязать к конкретному wallet и основанию; legacy history не переименовывать. Этап 105, схема 98.
- [x] Проверять неизменяемое основание отмены заявки на создание кошелька, включая прямой SQL и одновременные команды. При обновлении наблюдения сохранять первое подтверждение для каждого адреса/актива, проверять текущего исполнителя и срок его полномочий после ожидания блокировки. Этап 106, схема 99.
- [x] Сохранять первоначальное время оценки AML и проверять повтор на этот момент без продления срока; отделить изменяемый счётчик расхождений по счёту от неизменяемого основания инцидента и создавать новый случай после устранения предыдущего. Этап 107, схема 100.
- [x] Зафиксировать первоначальную привязку preflight job и неизменяемое основание каждой попытки; сверять Save/Retry с исходными wallet/asset/policy/scope и текущим token после полного resource prefix. Нельзя повторно использовать завершённый token. Отделить queue hint активации от повторного исполнения существующей task и выбирать только готового фиксированного кандидата. Этап 108, схема 101.
- [x] Сохранить отдельные registration/activation основания существующего чтения TRON, заморозить активную начальную границу, проверить network/asset scale и first watch owner до принятия повторов; исключить обратное ожидание между catalog callbacks и ingestion. Этап 109, схема 102.
- [x] Сохранить первое основание внешнего списания отдельно от последующих наблюдений, связать наблюдение с run/incident, проверить полный payload source и прежних catalog owners при повторе; согласованно обновлять block tuple и получить early resource prefix. Busy откладывается без исчерпания попыток. Этап 110, схема 103.
- [x] Сохранить отдельные основания решений Classify/Resolve/ApproveAdjustment/RejectAdjustment, точные actor/reason/evidence и DB clocks, автоматический immutable audit и atomic incident/hold/reconciliation closure. Exact replay не проводит деньги и не снимает holds повторно; legacy NULL не backfill. P3-R1, этап 111, схема 104.

Этап 90 lock-order audit реализован в коде 07.11.2026. Новый SQL try-lock получает полный набор wallet-owner scopes и, для withdrawal, дополнительный merchant/network scope заявки одним сортированным объединением. NOWAIT failure откатывает только попытку вместе с её частичными locks. Withdrawal selector сериализует request/execution advisories до ресурсов, пропускает busy кандидата, сохраняет существующий резерв и блокирует request row только после wallets. Тот же prefix используется failure/signing publication и broadcast completion, включая AML source-line wallets. Sweep claim теперь планирует источники и collectors без row locks, атомарно пропускает busy набор, затем на свежем statement snapshot повторяет eligibility/amount checks только для выбранных wallets и уже заблокированных collectors; появившийся collector не добавляется поздним FK. Draft linking получает attempt/document advisories и wallets до attempt rows. Activation broadcast/finality получает workflow advisory → operation advisory → payer/target scopes и wallets → activation row; проверка fee basis и повторная проверка linked identity сохраняются. Это исправление сериализации, без новых переводов, комиссий, recognition owners и переписывания истории.

На этапе 90 оставались native-funding operational updates/claims и bulk expiry с SQL queue/invalidation hooks; их изменения описаны в этапе 91 ниже. Общий resource audit и ON CONFLICT payload audit остаются открытыми.

Проверки 90 прошли: build, полный Go race suite, production/integration lint, 436 HTTP contracts, TypeScript и ESLint; полные `crypto_repo`/`funding` integration suites и worker suite на отдельных свежих БД. Тесты проверяют освобождение частичного набора locks, объединение request/owner scopes, пропуск busy wallet/collector без лишних резервов/claims, сохранение единственного резерва при replay и реальные wait edges для withdrawal wallet/request, activation target/workflow row и operation advisory/payer. Рабочая БД не обновлялась. Общий integration gate за пределами этих suites по-прежнему не объявляется зелёным.

Этап 91 реализован 07.11.2026: native-funding operational mutations, confirmation claims/release, native replacement hash и expected-transfer nudge получают полный source/destination/fee-payer scope/wallet prefix до funding row. Новый SQL helper атомарно пропускает busy funding row вместе с её ресурсами. Создание funding использует preflight до FK и проверенный immutable replay через DO NOTHING + чтение сохранённого intent; linking проверяет точный fee basis/payer/principal. Sweep bulk maintenance сохраняет общую транзакцию, но сначала фиксирует IDs кандидатов, получает advisories и полный набор ресурсов. Все expiry/cancellation/draft/recovery изменения ограничены этим набором и повторно проверяют eligibility. Новые кандидаты обрабатываются следующим проходом. Signed/broadcast reserves, живой funding и reconciliation holds защищены исходными guards. Renewal predicate явно связывается с ID authorization. [Подробный контракт](../reference/crypto-custody-writers.md#19-native-funding-operational-updates-и-bulk-maintenance-схемы-91).

На этапе 91 оставались replacement/authorization publication prefixes остальных исходящих writers; они дополнены этапом 92 ниже. ON CONFLICT для создания native funding проверен в этапе 91; полный payload audit остальных writers ещё не завершён. Рабочая БД не обновлялась.

Проверки 91 прошли: fresh migration/rollback gate, build, полный Go race suite, production/integration lint, 436 HTTP contracts, TypeScript и ESLint; полные `crypto_repo`/`funding` integration suites и финальный полный worker suite на отдельных свежих БД. После общего funding-helper изменения повторены repository-сценарии native funding/expected-transfer. Новые tests проверяют реальные wait edges operational wallet/funding row, maintenance wallet/authorization row и handoff scope/funding row с AML queue hook, полное освобождение busy footprint, конкурентный immutable replay и отказ чужому fee basis, фиксированный cohort и защиту подписанного после подготовки envelope. Общий integration gate вне этих suites по-прежнему не объявляется зелёным. Временный PostgreSQL удалён после проверки; рабочая БД не обновлялась.

Этап 92 реализован 07.11.2026 без новой миграции: схема остаётся 91. Replacement и signing publication получают document advisories и объединение request/wallet-owner scopes, principal/collector/fee/funding wallets до business rows. Sweep hash publication проверяет связь line/attempt/document. AML authorization использует line basis вместо ошибочного legacy request lookup; SQL risk/generation guards сохранены. После ожидания ресурсов заново проверяются execution state/TTL. Legacy withdrawal replacement из broadcast допускается только с точным подписанным predecessor предыдущей generation, тем же principal intent, sequence и approval digest. Signed expected-effect replay сравнивает полный payload и ровно один effect текущего single-transfer profile; конфликт откатывает и signed envelope, и новый expected effect. [Контракт и изменения существующих механизмов](../reference/crypto-custody-writers.md#20-публикация-execution-и-authorization--этап-92).

Проверки 92 прошли: build, полный Go race suite, production/integration lint, 436 HTTP contracts, TypeScript и ESLint; полные `crypto_repo`/`funding` integration suites и финальный полный worker suite на разных свежих БД схемы 91. Новые tests покрывают реальные advisory/wallet/row wait edges у replacement/publication, настоящий AML line basis, publish-first/cancel-first с единственным дебетом/возвратом, exact signed predecessor и отказ изменённому/пропущенному/unsigned/terminal intent, полный expected-effect replay и атомарный rollback на конфликте destination/ordinal. Рабочая БД не обновлялась. Общий integration gate вне этих suites по-прежнему не объявляется зелёным.

На этапе 92 оставались inventory/resource apply queues, authorization tuple replay и chain settlements; они дополнены этапом 93 ниже. P3 lock-order и общий ON CONFLICT checklist остаются открытыми. Автоматический intake, attribution и cutover не включены. Рабочая БД не обновлялась.

Этап 93 реализован 07.11.2026 без новой миграции, схема остаётся 91. Аудит queue capture подтвердил scope-before-queue и producer wakeup barrier; вложенный capture теперь явно требует read committed, чтобы ack не использовал старый repeatable-read/serializable snapshot после последнего producer commit. Signing tuple replay в lookup и insert-race путях сравнивает полный bound authorization body, source wallet и заново проверенный canonical unsigned payload; исходные ID/deadline сохраняются. Fee settlement model 1/2 проверяет exact owner/outcome/components и direct financial movement payload. Непустой breakdown требует точного total и уникальных names; pending normalized collision требует reconciliation. No-op UPDATE append-only settlement убран; конфликт чужого movement line откатывает весь новый набор. [Контракт, изменения существующих механизмов и матрица аудита](../reference/crypto-custody-writers.md#21-queue-barriers-и-semantic-replay--этап-93).


Проверки 93 прошли: build, полный Go race suite, production/integration lint, 436 HTTP contracts, TypeScript и ESLint; полные `crypto_repo`/`funding` integration suites и worker suite на отдельных свежих БД схемы 91. После последнего isolation guard повторены queue tests и финальные build/race/lint gates. Проверены оба signing replay пути до signer/permit, сохранение deadline и запрет продления expired unsigned record, реальные producer/capture/ack wait edges и stale snapshot, единичный concurrent fee settlement, exact normalized replay и rollback чужого financial line ID. Более поздние observations сохраняют исходный accounting period. Рабочая БД не обновлялась; общий integration gate вне этих suites по-прежнему не объявляется зелёным.

Этап 94: миграция 00092 / ExpectedSchemaVersion 92 защищает source-bound identity заявки на сверку без заморозки operational status/lease/results. Go enqueue сверяет сохранённый payload после конфликта, получает полный scope/wallet prefix и атомарно пишет native-funding pair. Ручная отмена AML allocation/resource при replay требует исходных reason/actor. Expected-transfer replay сохраняет первое observation при поступлении final proof. Внимание к совместимости: upgrade останавливается на несовместимых старых reconciliation targets; другой reason/actor повторной ручной отмены теперь возвращает conflict. [Контракт, проверки и оставшийся batch audit](../reference/crypto-custody-writers.md#22-reconciliation-identity-и-ручная-aml-cancellation--этап-94). Общие P3 checkboxes остаются открытыми.

Проверки 94 прошли: build, полный Go race suite, production/integration lint, 436 HTTP contracts, TypeScript и ESLint; полный `crypto_repo` integration suite (464 с), отдельные TRON/EVM funding suites и полный worker suite на другой свежей БД схемы 92. Новые PostgreSQL/race tests проверяют конкурентный replay, полный immutable request target, completed projection без сброса, rollback всего пакета при коллизии, wallet-before-queue wait edge, source-bound native pair, отказ неверному network/source, Up/Down/Up без изменения валидной заявки и остановку upgrade на неверном legacy target. AML tests проверяют конфликт первоначального audit payload и concurrent terminal replay без повторного fee release; expected-transfer тест сохраняет первое provisional observation после verified replay. Рабочая БД не обновлялась. Временный тестовый контейнер этапа 94 удалён. Общий integration gate вне этих suites не объявляется полностью зелёным.

Этап 95 реализован 07.11.2026 без новой миграции, схема 92. Unsigned withdrawal cancellation сверяет исходные reason/actor для всего набора lines; remainder допускает credit только по полностью новому cancellation cohort, а pending evidence требует reconciliation без частичного refund. Sweep expiry проверяет audit payload до изменения budget/allocation status и откатывает предшествующий authorization update при коллизии. Portal approval/rejection и прямые cancellation entry points получают полный request/execution advisory → scopes → wallets prefix до parent/line rows; resource set проверяется ещё раз после ожидания. Изменение контракта: AML request resources и maintenance planning требуют read committed; cancellation/expiry требуют внешнюю compensation/maintenance transaction. [Контракт и дальнейший аудит](../reference/crypto-custody-writers.md#23-пакетная-отмена-withdrawal-и-sweep-expiry--этап-95).

Проверки 95 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint; полные `crypto_repo`, TRON/EVM funding integration suites и полный worker suite на отдельной свежей БД схемы 92. Новые race/PostgreSQL tests подтверждают отказ изменённым reason/actor с rollback authorization/status/releases, сохранение первого audit fact, unsigned terminal replay, отсутствие частичного refund при pending evidence, rollback enclosing cancellation и единственный credit при concurrent remainder. Настоящие конкурентные admin rejection commands создают одну компенсацию. Wait-edge tests проверяют wallet-before-parent и изменение fee-payer footprint во время ожидания scope; stale outer snapshot отклоняется. Временный контейнер этапа 95 удалён. Рабочая БД не обновлялась; общий integration gate вне этих suites не объявляется полностью зелёным.

Этап 96 реализован 07.11.2026 без новой миграции, схема 92. Resource-rental refund только нарастает в пределах gross: меньший или чрезмерный refund возвращает conflict. Charge/refund проверяют полную immutable provider/platform history до и после записи; orphan/pending facts требуют reconciliation без достройки истории. Exact replay сохраняет header/period/expiry/первый close; terminal status не переоткрывается через held. Financial writes блокируют provider account до rental, требуют read committed; allocation settlement блокирует rental до allocation и проверяет исходные usage/outcome/network. [Контракт и изменения совместимости](../reference/crypto-custody-writers.md#24-resource-rental-charge-и-cumulative-refund--этап-96).

Проверки 96 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint; полный `crypto_repo` integration suite (465 с), TRON/EVM funding и полный worker suite на отдельной свежей БД схемы 92. Новые race/PostgreSQL tests проверяют concurrent charge/refund, regression 400→200→700, zero cost, сохранение первого header/period/close и отказ другому serial/delegation/gross/outcome. Коллизии provider/platform IDs откатывают весь новый набор; orphan pending facts требуют reconciliation. Wait-edge tests подтверждают account-before-rental и rental-before-allocation. После final receipt-network guard повторены rental race tests, затронутый worker allocation recovery, build/race/lint gates. Confirmed allocation replay сохраняет стоимость после refund и отклоняет другую сеть/usage. Временный контейнер этапа 96 удалён. Рабочая БД не обновлялась; общий integration gate вне этих suites не объявляется полностью зелёным.

Этап 97 реализован 07.11.2026 без новой миграции, схема 92. Legacy finalization получает общий prefix до fee writes, проверяет posted task basis и исходные physical/merchant debits. Recorded replay сверяет outcome/fee/usage и полный compensation set; treasury fee inverses проверяются целиком, pending/чужая история требует reconciliation без достройки. Resource reservation replay сравнивает owner/assets/purpose/quantity под rental→allocation locks. Broadcast replay проверяет signed effect/network/basis/event payload, сохраняет timestamps и не делает повторный UPDATE. Изменения контракта: read-committed внешняя transaction, строгий historical replay и conflict при другой resource quantity либо неполном audit. [Контракт и дальнейший аудит](../reference/crypto-custody-writers.md#25-legacy-compensation-и-resource-allocation-replay--этап-97).

Проверки 97 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint; финальный полный `crypto_repo` suite (514 с), TRON/EVM funding и полный worker suite на отдельной свежей БД схемы 92. Повторены затронутые worker settlements после fee/usage guard. Новые PostgreSQL/race tests проверяют concurrent legacy replay всех трёх типов, altered basis/outcome/fee/usage, отсутствие исходных debits, ambiguity merchant source, pending/foreign/partial compensation и rollback enclosing transaction. Wait edges подтверждают wallet-before-workflow и rental-before-allocation. Resource tests проверяют quantity/owner mismatch, immutable broadcast replay, wrong event rollback, signed recovery, valid historical recovered/no-op pair и отказ missing JSON fields рядом с valid history. После финального JSON guard повторены focused race/build/race/lint gates. Временный контейнер этапа 97 удалён. Рабочая БД не обновлялась; общий integration gate вне этих suites не объявляется полностью зелёным.

Этап 98 реализован 08.11.2026, ожидаемая схема 93. Activation selector сначала читает кандидатов с доступными native asset и payer; внутри savepoint получает task/operation advisories → pool policy → полный scope/wallet footprint → activation row. NOWAIT/try locks пропускают занятого кандидата, освобождая весь частичный prefix. После ожидания повторно сверяются eligibility и footprint; создание task и claim выполняются отдельными SQL statements, ready capacity учитывает уже начатые задачи. Budget reservation, state updates и coordinator используют тот же порядок; retired signing attempt получает prefix до fee release. Terminal replay сохраняет исходные timestamps, failed reason и defer deadline; запоздалый success старой failed-задачи не скрывает новый workflow.

Схема 93 запрещает удаление activation task, изменение ID/network/wallet/created_at и замену/снятие установленной operation link. Link обязана указывать на exact network/target/task basis. Upgrade останавливается на несовместимой существующей identity, без автоматического исправления истории. Activation commands требуют read committed. Resource release проверяет исходный normalized reason/event под provider account → rental → allocation locks; cleanup после валидного terminal rental close остаётся no-op без выдуманного release event. [Контракт и совместимость](../reference/crypto-custody-writers.md#26-activation-selectorsstate-и-resource-release--этап-98-схема-93).

Проверки 98 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint; полный `crypto_repo` integration suite (542 с), TRON/EVM funding и полный worker suite (81 с) на отдельной свежей БД схемы 93. Новые PostgreSQL/race tests проверяют busy advisory/policy/scope/wallet/task и освобождение partial prefix при открытой outer transaction, отсутствие payer/native без task/wallet mutation, ready capacity, defer/terminal/failed/superseded replay, read-committed planning, SQL identity/link guards и сохранение history при Down/Up, отказ upgrade с неверным legacy target. Wait-edge tests подтверждают policy/wallet-before-task и rental-before-allocation. Resource release проверен на concurrent replay, изменённый reason, event collision rollback и parent close без выдуманного allocation event. Временный контейнер и migration runner удалены. Рабочая БД не обновлялась; общий integration gate вне этих suites не объявляется полностью зелёным.

Этап 99 реализован 08.11.2026, ожидаемая схема 94. Закрыт rental maintenance fencing из остатка этапа 98 и продолжен P3 lock-order audit. Каждый claim получает новый UUIDv7 token, включая повтор имени worker. Renewal, reclaiming/held, financial close и release проверяют token и wall-clock lease после provider account → rental locks в read-committed transaction. Устаревшая попытка не снимает новую lease и не проводит refund. Обычные activation/recovery/close commands также отклоняют live maintenance lease, включая legacy lease без token. Held сохраняет прежнюю lease как retry delay; release/close инвалидируют token. Сервис проверяет claim перед внешней работой и после Reclaim; RPC выполняются вне transaction.

Изменения совместимости: миграция 94 добавляет только operational `maintenance_claim_token`, сохраняя все старые leases и financial history. При выкладке требуется остановить workers прежней версии: они не проверяют token. Новые workers ждут истечения старой lease и затем получают новый token. Internal repository/adapter ports передают явный claim; публичные HTTP DTO и financial owners не меняются. DB fencing защищает запись результата; перекрытие внешних Reclaim при истечении lease во время RPC требует корректного повторного вызова провайдера с тем же order reference. [Контракт](../reference/crypto-custody-writers.md#27-fencing-rental-maintenance--этап-99-схема-94).

Проверки 99 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint; финальный полный `crypto_repo` integration suite (525 с) на свежей БД схемы 94, TRON/EVM funding (64/4 с) и полный worker suite (86 с) на отдельной свежей БД. PostgreSQL/race tests проверяют concurrent acquisition, stale/expired/missing token для всех maintenance commands, refund/state preservation, unclaimed recovery conflict, новый token того же worker, held retry delay, wall time внутри старой transaction, read-committed requirement, account-before-rental wait edge и сохранение legacy lease при Down/Up. Service tests проверяют expired queued claim, потерю claim во время Reclaim и guarded terminal close. Первый полный прогон выявил clock race в testnet AML fixture; due переведён с DB NOW() на Go clock, без sleep/retry и без production AML изменений. После исправления AML/fencing профиль прошёл три повтора с race detector и весь `crypto_repo` suite. Временный контейнер и migration runner удалены. Рабочая БД не обновлялась; общий integration gate вне этих suites не объявляется полностью зелёным.

Этап 100 реализован 08.11.2026, ожидаемая схема 95. Закрыт отдельный пункт P3 activation cost-repair fencing. Claim выдаёт новый `cost_claim_token`, task передаёт его в preflight/held/guarded completion. До RPC claim проверяется и продлевается; после workflow/operation advisories → policy → scopes/wallets → activation row повторно проверяются token, wall-clock lease, network и exact linked transaction. Устаревший RPC не снимает новую lease и не проводит fee. Held retry delay начинается от фактического времени записи, включая длинную outer transaction. Completion проверяет claim до fee settlement/posting и отзывает token атомарно с cost projection.

Authoritative `FinalizeActivation` остаётся отдельным finality/reconciliation путём и может завершить исходный документ при активной repair lease. Его completion также отзывает token, поэтому поздний held/repair больше не меняет recorded cost. Сохранены независимые fee proofs, единственный `CryptoNetworkOperation` owner и exact replay model 1/2. Схема 95 добавляет только operational column/constraint и сохраняет legacy leases без token до expiry; для выкладки остановить прежние activation workers. Public HTTP DTO и financial owners не меняются. [Контракт и проверки](../reference/crypto-custody-writers.md#28-activation-cost-repair-fencing--этап-100-схема-95).

Проверки 100 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint; финальный полный `crypto_repo` integration suite (534 с) на свежей БД схемы 95, TRON/EVM funding (47/4 с) и полный worker suite (80 с) на отдельной свежей БД. PostgreSQL/race tests проверяют concurrent acquisition, stale/expired/missing token, changed network/hash, wall-clock lease и held retry timing внутри старой transaction, read-committed requirement, policy/payer-before-task wait edges и сохранение legacy lease при Down/Up. Service tests подтверждают preflight до RPC и guarded completion/held; реальные fee tests — отсутствие financial writes старого token, единственный owner/movement set нового token, отзыв token authoritative finality и exact model 1 success/revert replay. После финального retry guard повторены focused/build/race/lint checks. Первый полный прогон выявил несовместимое сравнение migration snapshots схем 95 и 92 при ожидаемом отказе upgrade; тест теперь строго сравнивает снимки одной схемы, сохраняя проверки всех полей. Migration/fencing tests и полный `crypto_repo` suite повторены успешно. Временный контейнер и migration runner удалены. Рабочая БД не обновлялась; общий integration gate вне этих suites не объявляется полностью зелёным.

Этап 101 реализован 08.11.2026, ожидаемая схема 96. Закрыт основной activation execution claim fencing: selector возвращает token, actor preflight проверяет token/owner/wall-clock lease и task/network/wallet/payer/native identity после общего prefix, затем продлевает lease. Typed claim updates защищают signing/held/failed/defer/external-active. State/dispatch/factual publication требуют коротких read-committed transactions; unclaimed actor commands отклоняют живую lease. Authoritative finality/active projection остаются отдельными путями.

Изменён механизм резервирования: `ReserveActivation` атомарно резервирует pool slot и network fee в одной serializable transaction, сохраняя isolation contract существующего FeeBudget. Public pool-budget command также требует serializable; вложение в read-committed transaction отклоняется. Prefix для резерва: workflow/operation advisories → pool policy → полный custody scopes set → network fee policy → wallets → activation row. Ошибка fee reserve откатывает pool slot; replay сохраняет единственный fee reserve. Остальные actor commands сохраняют read committed.

Guarded authorization API проверяет claim при publication/renewal и перед authorize/sign/broadcast dispatch, сохраняя внешние signer/RPC вызовы вне transaction. Схема 96 добавляет immutable dispatch intent по original authorization/phase/claim, с одним sign dispatch на authorization. До Sign intent фиксируется durable; неопределённый ответ не запускает второй Sign и не освобождает fee reserve, даже когда signed bytes/hash ещё отсутствуют. Actor failed/external-active и generic activation fee release также отклоняют signing exposure. Ошибочный/rejected broadcast после Sign остаётся held до evidence/reconciliation вместо освобождения по transport label.

Вернувшиеся JWS/signed bytes сохраняются для исходной frozen authorization после смены lease; activation signed publication берёт общий prefix до authorization row без проверки текущего actor. Accepted/observed known transaction восстанавливает metadata исходных fee/auth и связывает исходный CryptoNetworkOperation только по persisted signed identity либо exact existing link. Первый evidence binding меняет только link/broadcast time/modification time, сохраняя новый token/owner/lease/status; повтор binding — no-op. Поздний receipt после timeout восстанавливает fee metadata до settlement; CNO остаётся единственным expense owner. [Контракт, ограничения и совместимость](../reference/crypto-custody-writers.md#29-activation-execution-fencing-и-dispatch-recovery--этап-101-схема-96).

Совместимость: остановить старые activation workers перед выкладкой. Кастомные authorization adapters должны поддержать guarded API; обход без guard для нового activation dispatch отклоняется. Token не входит в frozen authorization/fingerprint. Неопределённый Sign требует late result/reconciliation, автоматического повторного Sign нет. Audit не переписывается; rollback до 95 после появления dispatch facts запрещён и требует forward recovery. Legacy leases/financial history/HTTP DTO сохраняются; рабочая БД не обновлялась.

Проверки 101 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint. Финальные полные PostgreSQL/race suites после signed-publication prefix и совместного serializable резерва: `crypto_repo` (565 с), TRON/EVM funding (33/4 с) на свежей БД схемы 96 и worker (67 с) на другой свежей БД. Focused профиль (32 с) проверяет stale commands/budget/dispatch, один concurrent Sign, immutable audit/отказ Down, signing exposure без bytes/hash, late signed/broadcast recovery и единственный CNO fee set. Настоящий FeeBudget проверяет rollback pool slot при отказе fee policy, success/replay одного reserve и отказ внешней read-committed transaction. Wait edges подтверждают policy/payer до task, network fee policy до wallet и wallet до authorization publication. Service tests моделируют lease loss во время control/family Sign и broadcast, сохранение результата без нового send и отсутствие внешнего I/O внутри transaction. Временный контейнер с его тестовым volume и migration runner удалены. Рабочая БД не обновлялась; общий integration gate вне этих suites не объявляется полностью зелёным.

Этап 102 реализован 08.11.2026 без новой миграции, схема 96. У существующего CryptoNetworkOperation metadata/protection updates и FOR UPDATE теперь получают тот же workflow/operation advisory → pool policy → scopes/wallets → activation row prefix, что activation finality. Общий helper вынесен в PostgreSQL layer без зависимости document repository от outbound repository; selector и serializable activation reserve сохраняют прежний контракт. Набор ресурсов включает текущие source/target, предлагаемые wallets и все historical custody movement wallets; после ожидания повторно проверяются footprint и workflow binding. Изменение набора требует retry без metadata mutation.

Post/unpost/repost и posted deletion mark получают prefix до PostingEngine и reversal balances. User mutation берёт блокировки до повторного чтения manual-edit permission и immutable fields; предварительная admin-role проверка сохраняется до DB work. Allow/Deny editing также перечитывают состояние после prefix. Точный protection replay не увеличивает version и не меняет actor lease/financial state. Legacy manual repost сохраняет один net fee; действующие ограничения model 2 не ослабляются. Новая схема, financial owner, movements или HTTP DTO не добавлены. [Контракт и совместимость](../reference/crypto-custody-writers.md#30-cryptonetworkoperation-metadataprotection-и-posting-prefix--этап-102).

Проверки 102 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint. Новый PostgreSQL/race профиль (19 с) и финальные полные suites: `crypto_repo` (563 с), TRON/EVM funding (32/4 с) на свежей БД схемы 96 и worker (68 с) на другой свежей БД. Реальные wait edges подтверждают workflow-before-operation, policy-before-wallet и wallet-before-document для metadata/row/post/unpost/repost/deletion mark, включая historical wallets. Изменение footprint во время ожидания отклоняет запись; protection replay сохраняет actor state, permission проверяется после ожидания, настоящий legacy repost сохраняет один net fee. Временный контейнер с его тестовым volume и migration runner удалены. Рабочая БД не обновлялась; общий integration gate вне этих suites не объявляется полностью зелёным.

Этап 103 реализован 08.11.2026, ожидаемая схема 97. Закрыт ограниченный участок P3: повторные сообщения о проблемах сети и повторное финансовое сторно CryptoPayment. До сторно проверяются текущая версия проведения, исходное основание документа и полный набор исходных движений по кошельку, мерчанту и processing fee. Блокируются реальные текущие/исторические wallets до payment row; набор перечитывается после ожидания. Прямой financial reorg command требует read committed. Старый callback не может сторнировать новое проведение; частичная ручная компенсация сохраняется, сторнируется только остаток.

Миграция 97 добавляет immutable `request_basis` для новых reg_crypto_reorg_compensations. Основание фиксирует документ, версию проведения, владельца/сеть/кошелёк/токен, сумму и fee snapshot, исходный блок и существенные данные сообщения об изменении истории. Повтор сверяет всё основание, исходный incident и observed block; затем проверяет полную компенсацию всех трёх регистров. Одна запись audit без завершённых движений больше не считается успешным сторно. Confirmations, время доставки и обновлённый proof ID не меняют финансовое основание. Инцидент хранит первое наблюдение, отдельные поколения сторно — собственные основания; оба payment incident writers проверяют владельца payment без перезаписи первого payload.

Одинаковое operational network incident сообщение сериализуется по детерминированному ключу и ищет JSONB-equivalent legacy record. Исходные ID/hash/details/status сохраняются, history дубликатов не удаляется автоматически. Повтор не создаёт новый incident/hold из-за порядка полей и не открывает разрешённую ранее блокировку заново. Canonical key с чужим payload отклоняется. Общий audit других operational holds и security incident kinds этим не закрывается.

Совместимость: старые compensation rows остаются с NULL request_basis; их replay проверяет сохранённые incident/block поля, актуальное основание документа и полный финансовый результат, без автоматической дописки history. Новые SQL inserts без basis version 1 запрещены; UPDATE/DELETE audit запрещены. Остановить прежние workers перед выкладкой. Down после новых basis-bearing facts требует forward recovery. Новый автоматический механизм сторно признанных платежей не включается: действующая проверка доказательств по-прежнему удерживает такие случаи для расследования. Public HTTP DTO и financial owners не меняются. [Технический контракт](../reference/crypto-custody-writers.md#31-повторы-инцидентов-и-финансового-сторно-платежей--этап-103-схема-97).

Проверки 103 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint. После окончательного SQL insert guard прошли focused PostgreSQL/race профиль (44 с), полный `crypto_repo` (608 с), TRON/EVM funding (54/10 с) на свежей БД схемы 97 и полный worker suite (92 с) на другой свежей БД; отдельные worker-проверки нового поколения и частичного сторно (3 с). Подтверждены concurrent exact replay, changed/foreign/stale basis, отказ пустому audit без движений, сохранение resolved hold, semantic legacy incident lookup, SQL immutability/обязательный basis, Down/Up с сохранением старых rows и отказ Down после новых facts. Временный контейнер с его тестовым volume и migration runner удалены. Рабочая БД не обновлялась; общий integration gate вне этих suites не объявляется полностью зелёным.

Этап 104 реализован 08.11.2026 без новой миграции, схема 97. Закрыта проверка повторных команд общего OperationalHold API. Повтор постановки сверяет target/owner/reason/held_by и весь JSONB details, включая resolved history, и не делает UPDATE. Другой actor/reason/details отклоняется. После завершения owner новое ограничение на другой target требует нового owner; поздний callback не расширяет уже снятый набор. Для кошельков вне merchant_pool allocation hold по-прежнему не создаётся, неизвестный wallet отклоняется.

Снятие сначала проверяет и блокирует весь owner cohort в network/wallet tables. Все ранее снятые записи должны иметь те же resolver/reason; противоречие отклоняется до изменения любой таблицы. Exact terminal replay не меняет timestamps, совместимый legacy mixed cohort может завершить оставшиеся open rows с первоначальным resolution audit. Чужие owners сохраняются. Общий owner advisory берётся без ожидания, чтобы caller с уже заблокированными business rows/wallet не создавал новое встречное ожидание; busy возвращает conflict для rollback/retry в свежей transaction. Команды требуют read committed и сами координируют transaction.

Исправлен producer сверки остатков: первое immutable observation определяет hold details, новые run/delta сохраняются самостоятельными observations и не меняют первоначальный запрет. Missing initial evidence требует reconciliation вместо выдуманного основания. Repository port дополнен InitialBalanceIncidentObservation. Схема, движения денег и публичные HTTP DTO не меняются. [Контракт и границы](../reference/crypto-custody-writers.md#32-повторные-команды-постановки-и-снятия-запретов--этап-104).

Проверки 104 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint; focused PostgreSQL/race профиль (23 с), worker-профиль сверки/восстановления (5 с). Полные итоговые suites: `crypto_repo` (602 с), TRON/EVM funding (48/3 с) на свежей БД схемы 97 и worker (86 с) на другой свежей БД. Подтверждены exact/changed acquisition и resolution replay, проверка всего mixed cohort до первого UPDATE, сохранение других owners и денежных остатков, owner contention без встречного ожидания, concurrent acquire/resolve, isolation и сохранение первоначального hold при новых balance observations. Временный контейнер с его тестовым volume и migration runner удалены. Рабочая БД не обновлялась; общий integration gate вне этих suites не объявляется полностью зелёным.

Этап 105 реализован 08.11.2026, ожидаемая схема 98. Защита operational hold перенесена также на прямые SQL insert/update/delete, включая публикацию кошельков, AML preflight, pool scope/high triggers и baseline release. Общий SQL owner gate сохраняет точный ключ Go-протокола этапа 104, не ждёт и требует read committed. Exact insert replay сохраняет первую запись и timestamps даже после resolution; другой acquisition payload, NULL в обязательных полях и недопустимый lifecycle отклоняются. Resolution сверяет существующий owner audit сразу в network/wallet tables, не позволяет менять ID/основание, переоткрывать resolved record или добавлять target завершённому owner. Exact terminal SQL update также не переписывает время.

Изменён механизм pool/preflight identifiers: новые scope-baseline owner содержат wallet+token, high-risk — wallet+token+исходную risk revision, preflight — wallet+token+исходное observation. Это исключает объединение разных кошельков только по token ID. Пока hold открыт, новые версии проверки не меняют его первое основание; после закрытия новое независимое high-risk finding/новое preflight observation получают отдельный lifecycle, а повтор того же finding не открывает старый. Источники проверяются по актуальной scope/risk записи и неизменяемому preflight observation. Legacy token-only identifiers и первые details сохраняются без массового backfill.

Миграция 98 добавляет функции/триггеры, без новых денежных регистров, движений и HTTP DTO. Existing baseline readiness proofs продолжают проверяться до release. История не переписывается; противоречивое legacy основание требует расследования. Down после новых wallet-bound lifecycle facts запрещён; до их использования Down/Up сохраняет данные. Перед выкладкой остановить старые workers/controllers, особенно старый preflight writer. [Технический контракт](../reference/crypto-custody-writers.md#33-прямые-sql-запреты-и-отдельные-основания-кошельков--этап-105-схема-98).

Проверки 105 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint. После окончательного NULL guard прошли focused PostgreSQL/race профиль (47 с), полный `crypto_repo` (622 с), TRON/EVM funding (53/8 с) на свежей БД схемы 98 и worker (93 с) на другой свежей БД. Дополнительно прежний pool/provisioning/operational-hold профиль прошёл за 35 с. Проверены exact/changed/NULL SQL replay, immutable ID/history, resolution cohort/время, старый Go owner key и nonblocking SQL contention, независимость wallets, first scope/risk/preflight source, новые findings после resolution, legacy Down/Up snapshot и отказ Down после новых lifecycle facts. Временный контейнер с его тестовым volume и migration runner удалены. Рабочая БД не обновлялась; общий integration gate вне этих suites не объявляется полностью зелёным.

Этап 106 реализован 08.11.2026, ожидаемая схема 99. Отмена заявки на создание кошелька сравнивает исходные reason/cancelled_by и сохраняет всю первую запись, включая время. Повтор с другой причиной или ответственным возвращает conflict. SQL trigger проверяет то же основание до ON CONFLICT, не пропускает NULL/пустые обязательные поля и сериализует отмену в порядке custody policy → отсортированные routing scopes → intent. Команда требует read committed; запрет отмены после резервирования derivation сохраняется. При откате внешней операции отмена также откатывается.

Подтверждение наблюдения за адресом — сохраняемое первое наблюдение, а не неизменная команда каждого обновления. При действующих полномочиях новый проход/новый исполнитель не переписывает прежние start_height, lease_owner, fencing_token и acknowledged_at; впервые добавленный адрес получает собственную границу. Неверные сеть/актив/тип наблюдателя, старый token и истёкшая lease отклоняются даже для пустого списка. Lease row удерживается на время всей записи; приложение и SQL trigger проверяют database wall clock после ожидания, а не время начала transaction. Неподходящие адреса и кошельки вне включённого автоматического пула по-прежнему не создают coverage.

Миграция 99 заменяет две trigger functions без изменения сохранённых данных, финансовых движений и HTTP DTO. Up/Down/Up сохраняет старые записи; Down снимает новые проверки, поэтому для выкладки и отката остановить прежние workers/controllers. Изменено существующее поведение: устаревшее подтверждение наблюдения теперь возвращает ошибку вместо молчаливого отсутствия записи. [Технический контракт](../reference/crypto-custody-writers.md#34-отмена-заявки-на-кошелёк-и-первое-подтверждение-наблюдения--этап-106-схема-99).

Проверки 106 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint; focused PostgreSQL/race профиль новых команд и preflight (13 с). Итоговые полные suites: crypto_repo (594 с), TRON/EVM funding (28/3 с) на свежей БД схемы 99 с timeout 30m; worker (82 с) на другой свежей БД. Подтверждены exact/changed/NULL cancellation replay, одновременные Go/SQL команды, scope-before-intent wait edge, enclosing rollback, immutable audit, Up/Down/Up без переписывания cancellation, first address coverage при refresh/смене lease, самостоятельная граница нового адреса, stale/foreign identity и истечение lease во время ожидания через Go и прямой SQL. Первый общий Go-прогон упёрся в запрет локальных HTTP-портов; полный повтор с доступом к ним прошёл. Первый длительный PostgreSQL-прогон остановлен для увеличения timeout; итоговый прогон выполнен на новой БД. Временный контейнер с тестовым volume и migration runner удалены. Общий integration gate вне названных suites не объявляется полностью зелёным; рабочая БД не обновлялась.

Этап 107 реализован 08.11.2026, ожидаемая схема 100. Новое решение AML сохраняет evaluated_at с точностью PostgreSQL. Повтор доставки отчёта использует тот же immutable report/policy и исходное время оценки: класс, причина и expires_at должны совпасть. Продления срока и замены результата при старении отчёта нет. Противоречие возвращает conflict, не удаляет элемент очереди и не меняет историю. Старые решения с NULL evaluated_at сохраняются как прежний результат; время оценки не восстанавливается по created_at и не дописывается. SQL INSERT guard сверяет полный повтор, подтверждает report/subject/job generation/current policy и границы срока; первоначальную классификацию рассчитывает Go Policy.Assess, SQL guard не дублирует эту бизнес-логику. Находящийся в БД новый результат проверяется Go до подтверждения очереди.

Сверка счёта теперь хранит изменяемую проекцию текущего случая отдельно от первого основания security incident. Новые episode_id, initial_details и incident_id фиксируют первый сбой и ссылку на инцидент. Два неудачных наблюдения создают один инцидент; дальнейшие обновляют только счётчик/последнюю ошибку, сохраняя первоначальные details и acknowledgement. Устранение закрывает только текущий случай, повтор сохраняет первое время закрытия. После устранения первое новое наблюдение начинает отдельный случай со счётчика 1; второе создаёт другой инцидент, прежний не открывается и не переписывается. Обе команды выполняются в transaction; внешний rollback отменяет и проекцию, и инцидент. Денежных движений они не создают.

Go и SQL используют общий неблокирующий invoice gate до проекции/инцидента. Raw SQL triggers не ждут invoice, если уже держат дочернюю строку; busy требует rollback/retry. AML INSERT также не ждёт занятый routing scope. Guards защищают исходные поля, связи invoice/network/incident, timestamps и terminal status; DELETE invoice drift history запрещён. Legacy активный инцидент с прежним invoice-only key используется без изменения его первого payload. Неизвестные initial_details/episode не выдумываются. Противоречивая старая пара open projection/resolved incident отклоняется для разбора, а не открывает закрытую историю.

Совместимость: миграция 100 добавляет nullable evaluated_at и три поля текущей сверки, без backfill, финансовых движений и HTTP DTO. Перед применением остановить старые crypto workers: прежние writers не передают новые обязательные основания. Down оставляет additive evaluated_at, чтобы сохранить время оценки; повторный Up использует его. После использования новых invoice episodes/links/history Down запрещён, нужен forward recovery. До их использования legacy Up/Down/Up сохраняет историю. [Технический контракт](../reference/crypto-custody-writers.md#35-первое-решение-проверки-риска-и-история-расхождений-счёта--этап-107-схема-100).

Проверки 107 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint; focused PostgreSQL/race профиль окончательных AML/invoice guards (14 с), прежние resource/sweep upgrade сценарии (15 с). Итоговые полные suites: crypto_repo (627 с), TRON/EVM funding (44/7 с) на свежей БД схемы 100 с timeout 30m и worker (78 с) на другой свежей БД. Проверены late/concurrent AML replay без изменения expiry, changed/NULL SQL payload, отказ новому решению без clock, несовпадающий сохранённый результат с сохранением queue, scope busy, retention clock при Down/Up и legacy NULL outcome. Invoice проверки покрывают threshold/reset, first basis/acknowledgement, независимые episodes, exact terminal replay, outer rollback, raw identity/link/lifecycle guards, общий Go/SQL NOWAIT gate, чужой invoice, legacy history и partial-reopen conflict, отсутствие денежных движений и forward-recovery gate. Временный контейнер с тестовым volume и migration runner удалены. Общий integration gate вне названных suites не объявляется полностью зелёным; рабочая БД не обновлялась.

Этап 108 реализован 08.11.2026, ожидаемая схема 101. Новые preflight jobs сохраняют initial_policy_version/initial_scope_version; ID, wallet/token/policy и первое время создания неизменяемы. Текущие версии правил остаются состоянием очереди и могут меняться при следующей попытке, не переписывая исходную регистрацию. Каждая новая lease получает append-only reg_crypto_aml_preflight_attempts с полным basis: job/policy/merchant/network/wallet/token, версии, адрес, asset, worker, начало и конец lease. Transaction с новым claim не коммитится без этой записи. Старый token нельзя использовать повторно даже до его первоначального срока окончания.

Save и Retry сверяют весь переданный basis с зарегистрированным. Общий SQL helper сначала получает полный набор custody scopes/wallet, затем job и (для Save) policy, после чего проверяет lease по clock_timestamp(). Busy не создаёт встречного ожидания, требует свежего retry. Save дополнительно проверяет текущие enabled policy/scope; отказавшая попытка может освободить только собственную действующую lease, даже если настройки уже изменены. Старый callback не снимает lease новой попытки. SQL observation guard требует соответствующую попытку и свежий proof, включая полный asset snapshot; прежняя quorum/baseline/history validation сохраняется. Неденежный baseline и запрет при обнаруженном остатке по-прежнему не создают приход денег.

Создание очереди отделено короткой transaction от claim. Пакет до 100 новых jobs исключает уже существующие, чтобы первые 100 повторов не вытесняли следующие кошельки. Claim просматривает до 100 кандидатов, получает ресурсы до job row и повторно проверяет eligibility; busy candidate откатывается в savepoint. RPC остаётся вне SQL transaction. Enqueue collision сверяет неизменяемую policy-привязку, а новые версии правил не считаются новым заданием.

Очередь активации — подсказка о работе, не повторное исполнение имеющейся task. Полная готовность проверяется уже при выборе сети, затем заново для того же wallet после custody/scopes/wallet locks. Ранее первая неподготовленная сеть могла возвращать false до обработки готовой другой сети. Новый fixed target устраняет эту остановку; существующий activation ID/state не меняется, queued=false не означает новое исполнение. ID новой task генерируется в Go, conflict ограничен unique active wallet identity. Новых адресов, signing/broadcast, fee reserve или денежных движений эта очередь не создаёт.

Совместимость: миграция 101 добавляет attempt register, два nullable initial version поля и guards. Legacy начальные версии и отсутствующие attempt snapshots не восстанавливаются. Старые observations сохраняются; FK на попытку добавлен NOT VALID только для исторических rows, новые записи обязаны иметь источник. Прежняя processing lease без snapshot не может публиковать результат; после её истечения новый worker создаёт первую доказанную попытку. Противоречивые legacy policy/provision связи требуют разбора. До новых registrations/attempts legacy Up/Down/Up сохраняет данные; после них Down запрещён, нужен forward recovery. Перед upgrade остановить старые workers; публичные HTTP DTO не меняются, внутренний AMLPreflightJob дополнен PolicyID. [Технический контракт](../reference/crypto-custody-writers.md#36-основания-попыток-проверки-и-очередь-активации--этап-108-схема-101).

Проверки 108 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint. Итоговый focused PostgreSQL/race профиль (13 с) проверяет basis/identity, expiry, policy changes, reuse token, commit без attempt, busy locks, legacy migration и activation queue/wait edge между двумя TRON-сетями. Прежний preflight/pool/hold профиль прошёл за 20 с. Итоговые полные suites: crypto_repo (656 с), TRON/EVM funding (57/25 с) на свежей БД схемы 101 с timeout 30m и worker (99 с) на другой свежей БД. Временный контейнер с тестовым volume и migration runner удалены. Общий integration gate вне названных suites не объявляется полностью зелёным; рабочая БД не обновлялась.

Этап 109 реализован 09.11.2026, ожидаемая схема 102. Доработан существующий механизм чтения блоков TRON. В sys_crypto_block_ingestion добавлены registration_basis и activation_basis: первое начало пробного чтения сохраняется отдельно от первой финансовой активации. Повтор не обновляет первое основание и время. Новый финансовый mode без activated_at отклоняется и при прямом SQL INSERT/UPDATE; противоречивый legacy financial mode без времени активации блокирует Up до разбора. После активации start height/hash/timestamp и activated_at неизменяемы; pause/resume и переход solidified ↔ hybrid сохраняют их. Новая голова при повторе — подсказка, а не разрешение передвинуть активную границу. Противоречивый hash/timestamp того же блока отклоняется; первая активация не может начинаться раньше регистрации. Shadow downgrade после финансовой активации запрещён. Переключение на новую учётную модель P12 остаётся отдельной незавершённой работой.

EnsureCutover проверяет реальную сеть и financial asset identity/scale для всех переданных assets до изменения режима; native asset также проверяется, если передан в NetworkRef. Поздняя ошибка всего пакета откатывает регистрацию и режим. При уже существующей записи Go сначала читает её под lock, а raw SQL INSERT replay сверяет исходную регистрацию до ON CONFLICT. Для hash допустима разница регистра hex-символов; первый сохранённый текст не переписывается. Сохраняются прежние decoder/quorum/capture проверки.

Watch target хранит первого owner/start/source/time. При том же owner более поздний boundary/source — обычное обновление списка, которое не меняет историю; другой owner отклоняется. UPDATE/DELETE target запрещены. Существенная identity наблюдаемого wallet/token в каталоге фиксирована: ID/network/address или contract/standard/decimals нельзя менять задним числом. Деактивация и deletion_mark разрешены с сохранением истории. При создании нового активного owner для уже занятого адреса/контракта срабатывает индекс каталога либо first-owner guard; прошлый owner не заменяется молча.

Общий неблокирующий network gate сериализует регистрацию, включая ещё отсутствующую ingestion row, и используется catalog callbacks. Он требует read committed также для прямых SQL writers: старый snapshot не должен скрывать только что зарегистрированного watch owner. Ensure получает network metadata, только владельцев новых watch targets (full scopes/wallet prefix), token metadata, затем ingestion row. Повторная регистрация уже известных владельцев не блокирует весь пул. Callback с уже занятой catalog row не ждёт network gate и откатывается при busy; это исключает встречное ожидание. TRON driver повторяет только ErrIngestionRegistrationBusy в свежей SQL transaction с отменой по context; RPC не повторяется внутри DB и не выполняется под locks. Постоянное противоречие не превращается в бесконечный retry.

Совместимость: миграция 102 проверяет существующие owner/key связи и ранее сохранённые basis; несовместимая история требует разбора до upgrade. Legacy NULL bases остаются NULL; финансовая граница legacy active row фиксируется по известной текущей записи, без восстановления потерянной shadow history. Новых денежных движений, регистраторов или HTTP DTO нет. Перед upgrade остановить старые workers, включая catalog writers. Down до новых activation_basis сохраняет additive basis columns и восстанавливает прежние callbacks; Up проверяет retained history. После новых financial activation facts Down запрещён, нужен forward recovery. [Технический контракт](../reference/crypto-custody-writers.md#37-начальные-блоки-и-владельцы-наблюдения--этап-109-схема-102).

Проверки 109 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint. Итоговый focused PostgreSQL/race профиль (55 с) проверяет основания, прямые SQL повторы, ошибки metadata, first watch owner, busy/reverse wait, legacy/Down/Up, отсутствие activation time и stale Repeatable Read snapshot. Прежний capture/shadow профиль и TRON driver retry прошли (7/2 с). Итоговые полные suites с окончательной миграцией 102: crypto_repo (699 с), TRON/EVM funding (49/4 с), worker (91 с); repository/funding и worker использовали разные свежие одноразовые БД, timeout 30m. Документированные read-only preflight запросы проверены на тестовой БД. Временный PostgreSQL-контейнер, его тестовый volume и migration runner удалены. Общий integration gate вне названных suites не объявляется полностью зелёным. Рабочая БД не обновлялась.

Этап 110 реализован 09.11.2026, ожидаемая схема 103. В reg_crypto_external_outflows добавлено nullable initial_basis: первое известное основание включает сохранённые wallet/token IDs, нормализованные и исходные адреса/контракт, сумму, block number/hash, confirmations/required floor/finality и DB clock. Новая запись получает основание от DB, а не из переданного JSON. Первые identity, тексты адресов, сумма, время и JSON защищены от UPDATE. Raw INSERT с тем же effect сохраняет первую строку без обновления времени; несовпадение effect отклоняется до ON CONFLICT. Legacy NULL не backfill: нынешняя проекция не выдаётся за первоначальное наблюдение.

Upsert сначала проверяет ранее сохранённого owner, затем для нового effect разрешает текущие catalog IDs. Деактивация wallet/token или поздний alias не перенаправляют прежнее событие на новый owner. Первоначально неизвестный token остаётся неизвестным; его admission не угадывается. Полный source payload проверяется также перед incident и observation writes, включая from/to/contract/amount, status, run и исходные IDs. reg_crypto_external_outflow_observations получает incident_id; новые SQL observations обязаны ссылаться на согласованные incident/run, старые NULL сохраняются.

Block height/hash/confirmations/finality меняются согласованно. Старое наблюдение остаётся в журнале, но не подставляет свой hash/confirmations в более новый блок. Для того же блока подтверждения монотонны; новая inclusion начинает собственный счёт и finality. Неизвестный hash не стирает известный; один известный hash на другой высоте отклоняется. Required confirmation floor не снижается. Существующий reorg state не открывается автоматически: его исходный block tuple сохраняется, новые сообщения остаются отдельными observations. Решения по повторному появлению после закрытия и весь reorg/re-inclusion workflow ещё требуют отдельного аудита.

Общий read-committed try-prefix использует прежний tx-hash gate expected matcher и полный scope/wallet набор до outflow/incident rows. Он подключён к detection, GetExternalOutflow(forUpdate) и обоим custody adjustment decisions. SQL trigger из уже занятой строки не ждёт обратного ресурса. ErrExternalOutflowBusy означает откат попытки и повтор в свежей transaction; API получает conflict. Chain observation worker сохраняет исходный payload, откладывает на 5 секунд, освобождает lease и возвращает потраченную попытку, не создавая ложный needs_review. Постоянное payload-противоречие не считается busy.

Совместимость: перед upgrade остановить старые API/workers/writers. Новые денежные движения, HTTP DTO и включение P4/P12 не добавляются. Down 103 сохраняет additive source columns и разрешён только до новых initial_basis/incident links; после них требуется forward recovery. Legacy данные и ранее проведённые суммы не переписываются. Старый тест rollback guard схемы 85 теперь проверяет этот guard на UpTo(85), чтобы более ранний запрет 103 не подменял его проверку. [Технический контракт](../reference/crypto-custody-writers.md#38-первоначальные-сведения-внешних-списаний--этап-110-схема-103).

Проверки 110 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint. Итоговый focused PostgreSQL/race профиль (69 с) проверяет basis/clock, raw replay, stale и новую inclusion, confirmation floor, retirement/alias, source DTO/incident links, busy/reverse wait, legacy Up/Down и прежний custody-loss workflow. Дополнительный сквозной worker/repository профиль (2/4 с) проверяет типизированный busy, десять повторов без исчерпания attempts и однократное завершение после освобождения wallet. Полные suites на свежих одноразовых БД схемы 103: crypto_repo (709 с), TRON/EVM funding (39/6 с), итоговый worker с busy defer (96 с); repository/funding и worker использовали разные БД, timeout 30m. Временный PostgreSQL-контейнер, его тестовый volume и migration runner удалены. Общий integration gate вне названных suites не объявляется полностью зелёным. Рабочая БД не обновлялась.

Этап 111 реализован 09.11.2026, ожидаемая схема 104. Остаток P3 разделён на восемь конечных пакетов приёмки; поимённые индексы содержат кандидатов Go и фактические публичные SQL routines/trigger bindings свежей одноразовой БД. Это не число обнаруженных ошибок или необходимых исправлений. Журнал отмечает конкретные проверенные функции, source hash и тесты; неприменимые пути требуют явной классификации. Проверка полноты и общих зависимостей остаётся P3-R8, а новые хозяйственные функции сохраняют границы P4–P12.

Миграция 104 добавляет classification_basis/resolution_basis в outflow, decision_basis в adjustment и nullable decision_basis в существующий append-only action audit. Новые decision UUID генерируются Go; DB принимает только seed ID и сама фиксирует полный command, source snapshot и wall-clock момент. Legacy NULL сохраняются. Классификация сравнивает весь actor/reason/evidence/classification/matched basis; approval/rejection — adjustment/actor/reason и вид решения; resolution — outflow/actor/reason. Exact replay возвращает сохранённый факт с текущим состоянием карточки без новых audit, posting, request/outbox или hold writes. Новый actor, reason, evidence либо другой вид решения не становятся успехом по занятому ключу.

История нового решения не зависит от сегодняшней пригодности сети для нового действия: повтор уже проведённого approval возвращает прежний результат без повторного финансового admission. Новая классификация после отказа разрешена один раз из unauthorized_loss в matched_basis/false_positive_or_reorg; точный повтор принятой reclassification безопасен. Устаревшее первое решение и произвольное редактирование уже переклассифицированного решения отклоняются. Новый случай после закрытия относится к P3-R2, а не к редактированию текущего основания.

SQL BEFORE guards фиксируют command/source/clock и защищают в том числе legacy terminal fields от переписывания. AFTER trigger пишет один audit с тем же decision ID; Go больше не дублирует эти INSERT. Прямой audit INSERT должен точно совпасть с сохранённым parent decision. Deferred guards требуют audit и, для resolution, выбранные check/run с неизменившимися данными, точный resolved incident и полный исходный hold cohort с теми же actor/reason. Hold owner try-gate исключает добавление targets между snapshot и release. Изменение выбранной сверки до commit откатывает решение, audit, incident и holds целиком. Clock нового rejection возвращается из DB, вместо приблизительного time.Now в Go.

Совместимость: остановить старые API/workers/writers до upgrade. Raw decision UPDATE теперь требует seed нового Go UUID, terminal decision/audit не редактируется. Уже принятое legacy решение без basis не получает выдуманную возможность semantic replay. Новые records/DTO денежных операций не вводятся, старые суммы не перепроводятся. Down сохраняет additive columns и разрешён только до новых decision facts; после них требуется forward recovery. [Контракт](../reference/crypto-custody-writers.md#39-основания-и-повторы-решений-внешних-списаний--этап-111-схема-104).

Проверки 111 прошли: build, полный Go race suite, production/integration lint (0 issues), 436 HTTP contracts, TypeScript и ESLint. Итоговый focused PostgreSQL/race профиль (39 с) проверяет полные command bases, exact и изменённые повторы, JSON field order, approval/rejection/reclassification, immutable clocks/audit, exact reconciliation/incident/hold cohort, rollback при изменении сверки до commit и legacy Up/Down. Полные suites на свежих одноразовых БД схемы 104: crypto_repo (729 с), TRON/EVM funding (45/4 с), worker (80 с); repository/funding и worker использовали разные БД, timeout 30m. Индекс Go воспроизводим, SQL definitions/bindings не изменились после полного прогона, hash всех 21 R1 review entries проверены. Временный PostgreSQL-контейнер, тестовый volume и migration runner удалены. Общий integration gate вне названных suites не объявляется полностью зелёным. Рабочая БД не обновлялась.

Ближайшая очередь оставшегося аудита P3 после этапа 111:

| Проверяемый участок | Что требуется подтвердить |
|---|---|
| Закрытые и повторно появившиеся внешние списания | Проверить отдельную историю эпизодов, смену canonical/reorg evidence после классификации и закрытия; не считать прежнее решение доказательством для нового появления |
| P3-R4–R8 — общий остаток | Закончить проверку parent commands, metadata/maintenance, общих механизмов, комбинаций и полноты по конечной таблице и индексам |
| Остальные прямые SQL/parent commands и audit actions внешних списаний | После early prefix этапа 110 проверить полные source/action payloads и оставшиеся caller prefixes до hold commands, включая raw incident/decision writers |

Эта очередь ссылается на конечную таблицу P3-R1–R8. Каждый кандидат получает отметку о применимости и evidence; новые найденные пути учитываются проверкой полноты R8. Наличие строки в индексе не означает обнаруженную ошибку.

В терминах хозяйственных операций завершена защита существующего сторно и постановки/снятия запретов, включая перечисленные прямые SQL writers. P3 остаётся открытым для остальных metadata/reorg/maintenance writers и caller/SQL prefixes, других incident writers и полного payload audit ON CONFLICT, в том числе оставшихся административных заявок и родительских commands до обращения к hold; отмена pool intent и first watch coverage проверены этапом 106, решения AML и история invoice drift — этапом 107, preflight attempts и activation queue — этапом 108, существующая ingestion registration и watch ownership — этапом 109, initial outflow evidence/paired block/owner replay и busy defer — этапом 110, решения R1 — этапом 111. Оставшиеся проверки ограничены пакетами R2–R8 конечной таблицы; их закрытие ещё не доказано. Legacy ambiguity и operator forward recovery не исправляются автоматически. Далее по основному функциональному плану остаются P4–P12 и итоговая приёмка P13; незакрытые правила/структуры P0–P2 включаются в соответствующие работы. Автоматический intake, attribution и cutover не реализованы полностью; рабочая БД не обновлялась.

Приёмка: watcher-before-settlement и settlement-before-watcher дают одинаковые balances. Одновременные payment/CLI/backfill не могут дважды принять один приход; internal transfers дают нулевое изменение суммарного principal в managed wallets.

Результат 88: proposal замораживает basis и выбранные source lines; approval проверяет текущее поколение и остаток компенсации. DB deferred guards требуют exact reversals либо custody + variance, финальное решение и отдельный accounting outbox одним коммитом. Компенсация ссылается на исходного владельца effect и не создаёт нового principal recognition. Изменён порядок блокировок этого writer: source document advisory → scope/wallet → payment row → adjustment row; общий аудит остальных writers остаётся следующим пунктом P3. API/карточка показывают accountingModelVersion, basisKind, sourcePostingId. Старые решения/суммы не переписываются; Down после нового model 2 proposal запрещён. Полное/частичное сторно CryptoPayment v1 по-прежнему компенсирует физический gross вместе с net/fee; будущая attribution correction через suspense без custody относится к P4/P5 и здесь не включается.

Проверки 88: build, полный Go race suite, production/integration lint, 436 HTTP contracts, TypeScript и ESLint; полный PostgreSQL crypto_repo suite и повтор worker suite на отдельной свежей БД; partial/repeated/concurrent compensation, stale generation, exact set omissions, hash/recorder spoofing, indexed legacy adoption и upgrade history. Общий integration run выявил отдельный конкурентный сбой `TestWorkerReliabilityScheduledRunPersistence/all` по `uq_automation_runs_processing_rule`; его изолированный повтор прошёл, код планировщика здесь не менялся. Поэтому общий integration gate не объявляется полностью зелёным. Рабочая БД не обновлялась.

## 8. P4 — независимый intake и payment attribution

Новые предлагаемые пакеты: documents/crypto_custody_receipt, registers/crypto_receipt_suspense, cryptoapp/receipt_intake_service.go, cryptoapp/receipt_attribution_service.go.

Уже реализован внутренний ReceiptIntakeService: единый коммит custody + suspense + recognition + intake audit + generic posted outbox; стабильные movement line IDs; replay по occurrence. Он работает только в maintenance-границе схемы 78. Следующие пункты относятся к полноценному intake и attribution, поэтому остаются открытыми.

- [ ] Обрабатывать verified supported managed receipts до no-allocation/wrong-token/dust/retired matching exits.
- [ ] Сохранить быстрый provisional inbox и отдельную durable очередь finality/canonicality для receipt без payment.
- [ ] Создавать custody + suspense при successful final receipt; matching failures не отменяют этот факт.
- [ ] Unsupported assets направлять в technical journal; token admission выполняет контролируемый replay исходного effect.
- [ ] Обычный matched receipt → full payment attribution выполнять одним коммитом при готовых данных.
- [ ] CryptoPayment v2 создаёт только suspense expense, merchant net и processing fee.
- [ ] Partial command проверяет remaining/reserved, token, owner, proof generation и fee assessment revision.
- [ ] Fixed/min fee считать один раз на согласованную assessment scope; повторные portions начисляют только дельту.
- [ ] Не пересчитывать historic fee snapshot при ручной идентификации или reorg.
- [ ] Обновить invoice received/confirmed/credited projections и selection: chain amount не суммируется по количеству allocations.
- [ ] Late payment не открывает terminal invoice; wrong token не конвертируется в ожидаемый.
- [ ] Ввести receipt-aware reorg и ownership correction до включения активного intake.
- [ ] Доказанная orphaning коммитит invalidation/holds и durable compensation job даже при невозможности немедленного financial reversal; partial ledger set не допускается.

Приёмка: normal payment даёт прежний net/fee; unidentified даёт только custody/suspense; partial attribution не меняет custody. Повтор confirmation, replay и manual decision не дают второго net/fee.

## 9. P5 — AML inventory и доступность для всех режимов

Изменяемые места: crypto_repo/aml_inventory.go, aml_routing.go, aml_balance.go, outbound_repo/aml_operating_balance.go, новые migrations поверх views/guards 65–76, batch/source planners.

- [ ] Заменить зависимость physical.recorder_type=CryptoPayment на проверенный receipt/recognition link; legacy compatibility оставить явной.
- [ ] Добавить unattributed ownership до merchant credit; поддержать partial ownership transitions без нового physical inventory.
- [ ] Разделить treasury/operating/refund-reserved; не записывать unknown principal как service fee.
- [ ] Атомарно инвалидировать source/risk revision при новом verified external effect до допуска новой подписи/отправки.
- [ ] Переиспользовать existing subject/generation/report и paid-budget: receipt не создаёт повторный screening.
- [ ] Добавить owner-aware platform/unknown-owner adapter без фиктивного merchant scope.
- [ ] Подключить бухгалтерскую availability guard и при AML disabled: suspense/refund/provider-transit не расходуются обычным sweep/withdrawal.
- [ ] Сохранить conservative wallet/token classification и existing batch execution revisions/dispatch permits.
- [ ] Учесть merchant-owned custody в returns и operating funding; независимые owner не объединяются.
- [ ] Право на merchant attribution не снимает risk/financial holds.
- [ ] Покрыть поздний High после Low, новый dust во время signed preparation и receipt на collector.

Приёмка: ledger balance может увеличиться, а spendable остаётся прежним/закрытым по policy; credited merchant balance и risk availability показываются раздельно. Existing low/medium internal transfer не расходует unattributed portion.

## 10. P6 — block-bound сверка

Изменяемые места: BalanceObservation, crypto_worker/reconciliation.go, WorkerRuntimeRepo, chain balance ports/drivers, movement provenance и historical projections.

- [ ] Добавить height/hash, finality, read source/time, accounting watermark и completeness в observation/run.
- [ ] Реализовать per-family block-bound balance read либо явно доказанный stable window; capability публиковать в readiness.
- [ ] Подготовить ledger projection на том же checkpoint, включая opening и explicit manual basis.
- [ ] Дождаться полного ingestion/accounting watermark перед точным сравнением.
- [ ] Отделить processing_lag/stale/incomparable/read_error от подтверждённого balance_mismatch.
- [ ] Backfill искать исходные effects; observation delta сама не проводит receipt.
- [ ] Добавить сверку ownership partitions/reservations и provider/external positions.
- [ ] Выполнять повторную успешную сверку после corrections; снимать только hold своего incident owner.

Приёмка: приход или расход между загрузкой ledger и RPC-чтением не создаёт ложной денежной корректировки. Неполный historical RPC не помечает сверку successful. В отчёте видно, на каком блоке доказано равенство.

## 11. P7 — журнал расследования, команды и публичные проекции

Изменяемые места: handlers/inbound_review.go, DTO/contracts registry, internal/content, frontend inbound-reviews, Portal/merchant read models, OpenAPI и SDK consumers.

- [ ] Журнал показывает все unresolved причины: no_allocation, wrong_token, timestamp_before_lease, retired, additional, dust, liquidity receipt и conflicts.
- [ ] Разделить received/accounted/unallocated/reserved/risk/review states в API.
- [ ] Добавить карточку receipt: chain proof, accounting movements, доступный остаток, decisions, reservations, related documents и audit.
- [ ] Пример целевых команд: prepare-merchant-credit, prepare-treasury-classification, prepare-refund, approve, reject, request-evidence.
- [ ] Команды используют command.Executor, Idempotency-Key и expectedVersion; prepare возвращает immutable intent и предпросмотр, approve привязан к его fingerprint.
- [ ] Для public payment добавить receiptId/chainAmount/attributedGrossAmount/allocationId и документировать amount semantics v2.
- [ ] Изменить invoice aggregation, Portal totals и webhooks без двойного chain credit; обычный full payment остаётся совместимым.
- [ ] Partial merchant attribution включить после проверки OpenAPI/SDK contract; legacy webhook consumer не получает неоднозначные суммы.
- [ ] Связанный проведённый документ автоматически определяет credited/refunded/classified outcome.
- [ ] not_attributable означает завершённый review, но открытый финансовый остаток; ageing продолжает работать.
- [ ] Сохранить исторические ручные outcomes и HTTPS references как evidence, не объявляя их проверенными проводками.
- [ ] Entity form metadata/RLS/FLS и permissions не открывают клиенту приватные keys/provider credentials/чужие merchant receipts.

Приёмка: оператор может пройти от chain event до конкретной проводки и назад. Он не может закрыть 1 000 USDT как refunded одной текстовой ссылкой. UI показывает отдельно 1 000 chain / 400 attributed / 600 unresolved.

## 12. P8 — казначейские основания и невостребованные суммы

Новые предлагаемые пакеты: documents/crypto_treasury_receipt_classification, registers/crypto_external_position, registers/crypto_treasury_source, registers/crypto_platform_other_income.

- [ ] Создать external account catalog с proof of control, asset mappings, counterparty/contract и statements.
- [ ] Собственный CEX transfer закрывает external asset/in-transit; отсутствующий opening не подменяется equity.
- [ ] Loan/contribution/receivable recovery/rebate имеют разные typed basis и встречные movements.
- [ ] Возврат staking principal отделить от reward; unsupported chain evidence требует adapter work до автоматического admission.
- [ ] Классифицировать внешний operating top-up до допуска к native gas budget.
- [ ] Treasury classification не меняет merchant payable и не снимает wallet/token risk.
- [ ] Создать ageing queue, срок review и evidence requirements; денежного auto-writeoff по таймеру нет.
- [ ] Income recognition/transfer to authority включать только при готовой legal policy и independent approval.
- [ ] Прочий доход учитывать отдельно от processing fee и provider refund.

Приёмка: неизвестный перевод на treasury остаётся suspense; собственный перевод не повышает капитал; заём повышает payable; правовое отсутствие основания оставляет aged amount в suspense.

## 13. P9 — возврат до атрибуции и новые signing capabilities

Новые предлагаемые пакеты: documents/crypto_receipt_refund_request, documents/crypto_refund_execution, cryptoapp/receipt_refund_service.go, outboundapp/refund.go. Существующие: signing purposes, money movement intent/approval, custody policy, оба Vault plugins.

- [ ] Реализовать receipt portions → refund payable и независимые reservations.
- [ ] Сохранять claimant evidence и проверенный destination; from_address не является default approved destination.
- [ ] Сформировать immutable intent с source principal/gas, owner, max fee, network/token, destination и refund basis.
- [ ] Добавить отдельный signing purpose/basis и approval bundle; определить новую version/capability без переопределения старых signed bytes.
- [ ] Обновить SQL/application authorization и TRON/EVM Vault проверки, cached signature path и golden vectors.
- [ ] Добавить preflight matrix server/worker/plugin capabilities; новый purpose на старом plugin закрыт.
- [ ] Сохранить owner-based principal/gas funding: platform-paid expense не разрешает расход чужого fee wallet или principal.
- [ ] Переиспользовать reserve/build/sign/persist/broadcast/finality recovery, но не merchant debit-first/withdrawal fee generators.
- [ ] Successful principal уменьшает custody и refund payable; reverted сохраняет payable и записывает фактический gas; unknown удерживает reservations.
- [ ] Отмена до необратимой отправки компенсирует payable в suspense; partial execution закрывает только выполненные portions.
- [ ] Первое включение — platform-paid gas. Fee withholding реализовать отдельной policy/quote и тестами до его разрешения.
- [ ] Refund after merchant attribution направлять в merchant workflow; один receipt portion не потребляется обоими путями.

Приёмка: возврат без merchant не создаёт merchant ledger строк; нельзя отправить сумму на другой адрес под старым approval; процесс восстанавливается после сбоя после sign/broadcast без второй отправки.

## 14. P10 — provider capabilities и корректный rental lifecycle

Изменяемые места: resource_provider.go, crypto/resources/itrx/adapter.go/client.go, resource rental manager/workflow, provider balance observations и operation approvals.

- [ ] Проверить документированные и доступные конкретному аккаунту Balance/Statement/FundingReceipt APIs; зафиксировать limitations и способ получения originals.
- [ ] Не использовать наблюдение balance из нового платного заказа как независимую бесплатную сверку.
- [ ] Разделить adapter status accepted/paid/partially_delegated/active/closed/failed; response Place сам не доказывает ready resource.
- [ ] Проверить реальную гарантию unique out_trade_no; unknown create восстанавливать query, без слепого повторного Place.
- [ ] Сохранить vendor raw evidence, serial/client key, charge/refund cumulative totals, timestamps/sequence и decoder policy version.
- [ ] Ввести provider charge → service pending → delivered expense; charge+delivery вместе допустимы в одном коммите.
- [ ] Перевести непосредственные SQL Activate/Close и model generators на один settlement owner.
- [ ] Partial delivery/refund применять по доказанным quantities/prices либо удерживать в pending; не угадывать cost split.
- [ ] Убрать silent refund clamp; decreases/conflicts/refund above charge создают incident и separate correction.
- [ ] Аналитическое распределение net cost учитывать для failed/успешных allocations и unused остатка, не создавая второй OPEX.
- [ ] Opening ограничить доказанной baseline position; operational mismatch не даёт повторный opening.
- [ ] Создать независимую reconciliation queue по statement/checkpoint или verified import, с stale/incomparable состояниями.

Приёмка: есть затраты при failed parent operation, если ресурс был оказан/потреблён; order timeout не создаёт вторую аренду; re-read refund не увеличивает balance повторно; несовпадение открывает incident без автоматического P&L.

## 15. P11 — provider funding, cash refund и автопополнение

Новые предлагаемые пакеты: cryptoapp/provider_funding_service.go, outboundapp/provider_funding.go, provider transfer/transit repositories; optional ExternalProviderAccountStatements port отдельно от rental API.

- [ ] Подготовить verified destination/account binding и change-control для его изменения.
- [ ] Создать CryptoProviderFunding с platform inventory principal reservation, gas budget, limits, independent approval и persisted intent.
- [ ] Добавить provider_funding signing purpose/capability и соответствующую Vault проверку; treasury withdrawal с расходом fee balance не переиспользовать без изменения его семантики.
- [ ] Chain success проводит wallet expense + provider transit; credit evidence проводит transit expense + provider prepayment.
- [ ] Поддержать partial credits, provider deposit fee и mismatch без второго wallet расхода.
- [ ] При отсутствии API credit receipt реализовать verified statement import; user booleans и разница общего balance не являются proof.
- [ ] Provider cash refund проводить через return transit и собственный incoming receipt matching, без platform income.
- [ ] Проверить source owner и отсутствие расхода merchant principal/suspense/refund payable.
- [ ] Auto-funding policy учитывает available/pending/committed, один active intent, daily/per-operation caps и pause при incident.
- [ ] Повторное падение видимого balance при pending credit не создаёт серию переводов.
- [ ] Unknown chain или vendor outcome удерживает исходный intent и требует lookup; restart не сбрасывает идемпотентность.

Приёмка: 5 000 TRX отправлены, 4 900 credit подтверждены → transit 100, provider 4 900, wallet principal уменьшен на 5 000 ровно один раз; комиссия сети отдельно. Недоказанный остаток transit не списан автоматически.

## 16. P12 — инвентаризация истории, backfill и переключение

### 16.1. Manifest и classification истории

- [ ] Создать resumable dry-run command с tenant/network/checkpoint, manifest digest, cursors и counts/sums.
- [ ] Разделить legacy full/partially reversed payments, orphan/reincluded generations, unmatched/retired/dust, manual credits/refunds, wallet funding CLI, sweeps/JIT и provider openings/charges/refunds.
- [ ] Для каждой observed effect identity найти все существующие custody/merchant/fee/variance lines и ручные review references.
- [ ] Изменённый log.Index сопоставить по verified receipt alias; неоднозначность требует review.
- [ ] Уже проведённый legacy effect принять в registry ссылками на исходные строки без новой custody, suspense или fee.
- [ ] Legacy financial facts остаются v1; metadata adoption не переписывает recorder/version/даты.
- [ ] Unmatched с доказанным отсутствием финансового учёта провести v2 после проверки manual corrections.
- [ ] Ручное начисление/возврат без строгой chain связи не считать автоматически ни дублем, ни свободным эффектом; поместить в migration conflict.
- [ ] Необъяснённый opening balance импортировать отдельным утверждённым opening manifest с unattributed ownership, без выдуманного TX hash и без автоматического Low.
- [ ] Старые отрицательные provider balances и unexplained delta сохранить как evidence; не скрывать opening задним числом.

### 16.2. Cutover

1. Развернуть expand schema, readers, guards и shadow comparison; новые команды выключены.
2. Обновить совместимые server/worker и, для новых outgoing capabilities, оба Vault plugins; проверить readiness.
3. Выполнить dry-run/backfill manifest и устранить duplicate/ownership conflicts.
4. Для tenant/network остановить новые money commands/address allocations и новые отправки; продолжать ingestion, lookup и final settlement сохранённых signed intents.
5. Доказать отсутствие необъяснённых in-flight effects либо закрепить их за legacy posting owner до terminal outcome. Unknown signed operation нельзя отменить ради cutover.
6. Зафиксировать chain/accounting watermark и построить сопоставимую custody/ownership/provider сверку.
7. Атомарно зафиксировать cutover generation и выбор model для новых economic effects. Уже созданный pending v1 payment не получает параллельный v2 receipt.
8. Включить новый intake/attribution/availability только согласованным набором. Оставить legacy recovery для закреплённых v1 facts.
9. Возобновить commands и наблюдать canary; refunds/provider sends включать своими capabilities после приёмки.

Критерий: существующие balances не меняются от adoption уже учтённых фактов. Любое изменение от backfill объясняется конкретным ранее пропущенным receipt/компенсацией и присутствует в manifest.

### 16.3. Восстановление

Shadow можно выключить без финансового эффекта. До активного v2 допустим rollback только совместимых readers/binaries по preflight.

После v2 writes: остановить новые commands/dispatch, сохранить ingestion и recovery исходных signed операций, исправить совместимым binary или forward migration. Не удалять новые движения, не сбрасывать cutover, не запускать старый payment poster.

Down-migration, удаляющая v2 financial history, запрещена после активации. Up/Down/Up проверяется на disposable fresh schema; сценарий действующей истории использует forward upgrade и rollback guard.

## 17. P13 — тестирование и приёмка

Это перечень требуемых проверок всей функции. Пройденные unit/integration tests identity foundation описаны в §1; они не означают прохождение финансовых сценариев A01–A40.

| ID | Сценарий | Ожидаемое доказательство |
|---|---|---|
| A01 | Полный normal payment | Один receipt, net/fee прежние, invoice completed только после credit |
| A02 | No allocation | Custody/suspense есть, merchant/fee отсутствуют |
| A03 | Wrong supported token | Учтён фактический актив, ожидаемый актив не зачислен |
| A04 | Before lease/late/retired/additional single | Историческая attribution корректна, terminal invoice не открыт снова |
| A05 | Supported dust | Raw units сохранены; budget скрининга не меняет сумму |
| A06 | Unsupported/spam admission | Только technical journal, после onboarding один financial receipt |
| A07 | Partial credit + refund remainder | Conservation capacity; custody при attribution не меняется |
| A08 | Две partial credit команды | Сумма не выше remaining; fixed/min fee один раз |
| A09 | Одновременные credit/refund/classification | Только допустимые portions; проигравший получает conflict |
| A10 | Watcher/internal sweep/JIT/CLI race | По одному controlled-side recognition, net internal principal 0 |
| A11 | Несколько logs и разные RPC | Нет склейки разных effects и нет дублей одного |
| A12 | EVM re-inclusion с другим log.Index | Одна logical identity, новая occurrence/generation по proof |
| A13 | Payload conflict той же occurrence | Incident/hold, никакого второго прихода |
| A14 | Reorg без payment | Receipt компенсирован; suspense/inventory не остаются доступными |
| A15 | Reorg после partial reversal/credit/refund | Нет over-compensation; исполненные sends остаются; deficit виден |
| A16 | Unattributed при AML disabled/enabled | Обычный outbound не расходует portion в обоих режимах |
| A17 | Новый High/unknown на collector | Старый dispatch permit инвалидирован, ownership credit сохранён |
| A18 | Receipt attribution при unknown | Обязательство отражено, расходование ограничено независимо |
| A19 | Чужой custody merchant | Нельзя перепривязать owner или использовать foreign gas/source |
| A20 | Refund address substitution | Старое approval не проходит; claimant TX hash недостаточен |
| A21 | Refund reverted/unknown/restart | Payable не исчезает, gas учтён, второй send отсутствует |
| A22 | Treasury own exchange/loan/equity | Разные counterpart effects; own transfer не увеличивает капитал |
| A23 | Ageing без legal basis | Review создан, income отсутствует |
| A24 | Provider charge до delivery | Prepayment уменьшается, pending service растёт, OPEX ещё нет |
| A25 | Shared rental и failed tx | Expense один, allocations/unused дают net total |
| A26 | Partial/duplicate/conflicting vendor refund | Корректная delta; conflict не скрыт clamp |
| A27 | Funding chain final, credit delayed/partial | Wallet один расход, transit до полного подтверждения |
| A28 | Provider cash refund приходит первым | Нет income/merchant credit, после match transit закрыт |
| A29 | Auto-funding workers и pending credit | Один intent, caps не обойдены |
| A30 | Сверка при concurrent chain/ledger change | Incomparable/lag или сравнение того же checkpoint |
| A31 | Legacy manual correction + inbox replay | Dry-run находит ambiguity; backfill не удваивает актив |
| A32 | Cutover с pending v1 и signed unknown | Единственный закреплённый owner; recovery сохраняется |
| A33 | Сбой между state/movement/audit/outbox | Полный rollback либо единый durable commit |
| A34 | RBAC/step-up/maker-checker/replayed command | Повтор безопасен, иной payload/actor не обходит policy |
| A35 | Старый Vault plugin/cached signature | Новый purpose закрыт без capability; cache не обходит owner/policy |
| A36 | DTO/API/webhook/Portal contract | Chain total не задвоен, partial semantics явные |
| A37 | Direct SQL финансового набора | FK/immutable/deferred constraints отклоняют неполный/чужой basis |
| A38 | Activation/withdrawal fee и два settlement writers | Реальный native fee учтён один раз, principal recognition независим |
| A39 | Reorg при невозможном немедленном reversal | Canonical invalidation/holds/job сохранены, нет частичных движений и доступного старого credit |
| A40 | Токен возврата отличается от gas | Нет вычитания raw units разных активов; fee bearer и custody source проверены отдельно |

Проверки выполняются последовательно по затронутым пакетам:

- Domain unit/property tests: exact amounts, fee allocation, FSM, conservation, reversals и accounting identity golden vectors.
- PostgreSQL integration: реальные UNIQUE/FK/deferred constraints, конкуренция workers, cutover, savepoint/rollback, immutable guards.
- Targeted race tests для A08–A10/A15/A29/A32, повторение повышается при выявленной гонке или перед release acceptance.
- HTTP contract и generated DTO/Zod/SDK checks, frontend tests и Playwright для prepare/approve/conflict/retry.
- Отдельные tests в Go modules TRON/EVM Vault plugins и mixed-version preflight.
- Disposable schema-76 snapshot → forward upgrade → dry-run → resumable backfill → сверка → cutover rehearsal.
- Nile и Sepolia: реальные receipt/finality и persisted-envelope recovery. Реальный iTRX account: charge/delegation/query/refund и подтверждённый способ funding credit, с лимитированным budget.

Существующие команды проекта: make test-unit; интеграционные проверки запускаются через go run ./cmd/integration-test либо целевые go test -tags=integration с disposable database. Секреты и production DSN не записываются в отчёт приёмки.

## 18. Наблюдаемость и критерии включения

Добавить метрики и отчёты:

- Final verified effects без recognition, возраст очереди, duplicate/conflict counts.
- Suspense amount/count/age по сети/активу/custody scope; отдельно reserved и unresolved.
- Chain/custody watermark lag, incomparable observations, confirmed mismatches.
- Ownership coverage, source/risk revision conflicts, заблокированные dispatch.
- Refund payable age, unknown signed/broadcast outcomes, удержанные reservations.
- Provider prepayment/transit/service pending, statements freshness, unmatched charge/refund и реальные net costs.
- Backfill progress, manifest checksum, legacy/v2 generations и суммы adoption/new postings.

Критерии go-live:

- [ ] Нет необъяснённых duplicate recognitions и активных миграционных financial conflicts.
- [ ] Каждая final chain custody сторона имеет ровно одного posting owner либо явный incident.
- [ ] Сверка на общем checkpoint успешна по включаемым wallet/token; все отклонения имеют basis и owner.
- [ ] Неатрибутированные суммы учтены и не доступны обычному outbound.
- [ ] Reorg и partial resolution не превышают исходные source lines.
- [ ] Нельзя выполнить новый purpose на несовместимом Vault.
- [ ] Provider funding не включён без доказуемого credit mechanism.
- [ ] Нет timer-based income и автоматической денежной подгонки balance.
- [ ] Canary прошёл обычные и отказные сценарии, recovery не создаёт повторных отправок.
- [ ] Для всех A01–A40 приложены evidence либо явно закрытая capability; полное покрытие требует прохождения всех включаемых функций.

## 19. Обновление документации после реализации

- [ ] Обновить systems/crypto-processing.md и systems/posting-engine.md по фактическому поведению.
- [ ] Переписать актуальные разделы crypto-processing-architecture.md о intake, review, provider balance и reconciliation; не сохранять неподтверждённое утверждение о standalone iTRX Balance.
- [ ] Обновить crypto-adjustment-v2-design.md с границей attribution/custody correction и legacy v1.
- [ ] Обновить wallet-address-reuse.md, canonical contracts, AML inventory/resources/outbound/risk-balances и merchant custody.
- [ ] Обновить METADATA.md, HTTP/OpenAPI/SDK, ROUTER.md и acceptance scenario matrix.
- [ ] Опубликовать operator runbook для investigation/refund/provider incidents и forward recovery.
- [ ] Сменить статус документов с предложения на реализованный только после соответствующих tests и acceptance.
