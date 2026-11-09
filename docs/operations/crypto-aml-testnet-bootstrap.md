# Начальная классификация существующего testnet inventory

Миграция 00075 и `cmd/aml-testnet-bootstrap` добавляют явный однократный импорт существующих средств Nile/Sepolia в синтетический класс `low`. Это начальная тестовая оценка, не отчёт AML-провайдера. Финансовые документы, physical/merchant movements и fee ledger сохраняются.

## Условия

- Только точные network identities `tron/nile` и `evm/11155111`; immutable namespace enrollment базы.
- Остановлены writers и network dispatch. Нет активных principal/fee reserves или незавершённых withdrawal requests.
- Исторический principal принадлежит одному явно указанному merchant; owner уже согласован в SQL и независимом Vault registry.
- Для всех участвующих активов включены scopes отдельного `testnet_aml`. Уже существующий baseline или AML effects не перезаписываются.
- Для каждой пары wallet/token нужна реальная сверка chain=ledger=journal, без delta, не старше 10 минут. Штатная `crypto-preflight -mode balance` получает свежие проверки; их timestamps не изменяются вручную.
- Canonical cutoff height/hash берутся из остановленного watcher checkpoint. Покрываются все активные кошельки и любые ненулевые остатки сети.

## План и применение

`AML_CONTROL_DATABASE_URL`/`AML_CONTROL_DATABASE_URL_FILE` указывают на enrolled tenant-базу. Текущий бинарник ожидает schema 76 (`internal/core/version/version.go`); opening inventory введён миграцией 00075. В `init` Docker image включены `aml-testnet-config`, `aml-testnet-bootstrap`, `aml-wallet-provision` и `aml-pool-controller`.

```sh
go run ./cmd/aml-testnet-bootstrap \
  -tenant-id '<tenant UUID>' \
  -request-id '<постоянный bootstrap UUID>' \
  -merchant-id '<merchant UUID>' \
  -network-code TRON-NILE \
  -block-number '<canonical height>' \
  -block-hash '<canonical hash>' \
  -valid-days 90 \
  -actor '<operator>' > bootstrap-nile.json
```

Без `-apply-manifest` команда читает repeatable read snapshot и выводит JSON-план. Principal распределяется по существующим owned wallets: сначала liquidity hot, затем merchant pool, внутри роли по wallet UUID. Затем отдельно распределяется существующая processing fee. Остаток native inventory становится operating capital; неатрибутированный остаток contract token отклоняется. Network fee wallet не получает merchant principal.

Перед применением проверить manifest и сохранить его вместе с backup:

```sh
go run ./cmd/aml-testnet-bootstrap \
  -tenant-id '<tenant UUID>' \
  -apply-manifest bootstrap-nile.json
```

Для Sepolia создаётся отдельный plan/request UUID. SQL повторно сверяет owner, тип сети, emulator account, paused policy, journal digest и реальную reconciliation evidence. В одной транзакции сохраняются immutable opening evidence, baselines и component positions. Deferred constraint проверяет полноту кошельков/baselines и точное сохранение merchant/fee totals. Повтор того же manifest идемпотентен; изменённый manifest с тем же ID отклоняется.

## После импорта

Worker пересчитывает обычные risk projections и события. Opening contribution имеет отдельный срок действия — по умолчанию 90 дней, максимум 365. Она не наследует 10-минутный TTL обычной provider decision. Истечение переводит оценку в unknown; команда не продлевает уже сохранённый bootstrap и не снимает исторический high.

История до cutoff покрывается opening evidence: backlog не создаёт случайные отчёты для уже импортированных старых поступлений. Новые proofs после cutoff проходят обычный emulator submit/poll/report/decision. Новый high распространяется по обычным правилам. Изменение старого physical journal обнаруживается по digest; расхождение component/physical inventory закрывает покрытие.

Выполнить AML configuration preflight, проверить Portal risk balances, сверить финансовые fingerprints и восстановить прежние network policies. После остановки watcher может догонять цепочку: свежий dispatch permit продолжает требовать актуальные checkpoints и не разрешает обход backlog.

Проверки реализации: PostgreSQL bootstrap без financial writes, replay/изменённый request, сохранение principal/fee/operating split, новые случайные проверки после opening, повреждение inventory, mainnet rejection; полный integration/race suite и migration Down/Up.
