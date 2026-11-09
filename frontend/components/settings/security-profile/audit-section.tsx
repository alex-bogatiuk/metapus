"use client"

import { Badge } from "@/components/ui/badge"
import { api } from "@/lib/api"
import { formatAuditChanges, getActionLabel } from "@/lib/audit-format"
import { cn } from "@/lib/utils"
import type {
  AuditEntryResponse
} from "@/types/security"
import {
  Loader2
} from "lucide-react"
import { useEffect, useState } from "react"

export function AuditSection({ profileId }: { profileId: string }) {
  const [entries, setEntries] = useState<AuditEntryResponse[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    api.security.profiles.auditHistory(profileId, 100).then((res) => {
      if (!cancelled) {
        setEntries(res.items ?? [])
        setLoading(false)
      }
    }).catch(() => {
      if (!cancelled) setLoading(false)
    })
    return () => { cancelled = true }
  }, [profileId])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (entries.length === 0) {
    return (
      <div className="text-center text-sm text-muted-foreground py-8">
        Нет записей в журнале изменений
      </div>
    )
  }

  return (
    <div className="max-w-2xl space-y-2">
      {entries.map((entry) => {
        const lines = formatAuditChanges(entry.action, entry.changes)
        return (
          <div key={entry.id} className="rounded-md border p-3 space-y-1">
            <div className="flex items-center justify-between">
              <Badge variant={entry.action === "delete" ? "destructive" : "secondary"} className="text-[10px]">
                {getActionLabel(entry.action)}
              </Badge>
              <span className="text-[11px] text-muted-foreground">
                {new Date(entry.createdAt).toLocaleString("ru-RU")}
              </span>
            </div>
            {entry.userEmail && (
              <p className="text-xs text-muted-foreground">{entry.userEmail}</p>
            )}
            {lines.length > 0 && (
              <div className="mt-1 rounded bg-muted/40 p-2 text-[11px] space-y-0.5">
                {lines.map((line, i) => (
                  <div
                    key={i}
                    className={cn(
                      "flex gap-1.5",
                      line.variant === "added" && "text-success",
                      line.variant === "removed" && "text-destructive",
                      line.variant === "changed" && "text-foreground",
                      line.variant === "neutral" && "text-muted-foreground",
                    )}
                  >
                    <span className="truncate">{line.text}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
