"use client"
import Link from "next/link"
import type { ReactNode } from "react"
import { ArrowDownToLine, ArrowRightLeft, ArrowUpFromLine, Check, ChevronRight, Network, type LucideIcon } from "lucide-react"
import { api } from "@/lib/api"
import { cryptoApi } from "@/configuration/crypto/lib/api"
import { cn } from "@/lib/utils"
import { useAdminNetworks } from "@/configuration/crypto/hooks/useAdminNetworks"
import { useWidgetData } from "@/hooks/useWidgetData"
import { counterLabels, networkProblem, queueHref } from "@/configuration/crypto/lib/admin-dashboard"
import type { WidgetRenderProps } from "@/types/dashboard"
import type { CounterConfig } from "@/configuration/crypto/lib/widget-configs"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"
import { StatusBadge } from "@/components/dashboard/widgets/widget-state"

type Metric = WidgetRenderProps<CounterConfig>["config"]["metric"]
const icons: Record<Metric, LucideIcon> = { withdrawals: ArrowUpFromLine, inbound: ArrowDownToLine, outflows: ArrowRightLeft, networks: Network }
const busyHint: Record<Metric, string> = { withdrawals: "Ждут решения", inbound: "Ждут зачисления", outflows: "Ждут классификации", networks: "Не обрабатывают операции" }
const calmHint: Record<Metric, string> = { withdrawals: "Очередь пуста", inbound: "Очередь пуста", outflows: "Очередь пуста", networks: "Все сети работают" }

export default function Counter({ config, isEditMode }: WidgetRenderProps<CounterConfig>) {
  const { metric } = config
  const queues = useWidgetData(() => cryptoApi.dashboard.queues(), { key: "widget:admin-queue:all:5", isEditMode, enabled: metric !== "networks", pollInterval: 60_000 })
  const networks = useAdminNetworks({ isEditMode, enabled: metric === "networks" })
  const data = metric === "networks" ? networks : queues
  const count = metric === "networks" ? networks.data?.items.filter(n => networkProblem(n, networks.data?.evaluatedAt)).length : queues.data?.[metric]
  const href = metric === "networks" ? "/admin/crypto-networks" : queueHref(metric === "withdrawals" ? "withdrawal" : metric === "outflows" ? "outflow" : "inbound")
  // A problem network stops processing, so it is louder than a queue that waits for a person.
  const tone = count == null ? "unknown" : count === 0 ? "calm" : metric === "networks" ? "problem" : "busy"
  const Icon = tone === "calm" ? Check : icons[metric]
  const label = counterLabels[metric]

  const body = <>
    <div className="flex items-center justify-between gap-2">
      <span className="line-clamp-2 text-[13px] font-medium leading-snug text-foreground/80">{label}</span>
      <span aria-hidden className={cn("flex size-7 shrink-0 items-center justify-center rounded-md [&_svg]:size-4",
        tone === "calm" && "bg-success/10 text-success",
        tone === "busy" && "bg-warning/10 text-warning",
        tone === "problem" && "bg-destructive/10 text-destructive",
        tone === "unknown" && "bg-muted text-muted-foreground")}><Icon /></span>
    </div>
    {data.loading ? <Skeleton className="h-8 w-16" /> : data.error
      ? <Button variant="outline" size="sm" className="self-start" onClick={data.refetch}>Повторить</Button>
      : <div className="flex items-center justify-between gap-2">
        <span className={cn("text-[26px] font-semibold leading-none tracking-tight tabular-nums", tone === "calm" && "text-muted-foreground")}>{count ?? "—"}</span>
        <span className="flex min-w-0 items-center gap-1">
          {tone === "busy" || tone === "problem"
            ? <StatusBadge tone={tone === "problem" ? "destructive" : "warning"}>{busyHint[metric]}</StatusBadge>
            : tone === "calm" ? <span className="truncate text-xs text-muted-foreground">{calmHint[metric]}</span> : null}
          {!isEditMode && <ChevronRight aria-hidden className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-[opacity,transform] duration-150 group-hover:translate-x-0.5 group-hover:opacity-100 group-focus-visible:opacity-100 motion-reduce:transition-none" />}
        </span>
      </div>}
  </>
  return <CounterSurface href={href} label={label} interactive={!isEditMode && !data.error}>{body}</CounterSurface>
}

/** The whole card opens the queue; in edit mode it must stay a plain surface so dragging never navigates. */
function CounterSurface({ href, label, interactive, children }: { href: string; label: string; interactive: boolean; children: ReactNode }) {
  const className = "group flex h-full min-w-0 flex-1 flex-col justify-between gap-3 px-4 py-3.5"
  return interactive
    ? <Link href={href} aria-label={`Открыть: ${label}`} className={cn(className, "outline-none transition-colors duration-150 hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:ring-[3px] focus-visible:ring-inset focus-visible:ring-ring/25 motion-reduce:transition-none")}>{children}</Link>
    : <div className={className}>{children}</div>
}
