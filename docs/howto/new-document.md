# Как добавить новый документ

> **TL;DR:** Пошаговая инструкция по добавлению нового учётного документа. Включает миграцию, доменную модель, движения регистров, формы ввода и валидацию. Справочники описаны в [new-entity.md](new-entity.md).

> **Тип:** How-To
> **Аудитория:** Developer
> **Связанные:** [new-entity.md](new-entity.md), [posting-engine.md](../systems/posting-engine.md), [smart-data-entry.md](../systems/smart-data-entry.md)

---

## Предварительные требования

Перед началом убедитесь, что все связанные справочники (контрагенты, склады, номенклатура) уже зарегистрированы. Документ ссылается на справочники через FK.

---

## Шаг 1. Миграция (таблицы шапки и табличной части)

Документ состоит из двух таблиц: шапка (`doc_*`) и строки (`doc_*_lines`).

```sql
-- db/migrations/NNNNN_doc_purchase_return.sql

-- ── Шапка ────────────────────────────────────────────────────────────────
CREATE TABLE doc_purchase_returns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version INT NOT NULL DEFAULT 1,
    posted BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version INT NOT NULL DEFAULT 0,
    attributes JSONB DEFAULT '{}',

    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(),

    organization_id UUID NOT NULL REFERENCES cat_organizations(id),
    number VARCHAR(50) NOT NULL,
    date TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    supplier_id UUID NOT NULL REFERENCES cat_counterparties(id),
    warehouse_id UUID NOT NULL REFERENCES cat_warehouses(id),
    currency_id UUID REFERENCES cat_currencies(id),
    contract_id UUID REFERENCES cat_contracts(id),

    reason TEXT,
    description TEXT,

    total_quantity BIGINT NOT NULL DEFAULT 0,
    total_amount BIGINT NOT NULL DEFAULT 0,
    total_vat BIGINT NOT NULL DEFAULT 0,

    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Строки (табличная часть) ─────────────────────────────────────────────
CREATE TABLE doc_purchase_return_lines (
    line_id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    document_id UUID NOT NULL REFERENCES doc_purchase_returns(id) ON DELETE CASCADE,
    line_no INT NOT NULL,
    product_id UUID NOT NULL REFERENCES cat_nomenclatures(id),
    unit_id UUID NOT NULL REFERENCES cat_units(id),
    coefficient NUMERIC(15,4) NOT NULL DEFAULT 1,
    quantity BIGINT NOT NULL DEFAULT 0,
    unit_price BIGINT NOT NULL DEFAULT 0,
    vat_rate_id UUID NOT NULL REFERENCES cat_vat_rates(id),
    vat_percent INT NOT NULL DEFAULT 0,
    vat_amount BIGINT NOT NULL DEFAULT 0,
    discount_percent NUMERIC(5,2) NOT NULL DEFAULT 0,
    discount_amount BIGINT NOT NULL DEFAULT 0,
    amount BIGINT NOT NULL DEFAULT 0,
    UNIQUE (document_id, line_no)
);

-- ── Индексы ──────────────────────────────────────────────────────────────
CREATE UNIQUE INDEX idx_doc_purchase_returns_number
    ON doc_purchase_returns (number) WHERE deletion_mark = FALSE;

CREATE INDEX idx_doc_purchase_returns_search
    ON doc_purchase_returns USING GIN (
        (number || ' ' || COALESCE(description, '')) gin_trgm_ops
    );

-- ── CDC-триггеры ─────────────────────────────────────────────────────────
CREATE TRIGGER trg_doc_purchase_returns_txid
    BEFORE UPDATE ON doc_purchase_returns FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_doc_purchase_returns_soft_delete
    BEFORE UPDATE OF deletion_mark ON doc_purchase_returns FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
```

> [!IMPORTANT]
> **Поисковый индекс.** GIN-индекс с `gin_trgm_ops` — обязателен. Без него fuzzy-поиск в списках (M5) не работает. Расширение `pg_trgm` уже подключено в миграции `00001`.

---

## Шаг 2. Доменная модель

Создайте пакет `internal/domain/documents/purchase_return/`.

### model.go

```go
// internal/domain/documents/purchase_return/model.go
package purchase_return

import (
    "context"

    "metapus/internal/core/apperror"
    "metapus/internal/core/entity"
    "metapus/internal/core/id"
    "metapus/internal/core/types"
    "metapus/internal/domain"
    "metapus/internal/domain/posting"
)

type PurchaseReturn struct {
    entity.Document

    SupplierID  id.ID  `db:"supplier_id" json:"supplierId" meta:"label:Поставщик,ref:supplier"`
    WarehouseID id.ID  `db:"warehouse_id" json:"warehouseId" meta:"label:Склад,ref:warehouse"`
    ContractID  *id.ID `db:"contract_id" json:"contractId,omitempty" meta:"label:Договор,ref:contract"`
    Reason      string `db:"reason" json:"reason,omitempty" meta:"label:Причина возврата"`

    entity.CurrencyAware
    AmountIncludesVAT bool `db:"amount_includes_vat" json:"amountIncludesVat" meta:"label:Сумма включает НДС"`

    TotalQuantity types.Quantity   `db:"total_quantity" json:"totalQuantity"`
    TotalAmount   types.MinorUnits `db:"total_amount" json:"totalAmount"`
    TotalVAT      types.MinorUnits `db:"total_vat" json:"totalVat"`

    Lines []PurchaseReturnLine `db:"-" json:"lines" meta:"label:Товары"`
}

// Validate — чистая функция, без обращений к БД.
func (d *PurchaseReturn) Validate(ctx context.Context) error {
    if err := d.Document.Validate(ctx); err != nil {
        return err
    }
    if err := d.ValidateCurrency(ctx); err != nil {
        return err
    }
    if id.IsNil(d.SupplierID) {
        return apperror.NewValidation("supplier is required").
            WithDetail("field", "supplierId")
    }
    if id.IsNil(d.WarehouseID) {
        return apperror.NewValidation("warehouse is required").
            WithDetail("field", "warehouseId")
    }
    return domain.ValidateDocumentLines(d.Lines)
}

// GetDocumentType — уникальный идентификатор типа документа.
func (d *PurchaseReturn) GetDocumentType() string { return "PurchaseReturn" }

// Compile-time interface checks.
var _ posting.Postable = (*PurchaseReturn)(nil)
```

Ключевые правила:

| Правило | Обоснование |
|---------|-------------|
| `entity.Document` embed | Наследует `id`, `number`, `date`, `organizationId`, `posted`, `version`, `deletionMark` |
| `entity.CurrencyAware` embed | Наследует `currencyId` + `ValidateCurrency()` |
| `Validate()` — без БД | Тестируемость. FK-валидация — в infrastructure слое |
| `WithDetail("field", "...")` | Фронтенд отображает ошибку под конкретным полем |
| `domain.ValidateDocumentLines()` | Единая стратегия проверки строк для всех документов |
| **`meta:"ref:..."` на FK-полях** | Без `ref:` AutoForm отрисует UUID как текст вместо combobox. См. [ссылочные поля](new-entity.md#ссылочные-поля-meta-ref-теги) |

---

## Шаг 3. Движения регистров

Метод `GenerateMovements()` — детерминированная функция без side effects.

```go
// internal/domain/documents/purchase_return/model.go (продолжение)

func (d *PurchaseReturn) GenerateStockMovements(ctx context.Context) ([]entity.StockMovement, error) {
    newVersion := d.PostedVersion + 1
    movements := make([]entity.StockMovement, 0, len(d.Lines))

    for _, line := range d.Lines {
        baseQty := line.Quantity.MulCoefficient(line.Coefficient)

        movements = append(movements, entity.NewStockMovement(
            d.ID, d.GetDocumentType(), newVersion, d.Date,
            entity.RecordTypeExpense,   // Возврат = расход со склада
            d.WarehouseID, line.ProductID, baseQty,
        ))
    }
    return movements, nil
}
```

> [!IMPORTANT]
> Тип записи определяется экономическим смыслом: **Возврат поставщику = расход** (`RecordTypeExpense`), а не приход.

---

## Шаг 4. Регистрация (Composition Root)

```go
// internal/content/document_registrations.go (добавить)

type PurchaseReturnRegistration struct{}

func (r *PurchaseReturnRegistration) RoutePrefix() string { return "purchase-return" }
func (r *PurchaseReturnRegistration) Permission() string  { return "document:purchase_return" }
func (r *PurchaseReturnRegistration) EntityName() string  { return "PurchaseReturn" }
func (r *PurchaseReturnRegistration) EntityLabel() string { return "Возврат поставщику" }
func (r *PurchaseReturnRegistration) EntityPresentation() metadata.Presentation {
    return metadata.Presentation{
        Singular: "Возврат поставщику",
        Plural:   "Возвраты поставщикам",
        NewLabel: "Новый возврат",
        Genitive: "возврата поставщику",
    }
}
```

В `register.go`:
```go
factoryReg.RegisterDocument(&PurchaseReturnRegistration{})
```

---

## Шаг 5. Frontend — список документов

Список создаётся по паттерну `goods-receipts/page.tsx`. Ключевые механизмы подключаются автоматически через хуки:

| Механизм | Хук / Компонент | Что обеспечивает |
|----------|-----------------|------------------|
| Курсорная пагинация | `useEntityListPage` | Бесконечная прокрутка, фильтрация, сортировка |
| Fuzzy-поиск (M5) | `searchQuery` из `useEntityListPage` + `DataToolbar` | Мульти-токенный поиск по `pg_trgm` |
| Scroll Restore (M2) | `useScrollRestore(scrollContainerRef)` | Восстановление позиции при переключении вкладок |
| Ctrl+F фокус | `useShortcut("list.search", "ctrl+f", ...)` | Фокус на поиск |

```tsx
// app/(main)/documents/purchase-returns/page.tsx

const {
  items, loading, error, refresh,
  searchQuery, setSearchQuery,   // ← M5: поиск
  // ... остальные поля
} = useEntityListPage<PurchaseReturnResponse>({
  entityKey: "PurchaseReturn",
  api: api.purchaseReturns,
  periodField: "date",
  limit: 100,
})

// M5: Ctrl+F → фокус на поиск
const searchInputRef = useRef<HTMLInputElement | null>(null)
useShortcut("list.search", "ctrl+f", "Поиск", "list", () => {
  searchInputRef.current?.focus()
  searchInputRef.current?.select()
})

// M2: восстановление скролла
useScrollRestore(scrollContainerRef)

// В JSX:
<DataToolbar
  searchValue={searchQuery}
  onSearchChange={setSearchQuery}
  searchInputRef={(el) => { searchInputRef.current = el }}
  ...
/>
```

---

## Шаг 6. Frontend — форма документа

Форма документа — кастомная страница (не `useCatalogForm`). Состоит из шапки (reference-поля), табличной части (строки товаров) и подвала (итоги).

### 6.1. Валидация полей формы

Документы используют `useFormValidation` напрямую. Хук обеспечивает:

- **On-blur**: при уходе из обязательного поля без значения — shake-анимация + красная рамка
- **On-submit**: при нажатии «Провести и закрыть» — проверка всех полей, auto-scroll к первой ошибке + фокус

```tsx
// app/(main)/documents/purchase-returns/new/page.tsx

import { useFormValidation } from "@/hooks/useFormValidation"

// Объявление правил: field должен совпадать с data-field атрибутом в DOM
const validation = useFormValidation<PurchaseReturnFormState>({
  rules: [
    { field: "organizationId", validate: (s) => s.organizationId ? null : "Укажите организацию" },
    { field: "supplierId",     validate: (s) => s.supplierId ? null : "Укажите поставщика" },
    { field: "warehouseId",    validate: (s) => s.warehouseId ? null : "Укажите склад" },
    { field: "lines", trigger: "submit",
      validate: (s) => s.lines.length > 0 ? null : "Добавьте хотя бы одну строку товаров" },
  ],
})
```

### 6.2. Привязка к DOM

Каждое валидируемое поле оборачивается в контейнер с `data-field`:

```tsx
<div data-field="supplierId">
  <Label className="text-xs text-muted-foreground">Поставщик *</Label>
  <ReferenceField
    value={f.supplierId}
    displayName={f.supplierName}
    apiEndpoint="/catalog/counterparties"
    placeholder="Выберите поставщика"
    error={fieldErrors.supplierId || validation.fieldErrors.supplierId}
    onChange={(id, name) => {
      update({ supplierId: id, supplierName: name })
      markDirty()
      validation.clearFieldError("supplierId")
    }}
    onBlur={() => validation.handleBlur("supplierId", f)}
  />
</div>
```

> [!IMPORTANT]
> **`data-field` — обязателен.** Хук `useFormValidation` ищет элемент по `[data-field="supplierId"]` для анимации и скролла. Без атрибута shake и scroll-to-error не сработают.

> [!TIP]
> `ReferenceField` поддерживает `onBlur` — вызывается при закрытии popover (пользователь ушёл без выбора). Именно здесь срабатывает on-blur валидация.

### 6.3. Валидация при сохранении

Ручная проверка `if (!f.supplierId) ...` **заменяется** одной строкой:

```tsx
const handleSave = async (postImmediately: boolean, andClose: boolean) => {
  // M15: валидация всех полей — shake первого невалидного + scroll
  if (!validation.validateAll(f)) return

  setSaving(true)
  validation.clearAllErrors()
  // ... API-вызов
}
```

### 6.4. Последовательность UX при ошибке

1. `validation.validateAll(f)` проверяет все правила (blur + submit)
2. Первое невалидное поле получает CSS-класс `animate-shake` (0.5s)
3. `scrollIntoView({ behavior: "smooth", block: "center" })` прокручивает к полю
4. `requestAnimationFrame` → `focus()` на input внутри контейнера
5. Под полем отображается текст ошибки через `validation.fieldErrors.supplierId`

---

## Шаг 7. Sticky Defaults (M1)

При первом сохранении документа запоминаются поля шапки (организация, склад, валюта). При создании следующего документа они подставляются автоматически:

```tsx
import { useLastUsedDefaults, saveLastUsed } from "@/hooks/useLastUsedValues"

// При инициализации формы — восстановить
const stickyDefaults = useLastUsedDefaults<FormState>("purchase_return")

// При сохранении — запомнить
saveLastUsed("purchase_return", {
  organizationId: f.organizationId, organizationName: f.organizationName,
  warehouseId: f.warehouseId, warehouseName: f.warehouseName,
  currencyId: f.currencyId, currencyName: f.currencyName,
})
```

---

## Файловая карта

```
Backend:
  db/migrations/NNNNN_doc_purchase_return.sql          — миграция
  internal/domain/documents/purchase_return/model.go   — модель + Validate + Movements
  internal/domain/documents/purchase_return/service.go — BaseDocumentService embed
  internal/domain/documents/purchase_return/builder.go — DTO → Model mapping
  internal/domain/documents/purchase_return/repository.go — интерфейс репозитория
  internal/infrastructure/storage/postgres/document_repo/purchase_return.go — SQL
  internal/infrastructure/http/v1/dto/purchase_return.go — DTO (Create/Update/Response)
  internal/infrastructure/http/v1/handlers/purchase_return.go — Handler (тонкий адаптер)
  internal/content/document_registrations.go           — регистрация

Frontend:
  frontend/types/document.ts                           — TypeScript-типы (=== Go DTO)
  frontend/lib/api.ts                                  — createDocumentApi<>() вызов
  frontend/app/(main)/documents/purchase-returns/page.tsx  — список
  frontend/app/(main)/documents/purchase-returns/new/page.tsx — создание
  frontend/app/(main)/documents/purchase-returns/[id]/page.tsx — редактирование
```

---

## Чеклист

- [ ] Миграция: таблицы `doc_*` и `doc_*_lines` с GIN-индексом
- [ ] Модель: `entity.Document` embed, `Validate()` без БД
- [ ] Модель: **все FK-поля (`id.ID`/`*id.ID`) имеют `ref:<refType>` в `meta`-теге** (см. [new-entity.md#ссылочные-поля](new-entity.md#ссылочные-поля-meta-ref-теги))
- [ ] Движения: `GenerateStockMovements()` детерминированна
- [ ] Регистрация: `RegisterDocument()` в `register.go`
- [ ] DTO: `Create`, `Update`, `Response` — поля совпадают с TypeScript
- [ ] TypeScript: типы в `types/document.ts`
- [ ] API: `createDocumentApi<>()` в `lib/api.ts`
- [ ] Список: `useEntityListPage` + `DataToolbar` (search) + `useScrollRestore` + `Ctrl+F`
- [ ] Форма: `useFormValidation` с `data-field` на обязательных полях
- [ ] Форма: `onBlur` на `ReferenceField` для on-blur валидации
- [ ] Sticky defaults: `useLastUsedDefaults` + `saveLastUsed`
- [ ] Проверка: `go build ./... && golangci-lint run ./...`
- [ ] Проверка: `cd frontend && npx tsc --noEmit && npm run lint`

---

## Связанные документы

- [new-entity.md](new-entity.md) — добавление справочника (более простой случай)
- [posting-engine.md](../systems/posting-engine.md) — движок проведения, Visitor pattern
- [smart-data-entry.md](../systems/smart-data-entry.md) — каскадное автозаполнение строк
- [filtering.md](../systems/filtering.md) — система фильтрации списков
- [frontend-guidelines.md](../reference/frontend-guidelines.md) — правила фронтенда
