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
| `pkg/sdk/sdktest` | `Check` — контрактная проверка конфигурации; с тегом `integration`: `NewTenant` — свежий тенант с её миграциями, `NewTenantWithMigrations` — то же для тестов внутри пакетов конфигурации, `NewExchangeRateDatabase` — пустая база кластера курсов |

### Расширенный слой

Пакеты повторяют внутренние пакеты платформы один к одному, с тем же именем пакета: перевод конфигурации на фасад сводится к замене путей импорта. Наружу вынесены только символы, которые конфигурации используют, а не весь внутренний пакет.

| Группа | Пакеты `pkg/sdk/...` | Что фронтируют в `internal/` |
|---|---|---|
| Ядро запроса | `appctx`, `tenant`, `tx`, `tenantdb`, `json`, `logger`, `encryption`, `numerator`, `workerjob` | `core/context`, `core/tenant`, `core/tx`, `infrastructure/tenantdb`, `core/json`, `pkg/logger`, `core/crypto`, `core/numerator`, `core/workerjob` |
| Безопасность | `security`, `security_profile`, `readaccess`, `readpolicy` | политики RLS, CEL и полей, авторизованное чтение |
| Сервисы | `domain`, `audit`, `cursor`, `filter`, `eventlog`, `notifications`, `settings`, `command` | базовые сервисы справочников и документов, хуки, удаление, ссылки, события, outbox, идемпотентные команды |
| Регистры и отчёты | `registers/exchange_rate`, `reports/schema`, `reports/contract`, `reports/compiler`, `printing`, `printing/render` | регистр курсов, наборы данных отчётов, печатные формы |
| Интеграции | `integrations`, `integrations/transport`, `integrations/profiles`, `integrations/providerpolicy`, `integrations/integrationapp`, `urlsafe` | аккаунты, пулы и провайдеры, транспорт, профили, допуск вызовов, проверка публичных URL |
| HTTP | `http/v1`, `http/handlers`, `http/middleware`, `http/dto`, `http/httpvalue`, `http/jsoncodec`, `contracts` | регистрация маршрутов, базовые обработчики, middleware прав, step-up и идемпотентности, контракты |
| Хранилище | `postgres`, `postgres/catalog_repo`, `postgres/document_repo`, `postgres/integration_repo`, `postgres/filter`, `postgres/keyset`, `postgres/numerator`, `registers`, `migration` | менеджер транзакций, базовые репозитории, блокировки документов, outbox, миграции и версии схемы |
| Метаданные и процесс | `metadata`, `telemetry`, `envconfig`, `secretfile` | описание сущностей для UI, трассировка, окружение, секреты из файлов |

Расширенный слой широк (всего в фасаде 62 пакета и около 590 объявлений) и повторяет устройство платформы. Поэтому он стабилен слабее ядра: изменение внутреннего типа видно через алиас. Сужать его — отдельная работа; кандидаты — места, где конфигурация собирает платформенную механику сама (маршрутизация тенантов по ключу мерчанта, сборка сервисов из репозиториев).

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
