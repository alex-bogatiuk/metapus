# CHANGELOG публичного API (`pkg/sdk`)

Каждое изменение `pkg/sdk` записывается здесь вместе с новой `sdk.APIVersion` ([правила](README.md#версионирование)).

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
