"use client"
import Link from "next/link"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useAdminNetworks } from "@/configuration/crypto/hooks/useAdminNetworks"
import { useWidgetData } from "@/hooks/useWidgetData"
import { networkHealth, observedTime } from "@/lib/admin-dashboard"
import type { WidgetRenderProps } from "@/types/dashboard"
import type { CryptoNetworkState } from "@/configuration/crypto/types/crypto-network"
import { Network } from "lucide-react"
import { StatusBadge, WidgetBody, WidgetEmpty, WidgetLink } from "./widget-state"
import { useCompactRows } from "./compact-rows"

type Tone = "success" | "warning" | "destructive" | "neutral"
const dots: Record<Tone, string> = {
  success: "bg-success shadow-[0_0_0_3px_hsl(var(--success)/0.15)]",
  warning: "bg-warning shadow-[0_0_0_3px_hsl(var(--warning)/0.15)]",
  destructive: "bg-destructive shadow-[0_0_0_3px_hsl(var(--destructive)/0.15)]",
  neutral: "bg-muted-foreground/50",
}

function networkStatus(state: CryptoNetworkState, now?: number): { tone: Tone; label: string } {
  if (!state.watching) return { tone: "neutral", label: "Не читается" }
  if (!state.policy.enabled) return { tone: "neutral", label: "Отключена" }
  const health = networkHealth(state, now)
  if (!health) return { tone: "success", label: "Работает" }
  return { tone: health.level === "problem" ? "destructive" : "warning", label: health.label }
}

export default function Health({ config, isEditMode }: WidgetRenderProps<"processing-health">) {
  const networks = useAdminNetworks({ isEditMode })
  const workers = useWidgetData(() => api.system.workerJobs.stats(), { key: "widget:admin-worker-stats", isEditMode, enabled: config.showWorkerStats, pollInterval: 60_000 })
  const items = networks.data?.items ?? []
  const stats = workers.data
  // No networks yet: a line with the way to set them up, plus the worker stats when shown.
  useCompactRows(networks.data && !networks.error && items.length === 0 ? (config.showWorkerStats ? 2 : 1) : null)
  const errorShare = stats?.total ? Math.min(100, (stats.error / stats.total) * 100) : 0

  return <WidgetBody title="Состояние обработки" description="Сети и задачи воркера"
    loading={networks.loading} error={networks.error} retry={networks.refetch}
    action={!isEditMode && items.length ? <WidgetLink href="/admin/crypto-networks">Сети</WidgetLink> : undefined}
    flush={!!items.length}
    footer={config.showWorkerStats && <div className="flex w-full flex-col gap-2">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <Link href="/admin/worker-jobs" className="font-medium text-foreground/80 hover:text-foreground hover:underline">Задачи за 24 часа</Link>
        {workers.error
          ? <span>Нет данных о задачах · <button className="font-medium text-info hover:underline" onClick={workers.refetch}>Повторить</button></span>
          : workers.loading ? <span>Загрузка…</span>
          : <span>Запуски: <b className="font-semibold text-foreground tabular-nums">{stats?.total ?? 0}</b> · Ошибки: <b className={cn("font-semibold tabular-nums", stats?.error ? "text-destructive" : "text-foreground")}>{stats?.error ?? 0}</b></span>}
      </div>
      {!!stats?.total && <div role="img" aria-label={`Успешных запусков: ${stats.total - stats.error}, с ошибкой: ${stats.error}`} className="flex h-1.5 gap-px overflow-hidden rounded-full bg-muted">
        <span className="h-full bg-success/70" style={{ width: `${100 - errorShare}%` }} />
        {!!stats.error && <span className="h-full bg-destructive" style={{ width: `${errorShare}%` }} />}
      </div>}
    </div>}
  >
    {!items.length ? <WidgetEmpty inline icon={Network} title="Сети не настроены" description="Подключите сеть, чтобы принимать и отправлять платежи." action={{ label: "Настроить сети", href: "/admin/crypto-networks" }} />
      : <ul className="flex flex-col px-2 pb-2">{items.map(n => {
        const status = networkStatus(n, networks.data?.evaluatedAt)
        return <li key={n.network.id}>
          <Link href="/admin/crypto-networks" className="flex items-center gap-3 rounded-md px-2 py-2.5 transition-colors hover:bg-muted/70 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/25">
            <span aria-hidden className={cn("size-2 shrink-0 rounded-full", dots[status.tone])} />
            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="truncate font-medium">{n.network.code}</span>
              <span className="text-xs text-muted-foreground">Состояние {observedTime(n.runtime.observedAt)}</span>
            </span>
            <StatusBadge tone={status.tone}>{status.label}</StatusBadge>
          </Link>
        </li>
      })}</ul>}
  </WidgetBody>
}
