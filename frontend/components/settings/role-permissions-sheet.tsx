"use client"

import { useCallback, useEffect, useState } from "react"
import { Loader2, Shield } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { ScrollArea } from "@/components/ui/scroll-area"
import { api } from "@/lib/api"
import { toast } from "sonner"
import type { RoleResponse, PermissionResponse } from "@/types/security"

interface RolePermissionsSheetProps {
  role: RoleResponse | null
  onClose: () => void
}

export function RolePermissionsSheet({ role, onClose }: RolePermissionsSheetProps) {
  const [permissions, setPermissions] = useState<PermissionResponse[]>([])
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    if (!role) return
    setLoading(true)
    try {
      const res = await api.roles.getPermissions(role.id)
      setPermissions(res.items ?? [])
    } catch {
      toast.error("Не удалось загрузить разрешения")
    } finally {
      setLoading(false)
    }
  }, [role])

  useEffect(() => { load() }, [load])

  // Group permissions by resource
  const grouped = permissions.reduce<Record<string, PermissionResponse[]>>((acc, p) => {
    const key = p.resource || "other"
    if (!acc[key]) acc[key] = []
    acc[key].push(p)
    return acc
  }, {})

  return (
    <Sheet open={!!role} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="w-full sm:max-w-md p-0 flex flex-col">
        <SheetHeader className="px-6 py-4 border-b shrink-0">
          <div className="flex items-center gap-2">
            <Shield className="h-4 w-4 text-primary" />
            <SheetTitle className="text-base">{role?.name}</SheetTitle>
            {role?.isSystem && (
              <Badge variant="secondary" className="text-[10px]">Системная</Badge>
            )}
          </div>
          {role?.description && (
            <p className="text-xs text-muted-foreground mt-1">{role.description}</p>
          )}
        </SheetHeader>

        <ScrollArea className="flex-1">
          <div className="px-6 py-4">
            {loading ? (
              <div className="flex items-center justify-center py-8">
                <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
              </div>
            ) : permissions.length === 0 ? (
              <div className="py-8 text-center text-sm text-muted-foreground">
                Нет разрешений
              </div>
            ) : (
              <div className="space-y-4">
                {Object.entries(grouped).map(([resource, perms]) => (
                  <div key={resource} className="space-y-1.5">
                    <h4 className="text-xs font-medium text-foreground font-mono">
                      {resource}
                    </h4>
                    <div className="flex flex-wrap gap-1.5">
                      {perms.map((p) => (
                        <Badge
                          key={p.id}
                          variant="outline"
                          className="text-[10px] font-mono"
                          title={p.description || p.code}
                        >
                          {p.action}
                        </Badge>
                      ))}
                    </div>
                  </div>
                ))}
                <div className="pt-2 border-t">
                  <p className="text-[11px] text-muted-foreground">
                    Всего: {permissions.length} разрешений
                  </p>
                </div>
              </div>
            )}
          </div>
        </ScrollArea>
      </SheetContent>
    </Sheet>
  )
}
