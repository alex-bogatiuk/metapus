# Публичный disposable-стенд Metapus на TRON Nile и Ethereum Sepolia

> **TL;DR:** Этот runbook поднимает новый test-only tenant с кошельками обеих сетей, PostgreSQL,
> production-like Vault boundary, frontend/API/worker и доверенный HTTPS на
> bare IPv4. Mainnet и реальные средства запрещены.
>
> **Тип:** How-To
>
> **Аудитория:** DevOps, QA, Security, Developer
>
> **Рабочая директория:** корень checkout Metapus.

После развёртывания результат фиксируется по
[TRON v1 Nile acceptance protocol](crypto-nile-acceptance.md). Успешный smoke
этого runbook сам по себе не является Mainnet gate.

## 1. Граница стенда

Публичный поток:

```text
Internet :80/:443
  -> host public-edge-proxy
  -> 127.0.0.1:18080/18443
  -> container Nginx
  -> frontend или metapus-app

metapus-worker
  -> tokenless Vault Proxy
  -> TLS Vault + Integrated Raft
  -> Transit + TRON и EVM plugins
  -> TRON Nile и Ethereum Sepolia
```

Публично разрешены только `80/443`. PostgreSQL, API `8080`, frontend `3000`,
Vault `8200` и Vault Proxy `8100` не публикуются.

`docker-compose.public-test.yml` — override для двух тестовых сетей. Он не является Mainnet
или production manifest.

## 2. Предварительные условия

- Windows 11 + Docker Desktop, macOS + Docker Desktop либо Linux + Docker
  Engine;
- Docker Compose `2.24.4+` с поддержкой `!reset` и `!override`;
- Go `1.27+`, Git и curl;
- свободные host ports `80`, `443`, `18080`, `18443`;
- статический публичный IPv4 и router/NAT forwarding только для `80/443`;
- отдельная test-only mnemonic, внешние кошельки для Nile и Sepolia и при необходимости
  TronGrid/iTRX test credentials;
- TOTP authenticator;
- доступ к `nile.trongrid.io` и Let’s Encrypt ACME.

До HTTPS не отправляйте через интернет password, TOTP, cookies, API keys или
Vault credentials.

## 3. Зафиксировать исходный код

Local Compose собирает текущий checkout. Он не обновляет Git автоматически.

```bash
git rev-parse HEAD
git status --short
```

Для нового актуального стенда сначала обновите выбранную базовую ветку,
перенесите поверх неё требуемые изменения и выполните quality gates из
[deployment-guide.md](deployment-guide.md). В тестовом отчёте сохраните commit
и признак наличия локального diff.

`METAPUS_VERSION` — только release label. Он должен описывать собранный commit,
но сам не переключает исходники.

## 4. Создать test-only state вне репозитория

### PowerShell

```powershell
$StateDir = Join-Path $env:USERPROFILE '.metapus\public-test-YYYYMMDD'
$PublicIP = '203.0.113.10'
$Version = (git rev-parse --short=12 HEAD).Trim()

go run ./cmd/public-test-bootstrap init `
  --state-dir $StateDir `
  --public-ip $PublicIP `
  --version $Version `
  --network both

$EnvFile = Join-Path $StateDir 'public-test.env'
```

### bash/zsh

```bash
STATE_DIR="$HOME/.metapus/public-test-YYYYMMDD"
PUBLIC_IP="203.0.113.10"
VERSION="$(git rev-parse --short=12 HEAD)"

go run ./cmd/public-test-bootstrap init \
  --state-dir "$STATE_DIR" \
  --public-ip "$PUBLIC_IP" \
  --version "$VERSION" \
  --network both

ENV_FILE="$STATE_DIR/public-test.env"
```

Команда создаёт новую mnemonic, разделённые по семействам seed и test signing keys, Vault TLS,
secret-файлы и RPC-конфигурацию Nile/Sepolia. На Windows каталог получает ACL только для текущего
пользователя и `SYSTEM`; на macOS/Linux используются `0700/0600`.

При обновлении существующего Nile state добавьте отсутствующую сеть командой
`public-test-bootstrap configure-chains --state-dir <STATE_DIR>` из актуального
operator image. Команда сохраняет уже настроенные Nile endpoints, включая
relay URL. Затем выполните Vault bootstrap с `--family evm`,
`--wrap-secret-id=false` и EVM checksum при работающем Vault Proxy,
пересоздайте disposable tenant БД по текущим миграциям и запустите
`configure-db`. Для нового state эти действия уже включены в общий bootstrap.

Все значения в репозиторных `.env*` являются тестовыми. Файлы, созданные в
`STATE_DIR`, также относятся только к disposable стенду, но остаются вне
Git, логов и тестового отчёта.

При необходимости заполните test-only файлы:

```text
STATE_DIR/secrets/tron_api_key
```

Пустой TronGrid key допустим при доступном public RPC. Для полного resource-rental теста создайте аккаунт iTRX через UI и сохраните credentials в каталоге провайдеров.

## 5. Единая Compose-команда

### PowerShell

```powershell
$Compose = @(
  '--project-name', 'metapus-public-test',
  '--env-file', $EnvFile,
  '-f', 'single-tenant-deployment\docker-compose.yml',
  '-f', 'single-tenant-deployment\docker-compose.public-test.yml'
)

function Invoke-PublicCompose {
  docker compose @Compose @args
  if ($LASTEXITCODE -ne 0) {
    throw "docker compose failed with exit code $LASTEXITCODE"
  }
}
```

### bash/zsh

```bash
COMPOSE_FILES=(
  -f single-tenant-deployment/docker-compose.yml
  -f single-tenant-deployment/docker-compose.public-test.yml
)

dc() {
  docker compose \
    --project-name metapus-public-test \
    --env-file "$ENV_FILE" \
    "${COMPOSE_FILES[@]}" \
    "$@"
}
```

До создания tenant в env-файле ещё нет `TENANT_ID`, поэтому для первой
проверки merge задайте только синтаксический UUID-placeholder и сразу удалите
его из shell. Этот UUID не используется при развёртывании:

```powershell
$env:TENANT_ID = '00000000-0000-0000-0000-000000000001'
Invoke-PublicCompose config --quiet
Remove-Item Env:TENANT_ID
```

```bash
TENANT_ID=00000000-0000-0000-0000-000000000001 dc config --quiet
```

После `finalize` повторите `config --quiet` уже с настоящим `TENANT_ID` из
обновлённого env-файла.

## 6. Host edge и Docker Desktop egress

### 6.1 Обязательный public edge

Compose намеренно публикует Nginx только на loopback `18080/18443`.
`public-edge-proxy` должен работать на host и является единственным listener
на публичных `80/443`. Он добавляет PROXY protocol с настоящим client IP.

Соберите binary:

```text
go build -o <STATE_DIR>/tools/public-edge-proxy[.exe] ./cmd/public-edge-proxy
```

На Windows выполните из elevated PowerShell и запускайте helper скрыто с
отдельными stdout/stderr log-файлами:

```powershell
$ToolDir = Join-Path $StateDir 'tools'
New-Item -ItemType Directory -Force -Path $ToolDir | Out-Null
go build -o (Join-Path $ToolDir 'public-edge-proxy.exe') ./cmd/public-edge-proxy

$EdgeProcess = Start-Process -WindowStyle Hidden -PassThru `
  -FilePath (Join-Path $ToolDir 'public-edge-proxy.exe') `
  -RedirectStandardOutput (Join-Path $StateDir 'public-edge.out.log') `
  -RedirectStandardError (Join-Path $StateDir 'public-edge.err.log')
$EdgeProcess.Id | Set-Content -Encoding ascii (Join-Path $StateDir 'public-edge.pid')
```

На Linux выдайте binary только capability bind low ports либо запустите его
через выбранный service manager. На macOS запускайте с требуемыми правами.
Для disposable foreground-проверки:

```bash
mkdir -p "$STATE_DIR/tools"
go build -o "$STATE_DIR/tools/public-edge-proxy" ./cmd/public-edge-proxy
sudo "$STATE_DIR/tools/public-edge-proxy"
```

Не публикуйте `18080/18443` в router. Router направляет внешний `80/443` на
host `80/443`.

### 6.2 Опциональные egress helpers

Сначала проверьте прямой HTTPS egress containers. Для direct mode добавьте в
`public-test.env`:

```dotenv
PUBLIC_TEST_NPM_HTTPS_PROXY=
PUBLIC_TEST_ACME_HTTPS_PROXY=
CRYPTO_PROCESSING_ENABLED=true
CRYPTO_CHAIN_CONFIG_FILE=/run/metapus/crypto/chains.json
```

Если npm/ACME direct egress не работает, удалите две пустые proxy-строки либо
задайте fallback явно:

```dotenv
PUBLIC_TEST_NPM_HTTPS_PROXY=http://host.docker.internal:19444
PUBLIC_TEST_ACME_HTTPS_PROXY=http://host.docker.internal:19445
```

Соберите restricted CONNECT helper один раз и запустите два экземпляра.
Разрешите только Docker subnet этого стенда и точные destinations:

```bash
mkdir -p "$STATE_DIR/tools"
go build -o "$STATE_DIR/tools/npm-connect-proxy" ./cmd/npm-connect-proxy

"$STATE_DIR/tools/npm-connect-proxy" -listen 0.0.0.0:19444 \
  -allowed-cidrs 172.30.250.0/24 \
  -allowed-targets registry.npmjs.org:443

"$STATE_DIR/tools/npm-connect-proxy" -listen 0.0.0.0:19445 \
  -allowed-cidrs 172.30.250.0/24 \
  -allowed-targets acme-staging-v02.api.letsencrypt.org:443,acme-v02.api.letsencrypt.org:443
```

PowerShell:

```powershell
$ConnectProxy = Join-Path $ToolDir 'npm-connect-proxy.exe'
go build -o $ConnectProxy ./cmd/npm-connect-proxy

$NpmProxyProcess = Start-Process -WindowStyle Hidden -PassThru `
  -FilePath $ConnectProxy `
  -ArgumentList @('-listen','0.0.0.0:19444','-allowed-cidrs','172.30.250.0/24','-allowed-targets','registry.npmjs.org:443') `
  -RedirectStandardOutput (Join-Path $StateDir 'npm-proxy.out.log') `
  -RedirectStandardError (Join-Path $StateDir 'npm-proxy.err.log')

$AcmeProxyProcess = Start-Process -WindowStyle Hidden -PassThru `
  -FilePath $ConnectProxy `
  -ArgumentList @('-listen','0.0.0.0:19445','-allowed-cidrs','172.30.250.0/24','-allowed-targets','acme-staging-v02.api.letsencrypt.org:443,acme-v02.api.letsencrypt.org:443') `
  -RedirectStandardOutput (Join-Path $StateDir 'acme-proxy.out.log') `
  -RedirectStandardError (Join-Path $StateDir 'acme-proxy.err.log')

@($NpmProxyProcess.Id, $AcmeProxyProcess.Id) |
  Set-Content -Encoding ascii (Join-Path $StateDir 'connect-proxies.pid')
```

При проблеме прямого доступа к Nile соберите и запустите узкий TCP relay:

```bash
go build -o "$STATE_DIR/tools/nile-tcp-relay" ./cmd/nile-tcp-relay
"$STATE_DIR/tools/nile-tcp-relay" -listen 0.0.0.0:19443 \
  -allowed-cidrs 172.30.250.0/24
```

PowerShell:

```powershell
$NileRelay = Join-Path $ToolDir 'nile-tcp-relay.exe'
go build -o $NileRelay ./cmd/nile-tcp-relay
$NileRelayProcess = Start-Process -WindowStyle Hidden -PassThru `
  -FilePath $NileRelay `
  -ArgumentList @('-listen','0.0.0.0:19443','-allowed-cidrs','172.30.250.0/24') `
  -RedirectStandardOutput (Join-Path $StateDir 'nile-relay.out.log') `
  -RedirectStandardError (Join-Path $StateDir 'nile-relay.err.log')
$NileRelayProcess.Id |
  Set-Content -Encoding ascii (Join-Path $StateDir 'nile-relay.pid')
```

Затем в `${PUBLIC_TEST_STATE_DIR}/crypto/chains.json` замените URL primary
endpoint нужных ролей на `https://nile.trongrid.io:19443`, сохраняя исходный
TLS hostname. Оставьте `disableKeepAlives: true`: Nile может зависать при
нескольких запросах по одному TLS-соединению, а per-endpoint настройка
исключает этот сценарий без изменения транспорта других сетей.

и подключите relay override.

PowerShell:

```powershell
$Compose += @(
  '-f',
  'single-tenant-deployment\docker-compose.public-test-nile-relay.yml'
)
```

bash/zsh:

```bash
COMPOSE_FILES+=(
  -f single-tenant-deployment/docker-compose.public-test-nile-relay.yml
)
```

Relay override перенаправляет только container DNS имени
`nile.trongrid.io` на host gateway; relay не завершает TLS и допускает только
фиксированный upstream `nile.trongrid.io:443`. Не добавляйте override при
direct mode. Helper ports должны быть закрыты host firewall от интернета.

## 7. Router и firewall

До ACME:

1. Удалите старые forwarding rules для `3000/8080`.
2. Направьте public TCP `80/443` на IP текущего host `80/443`.
3. Запретите internet ingress на `3000`, `8080`, `5432`, `8100`, `8200`,
   `18080`, `18443` и helper ports `19443–19445`.
4. Разрешите host firewall inbound только `80/443`.
5. Зафиксируйте реальный исходящий IP администратора, а не public IP сервера.

Начните с `ADMIN_SECURITY_MODE=warn`. Запишите точный admin IP `/32` в
`ADMIN_ALLOWED_CIDRS`, проверьте login через proxy и затем переключите режим на
`enforce`. `0.0.0.0/0` запрещён.

## 8. PostgreSQL, tenant и seed

Соберите операторские images и запустите PostgreSQL:

```text
Invoke-PublicCompose --profile init --profile operator build init seed public-test-bootstrap crypto-preflight
Invoke-PublicCompose up -d postgres
Invoke-PublicCompose --profile init run --rm init init-meta
Invoke-PublicCompose --profile init run --rm init create --slug nile-e2e --name "Nile E2E"
```

В bash/zsh замените `Invoke-PublicCompose` на `dc`.

Команда `create` печатает:

```text
Tenant ID: <uuid>
Database: <database>
```

Сохраните их в shell-переменные и finalize state:

```text
TENANT_ID=<uuid>
TENANT_DB=<database>

Invoke-PublicCompose --profile operator run --rm --no-deps public-test-bootstrap finalize --state-dir /state --tenant-id <uuid> --tenant-db <database>

Invoke-PublicCompose --profile operator run --rm --no-deps runtime-secret-permissions

Invoke-PublicCompose --profile init run --rm seed
```

`finalize` обновляет внешний env-файл и создаёт tenant DB URL и signed approval
policy bundle. `runtime-secret-permissions` проверяет точный allowlist runtime-
файлов и выставляет им владельца distroless `nonroot` (`65532:65532`) и режим
`0400`. Это обязательный кроссплатформенный шаг для file-backed Compose
secrets: приложение и воркер не запускаются от `root`, а Docker Desktop не
применяет `uid`/`gid` к таким bind mounts. Если после этого вручную менялись
`tron_api_key`, повторите команду перед
запуском сервисов. Содержимое секретов команда не читает и не выводит.

Seed выполняется с `SEED_CRYPTO_HISTORY=false`: synthetic
финансовая история в доказательный E2E не попадает.

Проверьте ожидаемую schema version:

```text
Invoke-PublicCompose exec -T postgres psql -U metapus -d <database> -c "SELECT max(version_id) FROM goose_db_version WHERE is_applied = TRUE"
```

Ожидаемая версия должна совпадать с `internal/core/version/version.go`: для текущего release — `76`.
В `sys_crypto_external_resource_policies` после чистой миграции отсутствует
столбец `providers`.

## 9. Vault init и tenant bootstrap

Запустите TLS Vault с persistent Raft volume:

```text
Invoke-PublicCompose --profile vault-test up -d --build vault-test
```

Инициализируйте его один раз:

```text
Invoke-PublicCompose --profile operator run --rm --no-deps public-test-bootstrap vault-init --state-dir /state --vault-addr https://vault-test:8200
```

Test-only root token и unseal key сохраняются в `STATE_DIR`, не выводятся
командой и не помещаются в Compose environment.

Получите checksum обоих plugins:

```text
Invoke-PublicCompose --profile vault-test exec -T vault-test sha256sum /vault/plugins/vault-plugin-secrets-tron
Invoke-PublicCompose --profile vault-test exec -T vault-test sha256sum /vault/plugins/vault-plugin-secrets-evm
```

Возьмите только первый hex field каждой команды и выполните bootstrap обеих сетей:

```text
Invoke-PublicCompose --profile operator run --rm --no-deps public-test-bootstrap vault-bootstrap --state-dir /state --tenant-id <uuid> --pool-account <POOL_ACCOUNT из public-test.env> --family both --plugin-sha256 <TRON sha256> --evm-plugin-sha256 <EVM sha256> --proxy-cidr 172.30.251.10/32 --vault-addr https://vault-test:8200
```

Команда создаёт:

- non-exportable, deletion-disabled per-tenant Transit Ed25519 key;
- TRON и EVM tenant configs и authorization keyrings;
- по одному derived liquidity hot и network-fee wallet и по два pool wallet
  для каждой сети; адреса сохранены отдельно в `wallets-tron.json` и `wallets-evm.json`;
- отдельные sweep allowlists с соответствующим derived hot address;
- worker policy и AppRole;
- wrapped one-use SecretID с TTL 120 секунд.

Сразу после `vault-bootstrap`, не дожидаясь истечения TTL:

```text
Invoke-PublicCompose --profile operator run --rm vault-bootstrap-permissions
Invoke-PublicCompose --profile vault-test up -d vault-proxy
Invoke-PublicCompose --profile vault-test exec -T vault-proxy test ! -e /run/metapus/vault-bootstrap/wrapped-secret-id
```

Proxy имеет `restart: "no"`. После потери его процесса всегда выпускайте новый
wrapped SecretID; не пытайтесь переиспользовать удалённый файл.

## 10. Связать Vault wallets с tenant DB

После успешного старта Proxy:

```text
Invoke-PublicCompose --profile operator run --rm --no-deps public-test-bootstrap configure-db --state-dir /state

Invoke-PublicCompose --profile operator run --rm --no-deps crypto-preflight
```

Повторите preflight для Sepolia: в bash/zsh выполните
`CRYPTO_NETWORK_CODE=ETHEREUM-SEPOLIA dc --profile operator run --rm --no-deps crypto-preflight`;
в PowerShell временно установите `$env:CRYPTO_NETWORK_CODE = 'ETHEREUM-SEPOLIA'`,
вызовите `Invoke-PublicCompose --profile operator run --rm --no-deps crypto-preflight`
и затем удалите переменную через `Remove-Item Env:CRYPTO_NETWORK_CODE`.

При обновлении worker, которому нужен асинхронный HD derive, обновите только
tenant-scoped ACL без повторного Vault bootstrap:

```text
Invoke-PublicCompose --profile operator run --rm --no-deps public-test-bootstrap vault-worker-policy --state-dir /state --tenant-id <uuid> --vault-addr https://vault-test:8200
```

Ожидается:

- для каждой сети один `liquidity_hot`, один `network_fee` и два `merchant_pool`
  wallet с совпадающими address, derivation path, `vault_key_id` и tenant;
- pool generation без неожиданной on-chain истории;
- `ReadErrors=0`;
- отсутствуют открытые reconciliation incidents.

Если preflight обнаружил состояние прежней generation, не снимайте hold.
Удалите disposable окружение или повторите bootstrap с новой mnemonic и новым
`PoolAccount`.

Положительный Vault smoke через Proxy должен читать только собственные Transit
metadata/readiness. Отрицательный smoke обязан отклонять:

- TRON authorization config;
- sweep policy write/read;
- wallet derivation;
- KV и `sys/config`;
- Transit/tx-sign другого tenant.

Полный ACL контракт описан в
[vault-tron-production.md](vault-tron-production.md).

## 11. Запуск приложения и HTTPS

Запустите frontend/API; worker запускайте только после Vault readiness. Публичный
Compose автоматически поднимает внутренний `metrics-sink` для обязательного
OTLP-экспорта worker. Его порт `4317` не публикуется на host:

```text
Invoke-PublicCompose up -d metapus-app frontend
Invoke-PublicCompose --profile vault-test up -d metapus-worker
```

Убедитесь, что `public-edge-proxy` и router forwarding уже работают. Затем:

### Windows

```powershell
.\single-tenant-deployment\public-test\bootstrap-tls.ps1 `
  -EnvFile $EnvFile `
  -PublicIP $PublicIP
```

### macOS/Linux

```bash
sh single-tenant-deployment/public-test/bootstrap-tls.sh \
  "$ENV_FILE" \
  "$PUBLIC_IP"
```

Bootstrap:

1. проверяет Certbot `5.4+`;
2. выполняет staging issuance;
3. получает production `shortlived` IP certificate;
4. переключает Nginx с ACME-only на HTTPS;
5. выполняет renewal dry-run;
6. запускает 12-часовой renewal loop и reload marker.

Проверка:

```bash
curl --fail --show-error "https://<PUBLIC_IP>/health/live"
curl --head "http://<PUBLIC_IP>/"
```

Дополнительно с внешней сети проверьте trusted chain/SAN и закрытые
`3000/8080/5432/8100/8200`.

Только после успешного trusted HTTPS разрешена публичная авторизация.

## 12. MFA и test identity

Test account:

```text
admin@metapus.io
Admin123!
```

Это test fixture, а не production credential.

Для enrollment, login, step-up, API-key creation, whitelist и withdrawal:

1. открыть форму через HTTPS;
2. заполнить всё, кроме TOTP;
3. пользователь вводит TOTP локально;
4. TOTP и secret не передаются в чат или отчёт.

После login выполните regression:

- две вкладки одного origin;
- одновременный reload;
- обе остаются авторизованными;
- session не получает `refresh_reuse`;
- остаётся ровно один активный refresh-token.

## 13. Финансирование и учёт TRX и ETH

Адреса берутся из `STATE_DIR/wallets-tron.json` и `STATE_DIR/wallets-evm.json`.
Приватных ключей там нет. `wallets.json` хранит последний обработанный набор и
не используется как источник для двухсетевого стенда.

Перед переводом всегда укажите:

- сеть `TRON Nile`;
- точный address;
- token/contract;
- сумму;
- назначение funding.

До включения обработки сети профинансируйте и отдельно учтите оба системных
адреса:

- `HOT-TRON-001` (`liquidity_hot`) — источник активаций и ликвидности;
- `FEE-TRON-001` (`network_fee`) — источник JIT native funding для pool
  wallet.

Внешний native TRX funding платформенного wallet после подтверждений должен
быть записан в wallet ledger отдельной командой для соответствующего кода.
Запишите его в течение 30 минут после финальности: позже worker учтёт перевод
как невыясненное поступление, и команда откажет. Такое поступление относят к
деньгам платформы в журнале «Невыясненные» действием «Пополнение платформы»
(два оператора, см. [crypto-custody-writers.md](../reference/crypto-custody-writers.md#пополнение-платформы-из-невыясненного)).

```text
Invoke-PublicCompose --profile operator run --rm --no-deps --entrypoint ./crypto-record-wallet-funding crypto-preflight -wallet-code <managed-wallet-code> -token-code TRX-NILE -tx-hash <confirmed-native-trx-hash>
```

Команда сама проверяет Nile transaction receipt, token contract, destination,
minor-unit amount, confirmations и idempotency. Для внешнего TRC-20 funding
укажите соответствующий token code, например `-token-code USDT-TRC20`.
Начиная со схемы 79, новые funding receipts обязательно связаны с indexed proof
и общим custody recognition. Managed sender/внутренний перевод требует своего
settlement workflow; merchant-owned wallet пополняется через
[AML operating receipt](crypto-aml-resources.md). Разные доказанные effects
одной tx имеют разные receipts; повтор того же effect не увеличивает остаток.
Если остаток ранее увеличен ручной custody correction без сторно, команда
откажет до ручной сверки. При расхождении balance projection и movements
команда тоже требует сверки, сохраняя прежний остаток.
Включить wallet-pool policy может только подтверждённый native TRX receipt
именно `liquidity_hot`; funding одного `network_fee` недостаточно. TRC-20
receipt учитывает физический token balance, но не заменяет native activation
budget.

После accepted broadcast account activation создаёт `CryptoNetworkOperation`.
После finality документ автоматически проводит расход TRX hot wallet и
`chain_network_fee` platform cost с `purpose=activation`. Не добавляйте баланс
или cost прямым SQL.

После записи обоих funding повторно выполните `configure-db`, затем
`crypto-preflight`: wallet-pool policy должен включиться, а ledger TRX каждого
wallet — совпасть с его chain balance. В admin policy выберите
`FEE-TRON-001` как fee wallet; нельзя назначать на эту роль `HOT-TRON-001`.

Для Sepolia профинансируйте test ETH на `HOT-EVM-001` и `FEE-EVM-001`,
затем учитывайте каждый подтверждённый перевод через
`crypto-record-wallet-funding` с `-token-code ETH-SEPOLIA`. Только после
funding hot wallet повторите `configure-db`: этот gate открывает выдачу новых
pool wallet. Для проверки платежей и свипа понадобится PYUSD Sepolia на
внешнем тестовом кошельке; для вывода нужен также остаток PYUSD на hot wallet.
Включайте каждую network policy через Admin UI после свежего preflight и
проверки native balance. Bootstrap намеренно оставляет политики выключенными,
пока сеть не готова к on-chain операциям.

EVM funding схемы 79 требует два независимых RPC trust groups и сохраняет тот же
verification proof, что processing. Успех одиночного receipt read не разрешает
проводку. Funding receipt, ledger, recognition, audit и outbox атомарны; при
ошибке проверки или обязательной записи не появляется частичный приход.

Перед включением network policy согласуйте лимиты в минимальных единицах TRX.
Не копируйте фиксированное значение из старого прогона: комиссия Nile зависит
от текущего состояния сети и ресурсов. `perTransactionMaxFeeMinor` должен
покрывать актуальную оценку полной операции с операционным запасом; более
дорогая оценка штатно переводит операцию в `held`, после чего лимит меняется
только через Admin UI/API со step-up MFA. Дополнительно обязательны инварианты:

```text
jitFundingAmountMinor >= perTransactionMaxFeeMinor + minSourceWalletResidualMinor
dailyJitFundingBudgetMinor > jitFundingAmountMinor
```

Последний лимит оставляет положительную ёмкость на комиссию самого пополнения.
Перед каждым JIT worker дополнительно и атомарно проверяет, что остаток дневного
budget покрывает конкретные `principal + quote.MaxFee`; общий transaction cap
не резервируется автоматически, если актуальная quote ниже. UI, доменная
модель и SQL constraint отклоняют заведомо невыполнимую конфигурацию.

## 14. Полный E2E и приёмка

Выполните
[crypto-processing-test-scenarios.md](../crypto/crypto-processing-test-scenarios.md).
Минимальный обязательный цикл выполните отдельно для обеих сетей:

```text
invoice ~12 USDT
  -> exact Nile transfer
  -> confirmations
  -> payment + ledger + webhook outbox
  -> sweep pool -> allowlisted hot
  -> withdrawal ~5 USDT -> external wallet

invoice ~12 PYUSD
  -> exact Sepolia transfer
  -> confirmations
  -> payment + ledger + webhook outbox
  -> sweep pool -> allowlisted hot
  -> withdrawal ~5 PYUSD -> external wallet
```

Для каждой стадии нужны одновременные доказательства:

- UI/API status;
- PostgreSQL document, settlement и balanced movements;
- Nile/Sepolia tx result и confirmations;
- повтор/restart не создаёт второй payment/sweep/withdrawal.

В конце:

```text
Invoke-PublicCompose --profile operator run --rm --no-deps crypto-preflight
```

Требуется `ReadErrors=0` и `0` открытых incidents. Test Merchant API key,
показанный оператору или использованный в отчёте, отзывается после теста.

## 15. Restart

После полного stop сначала запустите PostgreSQL и sealed Vault:

```text
Invoke-PublicCompose --profile vault-test up -d postgres vault-test
```

Ручная операторская команда unseal, не раскрывающая key в shell:

```text
Invoke-PublicCompose --profile operator run --rm --no-deps public-test-bootstrap vault-unseal --state-dir /state --vault-addr https://vault-test:8200
```

После потери процесса Proxy выпустите новый wrapped SecretID и используйте его
в течение 120 секунд:

```text
Invoke-PublicCompose --profile operator run --rm --no-deps public-test-bootstrap vault-wrap-secret-id --state-dir /state --vault-addr https://vault-test:8200
Invoke-PublicCompose --profile operator run --rm vault-bootstrap-permissions
Invoke-PublicCompose --profile vault-test up -d vault-proxy
```

Только после readiness запускайте application services:

```text
Invoke-PublicCompose --profile vault-test --profile tls up -d metapus-app frontend metapus-worker reverse-proxy certbot-renew
```

Повторите health, Vault ACL и reconciliation smoke.

## 16. Backup и restore drill

Для согласованной operator backup точки:

1. остановите `metapus-app` и `metapus-worker`;
2. сохраните meta и tenant PostgreSQL data;
3. выполните Vault Raft snapshot;
4. скопируйте файлы в зашифрованное хранилище вне репозитория;
5. проверьте checksum и восстановление в отдельном disposable окружении;
6. запустите сервисы и выполните reconciliation.

Пример PostgreSQL dump без host database port:

```text
Invoke-PublicCompose stop metapus-app metapus-worker
Invoke-PublicCompose exec -T postgres pg_dumpall -U metapus -f /tmp/metapus-public-test.sql
docker cp metapus-public-test-postgres-1:/tmp/metapus-public-test.sql <BACKUP_DIR>/
```

Vault snapshot выполняется оператором с token, введённым интерактивно внутри
Vault container:

```sh
export VAULT_ADDR=https://127.0.0.1:8200
export VAULT_CACERT=/vault/tls/ca.crt
read -r -s VAULT_TOKEN
export VAULT_TOKEN
vault operator raft snapshot save /tmp/metapus-vault.snap
unset VAULT_TOKEN
```

Скопируйте snapshot через `docker cp`, затем проверьте восстановление на новом
Raft volume. Dump/snapshot содержат test-only состояние, но не должны попадать
в Git.

## 17. TLS renewal

Автоматический container выполняет проверку дважды в сутки:

```text
Invoke-PublicCompose logs --tail=200 certbot-renew reverse-proxy
Invoke-PublicCompose --profile tls-tools run --rm certbot certificates --cert-name <PUBLIC_IP>
```

Диагностика renewal:

```text
Invoke-PublicCompose --profile tls-tools run --rm certbot renew --dry-run --no-random-sleep-on-renew --run-deploy-hooks --cert-name <PUBLIC_IP> --preferred-profile shortlived --webroot --webroot-path /var/www/certbot --deploy-hook "touch /etc/letsencrypt/.metapus-reload"
```

Для short-lived IP certificate порт `80` должен оставаться доступен для
HTTP-01. Контролируйте expiry и alert до того, как оставшееся время станет
меньше двух renewal cycles.

## 18. Обновление кода

1. Зафиксируйте отчёт и backup текущего стенда.
2. Обновите выбранную ветку и примените нужные изменения.
3. Запишите новый commit и пройдите quality gates.
4. Обновите `METAPUS_VERSION`.
5. Соберите все images из одного checkout.
6. Остановите app/worker.
7. Выполните `init init-meta` и `init migrate --all`.
8. Recreate app/worker/frontend.
9. Выполните health, MFA, Vault readiness и reconciliation smoke.

Не запускайте старый binary на новой schema без явно подтверждённой
совместимости.

## 19. Остановка и teardown

Остановка без удаления данных:

```text
Invoke-PublicCompose --profile vault-test --profile tls stop
```

Перед окончательным удалением:

- вывести оставшиеся Nile test funds;
- отозвать test API keys;
- остановить host helpers;
- удалить router forwarding `80/443`;
- сохранить разрешённый test report без credentials;
- подтвердить, что backup больше не нужен.

На Windows остановите edge по сохранённому PID:

```powershell
$EdgePid = Get-Content (Join-Path $StateDir 'public-edge.pid')
Stop-Process -Id $EdgePid

Get-ChildItem $StateDir -Filter '*.pid' |
  Where-Object Name -ne 'public-edge.pid' |
  ForEach-Object { Stop-Process -Id (Get-Content $_.FullName) }
```

На Linux/macOS остановите соответствующий service/foreground process. Так же
остановите только те egress helper processes, которые были запущены для этого
стенда.

Только после явного решения удалить disposable данные:

```text
Invoke-PublicCompose --profile vault-test --profile tls down -v
```

Затем удалите внешний `STATE_DIR`. `down -v` и удаление state необратимы.

## 20. Диагностика

| Симптом | Проверка |
|---|---|
| Public IP не открывается | host `80/443`, public-edge process, router/NAT, Windows/macOS firewall |
| ACME challenge timeout | public port 80, ACME-only Nginx, public-edge upstream `127.0.0.1:18080` |
| npm/certbot не имеют egress | direct mode env или helpers `19444/19445` и Docker CIDR |
| Worker не видит Nile | `CRYPTO_CHAIN_CONFIG_FILE`, readiness всех RPC-ролей, optional relay `19443`, TLS hostname сохранён |
| Proxy не стартует | остановить Proxy, новый wrapped SecretID, TTL 120s, bootstrap permissions |
| Vault sealed | выполнить operator `vault-unseal`, затем readiness |
| Admin CIDR denied | проверить настоящий client IP через proxy; временно `warn`, не `off` |
| Pool wallet held | preflight/reconciliation; новая generation вместо ручного снятия hold |
| Ledger TRX не совпадает | записать подтверждённый native funding штатным CLI |
| Повторная операция создаёт новый tx | остановить outbound и сверить stored signed tx, intent и reconciliation |


## Проверка RPC ingestion

Настройки operation routing, параллельного native prefetch и отдельный read-only
`crypto-preflight -mode ingestion` описаны в [RPC ingestion runbook](rpc-ingestion.md).
Balance preflight выполняется при остановленном worker; ingestion preflight — при живом.
