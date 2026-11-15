# Ревизия кодовой базы и рефакторинг (2026-11-11)

> **TL;DR:** Удалён подтверждённо мёртвый код (−1962 строки в 54 файлах), бизнес-поведение и контракты не менялись. Печать инвойса (F1) подключена отдельной веткой. Неподключённые кастомные поля (F2) удалены. Главная оставшаяся проблема — проверки, не входящие в `make check-all`.
> **Тип:** Reference
> **Аудитория:** Developer, AI-Agent
> **Связанные:** [Навигатор](../ROUTER.md), [Стандарт](00-doc-standard.md), [Сверка документации](01-documentation-audit.md)

---

## 1. Исходное состояние

| Параметр | Значение |
|---|---|
| Базовый commit | `c84464b` (ветка `crypto-receipt-single-audit`, рабочее дерево чистое) |
| Ветка с изменениями | `refactor/codebase-audit`, 4 commit поверх базы |
| `make check-all` до | проходит: build, golangci-lint (0 issues), tsc, eslint, `go test -short -race ./...`, 443 HTTP-контракта, extensions |
| `npm run test:unit` до | 1 падение (`tests/unit/auth-scope.spec.ts:55`), 6 не запущено, 120 пройдено. Причина — F1 (после исправления — 127/127). В `make check-all` не входит |
| Интеграционные тесты | Полный `go run ./cmd/integration-test` (`-race`, свежая БД, Postgres 17) на ветке рефакторинга: 124 пакета `ok`, 1 падение — `TestShadowWorkerPausesReleasesLeaseAndResumesWithoutRestart` (F22). На базовом `c84464b` тот же тест на той же БД падает 3 из 3, на ветке — тоже 3 из 3 |
| SDK и Vault-плагины до | `sdk/go`, `vault-sign-plugins/{tron,evm}`: vet и `-race` тесты проходят; TS SDK — 5/5; Python SDK — 6/6 (`unittest`). В `make check-all` не входят |

## 2. Метод

- **Достижимость Go.** `golang.org/x/tools/cmd/deadcode` из всех `cmd/*` main, затем с `-test` и с `-test -tags=integration` (172 файла под этим тегом). В реестр попадало только то, что недостижимо во всех трёх режимах.
- **Динамические обращения.** Каждый кандидат искался по имени в Go, шаблонах `cmd/scaffold/templates`, docs, SDK и frontend. Для типов отдельно проверялся реестр HTTP-контрактов (`internal/infrastructure/http/contracts/registry.go`): зарегистрированные типы не удалялись, даже если конвертеры к ним мертвы.
- **Frontend.** Граф импортов по всем `*.ts(x)` с учётом алиаса `@/`, относительных путей и `import()`. Точки входа: файлы маршрутов App Router (`page`, `layout`, `route`, `loading`, `error`, `not-found` и т. п.), `tests/`, конфиги.
- **SQL в циклах.** AST-обход нетестового Go: вызовы `Query/QueryRow/Exec/SendBatch/CopyFrom` внутри `for`/`range` (66 мест), затем ручной разбор.
- **Маркеры.** `legacy` (30 в Go, 13 в SQL, 11 в TS), `fallback`, `TODO` (1), `XXX` (2) — каждое место прочитано.

## 3. Карта обследования

| Область | Точки входа | Статус | Пробелы покрытия |
|---|---|---|---|
| Backend `internal/` (Go, ~242 тыс. строк с тестами) | 25 `cmd/*`, 309 регистраций маршрутов в `http/v1/routes_*.go`, worker, registries в `internal/content` | Обследовано: достижимость, маркеры, SQL в циклах | Пофайловое чтение бизнес-логики не делалось; методы, достижимые через интерфейс, deadcode считает живыми (пример — `RefFinder.CountReferences`) |
| Crypto lifecycle и workers | `crypto_worker/processor.go`, `outbound_*`, `crypto_repo/*` | Обследовано: параллельные реализации, legacy-ветки | Семантика каждого перехода FSM не перепроверялась; финансовые инварианты не трогались |
| Custody/AML и SQL | `db/migrations` (78 файлов), `crypto_repo/aml_*` | Обследованы legacy-ветки в Go | Миграции не читались пофайлово. Исторические данные, планы запросов и индексы без работающей БД не проверить |
| CRUD и метаданные | `catalog_factory.go`, `document_factory.go`, `factory_registry.go`, `internal/metadata` | Обследовано: незарегистрированные handlers, capabilities | Сложность фабрик не измерялась |
| Backend ↔ frontend ↔ SDK | `cmd/http-contracts`, `api/openapi/merchant-v1.yaml`, `sdk/*` | Обследовано: проверка генерации, сверка путей OpenAPI с маршрутами | Поля ручных SDK-типов с DTO не сверялись (кроме существующего `public_events_test.go`) |
| Frontend (~90 тыс. строк TS) | 51 `page.tsx`, App Router | Обследовано: неимпортируемые файлы, экспорты, зависимости | Bundle size не измерялся (`next build` не запускался); hooks/stores оценены выборочно |
| Vault-плагины | 2 отдельных Go-модуля | Обследовано: тесты, дублирование | Безопасность подписи не пересматривалась |
| Скрипты и конфигурация | `Makefile`, `scripts/*`, `deployments/*`, `single-tenant-deployment/*` | Обследовано | PowerShell-скрипты не запускались |
| `docs/`, `.agents/` | — | Проверены только ссылки на удаляемые символы | Содержимое не сверялось |

## 4. Выполненные изменения

Все порции — удаление кода без потребителей. Публичные контракты, миграции, денежные типы, ledger, порядок блокировок, RBAC/RLS/FLS и Vault-проверки не затронуты.

| ID | Commit | Что сделано | Доказательства | Проверка |
|---|---|---|---|---|
| E1 | `9e09736` | Удалён закоммиченный вывод теста scaffold `extensions/.scaffold-test-2128342989/` (28 файлов, 3 тестовых модуля с миграциями `10001–10003`) и два пустых `.audit-*` файла. `make build` пишет бинарники в `bin/` вместо корня. `.gitignore` и `.PHONY` дополнены | Каталог создаёт `cmd/scaffold/main_test.go:32` и удаляет в `t.Cleanup`; закоммичен в `4366246`. Go-шаблоны `./...` и glob `extensions/*/migrations` его не видят. Бинарники `server`/`worker` (77 МБ) оставались в корне неотслеживаемыми — риск случайного коммита | `make build`, `git status` |
| E2 | `d686315` | Удалён недостижимый Go-код: `security.PostingPolicy` и 3 реализации; `FeatureFlagProvider`, `InMemoryFlags`, `CacheBackedFlags`; однопуловый `HealthHandler`; `printing.RenderXLSX`; `CurrencyMetadataResolverImpl`; `ResolveCurrencies`/`ResolvedCurrencyRefs`; COPY-источник из канала и `BatchExecutor`; статистика пула; неиспользуемые trace-helpers; DTO-конвертеры без вызовов; обёртка `MerchantCryptoQueryService.InvoicePayments` | deadcode во всех трёх режимах; поиск по репозиторию. `/health` обслуживает `MultiTenantHealthHandler`. XLSX строят `listexport` и `reporting/export`. Политика закрытого периода нигде не подключена, хотя типы создавали видимость такой защиты. У `CurrencyMetadataResolverImpl` кэш процесса с ключом только по ID валюты — без разделения тенантов | `go build`, `go vet` (с тегом и без), `go test -short -race` затронутых пакетов, `golangci-lint` 0 issues, 443 контракта |
| E3 | `8c6a1cf` | Удалены frontend-экспорты без импортов: 7 обёрток в `lib/format.ts` (включая `toQuantity` на `parseFloat`), `hooks/useDocumentMovements.ts`, `isUuid`, `KNOWN_ACTIONS`, `buildDocumentListUrl`, `hasActiveFilters`, `buildColumnFilterEntry`, `merchantRoleLabel`. Окончания строк (CRLF/LF) сохранены | Граф импортов; ни одного упоминания в другом модуле, тесте или docs | `tsc --noEmit`, `eslint .`, `test:unit` — тот же результат, что до изменений |
| E4 | `9984db1` | Удалены `@types/react-grid-layout` (типы v1 при установленном v2.2.2 с собственными `.d.ts`, ещё и в runtime dependencies) и `@eslint/eslintrc` (не импортируется в `eslint.config.mjs`) | Поиск импортов; `node_modules/react-grid-layout/package.json` объявляет `types` | `tsc`, `eslint`; diff `package-lock.json` — 12 строк |

После всех порций: `make check-all` зелёный (0 issues, 443 контракта, extensions OK); полный интеграционный прогон — результат как на базе (см. §1); deadcode — 73 недостижимые функции вместо 134, новых нет.

Итог: 54 файла, +38/−1962 строки. Количество строк не является целью — главное, что исчезли ложные сигналы: «политика закрытого периода», «feature flags», «XLSX-печать» и тенант-небезопасный резолвер существовали в коде, но не работали.

**Замеры производительности.** Оптимизаций не выполнялось, поэтому замеров до/после нет. Все находки о производительности ниже — гипотезы.

## 5. Реестр находок и backlog

Формат: ID → категория → место → доказательства → влияние → изменение → риск → проверка → приоритет. Статусы: **подтверждено**, **гипотеза**, **требуется проверка в работающей системе**.

### P1 — корректность и вводящие в заблуждение подсистемы

**F1. Печать документов не подключена** — незавершённый переход — **исправлено** в ветке `feature/crypto-invoice-print`.
- *Было:* `NewDocumentPrintHandler` не вызывался ни из одного main. `caps.PrintForms` всегда был `false`, поэтому маршруты `/:id/print` и `/print-forms` не регистрировались, у `/print` не было контракта, и из-за этого падал `tests/unit/auth-scope.spec.ts:55`. `PrintMenuButton` нигде не монтировался. Шаблон `crypto_invoice.gohtml` был заглушкой с одним заголовком, `BuildPrintData` не было ни для одного документа. Обобщённая FLS-проверка смотрела на `lines/unit_price` — табличную часть, которой у crypto-документов нет.
- *Решение владельца:* сделать печать инвойса.
- *Сделано:* у `crypto_invoice` capability `PrintForms` и методы `Print`/`ListPrintForms`. Форма строится из FLS-проекции документа; связанные сущности читаются через `readaccess` (RBAC, RLS, CEL, FLS); скрытое поле печатается как «скрыто», а не `0`. Суммы переводятся точно по `decimalPlaces` токена (`FormatCryptoUnits`); без масштаба печатаются сырые единицы. HTML и DOCX строятся из одной `PrintTable`. Кнопка печати появляется в `AutoForm` по `capabilities.printForms`. Ручной TS-тип `PrintFormSummary` заменён алиасом сгенерированного контракта. Описание — [printing-system.md](../systems/printing-system.md).
- *Проверка:* unit-тесты построителя, проекции, отказа без шаблонов и точного форматирования; integration-тест на реальном авторизованном читателе; сверка gin-маршрутов с контрактами; `auth-scope.spec.ts` зелёный (frontend unit 127/127).
- *Не проверено:* сквозной сценарий в запущенном приложении (браузер → server → pdf-renderer). Форма проверена рендером HTML на тестовых данных.

**F2. `SchemaCache` не создаётся, кастомные поля не попадают в метаданные** — незавершённый переход — **исправлено удалением** в ветке `refactor/custom-fields-metadata`.
- *Было:* `NewSchemaCache` не вызывался, `mergeCustomFields` при `nil` сразу возвращался, поэтому CRUD `/sys/custom-fields` (без UI) записывал описания полей, но они нигде не появлялись. Кэш был построен на одном `pgxpool.Pool` — несовместимо с database-per-tenant. Подключить поля «как есть» тоже было нельзя: frontend сделал бы колонки `attributes.*` сортируемыми и фильтруемыми, а серверный whitelist фильтров такие ключи отклоняет («invalid filter column»).
- *Решение владельца:* удалить кастомные поля.
- *Сделано:* удалены пакет `infrastructure/cache` (`SchemaCache` с LISTEN/NOTIFY-горутиной и загрузкой feature flags), `RouterConfig.SchemaCache`, API `/sys/custom-fields` с его step-up-действиями `system.custom_fields.*`, `CustomFieldRepo`, `EntityDef.CustomFields`/`MergeCustomFields` и группа фильтров «Additional attributes». Из контракта `EntityMetadataResponse` ушло необязательное поле `customFields`; frontend больше его не читает. `attributes` остаётся свободным JSON-объектом: форма проверяет только то, что это объект. Документация обновлена (`METADATA.md`, `extension-api.md`, `01-overview.md`).
- *Не тронуто:* таблицы `sys_custom_field_schemas`, `sys_feature_flags` и их триггеры `pg_notify('schema_changed'|'feature_flags_changed')` остаются: применённые миграции не меняются, а удаление таблиц стирает данные. Условие для отдельной миграции `DROP`: во всех tenant DB нет строк в `sys_custom_field_schemas` или владелец согласен их потерять. До тех пор триггеры шлют уведомления без слушателя; на одной строке это дёшево.
- *Проверка:* `go build`/`go vet` (с тегом `integration` и без), `make check-all`, сверка маршрутов с контрактами, frontend unit 127/127, полный integration-прогон.

**F3. Обогащение outbox-событий валютой не работает** — dead plumbing — **подтверждено**; влияние на данные — **требуется проверка в работающей системе**.
- *Место:* `domain/document_automation.go:24-90`, `http/v1/router.go:99`, `document_factory.go:33`, `content/document_registrations.go:333,383,430,483,619`, `core/entity/traits.go` (`CurrencyAware`).
- *Доказательства:* `RouterConfig.CurrencyMetadataResolver` не заполняется, декоратор получает `nil`, ветка `payload["currency"]` никогда не выполняется. Методы `CurrencyAware` недостижимы. Реализация удалена в E2.
- *Изменение:* убрать параметр `currencyResolver` из `WithOutboxEvents` и трейт `CurrencyAware` — или реализовать тенант-безопасный резолвер.
- *Условие перехода:* до изменения убедиться, что сохранённые шаблоны автоматизаций в tenant DB не обращаются к `.currency` в payload.
- *Риск:* низкий для кода; payload — контракт для пользовательских шаблонов.
- *Проверка:* unit-тест payload и поиск по шаблонам в каждой tenant DB.

**F4. Две реализации сопоставления ожидаемых chain effects** — legacy, crypto lifecycle — **исправлено** в ветке `refactor/legacy-chain-effect-matcher`.
- *Было:* production (`crypto_worker/processor.go:249-251`) использовал только `ExpectedTransferRepo`, а старый `ExpectedChainEffectRepo` держали тесты: `TestIntegrationOnlyExactExpectedEffectMakesOutboundKnown`, хелпер `newIntegrationExternalOutflowRecorder` и `TestExternalOutflowBusyDefersObservationWithoutExhaustingAttempts`.
- *Разница семантики:* старый матчер «потреблял» ожидаемые эффекты по `ordinal` и запоминал `event_index` в `reg_crypto_chain_effect_matches`. Продакшен намеренно так не делает: при нескольких кандидатах все остаются в ожидании settlement, порядковый номер не угадывается (`TestExpectedTransferEconomicMatchingAndAmbiguity`). Поэтому проверка «второй одинаковый эффект достаётся своему `event_index`» описывала поведение, которого в продакшене нет, и не переносилась.
- *Перенос свойств:* несовпадение суммы, источника, получателя, контракта, вида события, хэша и сети, а также неоднозначность уже проверялись на `ExpectedTransferRepo`. Добавлен случай «повтор той же цепочечной идентичности с другой суммой после успешной маршрутизации отклоняется». Хелпер и busy-тест переключены на `ExpectedTransferRepo`; в их сценариях ожидаемых эффектов нет, поэтому оба матчера дают `false`. Compile-time проверка `ExpectedChainEffectMatcher` перенесена на `ExpectedTransferRepo`.
- *Сделано:* `ExpectedChainEffectRepo` (~80 строк SQL) и его тест удалены. Таблица `reg_crypto_chain_effect_matches` не тронута: в неё по-прежнему пишет `external_outflow_workflow.go`.

**F5. Проверки вне `make check-all`** — инструменты — **подтверждено**.
- *Доказательства:* не запускаются `npm run test:unit` (был красным до исправления F1), `sdk/go`, `sdk/typescript`, `sdk/python`, `vault-sign-plugins/{tron,evm}`. Красный unit-тест незаметно дожил до текущего commit.
- *Изменение:* цели `test-frontend-unit` и `test-modules` (цикл по `sdk/go` и двум плагинам; TS SDK через `node node_modules/typescript/bin/tsc` — `npm test` падает с `Permission denied` на `.bin/tsc`; Python — `python3 -m unittest tests.test_webhook tests.test_reference`). Подключить к `check-all` после решения F1.
- *Риск:* низкий.
- *Зависимость:* F1 закрыта, блокеров нет.

**F22. Интеграционный тест TRON shadow worker падает детерминированно** — корректность проверок — **подтверждено**.
- *Место:* `internal/infrastructure/blockchain/tron/shadow_control_integration_test.go:118`.
- *Доказательства:* checkpoint shadow watcher за 10 с не доходит до высоты 14; 3 из 3 запусков падают и на `c84464b`, и на ветке рефакторинга. План `docs/howto/crypto-inbound-provider-accounting-plan.md:149` фиксирует падение ещё на `361438c`. В логе — повторы запросов к тестовому TronGrid-серверу (`pool_exhausted`, `timeout`).
- *Влияние:* `cmd/integration-test` всегда завершается с exit 1, поэтому новая регрессия на этом gate не отличается от старой.
- *Изменение:* разобрать отдельно: либо дефект ingestion/lease, либо тестовый RPC-стенд не выдерживает конкурентных запросов watcher.
- *Риск:* средний — затрагивает приём блоков TRON.
- *Проверка:* `-count=10 -race` по тесту.

### P2 — стоимость сопровождения

**F6. Дублирование между Vault-плагинами TRON и EVM** — дублирование — **подтверждено**.
- *Доказательства:* после нормализации имён сети различаются: `path_merchant_custody.go` — 30 из ~315 строк, `path_sweep_destinations.go` — 34 из ~190, `path_config.go` — 72 из ~310, `internal/json/json.go` идентичен.
- *Влияние:* правило авторизации или конфигурации меняется в двух местах.
- *Изменение:* плагины — отдельные бинарники и отдельная граница доверия, поэтому рекомендуется не общий модуль, а parity-тест: общие golden-фикстуры запросов/ответов для совпадающих путей в обоих модулях.
- *Риск:* высокий при объединении кода подписи; низкий для parity-теста.

**F7. Два lockfile во frontend** — инструменты — **подтверждено**, выполнено: `pnpm-lock.yaml` удалён.
- *Доказательства:* `frontend/Dockerfile` и `CONTRIBUTING.md` используют npm (`package-lock.json`); у `pnpm-lock.yaml` нет документированного потребителя, и он уже разошёлся с `package.json` после E4.
- *Изменение:* удалить `pnpm-lock.yaml` после подтверждения владельца.

**F8. ESLint не ловит неиспользуемые импорты** — инструменты — **подтверждено**.
- *Доказательства:* после E3 в `lib/format.ts` и `lib/list-column-filter.ts` остались 6 неиспользуемых импортов, `eslint .` прошёл без замечаний (их убрали вручную).
- *Изменение:* `@typescript-eslint/no-unused-vars: ["error", { argsIgnorePattern: "^_" }]`; сначала посчитать текущие нарушения.

**F9. Форматирование Go не проверяется** — инструменты — **подтверждено**.
- *Доказательства:* 44 Go-файла не проходят `gofmt -l` на базовом commit; golangci-lint это не ловит.
- *Изменение:* один механический `gofmt -w` отдельным commit плюс formatter в конфиге golangci-lint.

**F10. Оставшиеся недостижимые функции (~60)** — dead code — **подтверждено**, удаление не выполнено.
- *Место:* конструкторы `core/types` (`NewMoney`, `MustMoney`, `NewQuantityFromFloat64` …), `apperror.NewDuplicate/NewMFARequired/NewPeriodClosed`, `Find*` в доменных сервисах, `merchant.Service.AddUser/RemoveUser/…`, `crypto_provider_account.Service.Set/GetCredentials`, `wallet.NewSystemWallet`, `crypto.WithClock`, `cryptoposting.CryptoBalanceMovements`, `entity.NewSettlementMovement`, `settings.Default*`, `middleware.OptionalAuth`, `publicref.Redact`, `extensions.HTTPContracts`, `RefFinder.CountReferences` (метод интерфейса без вызовов).
- *Почему не удалено:* `internal/core` и `internal/platform` служат API для расширений (`docs/systems/extension-api.md`), а часть функций описывает намеренные точки расширения. Нужно решение, что входит в поддерживаемый extension API. Отдельно стоит `NewQuantityFromFloat64`: денежный/количественный тип из `float64` — кандидат на удаление в первую очередь.
- *Проверка:* deadcode в трёх режимах + `make check-extensions` + scaffold-тест.

**F11. Legacy-ветки crypto/AML, обслуживающие исторические записи** — legacy — **требуется проверка в работающей системе**.
- *Место:* `aml_decisions.go:68` (решения без `evaluated_at`), `custody_recognition.go:49` и `custody_loss_recognition.go:74` (legacy writers), `crypto_worker/withdrawal_recovery.go:29` (receipt-only recovery), `fee_settlement_replay.go:26` (отчёты без breakdown), `invoice_drift.go:37`, `evm/driver.go:532`.
- *Вывод:* append-only записи нельзя переписать, поэтому ветки чтения старых форматов остаются навсегда, пока такие строки есть хотя бы в одной tenant DB. Пример условия удаления для `aml_decisions.go`: во всех tenant DB `SELECT count(*) FROM reg_crypto_aml_decisions WHERE evaluated_at IS NULL` = 0, и новые записи без `evaluated_at` невозможны по ограничению схемы. Для остальных веток условия аналогичные: отсутствие строк старого формата и незавершённых заданий. Для каждой ветки нужен свой запрос, согласованный со схемой.
- *Рекомендация:* не удалять; в каждой ветке зафиксировать формат и условие в комментарии рядом с кодом.
- *Уточнено 11.11.2026:* `outbound_settlement.go:83` — не legacy. Вывод без AML связан с заявкой через `doc_withdrawal_requests.withdrawal_id` (`outbound_repo_withdrawals.go:202`), AML-вывод — через `reg_crypto_aml_withdrawal_lines`; алиас `legacy` переименован в `direct`, как и `legacySweepCandidateCTEs` → `directSweepCandidateCTEs`. Так же не legacy и `EffectiveEvidenceSchema` (ноль → схема 1): EVM-watcher не заполняет `DecoderVersion`, поэтому ноль — штатная форма каждого EVM-события. Выбор схемы доказательства по семейству сети сведён в `crypto.TransferEvidenceSchemaFor`. Остатки двойной модели учёта убраны из docs, текста ошибки триггера и CHECK `reg_crypto_aml_withdrawal_facts`.

### P3 — низкий приоритет

| ID | Категория | Место | Доказательства | Изменение | Статус |
|---|---|---|---|---|---|
| F12 | Корректность | `ref_finder_repo.go:231-240`, `ref_deleter_repo.go:81` | `CountReferencesBatch` пропускает ошибки `Scan` и не проверяет `rows.Err()`; ошибки самих запросов доходят до вызывающего через отложенный `Close` (pgx `batch.go:221`). `ListMarkedObjects` игнорирует ошибку подсчёта (только отображение; перед `DELETE` подсчёт повторяется с проверкой) | Проверять `Scan` и `rows.Err()`; тест с битой строкой | гипотеза |
| F13 | Корректность | `ref_deleter_repo.go:126-150` | Между подсчётом ссылок и `DELETE` нет блокировки. Защищают ли FK все виды ссылок (включая typed refs `ref_type/ref_id`), не проверено | Проверить FK по схеме; если нет — `FOR UPDATE` целей или повторная проверка в `DELETE … WHERE NOT EXISTS` | требуется проверка в работающей системе |
| F14 | Производительность | `application/automation/engine.go:464` | Предзагрузка: 1 запрос на канал + 2 на аккаунт (есть TODO) | Замерить число запросов на батч при реальном числе подписчиков; при необходимости `ListByIDs` | гипотеза |
| F15 | Дублирование | `eventlog_repo.go:157`, `notification_repo.go:93`, `outbox.go:128` | Одинаковый idiom `SendBatch`+цикл `Exec`; `notification_repo` молча игнорирует ошибку `Close` | Выровнять обработку `Close`; общий helper — по желанию | подтверждено |
| F16 | Дублирование | `listexport/xlsx.go:208`, `reporting/export/export.go:233` | Две копии стилей XLSX (`thinBorders` и др.) | Общий пакет стилей при следующем изменении экспорта | подтверждено |
| F17 | Скрипты/конфигурация | `deployments/single-tenant/docker-compose.yml` | Сам себя называет legacy; `production-deployment.md:37` описывает как демо; рядом полноценный `single-tenant-deployment/` | Удалить после подтверждения; обновить `crypto-nile-acceptance.md:49`, `vault-tron-production.md:60` | подтверждено |
| F18 | Контракты | `api/openapi/merchant-v1.yaml`, `routes_crypto.go:122-170` | OpenAPI написан вручную; тест проверяет только парсинг и camelCase. Сейчас 13 путей совпадают с маршрутами, но автоматической проверки нет | Тест: маршруты gin под `/merchant/v1` = пути и методы OpenAPI | подтверждено |
| F19 | Frontend | `lib/entity-url.ts` (`_routeSegmentToEntity`) | Статическая таблица (18 записей) дублирует route prefixes реестра (24 пакета сущностей) и уже неполна; используется до загрузки метаданных | Генерировать из контрактов или parity-тест | подтверждено |
| F20 | Frontend | `components/ui/{avatar,breadcrumb,slider}.tsx` | Не импортируются; держат `@radix-ui/react-avatar`, `@radix-ui/react-slider` | Удалить вместе с зависимостями, если shadcn-набор не хранится намеренно | подтверждено |
| F21 | Frontend | `lib/automation-helpers.ts:57` (`getAccountConfigFields`), `lib/table-field-navigation.ts:46,65` | Не вызываются. `advanceToFirstEmptyField` описан в `docs/systems/smart-data-entry.md:7` как доступная утилита; конфиг-поля аккаунтов автоматизации не отображаются | Решить: подключить в формы или удалить вместе с описанием | гипотеза о незавершённой функции |

## 6. Зависимости backlog

```text
F22 — до неё полный integration gate не может быть зелёным
F3 требует сканирования шаблонов в tenant DB
F9 — отдельный механический commit до любых массовых Go-правок
F10 ──► решение о границах extension API (docs/systems/extension-api.md)
```

## 7. Ограничения анализа

- Нет работающей системы: планы запросов, индексы, длительность блокировок и данные tenant DB не проверялись (F3, F11, F13).
- deadcode не видит недостижимость методов, вызываемых через интерфейсы живых типов, и не учитывает внешние модули, импортирующие `metapus/internal` (такое возможно только внутри модуля, то есть в `extensions/`).
- Bundle size не измерялся: удалённые frontend-модули и так не попадали в сборку, поэтому эффект — в сопровождении, а не в размере.
- Миграции, `docs/courses` и `.agents/skills` пофайлово не проверялись.

## Связанные документы

- [01-documentation-audit.md](01-documentation-audit.md) — предыдущая сверка документации с кодом.
- [../systems/printing-system.md](../systems/printing-system.md) — подсистема печати (F1).
- [../systems/extension-api.md](../systems/extension-api.md) — границы API расширений (F10).
