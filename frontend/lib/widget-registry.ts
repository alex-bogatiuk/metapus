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

import { lazy, createElement } from "react"
import { Wallet, ListTodo, Activity, ShieldCheck, BarChart3, Percent, CheckCircle2, AlertCircle } from "lucide-react"
import type { WidgetDefinition, WidgetType, RegisteredWidgetDefinition, WidgetRuntimeProps } from "@/types/dashboard"

import { widgetConfigSchemas } from "@/types/dashboard"
import { jsonObjectSnapshot } from "@/lib/json-value"

// ── defineWidget — type-safe factory with runtime validation ────────

export function defineWidget<T extends WidgetType>(def: WidgetDefinition<T>): RegisteredWidgetDefinition {
    if (def.allowedSizes.length === 0) {
        throw new Error(`Widget "${def.type}": allowedSizes must not be empty`)
    }
    if (!def.allowedSizes.includes(def.defaultSize)) {
        throw new Error(`Widget "${def.type}": defaultSize must be in allowedSizes`)
    }
    const schema = widgetConfigSchemas[def.type]
    const validatedDefault = schema.parse(def.defaultConfig)
    function RegisteredWidget(props: WidgetRuntimeProps) {
        const parsed = schema.safeParse(props.config)
        return createElement(def.component, {
            ...props,
            config: parsed.success ? parsed.data : validatedDefault,
            onConfigChange: (next) => props.onConfigChange(jsonObjectSnapshot(schema.parse(next))),
        })
    }
    return { ...def, defaultConfig: jsonObjectSnapshot(validatedDefault), component: RegisteredWidget }
}

// ── Widget Definitions ──────────────────────────────────────────────

const WIDGET_DEFINITIONS: RegisteredWidgetDefinition[] = [
    defineWidget({
        type: "attention-counter", label: "Требует внимания", description: "Количество операций в очереди или сетей с проблемами", icon: AlertCircle, category: "kpi",
        allowedSizes: ["2x1", "3x1", "4x1"], defaultSize: "2x1", defaultConfig: { metric: "withdrawals" },
        requiredPermission: null, adminOnly: true,
        component: lazy(() => import("@/components/dashboard/widgets/attention-counter-renderer")),
    }),
    defineWidget({
        type: "attention-queue", label: "Требуют решения", description: "Заявки на вывод, дополнительные платежи и внешние списания", icon: ListTodo, category: "lists",
        allowedSizes: ["4x3", "4x4"], defaultSize: "4x3", defaultConfig: { kind: "all", limit: 5 },
        requiredPermission: null, adminOnly: true,
        component: lazy(() => import("@/components/dashboard/widgets/attention-queue-renderer")),
    }),
    defineWidget({
        type: "processing-health", label: "Состояние обработки", description: "Сети, RPC, подпись и ошибки задач за 24 часа", icon: Activity, category: "system",
        allowedSizes: ["2x2", "2x3", "3x2", "4x3"], defaultSize: "2x3", defaultConfig: { showWorkerStats: true },
        requiredPermission: null, adminOnly: true,
        component: lazy(() => import("@/components/dashboard/widgets/processing-health-renderer")),
    }),
    defineWidget({
        type: "financial-summary", label: "Финансовый обзор", description: "Обязательства перед мерчантами или средства на кошельках", icon: Wallet, category: "kpi",
        allowedSizes: ["3x2", "3x3", "4x2", "4x3"], defaultSize: "3x3", defaultConfig: { view: "obligations" },
        requiredPermission: null, adminOnly: true,
        component: lazy(() => import("@/components/dashboard/widgets/financial-summary-renderer")),
    }),
    defineWidget({
        type: "custody-reconciliation", label: "Сверка кошельков", description: "Последние проверки учёта и блокчейна по токенам и сетям", icon: ShieldCheck, category: "lists",
        allowedSizes: ["4x3", "fullx3", "4x4"], defaultSize: "fullx3", defaultConfig: { limit: 8 },
        requiredPermission: null, adminOnly: true,
        component: lazy(() => import("@/components/dashboard/widgets/custody-reconciliation-renderer")),
    }),
    defineWidget({
        type: "receipts-volume", label: "Объём поступлений", description: "Подтверждённые поступления по дням, токену и сети", icon: BarChart3, category: "charts",
        allowedSizes: ["4x3", "4x4-compact", "4x4"], defaultSize: "4x4-compact", defaultConfig: { period: "30d", tokenId: "" },
        requiredPermission: null, adminOnly: true,
        component: lazy(() => import("@/components/dashboard/widgets/receipts-volume-renderer")),
    }),
    defineWidget({
        type: "platform-fees", label: "Комиссии платформы", description: "Учтённые комиссии за обработку платежей, за вычетом сторно", icon: Percent, category: "kpi",
        allowedSizes: ["2x2", "2x3", "3x2", "4x3"], defaultSize: "2x3", defaultConfig: { period: "30d", tokenId: "" },
        requiredPermission: null, adminOnly: true,
        component: lazy(() => import("@/components/dashboard/widgets/platform-fees-renderer")),
    }),
    defineWidget({
        type: "payment-results", label: "Результаты инвойсов", description: "Текущие статусы инвойсов, созданных за выбранный период", icon: CheckCircle2, category: "charts",
        allowedSizes: ["2x2", "2x3", "2x4", "3x2", "4x3"], defaultSize: "2x2", defaultConfig: { period: "30d", tokenId: "" },
        requiredPermission: null, adminOnly: true,
        component: lazy(() => import("@/components/dashboard/widgets/payment-results-renderer")),
    }),
]

// ── Registry Map ────────────────────────────────────────────────────

export const widgetRegistry = new Map<WidgetType, RegisteredWidgetDefinition>(
    WIDGET_DEFINITIONS.map((def) => [def.type, def])
)

/** All definitions as array (for Widget Gallery). */
export const allWidgetDefinitions = WIDGET_DEFINITIONS

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
    const registered = defineWidget(def)
    WIDGET_DEFINITIONS.push(registered)
    widgetRegistry.set(def.type, registered)
}
