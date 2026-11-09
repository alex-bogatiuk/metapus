# Развёртывание Metapus: выбор контура

> **TL;DR:** Сначала выберите режим, затем используйте ровно один канонический
> runbook. Local/test Compose и закоммиченные `.env*` не являются production
> deployment.
>
> **Тип:** Guide
>
> **Аудитория:** Developer, QA, DevOps, Security, SRE

## 1. Выбор режима

| Цель | Каноническая инструкция | Допустимые сети и данные |
|---|---|---|
| Локальная разработка на Windows | [crypto-environment-setup.md](../crypto/crypto-environment-setup.md) | TRON Nile, test-only |
| Публичный disposable E2E | [public-nile-deployment.md](public-nile-deployment.md) | TRON Nile и опционально Ethereum Sepolia, test-only |
| Проверка полного финансового цикла | [crypto-processing-test-scenarios.md](../crypto/crypto-processing-test-scenarios.md) | TRON Nile / Ethereum Sepolia по матрице, test-only |
| Production/self-hosted | [production-deployment.md](production-deployment.md) | Только после всех release gates |
| Vault signing boundary | [vault-tron-production.md](vault-tron-production.md) | Компонент production-контура |

`single-tenant-deployment/docker-compose.yml`,
`single-tenant-deployment/docker-compose.public-test.yml`,
`deployments/vault-test/*`, `scripts/dev/*` и все закоммиченные `.env*`
предназначены для разработки и тестов. Их примерные значения являются
тестовыми данными; раскрытие этих значений не является раскрытием production
credentials.

Сгенерированные local/test `.env*` этого checkout, включая
`.env.crypto.local` и `public-test.env`, также содержат только тестовые
credentials. Они не образуют production secrecy boundary и никогда не
переносятся в production.

Production credentials никогда не получают копированием значений из `.env*`.
Их выпускает выбранный secret manager/KMS/Vault в рамках production ceremony.

## 2. Какая версия кода разворачивается

Local-build Compose собирает содержимое текущего checkout в момент
`docker compose build`. Он не выполняет `git fetch`, не переключает ветку и не
обновляет уже запущенные контейнеры.

Перед каждым новым стендом оператор фиксирует:

```bash
git rev-parse HEAD
git status --short
```

Правила:

1. `METAPUS_VERSION` — метка внутри бинарника, а не механизм загрузки кода.
2. Изменение файлов требует нового `docker compose build` и recreate сервиса.
3. Для воспроизводимого теста в отчёте сохраняются commit и наличие локального
   diff.
4. Production собирается только из чистого commit/tag; server, worker, tenant
   CLI и frontend получают один release ID.
5. Production rollout использует immutable image digest. Тег `latest`
   запрещён.

Если нужен «самый актуальный код», сначала обновите выбранную основную ветку,
перенесите поверх неё требуемые изменения, выполните quality gates и только
затем собирайте новый release. Автоматически считать удалённую ветку
«актуальной» deployment-команды не должны.

## 3. Общие quality gates

До тестового или production build:

```bash
go test -count=1 ./...
golangci-lint run --timeout=5m

cd frontend
npm ci
npm run typecheck
npm run lint
npm run build
cd ..

git diff --check
```

Дополнительно выполняются race-тесты затронутых crypto/Vault/auth пакетов,
тесты обоих Go modules `vault-sign-plugins/tron` и `vault-sign-plugins/evm`, SDK, HTTP contracts и PostgreSQL integration tests с `-tags integration`. Точный
финансовый acceptance находится в
[crypto-processing-test-scenarios.md](../crypto/crypto-processing-test-scenarios.md).

## 4. Общие критерии готовности

Независимо от платформы стенд не считается готовым, пока не подтверждены:

- ожидаемые commit/release ID и schema version;
- health/readiness всех компонентов;
- отсутствие прямой интернет-публикации PostgreSQL, Vault и worker;
- корректные trusted proxy и client IP;
- Vault readiness и отрицательные ACL smoke;
- один активный refresh-token после MFA reload regression;
- ledger/blockchain reconciliation без открытых incidents;
- backup создан и восстановление проверено для production;
- deployment report не содержит credentials или приватных ключей.

## 5. Источники истины

При расхождении документов используйте следующий приоритет:

1. migration schema и runtime validation текущего release;
2. соответствующий deployment runbook из `docs/operations`;
3. [crypto-processing-architecture.md](../crypto/crypto-processing-architecture.md);
4. [crypto-processing-test-scenarios.md](../crypto/crypto-processing-test-scenarios.md);
5. учебные материалы и исторические control runs.

Исторический control run доказывает поведение указанного commit, но не заменяет
проверку нового release.
