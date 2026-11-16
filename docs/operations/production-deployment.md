# Production-развёртывание Metapus

> **TL;DR:** Репозиторий содержит полностью воспроизводимые local/Nile
> окружения, но не содержит turnkey production-манифест. Production строится
> из immutable release, использует внешний PostgreSQL и Vault, закрытый
> worker-контур, проверенные backup/restore и поэтапный rollout.
>
> **Тип:** Runbook
>
> **Аудитория:** DevOps, SRE, Security, DBA, Finance, Release Manager
>
> **Связанные:** [выбор контура](deployment-guide.md),
> [Vault/TRON](vault-tron-production.md),
> [архитектура криптопроцессинга](../crypto/crypto-processing-architecture.md),
> [release acceptance](../crypto/crypto-processing-test-scenarios.md)

## 1. Граница применимости

Следующие файлы являются local/test fixtures и **не являются production
deployment**:

- `single-tenant-deployment/docker-compose*.yml`;
- `deployments/single-tenant/docker-compose.yml`;
- `deployments/vault-test/*`;
- `scripts/dev/*`;
- все закоммиченные `.env*`, включая `.env.example`.

Значения в закоммиченных `.env*` — раскрываемые тестовые данные. Их публикация
не является утечкой production credentials. Это не делает их допустимыми в
production: production credentials выпускаются заново в secret manager/Vault,
имеют владельца, срок, аудит и процедуру ротации.

Сгенерированные `.env.crypto.local`, `public-test.env` и другие `.env*`
описанных local/Nile workflows также являются test-only. Production deployment
не использует `.env` как secret transport.

Старый `deployments/single-tenant/docker-compose.yml` оставлен как
демонстрационный self-hosted пример. Он использует публичные host ports,
env-based secrets и mutable images, поэтому запускать его как production
конфигурацию запрещено.

## 2. Целевая топология

```text
Internet
   |
managed LB/WAF or hardened edge proxy :443
   |-------------------------|
frontend                 metapus-server
                              |
                    private application network
                  /           |              \
        PostgreSQL TLS   metapus-worker   Vault Proxy
          + PITR          no ingress          |
                                          Vault HA
                                      Transit + TRON plugin
```

Обязательные свойства:

- наружу опубликован только HTTPS edge;
- PostgreSQL, worker, Vault и Vault Proxy не имеют public ingress;
- frontend, server, worker и tenant CLI используют один release ID;
- PostgreSQL шифрует transport, имеет PITR и отдельные роли для meta/tenant DB;
- Vault работает независимо от application hosts, с TLS, audit device,
  Integrated Raft/поддерживаемым HA backend и защищённым unseal process;
- private keys и authorization private keys не попадают в приложение;
- RPC/iTRX egress ограничен утверждёнными destinations и наблюдаем;
- admin endpoints требуют MFA/step-up, корректного client IP и доверенных
  proxy CIDR.

## 3. Production baseline

До первого релиза владельцы платформы фиксируют:

| Область | Минимальное решение |
|---|---|
| Release | immutable image digest, SBOM, provenance, release ID |
| Secrets | secret manager/Vault, owner, expiry, rotation и break-glass |
| PostgreSQL | TLS, encryption at rest, PITR, tested restore |
| Vault | HA, TLS, audit, backup, unseal/auto-unseal, tested restore |
| Edge | валидный public certificate, HSTS, rate limits, request limits |
| Network | deny-by-default ingress/egress, private DB/Vault |
| Observability | metrics, structured logs, traces, alert routing |
| Operations | RPO/RTO, rollback, incident response, on-call |
| Finance | wallet limits, treasury allowlist, reconciliation sign-off |

Конкретный оркестратор может быть Kubernetes, Nomad или управляемая container
platform. Этот runbook задаёт инварианты и порядок, а не подменяет
platform-specific manifests.

## 4. Сборка release

Production release собирается только из чистого commit/tag:

```bash
git fetch --tags --prune
git checkout <approved-tag-or-commit>
git status --short
git rev-parse HEAD

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

Дополнительно обязательны race/integration тесты затронутых crypto, Vault,
auth и storage пакетов и acceptance из
[crypto-processing-test-scenarios.md](../crypto/crypto-processing-test-scenarios.md).

Один pipeline собирает и подписывает:

- `metapus-server`;
- `metapus-worker`;
- `tenant` CLI;
- frontend;
- совместимые TRON/EVM Vault plugins для используемых family; при AML — оба с точным AML signing contract.

В deployment попадают image digests, а не `latest`. Release manifest сохраняет
commit, schema version, plugin checksum, migration checksum и SBOM.

## 5. PostgreSQL

Используйте отдельные credentials и минимальные права для:

- meta DB;
- каждой tenant DB;
- migrations;
- runtime server/worker;
- read-only диагностики/аудита.

Приложение не должно иметь права создавать роли или изменять инфраструктурную
конфигурацию. Migration principal может быть краткоживущим и доступным только
release job.

До rollout:

1. Создайте consistent snapshot/PITR restore point.
2. Проверьте свободное место, replication lag и backup freshness.
3. Выполните migrations бинарником `tenant` **того же release**.
4. Сверьте схему tenant DB с [ExpectedSchemaVersion](../../internal/core/version/version.go) из целевого release.
5. Выполните read-only smoke и только затем запускайте новый runtime.

Не запускайте старый binary поверх schema, для которой его совместимость не
доказана. Destructive migrations выполняются только expand/contract циклом:
сначала backward-compatible expand, затем rollout, затем отдельный cleanup
release после истечения rollback window.

## 6. Vault и signing boundary

Полная конфигурация Transit, AppRole/Proxy, TRON plugin и tenant policy описана
в [vault-tron-production.md](vault-tron-production.md). Для production:

- Compose profile `vault-test` не используется;
- Vault и его storage не находятся на application VM;
- TLS certificate validation не отключается;
- audit device и алерты включены до выдачи AppRole;
- plugin binary проверяется по checksum и версии release;
- unseal shares/root token не попадают в репозиторий, `.env`, CI log или
  application secret store;
- worker получает только адрес Vault Proxy и работает с
  `CRYPTO_AUTH_SIGNER=transit`;
- `VAULT_TOKEN` и `CRYPTO_SIGNING_AUTH_PRIVATE_KEY` в worker отсутствуют.

Response-wrapped SecretID одноразовый и краткоживущий. При потере state Vault
Proxy или истечении bootstrap нельзя повторно использовать старое значение:
оператор выпускает новый wrapped SecretID, доставляет его по защищённому
каналу и проверяет удаление bootstrap-файла после старта.

До Mainnet выполните Raft snapshot/restore, plugin recovery, Transit/signing
recovery, key rotation и loss-of-proxy drills.

## 7. Runtime configuration

Production configuration поступает через platform secret/config mechanism.
В deployment report фиксируются только имена параметров и версии secret, не
значения.

Минимально проверьте:

- `APP_ENV=production`;
- secure cookies и public canonical URL;
- `TRUSTED_PROXIES` содержит только фактические edge/LB CIDR;
- `ADMIN_SECURITY_MODE=enforce`;
- MFA/step-up и session TTL соответствуют политике;
- database URLs требуют TLS;
- TRON network, token contracts и confirmation policy соответствуют Mainnet;
- RPC credentials выпущены для production и имеют лимиты;
- outbound webhook DNS/SSRF policy включена;
- iTRX либо настроен и сверяется, либо явно отключён;
- wallet, withdrawal и treasury limits утверждены;
- hot-wallet TRX funding выполняется только через chain-verified immutable
  funding register.

## 8. Порядок rollout

### Подготовка

1. Утвердите release manifest и change record.
2. Проверьте quality gates, Nile acceptance и совместимость schema.
3. Проверьте свежесть PostgreSQL/Vault backup и результат последнего restore
   drill.
4. Проверьте on-call, alert routing, RPC/iTRX status и доступность rollback
   artifacts.
5. Зафиксируйте открытые reconciliation incidents; необъяснённых расхождений
   быть не должно.

### Развёртывание

1. Остановите создание новых финансовых операций либо включите maintenance
   policy, сохранив read-only status API.
2. Дождитесь завершения уже claim-нутых jobs, остановите **все server и worker**, обращающиеся к меняющейся схеме. Read-only API старой версии во время миграций также не обслуживается.
3. Создайте DB restore point.
4. Запустите `init-meta`/`migrate --all` binary текущего release.
5. Разверните server canary и проверьте health/readiness/schema/release ID.
6. Разверните frontend и выполните login/MFA/merchant portal smoke.
7. Запустите одну реплику worker. Встроенного tenant allowlist-флага нет: ограничьте canary явными network policies подготовленного tenant, оставив остальные исходящие политики выключенными.
8. Выполните read-only Vault checks и controlled low-value signing smoke.
9. Убедитесь, что нет новых held/failed/reconciliation incidents.
10. После приёмки поэтапно включите разрешённые network policies, масштабируйте worker и снимите maintenance policy.

### После rollout

Проверьте:

- release ID и schema на всех instances;
- invoice creation и payment page;
- MFA reload: остаётся один активный refresh-token;
- webhook delivery/retry;
- watcher checkpoints и RPC latency;
- Vault Proxy token renewal;
- outbox lag;
- held/failed movements;
- reconciliation и provider ledger.

Release закрывается только после совместной подписи Release Manager, SRE,
Security и Finance.

## 9. Rollback

Rollback приложения допустим, только если старый release совместим с текущей
schema.

| Ситуация | Действие |
|---|---|
| Ошибка до migrations | вернуть предыдущие image digests |
| Expand migration | Текущий `CompatibleSchema` требует точного совпадения версии: старый бинарник не допускается только на основании additive DDL. Нужен явно совместимый release или forward fix |
| Ошибка после начала blockchain broadcast | не повторять операцию вручную; восстановить по persisted signed tx/settlement |
| Несовместимая/destructive migration | остановить writes и выполнить утверждённый DB restore/forward fix |
| Vault/Proxy недоступен | signing остаётся fail-closed; не обходить Vault local signer |
| RPC/provider недоступен | остановить affected worker jobs, сохранить intents/reservations, дождаться recovery |

После rollback повторите readiness, reconciliation и controlled financial
smoke. Никогда не «лечите» инцидент удалением документов, movements,
settlements или immutable observations.

## 10. Backup, restore и disaster recovery

Для каждого production-контура заранее определяются RPO/RTO, retention,
географическое размещение и владелец восстановления.

Обязательный набор:

- PostgreSQL full backup + WAL/PITR;
- Vault Raft snapshots или backup поддерживаемого storage;
- versioned runtime configuration без secret values;
- release images/digests, migration artifacts и plugin binary/checksum;
- edge/TLS automation configuration;
- журнал wallet/Vault tenant mapping и операционные runbooks.

Backups шифруются, имеют checksum, immutable retention и отдельный access
control. Успешное создание backup не считается доказательством восстановления.
Не реже принятой политикой выполняйте isolated restore drill:

1. Восстановите meta и tenant DB в изолированной сети.
2. Восстановите Vault без доступа application runtime.
3. Проверьте plugin, tenant config, derived public addresses и policy.
4. Не выполняя Mainnet broadcast, проверьте authorization/signing на
   контролируемом fixture.
5. Сверьте balances, movements, settlements и reconciliation.
6. Зафиксируйте фактические RPO/RTO и устраните отклонения.

## 11. Мониторинг и алерты

| Сигнал | Минимальная реакция |
|---|---|
| `held`/`failed` financial operation | P1/P2 по сумме и причине, запрет ручного duplicate |
| Reconciliation mismatch/unknown outflow | P1, allocation hold, Finance + Security |
| Vault sealed/unready/renewal failure | P1, signing fail-closed |
| Watcher checkpoint lag/RPC errors | P1/P2, остановить выдачу затронутых адресов |
| Outbox/webhook lag | P2, replay только через штатный workflow |
| PostgreSQL replication/backup failure | P1/P2 по RPO |
| iTRX balance/order ambiguity | P2, provider query и reconciliation |
| TLS renewal failure | P1 до срока истечения |
| MFA/admin anomaly | Security incident workflow |

Алерт считается готовым только после тестовой доставки в дежурный канал и
проверки escalation.

## 12. Mainnet release gate

Mainnet запрещён, пока не выполнены:

- внешний hardened Vault и проверенные backup/restore/rotation;
- PostgreSQL PITR и restore drill;
- P1 alerting и on-call;
- формализованная finality/reorg policy;
- load, concurrency, timeout, restart и provider-outage tests;
- treasury happy path и maker-checker;
- reconciliation/proof-of-assets-and-liabilities процедура;
- утверждённые лимиты hot wallet и treasury;
- независимый security/finance sign-off.

Закрытые программные regression gates — lifecycle webhook, failed-withdrawal
compensation, fee fail-closed, token isolation, dynamic watcher и
multi-contract loading — должны продолжать выполняться на каждом release.

## 13. Teardown и decommission

Production teardown — отдельное change с Finance/Security approval:

1. Запретите новые операции.
2. Дождитесь settlement и закройте incidents.
3. Сверьте ledger, blockchain, provider и treasury.
4. Переведите остатки утверждённым withdrawal/treasury workflow.
5. Создайте финальные backups и проверьте checksums.
6. Отзовите API keys, AppRole, RPC/iTRX credentials и certificates.
7. Удалите runtime resources по platform-specific procedure.
8. Сохраните audit, financial records и backups на срок retention policy.

Удаление volume, DB или Vault storage до финансовой сверки и проверенного
backup запрещено.
