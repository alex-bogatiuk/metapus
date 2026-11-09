"use client"
import Link from "next/link"
import { api } from "@/lib/api"
import { useAdminNetworks } from "@/hooks/useAdminNetworks"
import { useWidgetData } from "@/hooks/useWidgetData"
import { counterLabels, networkProblem, queueHref } from "@/lib/admin-dashboard"
import type { WidgetRenderProps } from "@/types/dashboard"
import { CardHeader, CardTitle, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"

export default function Counter({ config, isEditMode }: WidgetRenderProps<"attention-counter">) {
  const { metric } = config
  const queues = useWidgetData(() => api.dashboard.queues(), { key: "widget:admin-queue:all:5", isEditMode, enabled: metric !== "networks", pollInterval: 60_000 })
  const networks = useAdminNetworks({ isEditMode, enabled: metric === "networks" })
  const data = metric === "networks" ? networks : queues
  const count = metric === "networks" ? networks.data?.items.filter(n => networkProblem(n, networks.data?.evaluatedAt)).length : queues.data?.[metric]
  return <><CardHeader className="p-4 pb-1"><CardTitle className="text-muted-foreground">{counterLabels[metric]}</CardTitle></CardHeader><CardContent className="flex flex-1 items-center justify-between gap-2 p-4 pt-0">
    {data.loading ? <Skeleton className="h-8 w-16" /> : data.error ? <Button variant="outline" size="sm" onClick={data.refetch}>Повторить</Button> : <><span className="text-3xl font-semibold tabular-nums">{count ?? "—"}</span><Link aria-label={`Открыть: ${counterLabels[metric]}`} className="text-xs text-muted-foreground underline underline-offset-4" href={metric === "networks" ? "/admin/crypto-networks" : queueHref(metric === "withdrawals" ? "withdrawal" : metric === "outflows" ? "outflow" : "inbound")}>Открыть</Link></>}
  </CardContent></>
}
