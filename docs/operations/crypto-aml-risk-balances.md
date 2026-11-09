# AML: баланс мерчанта по риску

Состояние на 26.10.2026. `GET /portal/v1/balances/risk?merchant_id=<uuid>` возвращает один согласованный снимок net principal выбранного мерчанта. В обзоре кабинета добавлен блок «Риск средств» с опциональной таблицей «По риску». Выбранный вид хранится в пользовательских interface preferences (`portalRiskBreakdown`); скрытие таблицы не скрывает предупреждения о высоком/неизвестном риске и суммах, требующих проверки.

## Контракт и точность

Endpoint требует явный `merchant_id` из авторизованного merchant scope. Viewer может читать тот же собственный баланс; права на создание вывода этим endpoint не выдаются. Несколько мерчантов, сетей и токенов не объединяются в одну сумму. Запрос выполняется в read-only `REPEATABLE READ` транзакции, без внешних вызовов и финансовых записей. Более слабая внешняя SQL-транзакция отклоняется, а не ослабляет snapshot isolation.

Ответ содержит `snapshotId`, `asOf`, `freshUntil`, `items`. В каждом item есть merchant/network/token IDs, symbol/scale, состояние routing scope, признак полноты net allocation, причина ограничения, известная high exposure и суммы в minor units:

- `ledgerBalance` — signed sum существующих merchant financial movements. Материализованный balance сверяется с ним; расхождение закрывает свободную часть для этого представления.
- `reserved` — ещё не исполненный debit-first principal в potentially executable заявках, включая held/signing/broadcast. Физический source reserve не добавляется к нему второй раз.
- `pendingRestoration` — ещё не компенсированный debit закрытой failed/rejected заявки. Он виден как технически удерживаемая часть требования мерчанта.
- `total = ledgerBalance + reserved + pendingRestoration` — оставшийся net principal, включая незавершённые обязательства. Подтверждённый вывод больше не добавляется к total.
- `free`, `reserved`, `technicalHold` — взаимоисключающие части `total`. Ровно те же суммы разбиты на четыре `buckets`: low/medium/high/unknown. Для каждого bucket также выполняется `total = free + reserved + technicalHold`.
- `pendingChain` — отдельная сумма canonical unposted receipts до комиссии. Она не входит в total и не прибавляется к подтверждённому net.

Все денежные значения — точные строки целых minor units. Между разными активами нет сложения или преобразования через float. Processing fee исключена из merchant principal по фактическим финансовым movements, а не по текущему тарифу.

`snapshotId` — digest содержимого view, включая source revisions, scope version и незавершённые debit obligations. Это не approval token и не dispatch permit. `freshUntil` ограничен ближайшим сроком source evidence и максимумом 30 секунд; при чтении просроченный low/medium становится unknown даже до выполнения TTL worker. Кабинет обновляет снимок каждые 15 секунд и на expiry; устаревшая таблица скрывается, предупреждение о ранее известном high сохраняется. Ошибка загрузки не изображается как нулевой баланс.

## Какие источники уже распределяются точно

Текущий read model использует [durable inventory allocations](crypto-aml-inventory-allocations.md) по wallet/token с явным пустым AML checkpoint. Payment fact связывает согласованные financial basis/version, merchant net, processing fee, physical receipt и canonical verified effect. Заранее зарезервированный token sweep переносит сохранённые net/fee доли на своего collector, включая частичное расходование. Проверяются owner/network/token identity, совпадение projection с журналом, полное покрытие физических movements и canonical incoming effects, в том числе ancestors. Чужие wallets и shared legacy principal не подбираются для покрытия недостающей суммы.

Текущий класс берётся по всему wallet/token. Известный high не снимается по TTL. Отсутствующее покрытие или несовпавшая scope revision удерживают principal технически; обычный unknown score при доказанном inventory может иметь свободную часть для явного unknown export. Активный source/native fee reserve и незавершённый internal allocation plan удерживают затронутые источники; это консервативно и не увеличивает total. Queue allocation обрабатывается отдельно от paid provider jobs: до materialization net ещё не считается подтверждённо распределённым.

После исторического sweep без предварительного allocation plan, native funding/cost или неразобранной компенсации пока нет durable факта о том, какая часть net/fee была перенесена. Совпадение сумм не восстанавливает эту связь. Нераспределённый остаток показывается в unknown/technicalHold с причиной `source_allocation_incomplete`. Для таких операций нужен отдельный workflow; текущий endpoint их не угадывает.

Новые AML debit-first заявки имеют immutable class/source lines. Подтверждённые lines погашают principal; в reserved остаётся только непогашенная часть исходного debit. Частичные cancel/revert refunds не возвращают уже выплаченную сумму. Class-specific execution и история описаны в [outbound runbook](crypto-aml-outbound.md).

Исторические classless debit-first заявки не имеют утверждённой точной net/class/source allocation. При незавершённом дебете свободная часть всего asset scope закрывается в этом view: обязательство остаётся в reserved, остаток — technicalHold, класс распределения — unknown. `hasHighExposure` сохраняет предупреждение об известном high среди источников даже при такой неопределённости. Потенциально исполнимая заявка без подтверждённого financial debit требует сверки и тоже не оставляет свободного low. После доказанной компенсации и освобождения физических reserves источники снова рассчитываются по актуальным данным.

## Причины ограничения

| reason | Значение |
|---|---|
| `routing_not_enabled` | AML scope актива ещё не включён; нет достоверной разбивки по риску |
| `ledger_reconciliation_required` | Материализованный merchant balance расходится с financial movements |
| `withdrawal_reconciliation_required` | Потенциально исполнимая заявка не имеет непогашенного debit |
| `withdrawal_allocation_required` | Нет точной net/class/source allocation незавершённой заявки |
| `source_reconciliation_required` | Подтверждённые source allocations превышают merchant claim |
| `source_allocation_incomplete` | Часть net principal ещё нельзя точно привязать к источнику |
| `source_allocation_complete` | Net principal распределён; наличие резервов/актуальный класс проверяются по каждому source |

`free` означает отсутствие удержания в этом снимке и не разрешает отправку. Существующие signing/dispatch gates остаются обязательными. Включённые AML scopes всё ещё закрывают старый outbound контракт: class-specific withdrawal, high/unknown acceptance, activation, точные компенсации withdrawal lines, независимый автоматический preflight и dispatch permits подключены через [новый outbound contract](crypto-aml-outbound.md), миграции 00070–00073. Для durable inventory facts требуется миграция 00065; native coverage учитывает отдельный [operating capital и gas cost journal](crypto-aml-resources.md) миграции 00066. Operating gas не увеличивает merchant net. Execution hold своей партии учитывается как technicalHold даже при последнем low score; закрытый intake без execution hold сам по себе не блокирует остаток. Старые scopes не включаются от открытия этой страницы; рабочие БД не обновлялись.

## Проверка

Unit tests проверяют сохранение суммы на больших minor units, TTL, holds, повторные source IDs, неизвестные allocations и debit-first обязательства. PostgreSQL tests используют отдельную БД на сценарий: 100 gross → 90 net + 10 fee, повторные чтения без financial writes, late high, expiry до worker, один учёт request/source reserve, missing compensation, повреждённая balance projection, частичный spend и запрет более слабой вложенной транзакции. HTTP tests проверяют отсутствие доступа к чужому merchant scope. Chrome tests проверяют предупреждения при скрытой таблице, сохранение пользовательского вида, mobile overflow и скрытие stale classes при ошибке обновления.
