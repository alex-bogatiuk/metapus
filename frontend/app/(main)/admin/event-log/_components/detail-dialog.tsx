import { format } from "date-fns"
import { ru } from "date-fns/locale"
import { Copy, ExternalLink } from "lucide-react"
import { useRouter } from "next/navigation"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { useTabsStore } from "@/stores/useTabsStore"
import type { EventLogEntry } from "@/types/event-log"
import { SeverityBadge } from "./severity-badge"

/** Resolves entity metadata for display in the detail dialog. */
function resolveEntityInfo(entityType?: string, entityId?: string) {
  if (!entityType) return null

  const meta = useMetadataStore.getState()
  const entity = meta.getEntity(entityType)

  const label = entity
    ? meta.getLabel(entityType, "singular") || entityType
    : entityType

  let url: string | null = null
  if (entity && entityId) {
    const prefix = entity.routePrefix
    if (prefix) {
      const section = entity.type === "document" ? "documents" : "catalogs"
      const routePrefix = entity.type === "document" && !prefix.endsWith("s") ? prefix + "s" : prefix
      url = `/${section}/${routePrefix}/${entityId}`
    }
  }

  return { label, url }
}

export function DetailDialog({
  event,
  open,
  onOpenChange,
  onOpenTrace,
}: {
  event: EventLogEntry | null
  open: boolean
  onOpenChange: (v: boolean) => void
  onOpenTrace: (traceId: string) => void
}) {
  const router = useRouter()

  if (!event) return null

  const entityInfo = resolveEntityInfo(event.entityType, event.entityId)

  // Build human-readable entity display
  const entityDisplay = entityInfo
    ? event.entityNumber
      ? `${entityInfo.label} ${event.entityNumber}`
      : entityInfo.label
    : "—"

  const fields = [
    { label: "ID", value: event.id },
    { label: "Категория", value: event.category },
    { label: "Важность", value: event.severity },
    { label: "Тип", value: event.eventType },
    { label: "Источник", value: event.source },
    { label: "Пользователь", value: event.userEmail || event.userId || "—" },
    { label: "IP", value: event.clientIp || "—" },
    { label: "Сущность", value: entityDisplay },
    { label: "Номер", value: event.entityNumber || "—" },
    { label: "Длительность", value: event.durationMs != null ? `${event.durationMs}ms` : "—" },
    { label: "Request ID", value: event.requestId || "—" },
    { label: "Время", value: format(new Date(event.createdAt), "dd.MM.yyyy HH:mm:ss.SSS", { locale: ru }) },
  ]

  const handleEntityClick = () => {
    if (!entityInfo?.url) return
    const tabLabel = event.entityNumber
      ? `${entityInfo.label} ${event.entityNumber}`
      : entityInfo.label
    useTabsStore.getState().openTab({ id: entityInfo.url, title: tabLabel, url: entityInfo.url })
    router.push(entityInfo.url)
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[80vh]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <SeverityBadge severity={event.severity} />
            <span className="truncate">{event.eventType}</span>
          </DialogTitle>
          <DialogDescription className="sr-only">Детали события</DialogDescription>
        </DialogHeader>
        <ScrollArea className="max-h-[60vh]">
          <div className="space-y-2 pr-3">
            <p className="text-sm">{event.message}</p>
            <Separator />
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
              {fields.map((f) => (
                <div key={f.label} className="contents">
                  <dt className="text-muted-foreground">{f.label}</dt>
                  <dd className="font-mono text-xs break-all">{f.value}</dd>
                </div>
              ))}
            </dl>

            {/* Entity deep-link + copy ID */}
            {entityInfo && (
              <>
                <Separator />
                <div className="flex items-center gap-2 flex-wrap">
                  {entityInfo.url && (
                    <Button
                      variant="outline"
                      size="sm"
                      className="gap-1.5"
                      onClick={handleEntityClick}
                    >
                      <ExternalLink className="h-3.5 w-3.5" />
                      Открыть {entityInfo.label}
                      {event.entityNumber ? ` ${event.entityNumber}` : ""}
                    </Button>
                  )}
                  {event.entityId && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="gap-1 text-xs text-muted-foreground font-mono"
                      onClick={() => {
                        navigator.clipboard.writeText(event.entityId!)
                      }}
                    >
                      <Copy className="h-3 w-3" />
                      ID
                    </Button>
                  )}
                </div>
              </>
            )}

            {event.traceId && (
              <>
                <Separator />
                <div className="flex items-center gap-2">
                  <span className="text-sm text-muted-foreground">Trace ID:</span>
                  <Button
                    variant="link"
                    size="sm"
                    className="h-auto p-0 font-mono text-xs"
                    onClick={() => {
                      onOpenChange(false)
                      onOpenTrace(event.traceId!)
                    }}
                  >
                    {event.traceId}
                  </Button>
                </div>
              </>
            )}

            {event.details && Object.keys(event.details).length > 0 && (
              <>
                <Separator />
                <div>
                  <p className="text-sm text-muted-foreground mb-1">Детали</p>
                  <ScrollArea className="max-h-40">
                    <pre className="text-xs bg-muted p-2 rounded">
                      {JSON.stringify(event.details, null, 2)}
                    </pre>
                  </ScrollArea>
                </div>
              </>
            )}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
