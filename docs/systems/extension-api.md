# API Расширений (Extension API)

> **TL;DR:** Трехуровневая архитектура позволяет интеграторам добавлять новые сущности (справочники, документы), регистры и бизнес-логику (через Хуки) в папку `extensions/` без изменения исходного кода ядра платформы.

> **Тип:** Concept
> **Аудитория:** Developer, Integrator
> **Связанные:** [crud-pipeline.md](crud-pipeline.md)

---

## 1. Трехуровневая модель

Вместо того чтобы изменять ядро платформы (Hard Fork), Metapus использует модульную систему.

| Слой | Назначение | Кто разрабатывает | Папка |
|------|------------|-------------------|-------|
| **Core** | Движок, транзакции, права доступа | Команда Metapus | `internal/core/` |
| **Content** | Встроенные (стандартные) справочники и документы | Команда Metapus | `internal/content/` |
| **Extensions**| Кастомные клиентские сущности | Интегратор | `extensions/` |

## 2. Регистрация новых сущностей

Чтобы добавить новый Справочник или Документ, интегратор не пишет CRUD-хэндлеры с нуля. Он создаёт структуру, реализующую `CatalogRegistration` или `DocumentRegistration`.

Затем модуль подключается в общем Composition Root `extensions/register.go`; сервер и генератор контрактов используют этот список:
```go
myExtension.Register(factoryReg) // registration uses v1.EntityRegistration[Model]
```

**Frontend Auto-Discovery:**
Платформа генерирует UI для расширений автоматически! Фронтенд получает `/api/v1/meta/entities` и использует generic routes `[routePrefix]` для списка и AutoForm. Запросы/ответы должны быть зарегистрированы в HTTP contracts registry: metadata не разрешает незарегистрированный URL или дополнительное writable поле. Новые сущности автоматически появляются в разделе расширений меню; custom forms и особое размещение подключаются через frontend registry/navigation. Подробности — [новый справочник](../howto/new-entity.md).

## 3. Точки расширения

Кроме создания новых сущностей, Extension API позволяет "вклиниваться" в стандартные:

1. **Хуки (Hooks):** Инъекция кастомной логики в системные документы.
   ```go
   invoiceService.Hooks().OnBeforeCreate(func(ctx, doc) error { ... })
   ```
2. **Новые Регистры:** Интегратор может написать свой `RegisterVisitor` и `RegisterRecorder`, чтобы документы, реализующие нужные source-интерфейсы начали двигать новые кастомные регистры. Регистры конфигурации подключаются модулем `posting.Module` через `RouterConfig.PostingModules`: модуль добавляет visitors, recorders и проверки в движок и возвращает `MovementProvider` для отчёта о движениях документа. Пример — `cryptoregisters.Registers`.
3. **Атрибуты (JSONB):** У каждой сущности есть поле `attributes` для произвольных данных интегратора. Схемы, валидации, фильтрации и колонок для них нет; типизированное поле добавляется в Go-модель сущности.
4. **CEL Политики:** Написание кастомных правил блокировок (например, запрет проведения документов с суммой > 1 млн) в виде конфигурации через CEL (Common Expression Language), без перекомпиляции Go.

## 4. Модули конфигурации

Конфигурация (сейчас — криптопроцессинг) подключается к платформе модулями, а не правками ядра:

| Точка расширения | Что даёт конфигурация | Пример в Metapus |
|---|---|---|
| `posting.Module` | регистры, visitors/recorders и проверки проведения; возвращает `MovementProvider` | `infrastructure/crypto/cryptoregisters` |
| `v1.RouteModule` (`RouterConfig.RouteModules`) | маршруты на группах из `v1.RouteContext`: `Root`, `API`, `Protected`, `System`; свои зависимости модуль хранит сам | `infrastructure/http/cryptohttp` |
| `auth.WithAccessScopeProvider` | объектные права сессии в claim `scp` | `merchant.NewPortalAccessScopes` |
| `middleware.WithCommandScope` | владелец ключей идемпотентных команд (например, партнёр по API-ключу) | `cryptomiddleware.WithMerchant`, `MerchantPortal` |
| `content.Services` | сервисы конфигурации для регистраций документов вне `v1.DocumentDeps` | ссылки checkout, подписанты согласований |
| `integrations.Catalog` | назначения и провайдеры интеграций конфигурации рядом с платформенными (CoinGecko, Telegram, SMTP). Каталог неизменяем, собирается один раз и передаётся явно: сервисам аккаунтов и пулов, выбору аккаунтов (`integration_repo.NewRuntime`), `RouterConfig.IntegrationCatalog`; HTTP-профили — реестром `profiles.New(...)`. Список провайдеров в БД задаёт миграция конфигурации | `cryptointegrations.Catalog()`, `cryptoprofiles.Registry()` |
| `integrations.PoolConsumer` | справочник, читающий аккаунты через пулы интеграций: пул нельзя удалить или сменить ему провайдера, пока потребитель им пользуется; состав пула и новые ключи аккаунта проверяются по его требованиям. Конфигурация создаёт репозитории аккаунтов и пулов со своими потребителями | `crypto_integration_repo.RPCPoolConsumer` |
| `entity.MovementColumnDef.RefEntity` | колонка-ссылка движения регистра называет сущность, на которую ссылается; по ней разрешаются представление и ссылка `/catalogs|documents/{route}/{id}`. Колонка без `RefEntity` не показывается | колонки регистров `crypto_*` |
| `platform.MovementSourceProvider` | для документа с движениями: какие его поля раскрывают каждую колонку движения. При FLS-политике на документ колонка видна, только если объявлена и все перечисленные поля читаемы; пустой список — колонка-константа | `content/movement_sources.go` |
| `db/configuration/<имя>/` | миграции конфигурации: её таблицы, права и FLS её сущностей, ограничения её правил на таблицах платформы. Они в общей последовательности версий с платформенными `db/migrations/` и применяются одним источником `dbassets.TenantMigrations()` | `db/configuration/crypto/` |
| `spec.Bundle` | контракты HTTP конфигурации; генератор пишет их в её собственные файлы `frontend/configuration/<имя>/{types,lib}/generated/`, а типы платформы импортирует из платформенных файлов (`contracts.GenerateOn`, `contracts.GenerateRoutesOn`) | `content.CryptoHTTPContractBundles`, `content.HTTPArtifacts` |

Граница проверяется тестами в `internal/architecture`:

- `configuration_boundary_test.go`: платформенный пакет не импортирует пакет конфигурации; список исключений пуст и должен таким остаться;
- `configuration_storage_test.go`: строковые литералы платформенного кода не называют таблицы из миграций конфигурации (`db/configuration/*`), а тесты платформы не импортируют конфигурацию; оба списка исключений пусты. Проверки собранного приложения (все HTTP-контракты, актуальность сгенерированных файлов, наборы отчётов конфигурации) лежат в `internal/content`, проверки поведения конфигурации — в её пакетах.

Во фронтенде код конфигурации лежит в `frontend/configuration/<имя>/` (`components`, `lib`, `hooks`, `types`, `stores` с той же структурой, что у платформы), платформенный — в `frontend/components`, `lib`, `hooks`, `stores`, `types`. Страницы и layout конфигурации лежат в `frontend/configuration/<имя>/pages/` по структуре маршрутов. `frontend/app/` — точка сборки: маршрут конфигурации в нём — однострочная обёртка `export { default } from "@/configuration/<имя>/pages/…"`, которую требует Next.js. Тест `frontend/tests/unit/configuration-boundary.spec.ts` проверяет, что:

- платформенные файлы не импортируют `configuration/`, и список исключений пуст;
- платформенный код и маршруты `app/` не используют словарь конфигурации (crypto, merchant, portal, wallet и т. п.), кроме путей модулей;
- `app/` обращается к конфигурации только через такие обёртки и `app/configuration.ts`.

Конфигурация подключает свои части при старте: `frontend/app/configuration.ts` вызывает `registerCryptoConfiguration()`, его импортируют первыми клиентский layout `app/(main)` и страница дашборда.

| Точка расширения фронтенда | Что даёт конфигурация | Пример |
|---|---|---|
| `registerEntityOverlays` (`lib/entity-registry-defaults`) | колонки, видимость, фильтры, форма и список для сущностей из metadata | `configuration/crypto/lib/entity-overlays.ts` |
| `registerWidget` (`lib/widget-registry`) | виджет дашборда со своей схемой настроек (`configSchema`) и редактором (`configEditor`) | `configuration/crypto/lib/widgets.tsx` |
| `registerDefaultDashboard` (`lib/dashboard-layout`) | дашборд пользователя по умолчанию | там же |
| `detailContent`, `renderField` в overlay сущности | панели под формой существующей записи и свои редакторы полей на generic-страницах `[routePrefix]/[id]` | `configuration/crypto/components/detail-panels.tsx` |
| `registerNavigation` (`lib/navigation`) | группы разделов меню (в существующую группу или после указанной), системные ссылки, пункты палитры команд; у пункта может быть `permission` | `configuration/crypto/lib/navigation.ts` |
| `registerRouteEntities`, `registerIcons`, `registerRouteLabels` | ключ сущности, иконка и заголовок вкладки по URL до загрузки metadata | там же |
| `registerWorkspace` (`lib/access`) | отдельный интерфейс для пользователей без доступа к ERP; `landingPath(user)` выбирает его после входа | `configuration/crypto/lib/access.ts` (портал) |
| `createScopedClient(scope)` (`lib/api`) | клиент своей HTTP-области `/<scope>/v1` с общей сессией, проверкой контрактов и идемпотентностью | `configuration/crypto/lib/api/portal.ts` |
| `registerSecurityDimension`, `registerProfilePreset`, `registerAffinityRule`, `registerResourceLabels` (`lib/security-registry`) | RLS-измерение с поиском значений, шаблоны профилей безопасности, подсказки ролей, названия ресурсов прав | `configuration/crypto/lib/security.ts` |
| `registerSettingsPage` (`lib/settings-registry`), `registerAdminSection` (`lib/admin-registry`) | свои страницы в «Настройках» и разделы в «Управлении» | `configuration/crypto/lib/settings.tsx`, `lib/admin.ts` |
| `registerStatusColors`, `registerWorkerJobs` | цвет статусов и фильтры журнала задач воркера | `configuration/crypto/lib/labels.ts` |
| собственный API-клиент | её эндпоинты; платформенный `@/lib/api` содержит только платформенные | `configuration/crypto/lib/api` (`cryptoApi`) |

Регистрации выполняются при загрузке модуля `app/configuration.ts`, поэтому платформа читает реестры при рендере, а не при загрузке своих модулей. Хранилища конфигурации, сохраняемые в `localStorage`, называются с префиксом `SCOPED_STORAGE_PREFIX` (`metapus-scoped-`): платформа очищает их при смене пользователя, даже если модуль ещё не загружен.

Печатные формы документ конфигурации объявляет через `v1.PrintFormsProvider`; типы областей команд (`command.Metadata.ScopeType`) и маршрутов контрактов — открытые идентификаторы, конфигурация добавляет свои (`merchant`, `portal`) без правок ядра. Состояние раскрытых секций интерфейса хранится в `interface.expandedSections` под ключом, который выбирает UI.

Коды ошибок конфигурации объявляются рядом с её доменом и собираются из `apperror.AppError`; `core/apperror` хранит только платформенные коды. Например, `WALLET_POOL_EXHAUSTED` объявлен в `catalogs/wallet`, `CRYPTO_NETWORK_UNAVAILABLE` в `catalogs/blockchain_network`. Подсказку `details.retryAfterSeconds` любой ошибки `ErrorHandler` выставляет в заголовок `Retry-After`.

## 5. Scaffold CLI

Для ускорения разработки расширений используется встроенный кодогенератор:
```bash
go run cmd/scaffold/main.go --name waybill --type document
```
Генератор создаёт модель выбранного вида, service/repository, явные writable DTO, update-version, регистрацию, типизированный HTTP bundle и миграцию с актуальными CDC triggers и permissions. Миграция пишется в `db/configuration/<name>/` с версией 10000+: оттуда она встраивается в бинарники и применяется вместе с платформенными. Документ создаётся без искусственной табличной части. `--hierarchical` включает папки и проверки иерархии справочника; без флага справочник плоский.

Общее `entity.Definition` связывает identity, таблицу, маршрут и hierarchy. `contracts/spec.Entity` выводит стандартные HTTP-контракты из Definition и конкретных DTO. После добавления модуля в `extensions/register.go` выполните `go run ./cmd/http-contracts`; отдельные правки registry/routes для расширения не нужны. Права других ролей, бизнес-поля, локализация, движения и новый worker workflow требуют явной настройки.

Модули `extensions/` компилируются внутри Metapus. Публичный внешний SDK в `pkg/extension` пока ограничен версией API; не используйте его как готовый фасад регистрации.

## 6. Единый контракт встроенных сущностей и расширений

Все встроенные справочники и документы предоставляют domain `Definition`. Модель реализует `EntityDefinition()`; generic `EntityRegistration[Model]` выводит identity и metadata, `CatalogRegistrationFor[Model,Create,Update,Read]` также выводит стандартные transport contracts. Нулевое значение регистрации работоспособно: передавать Definition вручную в struct literal не требуется.

Factory contracts требуют Definition и HTTP bundle. Стандартные DTO и маршруты codegen получает из того же registry, что и сервер. Разрешённые document capabilities ограничивают набор сгенерированных маршрутов; custom команды и специальные detail projections объявляются у владельца документа.

Catalog service и repository читают hierarchy из Definition, включая type, folder restrictions и max depth. Глобальный реестр hierarchy удалён; прямые repository writes используют ту же политику, что и service. Для отдельного тестового ограничения передайте копию Definition с нужной Hierarchy.

## Extension API 2.0

Добавлены обязательные Definition и HTTPContracts у factory registrations. CatalogServiceConfig принимает Definition вместо строкового EntityName; глобальные GetCatalogMeta/RegisterCatalogMeta удалены. Внешний модуль на прежнем API должен перейти на EntityDefinition, typed registration и Definition-based repository/service constructors. Новые scaffold-модули сразу используют API 2.0.
