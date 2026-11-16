/**
 * Dashboard widgets of the crypto-processing configuration: work queues,
 * processing health, balances, reconciliation and payment analytics.
 */
import { lazy } from "react"
import { Wallet, ListTodo, Activity, ShieldCheck, BarChart3, Percent, CheckCircle2, AlertCircle } from "lucide-react"
import { registerWidget } from "@/lib/widget-registry"
import { registerDefaultDashboard } from "@/lib/dashboard-layout"
import type { WidgetConfigEditorProps, WidgetPlacement, WidgetSize } from "@/types/dashboard"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { counterLabels, queueKinds } from "@/configuration/crypto/lib/admin-dashboard"
import {
    analyticsConfigSchema, counterConfigSchema, financeConfigSchema, healthConfigSchema, queueConfigSchema, reconciliationConfigSchema,
    type AnalyticsConfig, type CounterConfig, type FinanceConfig, type HealthConfig, type QueueConfig, type ReconciliationConfig,
} from "@/configuration/crypto/lib/widget-configs"

function Choice<K extends string>({ label, value, options, onChange }: { label: string; value: K; options: Record<K, string>; onChange: (value: K) => void }) {
    return <Field><FieldLabel>{label}</FieldLabel><Select value={value} onValueChange={v => onChange(v as K)}><SelectTrigger aria-label={label}><SelectValue /></SelectTrigger><SelectContent><SelectGroup>{(Object.entries(options) as [K, string][]).map(([key, title]) => <SelectItem key={key} value={key}>{title}</SelectItem>)}</SelectGroup></SelectContent></Select></Field>
}

function RowLimit({ value, onChange }: { value: number; onChange: (value: number) => void }) {
    return <Field><FieldLabel htmlFor="widget-limit">Количество строк</FieldLabel><Input id="widget-limit" type="number" min={1} max={20} value={value} onChange={e => { const next = Number(e.target.value); if (Number.isInteger(next) && next >= 1 && next <= 20) onChange(next) }} /><FieldDescription>От 1 до 20. Итоги учитывают все операции.</FieldDescription></Field>
}

const CounterEditor = ({ config, onConfigChange }: WidgetConfigEditorProps<CounterConfig>) =>
    <Choice label="Показатель" value={config.metric} options={counterLabels} onChange={metric => onConfigChange({ ...config, metric })} />
const QueueEditor = ({ config, onConfigChange }: WidgetConfigEditorProps<QueueConfig>) => <>
    <Choice label="Очередь" value={config.kind} options={queueKinds} onChange={kind => onConfigChange({ ...config, kind })} />
    <RowLimit value={config.limit} onChange={limit => onConfigChange({ ...config, limit })} />
</>
const ReconciliationEditor = ({ config, onConfigChange }: WidgetConfigEditorProps<ReconciliationConfig>) =>
    <RowLimit value={config.limit} onChange={limit => onConfigChange({ ...config, limit })} />
const FinanceEditor = ({ config, onConfigChange }: WidgetConfigEditorProps<FinanceConfig>) =>
    <Choice label="Остатки" value={config.view} options={{ obligations: "Обязательства перед мерчантами", wallets: "Средства на кошельках" }} onChange={view => onConfigChange({ ...config, view })} />
const HealthEditor = ({ config, onConfigChange }: WidgetConfigEditorProps<HealthConfig>) =>
    <Field orientation="horizontal"><FieldLabel htmlFor="widget-workers">Показывать статистику задач</FieldLabel><Switch id="widget-workers" checked={config.showWorkerStats} onCheckedChange={showWorkerStats => onConfigChange({ ...config, showWorkerStats })} /></Field>
const AnalyticsEditor = ({ config, onConfigChange }: WidgetConfigEditorProps<AnalyticsConfig>) => <>
    <Choice label="Период по умолчанию" value={config.period} options={{ "7d": "7 дней", "30d": "30 дней", "90d": "90 дней" }} onChange={period => onConfigChange({ ...config, period })} />
    <FieldDescription>Токен и сеть можно выбрать в самом виджете.</FieldDescription>
</>

function registerWidgets() {
    registerWidget({
        type: "attention-counter", label: "Требует внимания", description: "Количество операций в очереди или сетей с проблемами", icon: AlertCircle, category: "kpi",
        allowedSizes: ["2x1", "3x1", "4x1"], defaultSize: "2x1", configSchema: counterConfigSchema, defaultConfig: { metric: "withdrawals" },
        requiredPermission: null, adminOnly: true, configEditor: CounterEditor,
        component: lazy(() => import("@/configuration/crypto/components/dashboard/widgets/attention-counter-renderer")),
    })
    registerWidget({
        type: "attention-queue", label: "Требуют решения", description: "Заявки на вывод, дополнительные платежи и внешние списания", icon: ListTodo, category: "lists",
        allowedSizes: ["4x3", "4x4"], defaultSize: "4x3", configSchema: queueConfigSchema, defaultConfig: { kind: "all", limit: 5 },
        requiredPermission: null, adminOnly: true, configEditor: QueueEditor,
        component: lazy(() => import("@/configuration/crypto/components/dashboard/widgets/attention-queue-renderer")),
    })
    registerWidget({
        type: "processing-health", label: "Состояние обработки", description: "Сети, RPC, подпись и ошибки задач за 24 часа", icon: Activity, category: "system",
        allowedSizes: ["2x2", "2x3", "3x2", "4x3"], defaultSize: "2x3", configSchema: healthConfigSchema, defaultConfig: { showWorkerStats: true },
        requiredPermission: null, adminOnly: true, configEditor: HealthEditor,
        component: lazy(() => import("@/configuration/crypto/components/dashboard/widgets/processing-health-renderer")),
    })
    registerWidget({
        type: "financial-summary", label: "Финансовый обзор", description: "Обязательства перед мерчантами или средства на кошельках", icon: Wallet, category: "kpi",
        allowedSizes: ["3x2", "3x3", "4x2", "4x3"], defaultSize: "3x3", configSchema: financeConfigSchema, defaultConfig: { view: "obligations" },
        requiredPermission: null, adminOnly: true, configEditor: FinanceEditor,
        component: lazy(() => import("@/configuration/crypto/components/dashboard/widgets/financial-summary-renderer")),
    })
    registerWidget({
        type: "custody-reconciliation", label: "Сверка кошельков", description: "Последние проверки учёта и блокчейна по токенам и сетям", icon: ShieldCheck, category: "lists",
        allowedSizes: ["4x3", "fullx3", "4x4"], defaultSize: "fullx3", configSchema: reconciliationConfigSchema, defaultConfig: { limit: 8 },
        requiredPermission: null, adminOnly: true, configEditor: ReconciliationEditor,
        component: lazy(() => import("@/configuration/crypto/components/dashboard/widgets/custody-reconciliation-renderer")),
    })
    registerWidget({
        type: "receipts-volume", label: "Объём поступлений", description: "Подтверждённые поступления по дням, токену и сети", icon: BarChart3, category: "charts",
        allowedSizes: ["4x3", "4x4-compact", "4x4"], defaultSize: "4x4-compact", configSchema: analyticsConfigSchema, defaultConfig: { period: "30d", tokenId: "" },
        requiredPermission: null, adminOnly: true, configEditor: AnalyticsEditor,
        component: lazy(() => import("@/configuration/crypto/components/dashboard/widgets/receipts-volume-renderer")),
    })
    registerWidget({
        type: "platform-fees", label: "Комиссии платформы", description: "Учтённые комиссии за обработку платежей, за вычетом сторно", icon: Percent, category: "kpi",
        allowedSizes: ["2x2", "2x3", "3x2", "4x3"], defaultSize: "2x3", configSchema: analyticsConfigSchema, defaultConfig: { period: "30d", tokenId: "" },
        requiredPermission: null, adminOnly: true, configEditor: AnalyticsEditor,
        component: lazy(() => import("@/configuration/crypto/components/dashboard/widgets/platform-fees-renderer")),
    })
    registerWidget({
        type: "payment-results", label: "Результаты инвойсов", description: "Текущие статусы инвойсов, созданных за выбранный период", icon: CheckCircle2, category: "charts",
        allowedSizes: ["2x2", "2x3", "2x4", "3x2", "4x3"], defaultSize: "2x2", configSchema: analyticsConfigSchema, defaultConfig: { period: "30d", tokenId: "" },
        requiredPermission: null, adminOnly: true, configEditor: AnalyticsEditor,
        component: lazy(() => import("@/configuration/crypto/components/dashboard/widgets/payment-results-renderer")),
    })
}

function defaultRows(): WidgetPlacement[][] {
    const widget = (key: string, widgetType: string, size: WidgetSize, config: WidgetPlacement["config"]): WidgetPlacement =>
        ({ instanceId: `default-${key}`, widgetType, size, config })
    return [
        ["withdrawals", "inbound", "outflows", "networks"].map(metric => widget(metric, "attention-counter", "2x1", { metric })),
        [widget("queue", "attention-queue", "4x3", { kind: "all", limit: 5 }), widget("health", "processing-health", "2x3", { showWorkerStats: true })],
        [widget("obligations", "financial-summary", "3x3", { view: "obligations" }), widget("wallets", "financial-summary", "3x3", { view: "wallets" })],
        [widget("reconciliation", "custody-reconciliation", "fullx3", { limit: 8 })],
        [widget("receipts", "receipts-volume", "4x4", { period: "30d", tokenId: "" })],
        [widget("fees", "platform-fees", "3x2", { period: "30d", tokenId: "" }), widget("results", "payment-results", "3x2", { period: "30d", tokenId: "" })],
    ]
}

let registered = false

/** Registers the configuration's widgets and default dashboard once. */
export function registerCryptoWidgets(): void {
    if (registered) return
    registered = true
    registerWidgets()
    registerDefaultDashboard(defaultRows)
}
