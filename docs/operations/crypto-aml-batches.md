# AML: партии консолидации и доверенный provisioning

Миграции 00067–00068 добавляют batch intake/capacity, выбор своего collector при планировании свипа и отдельный control-plane путь создания merchant wallets. Они не включают отправку через старый outbound контракт.

Схема 82: для sweep `reserved → committed` определяется отдельным final principal каждой строки и её точными registry movement IDs. Не нужно ждать весь sweep-документ; broadcast не расходует физический principal и не освобождает allocation. Final revert атомарно сохраняет actual fee и audited cancellation, без principal/storno.

## Партии

`ConfigureAMLBatchPolicy` создаёт неизменяемую версию лимитов для merchant/network/token/low-or-medium. Нужны enabled AML scope, enforced custody, остановленное network execution и отсутствие активных старых allocations без batch. Лимиты задаются в точных gross minor units и секундах. Изменение политики закрывает и удерживает старые партии.

`OpenAMLBatch` закрепляет один собственный collector за одним token/class на весь срок его использования. Один wallet нельзя повторно назначить другой партии, даже после опустошения. При открытии проверяются active chain account, явный пустой baseline, отсутствие allocation holds, token history и других ненативных остатков. Партия сохраняет policy/scope versions, лимит суммы и срок приёма; одинаковый ID с изменённым содержимым не принимается.

`ReserveAMLBatchSweep` выбирает открытый collector своего merchant/network/token/class, создаёт durable sweep attempt и резервирует exact principal/fee split в одной транзакции. `ReserveAMLInternalAllocation` также принимает явные batch/revisions. После настройки batch policy нельзя создать новую allocation без batch, в том числе прямым SQL INSERT. Batch identity уже созданного плана неизменяема.

Ёмкость считается по lifetime intake:

```text
committed + reserved + unplanned + new gross <= max gross
```

`committed` — применённые incoming allocations; последующие выводы не восстанавливают intake capacity. `reserved` сохраняет весь gross подписанного/неизвестного свипа. `unplanned` показывает canonical incoming effects, не связанные с действующим планом этой партии. Proof уже запланированного свипа не учитывается вторично поверх его резерва.

Достижение лимита и истечение времени закрывают intake. CryptoProcessor закрывает просроченные партии до HTTP AML jobs. Ручное закрытие требует текущую revision, причину и actor. Закрытие не отменяет ранее зарезервированные свипы: при сохранении технических/risk guards они могут завершиться, и reserved перейдёт в committed. Audited cancellation до любой авторизации освобождает резерв и завершает unsigned attempt, но не открывает старую партию снова. Планировщик выбирает следующий заранее подготовленный collector.

Execution revision отделена от intake revision. Простое закрытие intake не обесценивает средства в пути. Late high у origin/collector, reorg, смена scope/policy или незапланированный incoming закрывают intake и ставят execution hold. Лишний внешний перевод сохраняется в evidence и `unplanned`, даже при превышении лимита. Новая подпись по удержанной партии отклоняется; реально выполненный перевод всё равно учитывается по исходному split. История открытия/закрытия/hold и allocation transitions неизменяема. Снятие постоянного hold требует будущего reviewed resolution workflow.

## Создание кошелька мерчанта

`MerchantWalletProvisioningService` и команда `cmd/aml-wallet-provision` работают через отдельный доверенный Vault Proxy. Права обычного worker не расширяются. Для каждого request ID сначала фиксируются owner, network, role, actor и один HD path из общего append-only derivation registry. Проверяются review flags, конечная network capacity и отсутствие второго активного fee wallet мерчанта. Потерянный ответ не выделяет другой путь.

После коммита SQL lease сервис проверяет custody readiness Vault, выводит адрес и записывает immutable owner/role binding. Только после успешной проверки ответа он публикует owned wallet в SQL. Внешний вызов не выполняется внутри SQL transaction; вызов сервиса из такой транзакции отклоняется. Completed request повторно возвращает тот же wallet, изменённая identity отвергается. Lease token не позволяет старому процессу завершить публикацию после перехвата задачи. Ошибка оставляет тот же request/path с backoff 30 секунд.

Vault namespace связан с именем конкретной tenant database неизменяемой строкой `sys_crypto_aml_provisioning_identity`. Это криптографическая конфигурация базы, а не поле tenant для фильтрации строк. Привязка создаётся отдельным явным первым enrollment; командный tenant ID должен совпадать с сохранённой привязкой. Копия базы с другим именем не проходит preflight автоматически.

Для команды нужны `AML_CONTROL_DATABASE_URL` и `AML_CONTROL_VAULT_ADDR`, указывающий на выделенный control-plane proxy. Команда не принимает Vault token и не меняет enforcement policies:

```sh
go run ./cmd/aml-wallet-provision \
  -tenant-id '<tenant UUID>' \
  -merchant-id '<merchant UUID>' \
  -network-id '<network UUID>' \
  -request-id '<stable request UUID>' \
  -role liquidity_hot \
  -actor '<operator identity>'
```

При первом доверенном enrollment базы дополнительно передаётся `-enroll-database`. Повторяются те же request ID, owner и role. Успех печатает wallet UUID. Флаг не разрешает перепривязать уже зарегистрированную базу.

Новый кошелёк получает `chain_balance_check_pending`; TRON account остаётся `unknown` до отдельного подтверждения активации. Targeted reconciliation подбирает и опубликованные merchant collectors/resource wallets. Для batch по-прежнему нужны актуальная независимая сверка, явный пустой token baseline, активный account и снятый reconciliation hold. Provisioning не создаёт merchant credit, gas funds или предположительный low.

В enforced custody сетях общий warm-pool больше не планирует и не claim-ит новые shared derivations. SQL guard также блокирует публикацию unowned pool wallet из старой уже claimed задачи. Старые pending paths остаются сохранёнными для разбора.

## Границы и проверки

Явный trusted request поддерживается наряду с [автоматическими merchant pool intents](crypto-aml-merchant-pools.md), миграция 00069. Watermarks и ротация подготовленных batches подключены. [Автоматический baseline/activation, class-specific export и permits](crypto-aml-outbound.md) реализованы миграциями 00070–00073. Token sweep allocation проверяется перед authorize/sign/broadcast. Native principal consolidation, общий enrollment/rollout orchestration и административный UI остаются отдельными этапами.

SQL tests проверяют concurrent capacity разных origins, закрытие при неизвестном результате, исполнение ранее зарезервированного свипа после закрытия intake, lifetime committed, безопасную отмену и выбор следующей партии, unsolicited excess, expiry и policy change. Provisioning tests проверяют публикацию после enrollment, отсутствие SQL transaction во время внешнего вызова, replay, потерю ACK, тот же HD path, foreign binding и lease fencing. HTTP tests обоих Vault adapters используют synthetic responses. Рабочие БД и Vault не менялись, настоящих подписей, broadcast и платных AML-запросов нет. Откат с batch/provisioning evidence запрещён.
