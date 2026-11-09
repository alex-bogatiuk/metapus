# План: учёт криптопоступлений и ресурсных провайдеров

> **TL;DR:** Общий механизм признания движений уже в коде (миграция 00077), учёт исходящих только по финальному доказательству. Следующие шаги: бизнес-правила, затем приём поступлений → невыясненные → зачисление мерчанту и приёмка на testnet.
> **Тип:** How-To
> **Аудитория:** Developer, AI-Agent, Admin
> **Статус:** 09.11.2026. Продакшена нет, обратная совместимость со старыми данными не поддерживается.
> **Связанные:** [Архитектурное решение](../crypto/crypto-inbound-provider-accounting-design.md), [Контракты writers](../reference/crypto-custody-writers.md), [Тестовые сценарии](../crypto/crypto-processing-test-scenarios.md).

Полная прежняя версия плана (этапы 89–111, разделы P0–P13) сохранена в истории git, коммит `101831a`.

## 1. Решения от 09.11.2026

| Решение | Почему |
|---|---|
| Совместимость со старыми данными не поддерживается; стенд пересоздаётся с нуля | Продакшена с реальными мерчантами нет |
| Миграции 00077–00104 свёрнуты в [00077_crypto_custody_accounting.sql](../../db/migrations/00077_crypto_custody_accounting.sql), `ExpectedSchemaVersion = 77` | Итоговая схема совпадает с прежней схемой 104 байт в байт (проверено `pg_dump`, Up и Down) |
| Пока нет продакшена, изменения схемы вносятся в 00077, а не новыми миграциями | Правило `.agents/rules/GLOBAL.md` для раннего этапа |
| Явное принятие (adoption) старых платежей удалено | Платёж без записи в реестре признания в новой схеме возникнуть не может |
| Двойная модель учёта удалена: нет `accounting_model_version`, `AccountingModelVersion` и флага в API/UI | Исходящие документы и резервы комиссий признают principal и fee только по финальному независимому доказательству |
| Сплошной аудит P3 (1777 Go- и 654 SQL-функции) прекращён, индексы и `cmd/crypto-p3-inventory` удалены | Проверка без конца, не дающая хозяйственного результата. Проверяются только пути, которые затрагивает текущий шаг |
| План не ведётся как журнал этапов | Готовность определяется принятым хозяйственным сценарием, а не числом проходов |

## 2. Что уже работает

| Механизм | Суть |
|---|---|
| Идентичность эффекта и доказательства | Один физический перевод = один logical effect; независимое indexed proof из двух источников |
| Receipt + suspense | Внутренний сервис приёма поступления с отражением на счёте невыясненных; к watcher/HTTP ещё не подключён |
| Общий реестр признания | Funding CLI, CryptoPayment, JIT native funding, sweep, treasury, merchant withdrawal, custody loss, network fee — один владелец движения |
| Final settlement исходящих | Principal и комиссия проводятся по финальному доказательству сети, резервы держатся до результата |
| Expected routing | Ожидаемый подписанный перевод передаётся исходному владельцу, а не учитывается как внешний приход |
| Порядок блокировок, fencing, неизменяемые основания | Единый порядок ресурсов, токены попыток у аренды и активации, безопасные повторы команд |

## 3. Известный долг

- **Справочник writers.** [crypto-custody-writers.md](../reference/crypto-custody-writers.md) (≈220 КБ) — подробные контракты механизмов со старыми номерами схем. Сокращать по мере изменения механизмов.

## 4. Порядок работ

1. **P0 — бизнес-правила.** Решено 10.11.2026:
    - Сети и токены: все уже настроенные в справочниках (TRON: TRX и TRC-20; EVM: ETH и ERC-20), если перевод проходит проверку независимого доказательства.
    - Поддерживаемый актив учитывается полностью, в том числе dust; порог dust влияет только на автоматическое сопоставление со счётом.
    - Зачисление мерчанту невыясненного поступления: до порога — один оператор с ролью, выше порога — подготовка и подтверждение разными сотрудниками. Порог задаётся по токену; если порог не задан, подтверждение вторым сотрудником требуется всегда.
    - Комиссия при ручном зачислении: тариф мерчанта на момент подготовки зачисления. Она считается один раз и фиксируется в документе.
    - Ошибочные и неопознанные переводы остаются в невыясненных и не расходуются. Возврат пока делается вручную через казначейский вывод с основанием; отдельный процесс возврата (P9) — позже.
2. **Минимальный рабочий сценарий: приход → невыясненные → зачисление мерчанту.**
    - P4: подтверждённый приход отражается до проверок матчинга; при найденном счёте оплата v2 в том же коммите (только полная атрибуция); invoice-проекции не суммируют chain amount по числу распределений.
    - P5: свип и вывод не расходуют невыясненные суммы ни при включённом, ни при выключенном AML.
    - Reorg: блокировка и ручной разбор вместо автоматической компенсации.
    - P7: список невыясненных и команда «зачислить мерчанту» (prepare → approve, Idempotency-Key, expectedVersion, maker/checker).
    - Готово, когда проходят сценарии A01, A02, A03, A05, A10, A14, A16, A33.
3. **Приёмка на Nile и Sepolia** по тем же сценариям, с реальной финальностью и восстановлением после сбоя.
4. **Дальше — отдельными проектами по приоритету.** Учёт аванса и услуги провайдера (P10) → возвраты неопознанных поступлений (P9) → сверка с привязкой к блоку (P6) → казначейская классификация (P8) → частичная атрибуция → автопополнение провайдера (P11). Для каждого заводится свой короткий план с критерием готовности.

## 4a. Технический дизайн шага 2

Правило: **внешний приход отражает только `CryptoCustodyReceipt`; `CryptoPayment` распределяет уже учтённое поступление.**

| Факт | Документ | Движения |
|---|---|---|
| Подтверждённый (финальный, канонический, с независимым доказательством) приход на управляемый кошелёк поддерживаемого актива | `CryptoCustodyReceipt` | кошелёк +gross (`reg_crypto_balance_movements`), невыясненные +gross (`reg_crypto_receipt_suspense_movements`) |
| Сопоставление со счётом (автоматически, в том же коммите) или ручное зачисление | `CryptoPayment` со ссылкой на receipt | невыясненные −gross, мерчант +net, комиссия +fee; движений по кошельку нет |
| Неподдерживаемый актив, неизвестный кошелёк | только технический журнал (inbox) | нет |
| Orphan/reorg после учёта | инцидент и запрет расходования по сети/кошельку | автоматического сторно нет, разбор вручную |

Изменения по слоям:

1. **Схема (00077).** Убрать ограничения переходного периода у receipt: работа только на отключённой сети, запрет других движений по кошельку, проверки adoption. Разрешить расход невыясненных документом `CryptoPayment` с остатком ≥ 0. Владение custody у платежа (`reg_crypto_payment_custody_bindings`, поколения платежа) заменить неизменяемой записью распределения: receipt, gross/net/fee, снимок тарифа, ответственный.
2. **Домен.** `CryptoPayment` получает `ReceiptID`, движения: расход невыясненных, мерчант, комиссия. Receipt создаётся в момент финальности — для сопоставленного перехода в том же коммите, что и платёж.
3. **Несопоставленные поступления.** Строка inbox при достижении финальности (обновление состояния цепочки) создаёт receipt с причиной `no_allocation`, `wrong_token`, `retired_deposit_address`, `dust` и т. д.
4. **Доступность (P5).** Свип и вывод не расходуют сумму, лежащую в невыясненных: доступный остаток кошелька = custody − невыясненные по этому кошельку/токену − резервы.
5. **Reorg.** Orphan учтённого receipt: инцидент и hold, платёж и receipt не сторнируются автоматически.
6. **Журнал и команда (P7).** Список невыясненных; `prepare-merchant-credit` (merchant, invoice — необязательно) → `approve`; Idempotency-Key, expectedVersion; порог двойного подтверждения из политики токена.

## 5. Сценарии приёмки

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

## 6. Как проверять

Интеграционные тесты создают отдельную БД на каждый тест через `METAPUS_TEST_ADMIN_DSN`. Для части тестов нужна уже размеченная БД в `METAPUS_TEST_CRYPTO_DSN`.

```bash
go build ./...
go test -short -race ./...
export METAPUS_TEST_ADMIN_DSN='postgres://postgres@localhost:5432/postgres?sslmode=disable'
export METAPUS_TEST_CRYPTO_DSN='<disposable db, migrated>' METAPUS_TEST_MIGRATION_DSN="$METAPUS_TEST_CRYPTO_DSN"
go test -tags=integration -race ./internal/infrastructure/storage/postgres/crypto_repo -count=1 -timeout 30m
go test -tags=integration -race ./internal/infrastructure/crypto_worker ./internal/infrastructure/blockchain/funding ./internal/infrastructure/blockchain/evm/funding ./internal/infrastructure/storage/postgres/migration -count=1
```

Состояние на 10.11.2026: `crypto_repo` 271/271, `crypto_worker` 197/197, TRON funding 12/12, EVM funding 3/3, migration 21/21, portal 12/12, postgres 36/36, unit-тесты с race и `tsc` зелёные.
