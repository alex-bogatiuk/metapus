# TRON v1 — Mainnet go/no-go checklist

> **Default decision:** `NO-GO`.
>
> Этот checklist применяется только после подписанного
> [Nile acceptance](crypto-nile-acceptance.md) и
> [merchant webhook production readiness](merchant-webhooks-production-readiness.md).
> `GO` разрешает отдельный
> ограниченный canary; он не означает production launch.

## 1. Release identity и change control

- [ ] Commit и image digests совпадают с Nile-accepted candidate.
- [ ] Schema version совпадает с `ExpectedSchemaVersion` текущего release (сейчас `76`); migration/rollback rehearsal и [AML configuration preflight](crypto-aml-preflight.md) приложены.
- [ ] TRON plugin binary зарегистрирован по проверенному SHA-256.
- [ ] Diff после Nile принятия пуст либо повторно прошёл полный acceptance.
- [ ] Change ticket содержит окно, владельца, rollback authority и контакты P1.

## 2. Vault и secrets

- [ ] Vault работает с TLS, HA/Raft, audit devices и раздельным mount/plugin на
  family `tron`.
- [ ] Worker использует tokenless proxy; статический `VAULT_TOKEN` отсутствует.
- [ ] Transit используется только для control-plane authorization, TRON plugin
  — для policy-aware transaction signing.
- [ ] AppRole/identity policies проверены отрицательными cross-tenant и
  cross-family тестами.
- [ ] Выполнены backup/restore и key-rotation drills; recovery material хранится
  по maker-checker процедуре.
- [ ] Все credentials, когда-либо попавшие в source, terminal/chat или общий
  artifact, отозваны и заменены.

## 3. PostgreSQL и финансовые гарантии

- [ ] PITR и restore drill восстановили tenant до согласованного chain/ledger
  checkpoint.
- [ ] Serializable concurrency tests доказали отсутствие daily-budget
  oversubscription и duplicate JIT funding.
- [ ] Finance сверил taxonomy: rental = `chain_resource_rent`, receipt fee =
  `chain_network_fee`, activation receipt fee = `chain_network_fee` с
  `purpose=activation`; отдельного cost type для активации в актуальной схеме нет.
- [ ] `native_burn` используется только как strategy/telemetry dimension.
- [ ] Principal JIT funding не признан platform cost.
- [ ] Proof-of-assets-and-liabilities процедура выполнена и подписана.

## 4. Provider и network resilience

- [ ] Для каждой Mainnet RPC role настроены минимум два административно
  независимых endpoint; приоритеты и quota документированы.
- [ ] Circuit breaker и failover проверены на 429, 5xx, timeout, invalid reply и
  lagging SolidityNode.
- [ ] iTRX contract/SLA, capacity, reconciliation и dispute procedure приняты.
- [ ] Неизвестный iTRX order status fail-closed и не включает burn fallback.
- [ ] Ambiguous broadcast drill подтвердил lookup и повтор того же envelope без
  rebuild/re-sign.

## 5. Limits и policy sign-off

Заполнить до голосования; пустое поле означает `NO-GO`:

| Control | Mainnet value | Owner | Evidence |
|---|---:|---|---|
| Per-transaction max fee |  | Finance |  |
| Daily burn budget |  | Finance/Risk |  |
| Daily JIT funding budget |  | Treasury/Risk |  |
| Minimum fee-wallet balance |  | Treasury |  |
| Minimum source residual |  | Finance |  |
| JIT funding amount |  | Treasury |  |
| iTRX/RPC breaker thresholds |  | SRE |  |
| Canary total value limit |  | Risk |  |
| Canary transaction count |  | Risk |  |
| Canary duration |  | SRE |  |

Привязки fee sources каждого custody owner проверяются в реестре кошельков.
Budgets и `prefer_rental` применяются через audited Admin PATCH со step-up MFA
непосредственно перед canary. До этого policy выключена.

## 6. Observability, alerts и runbooks

- [ ] Dashboards показывают RPC role/provider health, signer/iTRX readiness,
  reservation/budget, ambiguous broadcast, settlement lag и reconciliation.
- [ ] P1 alerts имеют tested routing, primary/secondary on-call и escalation.
- [ ] Runbooks покрывают signer down, RPC partition, lagging Solidity,
  ambiguous broadcast, iTRX ambiguity, fee-wallet low balance, held reservation
  и ledger divergence.
- [ ] OTel cardinality/billing оценены; секретные и персональные attributes
  отсутствуют.
- [ ] Merchant webhook SLO, queue age, terminal failure и reconciliation drift
  alerts прошли test paging; TRON Nile и Ethereum Sepolia conformance приложены.
- [ ] Load/concurrency/provider-outage chaos tests выдержали ожидаемый peak с
  запасом, зафиксированным SRE.

## 7. Security и treasury controls

- [ ] Независимый security review закрыт без critical/high findings.
- [ ] Treasury maker-checker протестирован на реальной Mainnet-конфигурации без
  отправки транзакции.
- [ ] Fee wallet имеет минимально необходимый баланс и отдельные alert limits.
- [ ] Network fee wallet исключён из invoice allocation и hot-wallet selection.
- [ ] Permission `crypto.network_operation.allow_editing` назначен только
  системным администраторам; `security_admin` получает отказ.
- [ ] Fresh step-up, confirmation, `sys_event_log` и autoblock проверены для CNO
  lifecycle `unlock → unpost → edit → post`.
- [ ] Принят residual risk MVP: отдельной истории значений CNO до/после ручной
  коррекции нет; доступ и журнал команд включены в процедуру контроля.
- [ ] Emergency disable проверен: `enabled=false` останавливает новые attempts,
  но confirmation/reconciliation продолжают безопасно обрабатывать уже
  broadcast транзакции.

## 8. Решение и canary

Обязательные подписи: Engineering, SRE, Security, Finance, Risk, Treasury и
Product owner. Любой `NO-GO` имеет приоритет над голосами `GO`.

```text
decision: GO / NO-GO
candidate digest:
policy version:
decision time UTC:
approvers:
exceptions: none / linked risk acceptance
```

После `GO` проводится отдельный canary:

1. Включить одну Mainnet network policy с минимальными лимитами.
2. Выполнить заранее утверждённые малые операции и дождаться solidified
   receipts/ledger reconciliation.
3. При любом ambiguous/held/unexplained incident немедленно выключить новые
   attempts и перейти к runbook; не rebuild и не re-sign.
4. Production launch разрешить только отдельным решением после canary review.
