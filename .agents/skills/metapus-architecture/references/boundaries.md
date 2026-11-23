# Границы и как их не нарушать

Содержание:
1. Слои
2. Платформа и конфигурация
3. Публичный API `pkg/sdk`
4. Хранилище и команды
5. Миграции трёх историй
6. Фронтенд
7. Если тест упал

Все тесты лежат в `internal/architecture/`. Запуск: `go test -short ./internal/architecture/...`. Тесты читают только импорты и строковые литералы, поэтому падают сразу, без базы.

---

## 1. Слои

**Правило.** `core` ← `domain` ← `application` ← `infrastructure`. Стрелка — «может импортировать». `metadata` не импортирует infrastructure. Внутри `internal/configuration/<имя>/` те же слои; домен конфигурации не импортирует ни платформенные, ни свои `application/` и `infrastructure/`.

**Сквозь фасад.** `sdkFronts` строит карту «пакет `pkg/sdk` → какие `internal/` пакеты он фронтирует (транзитивно)». Поэтому:

| Пакет фасада | Считается слоем |
|---|---|
| `pkg/sdk/entity`, `id`, `types`, `apperror`, `tx`, `tenant`, `appctx` | core |
| `pkg/sdk/domain`, `posting`, `audit`, `filter`, `cursor`, `eventlog`, `integrations` | domain |
| `pkg/sdk/command`, `integrations/integrationapp` | application |
| `pkg/sdk/postgres/...`, `http/...`, `tenantdb`, `registers`, `migration` | infrastructure |
| `pkg/sdk/registry`, `configuration`, `contracts` | infrastructure — поэтому регистрация сущностей и сборка конфигурации живут в `content/`, а не в `domain/` |

Точную принадлежность смотри по импортам файла фасада — тест делает так же: `grep '"github.com/alex-bogatiuk/metapus/internal' pkg/sdk/<пакет>/*.go`.

**Типичное нарушение.** Доменный сервис конфигурации импортирует `pkg/sdk/postgres/catalog_repo`, чтобы вызвать репозиторий напрямую.

**Исправление.** В домене — интерфейс порта (`type Repository interface { … }` рядом с сервисом, как `crypto/domain/catalogs/merchant/service.go`). Реализация — в `infrastructure/storage/postgres/<…>_repo` конфигурации. Склейка — в `content/`.

**Транзитивное правило.** `TestInnerPackagesDoNotTransitivelyDependOnAdapters` идёт по всему графу импортов от `core`/`domain`/`application` (и слоёв конфигураций) и падает, если на пути встречается gin, pgx, squirrel, jwt, excelize, godocx или go-qrcode. Сообщение показывает цепочку: найди в ней первое звено, где внутренний пакет взял внешний, и разорви его портом.

## 2. Платформа и конфигурация

**Что такое платформа.** Всё в `internal/` и `pkg/`, кроме `internal/configuration/<имя>/` для имён из `configurationPackages`. Сюда же относятся `internal/configuration` (контракт), `internal/configuration/none`, `internal/configuration/configurationtest`, `internal/platformcontent`.

**Правила.**
- `TestPlatformDoesNotDependOnConfiguration` — платформа не импортирует конфигурацию. `knownConfigurationLeaks` пуст (было 105 рёбер).
- `TestConfigurationsDoNotDependOnEachOther` — крипто не импортирует «Автозапчасти» и наоборот, включая тесты.
- `TestPlatformTestsDoNotImportConfiguration` — тест платформы тоже не берёт конфигурацию. Проверка собранного приложения (все контракты, сгенерированные файлы) живёт в `internal/configuration/<имя>/content`, проверка служебных маршрутов платформы — в `internal/configuration/none`.

**Типичное нарушение.** Платформенному сервису нужно знать, что пул интеграций используется справочником крипто, и он импортирует крипто-репозиторий.

**Исправление — инверсия.** Платформа объявляет интерфейс (`integrations.PoolConsumer`), конфигурация реализует (`crypto_integration_repo.RPCPoolConsumer`) и передаёт через `Configuration.PoolConsumers()`. Общий рецепт:
1. Интерфейс — в платформенном домене или контракте (`internal/domain/...`, `internal/platform`, `internal/configuration`).
2. Метод или поле в `Configuration`, `configuration.Server`/`Worker`, `RouterConfig` или реестре, через которое конфигурация его отдаёт.
3. Алиас в `pkg/sdk`, если интерфейс должна видеть конфигурация.
4. Реализация — в конфигурации; регистрация — в её `content/`.

Готовые точки расширения перечислены в `docs/systems/extension-api.md`, раздел 4 (бэкенд и фронтенд). Сначала ищи там.

## 3. Публичный API `pkg/sdk`

**Правила.**
- `TestSDKConfigurationsUseOnlyThePublicAPI` — конфигурации из `sdkConfigurations` (`autoparts`, `crypto`) импортируют из `metapus/...` только свой корень и `github.com/alex-bogatiuk/metapus/pkg/sdk/...`. Тесты тоже.
- `TestSDKIsAFacade` — в `pkg/sdk` только `type X = internal.X`, обёртки-функции и ограничения дженериков. Свой тип — только `sdktest.Tenant` (список `sdkOwnTypes`).
- `TestSDKTestStaysInTests` — `pkg/sdk/sdktest` (фикстуры строят `TxManager` над сырым пулом и вручную привязывают тенанта) нельзя импортировать в рабочем коде конфигурации, кроме пакетов с именем `*test`.

**Два слоя фасада.**
- Ядро (с 3.0.0): `entity`, `id`, `types`, `apperror`, `registry`, `posting`, `registers`, `configuration`, `integrations`, `contracts`, `sdktest`. Его хватает «Автозапчастям».
- Расширенный слой (с 3.1.0): пакеты один к одному повторяют внутренние, **с тем же именем пакета** — перевод на фасад сводится к замене пути импорта. Наружу вынесены только символы, которые кто-то использует.

**Как добавить то, чего не хватает.**
1. Подумай, не собирает ли конфигурация механизм из деталей. Если да — сделай в платформе одну функцию (как `middleware.BindTenant`, `RouteContext.ProtectedGroup`) и выставь её, а детали не выставляй. Это сужает фасад и делает конфигурацию проще.
2. Иначе — алиас типа или тонкая обёртка функции в пакете фасада с тем же именем, что внутренний. Комментарий — что это и когда этим пользоваться.
3. Запись в `docs/sdk/CHANGELOG.md`.
4. Версия `sdk.APIVersion` по semver: добавление — минорная, удаление или несовместимое изменение — мажорная (сначала `// Deprecated:` на минорную версию). Конфигурация, которой нужно новое, поднимает свой `APIVersion()`. Пока версия не выпущена тегом `sdk/vX.Y.Z`, изменения складываются в неё же.

**Чего не делать.** Импортировать `internal/` «пока не добавлю в SDK». Объявлять в фасаде свой тип-обёртку со своим поведением. Выставлять сырой пул, `ManagedPool`, конструктор `TxManager` в рабочих пакетах фасада.

## 4. Хранилище и команды

- `TestPlatformCodeDoesNotQueryConfigurationTables`, `TestPlatformMigrationsDoNotNameConfigurationTables` — строковые литералы платформенного кода и платформенные миграции не называют таблиц, которые создают `db/configuration/*`. SQL по таблицам конфигурации — в её `infrastructure/storage/postgres`.
- `TestConfigurationsDoNotQueryIntegrationTables` — обратное направление для интеграций: рабочий код конфигурации не называет таблиц `cat_integration_*`/`sys_integration_*` платформы. Нужен провайдер пула, пулы аккаунта или годные аккаунты провайдера — функция `pkg/sdk/postgres/catalog_repo`, а запрос по своим таблицам фильтрует `= ANY($n)` по полученным ID. Если нужна блокировка аккаунта на время расхода (как допуск платного AML-запроса), — `LockUsableAccount` в той же транзакции до блокировки своих строк. Миграции конфигурации на эти таблицы ссылаться могут (FK, свои CHECK и триггеры).
- `TestConfigurationSeedsUseThePlatformDemoSeed` — то же для демо-данных: сид конфигурации заводит валюты, привязки курсов и уведомления через `pkg/sdk/demoseed`, а не `INSERT` в `sys_automation_*` и `reg_rate_source_mappings`. Секреты бота и ключ шифрования остаются у платформы.
- `TestConfigurationsUseThePlatformOutbox` — сообщения outbox ставит платформа: `OutboxPublisher.Publish` для события домена, `postgres.EnqueueOutbox(ctx, OutboxRecord{…})`, когда payload и causation уже зафиксированы (в той же транзакции, что и своё состояние). Повторная постановка потерянных сообщений исключает ещё ожидающие через `PendingOutboxPayloadValues` и `<> ALL($n)`.
- `TestConfigurationsUseThePlatformEventLogAndNotifications` — аудит пишет `postgres.NewEventLogRepo().Write(ctx, eventlog.Event{…})` в транзакции операции. Платформа сама добавляет trace и request ID, а пользователя берёт из контекста, если `UserID` пуст. Уведомления создаёт `postgres.NewNotificationRepo().CreateBatch`. Получателей строит конфигурация из своих таблиц, а уже уведомлённых по атрибуту события исключает `UsersNotifiedWith`.
- `TestConfigurationsReadPlatformUsersThroughThePlatform` — вместо `LEFT JOIN auth_users` конфигурация выбирает свои строки с ID пользователя, закрывает `rows`, затем вызывает `postgres.UserCards(ctx, ids)` и подставляет `Email`/`DisplayName`. Пропавший или помеченный на удаление пользователь карточки не имеет, как при старом `LEFT JOIN … deletion_mark = FALSE`. Сортировать или фильтровать по имени пользователя в SQL конфигурации нельзя; если это понадобится, нужен метод платформы.
- `TestConfigurationsReadCurrenciesThroughThePlatform` — вместо `LEFT JOIN cat_currencies` конфигурация выбирает свой `currency_id`, закрывает `rows` и подставляет коды из `postgres.CurrencyCodes(ctx, ids)`. Валюта с пометкой удаления код сохраняет, у пропавшей кода нет (пустая строка, как при старом `COALESCE(iso_code, '')`).
- `TestConfigurationsUseThePlatformRateSources` — источники курсов меняет платформа. `MoveRateSourceKeyToPool` проверяет пул по правилам формы справочника (только CoinGecko и только активный пул CoinGecko Demo), берёт блокировку конфигурации интеграций и возвращает Validation или NotFound, а не молча ничего не меняет.
- `TestCryptoWorkerRuntimeDoesNotExecuteSQL` — пакет `crypto/infrastructure/crypto_worker` не импортирует `database/sql` и pgx: воркер держит таймеры, остановку и клиентов, а SQL — в репозиториях, реализующих порты `application/`.
- `configuration_commands_test.go` — каждая программа в `cmd/` есть в `configurationCommands` или `platformCommands` (новая команда без классификации — ошибка). Платформенные команды и их тесты не импортируют конфигурацию и не называют её таблиц. Команда конфигурации — однострочный `main`, вызывающий `launch.Server|Worker|Tenant|Seed(конфигурация, опции…)` из `pkg/sdk/launch`. `TestConfigurationCommandsUseThePublicLauncher` разрешает командам конфигураций из платформы только `pkg/sdk` и `extensions`; утилиты крипто, которые пока сами открывают пулы и секреты, перечислены в `knownConfigurationCommandPlatformImports` — из списка только удаляют.
- `TestEncodingJSONV1IsIsolatedToCompatibilityFacades` — JSON только через `internal/core/json` (`pkg/sdk/json`).
- `TestExtensionMigrationsAreEmbedded` — SQL расширения лежит в `db/extensions/<модуль>/`, иначе его не встроят в образ.

## 5. Миграции трёх историй

| Источник | Каталог | История goose | Нумерация |
|---|---|---|---|
| Платформа | `db/migrations/` | `goose_db_version_platform` | `00001…` |
| Конфигурация | `db/configuration/<имя>/` | `goose_db_version_<имя>` | своя, ниже 10000 |
| Расширения | `db/extensions/<модуль>/` | `goose_db_version_extensions` | 10000+, общая |

- Порядок применения: платформа → конфигурация → расширения (`configuration.MigrationOptions`).
- Тенант принадлежит одной конфигурации навсегда (`tenants.configuration` в meta-базе).
- Конфигурация может ссылаться на таблицы платформы (FK, CHECK своих правил); платформа на таблицы конфигурации — никогда.
- Конфигурация отдаёт миграции через `Migrations()` (`configuration.EmbeddedMigrations(Name)`), формат и версии проверяет `sdktest.Check`.
- Платформенные миграции не правятся: их уже получили тенанты всех конфигураций. Изменение платформенной схемы — новая миграция в `db/migrations/`.
- Миграции конфигурации и расширений до первого production-релиза: итоговая схема — в миграции, владеющей таблицей, без переходных ALTER/backfill; проверка на свежей базе (`Up → Down → Up`). После релиза — только новые forward-only миграции в любой истории.
- Строгий DDL (`IF NOT EXISTS` только для PostgreSQL extensions), `TIMESTAMPTZ`, у изменяемых таблиц `version`, `_txid`, `_deleted_at` и CDC-триггеры; UUID по умолчанию — `gen_random_uuid_v7()`.

Полные правила — `db/migrations/README.md`.

## 6. Фронтенд

- Платформенный UI: `frontend/{components,lib,hooks,stores,types}`. Конфигурация: `frontend/configuration/<имя>/` с той же структурой и `pages/` по структуре `app/`.
- `frontend/app/` генерируется под одну конфигурацию (`npm run configure -- <имя>`); сгенерированные обёртки не коммитятся.
- Платформа не импортирует `configuration/` и не использует её словарь (crypto, merchant, portal, wallet…). Конфигурация подключается через реестры: `registerEntityOverlays`, `registerNavigation`, `registerWidget`, `registerSettingsPage`, `registerSecurityDimension` и другие (таблица в `docs/systems/extension-api.md`).
- Типы TypeScript генерируются из Go-контрактов (`go run ./cmd/http-contracts`); руками не правятся.

## 7. Если тест упал

1. Прочитай сообщение: оно называет файл, импорт и (для транзитивного правила) всю цепочку.
2. Найди, какой слой или какая сторона границы владеет кодом, по разделу 2 SKILL.md.
3. Перенеси код к владельцу или разверни зависимость портом / точкой расширения.
4. Не добавляй запись в списки `known…Leaks`, `sdkOwnTypes`, `allowedDirectories` и не переименовывай пакет в `*test`, чтобы обойти проверку. Если кажется, что исключение оправдано, — объясни пользователю и спроси.
5. Перезапусти `go test -short ./internal/architecture/...` и сборку.
