# Связанные документы (Related Documents)

> **TL;DR:** Система автоматически строит дерево подчиненности документов (на основе `basis_id`) и генерирует карточки предпросмотра (Preview Cards) через `metadata.Inspector`. Frontend отображает рекурсивное дерево с hover-превью, context menu и quick actions.

> **Тип:** Concept
> **Аудитория:** Developer, AI-Agent
> **Связанные:** [posting-engine.md](posting-engine.md), [crud-pipeline.md](crud-pipeline.md), [core-layer.md](core-layer.md)

---

## 1. Архитектурный обзор

Подсистема связанных документов — аналог «Структуры подчинённости» 1C и Document Flow SAP. Состоит из двух независимых механизмов:

| Механизм | Источник данных | Результат |
|----------|----------------|-----------|
| **Дерево подчинённости** | `basis_type` + `basis_id` в документах | Рекурсивное дерево `RelatedDocTreeNode` |
| **FK-ссылки (Flat Groups)** | `RefFinderRepo` — обратный поиск FK | Плоские группы `RelatedDocGroup` |

Оба механизма объединяются в единый ответ `RelatedDocumentsResult`.

---

## 2. Карточки предпросмотра (Preview Cards)

В интерфейсе «Связанные документы» при наведении на документ всплывает карточка с ключевой информацией (Контрагент, Склад и т.д.). Разработчику **не нужно настраивать эти карточки вручную**.

### Zero-Config механизм

1. При запуске сервера `metadata.Inspect()` сканирует Go-структуру каждого документа.
2. Любое поле типа `id.ID` (или `*id.ID`), ссылающееся на справочник, автоматически попадает в `PreviewFields`. Имя берётся из тега `meta:"label:Поставщик"`, fallback — `guessLabel(FieldName)`.
3. Системные ссылки исключаются:
   ```go
   // metadata/inspector.go
   var previewSkipFields = map[string]bool{
       "id": true, "organizationId": true, "basisId": true, "basisType": true,
       "createdBy": true, "updatedBy": true, "parentId": true,
   }
   ```
4. Чтобы скрыть бизнес-ссылку из превью вручную: `meta:"preview:false"`.

### SQL-генерация (N+1 → 1 запрос)

`RefResolverRepo.ResolveRefs()` группирует документы по типу и строит один SQL per entity type с динамическими `LEFT JOIN`:

```sql
SELECT d.id, d.number, d.date, d.posted, d.deletion_mark,
       d.total_amount, d.currency_id,
       pv0.name AS pv0_name,  -- Контрагент
       pv1.name AS pv1_name   -- Склад
FROM doc_goods_receipts d
LEFT JOIN cat_counterparties pv0 ON pv0.id = d.counterparty_id
LEFT JOIN cat_warehouses     pv1 ON pv1.id = d.warehouse_id
WHERE d.id IN ($1, $2, ...)
```

Результат записывается в `PreviewData: map[string]string` (`label → resolved name`).

---

## 3. Дерево подчинённости (Hierarchy Tree)

### Алгоритм обхода (`RelatedDocRepo.FindRelatedDocuments`)

**Шаг 1 — Walk UP.** Рекурсивно идёт вверх по `basis_type/basis_id`, чтобы найти корень цепочки. Защита от циклов — `visited` set.

**Шаг 2 — BFS DOWN.** От корня строит полное дерево вниз. Один `UNION ALL` запрос per BFS level по всем зарегистрированным типам документов:

```sql
SELECT 'GoodsIssue' as child_type, id, basis_type, basis_id
FROM doc_goods_issues
WHERE (basis_type, basis_id) IN (SELECT unnest($1::text[]), unnest($2::uuid[]))
UNION ALL
SELECT 'GoodsReceipt' as child_type, id, basis_type, basis_id
FROM doc_goods_receipts
WHERE (basis_type, basis_id) IN (SELECT unnest($1::text[]), unnest($2::uuid[]))
LIMIT ...
```

**Шаг 3 — Batch Resolve.** Все узлы дерева резолвятся через `RefResolverRepo.ResolveRefs()` (один batch-запрос per entity type). Результат включает `presentation`, `number`, `date`, `posted`, `amount`, `previewData`.

**Шаг 4 — Convert.** `rawTreeNode` → `RelatedDocTreeNode` с рекурсивной сортировкой children по дате.

**Шаг 5 — FK Flat Groups.** Документы, ссылающиеся на текущий по FK (не через `basis_id`), собираются через `RefFinderRepo.FindReferences()` и тоже batch-резолвятся.

### Safety Limits

| Параметр | Значение | Назначение |
|----------|----------|------------|
| `_maxTreeDepth` | 10 | Максимальная глубина Walk UP |
| `_maxTreeNodes` | 100 | Максимум узлов в дереве (BFS) |
| `_maxItemsPerGroup` | 5 | Элементов в одной flat group |
| Cycle protection | `visited` / `allKeysSet` | Предотвращение зацикливания |

---

## 4. API-контракт

### Endpoint

```
GET /api/v1/document/{type}/{id}/related-documents
```

Маршрут монтируется автоматически для каждого типа документа через `RegisterDocumentRoutes()`:

```go
// route_helpers.go
group.GET("/:id/related-documents",
    middleware.RequirePermission(permission+":read"),
    relatedHandler.GetRelatedDocuments,
)
```

### Response DTO

```jsonc
{
  "tree": {                          // RelatedDocTreeNode (корень дерева)
    "id": "uuid",
    "entityName": "GoodsReceipt",
    "entityType": "document",
    "routePrefix": "goods-receipt",
    "presentation": "Поступление товаров ПТ-00042 от 15.03.2026",
    "number": "ПТ-00042",
    "date": "2026-03-15T10:00:00Z",
    "posted": true,
    "deletionMark": false,
    "amount": 1500000,               // MinorUnits
    "currencyId": "uuid",
    "isCurrent": true,               // документ, для которого вызван endpoint
    "previewData": {                  // auto-populated из PreviewFields
      "Контрагент": "ООО Ромашка",
      "Склад": "Основной склад"
    },
    "children": [                     // дочерние документы
      { "entityName": "GoodsIssue", ... }
    ]
  },
  "flatGroups": [                    // FK-ссылки вне basis-цепочки
    {
      "entityName": "GoodsIssue",
      "entityType": "document",
      "presentation": "Реализации товаров",
      "routePrefix": "goods-issue",
      "items": [ { "id": "...", "presentation": "...", ... } ],
      "totalCount": 3
    }
  ],
  "total": 5                         // tree nodes + flat group items
}
```

### TypeScript-типы (`frontend/types/common.ts`)

```typescript
interface RelatedDocumentsResponse {
  tree?: RelatedDocTreeNode
  flatGroups?: RelatedDocGroup[]
  total: number
}

interface RelatedDocTreeNode extends RelatedDocItem {
  entityName: string
  entityType: "document" | "catalog"
  routePrefix: string
  isCurrent: boolean
  children?: RelatedDocTreeNode[]
}

interface RelatedDocItem {
  id: string
  presentation: string
  number: string
  date: string
  posted: boolean
  deletionMark: boolean
  amount?: number
  currencyId?: string
  previewData?: Record<string, string>  // label → resolved name
}

interface RelatedDocGroup {
  entityName: string
  entityType: "catalog" | "document"
  presentation: string
  routePrefix: string
  items: RelatedDocItem[]
  totalCount: number
}
```

---

## 5. Frontend-архитектура

### Компоненты

| Компонент | Файл | Назначение |
|-----------|------|------------|
| `RelatedDocumentsPage` | `components/shared/related-documents-page.tsx` | Full-page view: дерево + flat groups + actions |
| `TreeNodeComponent` | (внутренний) | Рекурсивный узел дерева с expand/collapse |
| `FlatGroup` | (внутренний) | Группа FK-ссылок с tree connectors (├, └) |
| `DocumentPreviewCard` | (внутренний) | Hover-карточка: номер, дата, статус, сумма + `previewData` |
| `DocumentContextMenu` | (внутренний) | Контекстное меню: Открыть, Провести, Отменить, Удалить, Создать на основании |
| `StatusBadge` / `StatusIcon` | (внутренний) | Визуальный статус: Проведён ✓ / Черновик ○ / Удалён ✕ |

### Hook `useRelatedDocuments`

Lazy-loading hook для sidebar-интеграции:

```typescript
const { groups, tree, loading, refresh } = useRelatedDocuments({
  fetcher: (id) => api.goodsReceipts.getRelatedDocuments(id),
  documentId: params.id,
  enabled: !sidebarCollapsed,  // lazy: fetch only when visible
})
```

- `AbortController` — отмена in-flight запросов при навигации
- `fetchedRef` — дедупликация запросов для одного documentId
- `flattenTreeToGroups()` — backward-compat: tree → flat groups для sidebar-рендера

### Hover Preview (HoverCard)

Используется shadcn `HoverCard` с `openDelay={400}` и `closeDelay={100}`:

```tsx
<HoverCard openDelay={400} closeDelay={100}>
  <HoverCardTrigger asChild>
    {/* tree node row */}
  </HoverCardTrigger>
  <HoverCardContent className="w-80" side="right" align="start">
    <DocumentPreviewCard item={node} entityTypeLabel={node.entityName} />
  </HoverCardContent>
</HoverCard>
```

Карточка отображает:
- Тип + номер документа
- Статус (Badge: Проведён / Черновик / Удалён)
- Дата и сумма (с валютным форматированием через `useCurrencyScale`)
- **Динамические поля** из `previewData` — разделены `border-t`, каждое: `label: value`

### Context Menu (Quick Actions)

Контекстное меню (ПКМ) на каждом узле дерева и flat group:

| Действие | API | Условие |
|----------|-----|---------|
| Открыть | навигация | всегда |
| Провести | `POST /{type}/{id}/post` | `!posted && !deletionMark` |
| Отменить проведение | `POST /{type}/{id}/unpost` | `posted` |
| Пометить на удаление | `POST /{type}/{id}/deletion-mark` | всегда |
| Создать на основании | навигация `?basisType=...&basisId=...` | если есть `createBasedOn` config |

После каждого action → toast (sonner) + auto-refresh дерева.

### Tab Title

Динамический заголовок таба через `useTabTitle`:

```
Связанные: №GR-SEED-01371 (Поступление товаров)
```

---

## 6. Расширение: добавление нового типа документа

При регистрации нового документа в `metadata.Registry` связанные документы **подхватываются автоматически**:

1. **Preview Fields** — `Inspect()` сканирует struct, `id.ID`-поля попадают в `PreviewFields`.
2. **Дерево** — если документ содержит `basis_type`/`basis_id` → BFS найдёт его как потомка.
3. **FK-ссылки** — `RefFinderRepo` сканирует все таблицы, находя обратные ссылки.
4. **Frontend** — `relatedConfigs` в `RelatedDocumentsPage` определяет action routing для нового типа.

Единственная ручная настройка — `createBasedOn` options в frontend config (опционально).

---

## 7. Файловая карта

```
internal/domain/related_documents.go       — типы: RelatedDocumentsRequest/Result, RelatedDocTreeNode, RelatedDocFinder
internal/domain/ref_resolver.go            — типы: RefResolveRequest/Result, RefResolver interface
internal/metadata/inspector.go             — Inspect(): auto-collect PreviewFields, previewSkipFields, metaHasPreviewFalse
internal/metadata/registry.go              — PreviewFieldDef, EntityDef.PreviewFields
internal/infrastructure/storage/postgres/
  related_doc_repo.go                      — RelatedDocRepo: Walk UP + BFS DOWN + batch resolve
  ref_resolver_repo.go                     — RefResolverRepo: dynamic LEFT JOIN для preview + batch resolve
  ref_finder_repo.go                       — RefFinderRepo: обратный поиск FK-ссылок
internal/infrastructure/http/v1/
  handlers/related_documents.go            — RelatedDocumentsHandler (generic, per-entity)
  handlers/goods_receipt.go                — GetRelatedDocuments() → delegate to RelatedDocumentsHandler
  route_helpers.go                         — RegisterDocumentRoutes: auto-mount /:id/related-documents
frontend/types/common.ts                   — RelatedDocumentsResponse, RelatedDocTreeNode, RelatedDocItem, RelatedDocGroup
frontend/hooks/useRelatedDocuments.ts       — lazy-loading hook с AbortController
frontend/components/shared/
  related-documents-page.tsx               — full-page view: дерево, hover preview, context menu, quick actions
frontend/lib/api.ts                        — createDocumentApi().getRelatedDocuments()
```

---

## Связанные документы

- [posting-engine.md](posting-engine.md) — движок проведения, генерирует basis-цепочки
- [crud-pipeline.md](crud-pipeline.md) — generic CRUD, в который встроен related-documents endpoint
- [core-layer.md](core-layer.md) — базовые типы: `entity.TypedRef`, `id.ID`
