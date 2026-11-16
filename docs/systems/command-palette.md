# Command Palette (Ctrl+K)

> **TL;DR:** Глобальная точка входа для навигации, открытия страниц по ссылке, поиска по данным, контекстных действий, калькулятора и **предпросмотра сущностей**. Вызывается через `Ctrl+K`, `Ctrl+L` или кнопку «Поиск» в хедере. Объединяет в себе `FavoritesPopover`, `RecentPopover`, глобальный поиск по бизнес-данным и preview panel.

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
        ├── command-intent.ts        (взаимоисключающие режимы и проверка внутренних URL)
        ├── navigation.ts            (реестр навигации: меню, системные ссылки, пункты палитры)
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
| **Перейти** | `navigate` | Контрагенты, Мерчанты, Кошельки |
| **Создать** | `create` | Действия создания сущностей с capability create |
| **Отчёты** | `report` | Журнал документов |
| **Система** | `system` | Настройки, Администрирование |

Индекс строится из двух источников:
1. **Статический** — пункты с русскими ключевыми словами из реестра навигации (`lib/navigation.ts`): платформенные и добавленные конфигурацией через `registerNavigation({ commands })`; пункт с `permission` виден только при этом праве
2. **Динамический** — сущности из `useMetadataStore`, не покрытые статическим списком

### URL-state (внутренняя ссылка)

Путь, начинающийся с `/`, или абсолютный URL текущего домена активирует режим открытия страницы. Палитра скрывает остальные группы и показывает одну команду «Открыть страницу». Переход выполняется только после `Enter` или клика.

| Ввод | Результат |
|------|-----------|
| `/documents/crypto-invoice/…` | Открыть внутреннюю страницу |
| `https://<текущий-домен>/catalogs/…` | Нормализовать до внутреннего пути и открыть страницу |
| `https://example.com/…` | Показать ограничение: ссылка ведёт за пределы Metapus |
| `javascript:…` или некорректный URL | Показать ошибку распознавания |

Query-параметры и hash сохраняются в адресе перехода. Идентификатор вкладки строится только из `pathname`, поэтому разные состояния одной страницы не создают дубликаты вкладок.

`Ctrl+L` открывает эту же палитру с текущим внутренним URL, выделенным целиком. Это сохраняет быстрый сценарий вставки ссылки без отдельного элемента в хедере.

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
| `>CI-` | Найдёт инвойсы по номеру |
| `>cp052@seed` | Найдёт контрагента по email |
| `>USDT` | Найдёт токены по коду/названию |

**Поведение в data-search-state:**
- Избранное, Недавние и навигационные группы **скрыты** — отображаются только результаты поиска
- Результаты **группируются по типу сущности** (e.g. «Контрагенты», «Крипто-инвойсы»)
- Каждый результат показывает иконку entity, заголовок (title) и подзаголовок (subtitle)
- Клик / `Enter` по результату → навигация на карточку сущности + открытие вкладки
- `forceMount` на результатах обходит клиентскую фильтрацию cmdk

**Зачем префикс `>` вместо автоматического поиска:**

| Без префикса | С префиксом |
|-------------|-------------|
| Ввод `1000` → калькулятор, без запроса к серверу | `>1000` → поиск по данным (e.g. ИНН, код) |
| Ввод `Контрагенты` → навигация к списку | `>Иванов` → поиск конкретного контрагента |
| Нулевая нагрузка на API | Запрос только по явному намерению пользователя |

## 3. Контекстные действия

`useCommandPaletteStore` предоставляет `registerActions(sourceId, actions)`, `unregisterActions(sourceId)` и `getAllActions()`. Палитра читает объединённый список действий из store.

В текущей кодовой базе отдельного `useCommandActions` и готовых page-потребителей регистрации нет. При подключении действий компонент регистрирует стабильный source ID в effect и удаляет его в cleanup. `CommandAction` содержит обязательные `id`, `label`, `action`; optional `icon`, `shortcut`, `group` описаны в `frontend/stores/useCommandPaletteStore.ts`.

## 4. Навигация из палитры

Выбор элемента навигации выполняет два действия:
1. **Открывает вкладку** через `useTabsStore.openTab()` (или активирует существующую)
2. **Навигирует** через `router.push(url)`

Это гарантирует, что Command Palette и Tab Bar всегда синхронизированы.

## 5. Горячие клавиши

| Клавиша | Действие |
|---------|----------|
| `Ctrl+K` | Открыть / закрыть палитру |
| `Ctrl+L` | Открыть палитру с выделенным URL текущей страницы |
| `↑ / ↓` | Перемещение по элементам |
| `Enter` | Выбрать элемент (навигация / действие / копировать результат) |
| `→` | Открыть Preview Panel для выделенного элемента поиска (desktop ≥1024px) |
| `Esc` | Закрыть Preview Panel (если открыт), затем закрыть палитру |

Шорткаты `Ctrl+K` и `Ctrl+L` регистрируются внутри `CommandPalette` через стандартный механизм `useShortcut`.

### 5.1. Preview Panel (Предпросмотр)

При нажатии `→` на выделенном элементе в режиме data search (`>`) палитра расширяется до двухколоночного layout:

- **Левая колонка** (500px): CommandList с результатами поиска
- **Правая колонка** (320px): `EntityPreviewCard` с деталями сущности

**Поведение:**
- Для справочников: название, код + скалярные поля (ИНН, телефон, email) через `meta:"preview:true"`
- Для документов: номер, дата, статус (badge), сумма + FK-ссылки (мерчант, токен)
- Повторное `Esc` — скрывает preview (палитра сужается обратно)
- Desktop-only: на экранах < 1024px клавиша `→` не активирует preview
- Кнопка «Открыть» внизу карточки — переходит к сущности

## 6. Файловая карта

```
frontend/stores/useCommandPaletteStore.ts  — Zustand store: isOpen, actionsBySource, version
frontend/lib/command-intent.ts             — Intent resolver, same-origin URL validation, tab id normalization
frontend/lib/navigation.ts                 — Реестр навигации (registerNavigation)
frontend/lib/command-nav-items.ts          — Статический + динамический навигационный индекс
frontend/lib/navigation-access.ts          — Права пунктов, открывающих сущности (readPermission из metadata)
frontend/lib/calc-engine.ts               — Recursive descent parser для калькулятора
frontend/components/layout/command-palette.tsx — UI компонент (cmdk + Dialog + preview panel)
frontend/components/shared/entity-preview-card.tsx — Универсальная карточка предпросмотра сущности
frontend/components/ui/command.tsx         — shadcn/ui обёртка cmdk (dialogClassName для расширения)
frontend/types/search.ts                  — TypeScript типы: SearchResultItem, SearchResponse, PreviewResponse
frontend/lib/api.ts → api.search.query()  — Клиент глобального поиска
frontend/lib/api.ts → api.search.preview() — Клиент предпросмотра сущности
internal/infrastructure/search/service.go  — Глобальный поиск через общий authorized read service
internal/infrastructure/search/preview.go  — Preview через ReadOne, ReadMoney и AuthorizedPreviewData
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
| Discriminated union `CommandIntent` | URL, поиск по данным, калькулятор и fuzzy-поиск взаимоисключающие; ссылка не вызывает API и не попадает в cmdk-фильтрацию |
| Same-origin URL validation | Палитра открывает только страницы текущего экземпляра Metapus; внешние и небезопасные схемы отклоняются до `router.push()` |
| `pathname` как tab id | Query-параметры и hash сохраняются для перехода, но не создают дубликаты одной вкладки |
| `Map<sourceId, CommandAction[]>` | Атомарная замена действий по sourceId без гонок между несколькими компонентами |
| Recursive descent parser | Безопасность (нет eval), zero dependencies, полный контроль (поддержка `%`, `^`, пробелов в числах) |
| `scrollbar-thin` CSS утилита | Стилизованный скроллбар вместо нативного, согласованный с shadcn design tokens |
| Префикс `>` для data search | Разделение intent: навигация/калькулятор — без нагрузки на API; поиск по данным — только по явному запросу пользователя |
| Authorized reads | Поиск и preview используют общую проверку строк/полей и не формируют presentation из запрещённых данных |
| Metadata-driven search fields | Каждая фабрика декларирует свои searchable-поля через `SearchFieldsProvider`. Без реализации — дефолт (name+code / number) |
| Conditional `<CommandEmpty>` | cmdk не видит `forceMount`-группы при подсчёте результатов — «Ничего не найдено» скрывается когда есть серверные результаты или идёт загрузка |
| Desktop-only preview (≥1024px) | ERP — десктопный инструмент. Preview — бонус для больших экранов. Паттерн macOS Spotlight |
| `dialogClassName` для Command Dialog | Динамическое расширение диалога до 820px при активации preview, без CSS hacks |
| `meta:"preview:true"` для скалярных полей | Позволяет разработчику декларативно пометить поля (ИНН, телефон) для отображения в preview card |
| Standalone preview endpoint | Отдельный `GET /search/preview` вместо переиспользования `related-documents` — разные контексты, разный объём данных |

## Связанные документы

- [reference/keyboard-shortcuts.md](../reference/keyboard-shortcuts.md) — реестр всех горячих клавиш, включая `Ctrl+K` и `Ctrl+L`
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

### Доступ к данным

`search.Service` перебирает зарегистрированные searchable entities и вызывает общий `readaccess.Service`, который применяет permission, RLS, FLS и CEL **до** формирования title/subtitle/URL. Поисковые поля берутся из metadata; лимит на сущность ограничен десятью. Прежний standalone UNION ALL с товарами/складами не описывает действующий путь.

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
func (r *CryptoInvoiceRegistration) RLSDimensions() map[string]string {
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
    { "label": "Тип", "value": "Крипто-инвойс" },
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

`Preview` вызывает общий `reads.ReadOne`, затем `AuthorizedPreviewData` для разрешённых scalar/reference fields. Money читается через `ReadMoney` вместе с отдельно авторизованной валютой и scale. Прямого SQL к товарным таблицам здесь нет.

### Связь с Related Documents Preview

`EntityPreviewCard` (Command Palette) и `DocumentPreviewCard` (Related Documents) — **отдельные компоненты**:

| | EntityPreviewCard | DocumentPreviewCard |
|---|---|---|
| **Контекст** | Command Palette (Ctrl+K) | Related Documents (дерево подчинённости) |
| **Data source** | Standalone `GET /search/preview` | Inline из `RelatedDocTreeNode.previewData` |
| **Entity types** | Каталоги + документы | Только документы |
| **Backend** | `search.Service.Preview()` | `RefResolverRepo.ResolveRefs()` |
| **Общее** | `PreviewFieldDef` метаданные, `meta:"preview:true/false"` теги |
