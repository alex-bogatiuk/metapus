# Добавление справочника

> **TL;DR:** Новый справочник подключается через domain-модель, PostgreSQL repository, CatalogService, DTO, фабрику и generated HTTP-контракт; generic frontend использует metadata.
> **Тип:** How-To
> **Аудитория:** Developer
> **Связанные:** [CRUD pipeline](../systems/crud-pipeline.md), [Новый документ](new-document.md)

## 1. Выбор аналога и структуры

Для встроенного справочника используйте существующий `Counterparty`:

| Часть | Исходник |
|---|---|
| Модель и интерфейс repository | `internal/domain/catalogs/counterparty/model.go`, `repo.go` |
| Сервис и хуки | `internal/domain/catalogs/counterparty/service.go` |
| PostgreSQL | `internal/infrastructure/storage/postgres/catalog_repo/counterparty.go` |
| Регистрация | `counterpartyRegistration()` в `internal/platformcontent/catalogs.go`: `v1.Catalog` на собственном сервисе, DTO и контракты выводятся из модели |
| Таблица | `db/migrations/00003_catalogs.sql` |

Domain-модель находится в `internal/domain/catalogs/<name>/`, регистрации — в `internal/content/`. Клиентское расширение размещается в `extensions/<name>/` внутри Go-модуля Metapus. Доступны базовые типы `internal/core/entity` и совместимые aliases в `internal/platform`; `pkg/extension` пока содержит только версию API.

Для расширения можно сгенерировать стартовые файлы из корня проекта:

```bash
go run ./cmd/scaffold --name vehicle --type catalog --table cat_vehicles
```

Для иерархического справочника добавьте `--hierarchical`. По умолчанию создаётся плоский справочник. Модуль создаётся в `extensions/<name>/`, его миграция — в `db/configuration/<name>/`: только этот каталог и `db/migrations/` встраиваются в бинарники и применяются `cmd/tenant migrate`. Генератор выбирает следующую свободную версию из диапазона 10000+ по всем tenant-миграциям; `--migration-version` позволяет задать её явно.

Подключите import нового пакета и `<name>.Register(reg)` **один раз** в `extensions/register.go`. Этот список используется сервером и генератором HTTP-контрактов. Сгенерированная регистрация — одна строка `v1.Catalog[Vehicle](presentation)`: репозиторий, сервис, маршруты, DTO и HTTP-контракты выводятся из модели ([правило](../systems/extension-api.md#2-регистрация-новых-сущностей)). Затем выполните `go run ./cmd/http-contracts` и `go run ./cmd/tenant migrate`. После миграций команда создаёт права зарегистрированного справочника (`catalog:<key>:read|create|update|delete`) и выдаёт их роли admin; остальные роли назначаются явно.

`model.go` содержит `entity.Definition` (key, name, route, table и hierarchy) и модель. Добавьте бизнес-поле в модель с тегами `db`, `json`, `meta` и его колонку в миграцию, затем перегенерируйте контракты: поле сразу появляется в API, AutoForm и списке. Поле, которое заполняет только сервер, пометьте `meta:"readOnly:true"`: форма покажет его в «Дополнительно». Форма раскладывает поля сама ([компоновка AutoForm](../systems/extension-api.md#2-регистрация-новых-сущностей)); собрать поля в свою группу можно тегом `meta:"group:<заголовок>"`. Бизнес-проверки — в `Validate`, хуки сервиса — через `.Configure(...)` в `register.go`. Поле-ссылка с `meta:"ref:<ключ>"` получает в ответе представление `{id, name, code}`. Если справочнику понадобятся свои DTO или собственные маршруты, перейдите на явную регистрацию по аналогу из разделов ниже.

## 2. Таблица и модель

Для новой сущности добавьте исходную goose-миграцию со свободной версией; для расширений диапазон начинается с 10000, файл лежит в `db/configuration/<name>/`. Изменения существующей сущности до production вносятся в миграцию, владеющую таблицей. [Правила миграций](../../db/migrations/README.md) определяют CDC, UUIDv7 и типы служебных таблиц.

Модель встраивает `entity.Catalog`, добавляет поля с `db`, `json` и `meta` тегами. `Validate(ctx)` проверяет только данные в памяти. FK, колонки и nullability модели должны совпасть с SQL. `attributes` и `_txid` получают NOT NULL/default по выбранному аналогу.

### Ссылочные поля (meta ref-теги)

На FK задавайте label и reference key, например `meta:"label:Сеть,ref:blockchain_network"`. `ref` должен совпадать с `ReferenceTypes()` целевой регистрации. Текущие примеры: `counterparty`, `organization`, `contract`, `currency`, `merchant`, `wallet`, `token`, `blockchain_network`.

Проверьте реальные ключи в `catalog_registrations.go`: warehouse, product, unit и vat_rate в текущем реестре отсутствуют. Без `ref` generic UI не сможет корректно выбрать или показать связанный объект.

`meta:"list:false"` скрывает поле из каталога колонок, но **не защищает секрет**: секретные значения исключаются из response DTO, схемы чтения и list projection.

## 3. Repository, service и DTO

- Встройте `catalog_repo.BaseCatalogRepo[*YourEntity]`. Создавайте его через `NewCatalogRepoForDefinition`: Definition задаёт table, logical key и полную hierarchy policy; отдельно передаются select columns и фабрика объекта. RLS dimensions и reference fields подключаются по аналогу.
- Встройте `domain.CatalogService[*YourEntity]`; настройте `CatalogServiceConfig`. `BeforeCreate/Update` выполняются в транзакции. Обязательные audit/outbox эффекты регистрируются на `Transaction*`, необязательные — на `After*` после внешнего коммита.
- DTO не пишутся: запросы и ответ выводятся из модели ([правило](../systems/extension-api.md#2-регистрация-новых-сущностей)). Поле, которое заполняет только сервер, пометьте `meta:"readOnly:true"`; обязательное — `binding:"required"`; ссылку — `meta:"ref:<ключ>"`, тогда ответ содержит её представление. Значения по умолчанию и нормализация — в хуках сервиса, а не в DTO.
- Свои DTO с `handlers.NewCatalogHandler` нужны, только если справочник не укладывается в правило: секреты только для записи, вычисляемые поля ответа, коллекции, поля, которые можно менять лишь при изменении. Так устроены кошельки, мерчанты, сети, интеграции и источники курсов.

## 4. Фабрика, права и HTTP-контракт

Модель реализует `EntityDefinition() entity.Definition`. Регистрация — `v1.Catalog[Model](presentation)`; собственный сервис со своим репозиторием подключает `.Service(func(deps v1.CatalogDeps) *domain.CatalogService[*Model] { ... })`, псевдонимы ссылок — `.References(...)`, поисковые колонки — `.Search(...)`, имя ответа при конфликте — `.ResponseName(...)`. Явная регистрация `v1.CatalogRegistrationFor[Model, CreateDTO, UpdateDTO, ResponseDTO]` с `Build` остаётся для справочников вне правила. Оба пути общие для встроенных справочников и расширений.

Встроенную фабрику подключите в `internal/content/register.go` через `RegisterCatalog`. Расширение подключается в composition root. Права стандартных действий создаются из регистрации при `cmd/tenant migrate` и выдаются роли admin ([RBAC](../systems/auth-security.md#2-rbac-role-based-access-control)); назначения другим ролям и права рукописных маршрутов добавьте в seed. Регистрация metadata не выдаёт пользователю право доступа.

Стандартные DTO и маршруты собираются из factory registry через `HTTPContracts()` самой регистрации. Отдельного списка справочников или документов в HTTP codegen нет. Для custom подресурсов дополните bundle в регистрации сущности. Сущности конфигурации подключаются в её `Register` (у крипто — `register` в `internal/content/register.go`), расширения — в `extensions.Register`; сервер и codegen собирают их вместе с общими сущностями платформы через `configuration.Compose`. `apiFetch` отклоняет незарегистрированные URL. Выполните:

```bash
go run ./cmd/http-contracts
go run ./cmd/http-contracts -check
```

Generated types, Zod schemas и route registry не редактируются вручную.

## 5. Frontend и проверка

Generic страницы находятся в `frontend/app/(main)/catalogs/[routePrefix]/`. Метаданные определяют поля и список колонок. Custom form, renderers и defaults задаёт `frontend/lib/entity-registry-defaults.ts` для платформенных сущностей и `registerEntityOverlays(...)` конфигурации для её сущностей (`frontend/configuration/crypto/lib/entity-overlays.ts`); отдельная форма нужна только при специализированном UX. Новые сущности автоматически появляются в разделе расширений меню. Ручная frontend-регистрация нужна только для custom UX или размещения в существующем разделе. Канонический `entity.key` поступает из backend metadata и не зависит от множественного числа в `routePrefix`.

Поле списка должно присутствовать в list response. AutoForm отправляет writable projection **конкретного request DTO** и проверяет её generated schema; metadata не расширяет разрешённый payload.

- [ ] Миграция и domain-модель совпадают; FK и `ref` указывают на существующие сущности.
- [ ] Repository использует tenant TxManager из context, whitelist колонок и RLS.
- [ ] Права, фабрика, DTO и route contracts подключены.
- [ ] `go build ./...`, профильные тесты и `go run ./cmd/http-contracts -check` проходят.
- [ ] Из `frontend`: `npm run typecheck`, `npm run lint`; форма, список, фильтры и конфликт version проверены.
