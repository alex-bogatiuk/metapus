# CHANGELOG публичного API (`pkg/sdk`)

Каждое изменение `pkg/sdk` записывается здесь вместе с новой `sdk.APIVersion` ([правила](README.md#версионирование)).

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
