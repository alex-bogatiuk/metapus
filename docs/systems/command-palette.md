# Command Palette (Ctrl+K)

> **TL;DR:** Глобальная точка входа для навигации, поиска по данным, контекстных действий, калькулятора и **предпросмотра сущностей**. Вызывается через `Ctrl+K` или кнопку «Поиск» в хедере. Объединяет в себе `FavoritesPopover`, `RecentPopover`, глобальный поиск по бизнес-данным и preview panel.

> **Тип:** Concept
> **Аудитория:** Frontend Developer, AI-Agent
> **Связанные:** [reference/keyboard-shortcuts.md](../reference/keyboard-shortcuts.md), [systems/dashboard-widgets.md](dashboard-widgets.md)

---

## 1. Архитектура

Command Palette — клиентская система, построенная на shadcn/ui `<Command>` (cmdk) + `<Dialog>`.
Монтируется один раз в `AppShell`, доступна из любого экрана.

```
AppShell
  └── CommandPalette
        ├── useCommandPaletteStore   (Zustand: isOpen, contextActions)
        ├── useFavoritesStore        (избранное пользователя)
        ├── useRecentStore           (недавние документы)
        ├── useMetadataStore         (метаданные сущностей для группировки результатов)
        ├── command-nav-items.ts     (статический + динамический индекс навигации)
        ├── calc-engine.ts           (встроенный калькулятор)
        ├── api.search.query()       (клиент глобального поиска по данным)
        ├── api.search.preview()     (клиент предпросмотра сущности)
        └── EntityPreviewCard        (универсальная карточка предпросмотра)
```

## 2. Режимы работы

### Zero-state (пустой запрос)

При открытии палитры без ввода отображаются секции (в порядке приоритета):

| Секция | Источник данных | Лимит |
|--------|----------------|-------|
| ⚡ **Действия** | `useCommandPaletteStore.contextActions` | все |
| ⭐ **Избранное** | `useFavoritesStore.items` | 7 |
| 🕒 **Недавние** | `useRecentStore.items` | 7 |

На элементах Избранного при выделении появляется кнопка `StarOff` — снятие из избранного прямо в палитре.

### Search-state (ввод текста)

Fuzzy-поиск по навигационному индексу. Результаты группируются по секциям:

| Секция | Ключ | Пример элементов |
|--------|------|------------------|
| **Перейти** | `navigate` | Контрагенты, Номенклатура, Склады |
| **Создать** | `create` | Создать поступление, Создать реализацию |
| **Отчёты** | `report` | Остатки товаров, Оборотная ведомость |
| **Система** | `system` | Настройки, Администрирование |

Индекс строится из двух источников:
1. **Статический** — захардкоженные пункты с русскими ключевыми словами (`command-nav-items.ts`)
2. **Динамический** — сущности из `useMetadataStore`, не покрытые статическим списком

### Calc-state (математическое выражение)

Если ввод содержит цифры и оператор (`+−*/^%`), активируется встроенный калькулятор.
Парсер: recursive descent (без `eval`/`Function` — полная безопасность).

| Ввод | Результат |
|------|-----------|
| `150 * 1.2` | `180` |
| `200000 * 15%` | `30 000` |
| `2 + 3 * 4` | `14` (приоритет операций) |
| `(100 + 50) * 2` | `300` |
| `2 ** 10` или `2^10` | `1 024` |

`Enter` на результате → копирование числа в буфер обмена + toast.

### Data-search-state (префикс `>`)

Если ввод начинается с `>`, активируется **глобальный поиск по бизнес-данным** (Global Data Search).
Запрос отправляется к `GET /api/v1/search` после debounce 300ms, минимум 2 символа после `>`.

**Примеры:**

| Ввод | Что произойдёт |
|------|----------------|
| `>ООО Бета` | Найдёт контрагентов по имени |
| `>500000000280` | Найдёт контрагента по ИНН |
| `>GR-SEED` | Найдёт поступления по номеру |
| `>cp052@seed` | Найдёт контрагента по email |
| `>NM-GEN-018` | Найдёт номенклатуру по коду |

**Поведение в data-search-state:**
- Избранное, Недавние и навигационные группы **скрыты** — отображаются только результаты поиска
- Результаты **группируются по типу сущности** (e.g. «Контрагенты», «Поступления товаров»)
- Каждый результат показывает иконку entity, заголовок (title) и подзаголовок (subtitle)
- Клик / `Enter` по результату → навигация на карточку сущности + открытие вкладки
- `forceMount` на результатах обходит клиентскую фильтрацию cmdk

**Зачем префикс `>` вместо автоматического поиска:**

| Без префикса | С префиксом |
|-------------|-------------|
| Ввод `1000` → калькулятор, без запроса к серверу | `>1000` → поиск по данным (e.g. ИНН, код) |
| Ввод `Контрагенты` → навигация к списку | `>Иванов` → поиск конкретного контрагента |
| Нулевая нагрузка на API | Запрос только по явному намерению пользователя |

## 3. Контекстные действия (useCommandActions)

Любой компонент может зарегистрировать свои действия в палитре через хук `useCommandActions`.
Действия появляются при маунте компонента и автоматически удаляются при анмаунте.

```tsx
// frontend/app/(main)/documents/goods-receipts/[id]/page.tsx
import { useCommandActions } from "@/hooks/useCommandActions"
import { Play, Printer } from "lucide-react"

const actions = useMemo(() => [
  { id: "post",  label: "Провести документ", icon: Play,    shortcut: ["Ctrl", "Enter"], action: handlePost },
  { id: "print", label: "Печать формы",      icon: Printer, action: handlePrint },
], [handlePost, handlePrint])

useCommandActions("goods-receipt-form", actions)
```

**Контракт `CommandAction`:**

| Поле | Тип | Обязательное | Описание |
|------|-----|-------------|----------|
| `id` | `string` | ✅ | Уникальный идентификатор действия |
| `label` | `string` | ✅ | Текст, отображаемый в палитре |
| `action` | `() => void` | ✅ | Callback при выборе |
| `icon` | `LucideIcon` | — | Иконка (по умолчанию Zap) |
| `shortcut` | `string[]` | — | Подсказка клавиш (e.g. `["Ctrl", "Enter"]`) |
| `group` | `string` | — | Название группы (по умолчанию «Действия») |

`sourceId` (первый аргумент хука) — ключ атомарной регистрации. Один компонент = один sourceId. При ре-рендере массив действий заменяется целиком.

## 4. Навигация из палитры

Выбор элемента навигации выполняет два действия:
1. **Открывает вкладку** через `useTabsStore.openTab()` (или активирует существующую)
2. **Навигирует** через `router.push(url)`

Это гарантирует, что Command Palette и Tab Bar всегда синхронизированы.

## 5. Горячие клавиши

| Клавиша | Действие |
|---------|----------|
| `Ctrl+K` | Открыть / закрыть палитру |
| `↑ / ↓` | Перемещение по элементам |
| `Enter` | Выбрать элемент (навигация / действие / копировать результат) |
| `→` | Открыть Preview Panel для выделенного элемента поиска (desktop ≥1024px) |
| `Esc` | Закрыть Preview Panel (если открыт), затем закрыть палитру |

Шорткат `Ctrl+K` регистрируется через `useShortcut` (стандартный механизм) с приоритетом `general`.

### 5.1. Preview Panel (Предпросмотр)

При нажатии `→` на выделенном элементе в режиме data search (`>`) палитра расширяется до двухколоночного layout:

- **Левая колонка** (500px): CommandList с результатами поиска
- **Правая колонка** (320px): `EntityPreviewCard` с деталями сущности

**Поведение:**
- Для справочников: название, код + скалярные поля (ИНН, телефон, email) через `meta:"preview:true"`
- Для документов: номер, дата, статус (badge), сумма + FK-ссылки (контрагент, склад)
- Повторное `Esc` — скрывает preview (палитра сужается обратно)
- Desktop-only: на экранах < 1024px клавиша `→` не активирует preview
- Кнопка «Открыть» внизу карточки — переходит к сущности

## 6. Файловая карта

```
frontend/stores/useCommandPaletteStore.ts  — Zustand store: isOpen, actionsBySource, version
frontend/hooks/useCommandActions.ts        — Хук регистрации контекстных действий
frontend/lib/command-nav-items.ts          — Статический + динамический навигационный индекс
frontend/lib/calc-engine.ts               — Recursive descent parser для калькулятора
frontend/components/layout/command-palette.tsx — UI компонент (cmdk + Dialog + preview panel)
frontend/components/shared/entity-preview-card.tsx — Универсальная карточка предпросмотра сущности
frontend/components/ui/command.tsx         — shadcn/ui обёртка cmdk (dialogClassName для расширения)
frontend/types/search.ts                  — TypeScript типы: SearchResultItem, SearchResponse, PreviewResponse
frontend/lib/api.ts → api.search.query()  — Клиент глобального поиска
frontend/lib/api.ts → api.search.preview() — Клиент предпросмотра сущности
internal/domain/search/service.go          — UNION ALL SQL builder с RLS-инъекцией
internal/domain/search/preview.go          — Standalone entity preview (scalar + FK-resolved fields)
internal/infrastructure/http/v1/handlers/global_search.go — GET /api/v1/search handler
internal/infrastructure/http/v1/handlers/entity_preview.go — GET /api/v1/search/preview handler
internal/platform/catalog_contract.go      — SearchFieldsProvider, RLSProvider интерфейсы
internal/metadata/registry.go → SearchColumns — Конфигурация поисковых полей в EntityDef
internal/metadata/inspector.go             — Inspect(): preview:true/false meta-теги, PreviewFieldDef
```

## 7. Дизайн-решения

| Решение | Обоснование |
|---------|-------------|
| `forceMount` для калькулятора и поиска | cmdk фильтрует элементы по value — калькулятор и серверные результаты не матчатся с запросом, поэтому используем `forceMount` для обхода фильтрации |
| Controlled `search` state | Нужен доступ к сырому вводу для детекции math-выражений и `>` префикса |
| `Map<sourceId, CommandAction[]>` | Атомарная замена действий по sourceId без гонок между несколькими компонентами |
| Recursive descent parser | Безопасность (нет eval), zero dependencies, полный контроль (поддержка `%`, `^`, пробелов в числах) |
| `scrollbar-thin` CSS утилита | Стилизованный скроллбар вместо нативного, согласованный с shadcn design tokens |
| Префикс `>` для data search | Разделение intent: навигация/калькулятор — без нагрузки на API; поиск по данным — только по явному запросу пользователя |
| UNION ALL SQL | Один round-trip к БД для поиска по всем сущностям. RLS-условия инжектируются в каждый subquery |
| Metadata-driven search fields | Каждая фабрика декларирует свои searchable-поля через `SearchFieldsProvider`. Без реализации — дефолт (name+code / number) |
| Conditional `<CommandEmpty>` | cmdk не видит `forceMount`-группы при подсчёте результатов — «Ничего не найдено» скрывается когда есть серверные результаты или идёт загрузка |
| Desktop-only preview (≥1024px) | ERP — десктопный инструмент. Preview — бонус для больших экранов. Паттерн macOS Spotlight |
| `dialogClassName` для Command Dialog | Динамическое расширение диалога до 820px при активации preview, без CSS hacks |
| `meta:"preview:true"` для скалярных полей | Позволяет разработчику декларативно пометить поля (ИНН, телефон) для отображения в preview card |
| Standalone preview endpoint | Отдельный `GET /search/preview` вместо переиспользования `related-documents` — разные контексты, разный объём данных |

## Связанные документы

- [reference/keyboard-shortcuts.md](../reference/keyboard-shortcuts.md) — реестр всех горячих клавиш, включая `Ctrl+K`
- [systems/smart-data-entry.md](smart-data-entry.md) — паттерны умного ввода (калькулятор — часть этой парадигмы)

---

## Приложение A: Глобальный поиск — Backend-архитектура

### API-контракт

```
GET /api/v1/search?q=<query>&limit=<N>
Headers: Authorization: Bearer <token>, X-Tenant-ID: <uuid>
```

**Response:**
```json
{
  "query": "ООО",
  "results": [
    {
      "entityType": "catalog",
      "entityName": "Counterparty",
      "entityKey": "counterparty",
      "entityId": "019dd43d-...",
      "title": "ООО \"Бета\"",
      "subtitle": "CP-GEN-142",
      "url": "/catalogs/counterparties/019dd43d-..."
    }
  ]
}
```

### SQL-архитектура

```sql
-- Генерируется динамически из metadata.Registry
(SELECT id::text, name AS title, COALESCE(code, '') AS subtitle,
 'catalog' AS entity_type, 'Counterparty' AS entity_name, ...
 FROM cat_counterparties
 WHERE deletion_mark = false
   AND (name ILIKE $1 OR code ILIKE $1 OR inn ILIKE $1 OR phone ILIKE $1 OR email ILIKE $1)
 ORDER BY name LIMIT 5)
UNION ALL
(SELECT id::text, number AS title, '' AS subtitle,
 'document' AS entity_type, 'GoodsReceipt' AS entity_name, ...
 FROM doc_goods_receipts
 WHERE (number ILIKE $1)
   AND organization_id IN ($2, $3)  -- ← RLS из DataScope
 ORDER BY date DESC LIMIT 5)
```

### Расширение поисковых полей (SearchFieldsProvider)

По умолчанию все справочники ищутся по `name` + `code`, документы — по `number`.
Чтобы добавить свои поля, фабрика реализует интерфейс `platform.SearchFieldsProvider`:

```go
// internal/content/catalog_registrations.go
func (r *CounterpartyRegistration) SearchableFields() platform.SearchFields {
    return platform.SearchFields{
        SearchCols:  []string{"name", "code", "inn", "phone", "email"},
        TitleCol:    "name",
        SubtitleCol: "code",
    }
}
```

**Контракт `platform.SearchFields`:**

| Поле | Тип | Описание |
|------|-----|----------|
| `SearchCols` | `[]string` | DB-колонки для ILIKE-matching (e.g. `["name", "code", "inn"]`) |
| `TitleCol` | `string` | Колонка для заголовка в результатах |
| `SubtitleCol` | `string` | Колонка для подзаголовка (пустая строка = нет) |

Если интерфейс не реализован, применяются дефолты по типу сущности.

### RLS-безопасность

Для документов с ограниченным доступом фабрика реализует `platform.RLSProvider`:

```go
func (r *GoodsReceiptRegistration) RLSDimensions() map[string]string {
    return map[string]string{"organization": "organization_id"}
}
```

При выполнении поиска `search.Service` берёт разрешённые organization ID из `security.DataScope` текущего пользователя и инжектирует `WHERE organization_id IN (...)` в соответствующий subquery. Admin-пользователи обходят RLS.

---

## Приложение B: Preview Panel — Backend-архитектура

### API-контракт

```
GET /api/v1/search/preview?entityType=catalog&entityKey=counterparty&id=<uuid>
Headers: Authorization: Bearer <token>, X-Tenant-ID: <uuid>
```

**Response:**
```json
{
  "entityType": "catalog",
  "entityKey": "counterparty",
  "entityName": "Counterparty",
  "title": "ООО \"Бета\"",
  "subtitle": "CP-GEN-142",
  "fields": [
    { "label": "Тип", "value": "Контрагент" },
    { "label": "ИНН", "value": "7707083893" },
    { "label": "Телефон", "value": "+7 (495) 111-22-33" },
    { "label": "Email", "value": "info@beta.ru" }
  ],
  "references": {},
  "url": "/catalogs/counterparties/019dd43d-..."
}
```

Для документов — дополнительные поля:
```json
{
  "entityType": "document",
  "fields": [
    { "label": "Тип", "value": "Поступление товаров" },
    { "label": "Дата", "value": "15.03.2026" },
    { "label": "Статус", "value": "Проведён" },
    { "label": "Сумма", "value": "1500000" }
  ],
  "references": {
    "Контрагент": "ООО Ромашка",
    "Склад": "Основной склад"
  }
}
```

### Скалярные preview-поля (`meta:"preview:true"`)

По умолчанию `Inspect()` собирает только `id.ID` reference-поля (документы). Чтобы добавить скалярные поля (ИНН, телефон) в preview card, используйте мета-тег:

```go
// internal/domain/catalogs/counterparty/model.go
type Counterparty struct {
    entity.Catalog
    INN   *string `db:"inn"   json:"inn"   meta:"label:ИНН,preview:true"`
    Phone *string `db:"phone" json:"phone" meta:"label:Телефон,preview:true"`
    Email *string `db:"email" json:"email" meta:"label:Email,preview:true"`
}
```

**Мета-теги preview:**

| Тег | Назначение |
|-----|------------|
| `meta:"preview:true"` | Скалярное поле включается в preview (ИНН, телефон, код) |
| `meta:"preview:false"` | Reference-поле исключается из preview (opt-out для документов) |
| _(без тега)_ | Reference-поля на документах включаются автоматически; скалярные — не включаются |

### Архитектура preview-запроса

```sql
-- Catalog preview (counterparty)
SELECT d.name AS title, COALESCE(d.code, '') AS subtitle,
       d.inn AS pv_scalar_inn, d.phone AS pv_scalar_phone, d.email AS pv_scalar_email
FROM cat_counterparties d
WHERE d.id = $1

-- Document preview (goods_receipt) — with LEFT JOIN for FK references
SELECT d.number AS title, '' AS subtitle,
       d.date, d.posted, d.deletion_mark, d.total_amount, d.currency_id,
       pv0.name AS pv0_name, pv1.name AS pv1_name
FROM doc_goods_receipts d
LEFT JOIN cat_counterparties pv0 ON pv0.id = d.counterparty_id
LEFT JOIN cat_warehouses pv1 ON pv1.id = d.warehouse_id
WHERE d.id = $1
```

### Связь с Related Documents Preview

`EntityPreviewCard` (Command Palette) и `DocumentPreviewCard` (Related Documents) — **отдельные компоненты**:

| | EntityPreviewCard | DocumentPreviewCard |
|---|---|---|
| **Контекст** | Command Palette (Ctrl+K) | Related Documents (дерево подчинённости) |
| **Data source** | Standalone `GET /search/preview` | Inline из `RelatedDocTreeNode.previewData` |
| **Entity types** | Каталоги + документы | Только документы |
| **Backend** | `search.Service.Preview()` | `RefResolverRepo.ResolveRefs()` |
| **Общее** | `PreviewFieldDef` метаданные, `meta:"preview:true/false"` теги |
