# План: учёт криптопоступлений и ресурсных провайдеров

> **TL;DR:** Общий механизм признания движений уже в коде (миграция 00077). Следующие шаги: убрать двойную модель учёта, зафиксировать бизнес-правила, затем включить приём поступлений → невыясненные → зачисление мерчанту и принять его на testnet.
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

- **Двойная модель учёта (`accounting_model_version` 1/2).** Новые документы создаются только в model 2, но в коде остались ветки model 1: `outbound_repo/legacy_settlement_replay.go`, ветки в `outbound_settlement.go`, `outboundapp/confirmation.go`, `fee_budget_manager.go`, `sweep.go`, `treasury.go`, `withdrawal.go`, функции и триггеры в 00077. Удалить их сразу нельзя: около 20 интеграционных тестов (AML inventory/batch/routing, компенсации withdrawal в worker, sweep attempts) и большинство фикстур `reg_crypto_fee_reservations` создают строки model 1 как упрощённую подготовку данных.
- **Мёртвые ветки в SQL.** Триггеры 00077 содержат ветки для старых данных: пустые основания, режим `adoption`, `basis_kind='legacy'`. Удалять вместе с model 1 и проверять полными наборами тестов.
- **Справочник writers.** [crypto-custody-writers.md](../reference/crypto-custody-writers.md) (≈220 КБ) — подробные контракты механизмов со старыми номерами схем. Сокращать по мере изменения механизмов.

## 4. Порядок работ

1. **Убрать model 1.**
    - Перевести фикстуры тестов на model 2. Готовый образец: `TestSweepV2AMLUsesExactLineReceiptBeforeWholeBatchFinishes` в `crypto_repo/sweep_aml_integration_test.go`.
    - Удалить тесты, которые проверяют только поведение model 1 (`TestLegacy*`, `TestFailedWithdrawalCompensation*`, `TestWithdrawalCompensation*`).
    - Затем: `CHECK(accounting_model_version=2)` в 00077, удаление веток Go и SQL, после этого — удаление самого столбца и поля `accountingModelVersion` из API (5 мест во frontend).
    - Готово, когда: в коде нет `AccountingModelVersion`, все наборы тестов зелёные.
2. **P0 — бизнес-правила (решает владелец продукта).**
    - Поддерживаемые сети, токены и dust-порог матчинга.
    - Кто разбирает невыясненные поступления, роли и лимиты, подтверждение вторым сотрудником.
    - Комиссия при позднем и ручном зачислении.
    - Что делать с ошибочными переводами на первом этапе: вручную через казначейский вывод с основанием.
3. **Минимальный рабочий сценарий: приход → невыясненные → зачисление мерчанту.**
    - P4: подтверждённый приход отражается до проверок матчинга; при найденном счёте оплата v2 в том же коммите (только полная атрибуция); invoice-проекции не суммируют chain amount по числу распределений.
    - P5: свип и вывод не расходуют невыясненные суммы ни при включённом, ни при выключенном AML.
    - Reorg: блокировка и ручной разбор вместо автоматической компенсации.
    - P7: список невыясненных и команда «зачислить мерчанту» (prepare → approve, Idempotency-Key, expectedVersion, maker/checker).
    - Готово, когда проходят сценарии A01, A02, A03, A05, A10, A14, A16, A33.
4. **Приёмка на Nile и Sepolia** по тем же сценариям, с реальной финальностью и восстановлением после сбоя.
5. **Дальше — отдельными проектами по приоритету.** Учёт аванса и услуги провайдера (P10) → возвраты неопознанных поступлений (P9) → сверка с привязкой к блоку (P6) → казначейская классификация (P8) → частичная атрибуция → автопополнение провайдера (P11). Для каждого заводится свой короткий план с критерием готовности.

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

Состояние на 09.11.2026 (после сворачивания миграций): `crypto_repo` 278/278, `crypto_worker` 201/201, TRON funding 12/12, EVM funding 3/3, migration 21/21, unit-тесты зелёные.
