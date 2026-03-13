"use client"

import { useCallback, useEffect, useState } from "react"
import { Loader2, Check, Minus, ShieldCheck, Lock, Eye } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { api } from "@/lib/api"
import type {
  RoleResponse,
  PermissionResponse,
  SecurityProfileResponse,
} from "@/types/security"

// ── Types ────────────────────────────────────────────────────────────

interface MatrixData {
  roles: RoleResponse[]
  profiles: SecurityProfileResponse[]
  permissions: PermissionResponse[]
  /** roleId → Set<permissionCode> */
  rolePerms: Map<string, Set<string>>
}

// ── Resource labels ──────────────────────────────────────────────────

const RESOURCE_LABELS: Record<string, string> = {
  catalog: "Справочники",
  "document:goods_receipt": "Поступления товаров",
  "document:goods_issue": "Расход товаров",
  "report:stock": "Отчёты (склад)",
  "report:documents": "Журнал документов",
  "register:stock": "Регистр остатков",
  admin: "Администрирование",
}

/**
 * Extract entity name from a permission code.
 * E.g. 'document:goods_receipt:read' → 'goods_receipt'
 *      'catalog:nomenclature:read'   → 'nomenclature'
 */
function getEntityNameFromCode(code: string): string {
  const parts = code.split(':')
  if (parts.length >= 3) return parts[1]
  return code
}

/**
 * Normalize entity name for cross-system comparison.
 * Strips underscores and lowercases so 'goods_receipt' matches 'GoodsReceipt'.
 */
function normalizeEntity(name: string): string {
  return name.replace(/_/g, '').toLowerCase()
}

const ACTION_LABELS: Record<string, string> = {
  create: "Создание",
  read: "Чтение",
  update: "Изменение",
  delete: "Удаление",
  list: "Список",
  post: "Проведение",
  unpost: "Отмена пров.",
}

const ACTION_COLORS: Record<string, string> = {
  read: "text-blue-600",
  create: "text-emerald-600",
  update: "text-amber-600",
  delete: "text-red-600",
  post: "text-violet-600",
  unpost: "text-orange-600",
  list: "text-sky-600",
}

// ── Component ────────────────────────────────────────────────────────

export function AccessMatrix() {
  const [data, setData] = useState<MatrixData | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [rolesRes, permsRes, profilesRes] = await Promise.all([
        api.roles.list(),
        api.permissions.list(),
        api.security.profiles.list(),
      ])

      const roles = rolesRes.items ?? []
      const permissions = permsRes.items ?? []
      const profiles = profilesRes.items ?? []

      // Load permissions per role
      const rolePerms = new Map<string, Set<string>>()
      await Promise.all(
        roles.map(async (role) => {
          try {
            const res = await api.roles.getPermissions(role.id)
            const perms = (res.items ?? []).map((p) => p.code)
            rolePerms.set(role.id, new Set(perms))
          } catch {
            rolePerms.set(role.id, new Set())
          }
        })
      )

      setData({ roles, profiles, permissions, rolePerms })
    } catch {
      // silently fail
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  if (loading) {
    return (
      <div className="flex items-center gap-2 py-8 justify-center text-xs text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        Загрузка матрицы доступа...
      </div>
    )
  }

  if (!data) {
    return (
      <p className="text-xs text-muted-foreground py-4 text-center">
        Не удалось загрузить данные
      </p>
    )
  }

  // Group permissions by resource
  const resources = [...new Set(data.permissions.map((p) => p.resource))].sort()
  const permsByResource = new Map<string, PermissionResponse[]>()
  for (const perm of data.permissions) {
    if (!permsByResource.has(perm.resource)) permsByResource.set(perm.resource, [])
    permsByResource.get(perm.resource)!.push(perm)
  }

  // Determine which profiles have FLS/CEL restrictions per entity
  const profileRestrictions = new Map<string, { fls: number; cel: number }>()
  for (const profile of data.profiles) {
    const flsCount = profile.fieldPolicies?.length ?? 0
    const celCount = profile.policyRules?.filter((r) => r.enabled).length ?? 0
    profileRestrictions.set(profile.id, { fls: flsCount, cel: celCount })
  }

  return (
    <TooltipProvider delayDuration={200}>
      <div className="space-y-4">
        <p className="text-xs text-muted-foreground">
          Сводная таблица доступа: роли определяют RBAC-разрешения, профили безопасности — дополнительные ограничения (скрытие полей, условия).
        </p>

        <div className="rounded-md border overflow-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b bg-muted/40">
                <th className="px-3 py-2 text-left text-[11px] font-medium text-muted-foreground sticky left-0 bg-muted/40 z-10 min-w-[140px]">
                  Ресурс / Действие
                </th>
                {data.roles.map((role) => (
                  <th
                    key={role.id}
                    className="px-2 py-2 text-center text-[11px] font-medium text-muted-foreground min-w-[80px]"
                  >
                    <div className="flex flex-col items-center gap-0.5">
                      <Badge variant="outline" className="text-[9px] h-4 font-normal">
                        {role.name}
                      </Badge>
                      <span className="text-[9px] text-muted-foreground/60">{role.code}</span>
                    </div>
                  </th>
                ))}
                {data.profiles.length > 0 && (
                  <th className="px-2 py-2 text-center text-[11px] font-medium text-muted-foreground border-l min-w-[60px]">
                    {/* Spacer for profiles section */}
                  </th>
                )}
                {data.profiles.map((profile) => {
                  const r = profileRestrictions.get(profile.id)
                  return (
                    <th
                      key={profile.id}
                      className="px-2 py-2 text-center text-[11px] font-medium text-muted-foreground min-w-[80px]"
                    >
                      <div className="flex flex-col items-center gap-0.5">
                        <Badge variant="secondary" className="text-[9px] h-4 font-normal gap-0.5">
                          <ShieldCheck className="h-2.5 w-2.5" />
                          {profile.name}
                        </Badge>
                        <span className="text-[9px] text-muted-foreground/60">
                          {r && (r.fls > 0 || r.cel > 0)
                            ? `${r.fls} полей, ${r.cel} усл.`
                            : "без огр."}
                        </span>
                      </div>
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {resources.map((resource) => {
                const perms = permsByResource.get(resource) ?? []
                const actions = [...new Set(perms.map((p) => p.action))].sort()
                const resourceLabel = RESOURCE_LABELS[resource] ?? resource

                return actions.map((action, actionIdx) => {
                  const permCode = perms.find((p) => p.action === action)?.code
                  return (
                  <tr
                    key={`${resource}-${action}`}
                    className={cn(
                      "border-b last:border-b-0 hover:bg-muted/20",
                      actionIdx === 0 && "border-t",
                    )}
                  >
                    <td className="px-3 py-1.5 sticky left-0 bg-background z-10">
                      {actionIdx === 0 && (
                        <span className="font-medium text-foreground">{resourceLabel}</span>
                      )}
                      <span className={cn(
                        actionIdx === 0 ? "ml-2" : "ml-4",
                        ACTION_COLORS[action] ?? "text-muted-foreground",
                      )}>
                        {ACTION_LABELS[action] ?? action}
                      </span>
                    </td>

                    {/* Role columns */}
                    {data.roles.map((role) => {
                      const hasAccess = permCode
                        ? data.rolePerms.get(role.id)?.has(permCode) ?? false
                        : false

                      return (
                        <td key={role.id} className="px-2 py-1.5 text-center">
                          {hasAccess ? (
                            <Tooltip>
                              <TooltipTrigger>
                                <Check className="h-3.5 w-3.5 text-emerald-600 mx-auto" />
                              </TooltipTrigger>
                              <TooltipContent side="top" className="text-[10px]">
                                {permCode}
                              </TooltipContent>
                            </Tooltip>
                          ) : (
                            <Minus className="h-3 w-3 text-muted-foreground/30 mx-auto" />
                          )}
                        </td>
                      )
                    })}

                    {/* Profile spacer */}
                    {data.profiles.length > 0 && (
                      <td className="border-l" />
                    )}

                    {/* Profile columns — show FLS/CEL indicators */}
                    {data.profiles.map((profile) => {
                      const entityName = permCode ? getEntityNameFromCode(permCode) : resource
                      const normEntity = normalizeEntity(entityName)
                      const hasFlsForResource = (profile.fieldPolicies ?? []).some(
                        (fp) => normalizeEntity(fp.entityName) === normEntity && fp.action === action
                      )
                      const hasCelForResource = (profile.policyRules ?? []).some(
                        (r) => r.enabled && (normalizeEntity(r.entityName) === normEntity || r.entityName === '*')
                      )

                      if (hasFlsForResource && hasCelForResource) {
                        return (
                          <td key={profile.id} className="px-2 py-1.5 text-center">
                            <Tooltip>
                              <TooltipTrigger>
                                <div className="flex items-center justify-center gap-0.5">
                                  <Eye className="h-3 w-3 text-violet-500" />
                                  <Lock className="h-3 w-3 text-amber-500" />
                                </div>
                              </TooltipTrigger>
                              <TooltipContent side="top" className="text-[10px]">
                                Скрытие полей + условия
                              </TooltipContent>
                            </Tooltip>
                          </td>
                        )
                      }

                      if (hasFlsForResource) {
                        return (
                          <td key={profile.id} className="px-2 py-1.5 text-center">
                            <Tooltip>
                              <TooltipTrigger>
                                <Eye className="h-3 w-3 text-violet-500 mx-auto" />
                              </TooltipTrigger>
                              <TooltipContent side="top" className="text-[10px]">
                                Скрытие полей
                              </TooltipContent>
                            </Tooltip>
                          </td>
                        )
                      }

                      if (hasCelForResource) {
                        return (
                          <td key={profile.id} className="px-2 py-1.5 text-center">
                            <Tooltip>
                              <TooltipTrigger>
                                <Lock className="h-3 w-3 text-amber-500 mx-auto" />
                              </TooltipTrigger>
                              <TooltipContent side="top" className="text-[10px]">
                                Условия (CEL)
                              </TooltipContent>
                            </Tooltip>
                          </td>
                        )
                      }

                      return (
                        <td key={profile.id} className="px-2 py-1.5 text-center">
                          <Minus className="h-3 w-3 text-muted-foreground/30 mx-auto" />
                        </td>
                      )
                    })}
                  </tr>
                  )
                })
              })}
            </tbody>
          </table>
        </div>

        {/* Legend */}
        <div className="flex flex-wrap items-center gap-4 text-[10px] text-muted-foreground">
          <span className="flex items-center gap-1">
            <Check className="h-3 w-3 text-emerald-600" /> Разрешение (RBAC)
          </span>
          <span className="flex items-center gap-1">
            <Eye className="h-3 w-3 text-violet-500" /> Скрытие полей (FLS)
          </span>
          <span className="flex items-center gap-1">
            <Lock className="h-3 w-3 text-amber-500" /> Условия (CEL)
          </span>
          <span className="flex items-center gap-1">
            <Minus className="h-3 w-3 text-muted-foreground/30" /> Нет доступа / ограничений
          </span>
        </div>
      </div>
    </TooltipProvider>
  )
}
