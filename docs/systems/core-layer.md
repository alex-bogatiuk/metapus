# Слой Core

> **TL;DR:** Core содержит базовые сущности, ошибки, UUIDv7, точные числовые типы, security- и tenant-контракты; он не зависит от Domain, HTTP или PostgreSQL.
> **Тип:** Concept
> **Аудитория:** Developer
> **Связанные:** [Архитектура](../guide/02-architecture.md)

## Композиция сущностей

| Тип | Поля и назначение |
|---|---|
| `BaseEntity` | `ID`, `DeletionMark`, `Version`, `Attributes`, встроенные `CDCFields` |
| `BaseCatalog` | Встраивает `BaseEntity`; не добавляет code/name или audit timestamps |
| `Catalog` | Встраивает `BaseCatalog`, добавляет `Code`, `Name`, `ParentID`, `IsFolder` |
| `BaseDocument` | Встраивает `BaseEntity`, добавляет `CreatedAt`, `UpdatedAt`, `CreatedBy`, `UpdatedBy` |
| `Document` | Встраивает `BaseDocument`, добавляет `Number`, `Date`, `Posted`, `PostedVersion`, `BasisType`, `BasisID`, `Description` |

`OrganizationID` не входит в базовый документ: конкретная модель добавляет его при необходимости. Иерархия справочника включается его metadata. `Attributes` хранится в JSONB и содержит дополнительные реквизиты.

Конструкторы `NewBaseEntity`, `NewCatalog`, `NewDocument` выделяют UUIDv7 через `id.New()`. `TypedRef` хранит полиморфную ссылку (`RefType`, `RefID`); traits в `entity/traits.go` добавляют общие возможности через embedding.

## Ошибки и транзакционный context

`apperror.AppError` хранит code, message, details, HTTP status и внутреннюю причину. HTTP возвращает собственный JSON `{code, message, details}`; RFC 7807 Problem Details не реализован. Handler передаёт ошибку в `c.Error(err)`.

`tenant.WithTxManager`, `tenant.GetTxManager` и `tenant.MustGetTxManager` передают абстрактный `tx.Manager` через context. PostgreSQL-репозитории используют инфраструктурный `postgres.MustGetTxManager`, чтобы получить конкретный querier. `tx.OnCommit` откладывает необязательную постобработку до коммита внешней транзакции.

## Файловая карта

```path
internal/core/entity/base.go
internal/core/entity/catalog.go
internal/core/entity/document.go
internal/core/entity/traits.go
internal/core/entity/typed_ref.go
internal/core/apperror/error.go
internal/core/tenant/context.go
internal/core/tx/commit.go
```
