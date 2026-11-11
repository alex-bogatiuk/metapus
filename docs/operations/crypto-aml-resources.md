# AML: операционные средства и native funding

Миграция 00066 и `AMLRepository` добавляют отдельный учёт native operating capital. Он нужен для комиссии сети и пополнения своих кошельков gas. Merchant principal, processing fee income и operating capital остаются отдельными компонентами физического остатка. Наличие денег мерчанта или заработанной комиссии само по себе не даёт операционного бюджета.

## Входящий операционный запас

`RegisterAMLOperatingReceipt` — внутренняя control-plane команда с reference и actor. Она принимает только canonical verified external native effect на собственном `network_fee` кошельке после явного пустого baseline. Проверяется точная network/token/wallet/effect identity, отсутствие managed sender и payment allocation. Нераспределённый исторический physical balance нельзя объявить операционным запасом.

Команда атомарно сохраняет immutable attestation, физический `AMLOperatingReceipt` и `seed` resource fact. Она не создаёт merchant credit и не учитывает fee income. Повтор того же effect не создаёт второй receipt; изменение исходного attestation отклоняется. После этого источник проходит обычную AML-проверку. Приём операционного запаса не присваивает ему low автоматически.

## Резерв до подписи

`ReserveAMLResourceBudget` связывает operating budget с существующим fee reservation. Native funding резервирует `funding amount + max fee`, обычная sweep/withdrawal операция — `max fee`. Activation резервирует только max fee. Доступен только незанятый operating capital собственного payer; principal и processing fee не покрывают недостаток gas.

Сохраняются source resource/risk revisions, scope version, class и digest команды. Нужны свежий покрытый low/medium источник и точная финансовая/chain identity. Активные untracked или held fee reservations блокируют новый расход. Native funding допускается в собственный active wallet того же класса либо в доказанно пустой wallet. Unposted incoming тоже нарушает пустоту; разные классы не занимают один пустой target одновременно.

SQL guard новой signing authorization повторяет проверки source/key/asset/basis/amount/fee cap, текущего risk revision и destination coverage. Поздний high, отмена и устаревшая проекция запрещают новую авторизацию. Сохранённые authorizations и signed bytes остаются доступны recovery. Рост fee cap и replacement identity при существующем resource budget пока запрещены: этот этап не реализует resource-aware replacements.

Порядок блокировок: routing scopes одного merchant/network по token ID, затем нужные fee/component rows. Fee manager берёт scope locks до изменения fee row; resource capture меняет только native component под native scope lock. Внешних вызовов внутри этих транзакций нет.

## Финальный исход

Схема 81 требует independent indexed transfer proof до финансового признания нового JIT principal. Две controlled physical стороны, shared posting/recognition, fee/cost, immutable binding и outbox сохраняются вместе. Неподтверждённый principal не коммитится в ожидании будущего resource capture. Recovery завершает уже принятый перевод также при paused ingestion/kill switch и после позднего High; signing и новый dispatch сохраняют свои ограничения. Исторические settled funding rows не переписываются. Подробности: [карта custody writers](../reference/crypto-custody-writers.md#9-jit-native-funding-схемы-81).

Durable inventory queue получает `resource` jobs при изменениях fee/funding/normalized settlement, physical movements и canonical transfer proof. Capture не создаёт финансовых проводок; он распределяет уже учтённые движения.

| Доказанный исход | Resource facts | Резерв |
| --- | --- | --- |
| Успех native funding | Перенос funding source → target и точный native cost | Закрывается после всех доказательств |
| Reverted | Только фактический native cost; principal funding movements отсутствуют | Закрывается как failed |
| Обычная операция с финальным receipt | Только фактический native cost | Закрывается по исходу |
| Pending, provisional, неизвестный исход | Нет предположительного применения | Сохраняется |
| Нет proof/physical movement, cap превышен, projection повреждена, evidence противоречат друг другу | Нет частичного применения | Сохраняется |

Пример: operating source `100`, funding `20`, cap `10`, actual fee `3`. После успеха source/target = `77/20`; после revert = `97/0`. Остаток cap освобождается только с финальным результатом. Доказанный фактический расход учитывается и после позднего high; пересмотр score не отменяет состоявшийся перевод.

Cost fact связан с exact fee reservation, immutable normalized final receipt и конкретным `ChainSettlement` physical expense. Funding fact требует signed expected effect, canonical incoming effect с тем же block hash/height, settled funding document и две точные physical стороны. Replay не повторяет перенос. Native coverage проверяет сумму principal + processing fee + operating capital против physical ledger, immutable journals и каждого post-baseline effect/movement. Portal показывает только merchant net; gas не увеличивает его баланс.

Ошибки применения откатываются до savepoint, оставляют резерв и получают durable backoff 30 секунд. Остальные queue jobs продолжаются. `CancelAMLResourceBudget` атомарно освобождает resource и base fee reservation с неизменяемым аудитом только до любой signing authorization. Timeout или unsigned authorization не являются доказательством безопасной отмены.

## Границы

Legacy outbound gate включённых AML scopes остаётся закрыт; точные зарегистрированные планы используют [dispatch permits и новый withdrawal contract](crypto-aml-outbound.md). Activation budgets и final cost подключены миграцией 00071. High/unknown operating gas разрешён только для явно принятого вывода того же класса; другие автоматические операции требуют low/medium. Production rollout, public operating-funding API, перенос processing fee income в operating capital, resource-aware replacements и reorg corrections остаются отдельными этапами. [Batch planning/capacity и trusted provisioning](crypto-aml-batches.md) подключены. Миграция не применялась к рабочим БД; проверки используют изолированную PostgreSQL и synthetic evidence без платных AML-запросов и реального chain signing/broadcast. Откат при сохранённых resource evidence запрещён, нужен forward migration.

Изолированные SQL tests проверяют success/revert, неизвестный исход, late high до и после подписи, недостаток operating capital при наличии merchant principal/fee income, replay, concurrent reserve, unposted destination, повреждение projection/backoff, несовпадение финального блока, противоречащие final evidence и запрет отката evidence. Regression fee manager/native funding recovery и общий Go suite с race detector проверяют совместимость старого выключенного AML режима.

Схема 86: fee reservations при JIT funding требуют независимый NetworkFeeProof дополнительно к proof успешного principal. При revert признаётся только доказанная комиссия. Нулевой fee не создаёт expense/cost. Недоступный quorum оставляет резерв незавершённым; доказанное превышение cap сохраняет hold. [Профили и порядок восстановления](../reference/crypto-custody-writers.md#14-независимое-признание-network-fee-схемы-86).

Схема 87 направляет exact expected native funding к исходному funding owner до payment matching. Pending observation создаёт request/outbox, independent proof — logical route; handoff не принимает principal/fee самостоятельно. Nudge меняет только due time и сохраняет claimant/lease/hold. Порядок watcher-before/after-settlement даёт тот же финансовый результат. [Операционная проверка](../reference/crypto-custody-writers.md#15-маршрутизация-ожидаемых-переводов-схемы-87).
