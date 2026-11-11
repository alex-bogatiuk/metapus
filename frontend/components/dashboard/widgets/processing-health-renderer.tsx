"use client"
import Link from "next/link"
import { api } from "@/lib/api"
import { useAdminNetworks } from "@/hooks/useAdminNetworks"
import { useWidgetData } from "@/hooks/useWidgetData"
import { networkProblem, observedTime } from "@/lib/admin-dashboard"
import type { WidgetRenderProps } from "@/types/dashboard"
import { Badge } from "@/components/ui/badge"
import { WidgetBody, WidgetEmpty } from "./widget-state"

export default function Health({ config, isEditMode }: WidgetRenderProps<"processing-health">) {
  const networks = useAdminNetworks({ isEditMode })
  const workers = useWidgetData(() => api.system.workerJobs.stats(), { key: "widget:admin-worker-stats", isEditMode, enabled: config.showWorkerStats, pollInterval: 60_000 })
  return <WidgetBody title="Состояние обработки" description="Сети и задачи воркера" loading={networks.loading} error={networks.error} retry={networks.refetch}>
    {!networks.data?.items.length ? <WidgetEmpty title="Сети не настроены" /> : <div className="divide-y">{networks.data.items.map(n => <div key={n.network.id} className="space-y-1 py-3 first:pt-0"><div className="flex flex-wrap items-center justify-between gap-2"><Link href="/admin/crypto-networks" className="font-medium hover:underline">{n.network.code}</Link><Badge variant={networkProblem(n, networks.data?.evaluatedAt) ? "destructive" : "secondary"}>{!n.watching ? "Не читается" : !n.policy.enabled ? "Отключена" : networkProblem(n, networks.data?.evaluatedAt) ?? "Работает"}</Badge></div><p className="text-xs text-muted-foreground">Состояние {observedTime(n.runtime.observedAt)}</p></div>)}</div>}
    {config.showWorkerStats && <div className="mt-3 border-t pt-3 text-sm"><Link href="/admin/worker-jobs" className="font-medium hover:underline">Задачи за 24 часа</Link><p className="mt-1 text-muted-foreground">{workers.error ? "Нет данных о задачах" : workers.loading ? "Загрузка…" : `Запуски: ${workers.data?.total ?? 0} · Ошибки: ${workers.data?.error ?? 0}`}</p>{workers.error && <button className="text-xs underline" onClick={workers.refetch}>Повторить</button>}</div>}
  </WidgetBody>
}
