---
name: metapus-architecture
description: >
  Архитектурные правила Metapus и рекомендуемые паттерны проектирования: слои
  core → domain → application → infrastructure, граница «платформа /
  базовая конфигурация / надстройка / расширение», публичный API pkg/sdk,
  database-per-tenant, транзакции через context, проведение и регистры,
  хуки и outbox, миграции трёх историй. Используй этот скилл всякий раз,
  когда пишешь, переносишь или ревьюишь Go-код в internal/, pkg/sdk,
  configurations/, extensions/ или cmd/ проекта Metapus: новый справочник,
  документ, регистр, маршрут, воркер, интеграцию, миграцию, код крипто,
  «Торговли и сервиса» или надстройки «Автозапчасти»,
  а также когда решаешь, в какой пакет положить код, что можно импортировать,
  почему упал тест internal/architecture, как расширить платформу для
  конфигурации, или когда проектируешь новую фичу или конфигурацию, даже если
  слово «архитектура» не прозвучало.
---

# Архитектура Metapus

Metapus — мультитенантная ERP-платформа (Go 1.27, gin, pgx, squirrel, goose; фронтенд Next.js). Приложение — **три слоя** (`docs/systems/configuration-layers.md`):

1. **Платформа** — механизмы без бизнес-логики: формы и списки по метаданным, проведение, регистры, права, отчёты (как 1С:Предприятие с БСП). `internal/`, публичный API — `pkg/sdk`.
2. **Базовая конфигурация** — самодостаточная бизнес-логика, которой хватает для автоматизации бизнеса (как типовая УНФ): «Торговля и сервис» (`configurations/trade`) и крипто (`internal/configuration/crypto`).
3. **Надстройка** (`configuration.AddOn`) — необязательная отраслевая специфика над **одной конкретной** базой: «Автозапчасти» (`configurations/autoparts`) над `trade`. У тенанта не больше одной надстройки; две отрасли сразу — это одна надстройка, которая их объединяет.

Клиентские доработки конкретного внедрения — **расширения** (`extensions/`).

Главная идея, из которой следует почти всё остальное: **зависимости направлены внутрь и вниз по слоям, никогда обратно**: надстройка → база → платформа. Платформа даёт точки расширения, база их заполняет и объявляет свои точки для надстройки. Поэтому базу и надстройку можно вынести в свои репозитории, а платформу и базу — обновлять под всеми, кто на них построен.

Большинство правил закреплены тестами в `internal/architecture/`. Это значит: нарушение не «плохой стиль», а красная сборка. Списки исключений в этих тестах пусты и должны такими остаться — исправляй код, а не список.

## 1. Карта системы

```
cmd/<конфиг>-server|worker|tenant|seed   тонкие main: launch.Server|Worker|Tenant|Seed из pkg/sdk/launch
internal/bootstrap/                      Composition Root платформы (сборка, запуск, shutdown)
internal/infrastructure/                 адаптеры: http/v1 (gin), storage/postgres, tenantdb, worker
internal/application/                    сценарии и их порты (command, automation, integrationapp…)
internal/domain/                         бизнес-правила: сервисы, хуки, posting, интерфейсы репозиториев
internal/metadata/                       описание сущностей для UI
internal/core/                           фундамент: entity, id, apperror, tx, tenant, types
internal/platform/, internal/configuration/*.go   контракты фабрик и самой конфигурации; stack.go — база и надстройка
internal/platformcontent/                общие сущности платформы (валюты, контрагенты, интеграции, курсы)
internal/configuration/none              пустая конфигурация: «только платформа»
internal/configuration/crypto/           базовая конфигурация крипто (на ней надстроек нет, живёт в internal/)
configurations/trade/                    базовая «Торговля и сервис»: domain/ application/ infrastructure/ content/
configurations/autoparts/                надстройка «Автозапчасти» над trade
pkg/sdk/                                 публичный API платформы — фасад из алиасов и обёрток
extensions/                              клиентские модули, регистрируются в extensions/register.go
db/migrations | db/configuration/<база> | db/configuration/<надстройка> | db/extensions/<модуль>   истории миграций
db/meta/                                 метабаза: тенанты, их база (configuration) и надстройка (addon)
frontend/{components,lib,hooks,stores,types}   платформа; frontend/configuration/<имя>/ — база или надстройка (manifest "base")
```

Слои внутри конфигурации те же, что у платформы, и правило направления действует внутри её корня. База, на которой строятся надстройки, живёт в `configurations/`, а не в `internal/`: Go не пустит надстройку из другого репозитория в `internal/`.

## 2. Куда положить код

Отвечай на вопросы по порядку — первый «да» решает.

1. **Это бизнес-логика, а не механизм?** Реши, какого слоя:
   - типовая для любого бизнеса этого класса (склад, номенклатура, заказы, цены, себестоимость; merchant, wallet, sweep для крипто) → в базовую конфигурацию (`configurations/trade/`, `internal/configuration/crypto/`);
   - специфика одной отрасли поверх базы (артикулы и аналоги автозапчастей, заказ-наряд СТО) → в надстройку (`configurations/autoparts/`). Надстройка добавляет объекты, реквизиты и движения и меняет поведение базы **только в точках, которые база объявила**; если точки нет — сначала добавь её в базу. Платформенных механизмов «реквизит надстройки к объекту базы» и «движения надстройки к документу базы» пока нет (этап B): своими объектами надстройка пользуется как любая конфигурация, а для остального сначала нужен механизм в платформе.
   Платформа ни о той, ни о другой не узнаёт; база не знает о надстройке.
2. **Это общий механизм, который конфигурация сейчас собирает из платформенных деталей сама?** → в платформу одной функцией или точкой расширения, а наружу — через `pkg/sdk` (см. паттерн «Фасад и сужение»). Пример: `middleware.BindTenant` вместо ручной привязки пула в крипто.
3. **Это модель, инвариант, правило?** → `domain/` (своего корня). Чистые функции, интерфейсы портов, никаких gin/pgx.
4. **Это сценарий, склеивающий несколько доменных сервисов и внешние порты** (RPC, подписание, доставка)? → `application/`.
5. **Это SQL, HTTP, клиент внешнего API, таймер воркера?** → `infrastructure/`, как реализация порта из domain/application.
6. **Это сборка зависимостей?** → `content/` конфигурации (её Composition Root) или `internal/bootstrap` для платформы. В `cmd/` — только однострочный main: `cmd/<база>-*` запускают базу одну, `cmd/<надстройка>-*` — базу с надстройкой (`launch.Server(content.AutoParts())`).
7. **Клиентская доработка у конкретного внедрения?** → модуль в `extensions/` (`go run ./cmd/scaffold`), миграции в `db/extensions/<модуль>/` с версией 10000+.

Перед тем как создавать новое — найди ближайший аналог (обычно в `configurations/trade` для простого случая и в `internal/configuration/crypto` для сложного) и `docs/ROUTER.md` для документации по подсистеме. Расширяй существующую абстракцию, если она подходит.

## 3. Правила, которые проверяют тесты

Подробности, типичные нарушения и правильные исправления — в `references/boundaries.md`.

| Правило | Тест |
|---|---|
| `core` не импортирует domain/application/infrastructure и pgx; `domain` — application/infrastructure; `application` — infrastructure. То же внутри корня конфигурации | `TestInternalLayerDependenciesPointInward` |
| Внутренние слои не тянут gin, pgx, squirrel, jwt, excelize, qrcode даже транзитивно | `TestInnerPackagesDoNotTransitivelyDependOnAdapters` |
| Оба правила смотрят **сквозь** `pkg/sdk`: `pkg/sdk/postgres` в домене — то же, что `internal/infrastructure/...` | там же (`sdkFronts`) |
| Платформа (`internal/`, `pkg/` вне корней конфигураций) не импортирует конфигурацию | `TestPlatformDoesNotDependOnConfiguration` |
| Конфигурации не импортируют друг друга; **исключение — надстройка импортирует свою базу**. База не импортирует надстройку. Корни и базы конфигураций — в `repositoryConfigurations` (`internal/architecture/configurations_test.go`) | `TestConfigurationsDoNotDependOnEachOther`, `TestRepositoryConfigurationsAreBasesAndAddOns` |
| Конфигурация импортирует из платформы **только** `github.com/alex-bogatiuk/metapus/pkg/sdk/...` (надстройка — ещё пакеты своей базы) — и в коде, и в тестах | `TestSDKConfigurationsUseOnlyThePublicAPI` |
| Надстройка строится на базе, а не на надстройке; база объявляет версию (`configuration.Versioned`), совместимую с `BaseVersion` надстройки; платформа обслуживает `APIVersion` обоих слоёв | `configuration.CheckStack` на старте каждого процесса |
| `pkg/sdk` — только алиасы и тонкие обёртки, своих типов нет | `TestSDKIsAFacade` |
| `pkg/sdk/sdktest` — только в `_test.go` и пакетах `*test` | `TestSDKTestStaysInTests` |
| Рабочий код конфигурации не называет `AUTOMATION_ENCRYPTION_KEY`: учётные данные шифрует `pkg/sdk/secretbox`, аккаунты интеграций открывает `catalog_repo.AccountCredentials` | `TestConfigurationsDoNotHoldTheEncryptionKey` |
| Рабочий код конфигурации не называет таблиц интеграций платформы (`cat_integration_*`, `sys_integration_*`): провайдеры, пулы и годные аккаунты даёт `catalog_repo`, свои таблицы соединяются с ID через `= ANY($n)` | `TestConfigurationsDoNotQueryIntegrationTables` |
| Сид конфигурации не пишет в `sys_automation_*` и `reg_rate_source_mappings`: валюты, курсы и уведомления в Telegram заводит `pkg/sdk/demoseed` | `TestConfigurationSeedsUseThePlatformDemoSeed` |
| Команды конфигурации (`cmd/<конфиг>-*`) берут из платформы только `pkg/sdk` и `extensions`: `main` — `launch.Server/Worker/Tenant/Seed` | `TestConfigurationCommandsUseThePublicLauncher` |
| Конфигурация не пишет в `sys_outbox` сама: `OutboxPublisher.Publish` или `postgres.EnqueueOutbox`, ожидающие сообщения — `PendingOutboxPayloadValues` | `TestConfigurationsUseThePlatformOutbox` |
| Конфигурация не пишет в `sys_event_log` и `sys_notifications`: аудит — `NewEventLogRepo().Write`, уведомления — `NewNotificationRepo().CreateBatch`, уже уведомлённые — `UsersNotifiedWith` | `TestConfigurationsUseThePlatformEventLogAndNotifications` |
| Конфигурация не читает `auth_*` платформы: email и имя — `postgres.UserCards(ctx, ids)` после своего запроса, администраторы — `postgres.ActiveAdminIDs` | `TestConfigurationsReadPlatformUsersThroughThePlatform` |
| Конфигурация не читает `cat_currencies`: ISO-коды — `postgres.CurrencyCodes(ctx, ids)` после своего запроса | `TestConfigurationsReadCurrenciesThroughThePlatform` |
| Конфигурация не трогает `cat_rate_sources`: источники с собственным ключом — `catalog_repo.RateSourcesWithOwnKey`, перенос ключа в пул — `catalog_repo.MoveRateSourceKeyToPool` | `TestConfigurationsUseThePlatformRateSources` |
| Код и миграции платформы не называют таблиц конфигурации; тесты платформы не импортируют конфигурацию | `configuration_storage_test.go` |
| Каждая команда `cmd/` отнесена к платформе или конфигурации; платформенные не знают конфигураций | `configuration_commands_test.go` |
| `encoding/json` — только в фасаде `internal/core/json` | `TestEncodingJSONV1IsIsolatedToCompatibilityFacades` |
| Рантайм крипто-воркера не выполняет SQL | `crypto_storage_test.go` |
| SQL расширений — в `db/extensions/`, не в `extensions/*/migrations` | `TestExtensionMigrationsAreEmbedded` |
| Платформенный фронтенд не импортирует `configuration/` и не использует её словарь | `frontend/tests/unit/configuration-boundary.spec.ts` |

## 4. Правила, которые держатся на дисциплине

Тесты их не ловят целиком, поэтому они важнее всего на ревью.

- **Database-per-tenant.** Никакого `tenant_id` в бизнес-таблицах. Тенант, пул и `tx.Manager` приходят в `context.Context` (middleware `TenantDB`, `BindTenant`, воркер). Домен берёт `tenant.GetTxManager(ctx)`, репозиторий — querier из менеджера в контексте. Репозиторий не принимает пул параметром.
- **Транзакция — короткая и только с БД.** По умолчанию `statement_timeout` 30 с (`TxOptions.StatementTimeout`); ослабить его во вложенном вызове нельзя. RPC, Vault, broadcast, HTTP-доставка — вне транзакции. Побочный эффект, который должен случиться вместе с записью, пишется в outbox в той же транзакции (`Transaction*`-хук); «хорошо бы» — в `After*` через `tx.OnCommit`.
- **Чистые функции домена.** `Validate(ctx)` не ходит в БД и сеть; проверка уникальности — хук. `GenerateMovements`/`Generate…Movements` детерминированы: тот же документ — те же движения.
- **Конкурентность.** По умолчанию оптимистическая блокировка (`version`, 409 через `apperror.NewConcurrentModification`). Пессимистическая (`FOR UPDATE`, advisory lock) — только для остатков и переходов состояния, ключи **сортируются** перед захватом. Финансовые движения append-only: отмена — сторно (`AppendReversalsByRecorder`), не UPDATE/DELETE.
- **Ошибки.** Только `apperror.AppError`; оборачивание `fmt.Errorf("действие: %w", err)`; обработать один раз (залогировать *или* вернуть). Handler: `c.Error(err)` + `c.Abort()`, никогда `c.JSON(status, err)`. Клиенту — без сумм, балансов, внутренних ID, SQL (CWE-209): детали в `logger.Warn`, наружу — общее сообщение. Коды ошибок конфигурации объявляются в её домене.
- **Деньги.** `types.MinorUnits` и масштаб из метаданных валюты; никаких захардкоженных 100/1000. Количество — `types.Quantity`.
- **ID.** UUIDv7 из `id.New()` на стороне Go; ID документа, участвующего в проведении, не генерируется в БД.
- **Горутины.** У каждой есть владелец, `ctx.Done()` и остановка, у тикера — `defer ticker.Stop()`; долгоживущее в воркере — компонент под супервизором (`configuration.Worker`).
- **Миграции.** Своя история у каждого слоя: платформа (`db/migrations`), база (`db/configuration/<база>`, версии ниже 10000), надстройка (`db/configuration/<надстройка>`, своя история `goose_db_version_<надстройка>`, версии ниже 10000), расширения (`db/extensions`, 10000+). Надстройка не меняет таблицы базы (`ALTER` чужой таблицы — нет): её данные — в её собственных таблицах. Миграции платформы не знают таблиц конфигураций; конфигурация может ссылаться на таблицы платформы. Платформенная миграция не правится никогда — изменение платформенной схемы это новая миграция. Миграции конфигурации и расширений до первого production-релиза правятся в той, что владеет таблицей (без переходных ALTER), и проверяются на свежей базе; после релиза — только новые forward-only в любой истории. Строгий DDL без `IF NOT EXISTS`, префиксы `cat_`/`doc_`/`reg_`/`sys_`/`auth_`, табличная часть `<шапка>__<ключ>`.
- **Без ORM и без `any`.** Сырой SQL через pgx и squirrel; обобщение — дженериками.

## 5. Рекомендуемые паттерны

Каждый описан в `references/patterns.md`: когда применять, как он устроен в Metapus, где пример, в чём ловушка. Прочитай нужный раздел перед тем, как писать код соответствующего вида.

| # | Паттерн | Когда тянуться к нему |
|---|---|---|
| 1 | Декларативная сущность (модель + `Definition` + одна строка регистрации) | любой новый справочник или документ |
| 2 | Явная регистрация | сущность выходит за автоправило: секреты, вычисляемые поля, свои маршруты |
| 3 | Порты и адаптеры | домену или сценарию нужна БД, сеть, время, подпись |
| 4 | Точка расширения (инверсия зависимости) | платформе надо вызвать код конфигурации |
| 5 | Visitor / Recorder регистра | документ двигает регистр |
| 6 | Хуки жизненного цикла и transactional outbox | побочный эффект записи: аудит, событие, уведомление |
| 7 | Unit of Work через context | несколько записей должны быть атомарны |
| 8 | Контроль конкурентности | изменение остатков, переходы состояния, гонки |
| 9 | Фасад и сужение (`pkg/sdk`) | конфигурации не хватает чего-то из платформы |
| 10 | Composition Root и значение `Configuration` | сборка сервисов, сервер/воркер/сид конфигурации |
| 11 | Разбор на границе, тонкий адаптер | HTTP-обработчик, DTO, внешние данные |
| 12 | Состояние документа | правила «можно ли изменить/провести» |
| 13 | Идемпотентность | команды API, сиды, воркеры, повтор после сбоя |
| 14 | Functional options | конструктор с ≥3 необязательными параметрами |

## 6. Порядок работы

**До кода.** Определи по разделу 2, где живёт изменение. Найди аналог. Если конфигурации чего-то не хватает в `pkg/sdk` — сначала реши, это алиас существующего (минорная версия SDK) или новая функция платформы, которая заменит ручную сборку (лучше). Если затронута граница — прочитай `references/boundaries.md`.

**В коде.** Данные — раньше логики: сначала модель и `Definition`, потом поведение. Не обходи правило через исключение, `//nolint`, копию платформенного кода в конфигурацию или импорт `internal/` «временно».

**После кода** — проверь то, что затронул (команды из корня репозитория):

```bash
go build ./... && go vet ./... && go vet -tags integration ./...
go test -short ./internal/architecture/...
golangci-lint run ./...
go run ./cmd/http-contracts -check        # если менялись сущности, DTO или маршруты
make check-configurations                 # если менялась граница платформа/конфигурация или фронтенд
```

Интеграционные тесты (миграции, репозитории, проведение) требуют одноразового Postgres; полный прогон — `go run ./cmd/integration-test` с `METAPUS_TEST_ADMIN_DSN`. Если менялся `pkg/sdk` — запись в `docs/sdk/CHANGELOG.md` и, при необходимости, `sdk.APIVersion`.

**В ответе пользователю** назови, куда положен код и почему (какое правило из раздела 2 сработало), какие паттерны применены, и что проверено. Если пришлось отступить от правила — скажи это прямо, а не прячь.

## 7. Анти-паттерны — стоп-сигналы

- Импорт `github.com/alex-bogatiuk/metapus/internal/...` из конфигурации или расширения на SDK, даже в тесте.
- База, которая импортирует свою надстройку или знает её имя; надстройка, которая импортирует чужую базу или другую надстройку; надстройка поверх надстройки.
- Надстройка, которая правит таблицу базы (`ALTER TABLE cat_items …`) или подменяет поведение базы там, где база не объявила точку замены.
- Отраслевая специфика (автозапчасти, СТО) в базовой конфигурации, или типовая торговая логика в надстройке.
- Платформенный пакет, который знает имя конфигурации, её таблицу или её тип; `switch` по имени конфигурации в платформе.
- Новая строка в `known…Leaks` или любом другом списке исключений архитектурных тестов.
- Тип, объявленный в `pkg/sdk` (не алиас), или поведение, которого платформа не выполняет.
- `pkg/sdk/postgres`, `pkg/sdk/http/...` или gin/pgx в `domain/`.
- Ручная привязка тенанта, сборка `TxManager` над сырым пулом или цепочки middleware в конфигурации — для этого есть `BindTenant`, `ProtectedGroup`, `Authenticated`.
- Ключ шифрования или шифротекст учётных данных в руках конфигурации — для этого есть `secretbox.Seal/Open`, `catalog_repo.AccountCredentials`, `PoolAccountKeys`, `AccountKeysOf`.
- SQL конфигурации по таблицам интеграций платформы (`JOIN cat_integration_accounts`, `INSERT INTO cat_integration_accounts`) — спроси у `catalog_repo` (`PoolProvider`, `UsableAccountIDs`, `LockUsableAccount`, `CreateKeylessAccount`…) и соедини свои таблицы с ID.
- Бизнес-условие в HTTP-обработчике; запрос к БД в `Validate`; side effect в `GenerateMovements`.
- RPC или подпись внутри транзакции; обязательный аудит в `After*`-хуке.
- `SELECT FOR UPDATE` по несортированным ключам; UPDATE/DELETE финансовых движений.
- Копия CRUD-кода вместо `BaseCatalogRepo`/`CatalogService`/`BaseDocumentService`/автоматической регистрации.
- `tenant_id` в бизнес-таблице; `DEFAULT gen_random_uuid()` для ID проводимого документа.

## 8. Когда правило мешает

Правило мешает — значит, платформе не хватает точки расширения. Правильный ход почти всегда один из трёх:

1. **Добавь в платформу точку расширения** (интерфейс, который реализует конфигурация; реестр; модуль) и вызови её из платформы. Так появились `posting.Module`, `v1.RouteModule`, `integrations.PoolConsumer`.
2. **Подними повторяющуюся сборку в платформу одной функцией** и выставь её через `pkg/sdk` вместо деталей.
3. **Добавь алиас или обёртку в `pkg/sdk`** с записью в CHANGELOG — если конфигурации нужен уже существующий публичный по смыслу символ.

Если ни один не подходит, остановись и спроси пользователя: решение о новом исключении принадлежит ему, а не тебе.

## Справочники

- `references/boundaries.md` — каждое архитектурное правило: что запрещает, как выглядит нарушение, как исправить; правила `pkg/sdk` и его версий; миграции трёх историй.
- `references/patterns.md` — 14 паттернов с примерами из кода.
- В репозитории: `docs/ROUTER.md` (вход в документацию), `docs/systems/extension-api.md` (точки расширения конфигурации, таблицы бэкенда и фронтенда), `docs/sdk/README.md`, `db/migrations/README.md`, `.agents/rules/GLOBAL.md` (стиль Go и фронтенда).
