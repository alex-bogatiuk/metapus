/**
 * Widget Registry — реестр всех виджетов дашборда.
 *
 * Adding a new widget:
 *  1. Define Zod config schema in types/dashboard.ts, add to WidgetConfigMap.
 *  2. Create renderer in components/dashboard/widgets/<name>-renderer.tsx
 *     — Use useWidgetData() for data fetching.
 *     — Accept WidgetRenderProps<"your-type"> (typed config).
 *     — Delegate rendering to a presenter component (dumb component).
 *  3. Call defineWidget() below and append to WIDGET_DEFINITIONS.
 */

import { lazy } from "react"
import { Wallet, FileText, Zap, ListTodo, BarChart3, Shield } from "lucide-react"
import type { WidgetDefinition, WidgetType } from "@/types/dashboard"

// ── defineWidget — type-safe factory with runtime validation ────────

function defineWidget<T extends WidgetType>(def: WidgetDefinition<T>): WidgetDefinition<T> {
    if (def.allowedSizes.length === 0) {
        throw new Error(`Widget "${def.type}": allowedSizes must not be empty`)
    }
    if (!def.allowedSizes.includes(def.defaultSize)) {
        throw new Error(`Widget "${def.type}": defaultSize must be in allowedSizes`)
    }
    return def
}

// ── Widget Definitions ──────────────────────────────────────────────

const WIDGET_DEFINITIONS: WidgetDefinition[] = [
    defineWidget({
        type: "kpi",
        label: "Показатель (KPI)",
        description: "Числовой показатель с трендом",
        icon: Wallet,
        allowedSizes: ["2x1", "3x1", "4x1"],
        defaultSize: "3x1",
        category: "kpi",
        defaultConfig: { metric: "cash-balance" },
        requiredPermission: "report:stock:read",
        component: lazy(() => import("@/components/dashboard/widgets/kpi-renderer")),
    }),
    defineWidget({
        type: "recent-documents",
        label: "Последние документы",
        description: "Таблица недавних документов с фильтром по типу",
        icon: FileText,
        allowedSizes: ["4x2", "4x3", "4x4", "3x2"],
        defaultSize: "4x3",
        category: "lists",
        defaultConfig: { limit: 5, documentTypes: [] },
        requiredPermission: "report:documents:read",
        component: lazy(() => import("@/components/dashboard/widgets/recent-docs-renderer")),
    }),
    defineWidget({
        type: "quick-actions",
        label: "Быстрые действия",
        description: "Кнопки создания документов и справочников",
        icon: Zap,
        allowedSizes: ["2x1", "2x2", "4x1", "4x2"],
        defaultSize: "4x2",
        category: "actions",
        defaultConfig: {},
        requiredPermission: null,
        component: lazy(() => import("@/components/dashboard/widgets/quick-actions-renderer")),
    }),
    defineWidget({
        type: "tasks",
        label: "Текущие дела",
        description: "Чеклист задач и напоминаний",
        icon: ListTodo,
        allowedSizes: ["2x2", "4x2", "4x3"],
        defaultSize: "4x2",
        category: "actions",
        defaultConfig: {},
        requiredPermission: null,
        component: lazy(() => import("@/components/dashboard/widgets/tasks-renderer")),
    }),
    defineWidget({
        type: "stock-chart",
        label: "График остатков",
        description: "Столбчатая диаграмма остатков по складам",
        icon: BarChart3,
        allowedSizes: ["4x2", "4x3", "4x4"],
        defaultSize: "4x3",
        category: "charts",
        defaultConfig: { chartType: "bar", warehouseIds: [], period: "month" },
        requiredPermission: "report:stock:read",
        component: lazy(() => import("@/components/dashboard/widgets/chart-renderer")),
    }),
    defineWidget({
        type: "event-log",
        label: "Журнал событий",
        description: "Последние системные события (только для администраторов)",
        icon: Shield,
        allowedSizes: ["4x2", "4x3"],
        defaultSize: "4x3",
        category: "system",
        defaultConfig: { limit: 5 },
        requiredPermission: "system:event_log:read",
        component: lazy(() => import("@/components/dashboard/widgets/event-log-renderer")),
    }),
]

// ── Registry Map ────────────────────────────────────────────────────

export const widgetRegistry = new Map<WidgetType, WidgetDefinition>(
    WIDGET_DEFINITIONS.map((def) => [def.type, def as WidgetDefinition])
)

/** All definitions as array (for Widget Gallery). */
export const allWidgetDefinitions = WIDGET_DEFINITIONS as WidgetDefinition[]

// ── Extension API ───────────────────────────────────────────────────

/**
 * Register a custom dashboard widget at runtime.
 * Client extensions call this to add new widget types:
 *
 *   import { registerWidget } from "@/lib/widget-registry"
 *   registerWidget({
 *       type: "fuel-chart",
 *       label: "Fuel consumption",
 *       ...
 *   })
 */
export function registerWidget<T extends WidgetType>(def: WidgetDefinition<T>): void {
    // Validate via defineWidget, then store as base WidgetDefinition
    defineWidget(def)
    WIDGET_DEFINITIONS.push(def as WidgetDefinition)
    widgetRegistry.set(def.type, def as WidgetDefinition)
}
