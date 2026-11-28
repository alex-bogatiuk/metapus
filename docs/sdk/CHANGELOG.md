# CHANGELOG публичного API (`pkg/sdk`)

Каждое изменение `pkg/sdk` записывается здесь вместе с новой `sdk.APIVersion` ([правила](README.md#версионирование)).

## 3.10.0 — 2026-11-27

Реквизиты надстройки к справочникам базы и платформы (этап B, срез 1). Только добавления: конфигурации для 3.0.0–3.9.0 совместимы.

- `registry.Extend[T, E](addOn, group)` и `Registry.RegisterExtension` — надстройка объявляет свои реквизиты `E` (структура с тегами `db`/`json`/`meta`, как у модели) к справочнику модели `T` своей базы или платформы. Данные — в таблице надстройки `<таблица справочника>__<надстройка>` «один к одному» (`id` — PK и FK на таблицу справочника с `ON DELETE CASCADE`), её создаёт миграция надстройки. Платформа читает и пишет строку вместе с элементом в одной транзакции и отдаёт реквизиты плоско, рядом с реквизитами справочника: в запросах и ответах, списке и фильтрах, форме (группа `group`), загрузке из файла и политиках полей. `Extension.OnWrite` — хук записи для вычисляемых реквизитов, `Extension.Search` — поиск списка по колонке расширения, `Extension.Of` — значение расширения у элемента. Типы `registry.Extension`, `registry.ExtensionRegistration` ([слои приложения](../systems/configuration-layers.md#этап-b-реквизиты-надстройки)).
- HTTP-контракты: расширение даёт контракты `<Справочник><Надстройка>Request` и `Response` во фронтенд-файлах надстройки и маршруты с `extends: true`, которые `registerHTTPRoutes` вливает в маршруты базы.

## 3.9.0 — 2026-11-27

Декларативные RLS-измерения и лимитирование HTTP-маршрутов конфигураций. Только добавления: конфигурации для 3.0.0–3.8.0 совместимы.

- `entity`: поле `Definition.RLSDimensions` (`map[string]string` имя измерения → SQL-колонка) — единый источник правды для Row-Level Security справочников и документов. Репозитории (`catalog_repo.NewCatalogRepoForDefinition`, `document_repo.NewDocumentRepoForDefinition`), метаданные чтения и регистрация маршрутов автоматически читают `Definition.RLSDimensions` и проверяют наличие колонок на старте процесса.
- `http/middleware`: `RateLimit(rps, burst)` — токен-бакет лимитирование по IP для публичных и API-key маршрутов конфигураций.
- Платформенная миграция `00010_idempotency_archive_index.sql`: частичный индекс `idx_sys_idempotency_archive_pending`; `ExpectedSchemaVersion` = 10.

## 3.8.0 — 2026-11-27

Слои приложения: базовая конфигурация и надстройка. Только добавления: конфигурации для 3.0.0–3.7.0 совместимы.

- `configuration.AddOn` — конфигурация, построенная на базовой (`Base`, `BaseVersion`); `configuration.Versioned` — базовая конфигурация с версией (`Version`), на которую строятся надстройки. Платформа собирает базу и надстройку в одно приложение, проверяет совместимость версий, ведёт отдельные истории миграций и записывает надстройку у тенанта ([слои приложения](../systems/configuration-layers.md)).
- Метамиграция `00003_tenant_addon.sql`: `tenants.addon`, `tenants.addon_schema_version`.

## 3.7.0 — 2026-11-27

Последовательности документов. Только добавления: конфигурации для 3.0.0–3.6.0 совместимы.

- `entity`: `SequenceRule` (`In`, `Restores`), `SequencePeriod` и поле `Definition.Sequence`. Репозиторий документа поддерживает границу последовательности: проведение документа последовательности задним числом сдвигает её назад, закрывающий документ проводится, только пока граница не раньше его периода, и сдвигает её на конец периода. Объявленный закрывающий документ отдаёт состояние на `GET /document/<route>/:id/sequence` ([последовательности документов](../systems/document-sequences.md)).
- `posting.CodeSequenceNotRestored` (`SEQUENCE_NOT_RESTORED`, 422).
- Платформенная миграция `00009_sequence_boundaries.sql`: `sys_sequence_boundaries`; `ExpectedSchemaVersion` = 9.

## 3.6.0 — 2026-11-27

Статусы документов. Только добавления: конфигурации для 3.0.0–3.5.0 совместимы.

- `entity`: `StatusModel`, `StatusValue`, `StatusTransition` и поле `Definition.Statuses`. Документ объявляет колонку статуса, начальный статус, статусы с названиями и разрешённые переходы, у перехода может быть своё право `<вид>:<ключ>:<действие>`. Репозиторий документа (`document_repo.NewDocumentRepoForDefinition`) проверяет модель при каждой записи и пишет историю статусов. Объявленные документы отдают её на `GET /document/<route>/:id/status-history`, а право перехода появляется в редакторе ролей ([статусы документов](../systems/document-statuses.md)).
- Платформенная миграция `00008_document_status_history.sql`: `sys_document_status_history`; `ExpectedSchemaVersion` = 8.

## 3.5.0 — 2026-11-26

Регистры сведений. Только добавления: конфигурации для 3.0.0–3.4.0 совместимы.

- Новый пакет `registers/information`: `New[R]`, `Register[R]`, `Definition`, `Periodicity` (`NonPeriodic`, `Second`, `Day`), `Mode` (`Independent`, `Recorder`), `RecordBase`, `Schema`, `Field`, `FieldType`, `Filter`, `Declared`, `Install`. Независимый регистр пишут `Set` и `Delete`, подчинённый — проведение документа (`Register.Record` в `RegisterMovements`). Запросы: `SliceLast`, `Records`, `RecordsOf`.
- `registry.Registry.RegisterInformation`, `InformationRegisters`: сервер подключает подчинённые регистры к проведению, миграция тенанта сверяет таблицы с объявлением.
- `registers`: `InformationStore`, `NewInformationStore`; `migration`: `InformationDDL`.
- `registers/accumulation`: `Movements.Recorder`, `Movements.Fail`, `Collect` — общий сборщик записей документа для регистров разных видов.

## 3.4.0 — 2026-11-26

Ввод на основании. Только добавления: конфигурации для 3.0.0–3.3.0 совместимы.

- `registry`: `Basis`, `BasisRule`, `Registry.RegisterBasis`, `Registry.BasisRules`. Правило заполняет новый документ из другого. Платформа отдаёт черновик на `GET /document/<цель>/based-on/<ключ источника>/:id` (права `<цель>:create` и `<источник>:read`; источник читается с политиками пользователя, черновик не сохраняется) и добавляет пункт «Создать на основании» в метаданные источника (`createBasedOn`).
- Объявленные документы (`registry.Document`) принимают и возвращают `basisType` и `basisId`.

## 3.3.0 — 2026-11-26

Контроль отрицательных остатков декларативных регистров. Только добавления: конфигурации для 3.0.0–3.2.0 совместимы.

- `registers/accumulation`: `Definition.NonNegative` — проведение, перепроведение и отмена проведения не уводят ресурс регистра остатков ниже нуля. Платформа блокирует комбинации измерений документа в общем отсортированном порядке и после записи всех регистров отказывает ошибкой с кодом `CodeInsufficientBalance` (`INSUFFICIENT_BALANCE`, 422) и деталью `shortages` (`Shortage`, `DimensionValue`). Приход в уже отрицательный остаток проходит.
- `Declared.BindNames` — имена ссылок в сообщении о нехватке через `RefResolver` платформы; сервер привязывает его сам.
- `posting`: движок вызывает новые `PostingController.BeginControl` (до сторно) и `PostingCheck` (после записи регистров) у recorders, которые их реализуют.

## 3.2.0 — 2026-11-26

Декларативные регистры накопления. Только добавления: конфигурации для 3.0.0 и 3.1.0 совместимы.

- Новый пакет `registers/accumulation`: `New[M]`, `Register[M]`, `Definition`, `Kind` (`Balances`, `Turnovers`), `Schema`, `Field`, `FieldType`, `Filter`, `Source`, `Movements`, `Declared`, `Install`. Регистр объявляется структурой движения с тегами `register:"dimension"` и `register:"resource"`; платформа сама хранит движения и остатки, проводит регистр, показывает «Движения документа» и отвечает на запросы `Balances`, `BalancesAt`, `Turnovers`.
- `registry.Registry.RegisterAccumulation`, `AccumulationRegisters` — регистрация регистра; сервер подключает его к проведению, миграция тенанта сверяет его таблицы с объявлением.
- `registers`: `AccumulationStore`, `NewAccumulationStore` — хранилище декларативных регистров, для тестов, которые проводят без сервера.
- `migration`: `AccumulationDDL` — SQL таблиц и триггеров регистра для миграции конфигурации.
- `entity.MovementBase.Header` — общие поля движения для обобщённого кода.
- Платформенная миграция `00006_accumulation_registers.sql`: общие функции триггеров `sys_accumulation_balances` и `sys_accumulation_movements_immutable`; `ExpectedSchemaVersion` = 6.

## 3.1.0 — 2026-11-22

Расширенный слой: всё, что использует крипто-конфигурация, которая теперь, как и «Автозапчасти», импортирует из платформы только `pkg/sdk`. Только добавления, поэтому конфигурации для 3.0.0 совместимы. Версия ещё не выпущена (тега нет), поэтому сужение слоя вошло в неё же.

- Новые пакеты: `appctx`, `tenant`, `tx`, `tenantdb`, `json`, `logger`, `secretbox`, `demoseed`, `numerator`, `workerjob`, `security`, `security_profile`, `readaccess`, `readpolicy`, `domain`, `audit`, `cursor`, `filter`, `eventlog`, `notifications`, `settings`, `command`, `registers/exchange_rate`, `reports/{schema,contract,compiler}`, `printing`, `printing/render`, `integrations/{transport,profiles,providerpolicy,integrationapp}`, `urlsafe`, `http/{v1,handlers,middleware,dto,httpvalue,jsoncodec}`, `postgres`, `postgres/{catalog_repo,document_repo,integration_repo,filter,keyset,numerator}`, `migration`, `metadata`, `telemetry`, `envconfig`, `secretfile`.
- Дополнены `id`, `types` (`RawAmount` и конструкторы), `apperror`, `entity`, `integrations` (аккаунты, пулы, провайдеры, учётные данные), `contracts` (`Request`, `Response`, `RouteContract`, `Routes`, `Registry`), `registers` (`ExchangeRateRepo`), `configuration` (`Compose`, `HTTPArtifacts`, `HTTPContractBundles`, `Permissions`, `Extensions`).
- `posting`: `DocumentLocker`, `NewLockingEngine` — движок с блокировкой документов; `NewEngine()` по-прежнему без неё.
- `migration`: `PlatformSchemaVersion`, `CompatiblePlatformSchema`, `HighestVersion` — версии схемы для проверки тенантов конфигурацией.
- `http/middleware`: `BindTenant` — привязка тенанта для маршрутов, которые находят его сами; `http/v1`: `RouteContext.ProtectedGroup`, `RouteContext.Authenticated` — группы под своим префиксом с цепочкой платформы.
- `postgres`: `ReferenceResolver.Resolve(ctx)` — без неиспользуемого аргумента-пула.
- `secretbox`: `Seal`, `Open`, `Ready`, `ErrNotConfigured` — учётные данные шифрует платформа своим ключом; `encryption` с ключом-параметром в фасад не вынесен. `postgres/catalog_repo`: `AccountCredentials`, `PoolAccountKeys`, `AccountKeysOf`; ключ шифрования интеграций (`IntegrationEncryptionKey`), `PoolMemberIDs` и `AccountKeyNamesOf` с шифротекстом-параметром не вынесены.
- `postgres/catalog_repo`: `PoolProvider`, `AccountPoolIDs`, `PoolsSharingAccounts`, `AccountProvider`, `ProviderAccountIDs`, `UsableAccountIDs`, `RequireUsableAccount`, `LockUsableAccount`, `CreateKeylessAccount` — конфигурация читает пулы и аккаунты интеграций через платформу, а не SQL по её таблицам.
- Новый пакет `demoseed`: `Currency`, `EnsureCurrency`, `RateMapping`, `MapRate`, `Notification`, `TelegramNotification` — демо-валюты, привязки курсов и уведомление в Telegram для сида конфигурации.
- `postgres`: `OutboxRecord`, `EnqueueOutbox`, `PendingOutboxPayloadValues` — запись в outbox и проверка ожидающих сообщений без SQL по `sys_outbox`; `OutboxStatusPending` не вынесен.
- `eventlog`: `CategorySystem`; `notifications`: `SeverityWarning`; `NotificationRepo.UsersNotifiedWith` — получатели события по атрибуту уведомления. `NotificationRepo.CreateBatch` больше не теряет ошибку закрытия пакета, а пустой пакет ничего не отправляет.
- `postgres`: `UserCard`, `UserCards`, `ActiveAdminIDs` — email и отображаемое имя пользователей и активные администраторы без SQL по `auth_users`.
- `postgres`: `CurrencyCodes` — ISO-коды валют по ID без SQL по `cat_currencies`.
- Новый пакет `launch`: `Build`, `Option`, `WithExtensions`, `Server`, `Worker`, `Tenant`, `Seed` — `main` команд конфигурации без `internal/bootstrap`.
- `postgres/catalog_repo`: `RateSourceKey`, `RateSourcesWithOwnKey`, `MoveRateSourceKeyToPool` — перенос собственного ключа источника курсов в пул интеграций без SQL по `cat_rate_sources`.
- `postgres/catalog_repo`: `RenameAccountKey` — переименование сохранённого ключа аккаунта интеграции без выдачи значения конфигурации.
- `integrations`: `CredentialField.Optional` — ключ, который аккаунт может не указывать (например, токен сети, не входящей в подписку); аккаунту по-прежнему нужен хотя бы один ключ.
- `sdktest` (только для тестов): `Migrate`, `OpenPool`, `NewTxManager`, `TxManager`, `TxOptions`, `WithPool`, а с тегом `integration` — `NewTenantWithMigrations`, `Tenant.Context`, `NewExchangeRateDatabase`. Менеджер транзакций над сырым пулом, `ManagedPool` и `RunAll` в рабочие пакеты не вынесены.

## 3.0.0 — 2026-11-22

Первая версия фасада. Прежние `pkg/extension.ExtensionAPIVersion` и `internal/platform.ExtensionAPIVersion` (2.0.0) содержали только номер и нигде не проверялись; они удалены.

- `pkg/sdk`: `APIVersion`, `Compatible`.
- `entity`, `id`, `types`, `apperror`: модель справочников, документов и движений регистров, идентификаторы, `Quantity`, `MinorUnits`, ошибки.
- `registry`: `Catalog[T]`, `Document[T]`, `Registry`, `Presentation`, `MovementSourceProvider`.
- `posting`: `Module`, `Engine`, `Postable`, `MovementSet`, `RegisterVisitor`, `RegisterRecorder`, `PostingValidator`, `NewEngine` (для тестов).
- `registers`: `BaseAccumulationRepo`, `NewBaseAccumulationRepo`.
- `configuration`: `Configuration` (с новым методом `APIVersion`), `Server`, `Worker`, `ServerEnv`, `WorkerEnv`, `SeedEnv`, `TenantEnv`, `TenantWorker`, `OutboxRoute`, `Component`, `Cleanup`, `EmbeddedMigrations`.
- `integrations`: `Catalog`, `PoolConsumer`. `contracts`: `Bundle`.
- `sdktest`: `Check`, `NewTenant` (тег `integration`).
