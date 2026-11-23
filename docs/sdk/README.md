# Публичный API платформы (`pkg/sdk`)

> Фасад, на котором строится конфигурация или клиентское расширение. Реализация платформы остаётся в `internal/`, конфигурация видит только то, что вынесено сюда.

## Состав

Фасад состоит из двух слоёв. **Ядро** — модель и объявление сущностей; его хватает простой конфигурации вроде «Автозапчастей». **Расширенный слой** — сервисы, HTTP, хранилище, интеграции и процессы; он нужен конфигурации с собственными маршрутами, воркерами и провайдерами, как у крипто.

### Ядро

| Пакет | Что даёт |
|---|---|
| `pkg/sdk` | `APIVersion` и `Compatible` — версия API и проверка совместимости |
| `pkg/sdk/entity` | модель: `Catalog`, `Document`, `Definition`, `Part`, правила кодов и номеров, иерархия; движения регистров (`MovementBase`, `MovementProvider`, `DocumentMovement`, колонки) |
| `pkg/sdk/id`, `types`, `apperror` | UUIDv7, `Quantity` (4 знака) и `MinorUnits` (деньги в минимальных единицах валюты документа), ошибки 400/403/404/409 |
| `pkg/sdk/registry` | объявление справочников и документов: `registry.Catalog[T]`, `registry.Document[T]`, `Presentation`, `MovementSourceProvider` |
| `pkg/sdk/posting` | регистры в проведении: `Module`, `RegisterVisitor`, `RegisterRecorder`, `PostingValidator`, `MovementSet` |
| `pkg/sdk/registers` | хранение движений в PostgreSQL: `BaseAccumulationRepo` |
| `pkg/sdk/configuration` | контракт `Configuration` и части процессов: `Server`, `Worker`, `SeedEnv`, маршруты outbox, компоненты, очистки |
| `pkg/sdk/integrations`, `contracts` | каталог интеграций, потребители пулов, HTTP-контракты собственных маршрутов |
| `pkg/sdk/sdktest` | только для тестов. `Check` — контрактная проверка конфигурации; фикстуры своей базы: `Migrate`, `OpenPool`, `NewTxManager`, `WithPool`; с тегом `integration`: `NewTenant` — свежий тенант с её миграциями, `NewTenantWithMigrations` — то же для тестов внутри пакетов конфигурации, `NewExchangeRateDatabase` — пустая база кластера курсов |

### Расширенный слой

Пакеты повторяют внутренние пакеты платформы один к одному, с тем же именем пакета: перевод конфигурации на фасад сводится к замене путей импорта. Наружу вынесены только символы, которые конфигурации используют, а не весь внутренний пакет.

| Группа | Пакеты `pkg/sdk/...` | Что фронтируют в `internal/` |
|---|---|---|
| Ядро запроса | `appctx`, `tenant`, `tx`, `tenantdb`, `json`, `logger`, `secretbox`, `numerator`, `workerjob` | `core/context`, `core/tenant`, `core/tx`, `infrastructure/tenantdb`, `core/json`, `pkg/logger`, `infrastructure/secretbox`, `core/numerator`, `core/workerjob` |
| Безопасность | `security`, `security_profile`, `readaccess`, `readpolicy` | политики RLS, CEL и полей, авторизованное чтение |
| Сервисы | `domain`, `audit`, `cursor`, `filter`, `eventlog`, `notifications`, `settings`, `command` | базовые сервисы справочников и документов, хуки, удаление, ссылки, события, outbox, идемпотентные команды |
| Регистры и отчёты | `registers/exchange_rate`, `reports/schema`, `reports/contract`, `reports/compiler`, `printing`, `printing/render` | регистр курсов, наборы данных отчётов, печатные формы |
| Интеграции | `integrations`, `integrations/transport`, `integrations/profiles`, `integrations/providerpolicy`, `integrations/integrationapp`, `urlsafe` | аккаунты, пулы и провайдеры, транспорт, профили, допуск вызовов, проверка публичных URL |
| HTTP | `http/v1`, `http/handlers`, `http/middleware`, `http/dto`, `http/httpvalue`, `http/jsoncodec`, `contracts` | регистрация маршрутов, базовые обработчики, middleware прав, step-up и идемпотентности, контракты |
| Демо-данные | `demoseed` | `infrastructure/demoseed` — валюты, привязки к источникам курсов, уведомление в демо-чат Telegram |
| Хранилище | `postgres`, `postgres/catalog_repo`, `postgres/document_repo`, `postgres/integration_repo`, `postgres/filter`, `postgres/keyset`, `postgres/numerator`, `registers`, `migration` | менеджер транзакций, базовые репозитории, блокировки документов, outbox, миграции и версии схемы |
| Метаданные и процесс | `metadata`, `telemetry`, `envconfig`, `secretfile` | описание сущностей для UI, трассировка, окружение, секреты из файлов |

Расширенный слой широк (всего в фасаде 62 пакета и около 570 объявлений) и повторяет устройство платформы. Поэтому он стабилен слабее ядра: изменение внутреннего типа видно через алиас. Его сужают так: то, что конфигурация собирала из платформенных деталей сама, платформа делает одной функцией, а детали уходят из фасада.

- **Привязка тенанта.** Маршрут, который сам находит тенанта (по ключу API, по публичной ссылке), вызывает `middleware.BindTenant(ctx, manager, tenantID)`: пул, менеджер транзакций и тенант попадают в контекст так же, как в `TenantDB`. Сырого пула, `ManagedPool` и менеджера транзакций над пулом в рабочих пакетах фасада нет.
- **Группы маршрутов.** Группа под своим префиксом с цепочкой платформы — `RouteContext.ProtectedGroup(path)` (как `Protected`) или `RouteContext.Authenticated(path)` (тенант, аутентификация и admin guard без профиля безопасности, для маршрутов со своей политикой). Отдельные `TenantDB`, `Auth`, `AdminGuard`, `SecurityContext` конфигурации не нужны.
- **Ссылки.** `ReferenceResolver.Resolve(ctx)` читает имена через авторизованный reader запроса; пул ему не передаётся.
- **Тестовые фикстуры.** Своя база для теста — `sdktest.Migrate`, `sdktest.OpenPool`, `sdktest.NewTxManager`, `sdktest.WithPool`. Рабочий код конфигурации `sdktest` не импортирует (`TestSDKTestStaysInTests`; исключение — пакеты тестовой поддержки с именем `*test`).
- **Проверки платформы — в платформе.** Сверка служебных маршрутов платформы (автоматизация, аутентификация, списки) с контрактами живёт в `internal/configuration/none`, конфигурация проверяет только свои.

- **Ключ шифрования — у платформы.** Ключ учётных данных (`AUTOMATION_ENCRYPTION_KEY`) читает только `secretbox`. Конфигурация шифрует учётные данные своих провайдеров `secretbox.Seal`/`Open` и проверяет готовность ключа `secretbox.Ready`. Расшифрованные учётные данные аккаунта интеграции она получает `catalog_repo.AccountCredentials(ctx, id)`, а имена ключей для проверок пула — `PoolAccountKeys(ctx, poolID)`, `AccountKeyNames(ctx, ids)` и `AccountKeysOf(account)`. Ни ключа, ни шифротекста учётных данных интеграций в фасаде нет. Рабочий код конфигурации не называет секрет ключа (`TestConfigurationsDoNotHoldTheEncryptionKey`). Блокировка `LockIntegrationConfiguration` остаётся в фасаде: это общий контракт всех потребителей пулов (`integrations.PoolConsumer`).
- **Таблицы интеграций — у платформы.** Рабочий код конфигурации не называет `cat_integration_*` и `sys_integration_*` (`TestConfigurationsDoNotQueryIntegrationTables`). Что ей нужно о пулах и аккаунтах, она спрашивает у `catalog_repo`: провайдер пула (`PoolProvider`), пулы аккаунта и пулы с общими аккаунтами (`AccountPoolIDs`, `PoolsSharingAccounts`), провайдер аккаунта и аккаунты провайдера (`AccountProvider`, `ProviderAccountIDs`, `UsableAccountIDs`), годность аккаунта с блокировкой строки или без (`LockUsableAccount`, `RequireUsableAccount`), аккаунт без ключей для провайдера, которого она обслуживает сама (`CreateKeylessAccount`). Свои таблицы она соединяет с полученными ID (`= ANY($n)`). Миграции конфигурации по-прежнему ссылаются на эти таблицы (внешние ключи, свои CHECK и триггеры), тесты — заполняют их фикстурами.
- **Демо-данные платформы — у платформы.** Сид конфигурации заводит валюты `demoseed.EnsureCurrency`, привязывает их к источнику курсов `demoseed.MapRate`, а уведомление о своём событии в демо-чат Telegram отдаёт `demoseed.TelegramNotification` (правило: событие, сущности, условие CEL, шаблон). Токен бота, чат и ключ шифрования читает платформа; без них уведомление не заводится. Рабочий код конфигурации не называет `sys_automation_*` и `reg_rate_source_mappings` (`TestConfigurationSeedsUseThePlatformDemoSeed`).
- **Outbox — у платформы.** Конфигурация ставит сообщения в outbox через `postgres.NewOutboxPublisher().Publish` (событие домена) или `postgres.EnqueueOutbox` (готовая запись `OutboxRecord`: свой payload, время, trace-контекст и causation, например доставка неизменяемого события вебхука). Восстановление потерянных сообщений спрашивает `postgres.PendingOutboxPayloadValues`, какие ещё в пути. Рабочий код конфигурации не называет `sys_outbox*` (`TestConfigurationsUseThePlatformOutbox`).
- **Журнал событий и уведомления — у платформы.** Запись аудита конфигурация делает через `postgres.NewEventLogRepo().Write` в своей транзакции, уведомления пользователям — через `postgres.NewNotificationRepo().CreateBatch`. Повторная доставка одного события исключает уже уведомлённых пользователей через `UsersNotifiedWith(ctx, ключ, значение)`: метод учитывает и прочитанные, и удалённые уведомления. Рабочий код конфигурации не называет `sys_event_log*` и `sys_notifications` (`TestConfigurationsUseThePlatformEventLogAndNotifications`).
- **Пользователи — у платформы.** Конфигурация хранит ID пользователей в своих строках (утвердивший, участник мерчанта), а email и имя получает через `postgres.UserCards(ctx, ids)`. `DisplayName` — это имя и фамилия, а если они пустые, то email. Пользователь с пометкой удаления карточки не имеет. Получателей-администраторов даёт `postgres.ActiveAdminIDs`. Карточки подставляются в Go после своего запроса; закройте строки запроса до вызова, если он идёт в той же транзакции. Рабочий код конфигурации не называет таблиц `auth_*` платформы (`TestConfigurationsReadPlatformUsersThroughThePlatform`).
- **Валюты — у платформы.** Конфигурация хранит ID валюты в своих строках (валюта токена), а ISO-код получает через `postgres.CurrencyCodes(ctx, ids)` после своего запроса. Код валюты с пометкой удаления по-прежнему виден: строки, которые на неё ссылаются, продолжают его показывать. Рабочий код конфигурации не называет `cat_currencies` (`TestConfigurationsReadCurrenciesThroughThePlatform`).

Типы — это алиасы платформенных, функции — тонкие обёртки. Собственных типов у фасада нет, кроме ограничений для дженериков и тестового `sdktest.Tenant`. Это проверяет `internal/architecture` (`TestSDKIsAFacade`).

Обе конфигурации — [«Автозапчасти»](../autoparts/README.md) и крипто — в коде и тестах импортируют из платформы только `pkg/sdk`. Это закреплено тестом (`TestSDKConfigurationsUseOnlyThePublicAPI`, список `sdkConfigurations`). «Автозапчастям» хватает 11 пакетов ядра, крипто использует 61 пакет фасада.

Правила слоёв (`TestInternalLayerDependenciesPointInward`, `TestInnerPackagesDoNotTransitivelyDependOnAdapters`) смотрят сквозь фасад: импорт пакета `pkg/sdk` считается импортом того, что он фронтирует. Домен конфигурации не может взять `pkg/sdk/postgres` или `pkg/sdk/http/...`, как не мог взять `internal/infrastructure/...`.

Если конфигурации не хватает чего-то из платформы, это добавляется в `pkg/sdk` как алиас или обёртка с записью в [CHANGELOG](CHANGELOG.md). Прямой импорт из `internal/` не допускается.

## Версионирование

- `sdk.APIVersion` следует semver:
  - **мажорная** версия меняется, когда что-то в `pkg/sdk` удалено или изменено несовместимо, в том числе метод `Configuration`;
  - **минорная** — когда только добавлено;
  - **патч** — когда исправлено без изменения API.
- Конфигурация объявляет, для какой версии собрана: `Configuration.APIVersion()`. Платформа принимает её, если совпадает мажорная версия, а минорная не новее платформенной (`sdk.Compatible`).
- Проверку `configuration.CheckAPIVersion` вызывают все платформенные команды при старте (`server`, `worker`, `tenant`, `seed`) и `sdktest.Check`. Несовместимая конфигурация не запустится.
- **Устаревание.** Удаляемое сначала помечается `// Deprecated:` хотя бы на одну минорную версию и записывается в CHANGELOG. Удаляется только в следующей мажорной.
- **Релиз.** Тег `sdk/vMAJOR.MINOR.PATCH` на коммите с изменённым `APIVersion` и записью в CHANGELOG. `make changelog` показывает изменения `pkg/sdk` с прошлого коммита.

## Конфигурация в отдельном репозитории

Код к этому готов, но сам перенос ещё не сделан. Порядок такой:

1. **Путь модуля платформы.** Модуль называется `metapus` — такой путь нельзя получить через `go get`. Нужен настоящий путь (например, `github.com/<организация>/metapus`). Это переименование всех импортов и тег версии. Пока его нет, репозиторий конфигурации подключает платформу через `replace metapus => ../metapus`.
2. **Репозиторий конфигурации** держит:
   - свой `go.mod` с `require` платформы;
   - код на `pkg/sdk`;
   - миграции, встроенные своим `embed`: `Migrations()` возвращает их, `sdktest.Check` проверяет формат и версии;
   - свои `cmd/<имя>-server|worker|tenant|seed` — однострочные вызовы платформенных `Main` из `internal/bootstrap`. Для внешнего модуля их надо вынести в `pkg/`: сейчас они в `internal/` и снаружи недоступны.
3. **Обновление платформы:** поднять версию в `go.mod` → собрать (несовместимый `APIVersion` остановит запуск) → `tenant migrate`: сначала миграции платформы, затем конфигурации, у каждой своя история → перегенерировать контракты.
4. **Фронтенд — самая трудная часть.**
   - Платформенный UI (`components`, `lib`, `hooks`, `stores`, `types`) нужно выпустить npm-пакетом, а `app/` и `configuration/<имя>` останутся у конфигурации.
   - Генератор контрактов (`cmd/http-contracts`) тоже должен стать доступен снаружи, чтобы конфигурация генерировала свои файлы поверх файлов из пакета.
   - Пока фронтенд конфигураций живёт в общем репозитории, и `npm run configure -- <имя>` собирает его под одну из них.
