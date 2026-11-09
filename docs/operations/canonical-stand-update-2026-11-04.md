# Обновление тестового стенда 04.11.2026

> **TL;DR:** Существующий Nile/Sepolia стенд обновлён до `canonical-20261104T153835Z` с сохранением данных; readiness, MFA, RPC и сверка балансов прошли.
>
> **Тип:** Reference
>
> **Аудитория:** Developer, QA, Admin
>
> **Связанные:** [public-nile-deployment.md](public-nile-deployment.md), [crypto-canonical-contracts.md](../reference/crypto-canonical-contracts.md)

## 1. Развёрнутый release

Проект Docker Compose: `metapus-public-test`. Tenant: `nile-e2e`,
`01a0cfbe-5e01-72f5-8484-2046b6a672a5`. Публичный адрес:
[https://176.215.182.82](https://176.215.182.82).

API, worker, frontend, ingestion monitor и AML pool controller собраны из
текущего checkout и запущены 04.11.2026 в 20:43 по Екатеринбургу. Checkout
не содержит `.git`; commit отсутствует. Хеши исходных файлов, build log,
имена и фактические Docker image IDs сохранены в deployment backup.
Операторский образ: `metapus-operator:canonical-20261104T153835Z`.

| Компонент | Образ | Image ID |
|---|---|---|
| API / worker | `metapus-runtime:canonical-20261104T153835Z` | `sha256:c67829dc25e3e0a2728316aefdb9fdb6f236e38249d56495fedb57255b26a8d0` |
| Frontend | `metapus-frontend:canonical-20261104T153835Z` | `sha256:c483cd8ce1fd59c99b602b2d5c3fe018fcb022e9040efe20d99ec08a75dc5c63` |
| Monitor | `metapus-monitor:canonical-20261104T153835Z` | `sha256:a645abd43822debff8ee23f28b0abd1fb59fbfe20dbbcc061383473f585396fd` |
| Controller | `metapus-aml-control:canonical-20261104T153835Z` | `sha256:8259fde55ead3aab190eb6280843e7c9bda14be25b1dc1b580227cc82fcc976a` |

Сохранены прежние PostgreSQL, Vault с обоими family plugins, Vault proxies,
TLS, reverse proxy и кошельки. Runtime manifest и `METAPUS_VERSION` обновлены.
Права Vault и network policies не менялись.

## 2. Переход схемы с сохранением данных

На исходном стенде уже были schema 76 и TRON evidence schema 2. Выполнен
отдельный операторский переход в одной транзакции:

- ограничение `chk_crypto_network_policy_enablement` приведено к текущему DDL;
- удалена колонка `sys_crypto_network_policies.fee_wallet_id`;
- ограничение `decoder_version` закреплено на значении 2.

Версия Goose осталась 76: новые пронумерованные миграции не добавлялись.
Обычный `migrate --all` не применил бы изменения ранее выполненных миграций.
Скрипт проверяет исходную версию и отсутствие ingestion states со схемой 1,
использует advisory lock и ограниченные lock/statement timeouts.

Сначала переход проверен на восстановленной из dump изолированной базе.
Перед фактическим переходом остановлены API, worker, monitor и controller.
Сравнены количество строк и хеши содержимого всех 214 public tables до и
после DDL. Они совпали; при сравнении политики исключалось только удаляемое
поле. Ledger, резервы, signed payloads, checkpoints и audit evidence при
переходе не переписывались.

## 3. Backup и проверка восстановления

Каталог вне репозитория:

```text
/Users/alexandrbogatiuk/.metapus/public-test-20260923/backups/canonical-20261104T153835Z
```

Содержит custom-format dumps tenant и meta database, PostgreSQL globals,
Vault Raft snapshot, manifests до/после, SQL перехода, хеши исходников,
контрольные данные и результаты проверок. Доступ: каталог `0700`, файлы `0600`.
`SHA256SUMS` проверен.

Финальные dumps действительно восстановлены в отдельном PostgreSQL 17
без сетевого доступа. Хеши всех 214 tenant tables совпали с согласованной
исходной точкой; meta database восстановлена без ошибок. Временный контейнер
удалён. Vault не изменялся; его Raft snapshot сохранён, восстановление Vault
в рамках этого обновления не проводилось.

Старый runtime ожидает удалённую колонку. Откат требует согласованного
восстановления исходной схемы и данных вместе с прежним manifest; замены
одного image недостаточно. После новых финансовых операций сохранённая
точка требует отдельной оценки перед восстановлением.

## 4. Результаты проверок

| Проверка | Результат |
|---|---|
| Docker build всех обновляемых компонентов | Успешно; frontend production build включён |
| API internal `/health/ready` | Успешно |
| Public HTTPS `/health/live`, `/login` | HTTP 200; TLS verification включена |
| Admin password + существующий TOTP | HTTP 200 |
| Cookie refresh дважды и `/auth/me` | HTTP 200; один активный refresh token проверочной сессии |
| Чтение network policies | Обе enabled, JIT; `feeWalletId` отсутствует; версии 18 и 15 сохранены |
| AML preflight до/после | `ready=true`, custody и signing contracts готовы для обеих сетей |
| Ingestion после catch-up | Nile и Sepolia `ready=true`, leases активны |
| TRON RPC | `ready=true`; три независимых provider для каждой роли при требовании двух |
| TRON digests | Все шесть сравнений трёх высот по двум ролям совпали |
| TRON internal transfer sample | Положительный sample подтверждён тремя provider в обеих ролях |
| Sepolia RPC | `ready=true`; Alchemy и Infura подтвердили свежую и историческую высоты |
| Плановая сверка после старта | 64/64 пары, 0 read errors, 0 созданных/изменённых incidents |
| Открытые reconciliation incidents | 0 |
| Restore tenant и meta backups | Успешно |

Проверочная auth-сессия и её refresh tokens отозваны отдельно; существующие
пользовательские сессии не отзывались. Новые переводы, withdrawal, funding
или платные AML-запросы операторскими проверками не создавались. Полный
финансовый E2E с новыми операциями в это обновление не входил.

## 5. Ограничения RPC-провайдеров

`getblock-nile` недоступен в обеих ролях и продолжает давать `pool_exhausted`;
такие предупреждения были зафиксированы и до обновления. Quorum и positive
internal sample обеспечили TronGrid, Chainstack и Nileex. Провайдеры не
отключались, пороги и quorum не снижались.

В Sepolia исторический sample не покрыт всеми резервными endpoint:
Chainstack сообщил `archive_unavailable`, PublicNode — ошибки отдельных
receipt/logs reads, OnFinality — transport/decode failures. Alchemy и Infura
прошли все выбранные операции. Эти ограничения сохранены в RPC-отчётах;
общие RPC и ingestion checks завершились успешно.
