# Пофайловая сверка документации с кодом

> **Дата:** 2026-11-02 (Asia/Yekaterinburg).
> **Тип:** Reference.
> **Объём:** 113 существующих файлов: 111 Markdown, OpenAPI YAML и публичный webhook JSON-пример. Изменено 78, без правок 35. Сам отчёт добавлен отдельно.
> **Связанные:** [Навигатор](../ROUTER.md), [Стандарт](00-doc-standard.md).

## Основание и границы

Сверка выполнена по содержимому этого checkout: Go/TS-модели и DTO, factory/route registries, SQL, CLI/scripts, Compose и package scripts. `.git` в предоставленной копии отсутствует, поэтому commit не устанавливался; изменения сравнены со снимком файлов до начала работы. Исходники приложения и agent skills не менялись. Лицензионные условия и сторонние font licenses не пересматривались.

Это статическая сверка и локальные проверки контрактов. Исторические разделы «проверено», отчёты testnet и production readiness не считаются новым результатом приёмки. Рабочие DB/Vault, paid AML, реальные chain sends, Docker deployment и production rollout не запускались. План TON, Kubernetes auth и daily liquidity-address screening сохранён как будущий объём.

## Источники истины

- [Schema version](../../internal/core/version/version.go), [миграции](../../db/migrations), [built-in registrations](../../internal/content/register.go).
- [Router](../../internal/infrastructure/http/v1/router.go), [route capabilities](../../internal/infrastructure/http/v1/route_helpers.go), [command idempotency](../../internal/infrastructure/http/v1/middleware/idempotency.go).
- [Catalog lifecycle](../../internal/domain/service.go), [posting](../../internal/domain/posting/engine.go), [crypto reversal](../../internal/domain/registers/crypto_balance/service.go), [TxManager defaults](../../internal/infrastructure/storage/postgres/tx_manager.go).
- [DTO](../../internal/infrastructure/http/v1/dto), [generated contracts registry](../../internal/infrastructure/http/contracts), [frontend scripts](../../frontend/package.json).
- [AML domain](../../internal/domain/crypto/aml), [AML repositories](../../internal/infrastructure/storage/postgres/crypto_repo), [custody plugins](../../vault-sign-plugins).
- [Bootstrap CLI](../../cmd/public-test-bootstrap), [dev scripts](../../scripts/dev), [Compose](../../single-tenant-deployment/docker-compose.yml), [Dockerfile](../../Dockerfile).

## Проверки этого прохода

| Проверка | Результат |
|---|---|
| `go run ./cmd/http-contracts -check` | PASS: 432 зарегистрированных контракта и схемы; generated files не менялись |
| `go test ./api/openapi ./internal/infrastructure/http/contracts ./internal/core/version ./internal/architecture` | PASS; build cache направлен в `/tmp` из-за ограничений записи в системный cache |
| OpenAPI ↔ Go AST | PASS: совпадают property sets 13 public DTO schemas; все 10 публичных mutations описывают Idempotency-Key |
| SDK TypeScript | PASS: tsc и 5 тестов; tsc запущен через `node`, поскольку executable bit установленного shim недоступен |
| SDK Python | PASS: 6 unittest tests |
| SDK Go webhook | PASS: `go test ./metapus -run '^TestVerifyWebhook' -count=1` |
| SDK Go полный suite | Не завершён в sandbox: httptest не может bind локальный TCP port; профильные webhook tests выше пройдены |
| Markdown | Все локальные file links разрешаются; проверены source maps, отсутствующие/перенесённые пути и команды |

Полные integration/race/frontend build и live financial acceptance требуют отдельного тестового окружения; их прежние результаты в документах не выдаются за выполненные здесь.

## Реестр файлов

«Изменён» означает содержательную правку или уточнение статуса учебного примера. «Без правок» означает, что статическая сверка по указанной области не выявила расхождения; это не отметка live acceptance.

| Файл | Итог | Проверка / изменение |
|---|---|---|
| [CLA.md](../../CLA.md) | Без правок | Лицензионный документ сохранён; ссылки на участника/лицензию согласованы с README и CONTRIBUTING. Юридические условия не редактировались. |
| [CONTRIBUTING.md](../../CONTRIBUTING.md) | Изменён | Исправлены frontend path, npm-команды, bootstrap и направление зависимостей. |
| [README.md](../../README.md) | Изменён | Уточнены reflection, append-only crypto ledger, pkg, prerequisites и Makefile checks. |
| [api/fixtures/invoice.completed.json](../../api/fixtures/invoice.completed.json) | Изменён | finalityMode приведён к enum текущего domain; пример проверен SDK webhook tests. |
| [api/openapi/merchant-v1.yaml](../../api/openapi/merchant-v1.yaml) | Изменён | Добавлены addresses, externalId-only, headers всех mutations, version, nullable delivery/test fields и rotation event schema; 13 DTO property sets сверены. |
| [db/backup/README.md](../../db/backup/README.md) | Без правок | Сверены snapshot/restore scripts, manifest и граница PostgreSQL/Vault state. |
| [db/migrations/README.md](../../db/migrations/README.md) | Изменён | UUIDv7 в примере, типы mutable/append-only таблиц, migration lock и фактическое имя crypto register. |
| [deployments/pdf-renderer/README.md](../../deployments/pdf-renderer/README.md) | Без правок | Сверены отдельный renderer, Compose isolation, лимиты, readiness и smoke runner. |
| [docs/METADATA.md](../../docs/METADATA.md) | Изменён | Обновлены schema 76, AML 61–76, custody/retired fields, интеграционные поля и enums. |
| [docs/ROUTER.md](../../docs/ROUTER.md) | Изменён | Исправлены статусы реализованных курсов/адресов, ссылки на testnet и аудит. |
| [docs/courses/01-basics-1.md](../../docs/courses/01-basics-1.md) | Изменён | Учебные сокращённые примеры явно отделены от структур текущей кодовой базы. |
| [docs/courses/02-basics-2.md](../../docs/courses/02-basics-2.md) | Изменён | Исправлены source visitor, ограничения CatalogEntity, ID alias и отличие Fatal от panic. |
| [docs/courses/03-clean-code-and-arch.md](../../docs/courses/03-clean-code-and-arch.md) | Изменён | Исправлены направление импортов, поля BaseEntity и имя реально существующего теста. |
| [docs/courses/04-concurrency.md](../../docs/courses/04-concurrency.md) | Изменён | Проверены context/TxManager и учебный статус сокращённых goroutine/channel примеров. |
| [docs/courses/05-stdlib-and-db.md](../../docs/courses/05-stdlib-and-db.md) | Изменён | JSON facade/v2 omission, деньги, исходные миграции и разные CDC-контракты. |
| [docs/courses/06-metapus-api-auth.md](../../docs/courses/06-metapus-api-auth.md) | Изменён | Переписан маршрут обучения по текущим middleware, JWT/session, ticket и strict DTO. |
| [docs/courses/07-metapus-posting-engine.md](../../docs/courses/07-metapus-posting-engine.md) | Изменён | Заменены удалённые Stock APIs; отражены crypto Extensions, append reversals и внешний OnCommit. |
| [docs/courses/08-advanced-concurrency.md](../../docs/courses/08-advanced-concurrency.md) | Изменён | Сверены используемые concurrency primitives; примеры обозначены как учебные, старый loopvar пример уже ограничен Go <1.22. |
| [docs/courses/09-memory-and-profiling.md](../../docs/courses/09-memory-and-profiling.md) | Изменён | Сверены команды Go benchmark/pprof/lint; учебное включение pprof не выдано за открытый runtime endpoint. |
| [docs/courses/10-design-patterns.md](../../docs/courses/10-design-patterns.md) | Изменён | Актуальные repository/factory paths, реальные регистрации и Strategy нумератора вместо удалённого validation.go. |
| [docs/courses/Project defence.md](../../docs/courses/Project%20defence.md) | Изменён | Убраны production/exactly-once/deadlock overclaims; уточнены Vault, schema rollout, ID и финансовый ledger. |
| [docs/courses/README.md](../../docs/courses/README.md) | Изменён | Отладчик использует полное bootstrap environment; удалён неполный набор обязательных env. |
| [docs/crypto/aml-kyt-architecture.md](../../docs/crypto/aml-kyt-architecture.md) | Изменён | Уточнена редакция до 76 и границы implemented/production acceptance. |
| [docs/crypto/aml-kyt-risk-scenarios.md](../../docs/crypto/aml-kyt-risk-scenarios.md) | Без правок | Матрица сценариев и целевые reviewed corrections отделены от реализованного runtime; источники риска и owner scope сверены. |
| [docs/crypto/crypto-adjustment-v2-design.md](../../docs/crypto/crypto-adjustment-v2-design.md) | Изменён | Частичное распределение сторно описано как реализованное, оставшиеся UX-шаги сохранены как future. |
| [docs/crypto/crypto-environment-setup.md](../../docs/crypto/crypto-environment-setup.md) | Без правок | Сверены setup/server/worker scripts, generated env, dev Vault и отдельная production boundary. |
| [docs/crypto/crypto-processing-architecture.md](../../docs/crypto/crypto-processing-architecture.md) | Изменён | Schema 76, актуальный outbound storage path, allocation lifecycle и отдельная AML/custody ветвь. |
| [docs/crypto/crypto-processing-test-scenarios.md](../../docs/crypto/crypto-processing-test-scenarios.md) | Изменён | Приёмка переведена на schema 76; NOT RUN/live gates не объявлены пройденными. |
| [docs/crypto/crypto-security-and-risk-review.md](../../docs/crypto/crypto-security-and-risk-review.md) | Изменён | Append-only reversal и текущие AML/custody gates; сохранена отдельная production acceptance. |
| [docs/crypto/invoice-reference-ownership-design.md](../../docs/crypto/invoice-reference-ownership-design.md) | Изменён | Уточнена поддержка externalId-only, orderId-only и совместной привязки в DTO/SDK. |
| [docs/crypto/ton-implementation-plan.md](../../docs/crypto/ton-implementation-plan.md) | Изменён | TON остаётся планом без runtime; исправлены portable source links и перенесённый outbound path. |
| [docs/crypto/wallet-address-reuse.md](../../docs/crypto/wallet-address-reuse.md) | Изменён | Устаревший future design заменён действующим allocation/inbox/cooldown/late/lag контрактом. |
| [docs/guide/01-overview.md](../../docs/guide/01-overview.md) | Изменён | Актуальный контент вместо удалённых товарных модулей; metadata/custom fields и tenant binding. |
| [docs/guide/02-architecture.md](../../docs/guide/02-architecture.md) | Изменён | Добавлен application layer, собственный error JSON вместо RFC 7807. |
| [docs/guide/03-project-structure.md](../../docs/guide/03-project-structure.md) | Изменён | Фактические pkg/configs/application/platform/metadata и задачи worker. |
| [docs/guide/04-request-lifecycle.md](../../docs/guide/04-request-lifecycle.md) | Изменён | Текущие middleware groups, transactional hooks, presentation и sys_idempotency commands. |
| [docs/howto/add-blockchain-network.md](../../docs/howto/add-blockchain-network.md) | Без правок | Сверены NetworkDriver/capabilities, bootstrap/chainconfig, EVM signer и обязательные network gates. |
| [docs/howto/add-resource-provider.md](../../docs/howto/add-resource-provider.md) | Без правок | Сверены provider registry, ExternalResourceProvider, account factory и in-flight ownership. |
| [docs/howto/add-rpc-provider.md](../../docs/howto/add-rpc-provider.md) | Без правок | Сверены domain definitions, HTTP profiles, generic transport и provider registry. |
| [docs/howto/crypto-aml-liquidity-address-screening-plan.md](../../docs/howto/crypto-aml-liquidity-address-screening-plan.md) | Без правок | Daily checkaddr/reviewed high resolution остаются явно не реализованным планом; текущие reusable AML-компоненты существуют. |
| [docs/howto/new-document.md](../../docs/howto/new-document.md) | Изменён | Рабочие crypto аналоги, HeaderDocumentService, capabilities, source interfaces, strict guard и wire registry. |
| [docs/howto/new-entity.md](../../docs/howto/new-entity.md) | Изменён | Реальные domain paths, pointer generic types, DTO/factory registration, refs и frontend boundaries. |
| [docs/meta/00-doc-standard.md](../../docs/meta/00-doc-standard.md) | Без правок | Нормативный шаблон/таксономия; фиктивные пути внутри шаблона являются placeholders, не ссылками на код. |
| [docs/operations/crypto-aml-batches.md](../../docs/operations/crypto-aml-batches.md) | Без правок | Сверены batch policies/capacity, immutable plans и отдельный trusted provisioning. |
| [docs/operations/crypto-aml-inventory-allocations.md](../../docs/operations/crypto-aml-inventory-allocations.md) | Без правок | Сверены net/fee partitions, durable queue, exact transfer evidence, cancellation и recovery. |
| [docs/operations/crypto-aml-merchant-pools.md](../../docs/operations/crypto-aml-merchant-pools.md) | Без правок | Сверены scope/network budgets, ready/pending/queued, controller CLI и baseline gates. |
| [docs/operations/crypto-aml-outbound.md](../../docs/operations/crypto-aml-outbound.md) | Изменён | Порядок rollout обновлён до 76; source lines, acceptance, dispatch и exact renewal существуют в коде. |
| [docs/operations/crypto-aml-preflight.md](../../docs/operations/crypto-aml-preflight.md) | Без правок | Сверены read-only mode, plugin capability triple, blockers и отсутствие write/signing effects. |
| [docs/operations/crypto-aml-resources.md](../../docs/operations/crypto-aml-resources.md) | Без правок | Сверены operating receipts/budgets/facts и отделение gas от principal/fee income. |
| [docs/operations/crypto-aml-risk-balances.md](../../docs/operations/crypto-aml-risk-balances.md) | Без правок | Сверены Portal risk DTO, repeatable snapshot, total/free/reserved/technicalHold и freshness. |
| [docs/operations/crypto-aml-risk-projection.md](../../docs/operations/crypto-aml-risk-projection.md) | Без правок | Сверены verified effects, baselines, generations, sticky high, TTL и risk outbox. |
| [docs/operations/crypto-aml-runtime.md](../../docs/operations/crypto-aml-runtime.md) | Изменён | Устаревший статус outbound заменён реализованным class/source/permit маршрутом; paid/free flow сверены. |
| [docs/operations/crypto-aml-testnet-bootstrap.md](../../docs/operations/crypto-aml-testnet-bootstrap.md) | Изменён | Schema 76; opening введён в 75; CLI manifest и сохранение финансового ledger сверены. |
| [docs/operations/crypto-aml-testnet-emulator.md](../../docs/operations/crypto-aml-testnet-emulator.md) | Без правок | Сверены testnet identity, provider/contract/policy versions, 24h TTL, randomness и отсутствие paid admission. |
| [docs/operations/crypto-deposit-address-rotation.md](../../docs/operations/crypto-deposit-address-rotation.md) | Без правок | Сверены wallet retirement, rotation command/DTO/webhook, late needs_review и operator upgrade. |
| [docs/operations/crypto-mainnet-go-no-go.md](../../docs/operations/crypto-mainnet-go-no-go.md) | Изменён | Ожидаемая schema обновлена 75 → 76; default NO-GO сохранён. |
| [docs/operations/crypto-merchant-custody.md](../../docs/operations/crypto-merchant-custody.md) | Изменён | Устаревшее ожидание нового withdrawal contract заменено действующим AML outbound. |
| [docs/operations/crypto-nile-acceptance.md](../../docs/operations/crypto-nile-acceptance.md) | Изменён | Schema 76 и -tags=integration для реально запускаемых migration/worker tests. |
| [docs/operations/crypto-wallet-pool.md](../../docs/operations/crypto-wallet-pool.md) | Без правок | Сверены shared vs enforced merchant refill, derivation cursor, activation и retry/fencing. |
| [docs/operations/deployment-guide.md](../../docs/operations/deployment-guide.md) | Изменён | Добавлены Sepolia, оба plugin modules и явный integration build tag. |
| [docs/operations/integration-accounts.md](../../docs/operations/integration-accounts.md) | Изменён | Архивные отчёты вне checkout перестали быть неработающими ссылками; provider/quota/import контракты сверены. |
| [docs/operations/local-vault-recovery.md](../../docs/operations/local-vault-recovery.md) | Без правок | Runbook явно привязан к историческому существующему стенду; команды unseal/proxy recovery сохранены, live state не проверялось. |
| [docs/operations/merchant-webhooks-production-readiness.md](../../docs/operations/merchant-webhooks-production-readiness.md) | Изменён | Приёмка schema 76, текущий public API и независимые production gates. |
| [docs/operations/production-deployment.md](../../docs/operations/production-deployment.md) | Изменён | Schema 76, остановка всех writers/readers при migration, отсутствие несуществующего worker allowlist и exact compatibility. |
| [docs/operations/public-nile-deployment.md](../../docs/operations/public-nile-deployment.md) | Изменён | Schema 44 → 76; сверены bootstrap CLI/Compose profiles и dual-family setup. |
| [docs/operations/rpc-ingestion.md](../../docs/operations/rpc-ingestion.md) | Изменён | Schema 60 обозначена историческим этапом; текущая 76, TRON/EVM paths и preflight сверены. |
| [docs/operations/tron-block-ingestion.md](../../docs/operations/tron-block-ingestion.md) | Изменён | Исторический ingestion upgrade отделён от полного rollout; архивные отчёты обозначены внешними. |
| [docs/operations/vault-evm-sepolia.md](../../docs/operations/vault-evm-sepolia.md) | Изменён | Ожидаемая schema 59 → 76; template, custody seed/allowlist/preflight сверены. |
| [docs/operations/vault-kubernetes-migration.md](../../docs/operations/vault-kubernetes-migration.md) | Без правок | Kubernetes manifests отсутствуют; документ сохраняет явно будущий auto-auth контракт. |
| [docs/operations/vault-tron-production.md](../../docs/operations/vault-tron-production.md) | Без правок | Сверены tokenless Proxy/AppRole, Transit/keyring v3, policy template и production boundary. |
| [docs/operations/worker-reliability.md](../../docs/operations/worker-reliability.md) | Без правок | Сверены supervisors, leases/fencing, durable cron runs, retry/catch-up и worker_reliability.sql. |
| [docs/reference/auth-table-names.md](../../docs/reference/auth-table-names.md) | Изменён | Исправлено auth_mfa_challenges, уточнена отдельная процедура переноса старой БД. |
| [docs/reference/development-rules.md](../../docs/reference/development-rules.md) | Изменён | Реальные dependency ограничения, append-only crypto ledger, исходная migration 00006 и отсутствие make reset-db. |
| [docs/reference/frontend-guidelines.md](../../docs/reference/frontend-guidelines.md) | Изменён | Client pages, external unknown boundary, реальная URL/Zustand модель и routePrefix paths. |
| [docs/reference/json-contracts.md](../../docs/reference/json-contracts.md) | Изменён | Все три generated artifacts, текущий денежный transport и отсутствие товарных накладных. |
| [docs/reference/keyboard-shortcuts.md](../../docs/reference/keyboard-shortcuts.md) | Изменён | Сверены useShortcut/store/mod/priority; пояснение палитры перенесено к относящимся к нему клавишам. |
| [docs/reference/merchant-public-api.md](../../docs/reference/merchant-public-api.md) | Изменён | Обязательные ключи всех mutations, сохранение 201 при replay, partial withdrawals, network/finality enum и references. |
| [docs/reference/naming-conventions.md](../../docs/reference/naming-conventions.md) | Изменён | Текущие catalog/document/register/meta examples вместо удалённых endpoint-ов. |
| [docs/systems/auth-security.md](../../docs/systems/auth-security.md) | Изменён | Серверная auth-state проверка, физические auth_* имена и текущий permission example. |
| [docs/systems/automation-engine.md](../../docs/systems/automation-engine.md) | Изменён | Доставка возвращает ошибки executor для retry; update account PUT и отдельная reliability ссылка. |
| [docs/systems/command-palette.md](../../docs/systems/command-palette.md) | Изменён | Удалён несуществующий hook; поисковые/preview данные описаны через authorized reads вместо старого SQL. |
| [docs/systems/core-layer.md](../../docs/systems/core-layer.md) | Изменён | Точное разделение BaseEntity/BaseCatalog/Catalog/BaseDocument/Document и собственный error JSON. |
| [docs/systems/crud-pipeline.md](../../docs/systems/crud-pipeline.md) | Изменён | Before hooks внутри tx, Transaction* до commit, After* через OnCommit; mark и Delete различены. |
| [docs/systems/crypto-processing.md](../../docs/systems/crypto-processing.md) | Изменён | Сводный актуальный контракт; дублирующие старые API-примеры заменены ссылкой на canonical Merchant API/OpenAPI. |
| [docs/systems/dashboard-widgets.md](../../docs/systems/dashboard-widgets.md) | Без правок | Сверены registry v2, настройки/layout normalization, ограничения и dashboardapp read snapshots. |
| [docs/systems/domain-layer.md](../../docs/systems/domain-layer.md) | Изменён | Места регистрации hooks и обязательные/after-commit фазы. |
| [docs/systems/exchange-rates-design.md](../../docs/systems/exchange-rates-design.md) | Изменён | Реализованная версия и operator upgrade заполненного окружения вместо запрета переноса. |
| [docs/systems/exchange-rates.md](../../docs/systems/exchange-rates.md) | Без правок | Сверены CoinGecko feed, история, 300/900 секунд, mapping/source permissions и integration harness. |
| [docs/systems/extension-api.md](../../docs/systems/extension-api.md) | Изменён | Generic metadata UI дополнен DTO/route contracts, frontend wiring и границей scaffold. |
| [docs/systems/filtering.md](../../docs/systems/filtering.md) | Изменён | Сверены query/filter codecs; товарный EXISTS явно обозначен гипотетическим extension example. |
| [docs/systems/infrastructure-layer.md](../../docs/systems/infrastructure-layer.md) | Изменён | Текущая middleware chain, storage boundaries и фактические cleanup loops вместо Audit Cleaner. |
| [docs/systems/multi-tenancy.md](../../docs/systems/multi-tenancy.md) | Изменён | Порядок TenantDB/Auth, query tenant для WS и конкретный postgres querier accessor. |
| [docs/systems/numerator.md](../../docs/systems/numerator.md) | Изменён | Transaction-aware querier, Strict guarantees и per-key mutex вместо pool-only/lock-free claims. |
| [docs/systems/operational-modes.md](../../docs/systems/operational-modes.md) | Без правок | Сверены единый release, exact schema compatibility и операторская миграция active/suspended tenant-ов. |
| [docs/systems/posting-engine.md](../../docs/systems/posting-engine.md) | Изменён | Append-only reversal, реальные visitors/recorders, read committed default, OnCommit и debit-first/AML. |
| [docs/systems/printing-system.md](../../docs/systems/printing-system.md) | Изменён | DOCX генерируется godocx из PrintData.Table; фактическая standard форма и PDF boundary. |
| [docs/systems/realtime-notifications.md](../../docs/systems/realtime-notifications.md) | Изменён | JWT в URL заменён одноразовым ws-ticket и tenant query. |
| [docs/systems/related-documents.md](../../docs/systems/related-documents.md) | Изменён | Строковая amount, capabilities, текущий page fetcher и metadata action fallback. |
| [docs/systems/reporting-system.md](../../docs/systems/reporting-system.md) | Изменён | Dataset registration/route compiler вместо отсутствующего FactoryRegistry.RegisterReport. |
| [docs/systems/settings-system.md](../../docs/systems/settings-system.md) | Изменён | Frontend registry вместо несуществующего endpoint, sys_user_preferences и общая version singleton. |
| [docs/systems/smart-data-entry.md](../../docs/systems/smart-data-entry.md) | Изменён | Оставлены реальные focus utilities/autoAdvance; удалён отсутствующий товарный cascade. |
| [docs/systems/strict-documents.md](../../docs/systems/strict-documents.md) | Без правок | Сверены guard matrix, mutable whitelist, capabilities/actions и object-bound step-up. |
| [docs/systems/transactions.md](../../docs/systems/transactions.md) | Изменён | Реальный error constructor, crypto lock keys и nested/savepoint contracts. |
| [frontend/docs/frontend-guidelines.md](../../frontend/docs/frontend-guidelines.md) | Изменён | Исправлены client/server boundary, реальные hooks/test runner/routes, settings и canonical link. |
| [internal/infrastructure/blockchain/tron/testdata/receipt_profiles/protocol-audit.md](../../internal/infrastructure/blockchain/tron/testdata/receipt_profiles/protocol-audit.md) | Без правок | Сверено с receipt normalizer и TronPower fixtures/tests; внешний protocol commit заново не проверялся. |
| [internal/infrastructure/http/contracts/README.md](../../internal/infrastructure/http/contracts/README.md) | Без правок | Сверены три generated artifacts, registered routes, DTO projection и runtime validation. |
| [sdk/go/README.md](../../sdk/go/README.md) | Без правок | Сверены Create/Get/Cancel/BindInvoiceReference и raw HMAC/dedup semantics; webhook tests выполнены. |
| [sdk/python/README.md](../../sdk/python/README.md) | Без правок | Сверены методы, metadata omission и raw signature; 6 тестов выполнены. |
| [sdk/typescript/README.md](../../sdk/typescript/README.md) | Без правок | Сверены методы, externalId-only и raw signature; 5 тестов и tsc выполнены. |
| [single-tenant-deployment/DOCKER-EVM-TEST-STAND.md](../../single-tenant-deployment/DOCKER-EVM-TEST-STAND.md) | Изменён | Явные параметры state/public IP/admin password вместо workstation-specific defaults. |
| [single-tenant-deployment/README.md](../../single-tenant-deployment/README.md) | Изменён | Разделены полный bootstrap и ручная сборка; явный seed, действительная DB name и PDF service. |
| [vault-sign-plugins/evm/README.md](../../vault-sign-plugins/evm/README.md) | Без правок | Сверены Sepolia allowlist, отдельный seed, signing paths и AML readiness triple. |
| [vault-sign-plugins/tron/README.md](../../vault-sign-plugins/tron/README.md) | Без правок | Сверены keyring v3, derive/sign invariants, custody и AML readiness triple. |
