"use client"

import { Wallet, Package, ArrowDownLeft, ArrowUpRight, Landmark, Users, ArrowDownToLine, ArrowUpFromLine, CreditCard } from "lucide-react"
import type { LucideIcon } from "lucide-react"
import { useWidgetData } from "@/hooks/useWidgetData"
import { api } from "@/lib/api"
import type { WidgetRenderProps, WidgetConfigMap } from "@/types/dashboard"
import { cn } from "@/lib/utils"

type KpiConfig = WidgetConfigMap["kpi"]

interface MetricDef {
    label: string
    icon: LucideIcon
    fetch: (signal: AbortSignal) => Promise<{ value: string; trend?: string; trendUp?: boolean }>
}

const METRIC_DEFS: Record<KpiConfig["metric"], MetricDef> = {
    "cash-balance": {
        label: "Денежные средства",
        icon: Wallet,
        fetch: async () => ({ value: "—", trend: undefined }),
    },
    "stock-value": {
        label: "Остатки на складах",
        icon: Package,
        fetch: async (_signal) => {
            const report = await api.reports.getStockBalance({ excludeZero: true })
            return {
                value: `${report.totalItems} поз., ${report.totalQuantity.toLocaleString("ru-RU")} шт.`,
            }
        },
    },
    receivables: {
        label: "Дебиторская задолженность",
        icon: ArrowDownLeft,
        fetch: async () => ({ value: "—" }),
    },
    payables: {
        label: "Кредиторская задолженность",
        icon: ArrowUpRight,
        fetch: async () => ({ value: "—" }),
    },
    "net-assets": {
        label: "Чистые активы",
        icon: Landmark,
        fetch: async () => ({ value: "—" }),
    },
    "leads-count": {
        label: "Контрагенты",
        icon: Users,
        fetch: async () => {
            const list = await api.counterparties.list()
            return { value: String(list.totalCount ?? 0) }
        },
    },
    "receipts-period": {
        label: "Поступления за период",
        icon: ArrowDownToLine,
        fetch: async () => {
            const now = new Date()
            const startOfMonth = new Date(now.getFullYear(), now.getMonth(), 1)
            const journal = await api.reports.getDocumentJournal({
                fromDate: startOfMonth.toISOString(),
                toDate: now.toISOString(),
                documentType: ["goods_receipt"],
                posted: true,
            })
            return { value: String(journal.totalCount) }
        },
    },
    "sales-period": {
        label: "Продажи за период",
        icon: ArrowUpFromLine,
        fetch: async () => {
            const now = new Date()
            const startOfMonth = new Date(now.getFullYear(), now.getMonth(), 1)
            const journal = await api.reports.getDocumentJournal({
                fromDate: startOfMonth.toISOString(),
                toDate: now.toISOString(),
                documentType: ["goods_issue"],
                posted: true,
            })
            return { value: String(journal.totalCount) }
        },
    },
    "payments-period": {
        label: "Платежи за период",
        icon: CreditCard,
        fetch: async () => ({ value: "—" }),
    },
}

export default function KpiRenderer({ config, isEditMode }: WidgetRenderProps<"kpi">) {
    const metricDef = METRIC_DEFS[config.metric] ?? METRIC_DEFS["cash-balance"]
    const Icon = metricDef.icon

    const { data, loading } = useWidgetData(
        (signal) => metricDef.fetch(signal),
        { deps: [config.metric], isEditMode, pollInterval: 120_000 }
    )

    return (
        <div className="flex h-full items-center gap-4 p-4">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-muted">
                <Icon className="h-5 w-5 text-foreground" />
            </div>
            <div className="min-w-0 flex-1">
                <p className="truncate text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    {metricDef.label}
                </p>
                {loading ? (
                    <div className="mt-1 h-5 w-20 animate-pulse rounded bg-muted" />
                ) : (
                    <>
                        <p className="mt-0.5 text-lg font-semibold text-foreground">
                            {data?.value ?? "—"}
                        </p>
                        {data?.trend && (
                            <p className={cn(
                                "mt-0.5 text-xs font-medium",
                                data.trendUp ? "text-success" : "text-destructive"
                            )}>
                                {data.trend}
                            </p>
                        )}
                    </>
                )}
            </div>
        </div>
    )
}
