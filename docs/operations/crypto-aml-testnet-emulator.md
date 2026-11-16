# AML эмулятор для Tron Nile и Ethereum Sepolia

Эмулятор зарегистрирован как отдельный провайдер `testnet_aml`. Он генерирует синтетические ответы и использует обычные durable jobs, tickets, reports, risk decisions и wallet projections. Blockchain ingestion, подтверждение факта поступления, merchant ownership, principal/gas accounting и dispatch permits работают по прежним правилам.

Эмулятор не имеет HTTP client, не использует API-ключи и не расходует AML package credits/paid admissions. Автоматического fallback с настоящего провайдера на эмулятор нет. Его использование выбирается явно через account и routing scopes.

## Случайные ответы

- Risk score равномерно выбирается из целых `0..10000` и сохраняется также как точная дробь `0..1` в provider-shaped JSON.
- При стандартной policy границы классов — 3000/7000; low/medium/high определяет обычный risk policy evaluator. Срок действия оценки (TTL) составляет 24 часа (86 400 секунд), возраст отчёта — до 24 часов (политика `aml-testnet.v2`).
- По умолчанию 10% ответов имеют недостаточное покрытие: такой report приводит к unknown, независимо от score. `--unknown-rate-bp` задаёт долю `0..10000`.
- Ответ может оставаться pending до 10 секунд. `--max-delay-seconds` задаёт максимальную задержку `0..60`; значение 0 удобно для быстрого smoke test.
- Seed создаётся один раз либо задаётся явно. Результат зависит от seed, account/job identity, generation, submit request ID и digest проверяемого эффекта. Повторный poll и перезапуск worker не меняют score и raw report. Новая проверка/rescreen получает новый результат.
- Исторический high остаётся ограничением по обычной sticky-high policy. Новый случайный low сам по себе не стирает прежнее adverse evidence и не создаёт повторного credit.

Сигналы — `wallet` с точным дробным score и `sanctions=0`. Формат синтетический, а не принятие реального vendor contract. Persisted provider равен `testnet_aml`, contract — `aml-testnet.v1`, adapter — `testnet-aml.v1`; raw response содержит `emulated=true`. В AML preflight число таких scopes видно в `emulatedScopes`.

Для существующих средств доступен отдельный [testnet bootstrap](crypto-aml-testnet-bootstrap.md): явная стартовая оценка low без финансового перезачисления. Он не заменяет случайную оценку новых поступлений.

## Настройка

Сначала применить миграции до 75. Нужны активные catalog networks с точными identity `tron/nile` и `evm/11155111`, native tokens, подготовленный merchant custody и immutable enrollment базы в Vault namespace. Network policy на время настройки должна быть выключена. Source wallets и собственный operating gas подготавливаются обычным способом.

Для каждого мерчанта и сети выполнить control-plane команду, используя постоянный account UUID:

```sh
go run ./cmd/aml-testnet-config \
  -tenant-id '<enrolled tenant UUID>' \
  -merchant-id '<merchant UUID>' \
  -network-code TRON-NILE \
  -account-id '<stable emulator account UUID>' \
  -seed 'nile-scenarios-v1' \
  -max-delay-seconds 10 \
  -unknown-rate-bp 1000 \
  -actor '<operator identity>'
```

Используется `AML_CONTROL_DATABASE_URL` или `AML_CONTROL_DATABASE_URL_FILE`. Для Sepolia указать её catalog code (обычно `ETHEREUM-SEPOLIA`), отдельный account UUID и seed. Имя сети само по себе не разрешает эмуляцию: проверяются family и network identifier.

Команда атомарно создаёт account без credentials, immutable binding с seed/параметрами, synthetic mappings для активных native/TRC-20/ERC-20 assets, testnet risk policy и enabled scopes данного мерчанта. Она не включает network dispatch, не создаёт финансовые проводки и не назначает owner кошелькам. Пустой seed генерируется при первом запуске и возвращается в JSON; при повторе с тем же account используется сохранённый seed.

Повтор той же конфигурации идемпотентен. Seed, сеть и параметры binding неизменяемы; для другой конфигурации нужен новый account и явный переход scopes. Существующие отличающиеся scopes, включая live provider, команда не перезаписывает. Старые jobs/tickets остаются закреплены за своим provider/account; переключение тестового режима не превращает реальные pending jobs в синтетические.

Обычная форма создания integration account не создаёт этот профиль: настройка выполняется control plane с проверкой enrollment. Активность account и `allow_submissions` продолжают управлять обработкой новых jobs; календарного пополнения пакета для эмулятора нет.

## Ограничение testnet

Адаптер повторно проверяет network identity при каждой resolution. PostgreSQL независимо запрещает binding/job/synthetic mapping на другой сети, смену identity уже привязанной сети, подмену provider/account и смешивание синтетического report с live check. Кредиты и paid admissions для эмулятора запрещены. Отчёты и binding сохраняют неизменяемую историю; rollback с данными требует forward migration.

После настройки выполнить [AML configuration preflight](crypto-aml-preflight.md), ingestion preflight, сверку balances и обычные тестовые платежи. Эмуляция помогает проверить маршрутизацию и ошибки начальной классификации; mainnet vendor coverage и настоящий vendor billing принимаются отдельно.
