# Восстановление локального тестового стенда после выключения компьютера

> **TL;DR:** Для существующего disposable-стенда: unseal прежнего Vault → новый wrapped SecretID → запуск прежнего Vault Proxy → проверка API и входа.
> **Тип:** How-To
> **Аудитория:** Admin, Developer, QA
> **Связанные:** [Vault и AppRole](vault-tron-production.md), [публичный тестовый стенд](public-nile-deployment.md)

## 1. Границы

Команды ниже — для PowerShell 7 и существующего локального стенда
`metapus-public-test`, созданного 31 августа 2026 года. Выполняйте блоки по
порядку в одном окне PowerShell. Для другого стенда сначала проверьте имена
контейнеров, пути mount и tenant ID; не используйте команды вслепую.

Это восстановление доступности, не обновление приложения. Не выполняйте
`docker compose down -v`, `vault operator init`, миграции, пересборку образов
или замену tenant seed. Существующие БД, Raft volume и ключи сохраняются.
Работающие API/worker после восстановления Vault могут возобновить обработку
ранее разрешённых операций; этот runbook не меняет kill switches и holds.

В этом одноразовом тестовом стенде операторский токен и один unseal share уже
сохранены вне репозитория. Это не схема хранения секретов для production.
Не копируйте этот файл в репозиторий, `.env`, журналы или application containers.
Не запускайте шаги с секретами под PowerShell transcript/debug trace.

## 2. Диагностика

Сначала запустите Docker Desktop. Затем:

```powershell
# C:/Users/alex/go/src/metapus/docs/operations/local-vault-recovery.md
$ErrorActionPreference = 'Stop'
$standDirectory = 'C:\Users\alex\.metapus\public-test-20260831-dual-cycle'
$vaultContainer = 'metapus-public-test-vault-test-1'
$proxyContainer = 'metapus-public-test-vault-proxy-1'
$apiContainer = 'metapus-public-test-metapus-app-1'
$tenantId = '01a056fa-89e5-7e8b-8cea-06f3cd83b5a3'

docker ps -a --filter 'name=metapus-public-test-' --format 'table {{.Names}}\t{{.Status}}'
docker inspect --format '{{.Name}} {{range .Mounts}}{{.Source}} -> {{.Destination}}; {{end}}' $proxyContainer
docker exec $vaultContainer vault status -format=json
```

Если **существующий** `vault-test` остановлен, выполните `docker start $vaultContainer`
и повторите `vault status`. Если контейнер отсутствует, остановитесь: это уже
не восстановление прежних процессов.

- `initialized: true, sealed: true` — нужен unseal. Exit code `2` у `vault status` означает sealed, а не отсутствие Vault.
- `initialized: false` — остановитесь и проверьте подключение прежнего Raft volume; не инициализируйте новое хранилище.
- Docker `healthy` недостаточно: текущий healthcheck Vault допускает exit code `2`.
- Если Vault уже unsealed, прокси работает и API ready отвечает `200`, восстановление не требуется. Не останавливайте исправный прокси.

## 3. Unseal существующего Vault

Этот блок поддерживает именно сохранённый формат данного стенда:
`unseal_key` и `root_token`, один share при threshold `1`.

```powershell
# C:/Users/alex/go/src/metapus/docs/operations/local-vault-recovery.md
$recovery = Get-Content -Raw -LiteralPath (Join-Path $standDirectory 'secrets\vault_init.json') | ConvertFrom-Json
if ([string]::IsNullOrWhiteSpace($recovery.unseal_key) -or
    [string]::IsNullOrWhiteSpace($recovery.root_token)) {
    throw 'В файле отсутствуют операторские данные этого тестового Vault'
}
$statusJson = docker exec $vaultContainer vault status -format=json
if ($LASTEXITCODE -notin @(0, 2)) { throw 'Не удалось прочитать состояние Vault' }
$vaultStatus = $statusJson | ConvertFrom-Json
if (-not $vaultStatus.initialized) { throw 'Проверьте прежний Raft volume; init запрещён' }
if ($vaultStatus.sealed) {
    if ($vaultStatus.t -ne 1) { throw 'Нужна отдельная процедура quorum unseal' }
    $unsealBody = @{ key = $recovery.unseal_key } | ConvertTo-Json -Compress
    $unsealResult = $unsealBody | docker exec -i $vaultContainer vault write -format=json sys/unseal -
    if ($LASTEXITCODE -ne 0) { throw 'Unseal завершился ошибкой' }
    $unsealBody = $null
}
docker exec $vaultContainer vault status -format=json
if ($LASTEXITCODE -ne 0) { throw 'Vault всё ещё sealed или недоступен' }
```

Ожидаемый результат — `sealed: false`. Не меняйте TLS-настройки и не добавляйте
`tls-skip-verify`: контейнер уже использует CA данного стенда.

## 4. Новый одноразовый bootstrap прокси

Выполняйте этот шаг только для остановленного либо подтверждённо неработающего
прокси. Перед заменой bootstrap остановите его: параллельные попытки входа со
старым SecretID могут заблокировать AppRole.

```powershell
# C:/Users/alex/go/src/metapus/docs/operations/local-vault-recovery.md
docker stop $proxyContainer
if ($LASTEXITCODE -ne 0) { throw 'Не удалось остановить прежний прокси' }

function Invoke-RecoveryVault {
    param([string[]]$VaultArgs)
    # Token поступает через stdin, а не через аргументы docker exec.
    # tr удаляет Windows CR/LF, иначе Vault отвергает token как non-printable.
    $result = $recovery.root_token | docker exec -i $vaultContainer sh -c 'VAULT_TOKEN=$(tr -d "[:space:]"); export VAULT_TOKEN; exec vault "$@"' -- @VaultArgs
    if ($LASTEXITCODE -ne 0) { throw 'Операторская команда Vault завершилась ошибкой' }
    return $result
}

$lockouts = Invoke-RecoveryVault -VaultArgs @('read', '-format=json', 'sys/locked-users') | ConvertFrom-Json
if ($lockouts.data.total -ne 0) {
    throw 'Найдена блокировка Vault auth: проверьте причину вручную; автоматический unlock запрещён'
}

$role = Invoke-RecoveryVault -VaultArgs @('read', '-format=json', 'auth/approle/role/metapus-worker') | ConvertFrom-Json
if ($role.data.secret_id_num_uses -ne 1 -or $role.data.token_period -ne 600) {
    throw 'AppRole отличается от проверенной конфигурации стенда'
}
Invoke-RecoveryVault -VaultArgs @('policy', 'read', 'metapus-worker')
```

В policy должно быть `path "auth/token/renew-self"` с `capabilities = ["update"]`.
Если права отсутствуют, остановитесь и разберите отличие policy; не расширяйте
разрешения наугад. В данном стенде SecretID действует 300 секунд и допускает
один вход, service token имеет период 600 секунд.

Следующий блок выполните целиком: wrapping token действует только 120 секунд.

```powershell
# C:/Users/alex/go/src/metapus/docs/operations/local-vault-recovery.md
$wrappedResponse = Invoke-RecoveryVault -VaultArgs @(
    'write', '-wrap-ttl=120s', '-format=json', '-f',
    'auth/approle/role/metapus-worker/secret-id'
) | ConvertFrom-Json
if ($wrappedResponse.wrap_info.creation_path -ne 'auth/approle/role/metapus-worker/secret-id' -or
    [string]::IsNullOrWhiteSpace($wrappedResponse.wrap_info.token)) {
    throw 'Неожиданный результат выпуска wrapped SecretID'
}
$bootstrapPath = Join-Path $standDirectory 'vault\bootstrap\wrapped-secret-id'
[IO.File]::WriteAllText($bootstrapPath, $wrappedResponse.wrap_info.token, [Text.UTF8Encoding]::new($false))
$wrappedResponse = $null
docker start $proxyContainer
if ($LASTEXITCODE -ne 0) { throw 'Прокси не запустился; bootstrap скоро истечёт' }
$recovery = $null
```

Проверьте спустя несколько секунд:

```powershell
# C:/Users/alex/go/src/metapus/docs/operations/local-vault-recovery.md
docker logs --since 2m $proxyContainer 2>&1 | Select-String 'authentication successful|renewed auth token|error'
Test-Path -LiteralPath $bootstrapPath
```

Должно появиться `authentication successful`; `Test-Path` должен вернуть
`False`, поскольку прокси прочитал и удалил bootstrap. Само исчезновение файла
ещё не доказывает успешный вход — нужны лог и запрос защищённого ресурса.
При ошибке остановите прокси и разберите её; не повторяйте бесконечно вход
старым или истёкшим SecretID.

## 5. Проверка API и входа

API в crash loop имеет `restart: unless-stopped` и после восстановления прокси
обычно запускается сам. Если он явно остановлен, выполните `docker start $apiContainer`.
Не пересоздавайте его через `compose up --build`: восстановление не должно
разворачивать новый код поверх старой схемы тестовой БД.

```powershell
# C:/Users/alex/go/src/metapus/docs/operations/local-vault-recovery.md
docker ps --filter 'name=metapus-public-test-' --format 'table {{.Names}}\t{{.Status}}'
Invoke-RestMethod 'http://127.0.0.1:8080/health/live'
Invoke-RestMethod 'http://127.0.0.1:8080/health/ready'
docker inspect --format '{{.RestartCount}}' $apiContainer

docker exec -e VAULT_ADDR=http://127.0.0.1:8100 -e VAULT_TOKEN=deliberately-invalid-recovery-probe $proxyContainer vault read -format=json "tron/tenants/$tenantId/config/readiness"
if ($LASTEXITCODE -ne 0) { throw 'TRON signing readiness недоступен через прокси' }
docker exec -e VAULT_ADDR=http://127.0.0.1:8100 -e VAULT_TOKEN=deliberately-invalid-recovery-probe $proxyContainer vault read -format=json "evm/tenants/$tenantId/config/readiness"
if ($LASTEXITCODE -ne 0) { throw 'EVM signing readiness недоступен через прокси' }
```

Оба health endpoint должны вернуть `status: ok`; счётчик перезапусков не должен
расти. Readiness должен подтвердить ключи и seed для нужных сетей. Подложенный
нерабочий token в этих двух проверках намеренный: Proxy должен заменить его
своим AppRole token (`use_auto_auth_token = "force"`).

Откройте <http://127.0.0.1:3000/login>, введите обычный пароль и свежий TOTP.
Для администратора ответ `202 mfa_required` на `/api/v1/auth/login` означает,
что пароль принят и сервер штатно ожидает MFA, а не что полный вход уже завершён.

После периода token (10 минут) повторите защищённый readiness-запрос и проверьте
новые `renewed auth token` в логах. Один renewal при старте не доказывает работу
после истечения первого периода. Если компьютер снова уснул на длительное время,
token может истечь; проверьте авторизацию и при необходимости повторите bootstrap.

## 6. Контроль выполненного восстановления

3 сентября 2026 года на указанном стенде:

- Подтверждены `sealed: true` у Vault и остановленный Proxy; API падал при загрузке Transit keys.
- Выполнен unseal прежнего Raft-хранилища; auth lockouts отсутствовали, право renew-self уже было настроено.
- Выпущен новый wrapped SecretID. В `14:21:06 UTC` Proxy подтвердил аутентификацию и первоначальный renewal, bootstrap-файл удалён.
- API восстановился без ручного restart; `/health/live`, `/health/ready` и `/login` ответили `200`.
- Проверены TRON/EVM signing readiness через forced-token Proxy.
- В `14:22:51 UTC` запрос через frontend с паролем администратора вернул `202 mfa_required`. TOTP в рамках восстановления не вводился.
- Образы, миграции, БД, ключи, MFA policy, restart policy и финансовые документы вручную не изменялись.

## 7. Чеклист

- [ ] Vault initialized и unsealed; использован прежний Raft volume.
- [ ] Proxy успешно авторизован; bootstrap удалён; защищённый ресурс читается.
- [ ] API ready; счётчик рестартов не растёт; вход доходит до MFA.
- [ ] После 10 минут подтверждены повторный renewal и доступ к защищённому ресурсу.

## Связанные документы

- [Vault и AppRole](vault-tron-production.md) — почему `restart: "no"` намеренный и как устроен одноразовый bootstrap.
- [Production deployment](production-deployment.md) — отдельные требования к настоящему production; локальный файл unseal/root не является production recovery.
- [HashiCorp: AppRole auto-auth](https://developer.hashicorp.com/vault/docs/agent-and-proxy/autoauth/methods/approle) — чтение, удаление и response wrapping SecretID.
- [HashiCorp: Seal/Unseal](https://developer.hashicorp.com/vault/docs/concepts/seal) — поведение Vault после перезапуска.
