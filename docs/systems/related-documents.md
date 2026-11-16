# Связанные документы

> **TL;DR:** Backend строит basis-дерево и группы обратных FK-ссылок, разрешает доступные карточки; frontend показывает дерево, hover preview и действия по metadata capabilities.
> **Тип:** Concept
> **Аудитория:** Developer
> **Связанные:** [CRUD](crud-pipeline.md), [Posting Engine](posting-engine.md)

## Backend

`RelatedDocRepo.FindRelatedDocuments` поднимается по `basis_type/basis_id` к корню, затем строит потомков обходом BFS по зарегистрированным document tables. Отдельно собираются FK-ссылки, не входящие в basis-цепочку. Batch resolver обогащает узлы по типу, без запроса на каждую карточку.

Ограничения: глубина подъёма 10, дерево до 100 узлов, до 5 элементов flat group; visited sets предотвращают циклы. Metadata/authorized read contracts определяют доступные сущности и поля; разрешение прочитать текущий документ не выдаёт доступ ко всем связанным объектам.

Preview fields объявляются инспектором metadata. Ссылочные поля используют `ref` и `label`; `preview:false` исключает поле. Системные ссылки ID, basis, author и parent исключаются. Поля и имена связанных объектов должны проходить тот же read policy, что обычное чтение.

## API

`GET /api/v1/document/{routePrefix}/{id}/related-documents` существует только при capability `relatedDocuments` и поддержке `DocumentRelatedDocsHandler`. Маршрут требует document read permission; handler/service выполняют дополнительные проверки доступа.

Ответ содержит optional `tree`, optional `flatGroups` и `total`. Узел дерева включает `RelatedDocItem`, `entityName`, `entityType`, `routePrefix`, `isCurrent` и optional `children`. Item содержит ID, presentation, number/date, posted/deletionMark, optional `amount`, `currencyId`, `previewData`.

`amount` — **строка**, не JSON number. `previewData` — словарь label → разрешённое отображаемое значение. Точные wire types генерируются в `frontend/types/generated/http-contracts.ts` (платформа) и `frontend/configuration/<имя>/types/generated/http-contracts.ts` (конфигурации); `frontend/types/common.ts` использует транспортные контракты.

## Frontend

Generic маршрут `frontend/app/(main)/documents/[routePrefix]/[id]/related/page.tsx` подключает `RelatedDocumentsPage`. Компонент сам вызывает `fetcher(documentId)`, хранит loading/error/data и повторяет загрузку после команды. Отдельного `useRelatedDocuments` с lazy sidebar/AbortController в проекте нет.

HoverCard показывает реквизиты карточки; context menu использует `DocumentActionConfig`, metadata capabilities и состояние документа. API paths строятся из routePrefix. Explicit `relatedConfigs` необязателен для обычного document routing: fallback берёт регистрацию из metadata. `createBasedOn` задаётся отдельно при наличии такого сценария.

Команды Post/Unpost/Deletion mark доступны только когда их допускают capability и состояние. Backend остаётся окончательной границей прав и lifecycle; наличие связанного узла не разрешает провести системный факт.

## Файловая карта

```path
internal/domain/related_documents.go
internal/infrastructure/storage/postgres/related_doc_repo.go
internal/infrastructure/storage/postgres/ref_resolver_repo.go
internal/infrastructure/storage/postgres/ref_finder_repo.go
internal/infrastructure/http/v1/handlers/related_documents.go
internal/infrastructure/http/v1/route_helpers.go
frontend/components/shared/related-documents-page.tsx
frontend/app/(main)/documents/[routePrefix]/[id]/related/page.tsx
frontend/types/common.ts
```
