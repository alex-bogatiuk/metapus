"use client"

import { useCallback, useEffect, useState } from "react"
import { Loader2, Shield, Eye, EyeOff, AlertTriangle } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion"
import { api } from "@/lib/api"
import { toast } from "sonner"
import type { UserResponse, EffectiveAccessResponse } from "@/types/security"

interface EffectiveAccessDialogProps {
  user: UserResponse | null
  onClose: () => void
}

export function EffectiveAccessDialog({ user, onClose }: EffectiveAccessDialogProps) {
  const [data, setData] = useState<EffectiveAccessResponse | null>(null)
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    if (!user) return
    setLoading(true)
    try {
      const res = await api.users.effectiveAccess(user.id)
      setData(res)
    } catch {
      toast.error("Не удалось загрузить эффективные права")
    } finally {
      setLoading(false)
    }
  }, [user])

  useEffect(() => { load() }, [load])

  // Group permissions by resource
  const groupedPerms = (data?.permissions ?? []).reduce<Record<string, string[]>>((acc, p) => {
    const parts = p.split(":")
    const resource = parts.length >= 3 ? `${parts[0]}:${parts[1]}` : "other"
    const action = parts.length >= 3 ? parts[2] : p
    if (!acc[resource]) acc[resource] = []
    acc[resource].push(action)
    return acc
  }, {})

  return (
    <Dialog open={!!user} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="text-base">Эффективные права</DialogTitle>
          <p className="text-xs text-muted-foreground">{user?.fullName || user?.email}</p>
        </DialogHeader>

        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : data ? (
          <Accordion type="multiple" defaultValue={["rbac", "rls", "fls", "cel"]} className="space-y-2">
            {/* RBAC Permissions */}
            <AccordionItem value="rbac" className="border rounded-md px-3">
              <AccordionTrigger className="py-2 text-sm hover:no-underline">
                <div className="flex items-center gap-2">
                  <Shield className="h-3.5 w-3.5 text-primary" />
                  <span>RBAC-разрешения</span>
                  <Badge variant="secondary" className="text-[10px] h-4 ml-1">
                    {data.permissions?.length ?? 0}
                  </Badge>
                </div>
              </AccordionTrigger>
              <AccordionContent className="pb-3">
                {Object.keys(groupedPerms).length === 0 ? (
                  <p className="text-xs text-muted-foreground">Нет разрешений</p>
                ) : (
                  <div className="space-y-2">
                    {Object.entries(groupedPerms).map(([resource, actions]) => (
                      <div key={resource}>
                        <p className="text-[11px] font-mono text-muted-foreground mb-1">{resource}</p>
                        <div className="flex flex-wrap gap-1">
                          {actions.map((a) => (
                            <Badge key={a} variant="outline" className="text-[10px] font-mono">{a}</Badge>
                          ))}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </AccordionContent>
            </AccordionItem>

            {/* RLS Dimensions */}
            <AccordionItem value="rls" className="border rounded-md px-3">
              <AccordionTrigger className="py-2 text-sm hover:no-underline">
                <div className="flex items-center gap-2">
                  <Eye className="h-3.5 w-3.5 text-primary" />
                  <span>Видимость данных (RLS)</span>
                </div>
              </AccordionTrigger>
              <AccordionContent className="pb-3">
                {!data.rlsDimensions || Object.keys(data.rlsDimensions).length === 0 ? (
                  <p className="text-xs text-muted-foreground">Без ограничений — видны все данные</p>
                ) : (
                  <div className="space-y-2">
                    {Object.entries(data.rlsDimensions).map(([dim, items]) => (
                      <div key={dim}>
                        <p className="text-[11px] font-medium mb-1 capitalize">{dim}</p>
                        <div className="flex flex-wrap gap-1">
                          {items.map((item) => (
                            <Badge key={item.id} variant="secondary" className="text-[10px]">
                              {item.name || item.id.slice(0, 8) + "…"}
                            </Badge>
                          ))}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </AccordionContent>
            </AccordionItem>

            {/* FLS Policies */}
            <AccordionItem value="fls" className="border rounded-md px-3">
              <AccordionTrigger className="py-2 text-sm hover:no-underline">
                <div className="flex items-center gap-2">
                  <EyeOff className="h-3.5 w-3.5 text-primary" />
                  <span>Скрытые поля (FLS)</span>
                  {(data.flsPolicies?.length ?? 0) > 0 && (
                    <Badge variant="secondary" className="text-[10px] h-4 ml-1">
                      {data.flsPolicies!.length}
                    </Badge>
                  )}
                </div>
              </AccordionTrigger>
              <AccordionContent className="pb-3">
                {!data.flsPolicies || data.flsPolicies.length === 0 ? (
                  <p className="text-xs text-muted-foreground">Без ограничений — все поля видимы</p>
                ) : (
                  <div className="space-y-2">
                    {data.flsPolicies.map((fp, i) => (
                      <div key={i} className="rounded border bg-muted/30 p-2">
                        <p className="text-[11px] font-mono mb-1">
                          {fp.entityName}:{fp.action}
                        </p>
                        <div className="flex flex-wrap gap-1">
                          {(fp.hiddenFields ?? []).map((f) => (
                            <Badge key={f} variant="destructive" className="text-[10px] font-mono">
                              {f}
                            </Badge>
                          ))}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </AccordionContent>
            </AccordionItem>

            {/* CEL Rules */}
            <AccordionItem value="cel" className="border rounded-md px-3">
              <AccordionTrigger className="py-2 text-sm hover:no-underline">
                <div className="flex items-center gap-2">
                  <AlertTriangle className="h-3.5 w-3.5 text-primary" />
                  <span>Бизнес-правила (CEL)</span>
                  {(data.celRules?.length ?? 0) > 0 && (
                    <Badge variant="secondary" className="text-[10px] h-4 ml-1">
                      {data.celRules!.length}
                    </Badge>
                  )}
                </div>
              </AccordionTrigger>
              <AccordionContent className="pb-3">
                {!data.celRules || data.celRules.length === 0 ? (
                  <p className="text-xs text-muted-foreground">Нет активных правил</p>
                ) : (
                  <div className="space-y-2">
                    {data.celRules.map((rule, i) => (
                      <div key={i} className="rounded border p-2 space-y-1">
                        <div className="flex items-center gap-2">
                          <Badge variant="outline" className="text-[10px] font-mono h-4 min-w-5 justify-center">
                            {rule.priority}
                          </Badge>
                          <span className="text-xs font-medium">{rule.name}</span>
                          <Badge
                            variant={rule.effect === "deny" ? "destructive" : "default"}
                            className="text-[10px] h-4"
                          >
                            {rule.effect === "deny" ? "Запрет" : "Разрешение"}
                          </Badge>
                        </div>
                        <code className="block text-[10px] font-mono text-muted-foreground bg-muted/50 rounded px-2 py-0.5">
                          {rule.expression}
                        </code>
                      </div>
                    ))}
                  </div>
                )}
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
