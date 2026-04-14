"use client"

import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Input } from "@/components/ui/input"
import type { WidgetType } from "@/types/dashboard"

interface WidgetConfigPanelProps {
    widgetType: WidgetType
    config: Record<string, unknown>
    onConfigChange: (config: Record<string, unknown>) => void
}

// ── KPI Metric options (labels duplicated from kpi-renderer.tsx intentionally
//    to avoid breaking code-splitting of lazy-loaded renderer) ────────────────

const KPI_METRIC_OPTIONS: { value: string; label: string }[] = [
    { value: "cash-balance", label: "Денежные средства" },
    { value: "stock-value", label: "Остатки на складах" },
    { value: "receivables", label: "Дебиторская задолженность" },
    { value: "payables", label: "Кредиторская задолженность" },
    { value: "net-assets", label: "Чистые активы" },
    { value: "leads-count", label: "Контрагенты" },
    { value: "receipts-period", label: "Поступления за период" },
    { value: "sales-period", label: "Продажи за период" },
    { value: "payments-period", label: "Платежи за период" },
]

const CHART_TYPE_OPTIONS: { value: string; label: string }[] = [
    { value: "bar", label: "Столбчатая" },
    { value: "line", label: "Линейная" },
    { value: "pie", label: "Круговая" },
]

const CHART_PERIOD_OPTIONS: { value: string; label: string }[] = [
    { value: "week", label: "Неделя" },
    { value: "month", label: "Месяц" },
    { value: "quarter", label: "Квартал" },
    { value: "year", label: "Год" },
]

export function WidgetConfigPanel({ widgetType, config, onConfigChange }: WidgetConfigPanelProps) {
    const patch = (key: string, value: unknown) => {
        onConfigChange({ ...config, [key]: value })
    }

    switch (widgetType) {
        case "kpi":
            return (
                <div className="space-y-3">
                    <div className="space-y-1.5">
                        <Label className="text-xs text-muted-foreground">Метрика</Label>
                        <Select
                            value={(config.metric as string) ?? "cash-balance"}
                            onValueChange={(v) => patch("metric", v)}
                        >
                            <SelectTrigger className="h-8">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {KPI_METRIC_OPTIONS.map((opt) => (
                                    <SelectItem key={opt.value} value={opt.value}>
                                        {opt.label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                </div>
            )

        case "recent-documents":
            return (
                <div className="space-y-3">
                    <div className="space-y-1.5">
                        <Label className="text-xs text-muted-foreground">Количество документов</Label>
                        <Input
                            type="number"
                            min={1}
                            max={20}
                            className="h-8"
                            value={(config.limit as number) ?? 5}
                            onChange={(e) => {
                                const v = parseInt(e.target.value, 10)
                                if (!isNaN(v) && v >= 1 && v <= 20) patch("limit", v)
                            }}
                        />
                    </div>
                </div>
            )

        case "stock-chart":
            return (
                <div className="space-y-3">
                    <div className="space-y-1.5">
                        <Label className="text-xs text-muted-foreground">Тип диаграммы</Label>
                        <Select
                            value={(config.chartType as string) ?? "bar"}
                            onValueChange={(v) => patch("chartType", v)}
                        >
                            <SelectTrigger className="h-8">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {CHART_TYPE_OPTIONS.map((opt) => (
                                    <SelectItem key={opt.value} value={opt.value}>
                                        {opt.label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                    <div className="space-y-1.5">
                        <Label className="text-xs text-muted-foreground">Период</Label>
                        <Select
                            value={(config.period as string) ?? "month"}
                            onValueChange={(v) => patch("period", v)}
                        >
                            <SelectTrigger className="h-8">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {CHART_PERIOD_OPTIONS.map((opt) => (
                                    <SelectItem key={opt.value} value={opt.value}>
                                        {opt.label}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                </div>
            )

        case "event-log":
            return (
                <div className="space-y-3">
                    <div className="space-y-1.5">
                        <Label className="text-xs text-muted-foreground">Количество записей</Label>
                        <Input
                            type="number"
                            min={1}
                            max={20}
                            className="h-8"
                            value={(config.limit as number) ?? 5}
                            onChange={(e) => {
                                const v = parseInt(e.target.value, 10)
                                if (!isNaN(v) && v >= 1 && v <= 20) patch("limit", v)
                            }}
                        />
                    </div>
                </div>
            )

        // quick-actions, tasks — no configurable fields
        default:
            return null
    }
}
