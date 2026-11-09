# Развёртывание Metapus с криптовалютным контуром с нуля

> **TL;DR:** `crypto-setup.ps1` поднимает PostgreSQL, custom Vault plugin, tenant schema и Nile wallets; затем отдельно запускаются server, worker и frontend.
>
> **Тип:** How-To
>
> **Аудитория:** Developer, DevOps, QA, Admin
>
> **Связанные:** [crypto-processing-architecture.md](crypto-processing-architecture.md), [crypto-security-and-risk-review.md](crypto-security-and-risk-review.md), [crypto-processing-test-scenarios.md](crypto-processing-test-scenarios.md)
>
> Канонический путь для локальной Windows dev-среды на TRON Nile. Для
> cross-platform публичного disposable стенда используйте
> [public-nile-deployment.md](../operations/public-nile-deployment.md).
>
> Production-контур Vault Proxy/AppRole/Transit описан отдельно и проверен на
> Nile. Профиль `vault-test` запрещён для кошельков с реальной стоимостью.

---

## 1. Что будет развёрнуто

После выполнения инструкции будут работать:

- PostgreSQL meta DB и отдельная tenant DB;
- dev Vault с custom TRON signing plugin;
- tenant `demo`;
- каталоги TRON Nile, USDT-TRC20 и TRX;
- hot wallet и generation pool-кошельков, полученные из Vault;
- server;
- worker с watcher, activation, sweep, withdrawal, treasury, resources и reconciliation;
- frontend;
- обязательный MFA flow для администратора.

Setup создаёт test-only ключи policy/intent/risk/approvals и сохраняет локальную
конфигурацию в `.env.crypto.local`. Как и остальные `.env*` этого репозитория,
это тестовые данные: их раскрытие не является раскрытием production
credentials. Файл всё равно не переносится в production и не используется как
источник production secret; production значения выпускаются заново.

## 2. Предварительные требования

### ПО

- Windows 10/11 и PowerShell 7;
- Docker Desktop с Linux containers;
- Go 1.27+;
- Node.js, совместимый с Next.js 16, и npm;
- Git;
- доступ к `https://nile.trongrid.io`;
- свободные локальные порты 5432, 8080, 8200 и 3000.

Проверка:

```powershell
docker version
go version
node --version
npm --version
```

### Тестовые blockchain-данные

Нужны:

- отдельная mnemonic только для Nile;
- TronGrid API key — рекомендуется, хотя RPC может работать и без него;
- внешний Nile-адрес получателя;
- внешний кошелёк-плательщик с тестовым USDT и TRX/Energy;
- тестовый TRX для hot wallet.

Для проверки iTRX создайте аккаунт провайдера в админском UI и задайте Nile Base URL, API key и API secret в его credentials.

Не используйте mainnet mnemonic и реальные секреты в local setup.

## 3. Важный выбор PoolAccount

Pool-адреса детерминированы:

```text
m/44'/195'/{PoolAccount}'/0/{index}
```

Удаление локальной БД не удаляет историю адресов в TRON. Если та же mnemonic и тот же `PoolAccount` уже использовались, preflight может обнаружить on-chain balance или историю и поставить generation на hold.

Для каждого чистого E2E используйте новый неотрицательный account index, например:

```text
10, затем 11, затем 12
```

Не снимайте `chain_balance_check_pending` или reconciliation hold вручную ради прохождения setup.

## 4. Подготовка репозитория

```powershell
Set-Location C:\Users\alex\go\src\metapus
git status --short
```

Установите frontend dependencies:

```powershell
Set-Location .\frontend
npm install
Set-Location ..
```

Проверьте, что Docker Desktop запущен.

## 5. Конфигурация секретов

Есть два безопасных для dev варианта.

### Вариант A: передать mnemonic параметром и не сохранять

```powershell
.\scripts\dev\crypto-setup.ps1 `
  -Mnemonic "<NILE TEST MNEMONIC>" `
  -TronApiKey "<TRONGRID KEY>" `
  -WithdrawalAddress "<EXTERNAL NILE ADDRESS>" `
  -PoolAccount 10 `
  -NoPersistMnemonic
```

### Вариант B: локальный `.env.crypto.local`

Создайте файл, не добавляя его в Git:

```dotenv
TRON_DEV_MNEMONIC=<NILE TEST MNEMONIC>
CRYPTO_PROCESSING_ENABLED=true
CRYPTO_CHAIN_CONFIG_FILE=<ABSOLUTE PATH TO .crypto/chains.dev.json>
TRON_TEST_RPC_URL=https://nile.trongrid.io
TRON_TEST_API_KEY=<OPTIONAL KEY FOR DEV HELPERS>
TRON_WITHDRAWAL_TEST_ADDRESS=<EXTERNAL NILE ADDRESS>

```

Worker читает API key только через `apiKeySecretFile` внутри chain topology;
`TRON_TEST_API_KEY` используется исключительно прямыми dev-helper скриптами.

Затем:

```powershell
.\scripts\dev\crypto-setup.ps1 -PoolAccount 10
```

`crypto-setup.ps1` объединяет новые значения с существующим `.env.crypto.local`. Credentials iTRX задаются только через UI.

## 6. Чистый запуск или сохранение данных

### Полностью чистая disposable dev-среда

Команда ниже удаляет локальные PostgreSQL volumes:

```powershell
docker compose down -v
docker rm -f metapus-vault 2>$null
```

После этого выполните setup с новым `PoolAccount`.

### Сохранить существующий dev Vault

```powershell
.\scripts\dev\crypto-setup.ps1 -KeepVault -PoolAccount 10
```

Используйте `-KeepVault` только если Vault уже создан этим же проектом и его tenant config/HD seed нужно сохранить. Нельзя одновременно менять mnemonic и переиспользовать wallet codes с финансовой историей.

## 7. Что делает `crypto-setup.ps1`

Скрипт является каноническим bootstrap для local crypto и выполняет:

1. Запускает только PostgreSQL service из `docker-compose.yml`.
2. Собирает Linux binary custom TRON Vault plugin.
3. Запускает отдельный dev Vault container с примонтированным plugin directory.
4. Регистрирует и включает plugin на path `tron`.
5. Инициализирует meta DB.
6. Создаёт либо находит tenant.
7. Применяет tenant migrations.
8. Запускает seed без synthetic crypto history.
9. Генерирует role-separated Ed25519 keys и signed approval policy bundle.
10. Загружает `tron_tenant_signing_config.v3`, authorization keyring, sweep
    allowlist и HD seed в локальный Vault.
11. Выводит hot/pool addresses из Vault.
12. Привязывает `vault_key_id` и derivation path к `cat_wallets`.
13. Запускает on-chain pool preflight.
14. Отказывается продолжать, если generation не чиста.
15. Записывает `.env.crypto.local` и `frontend/.env.local`.

Корневой Compose больше не содержит небезопасный dev-сервис Vault. Для ручной
локальной разработки используйте setup script; для проверки production-границы
Proxy/AppRole/Transit используйте профиль `vault-test` по runbook
[`../operations/vault-tron-production.md`](../operations/vault-tron-production.md).

## 8. Проверка результата setup

Импортируйте env:

```powershell
. .\scripts\dev\_crypto-env.ps1
Import-CryptoEnv | Out-Null
```

Проверьте контейнеры:

```powershell
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
```

Ожидаются:

- `metapus-db`;
- `metapus-vault`.

Проверьте tenant:

```powershell
docker exec metapus-db psql -U metapus -d tenants -c `
  "SELECT id,slug,db_name,status FROM tenants ORDER BY created_at;"
```

Проверьте wallets:

```powershell
docker exec metapus-db psql -U metapus -d $env:TENANT_DB -c `
  "SELECT w.code,w.tier,w.status,w.chain_activation_status,w.address,w.vault_key_id,
          EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds h
                 WHERE h.wallet_id=w.id AND h.status='open') AS allocation_held
   FROM cat_wallets w WHERE w.code LIKE '%TRON%' ORDER BY w.code;"
```

Сразу после setup hot wallet должен быть `active`. Pool wallets могут потребовать
on-chain активации worker-ом, но preflight не должен оставить открытый
owner-bound allocation hold.

Проверьте Vault tenant config без вывода seed:

```powershell
docker exec `
  -e "VAULT_ADDR=http://127.0.0.1:8200" `
  -e "VAULT_TOKEN=$env:VAULT_TOKEN" `
  metapus-vault vault read "tron/tenants/$($env:TENANT_ID)/config/authorization"
```

В выводе должны присутствовать schema version и public keys. Не сохраняйте полный вывод в общий отчёт, если plugin показывает чувствительные поля.

## 9. Финансирование hot wallet

Worker активирует несуществующие TRON pool accounts через `wallet/createaccount`. Для этого hot wallet должен иметь TRX.

Получите адрес:

```powershell
docker exec metapus-db psql -U metapus -d $env:TENANT_DB -At -c `
  "SELECT address FROM cat_wallets WHERE code='HOT-TRON-001' AND deletion_mark=FALSE;"
```

Отправьте на него тестовый Nile TRX. Для нескольких pool-адресов нужен запас на активации и возможный fallback network fee.

iTRX предоставляет Energy для TRC-20 transfer, но не заменяет TRX, необходимый для создания нового TRON account.

## 10. Запуск приложения

Откройте три PowerShell-окна в корне проекта.

Server:

```powershell
.\scripts\dev\crypto-run-server.ps1
```

Worker:

```powershell
.\scripts\dev\crypto-run-worker.ps1
```

Frontend:

```powershell
Set-Location .\frontend
npm run dev -- --port 3000
```

Адреса:

- API: `http://127.0.0.1:8080`;
- frontend: `http://localhost:3000`;
- Vault: `http://127.0.0.1:8200`;
- PostgreSQL: `localhost:5432`.

В worker должны стартовать TRC-20/native watchers, activation, sweep, withdrawal, treasury, resource и settlement loops. Если worker завершился, продолжать blockchain E2E нельзя.

## 11. Первая авторизация и MFA

Dev seed создаёт:

```text
admin@metapus.io
Admin123!
```

Setup задаёт:

```dotenv
ADMIN_MFA_MODE=required
ADMIN_MFA_GRACE_PERIOD_HOURS=0
ADMIN_SECURITY_MODE=enforce
ADMIN_ALLOWED_CIDRS=127.0.0.1/32,::1/128
```

Поэтому первый login не обязан вернуть готовый JWT. Он возвращает `202 mfa_enrollment_required`, а frontend проводит пользователя через:

1. login;
2. начало enrollment;
3. добавление TOTP в authenticator;
4. подтверждение кода;
5. сохранение backup codes.

Для следующих login будет `202 mfa_required`.

Чувствительные действия вызывают step-up dialog. Step-up требует одновременно пароль и текущий TOTP. Backup code не подходит.

`crypto-create-invoice.ps1` работает через явно созданный `MERCHANT_API_KEY` и
завершается при первой ошибке. `crypto-create-withdrawal.ps1` и
`crypto-post-provider-opening.ps1` не являются каноническим MFA-потоком: для
production-like проверки выплаты используйте frontend либо MFA-aware API client.

## 12. Дать администратору доступ к dev-мерчанту

Seed создаёт `MERCHANT-001`, но setup не гарантирует связь admin user с ним.

Для disposable dev-среды:

```powershell
docker exec metapus-db psql -U metapus -d $env:TENANT_DB -c `
  "INSERT INTO sys_merchant_users(user_id,merchant_id,role)
   SELECT u.id,m.id,1
   FROM auth_users u
   CROSS JOIN cat_merchants m
   WHERE u.email='admin@metapus.io'
     AND m.code='MERCHANT-001'
   ON CONFLICT (user_id,merchant_id)
   DO UPDATE SET role=1;"
```

Здесь `role=1` — Owner. В production связь должна создаваться через административный workflow, а не прямым SQL.

После изменения merchant access старый JWT станет stale. Выполните logout/login или refresh.

## 13. Дождаться активации pool

Проверяйте:

```powershell
docker exec metapus-db psql -U metapus -d $env:TENANT_DB -c `
  "SELECT w.code,w.status,w.chain_activation_status,
          EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds h
                 WHERE h.wallet_id=w.id AND h.status='open') AS allocation_held
   FROM cat_wallets w
   WHERE w.tier='pool' AND w.deletion_mark=FALSE
   ORDER BY w.code;"
```

Готовый к выдаче кошелёк:

```text
status = free
chain_activation_status = active
allocation_held = false
```

Активации:

```powershell
docker exec metapus-db psql -U metapus -d $env:TENANT_DB -c `
  "SELECT a.wallet_id,a.status AS task_status,a.cost_status,
          o.number,o.operation_type,o.status AS operation_status,o.posted,
          o.tx_hash,o.cost_amount,o.chain_settlement_id,o.finalized_at,
          o.manual_edit_allowed,a.error_message
   FROM reg_crypto_account_activations a
   LEFT JOIN doc_crypto_network_operations o ON o.id=a.network_operation_id
   ORDER BY a.created_at;"
```

Для принятой сетью операции ожидается один `CNO*` документ. До finality он имеет
`operation_status=broadcast` и `posted=false`. После обработки financially final
receipt ожидается `succeeded` или `reverted`, заполненные settlement/finality и
`posted=true`. Для уже активного адреса и rejected pre-broadcast документа нет.

Расходы:

```powershell
docker exec metapus-db psql -U metapus -d $env:TENANT_DB -c `
  "SELECT purpose,cost_type,record_type,amount,external_order_id,period
   FROM reg_crypto_platform_cost_movements
   WHERE purpose='activation'
   ORDER BY period;"
```

Для активации фактическая комиссия имеет `cost_type=chain_network_fee`,
`purpose=activation`, `provider=chain`, а `external_order_id` равен TX hash.
Нулевая fee допускает проведённый документ без строк в movements.

Повторный restart worker не должен создавать новую activation transaction для уже активного адреса.

## 14. Настройка iTRX

Если credentials отсутствуют, resource provider считается `unconfigured`.
`rental_required` и `prefer_rental` в этом состоянии fail closed; автоматический
burn не включается. Работа без iTRX разрешена только явной policy `burn_only`
с положительным bounded budget.

Для реального iTRX E2E создайте активный аккаунт провайдера для сети Nile в UI,
сохраните Base URL, API key и API secret, затем добавьте аккаунт в порядок политики
ресурсов и выберите `prefer_rental` или `rental_required`.

Общие настройки воркера: `CRYPTO_RESOURCE_LEASE_TARGET_ENERGY=65000`,
`CRYPTO_RESOURCE_SAFETY_MARGIN_PCT=10`,
`CRYPTO_RESOURCE_RESERVATION_CUTOFF_SECONDS=120`.

Первое наблюдение положительного provider balance создаёт incident/observation, а не самовольный opening. Opening должен быть проведён контролируемо через соответствующий документ и maker-checker. Dev helper может помочь только после завершённой MFA-сессии или временной адаптации клиента под MFA.

Проверка:

```powershell
docker exec metapus-db psql -U metapus -d $env:TENANT_DB -c `
  "SELECT p.code,i.status,i.ledger_amount,i.provider_amount,i.delta_amount,i.last_seen_at
   FROM reg_crypto_provider_balance_incidents i
   JOIN cat_crypto_provider_accounts p ON p.id=i.provider_account_id
   ORDER BY i.last_seen_at DESC;"
```

## 15. Создание Merchant API key

В portal войдите как Owner:

```text
Portal -> Developers -> API keys
```

Создание key требует step-up. Plaintext key показывается один раз. В БД хранится только SHA-256 hash и prefix.

Минимальные scopes для приёма:

```text
invoice:create
invoice:read
address:create
```

Сохраните key в secret manager/Postman environment, но не в документации. Если нужен CLI:

```powershell
$env:MERCHANT_API_KEY = "<PLAINTEXT KEY>"
```

## 16. Базовая smoke-проверка

### Server health

Используйте доступный health endpoint окружения или проверьте, что login page и API отвечают без connection error.

### Merchant API auth

```powershell
$headers = @{
  "X-Api-Key" = $env:MERCHANT_API_KEY
  "Idempotency-Key" = "smoke-invoice-001"
  "Content-Type" = "application/json"
}

$body = @{
  amount = "1000000"
  currency = "USDT-TRC20"
  orderId = "smoke-order-001"
  ttlMinutes = 60
} | ConvertTo-Json

Invoke-RestMethod `
  -Method Post `
  -Uri "http://127.0.0.1:8080/merchant/v1/invoices" `
  -Headers $headers `
  -Body $body
```

Для USDT с 6 decimals `1000000` означает 1 USDT.

Ожидаются:

- invoice id;
- opaque checkout ref;
- адрес оплаты;
- status `created`;
- expiry.

`X-Tenant-ID` в Merchant API не требуется и не должен использоваться для tenant resolution.

## 17. Остановка и повторный запуск

Остановите server/frontend/worker через `Ctrl+C`.

PostgreSQL и Vault:

```powershell
docker stop metapus-db metapus-vault
docker start metapus-db metapus-vault
```

После restart снова запустите server, затем worker, затем frontend.

Если Vault пересоздан без `-KeepVault`, его dev storage потерян. Повторно выполните setup и не пытайтесь подписывать средствами wallet records, которые больше не соответствуют seed/config Vault.

## 18. Production: обязательные отличия

Local script не является production deployment. Полный production rollout,
rollback, backup/restore и Mainnet gate описаны в
[`../operations/production-deployment.md`](../operations/production-deployment.md).
В production:

- PostgreSQL — managed/HA, TLS, backup/PITR;
- meta и tenant credentials — разные и с минимальными правами;
- Vault — не dev mode, с HA storage, TLS, audit и non-root auth;
- mnemonic/HD seed загружается через controlled ceremony;
- approval signing keys создаются как tenant/role-scoped non-exportable ключи Vault Transit и никогда не попадают в process environment;
- `JWT_SECRET` и `AUTH_MFA_ENCRYPTION_KEY` ротируются по процедуре;
- `ADMIN_MFA_MODE=required`;
- `ADMIN_SECURITY_MODE` вводится через `warn`, затем `enforce`;
- `TRUSTED_PROXIES` заполнен реальными reverse proxy ranges;
- worker имеет network egress только к TRON, Vault и разрешённым provider endpoints;
- merchant webhook delivery использует durable outbox;
- alerts настроены на held/failed/reconciliation/provider incidents.

Production-подпись authorization реализована через per-tenant Vault Transit keys
и выделенный tokenless Vault Proxy. Компонентная процедура AppRole bootstrap,
keyring/sweep policy и signing preflight описана в
[`../operations/vault-tron-production.md`](../operations/vault-tron-production.md).

## 19. Типовые проблемы

### Setup сообщает, что pool generation не чиста

Причина: адреса с той же mnemonic/account уже имеют on-chain состояние.

Решение: повторить с новым `-PoolAccount`. Не очищать hold вручную.

### Worker падает из-за signing keys

Проверьте импорт `.env.crypto.local` и запуск через:

```powershell
.\scripts\dev\crypto-run-worker.ps1
```

Для `CRYPTO_AUTH_SIGNER=local` обязательны authorization private key и все
approval public keys. Для production-подобного запуска выберите `transit` и
проверьте Proxy/AppRole/readiness по
[`../operations/vault-tron-production.md`](../operations/vault-tron-production.md).

### Нет свободного wallet для invoice

Проверьте:

- `chain_activation_status='active'`;
- `status='free'`;
- отсутствуют строки `reg_crypto_wallet_allocation_holds` со `status='open'`;
- hot wallet имеет TRX;
- нет открытого reconciliation incident.

### Выплата или свип зависли в `held`

Не создавайте новую операцию вручную. Сначала проверьте tx hash в Nile, signing authorization, resource allocation, settlement error и reconciliation.

### Portal не показывает мерчанта

Проверьте `sys_merchant_users`, затем обновите JWT.

### Admin login заблокирован по IP

Local setup разрешает только loopback. При reverse proxy настройте `TRUSTED_PROXIES`; для dev с другой машины добавьте её CIDR осознанно. Не переводите `ADMIN_SECURITY_MODE=off` в production как постоянное решение.

## 20. Следующий шаг

После успешного bootstrap выполните все сценарии из `crypto-processing-test-scenarios.md`. Smoke invoice подтверждает только API и wallet allocation; он не проверяет watcher, confirmations, проводки, свип, Vault signing, settlement и reconciliation.

## 21. Чеклист

- [ ] PostgreSQL и custom-plugin Vault запущены.
- [ ] Tenant создан, migrations и seed выполнены.
- [ ] Vault tenant config имеет schema `tron_tenant_signing_config.v3`.
- [ ] Sweep destination policy указывает на derived hot wallet текущего tenant.
- [ ] Hot/pool wallet addresses получены из Vault.
- [ ] Pool generation прошла on-chain preflight без hold.
- [ ] Hot wallet пополнен TRX, pool wallets активированы.
- [ ] Server, worker и frontend запущены.
- [ ] Admin завершил MFA enrollment.
- [ ] Admin связан с `MERCHANT-001`.
- [ ] Merchant API key создан и сохранён вне репозитория.
- [ ] Smoke invoice создаётся.

## 22. Файловая карта

```text
scripts/dev/crypto-setup.ps1             — полный local bootstrap
scripts/dev/_crypto-env.ps1              — merge/import .env.crypto.local
scripts/dev/crypto-run-server.ps1        — запуск server с required env
scripts/dev/crypto-run-worker.ps1        — запуск worker с required env
cmd/tenant/main.go                       — meta DB, tenant create и migrations
cmd/seed/main.go                         — Nile/dev reference data
cmd/crypto-preflight/main.go             — on-chain pool generation check
vault-sign-plugins/tron                  — custom Vault plugin
docker-compose.yml                       — local PostgreSQL; Vault plugin запускается setup script
```

## Связанные документы

- [crypto-processing-architecture.md](crypto-processing-architecture.md) — назначение компонентов.
- [crypto-security-and-risk-review.md](crypto-security-and-risk-review.md) — production controls и gates.
- [crypto-processing-test-scenarios.md](crypto-processing-test-scenarios.md) — следующий обязательный этап.
