# Добавление учётного документа

> **TL;DR:** Выберите жизненный цикл документа, опишите модель и движения, подключите service/repository, capabilities, DTO и HTTP-контракты; generic UI использует зарегистрированные возможности.
> **Тип:** How-To
> **Аудитория:** Developer
> **Связанные:** [Справочник](new-entity.md), [Posting Engine](../systems/posting-engine.md)

## 1. Выбор рабочего аналога

Текущая кодовая база содержит crypto-документы. Прежний пример возврата поставщику зависел от удалённых складов, номенклатуры, строк товаров и StockVisitor; он не является готовой основой нового документа.

| Сценарий | Аналог |
|---|---|
| Документ без табличных частей | `internal/domain/documents/crypto_invoice/` и `crypto_document_repo/crypto_invoice.go` (база — `document_repo/base.go`) |
| Системный внешний факт только для чтения | `internal/domain/documents/crypto_network_operation/` и `handlers.NewReadOnlyDocumentHandler` |
| Денежный workflow без generic CRUD/post/unpost | `WithdrawalRequestRegistration` и `CustomDocumentRegistration` |
| Регистрация маршрутов и metadata | `internal/content/document_registrations.go` |

Изучите целиком model, service, repository, handler и регистрацию выбранного аналога: разные документы имеют разные запреты на создание, проведение и отмену.

Для нового header-only extension-документа используйте:

```bash
go run ./cmd/scaffold --name adjustment_note --type document
```

Добавьте import пакета и `adjustment_note.Register(reg)` в `extensions/register.go`. Сервер и HTTP codegen используют общий список. Генератор создаёт document-модель, header repository/service, DTO с обязательной update-version, capabilities, HTTP-контракты и миграцию. Identity берётся из `entity.Definition`. Специфические движения и бизнес-правила добавляются вручную.

## 2. Таблица и domain-модель

Создайте новую исходную goose-миграцию по [правилам миграций](../../db/migrations/README.md). Для существующей сущности меняется владеющая миграция до production; заполненное окружение обновляется отдельно оператором.

Модель в `internal/domain/documents/<name>/` встраивает `entity.Document`. Этот тип содержит ID, номер/дату, audit, posted/version, basis и description, но **не OrganizationID**. Добавляйте организацию, merchant/token/wallet и другие измерения явно с FK и `meta:"ref:…"`.

Табличная часть нужна только когда она есть в бизнес-модели. Документ без строк не требует `doc_*_lines` или товарных справочников. Набор select/insert/update columns repository должен совпасть с моделью и DDL.

ID выделяется в Go до INSERT и проведения. `Validate(ctx)` выполняет чистые проверки без SQL/RPC. Точные crypto amounts хранятся как `RawAmount`; HTTP DTO сохраняет строковый arbitrary-precision контракт. Для ERP MinorUnits и Quantity действуют [JSON-контракты](../reference/json-contracts.md).

## 3. Движения и проведение

Реализуйте `posting.Postable` и source-интерфейсы нужных регистров. Смотрите `SettlementMovementSource` в `internal/domain/posting/visitor.go` и криптовые интерфейсы вроде `CryptoBalanceMovementSource` в `internal/domain/registers/cryptoposting/`. Они задают точные имена методов и типы срезов; общего обязательного метода `GenerateMovements()` нет.

Генерация движений детерминирована и не вызывает внешние сервисы. Visitor собирает движения, recorder проверяет остатки и записывает их в общей транзакции. Для нового регистра подключите visitor и recorder в composition root; существующий engine расширяется через `AddVisitor`/`AddRecorder`.

Проверьте `CanPost` и, если факт нельзя отменять, `CanUnpost`. Финансовая корректировка settled-факта может требовать нового компенсирующего документа; generic Unpost не является универсальным механизмом бизнес-отмены.

## 4. Repository и service

Для header-only документа используйте действующий `domain.BaseHeaderDocumentService` и соответствующий repository. `BaseDocumentService[T,L]` остаётся основой документов с табличными частями. Не добавляйте пустые строки только ради generic-типа.

В `Build(deps v1.DocumentDeps)` создайте repository, service и handler; подключите policy engine, audit и outbox decorators по выбранному аналогу. Оба базовых document service используют единый lifecycle: `BeforeCreate/Update/Delete`, разрешение валюты и выдача номера выполняются в транзакции/savepoint; `TransactionCreate/Update/Delete` — после записи и до внешнего коммита; `AfterCreate/Update/Delete` — после успешного внешнего коммита. Ошибка обязательного хука откатывает и запись, и изменения `Before*`, даже если внешний command обработал ошибку. Для пометки удаления доступны `TransactionSetDeletionMark` и `TransactionClearDeletionMark`. `PostAndSave` и `UpdateAndRepost` завершают соответствующие create/update hooks в транзакции проведения.

`CurrencyAwareDoc` — дополнительная возможность: реализуйте её только для документов с валютой. Crypto и не денежные документы не требуют пустых валютных методов. Доступный resolver не вызывается для документа без этого интерфейса.

Бизнес-изменение, обязательный аудит и enqueue должны быть атомарны. RPC, подпись и доставка выполняются вне короткой DB-транзакции через durable workflow.

Документ, который создаёт и проводит только системный workflow, регистрируется только для чтения: capabilities без create/update/post и `handlers.NewReadOnlyDocumentHandler` с DTO.

## 5. Регистрация, capabilities и транспорт

Модель предоставляет `EntityDefinition() entity.Definition`. Регистрация встраивает `v1.EntityRegistration[Model]`: identity, таблица и metadata-структура получаются из модели. `Build` собирает инфраструктуру, `HTTPContracts()` связывает конкретные DTO с разрешёнными capabilities. Для списка и детали с разными проекциями используйте `spec.EntityWithDetail[CreateDTO, UpdateDTO, ListDTO, DetailDTO]`. Optional contracts добавляют presentation, RLS/search fields и actions. Это общий способ для встроенных документов и расширений.

Каждая коллекция модели (срез структур) объявляется в Definition. Табличная часть со своей таблицей — `Parts: []entity.Part{{Key: "lines", Field: "lines"}}`: таблица называется `<Table>__<Key>`, `Field` — JSON-имя коллекции. Коллекция без своей таблицы указывается в `DerivedParts`. Без объявления регистрация метаданных отказывает. В SQL репозитория используйте `Definition.MustPartTable(key)`.

`RegisterDocumentRoutes` публикует только разрешённые capabilities и поддержанные handler-интерфейсы. Явно закройте неподдерживаемые Create/Delete/Copy/Post/Unpost. Для отдельного workflow используйте `CustomDocumentRegistration`.

Подключите фабрику через `internal/content/register.go`, permissions и назначения ролей — через seed. Опишите Create/Update/Response DTO, mapper, version и read projection. Стандартные HTTP method/path выводятся из Definition и capabilities. Custom workflow-команды добавляются в bundle той же регистрации. Codegen собирает bundles из общего factory registry; отдельного списка документов в `internal/infrastructure/http/contracts` нет. Обновите generated artifacts:

```bash
go run ./cmd/http-contracts
go run ./cmd/http-contracts -check
```

Для денежной команды определите обязательность `Idempotency-Key`, scope, fingerprint и replay; см. [жизненный цикл запроса](../guide/04-request-lifecycle.md).

## 6. Frontend

Generic list/detail маршруты находятся в `frontend/app/(main)/documents/[routePrefix]/`. Metadata и capabilities управляют доступными командами. Custom forms/renderers платформенных сущностей задаёт `frontend/lib/entity-registry-defaults.ts`, сущностей конфигурации — её `registerEntityOverlays(...)` (например, `frontend/configuration/crypto/lib/entity-overlays.ts`); отдельные list/workflow pages нужны только при специализированном сценарии.

API consumer использует generated wire types, Zod schemas и зарегистрированный route contract. AutoForm проецирует writable fields конкретного request DTO. Для custom формы подключите dirty state, валидацию и обработку version conflict по существующему аналогу. `useFormValidation` использует `data-field` для фокуса и отображения ошибки. `useShortcut` принимает `mod+s`/`mod+f` для macOS и Windows/Linux.

## Проверка

- [ ] SQL, модель и repository согласованы; ID известен до проведения.
- [ ] Источники движений, visitor/recorder, balance checks и запреты отмены проверены профильными тестами.
- [ ] Create/Update/Post/Unpost публикуются только по capabilities; права и strict guard проверяются backend.
- [ ] Audit/outbox и business write находятся в одной транзакции; внешние вызовы вынесены в workflow.
- [ ] DTO и route contracts зарегистрированы, `go run ./cmd/http-contracts -check` проходит.
- [ ] `go build ./...`, профильные тесты; из `frontend`: `npm run typecheck`, `npm run lint`.
- [ ] В UI проверены список, ссылки, сохранение, ошибки, dirty state и допустимые действия.
