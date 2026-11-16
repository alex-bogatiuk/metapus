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
| `spec.Bundle` | контракты HTTP для генератора: `contracts.Registry(bundles...)`, `contracts.Routes(bundles...)` | `content.HTTPContractBundles` |

Граница проверяется тестами в `internal/architecture`:

- `configuration_boundary_test.go`: платформенный пакет не импортирует пакет конфигурации; список исключений пуст и должен таким остаться;
- `configuration_storage_test.go`: строковые литералы платформенного кода не называют таблицы из миграций конфигурации (`00006`–`00009`), а тесты платформы не импортируют конфигурацию. Списки известных исключений (RPC-подсистема, проверки сгенерированных контрактов и наборов отчётов) только сокращаются.

Печатные формы документ конфигурации объявляет через `v1.PrintFormsProvider`; типы областей команд (`command.Metadata.ScopeType`) и маршрутов контрактов — открытые идентификаторы, конфигурация добавляет свои (`merchant`, `portal`) без правок ядра. Состояние раскрытых секций интерфейса хранится в `interface.expandedSections` под ключом, который выбирает UI.

Коды ошибок конфигурации объявляются рядом с её доменом и собираются из `apperror.AppError`; `core/apperror` хранит только платформенные коды. Например, `WALLET_POOL_EXHAUSTED` объявлен в `catalogs/wallet`, `CRYPTO_NETWORK_UNAVAILABLE` в `catalogs/blockchain_network`. Подсказку `details.retryAfterSeconds` любой ошибки `ErrorHandler` выставляет в заголовок `Retry-After`.

## 5. Scaffold CLI

Для ускорения разработки расширений используется встроенный кодогенератор:
```bash
go run cmd/scaffold/main.go --name waybill --type document
```
Генератор создаёт модель выбранного вида, service/repository, явные writable DTO, update-version, регистрацию, типизированный HTTP bundle и миграцию с актуальными CDC triggers и permissions. Документ создаётся без искусственной табличной части. `--hierarchical` включает папки и проверки иерархии справочника; без флага справочник плоский.

Общее `entity.Definition` связывает identity, таблицу, маршрут и hierarchy. `contracts/spec.Entity` выводит стандартные HTTP-контракты из Definition и конкретных DTO. После добавления модуля в `extensions/register.go` выполните `go run ./cmd/http-contracts`; отдельные правки registry/routes для расширения не нужны. Права других ролей, бизнес-поля, локализация, движения и новый worker workflow требуют явной настройки.

Модули `extensions/` компилируются внутри Metapus. Публичный внешний SDK в `pkg/extension` пока ограничен версией API; не используйте его как готовый фасад регистрации.

## 6. Единый контракт встроенных сущностей и расширений

Все встроенные справочники и документы предоставляют domain `Definition`. Модель реализует `EntityDefinition()`; generic `EntityRegistration[Model]` выводит identity и metadata, `CatalogRegistrationFor[Model,Create,Update,Read]` также выводит стандартные transport contracts. Нулевое значение регистрации работоспособно: передавать Definition вручную в struct literal не требуется.

Factory contracts требуют Definition и HTTP bundle. Стандартные DTO и маршруты codegen получает из того же registry, что и сервер. Разрешённые document capabilities ограничивают набор сгенерированных маршрутов; custom команды и специальные detail projections объявляются у владельца документа.

Catalog service и repository читают hierarchy из Definition, включая type, folder restrictions и max depth. Глобальный реестр hierarchy удалён; прямые repository writes используют ту же политику, что и service. Для отдельного тестового ограничения передайте копию Definition с нужной Hierarchy.

## Extension API 2.0

Добавлены обязательные Definition и HTTPContracts у factory registrations. CatalogServiceConfig принимает Definition вместо строкового EntityName; глобальные GetCatalogMeta/RegisterCatalogMeta удалены. Внешний модуль на прежнем API должен перейти на EntityDefinition, typed registration и Definition-based repository/service constructors. Новые scaffold-модули сразу используют API 2.0.
