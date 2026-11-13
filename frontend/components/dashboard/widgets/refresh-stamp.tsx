"use client"
import { useSyncExternalStore, type ReactNode } from "react"
import { SWRConfig } from "swr"

// One stamp for the page: every widget response carries the server's "now", so per-card times only repeat it.
let lastSuccess: number | null = null
const listeners = new Set<() => void>()
function markSuccess() {
  lastSuccess = Date.now()
  listeners.forEach(listener => listener())
}
function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}
const config = { onSuccess: markSuccess }

/** Records when any widget below last received data; nested SWRConfig merges with the scoped cache above it. */
export function DashboardDataScope({ children }: { children: ReactNode }) {
  return <SWRConfig value={config}>{children}</SWRConfig>
}

export function RefreshStamp() {
  const at = useSyncExternalStore(subscribe, () => lastSuccess, () => null)
  if (at == null) return null
  const time = new Date(at).toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" })
  return <p className="text-xs text-muted-foreground" aria-live="polite">Обновлено в <time dateTime={new Date(at).toISOString()} className="tabular-nums">{time}</time> · данные обновляются каждую минуту</p>
}
