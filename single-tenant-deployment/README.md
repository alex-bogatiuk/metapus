# Развёртывание Metapus для одного tenant

Запуск Metapus из исходного кода: backend, frontend и worker через Docker
Compose.

> Этот каталог предназначен для local development и disposable Nile testing.
> Все `.env*` этого local/test checkout, включая сгенерированные файлы,
> содержат только тестовые данные; их раскрытие не является утечкой production
> credentials. Production deployment выполняется по отдельному runbook и не
> использует эти значения.
>
> Единая точка выбора контура:
> [`docs/operations/deployment-guide.md`](../docs/operations/deployment-guide.md).

## Требования

- Docker и Docker Compose v2;
- свободные порты **5432**, **8080** и **3000**; значения можно изменить в
  `.env`.

## Полный тестовый bootstrap

Для нового стенда с tenant, seed, независимыми TRON/EVM custody mounts и Transit keys используйте [public-test runbook](../docs/operations/public-nile-deployment.md) или [PowerShell-сценарий](DOCKER-EVM-TEST-STAND.md). Он содержит полный порядок подготовки signing boundary; одного `docker compose up` недостаточно.

## Ручная сборка Compose

Команды выполняются из `single-tenant-deployment/`. Перед запуском API/worker оператор должен подготовить Vault/Transit и signed approval policy по [Vault runbook](../docs/operations/vault-tron-production.md). Следующий фрагмент показывает инфраструктуру, tenant CLI и явный seed, но не заменяет Vault bootstrap.


```bash
# 1. Подготовьте окружение
cp .env.example .env
# Укажите auth secrets, PUBLIC_CHECKOUT_BASE_URL, AUTOMATION_ENCRYPTION_KEY
# и CRYPTO_* approval signer values, необходимые cmd/crypto-server.

# 2. Соберите образы
docker compose build

# 3. Запустите PostgreSQL
docker compose up -d postgres

# 4. Инициализируйте meta-БД: создаются база tenants и её schema
docker compose --profile init run --rm init init-meta

# 5. Создайте tenant: создаются БД и schema, выполняются migrations (без seed)
docker compose --profile init run --rm init create --slug default --name "My Company"
# Скопируйте Tenant ID из вывода команды.

# 6. Запишите UUID в .env
# TENANT_ID=<uuid-from-step-5>

# 7. Создайте администратора явным seed. ADMIN_PASSWORD_FILE — файл на хосте.
# TENANT_DATABASE_NAME — точное Database: из вывода create, не slug.
# ADMIN_PASSWORD_FILE и TENANT_DATABASE_NAME задаются оператором до команды.
docker compose --profile init run --rm \
  --entrypoint ./seed \
  -e DATABASE_URL="postgres://metapus:${POSTGRES_PASSWORD:-metapus}@postgres:5432/${TENANT_DATABASE_NAME}?sslmode=disable" \
  -e ADMIN_PASSWORD_FILE=/run/secrets/admin_password \
  -v "${ADMIN_PASSWORD_FILE}:/run/secrets/admin_password:ro" init

# 8. После подготовки Vault/Transit/signing policy запустите сервисы
docker compose up -d

# 9. Пересоберите frontend со встроенным TENANT_ID
docker compose build frontend
docker compose up -d frontend
```

После явного запуска `seed` (в образе это `cmd/crypto-seed`) с `ADMIN_PASSWORD_FILE` откройте
**http://localhost:3000** и войдите как `admin@metapus.io`, используя secret из
указанного файла. Команда сида не имеет пароля по умолчанию. `run-docker-stand.ps1` имеет собственный test-only default; при запуске задавайте `-AdminPassword` явно.

При `ADMIN_MFA_MODE=required` завершите регистрацию TOTP и последующие
step-up-проверки через интерфейс.

## Сервисы

| Сервис | Порт | Назначение |
|---|---|---|
| `postgres` | 5432 | PostgreSQL 17 |
| `metapus-app` | 8080 | Go API server |
| `metapus-worker` | — | Background jobs |
| `pdf-renderer` | не публикуется | Внутренний Chromium PDF service |
| `frontend` | 3000 | Next.js UI |
| `vault-test` | 8200, только `127.0.0.1` | Опциональный test-only Vault с Integrated Raft |
| `vault-proxy` | не публикуется | Tokenless boundary между worker и Vault |

## Публичный disposable-стенд TRON Nile

Полный путь от свежего checkout до HTTPS, tenant/Vault bootstrap, preflight,
MFA, funding, E2E-приёмки, restart, backup и teardown находится в
[`docs/operations/public-nile-deployment.md`](../docs/operations/public-nile-deployment.md).
Короткие фрагменты здесь намеренно не дублируются: public runbook является
единственным source of truth для интернет-доступного тестового контура.

## Частые команды

```bash
# Логи
docker compose logs -f metapus-app
docker compose logs -f frontend

# Список tenant
docker compose --profile init run --rm init list

# Пересборка после изменения кода
docker compose build && docker compose up -d

# Остановка
docker compose down

# Остановка с удалением volumes — УДАЛЯЕТ ДАННЫЕ
docker compose down -v
```

## Ручное обновление

Обновление выполняется только оператором и одним собранным релизом для API,
worker, CLI и frontend.

```bash
# 1. Сделайте резервные копии meta-БД и tenant-БД.

# 2. Соберите и проверьте новый релиз до остановки сервиса.
docker compose build

# 3. Остановите процессы, которые обращаются к tenant-БД.
docker compose stop metapus-app metapus-worker

# 4. Обновите meta-БД и все активные/приостановленные tenant-БД.
docker compose --profile init run --rm init init-meta
docker compose --profile init run --rm init migrate --all

# 5. Только после успешных миграций запустите единый новый релиз.
docker compose up -d
docker compose ps
```

`migrate --all` возвращает ненулевой код при любой ошибке. Не запускайте server
и worker до устранения причины или восстановления БД из резервной копии.

## Конфигурация

Полный перечень находится в `.env.example`.

| Переменная | Обязательна | По умолчанию | Назначение |
|---|---|---|---|
| `JWT_SECRET` | **да** | — | Secret key для JWT |
| `PUBLIC_CHECKOUT_BASE_URL` | **да** | — | Публичный base URL checkout/payment links |
| `AUTH_MFA_ENCRYPTION_KEY` | **да** | — | 32-byte key в base64 для TOTP secrets и step-up |
| `AUTOMATION_ENCRYPTION_KEY` | **да** | — | 32-byte key для encrypted automation credentials |
| `TENANT_ID` | **да** | — | UUID из `init create` |
| `CRYPTO_APPROVAL_POLICY_PUBLIC_KEY` | **да** | — | Public key platform security для проверки signed approval policy bundle |
| `CRYPTO_APPROVAL_POLICY_BUNDLE_JWS` | **да** | — | Signed approval policy bundle tenant |
| `VAULT_ADDR` | **да** | `http://vault-proxy:8100` | Tokenless Vault Proxy для Transit approval signing |
| `CRYPTO_TRANSIT_MOUNT` | нет | `transit` | Transit mount с tenant/role-scoped non-exportable Ed25519 keys |
| `POSTGRES_PASSWORD` | нет | `metapus` | Пароль PostgreSQL |
| `LOG_LEVEL` | нет | `info` | `debug`, `info`, `warn` или `error` |
| `ADMIN_MFA_MODE` | нет | `required` | `off`, `optional` или `required` для admin login MFA |
| `ADMIN_MFA_GRACE_PERIOD_HOURS` | нет | `168` | Grace period для admin без TOTP при required mode |
| `ADMIN_STEP_UP_TTL_SECONDS` | нет | `300` | TTL свежего подтверждения sensitive action |
| `ADMIN_SECURITY_MODE` | нет | `warn` | Admin CIDR mode: `off`, `warn` или `enforce` |
| `ADMIN_ALLOWED_CIDRS` | нет | пусто | Разделённый запятыми admin IP/CIDR allowlist |
| `TRUSTED_PROXIES` | нет | пусто | Trusted reverse proxies для определения client IP |
| `APP_PORT` | нет | `8080` | Host port API server |
| `FRONTEND_PORT` | нет | `3000` | Host port frontend |
| `POSTGRES_PORT` | нет | `5432` | Host port PostgreSQL |

Production-подпись TRON использует выделенный tokenless Vault Proxy и
per-tenant Transit authorization keys. Тестовый профиль, одноразовый AppRole
bootstrap, policy, rotation и переход на внешний Vault описаны в
[`docs/operations/vault-tron-production.md`](../docs/operations/vault-tron-production.md).

Для полного теста приём → свип → выплата используйте
[`docs/crypto/crypto-processing-test-scenarios.md`](../docs/crypto/crypto-processing-test-scenarios.md),
а не упрощённый password-only script: production-like поток требует TOTP и
object-bound step-up.
