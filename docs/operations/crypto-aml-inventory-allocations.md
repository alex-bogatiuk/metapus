# AML: распределение principal и комиссии при свипах

Состояние на 26.10.2026. Миграция 00065 и `AMLRepository` добавляют durable распределение уже учтённого физического inventory на merchant net principal и processing fee. Поддержаны проверенные payment receipts и заранее спланированные внутренние свипы токенов одного мерчанта. Это основа нового outbound контракта; прежний signing/dispatch путь включённого AML scope по-прежнему закрыт.

Обновление схемы 82 (06.11.2026): sweep записывают principal только при final successful settlement каждой строки, с общим registry owner обеих controlled сторон. Allocation применяется по точным source/destination movement IDs и canonical occurrence этой строки. Общая сумма документа и завершение соседних строк не являются доказательством её principal. AML coverage использует те же точные links. Broadcast сохраняет резерв; final revert записывает только actual fee, завершает signing attempts как failed и отменяет allocation с аудитом в одной транзакции. Повторный merchant credit не создаётся. Native operating capital остаётся в resource journal; отдельная fee identity и sweep reorg recovery ещё не подключены.

## Деньги и их распределение

`reg_crypto_aml_inventory_facts` хранит неизменяемые факты с ссылками на financial basis, verified effect и exact physical movement IDs. Один payment создаёт один allocation fact; один зарезервированный внутренний перенос применяется один раз. Факт внутреннего переноса одновременно уменьшает principal/fee источника и увеличивает те же доли получателя. Новых merchant credits, fee income или физических проводок он не создаёт.

`reg_crypto_aml_inventory_positions` — производная component projection по wallet/token с immutable owner/network scope и revision. При создании пустого checkpoint появляется нулевая строка; это не объявляет текущий legacy balance чистым. Projection проверяется по сумме фактов, физическому ledger и исходным financial/chain evidence. Неизвестная часть остаётся недоступной в risk view.

Payment allocation допускается после согласованного проведения: actual merchant net + processing fee равны verified gross; совпадают payment/version, owner/network/token, physical receipt, canonical effect и proof. Текущий тариф не используется для переоценки уже проведённой комиссии. Поздний report не создаёт новый financial receipt.

## Резерв до подписи

`ReserveAMLInternalAllocation` принимает существующий `sweepAttemptId`, owner/network/token, source/destination, gross amount, ожидаемые inventory/risk/scope revisions и класс low/medium. Это внутренний repository command; административного или merchant HTTP endpoint пока нет.

Команда блокирует routing scope, затем inventory rows в порядке wallet IDs. SQL guard повторно проверяет точную identity попытки, принадлежность адресов, pool → collector roles, accepted asset scope, отсутствие начатой авторизации, текущий покрытый risk class и остаток. На этом этапе поддержаны свипы ненативных токенов. Native operating capital и расходы gas учитываются отдельным [resource journal](crypto-aml-resources.md), миграция 00066.

Получатель должен иметь такое же подтверждённое low/medium inventory либо доказанный пустой checkpoint. Проверяется отсутствие нераспределённых canonical incoming effects, даже если физический ledger пока равен нулю. Другой мерчант, medium-остаток для low source, известный high и неподкреплённый unposted receipt не принимаются. Две зарезервированные поставки разных классов не могут занять один пустой collector. [Batch intake/capacity и lifecycle](crypto-aml-batches.md) подключены; [dispatch permit](crypto-aml-outbound.md) подключён к каждой границе authorize/sign/broadcast.

Доли рассчитываются по незарезервированным компонентам:

```text
principal = div(gross_to_transfer × available_principal, available_gross)
service_fee = gross_to_transfer − principal
```

Go использует `big.Int`, PostgreSQL — точный целочисленный `div(numeric,numeric)`. Обычная numeric division с последующим floor не используется: округление промежуточного результата может потерять единицу на границе NUMERIC(78,0). Неделимая единица остаётся в fee component. Все компоненты сохраняются, риск не усредняется.

Пример: из 100 gross = 90 net + 10 fee свип 50 переносит 45 net + 5 fee. Остаток источника — 45 + 5. Сохранённый split не пересчитывается из-за новых поступлений. Если уже после подписи добавилось 100 gross = 50 net + 50 fee, после исходного свипа источник содержит 95 net + 55 fee, получатель — прежние 45 + 5. Общий merchant principal равен 140, второй credit за свип отсутствует.

Повтор того же request возвращает исходный plan/status. Изменение amount или ожидаемых revisions в том же attempt не заменяет план. Reserve и безопасная отмена меняют inventory revision; старый запрос не может незаметно выбрать другие доли. Сам план не является approval или разрешением Vault/broadcast.

## Применение и восстановление

План переходит из `reserved` в `applied`, когда совпали:

- Signed expected effect этого attempt и canonical verified incoming effect на своём collector.
- Posted sweep/version, source line/tx/amount и обе физические стороны переноса.
- Exact source expense movement и destination receipt movement. Для общего batch receipt сумма применённых долей не может превысить его gross.

Одного tx hash или низкого score недостаточно. Chain proof без physical posting, posting без proof и неподходящий financial basis не двигают component projection. Все component updates и applied status выполняются в одной SQL-транзакции. Повтор job/proof не переносит доли второй раз. После реально состоявшегося перевода сохраняется исходный split, даже если риск уже повысился; оставшийся principal получает актуальный консервативный класс.

`reg_crypto_aml_inventory_queue` заполняется при изменении payment/sweep, financial movements, chain state и routing scope. Миграция ставит подходящий существующий intake в backlog. CryptoProcessor обрабатывает ограниченную порцию этой очереди до платных HTTP jobs. Неполная evidence не угадывается: следующий basis/proof update ставит запись в очередь снова.

Producer очереди и worker используют одну scope lock. Последний fee/net update не может потеряться из-за одновременного acknowledgement старой неполной записи. Привязка journal fact к incoming effect уникальна; один chain effect не покрывает несколько независимых receipt allocations.

Если component projection повреждена и применение нарушило constraint, savepoint откатывает все component writes и статус. Queue сохраняет `attempts`, код `allocation_evidence_conflict` и `next_attempt_at` через 30 секунд. Другие поступления продолжают обрабатываться. Финансовые движения при такой ошибке не откатываются и не повторяются. После исправления производной projection queued plan может примениться по тем же неизменяемым evidence.

## Отмена и неизвестный исход

`CancelAMLInternalAllocation` разрешён только для `reserved` плана, для которого ещё нет ни одной signing authorization. Требуются причина и оператор; cancellation fact неизменяем. Даже unsigned authorization уже может означать вызов подписанта в процессе, поэтому такой резерв не освобождается. Истечение attempt, timeout, смена класса или падение worker сами не возвращают компонентный бюджет.

SQL admission новой authorization сериализуется с отменой. Он проверяет plan/status, source/destination/key/asset/amount, срок попытки, актуальный risk revision и inventory coverage. После отмены или late high новая authorization отклоняется. Это дополнительная защита, не замена будущего risk/batch dispatch permit. Существующие authorizations и signed bytes сохраняются для recovery.

Доказанный on-chain failure после начала авторизации, компенсация reorg и reviewed correction allocation пока не снимают резерв автоматически. Для них нужен отдельный compensating workflow с доказанным исходом; текущий режим удерживает средства консервативно.

## Баланс по риску и границы этапа

[Portal risk balance](crypto-aml-risk-balances.md) читает principal из component projection. Проверяются все physical movements и canonical incoming effects после baseline, текущая валидность исходных allocation facts и их ancestors. Неподкреплённое incoming, orphaned effect, изменённый financial basis, пропавшая проводка или расхождение projection закрывают покрытие. Поздний high источника распространяется на остаток своего collector через существующие risk edges; нет обмена principal с другим мерчантом и усреднения score.

Пока внутренний план reserved, затронутые источники удерживаются в risk view. После применения точный net распределяется по обоим кошелькам, fee не попадает в merchant balance. Исторические transfers без предварительного плана не получают split по совпадению сумм. Ретроспективный подбор долей запрещён.

Native gas/funding поддержаны через отдельный [operating resource budget](crypto-aml-resources.md). Остаются: activation/resource-aware replacements, компенсирующие allocation facts, class/source withdrawal reservations, независимый автоматический baseline/activation и полный signing/dispatch permit. Legacy отправки AML asset всё ещё закрыты; автоматический production consolidation не включён. Рабочие БД не менялись, chain signing/broadcast и платные AML-вызовы при проверках не выполнялись. Rollback миграции при сохранённых facts/plans запрещён; используется forward migration.

## Проверка

Unit и изолированные PostgreSQL tests проверяют integer rounding и NUMERIC(78,0), частичный перенос net/fee без нового credit, неизменность split после нового тарифа, поздний high обоих остатков, replay, outer rollback, конкурентный idempotent reserve, запрет foreign/mixed-class destination, unposted incoming на формально пустом collector, unsigned cancellation/audit, запрет release после начала authorization и восстановление очереди после неудачного применения.
