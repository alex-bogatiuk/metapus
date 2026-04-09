"use client"

import { useCallback, useEffect, useState } from "react"
import { format } from "date-fns"
import { ru } from "date-fns/locale"
import { AlertCircle, Loader2, RefreshCw } from "lucide-react"

import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import type { EventLogEntry } from "@/types/event-log"
import { SeverityBadge } from "./severity-badge"

export function TraceDialog({
  traceId,
  open,
  onOpenChange,
}: {
  traceId: string
  open: boolean
  onOpenChange: (v: boolean) => void
}) {
  const [items, setItems] = useState<EventLogEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadTrace = useCallback(async () => {
    if (!traceId) return
    setLoading(true)
    setError(null)
    try {
      const r = await api.system.eventLog.trace(traceId)
      setItems(r.items)
    } catch (err) {
      setItems([])
      setError(err instanceof Error ? err.message : "Не удалось загрузить цепочку трассировки")
    } finally {
      setLoading(false)
    }
  }, [traceId])

  useEffect(() => {
    if (!open || !traceId) return
    loadTrace()
  }, [open, traceId, loadTrace])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[80vh]">
        <DialogHeader>
          <DialogTitle>Цепочка трассировки</DialogTitle>
          <DialogDescription className="text-xs text-muted-foreground font-mono">{traceId}</DialogDescription>
        </DialogHeader>
        <ScrollArea className="max-h-[60vh]">
          {loading ? (
            <div className="flex justify-center py-8">
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : error ? (
            <div className="flex flex-col items-center gap-3 py-8 text-muted-foreground">
              <AlertCircle className="h-8 w-8 text-red-500 opacity-60" />
              <p className="text-sm text-center">{error}</p>
              <Button variant="outline" size="sm" onClick={loadTrace} className="gap-1.5">
                <RefreshCw className="h-3.5 w-3.5" />
                Повторить
              </Button>
            </div>
          ) : items.length === 0 ? (
            <p className="text-center text-muted-foreground py-8">Нет событий</p>
          ) : (
            <div className="space-y-3 pr-3">
              {items.map((e, i) => (
                <div key={e.id} className="relative pl-6">
                  {i < items.length - 1 && (
                    <div className="absolute left-[9px] top-6 bottom-0 w-px bg-border" />
                  )}
                  <div className="absolute left-0 top-1.5 h-[18px] w-[18px] rounded-full border-2 border-border bg-background flex items-center justify-center">
                    <div
                      className={`h-2 w-2 rounded-full ${
                        e.severity === "error" || e.severity === "critical"
                          ? "bg-red-500"
                          : e.severity === "warning"
                          ? "bg-yellow-500"
                          : "bg-blue-500"
                      }`}
                    />
                  </div>
                  <div className="pb-3">
                    <div className="flex items-center gap-2 mb-1">
                      <SeverityBadge severity={e.severity} />
                      <span className="text-xs font-mono text-muted-foreground">
                        {format(new Date(e.createdAt), "HH:mm:ss.SSS", { locale: ru })}
                      </span>
                      {e.durationMs != null && (
                        <span className="text-xs text-muted-foreground">
                          {e.durationMs}ms
                        </span>
                      )}
                    </div>
                    <p className="text-sm">{e.message}</p>
                    <p className="text-xs text-muted-foreground">
                      {e.eventType}
                      {e.entityType && ` · ${e.entityType}`}
                      {e.userEmail && ` · ${e.userEmail}`}
                    </p>
                  </div>
                </div>
              ))}
            </div>
          )}
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
