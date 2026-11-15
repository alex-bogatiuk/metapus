# Навигатор документации Metapus

> **TL;DR:** Точка входа для разработчиков и ИИ-агентов. Найдите нужную тему по директории или задаче.

> **Тип:** Reference
> **Аудитория:** All
> **Связанные:** [meta/00-doc-standard.md](meta/00-doc-standard.md)

---

## 1. Путеводитель (Guide)
Читается по порядку для погружения в проект.

| Файл | Описание | Ключевые слова |
|------|----------|----------------|
| [guide/01-overview.md](guide/01-overview.md) | Обзор платформы, стек, манифест | metapus, erp, обзор, манифест, стек |
| [guide/02-architecture.md](guide/02-architecture.md) | Clean Architecture, слои, Code is Metadata | clean architecture, слои, core, domain, infrastructure |
| [guide/03-project-structure.md](guide/03-project-structure.md) | Структура директорий проекта | cmd, internal, pkg, папки, структура |
| [guide/04-request-lifecycle.md](guide/04-request-lifecycle.md) | Жизненный цикл HTTP-запроса, middleware | request, middleware, error handler, lifecycle |

## 2. Глубокое погружение (Systems)
Архитектура подсистем. Читается по мере необходимости.

| Файл | Описание | Ключевые слова |
|------|----------|----------------|
| [systems/auth-security.md](systems/auth-security.md) | Аутентификация, JWT, RBAC, RLS, FLS | jwt, rbac, rls, fls, security, permissions, roles |
| [systems/automation-engine.md](systems/automation-engine.md) | Движок автоматизации, события, правила | automation, engine, rules, events |
| [systems/core-layer.md](systems/core-layer.md) | Базовые примитивы, BaseEntity, BaseCatalog | core, entity, base, catalog, document |
| [systems/crud-pipeline.md](systems/crud-pipeline.md) | Generic CRUD pipeline, обработка запросов | crud, generic, pipeline, factory, handler |
| [systems/command-palette.md](systems/command-palette.md) | Command Palette (Ctrl+K), навигация, калькулятор, глобальный поиск по данным | command palette, ctrl+k, search, calculator, actions, global search, data search, SearchFieldsProvider, RLS |
| [systems/crypto-processing.md](systems/crypto-processing.md) | Криптопроцессинг: chain watcher, FSM, event processor, compliance, settlement, threshold sweep, sweep retain amount, TRON resource preparation, iTRX energy rental, platform cost accounting, allocation mode, merchant config, **Merchant Public API**, **Merchant Portal API**, **ERP/admin Withdrawal Approval**, Vault outbound execution, API-ключи, SSRF, BOLA, **Withdrawal**, **Address Whitelist**, **Debit-First Posting**, **curl-примеры** | crypto, payment, blockchain, tron, watcher, fsm, invoice, wallet, webhook, settlement, compliance, sweep, threshold, retain, dust, itrx, energy, platform cost, allocation, merchant config, merchant api, api key, scope, ssrf, bola, portal, portal api, dashboard, curl, examples, balance calculator, withdrawal, withdrawal request, approval, vault, outbound, address whitelist, network, debit-first |
| [systems/dashboard-widgets.md](systems/dashboard-widgets.md) | Дашборды, виджеты, layout пользователя | dashboard, widgets, UI, preferences |
| [systems/domain-layer.md](systems/domain-layer.md) | Бизнес-логика, сервисы, хуки | domain, services, hooks, logic, repository |
| [systems/exchange-rates.md](systems/exchange-rates.md) | Эксплуатация курсов: CoinGecko, неизменяемые наблюдения, оценка Portal и диагностика | exchange rate, rate source, currency, coingecko, rate feed, currency mapping, multiplier |
| [systems/exchange-rates-design.md](systems/exchange-rates-design.md) | Реализованная первая версия: CoinGecko, история наблюдений, freshness и оценка Portal; ограничения ручного ввода | exchange rates, architecture, UX, history, asOf, freshness, manual rates, rate policy, currency pair |
| [systems/extension-api.md](systems/extension-api.md) | Плагины, кастомные расширения, модули | extensions, plugins, api, modules, custom |
| [systems/filtering.md](systems/filtering.md) | Система фильтрации, SQL-генерация, типы | filtering, filters, sql, exists, ui |
| [systems/infrastructure-layer.md](systems/infrastructure-layer.md) | Инфраструктура, PostgreSQL, HTTP сервер | infrastructure, postgres, http, gin |
| [systems/multi-tenancy.md](systems/multi-tenancy.md) | Физическая изоляция Database-per-Tenant | multi-tenant, tenant, isolation, database |
| [systems/numerator.md](systems/numerator.md) | Автонумерация, генерация кодов и номеров | numerator, codes, auto-increment, numbering |
| [systems/operational-modes.md](systems/operational-modes.md) | SaaS, Self-Hosted, Dedicated режимы | saas, dedicated, self-hosted, modes, deployment |
| [systems/posting-engine.md](systems/posting-engine.md) | Движок проведения, Visitor pattern, **Debit-First Posting** | posting, engine, visitor, stock, balances, movements, debit-first, withdrawal |
| [systems/printing-system.md](systems/printing-system.md) | Печатные формы, шаблоны HTML/PDF | printing, templates, pdf, gohtml, export |
| [systems/realtime-notifications.md](systems/realtime-notifications.md) | Push-уведомления, WebSockets | realtime, websockets, notifications, sse, push |
| [systems/related-documents.md](systems/related-documents.md) | Дерево подчиненности, превью карточек, context menu, API-контракт | related, documents, tree, links, subordination, preview, hover card, context menu, basis_id, FK-references |
| [systems/reporting-system.md](systems/reporting-system.md) | Query Engine, сборка отчетов, СКД | reporting, query engine, xlsx, variants |
| [systems/settings-system.md](systems/settings-system.md) | Системные и пользовательские настройки | settings, configuration, ui, metadata |
| [systems/smart-data-entry.md](systems/smart-data-entry.md) | Утилиты навигации по полям таблицы и ReferenceField.autoAdvance; границы реализации | smart entry, autofill, ux, focus, cascade |
| [systems/transactions.md](systems/transactions.md) | Управление транзакциями, блокировки | transactions, lock, pessimistic, optimistic, txmanager |

## 3. Рецепты (How-To)
Пошаговые инструкции для частых задач.

| Файл | Описание | Ключевые слова |
|------|----------|----------------|
| [howto/new-document.md](howto/new-document.md) | Инструкция по добавлению нового документа | new, document, migration, posting, validation, M15 |
| [howto/new-entity.md](howto/new-entity.md) | Инструкция по добавлению справочника/документа | new, entity, catalog, document, tutorial |
| [operations/integration-accounts.md](operations/integration-accounts.md) | Настройка tenant-аккаунтов/пулов, перенос RPC/CoinGecko, dry-run и controlled rollout | integrations, credentials, alchemy, quota, import, polling |
| [howto/crypto-aml-liquidity-address-screening-plan.md](howto/crypto-aml-liquidity-address-screening-plan.md) | План ежедневных адресных AML-проверок ликвидности через GetBlock, причинного распространения риска и пересмотра исторических High | crypto, AML, getblock.net, checkaddr, liquidity, schedule, risk, provenance, resolution |
| [howto/crypto-inbound-provider-accounting-plan.md](howto/crypto-inbound-provider-accounting-plan.md) | План реализации receipt/suspense, merchant attribution, возвратов, казначейских оснований, авансов провайдерам и безопасной миграции учёта | crypto, accounting, inbound, suspense, unidentified, receipt, refund, provider, iTRX, transit, reconciliation, migration |

## 4. Справочник (Reference)
Таблицы, API, строгие правила.

| Файл | Описание | Ключевые слова |
|------|----------|----------------|
| [METADATA.md](METADATA.md) | Структура метаданных в терминах 1С: справочники, документы, реквизиты, табличные части, регистры, перечисления и отчёты | метаданные, 1С, конфигурация, справочники, документы, реквизиты, регистры |
| [reference/document-register-matrix.md](reference/document-register-matrix.md) | Матрица движений всех реализованных документов и краткий справочник назначения; отличия поступлений, пополнений кошельков и платформы | документы, движения, регистры, пополнение, невыясненные, свип, вывод, комиссии, корректировки |
| [reference/development-rules.md](reference/development-rules.md) | Строгие архитектурные правила и запреты, **CWE-209**, привязка ID документа | rules, development, invariants, limits, cwe-209, error messages, security, document id |
| [reference/frontend-guidelines.md](reference/frontend-guidelines.md) | Правила фронтенда, Next.js, shadcn/ui | frontend, guidelines, next.js, shadcn, react |
| [reference/keyboard-shortcuts.md](reference/keyboard-shortcuts.md) | Глобальные горячие клавиши, useShortcut | shortcuts, keyboard, hotkeys, bindings |
| [reference/json-contracts.md](reference/json-contracts.md) | Профили JSON v2, строгий HTTP, PostgreSQL codecs, byte-stable legacy protocols | json, encoding/json/v2, pgx, gin, canonical, omitzero, hmac, fingerprint |
| [reference/crypto-canonical-contracts.md](reference/crypto-canonical-contracts.md) | Канонические декодеры, стабильные evidence/signing schemas, AML coverage, owner-based funding и границы восстановления | crypto, canonical, decoder, evidence, fingerprint, AML, coverage, funding, custody, recovery |
| [reference/merchant-public-api.md](reference/merchant-public-api.md) | Публичная API-документация для мерчантов: инвойсы, payment page, постоянные адреса, webhooks, ошибки | merchant api, public api, invoices, payment page, webhook, api key, scopes, crypto payments, address |
| [reference/naming-conventions.md](reference/naming-conventions.md) | Стандарты именования (БД, Go, API) | naming, conventions, database, api, snake_case |

## 5. Мета-документация
Правила работы с проектом и документацией.

| Файл | Описание | Ключевые слова |
|------|----------|----------------|
| [meta/00-doc-standard.md](meta/00-doc-standard.md) | Стандарт написания документации | документация, стандарт, шаблон, стиль |
| [meta/01-documentation-audit.md](meta/01-documentation-audit.md) | Пофайловая сверка 2026-11-02, исправления, источники и выполненные проверки | audit, актуальность, документация, schema 76, verification |
| [meta/02-refactoring-audit.md](meta/02-refactoring-audit.md) | Ревизия кода 2026-11-11: карта обследования, удалённый мёртвый код, реестр находок и backlog рефакторинга | refactoring, dead code, legacy, backlog, deadcode, print, schema cache, check-all |

## 6. Криптовалютный контур

Архитектура, безопасность, развёртывание и проверка crypto processing.

| Файл | Описание | Ключевые слова |
|------|----------|----------------|
| [crypto/crypto-processing-architecture.md](crypto/crypto-processing-architecture.md) | Фактическая архитектура приёма, свипов, withdrawal, treasury, сетевых криптоопераций, Vault и reconciliation | crypto, tron, evm, family, adapter, payment, sweep, withdrawal, treasury, crypto network operation, wallet activation, vault, reconciliation |
| [crypto/crypto-adjustment-v2-design.md](crypto/crypto-adjustment-v2-design.md) | Реализованные сценарии CryptoAdjustment и следующие улучшения: сторно платежа, корректировка кошелька, условное согласование | crypto, adjustment, сторно, кошелёк, согласование |
| [crypto/crypto-inbound-provider-accounting-design.md](crypto/crypto-inbound-provider-accounting-design.md) | Предлагаемое разделение custody receipt и attribution, невыясненные суммы, provider transit/service accounting, AML и block-bound сверка; изменения существующих механизмов | crypto, accounting, receipt, custody, suspense, unidentified, treasury, refund, provider, iTRX, energy, reorg |
| [crypto/invoice-reference-ownership-design.md](crypto/invoice-reference-ownership-design.md) | Реквизиты мерчанта externalId/orderId и системная классификация top-up | crypto, invoice, externalId, orderId, topup, invoice_kind, creation_source, merchant reference, сверка, ERP |
| [howto/add-blockchain-network.md](howto/add-blockchain-network.md) | Алгоритм добавления сети и первого chain family на примере Ethereum/EVM, extension points и production gates | crypto, ethereum, evm, blockchain network, driver, adapter, vault, finality, nonce |
| [howto/add-resource-provider.md](howto/add-resource-provider.md) | Добавление вендора аренды ресурсов (iTRX, TronSave, …) через adapter registry, без копирования FSM | crypto, itrx, energy, resource provider, adapter, failover |
| [howto/add-rpc-provider.md](howto/add-rpc-provider.md) | Добавление RPC-провайдера через чистые provider contracts и HTTP registry; границы конфигурации, квот и проверки подключения | rpc, provider, registry, clean architecture, credentials, topology |
| [crypto/crypto-security-and-risk-review.md](crypto/crypto-security-and-risk-review.md) | Security/executive review, production blockers и merchant IP allowlist | crypto security, ciso, cfo, vault, ip allowlist, stripe, paypal |
| [crypto/crypto-environment-setup.md](crypto/crypto-environment-setup.md) | Развёртывание PostgreSQL, Vault, tenant, server, worker и frontend с нуля | crypto setup, nile, postgres, vault, worker, mfa |
| [crypto/crypto-processing-test-scenarios.md](crypto/crypto-processing-test-scenarios.md) | Пошаговые E2E, негативные и финансовые проверки всех механизмов | crypto qa, e2e, invoice, confirmations, sweep, withdrawal, reconciliation |
| [crypto/wallet-address-reuse.md](crypto/wallet-address-reuse.md) | Реализованные cooldown, история аллокаций, durable inbox, атрибуция поздних платежей и lease-only watcher-lag gate | crypto, wallet pool, cooldown, address reuse, late payment, attribution |
| [operations/crypto-aml-testnet-emulator.md](operations/crypto-aml-testnet-emulator.md) | Случайные воспроизводимые AML-ответы Nile/Sepolia, отдельный testnet provider, control-plane setup без API-ключей и paid calls | crypto, AML, emulator, testnet, nile, sepolia, seed |
| [operations/crypto-aml-testnet-bootstrap.md](operations/crypto-aml-testnet-bootstrap.md) | Однократный импорт старых testnet остатков в low, manifests, reconciliation evidence и сохранение financial ledger | crypto, AML, bootstrap, opening inventory, testnet, migration, low |
| [operations/crypto-aml-runtime.md](operations/crypto-aml-runtime.md) | Getblock.net jobs, pinned mappings, локальный бюджет paid submit и ограничения включения | crypto, AML, KYT, getblock.net, package, submit, poll, quota |
| [operations/crypto-aml-risk-projection.md](operations/crypto-aml-risk-projection.md) | Verified intake, полнота inventory, rescreen, late high, проекция wallet/token и risk events | crypto, AML, coverage, baseline, projection, rescreen, risk, webhook |
| [operations/crypto-deposit-address-rotation.md](operations/crypto-deposit-address-rotation.md) | AML в списке депозитных адресов, атомарная ротация, неизменяемая история, late intake без автозачисления и обновление схемы | crypto, deposit, address, rotation, AML, retired, webhook |
| [operations/crypto-aml-risk-balances.md](operations/crypto-aml-risk-balances.md) | Portal net balance по риску, согласованный snapshot, debit-first commitments, fee/source attribution и ограничения read model | crypto, AML, balance, risk, net, fee, reservations, portal |
| [operations/crypto-aml-inventory-allocations.md](operations/crypto-aml-inventory-allocations.md) | Durable net/fee partitions, план частичного свипа до подписи, применение chain/ledger evidence и recovery очереди | crypto, AML, inventory, principal, fee, allocation, sweep, reserve, recovery |
| [operations/crypto-aml-preflight.md](operations/crypto-aml-preflight.md) | Read-only AML configuration preflight, compatibility contract обоих signer plugins и blockers перед переключением | crypto, AML, preflight, signer, version skew, rollout |
| [operations/crypto-aml-merchant-pools.md](operations/crypto-aml-merchant-pools.md) | Автопополнение merchant pools, finite budgets, durable intents, control-plane consumer и baseline-gated intake/rotation | crypto, AML, merchant pool, watermarks, queue, preflight, baseline, provisioning |
| [operations/crypto-aml-batches.md](operations/crypto-aml-batches.md) | Batch intake/capacity, gross/age limits, execution hold и trusted merchant wallet provisioning через отдельный control plane | crypto, AML, batch, capacity, collector, provisioning, custody, enrollment |
| [operations/crypto-aml-resources.md](operations/crypto-aml-resources.md) | Operating gas capital отдельно от merchant net/fee, native funding/fee budgets, final cost/revert accounting и удержание unknown outcome | crypto, AML, native, gas, resource, funding, fee, reserve |
| [operations/crypto-merchant-custody.md](operations/crypto-merchant-custody.md) | Изоляция principal, immutable owner, независимый Vault registry и переключение custody policy | crypto, merchant custody, owner, allocation, gas, funding, vault, isolation |
| [operations/crypto-wallet-pool.md](operations/crypto-wallet-pool.md) | Автопополнение wallet pool, activation budget и CNO lifecycle | wallet pool, provisioning, activation, crypto network operation, budget, capacity |
| [operations/worker-reliability.md](operations/worker-reliability.md) | Аренды и fencing Outbox/webhook/cron, восстановление после сбоев и обновление tenant DB | worker, outbox, lease, fencing, cron, catch-up, recovery, upgrade |
| [operations/merchant-webhooks-production-readiness.md](operations/merchant-webhooks-production-readiness.md) | Conformance, SLO, chaos, kill switch и Mainnet canary для merchant invoice webhooks | merchant api, webhook, delivery, reconciliation, nile, sepolia, canary, production gate |
| [operations/deployment-guide.md](operations/deployment-guide.md) | Точка входа: выбор local, публичного Nile или production-контура и правила версии кода | deployment, compose, version, local, test, production |
| [operations/public-nile-deployment.md](operations/public-nile-deployment.md) | Cross-platform runbook публичного disposable TRON Nile / Ethereum Sepolia стенда | nile, public test, docker compose, tls, vault, e2e |
| [operations/canonical-stand-update-2026-11-04.md](operations/canonical-stand-update-2026-11-04.md) | Обновление существующего Nile/Sepolia стенда на канонический runtime с сохранением данных, backup/restore и smoke | canonical, deploy, schema 76, backup, restore, nile, sepolia |
| [operations/production-deployment.md](operations/production-deployment.md) | Production-инварианты, rollout/rollback, backup/restore, мониторинг и Mainnet gate | production, rollout, rollback, backup, restore, dr, mainnet |
| [operations/vault-tron-production.md](operations/vault-tron-production.md) | Production-контур Vault Proxy, AppRole, Transit, TRON plugin, rotation и внешний Vault | vault, proxy, approle, transit, tron, signing, secret zero, rotation, raft |
| [operations/local-vault-recovery.md](operations/local-vault-recovery.md) | Восстановление существующего локального тестового стенда после выключения компьютера: unseal, новый wrapped SecretID, Proxy и проверка входа | vault, recovery, restart, sealed, approle, login, powershell |
| [operations/rpc-ingestion.md](operations/rpc-ingestion.md) | RPC routing, bounded EVM prefetch, atomic checkpoint и read-only ingestion preflight | rpc, ingestion, sepolia, watcher, lag, circuit, preflight |
| [operations/vault-evm-sepolia.md](operations/vault-evm-sepolia.md) | Отдельный EVM Vault mount, Sepolia custody, tenant seed, sweep allowlist, preflight и rollout | vault, evm, ethereum, sepolia, custody, pyusd, signing, preflight |
| [operations/vault-kubernetes-migration.md](operations/vault-kubernetes-migration.md) | Будущая замена AppRole на Kubernetes auth без изменения application code | vault, kubernetes, auto-auth, serviceaccount, projected token, migration |

---

## Связанные документы
- [meta/00-doc-standard.md](meta/00-doc-standard.md) — правила, по которым написан этот навигатор и все остальные файлы
