# 20 — Dashboard Widget System

> Как создать новый виджет и сделать его доступным для выбора пользователями.

---

## Архитектура

Система виджетов полностью находится на **фронтенде** — бэкенд ничего не знает о самих виджетах. Состояние раскладки (layout) хранится в `user_preferences.dashboard_layout` (JSONB), типы и конфигурации — в коде.

```
frontend/
├── types/dashboard.ts                        # 1. Zod-схемы конфигов + TypeScript типы
├── lib/widget-registry.ts                    # 2. Реестр виджетов (единственный список)
├── components/dashboard/widgets/
│   ├── <name>-renderer.tsx                   # 3. Компонент рендеринга
│   └── widget-gallery-sheet.tsx              # Галерея (читает реестр — менять не надо)
├── hooks/useWidgetData.ts                    # Хук загрузки данных
└── stores/useDashboardStore.ts               # Zustand-стор: layout, edit mode
```

Пользователь добавляет виджет через **"Настроить" → галерею**. Галерея автоматически показывает все виджеты из реестра.

---

## Шаги добавления нового виджета

> Минимальный набор изменений: **3 файла** (типы → реестр → рендерер).

---

### Шаг 1 — Определи тип и Zod-схему конфига

Файл: `frontend/types/dashboard.ts`

```typescript
// 1a. Добавь Zod-схему конфига (все поля, которые виджет принимает)
export const myWidgetConfigSchema = z.object({
    limit: z.number().int().min(1).max(50).default(10),
    warehouseId: z.string().uuid().optional(),
    showArchived: z.boolean().default(false),
})

// 1b. Добавь запись в WidgetConfigMap
export type WidgetConfigMap = {
    // ...существующие...
    "my-widget": z.infer<typeof myWidgetConfigSchema>  // <-- добавь
}

// 1c. Добавь схему в widgetConfigSchemas (runtime-валидация в WidgetWrapper)
export const widgetConfigSchemas: Record<WidgetType, z.ZodType> = {
    // ...существующие...
    "my-widget": myWidgetConfigSchema,                  // <-- добавь
}
```

**Правила схемы:**
- Все поля с `.default(...)` — рендерер получит гарантированное значение.
- Используй `.optional()` только для полей, которые рендерер умеет обработать в `undefined`.
- Схема валидируется в `WidgetWrapper` при каждом рендере — невалидный конфиг → fallback-заглушка.

---

### Шаг 2 — Зарегистрируй виджет в реестре

Файл: `frontend/lib/widget-registry.ts`

```typescript
import { BarChart3 } from "lucide-react"  // выбери подходящую иконку

const WIDGET_DEFINITIONS = [
    // ...существующие...
    defineWidget({
        type: "my-widget",                             // должен совпадать с WidgetConfigMap
        label: "Мой виджет",                          // показывается в галерее
        description: "Краткое описание для галереи",  // subtitle в галерее
        icon: BarChart3,                              // LucideIcon
        allowedSizes: ["4x2", "4x3"],                 // допустимые размеры (см. ниже)
        defaultSize: "4x2",                           // размер при добавлении из галереи
        category: "lists",                            // kpi | lists | charts | actions | system
        defaultConfig: { limit: 10, showArchived: false },  // config для нового инстанса
        requiredPermission: "report:stock:read",      // null = без ограничений
        component: lazy(
            () => import("@/components/dashboard/widgets/my-widget-renderer")
        ),
    }),
]
```

После этого шага виджет уже появится в галерее — можно проверить, не создавая рендерер.

---

### Шаг 3 — Создай компонент-рендерер

Файл: `frontend/components/dashboard/widgets/my-widget-renderer.tsx`

```tsx
"use client"

import { useWidgetData } from "@/hooks/useWidgetData"
import { api } from "@/lib/api"
import type { WidgetRenderProps } from "@/types/dashboard"

// Типизированные пропсы — config автоматически получит тип из myWidgetConfigSchema
export default function MyWidgetRenderer({
    config,
    isEditMode,
}: WidgetRenderProps<"my-widget">) {
    const { limit, warehouseId, showArchived } = config

    const { data, loading, error } = useWidgetData(
        async () => {
            // Вызов API — используй существующие методы из api.ts
            const result = await api.reports.getStockBalance({
                excludeZero: !showArchived,
            })
            return result.items.slice(0, limit)
        },
        {
            deps: [limit, warehouseId, showArchived], // refetch при смене config
            isEditMode,                               // пауза в режиме редактирования
            pollInterval: 60_000,                     // авто-обновление раз в минуту (0 = выкл)
        }
    )

    // ── Состояние ошибки ──────────────────────────────────────────
    if (error) {
        return (
            <div className="flex h-full items-center justify-center p-4 text-center">
                <p className="text-sm text-destructive">Ошибка: {error.message}</p>
            </div>
        )
    }

    // ── Скелетон загрузки ─────────────────────────────────────────
    if (loading && !data) {
        return (
            <div className="space-y-2 p-4">
                {Array.from({ length: limit }).map((_, i) => (
                    <div key={i} className="h-8 animate-pulse rounded bg-muted" />
                ))}
            </div>
        )
    }

    // ── Пустое состояние ──────────────────────────────────────────
    if (!data || data.length === 0) {
        return (
            <div className="flex h-full items-center justify-center p-4">
                <p className="text-sm text-muted-foreground">Нет данных</p>
            </div>
        )
    }

    // ── Основной контент ──────────────────────────────────────────
    return (
        <div className="flex h-full flex-col">
            <div className="flex items-center justify-between border-b px-4 py-3">
                <h3 className="text-sm font-semibold">Мой виджет</h3>
            </div>
            <div className="flex-1 overflow-auto">
                {data.map((item) => (
                    <div key={item.productId} className="border-b px-4 py-2 text-sm">
                        {item.productName}
                    </div>
                ))}
            </div>
        </div>
    )
}
```

---

## Контракт рендерера

### `WidgetRenderProps<T>`

```typescript
interface WidgetRenderProps<T extends WidgetType> {
    placement: WidgetPlacement   // позиция на сетке (x, y, w, h) + instanceId
    config: WidgetConfigMap[T]   // типизированный конфиг, прошедший Zod-валидацию
    isEditMode: boolean          // true = дашборд в режиме редактирования
    onConfigChange: (config: WidgetConfigMap[T]) => void  // сохранить новый конфиг
}
```

- `config` всегда валиден — `WidgetWrapper` применяет Zod перед передачей.
- При `isEditMode = true` — рендерер должен приостановить fetching (передай `isEditMode` в `useWidgetData`).
- `onConfigChange` — для виджетов с inline-настройками (например, выбор склада прямо в заголовке).

### `useWidgetData<T>`

```typescript
const { data, loading, error, refetch } = useWidgetData(
    fetcher: (signal: AbortSignal) => Promise<T>,
    options: {
        deps?: unknown[]       // пересоздаёт fetcher при изменении
        isEditMode?: boolean   // true = не загружать
        pollInterval?: number  // мс (0 = без polling)
    }
)
```

| Поле | Описание |
|------|----------|
| `data` | Результат последнего успешного fetch (`null` до первого) |
| `loading` | `true` во время запроса |
| `error` | `Error` после исчерпания retry (1 попытка, задержка 3 с) |
| `refetch` | Принудительный перезапрос |

Хук автоматически:
- Отменяет in-flight запрос при размонтировании или смене `deps`.
- Делает 1 повторную попытку через 3 с при ошибке сети.
- Паузирует polling при `isEditMode = true`.

---

## Размеры виджетов

Формат: **`{ширина}x{высота}`** в единицах сетки (сетка 4 колонки).

| Размер | Ширина | Высота | Применение |
|--------|--------|--------|------------|
| `2x1` | 2 кол. | 1 ряд | Мини KPI |
| `3x1` | 3 кол. | 1 ряд | KPI с трендом |
| `4x1` | 4 кол. | 1 ряд | Широкий баннер |
| `2x2` | 2 кол. | 2 ряда | Компактный список |
| `3x2` | 3 кол. | 2 ряда | Средний список |
| `4x2` | 4 кол. | 2 ряда | Широкий список / действия |
| `4x3` | 4 кол. | 3 ряда | Таблица данных |
| `4x4` | 4 кол. | 4 ряда | Большой виджет / график |

Правило: `defaultSize` обязан входить в `allowedSizes` (проверяется в `defineWidget`).

---

## Категории

| Ключ | Назначение |
|------|-----------|
| `kpi` | Числовые показатели (денежные средства, остатки) |
| `lists` | Табличные данные (документы, номенклатура) |
| `charts` | Графики и диаграммы |
| `actions` | Кнопки быстрых действий, чеклисты |
| `system` | Системная информация (только для администраторов) |

Категории используются в галерее для группировки виджетов по вкладкам.

---

## Права доступа (`requiredPermission`)

Значение `requiredPermission` в реестре используется для **фильтрации галереи** — если у пользователя нет разрешения, виджет не показывается в списке для добавления.

```typescript
// Без ограничений — виден всем
requiredPermission: null

// Только пользователи с этим разрешением видят виджет в галерее
requiredPermission: "report:stock:read"
requiredPermission: "system:event_log:read"
```

> **Важно:** `requiredPermission` — это UX-ограничение (галерея), а не security-гарантия.
> Данные, которые виджет fetches, **всегда** должны быть защищены на уровне бэкенд-эндпоинта через middleware авторизации.

---

## Полный пример: виджет «Топ контрагентов»

### `types/dashboard.ts` — добавить в конец раздела схем:

```typescript
export const topCounterpartiesConfigSchema = z.object({
    limit: z.number().int().min(3).max(20).default(5),
    sortBy: z.enum(["receipts", "issues", "total"]).default("total"),
})
```

Добавить в `WidgetConfigMap`:
```typescript
"top-counterparties": z.infer<typeof topCounterpartiesConfigSchema>
```

Добавить в `widgetConfigSchemas`:
```typescript
"top-counterparties": topCounterpartiesConfigSchema,
```

### `lib/widget-registry.ts` — добавить виджет:

```typescript
defineWidget({
    type: "top-counterparties",
    label: "Топ контрагентов",
    description: "Рейтинг контрагентов по объёму документов",
    icon: Users,
    allowedSizes: ["4x2", "4x3"],
    defaultSize: "4x2",
    category: "lists",
    defaultConfig: { limit: 5, sortBy: "total" },
    requiredPermission: "report:documents:read",
    component: lazy(
        () => import("@/components/dashboard/widgets/top-counterparties-renderer")
    ),
}),
```

### `components/dashboard/widgets/top-counterparties-renderer.tsx`:

```tsx
"use client"

import Link from "next/link"
import { useWidgetData } from "@/hooks/useWidgetData"
import { api } from "@/lib/api"
import type { WidgetRenderProps } from "@/types/dashboard"

export default function TopCounterpartiesRenderer({
    config,
    isEditMode,
}: WidgetRenderProps<"top-counterparties">) {
    const { limit } = config

    const { data, loading, error } = useWidgetData(
        async () => {
            const result = await api.counterparties.list({ limit })
            return result.items
        },
        { deps: [limit], isEditMode, pollInterval: 120_000 }
    )

    if (error) return (
        <div className="flex h-full items-center justify-center p-4">
            <p className="text-sm text-destructive">{error.message}</p>
        </div>
    )

    if (loading && !data) return (
        <div className="space-y-2 p-4">
            {Array.from({ length: limit }).map((_, i) => (
                <div key={i} className="h-7 animate-pulse rounded bg-muted" />
            ))}
        </div>
    )

    return (
        <div className="flex h-full flex-col">
            <div className="flex items-center justify-between border-b px-4 py-3">
                <h3 className="text-sm font-semibold">Топ контрагентов</h3>
                <Link
                    href="/catalogs/counterparties"
                    className="text-xs font-medium text-foreground hover:underline"
                >
                    Все
                </Link>
            </div>
            <div className="flex-1 overflow-auto">
                {(data ?? []).map((cp, idx) => (
                    <div
                        key={cp.id}
                        className="flex items-center gap-3 border-b px-4 py-2 text-sm last:border-0"
                    >
                        <span className="w-5 text-right text-xs text-muted-foreground">
                            {idx + 1}
                        </span>
                        <span className="flex-1 truncate">{cp.name}</span>
                    </div>
                ))}
            </div>
        </div>
    )
}
```

---

## Checklist

```
[ ] 1. types/dashboard.ts
        — Добавлена Zod-схема: export const myWidgetConfigSchema = z.object({...})
        — Добавлена запись в WidgetConfigMap
        — Добавлена запись в widgetConfigSchemas

[ ] 2. lib/widget-registry.ts
        — Добавлен import иконки из lucide-react
        — Добавлен вызов defineWidget({...}) в WIDGET_DEFINITIONS
        — component: lazy(() => import("@/components/dashboard/widgets/..."))

[ ] 3. components/dashboard/widgets/<name>-renderer.tsx
        — "use client" в первой строке
        — export default function — именованный PascalCase
        — Типизация: WidgetRenderProps<"my-widget">
        — Обработаны 3 состояния: error / loading / empty
        — isEditMode передан в useWidgetData

[ ] 4. Проверка
        — npx tsc --noEmit (нет ошибок)
        — Виджет появился в галерее ("Настроить" на дашборде)
        — Виджет добавляется и отображает данные
        — Виджет корректно ведёт себя в режиме редактирования (нет fetch)
```

---

## FAQ

**Q: Как добавить настройки виджета, которые пользователь меняет прямо в интерфейсе?**

Используй `onConfigChange` из `WidgetRenderProps`. Например, выпадающий список склада в заголовке виджета:

```tsx
export default function MyRenderer({ config, onConfigChange }: WidgetRenderProps<"my-widget">) {
    return (
        <select
            value={config.warehouseId ?? ""}
            onChange={(e) => onConfigChange({ ...config, warehouseId: e.target.value })}
        >
            ...
        </select>
    )
}
```

Изменения автоматически сохраняются в `user_preferences.dashboard_layout` (debounced).

---

**Q: Где хранится layout пользователя?**

В таблице `user_preferences`, колонка `dashboard_layout` (JSONB). Схема:

```json
{
  "version": 1,
  "widgets": [
    {
      "instanceId": "uuid-v4",
      "widgetType": "my-widget",
      "x": 0, "y": 0, "w": 4, "h": 2,
      "config": { "limit": 10 }
    }
  ]
}
```

---

**Q: Как один и тот же тип виджета добавить дважды с разными настройками?**

Каждый инстанс имеет уникальный `instanceId`. Пользователь может добавить `kpi` пять раз — каждый с отдельным `config.metric`. Это работает автоматически.

---

**Q: Нужно ли менять бэкенд?**

Только если виджет требует **нового API-эндпоинта**. Если виджет использует существующие методы `api.*` — бэкенд не трогается.
