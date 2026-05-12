"use client"

import { useEffect, useState, useMemo } from "react"
import { Loader2 } from "lucide-react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api } from "@/lib/api"
import type { PortalChartPoint } from "@/types/portal-api"

interface VolumeChartCardProps {
  merchantId: string | null
}

type Period = "7d" | "30d" | "90d"

export function VolumeChartCard({ merchantId }: VolumeChartCardProps) {
  const [period, setPeriod] = useState<Period>("30d")
  const [points, setPoints] = useState<PortalChartPoint[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    api.portal.chart(period, merchantId ?? undefined)
      .then((res) => setPoints(res.items))
      .catch(() => setPoints([]))
      .finally(() => setLoading(false))
  }, [period, merchantId])

  // Calculate max for scaling bars
  const maxDeposit = useMemo(() => {
    return Math.max(...points.map((p) => Number(p.deposits)), 1)
  }, [points])

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-medium text-muted-foreground">
            Объём депозитов
          </CardTitle>
          <Tabs value={period} onValueChange={(v) => setPeriod(v as Period)}>
            <TabsList className="h-7">
              <TabsTrigger value="7d" className="text-xs px-2 h-5">7д</TabsTrigger>
              <TabsTrigger value="30d" className="text-xs px-2 h-5">30д</TabsTrigger>
              <TabsTrigger value="90d" className="text-xs px-2 h-5">90д</TabsTrigger>
            </TabsList>
          </Tabs>
        </div>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className="flex items-center justify-center h-40">
            <Loader2 className="size-5 animate-spin text-muted-foreground" />
          </div>
        ) : points.length === 0 ? (
          <p className="text-sm text-muted-foreground py-8 text-center">
            Нет данных
          </p>
        ) : (
          <div className="flex items-stretch gap-[2px] h-40">
            {points.map((point) => {
              const value = Number(point.deposits)
              const height = (value / maxDeposit) * 100
              return (
                <div
                  key={point.day}
                  className="flex-1 min-w-0 group relative flex flex-col justify-end"
                >
                  <div
                    className="w-full rounded-t bg-primary/80 hover:bg-primary transition-colors"
                    style={{ height: `${Math.max(height, 2)}%` }}
                  />
                  {/* Tooltip on hover */}
                  <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 hidden group-hover:block z-10">
                    <div className="bg-popover text-popover-foreground text-[10px] px-2 py-1 rounded shadow-md whitespace-nowrap border">
                      <div>{formatDay(point.day)}</div>
                      <div className="font-medium tabular-nums">
                        {BigInt(point.deposits).toLocaleString("ru-RU")}
                      </div>
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function formatDay(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString("ru-RU", { month: "short", day: "numeric" })
}
