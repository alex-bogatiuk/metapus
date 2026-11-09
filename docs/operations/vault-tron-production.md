# Production-контур Vault и подписи TRON

> **TL;DR:** Worker подписывает authorization, а API — пять approval-артефактов через tenant/role-scoped Vault Transit keys; оба процесса обращаются к Vault только через tokenless Proxy с одноразовым AppRole bootstrap.
>
> **Тип:** How-To
>
> **Аудитория:** DevOps, Security, Developer, QA
>
> **Связанные:** [полный production deployment](production-deployment.md),
> [Vault-аутентификация в Kubernetes](vault-kubernetes-migration.md),
> [тестирование криптопроцессинга](../crypto/crypto-processing-test-scenarios.md)

---

Этот runbook описывает реализованную в Metapus границу подписи:

```text
metapus-worker -> выделенный Vault Proxy -> Transit + TRON plugin
```

Это компонентный runbook signing boundary, а не полный production deployment.
Требования к release, PostgreSQL, edge, rollout/rollback, backup/restore,
monitoring и Mainnet gate находятся в
[production-deployment.md](production-deployment.md).

Приложение не получает Vault token. Vault Proxy аутентифицируется через AppRole
с одноразовым response-wrapped SecretID, продлевает периодический service token
и принудительно заменяет любой токен, переданный клиентом.

> Профиль Compose `vault-test` предназначен только для Nile и одноразовых
> тестовых кошельков. В нём запрещено хранить средства с реальной стоимостью.
> Mainnet требует отдельного Vault-сервиса или VM, независимого хранения unseal
> material и проверенных резервных копий.

> 🔗 **Fintech Insight: граница подписи**
>
> `SigningAuthorization` является неизменяемым одноразовым intent. Worker не
> владеет экспортируемым authorization key, а TRON plugin проверяет business
> intent, approval policy и destination до ECDSA-подписи. Компрометация worker
> не должна позволять изменить Vault config, расширить sweep allowlist или
> превратить одно одобрение в несколько blockchain-транзакций.

## Runtime-контракт

Worker в production использует только:

```env
VAULT_ADDR=http://vault-proxy:8100
CRYPTO_AUTH_SIGNER=transit
CRYPTO_TRANSIT_MOUNT=transit
```

`VAULT_TOKEN` и `CRYPTO_SIGNING_AUTH_PRIVATE_KEY` должны отсутствовать. При
`APP_ENV=production` worker завершает запуск, если задана хотя бы одна из этих
переменных либо выбран local signer.

## Запуск тестового Vault

Команды ниже выполняются из `single-tenant-deployment`. Для
`deployments/single-tenant` скорректируйте относительные пути.

1. Сгенерируйте краткоживущий тестовый TLS-комплект:

   ```sh
   sh ../deployments/vault-test/generate-test-tls.sh ./vault-test/tls
   ```

   На Linux VM дайте непривилегированному пользователю Vault внутри
   зафиксированного образа доступ к server key. Перед сменой версии образа
   повторно проверьте UID/GID:

   ```sh
   sudo chown 100:1000 ./vault-test/tls/server.key
   sudo chmod 0400 ./vault-test/tls/server.key
   ```

2. Запустите только Vault:

   ```sh
   docker compose --profile vault-test up -d --build vault-test
   ```

3. Выполните `vault operator init`. Все unseal shares и initial root token
   сохраните вне репозитория и тестовой VM. Их запрещено помещать в `.env`,
   историю shell, вывод CI или Compose volumes.

4. Выполните ручной `vault operator unseal`. Raft volume персистентен, но в
   тестовом контуре намеренно нет упрощённого auto-unseal.

Настройте операторский Vault CLI:

```sh
export VAULT_ADDR=https://127.0.0.1:8200
export VAULT_CACERT="$PWD/vault-test/tls/ca.crt"
```

### Особенности образа и Windows

- `generate-test-tls.sh` использует временный extfile, поскольку `/dev/stdin`
  несовместим с частью окружений Git Bash/Windows.
- Образ запускается от пользователя `vault`; `/vault/data` должен принадлежать
  ему, иначе Integrated Raft не откроет storage.
- В Compose команда Vault должна оставаться `command: ["server"]`. Entry point
  сам читает `/vault/config`; повторный `-config` создаёт дублирующий listener.
- Bootstrap-файлы на Windows записывайте в UTF-8 без BOM. UTF-16 или BOM
  приводят к ошибке `configured Vault token contains non-printable characters`.

## Регистрация secrets engines и TRON plugin

Вычислите checksum plugin внутри образа, зарегистрируйте его и подключите оба
secrets engine:

```sh
PLUGIN_SHA256="$(docker compose --profile vault-test exec -T vault-test sha256sum /vault/plugins/vault-plugin-secrets-tron | awk '{print $1}')"
vault plugin register -sha256="$PLUGIN_SHA256" secret vault-plugin-secrets-tron
vault secrets enable transit
vault secrets enable -path=tron -plugin-name=vault-plugin-secrets-tron plugin
vault auth enable approle
```

Версии Vault и plugin image должны быть зафиксированы в исходном коде.
Обновление выполняется как отдельная проверенная операция; тег `latest`
запрещён.

## Подготовка tenant

Во всех примерах используется канонический UUID:

```sh
TENANT_ID=00000000-0000-0000-0000-000000000001
TRANSIT_KEY="metapus-signing-auth-$TENANT_ID"
```

Создайте неэкспортируемый Ed25519 key. Plaintext backup, deletion и
автоматическая ротация должны оставаться выключенными:

```sh
vault write "transit/keys/$TRANSIT_KEY" \
  type=ed25519 \
  exportable=false \
  allow_plaintext_backup=false \
  auto_rotate_period=0
vault write "transit/keys/$TRANSIT_KEY/config" deletion_allowed=false
```

Создайте ещё пять отдельных ключей для approval pipeline. Их private material
никогда не экспортируется в API, worker, environment или secret files:

```sh
for ROLE in intent risk-assessment policy-decision admin-approval risk-approval; do
  KEY="metapus-approval-$ROLE-$TENANT_ID"
  vault write "transit/keys/$KEY" \
    type=ed25519 exportable=false allow_plaintext_backup=false auto_rotate_period=0
  vault write "transit/keys/$KEY/config" deletion_allowed=false
  vault read "transit/keys/$KEY"
done
```

Public keys актуальных версий запишите в соответствующие поля plugin config и
в `CRYPTO_*_SIGNING_PUBLIC_KEY` worker. API получает только policy public key,
signed policy bundle и доступ Proxy к точным `transit/keys/...` и
`transit/sign/...` путям своего tenant.

Прочитайте metadata ключа, выберите максимальную числовую версию в `data.keys`
и сформируйте:

```text
kid = transit/metapus-signing-auth-<tenant-uuid>/v<version>
```

Для Ed25519 скопируйте `public_key` этой версии в keyring plugin. Plugin
принимает PEM-представление Vault, а также raw Ed25519 public key в base64 или
hex. Подготовьте вне репозитория JSON:

```json
{
  "schema_version": "tron_tenant_signing_config.v3",
  "authorization_keyring": {
    "transit/metapus-signing-auth-00000000-0000-0000-0000-000000000001/v1": "<transit public key>"
  },
  "policy_public_key": "<base64 Ed25519 public key>",
  "intent_public_key": "<base64 Ed25519 public key>",
  "risk_assessment_public_key": "<base64 Ed25519 public key>",
  "policy_decision_public_key": "<base64 Ed25519 public key>",
  "admin_approval_public_key": "<base64 Ed25519 public key>",
  "risk_approval_public_key": "<base64 Ed25519 public key>",
  "hd_seed": "<encoded 64-byte BIP-39 seed>"
}
```

Запишите конфигурацию operator token:

```sh
curl --fail --silent --show-error \
  --cacert "$VAULT_CACERT" \
  --header "X-Vault-Token: $VAULT_TOKEN" \
  --header "Content-Type: application/json" \
  --request POST \
  --data @/secure/operator/tenant-signing-config.json \
  "$VAULT_ADDR/v1/tron/tenants/$TENANT_ID/config/authorization"
```

Offline policy key, пять approval Transit keys и authorization key должны
различаться.

## Sweep destinations

Сначала выведите consolidation wallet через tenant path plugin с operator-only
token. Затем разрешите только ранее выведенные адреса:

```json
{
  "schema_version": "tron_sweep_destinations.v1",
  "version": "2026-07-21.1",
  "destinations": {
    "tron_nile": ["<derived consolidation address>"]
  }
}
```

Endpoint записи:

```text
POST /v1/tron/tenants/<tenant-id>/config/sweep-destinations
```

Plugin отклоняет внешний адрес, адрес другого tenant и адрес другой сети.
Worker policy не позволяет читать или изменять allowlist.

## Worker policy и AppRole

Отрендерите `deployments/vault-test/worker-policy.hcl.template` отдельно для
каждого явно включённого tenant, затем объедините точные пути в одну policy.
Wildcard вместо tenant UUID запрещён.

Сохраните правило `auth/token/renew-self`. При
`token_no_default_policy=true` это единственное право, позволяющее Proxy
продлевать periodic token; дополнительных прав на подпись или config оно не
даёт.

```sh
vault policy write metapus-worker /secure/operator/metapus-worker.hcl

vault write auth/approle/role/metapus-worker \
  token_policies=metapus-worker \
  token_no_default_policy=true \
  token_type=service \
  token_period=10m \
  token_num_uses=0 \
  secret_id_ttl=5m \
  secret_id_num_uses=1 \
  secret_id_bound_cidrs=172.30.251.10/32 \
  token_bound_cidrs=172.30.251.10/32
```

Стандартная Compose network закрепляет Vault-facing адрес Proxy за
`172.30.251.10`. При изменении `VAULT_CONTROL_SUBNET` или
`VAULT_PROXY_CONTROL_IP` обновите обе CIDR-привязки на фактический egress-адрес
Proxy.

Сохраните RoleID как read-only файл:

```sh
mkdir -p ./vault-test/bootstrap
vault read -field=role_id auth/approle/role/metapus-worker/role-id > ./vault-test/bootstrap/role-id
chmod 0444 ./vault-test/bootstrap/role-id
```

Непосредственно перед стартом Proxy выпустите wrapping token с TTL 120 секунд,
содержащий одноразовый SecretID:

```sh
vault write -wrap-ttl=120s -format=json -f auth/approle/role/metapus-worker/secret-id \
  | jq -r '.wrap_info.token' > ./vault-test/bootstrap/wrapped-secret-id
chmod 0400 ./vault-test/bootstrap/wrapped-secret-id
```

На Windows не используйте стандартную запись Windows PowerShell 5 в UTF-16.
Надёжный вариант записи без BOM:

```powershell
$wrapped = (vault write -wrap-ttl=120s -format=json -f auth/approle/role/metapus-worker/secret-id | ConvertFrom-Json).wrap_info.token
$path = [IO.Path]::GetFullPath('.\vault-test\bootstrap\wrapped-secret-id')
[IO.File]::WriteAllText($path, $wrapped, (New-Object Text.UTF8Encoding($false)))
```

Каталог должен быть доступен на запись пользователю Vault внутри контейнера,
чтобы Proxy удалил `wrapped-secret-id` после чтения. На Linux VM после создания
обоих файлов:

```sh
sudo chown -R 100:1000 ./vault-test/bootstrap
sudo chmod 0700 ./vault-test/bootstrap
```

Перед заменой bootstrap-файла остановите Proxy. Иначе его повторные попытки с
использованным или повреждённым SecretID могут активировать user lockout для
RoleID.

Запустите Proxy и worker один раз:

```sh
docker compose --profile vault-test up -d vault-proxy metapus-worker
```

Proxy проверяет `creation_path` wrapping token на точное совпадение с
`auth/approle/role/metapus-worker/secret-id` и удаляет bootstrap-файл. После
потери процесса для перезапуска нужен новый wrapped SecretID. Поэтому service
имеет `restart: "no"` и не входит в бесконечный unauthenticated restart loop.

Проверьте удаление файла, forced-token режим, readiness и ACL:

```sh
sh ../deployments/vault-test/verify-proxy.sh \
  ./docker-compose.yml \
  "$TENANT_ID" \
  "transit/$TRANSIT_KEY/v1" \
  tron_nile
```

`verify-proxy.sh` предназначен для test Compose profile `vault-test`. Для
внешнего production Vault выполните эквивалентные forced-token, readiness,
tenant isolation, negative ACL и renewal checks средствами выбранной platform;
не подключайте production Vault к тестовому Compose profile ради запуска
скрипта.

Повторите проверку позже чем через один `token_period` — по умолчанию через 10
минут. В логах Proxy должны регулярно появляться успешные renewal-события, а
новый SecretID не должен требоваться.

## Preflight и запуск outbound

До включения исходящих операций tenant:

1. Прочитайте `tron/tenants/<tenant>/config/readiness` через Proxy без token.
2. Убедитесь, что активный Transit `kid` есть в keyring, approval keys и HD seed
   готовы, а сеть присутствует в `sweep_policy_networks`.
3. Проверьте Nile withdrawal, treasury withdrawal, sweep, exact retry и
   crash/retry.
4. Включите outbound только для явного tenant allowlist и расширяйте его после
   наблюдения.

Сбой Vault, Proxy или Transit обрабатывается fail-closed: операция остаётся в
`held`/retry, подпись и broadcast не создаются. Readiness выполняется до заказа
платных TRON resources.

Повторяемый сценарий и актуальный результат контрольного прогона находятся в
[сценариях тестирования криптопроцессинга](../crypto/crypto-processing-test-scenarios.md#control-run-2026-07-28).

## Подтверждённые эксплуатационные инварианты

Контрольный Nile E2E подтвердил:

- production API и worker не содержат `VAULT_TOKEN` и private signing keys;
- per-tenant Transit `kid` совпадает с keyring plugin;
- Proxy заменяет подложенный client token и не даёт worker читать config или
  пути другого tenant;
- без `auth/token/renew-self` periodic token истекает с `403`, а свип остаётся
  без подписи и broadcast; после добавления правила renewal работает дольше
  нескольких периодов;
- exact retry одного approval intent возвращает ту же подписанную транзакцию;
- тот же intent с другим txID или authorization ID отклоняется;
- sweep проходит только на derived address из tenant allowlist;
- после аренды Energy свип и выплата могут завершиться без сжигания TRX, при
  этом стоимость provider order остаётся отдельным platform cost;
- MFA/step-up пользователя и approval principals — разные контроли. Для
  low-risk policy, требующей только `risk`, заявка может получить системное
  одобрение без отдельного admin approval.

## Диагностика

Для восстановления существующего локального disposable-стенда после выключения
компьютера используйте [PowerShell runbook](local-vault-recovery.md). Он не
пересоздаёт БД, не меняет ключи и не разворачивает новые образы.

| Симптом | Вероятная причина | Действие |
|---|---|---|
| Через несколько минут Proxy отвечает `403 invalid token` | В policy нет `auth/token/renew-self` | Добавить только `update` на renew-self, выпустить новый SecretID и проверить renewal дольше `token_period` |
| `invalid role or secret ID` | SecretID использован, истёк или RoleID заблокирован повторами | Остановить Proxy, проверить lockout, при необходимости выполнить operator-only unlock, затем выпустить новый wrapped SecretID |
| `non-printable characters` | Bootstrap-файл записан в UTF-16 или с BOM | Перезаписать UTF-8 без BOM |
| Vault сообщает о дублирующемся listener | Config передан и entry point, и аргументом | Оставить `command: ["server"]` |
| Raft не открывает `/vault/data` | Неверный owner/mode volume | Проверить UID/GID зафиксированного образа и права каталога |
| Readiness не видит `kid` | Transit version не добавлена в plugin keyring | Остановить outbound, обновить keyring operator token и повторить preflight |
| Sweep destination отклонён | Адрес не выведен этим tenant/сетью или отсутствует в allowlist | Вывести consolidation wallet через plugin и обновить policy operator role |

Если AppRole заблокирован, разблокировка выполняется только оператором:

```sh
vault write -f sys/locked-users/<approle-mount-accessor>/unlock/<role-id>
```

Не автоматизируйте unlock в deployment script: блокировка может указывать на
атаку или ошибочный bootstrap loop.

## Ротация authorization key

Ротация выполняется оператором при приостановленном outbound tenant:

1. Приостановите outbound.
2. Выполните `vault write -f transit/keys/<tenant-key>/rotate`.
3. Добавьте public key и `kid` новой версии в plugin keyring, сохранив старую.
4. Выполните readiness/preflight и возобновите tenant. Worker выберет
   максимальную Transit version.
5. После максимального TTL authorization/approval удалите старый `kid` из
   keyring.

Worker запрещено выдавать права на rotation/deletion, Transit config, TRON
config, HD seed, KV и wallet derivation.

## Перенос Vault на отдельную VM

1. Остановите outbound и создайте проверенный Raft snapshot.
2. Восстановите snapshot в независимо управляемом Vault и установите точный
   подписанный/зафиксированный binary TRON plugin.
3. Сохраните имена mounts и AppRole, Transit key names и versions/keyring,
   policies и sweep config.
4. Подключите приватную копию `proxy-external.example.hcl` с новым адресом/CA и
   выпустите новый wrapped SecretID.
5. Запустите только профиль `vault-proxy`.

Go-код и runtime-контракт worker при миграции не меняются.

Официальные материалы: [Vault Proxy API proxy](https://developer.hashicorp.com/vault/docs/agent-and-proxy/proxy/apiproxy), [AppRole auto-auth](https://developer.hashicorp.com/vault/docs/agent-and-proxy/autoauth/methods/approle), [Transit API](https://developer.hashicorp.com/vault/api-docs/secret/transit).
