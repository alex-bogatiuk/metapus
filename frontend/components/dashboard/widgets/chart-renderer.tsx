"use client"

import { useMemo } from "react"
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts"
import { useWidgetData } from "@/hooks/useWidgetData"
import { api } from "@/lib/api"
import type { WidgetRenderProps } from "@/types/dashboard"

export default function ChartRenderer({ config, isEditMode }: WidgetRenderProps<"stock-chart">) {
    const warehouseIds = config.warehouseIds ?? []

    const { data, loading } = useWidgetData(
        async () => {
            const report = await api.reports.getStockBalance({
                excludeZero: true,
                warehouseId: warehouseIds.length > 0 ? warehouseIds : undefined,
            })
            return report.items
        },
        { deps: [warehouseIds.join(",")], isEditMode, pollInterval: 300_000 }
    )

    const chartData = useMemo(() => {
        if (!data) return []
        const byWarehouse = new Map<string, number>()
        for (const item of data) {
            const key = item.warehouseName || "Без склада"
            byWarehouse.set(key, (byWarehouse.get(key) ?? 0) + item.quantity)
        }
        return Array.from(byWarehouse.entries())
            .map(([name, quantity]) => ({ name, quantity }))
            .slice(0, 10)
    }, [data])

    return (
        <div className="flex h-full flex-col">
            <div className="border-b px-4 py-3">
                <h3 className="text-sm font-semibold text-foreground">Остатки по складам</h3>
            </div>
            <div className="flex-1 p-4">
                {loading && chartData.length === 0 ? (
                    <div className="flex h-full items-center justify-center">
                        <div className="h-32 w-full animate-pulse rounded bg-muted" />
                    </div>
                ) : chartData.length === 0 ? (
                    <div className="flex h-full items-center justify-center">
                        <p className="text-sm text-muted-foreground">Нет данных для отображения</p>
                    </div>
                ) : (
                    <ResponsiveContainer width="100%" height="100%">
                        <BarChart data={chartData}>
                            <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                            <XAxis dataKey="name" tick={{ fontSize: 11 }} className="fill-muted-foreground" />
                            <YAxis tick={{ fontSize: 11 }} className="fill-muted-foreground" />
                            <Tooltip
                                contentStyle={{ borderRadius: 8, fontSize: 12 }}
                                formatter={(value: number) => [value.toLocaleString("ru-RU"), "Количество"]}
                            />
                            <Bar dataKey="quantity" fill="hsl(var(--primary))" radius={[4, 4, 0, 0]} />
                        </BarChart>
                    </ResponsiveContainer>
                )}
            </div>
        </div>
    )
}
