"use client"

import React, { useCallback, useEffect, useMemo, useState } from "react"
import {
  Loader2,
  Search,
  ChevronRight,
  BookOpen,
  FileText,
  Database,
  BarChart3,
  Settings,
} from "lucide-react"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
// NOTE: Collapsible is NOT used here because Radix renders <div> wrappers
// which are invalid inside <tbody>. We use state-driven show/hide instead.
import { cn } from "@/lib/utils"
import { api } from "@/lib/api"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { RoleEditorSheet } from "@/components/settings/role-editor-sheet"
import type { RoleResponse, PermissionResponse } from "@/types/security"

// ── Types ────────────────────────────────────────────────────────────

interface MatrixData {
  roles: RoleResponse[]
  permissions: PermissionResponse[]
  /** roleId → Set<permissionCode> */
  rolePerms: Map<string, Set<string>>
}

/** One row in the matrix = one entity (or admin resource) */
interface EntityRow {
  /** Unique key for this row, e.g. "catalog:nomenclature" or "admin" */
  id: string
  /** Human-readable label */
  label: string
  /** Available actions with their permission codes */
  actions: { action: string; code: string }[]
}

/** A logical category grouping entity rows */
interface CategoryGroup {
  id: string
  label: string
  icon: React.ElementType
  entities: EntityRow[]
  defaultOpen: boolean
}

// ── Action config ────────────────────────────────────────────────────

const ACTION_SHORT: Record<string, string> = {
  create: "C",
  read: "R",
  update: "U",
  delete: "D",
  list: "L",
  post: "P",
  unpost: "X",
  manage: "M",
}

const ACTION_FULL: Record<string, string> = {
  create: "Создание",
  read: "Чтение",
  update: "Изменение",
  delete: "Удаление",
  list: "Список",
  post: "Проведение",
  unpost: "Отмена проведения",
  manage: "Управление",
}

const ACTION_STYLE: Record<string, string> = {
  create: "bg-emerald-500/15 text-emerald-700 border-emerald-500/25",
  read: "bg-blue-500/15 text-blue-700 border-blue-500/25",
  update: "bg-amber-500/15 text-amber-700 border-amber-500/25",
  delete: "bg-red-500/15 text-red-700 border-red-500/25",
  list: "bg-sky-500/15 text-sky-700 border-sky-500/25",
  post: "bg-violet-500/15 text-violet-700 border-violet-500/25",
  unpost: "bg-orange-500/15 text-orange-700 border-orange-500/25",
  manage: "bg-pink-500/15 text-pink-700 border-pink-500/25",
}

const INACTIVE_BADGE = "bg-muted/40 text-muted-foreground/40 border-transparent"

// ── Category definitions ─────────────────────────────────────────────

const CATEGORY_ORDER = ["catalog", "document", "register", "report", "admin"] as const

const CATEGORY_META: Record<string, { label: string; icon: React.ElementType; defaultOpen: boolean }> = {
  catalog:  { label: "Справочники",       icon: BookOpen,  defaultOpen: true },
  document: { label: "Документы",          icon: FileText,  defaultOpen: true },
  register: { label: "Регистры",           icon: Database,  defaultOpen: false },
  report:   { label: "Отчёты",             icon: BarChart3, defaultOpen: false },
  admin:    { label: "Администрирование",  icon: Settings,  defaultOpen: false },
}

// ── Helpers ──────────────────────────────────────────────────────────

/** Determine which category a permission resource belongs to using metadata store.
 *  resource is the raw value from the permission record (e.g. "nomenclature", "goods_receipt",
 *  "register_stock", "report_stock", "admin").
 */
function categorizeResource(
  resource: string,
  getEntity: (key: string) => import("@/types/metadata").EntityMeta | undefined,
): string {
  // 1. Try metadata store — it knows catalog vs document
  const meta = getEntity(resource)
  if (meta) return meta.type // "catalog" | "document"
  // 2. Prefix-based fallbacks for non-entity resources
  if (resource.startsWith("register")) return "register"
  if (resource.startsWith("report")) return "report"
  if (resource === "admin") return "admin"
  return "admin"
}

// Fallback labels for non-entity resources that are not in the metadata store
const RESOURCE_FALLBACK_LABELS: Record<string, string> = {
  admin: "Система",
  register_stock: "Регистр остатков",
  report_stock: "Отчёт по остаткам",
  report_documents: "Журнал документов",
}

// ── Component ────────────────────────────────────────────────────────

export function AccessMatrix() {
  const [data, setData] = useState<MatrixData | null>(null)
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState("")
  const [openCategories, setOpenCategories] = useState<Set<string>>(new Set(["catalog", "document"]))
  const [selectedRole, setSelectedRole] = useState<RoleResponse | null>(null)

  const getLabel = useMetadataStore((s) => s.getLabel)
  const getEntity = useMetadataStore((s) => s.getEntity)
  const metaLoaded = useMetadataStore((s) => s.loaded)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [rolesRes, permsRes] = await Promise.all([
        api.roles.list(),
        api.permissions.list(),
      ])

      const roles = rolesRes.items ?? []
      const permissions = permsRes.items ?? []

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

      setData({ roles, permissions, rolePerms })
    } catch {
      // silently fail
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  // ── Build category groups from permissions ──
  // Uses `resource` and `action` fields from PermissionResponse directly
  // (NOT parsing the `code` field, which uses dot notation e.g. "nomenclature.read").

  const categories: CategoryGroup[] = useMemo(() => {
    if (!data) return []

    // Group permissions by resource (e.g. "nomenclature", "goods_receipt", "admin")
    const entityMap = new Map<string, { resource: string; actions: Map<string, string> }>()

    for (const perm of data.permissions) {
      const resource = perm.resource  // e.g. "nomenclature", "goods_receipt", "admin"
      const action = perm.action      // e.g. "read", "create", "users"

      if (!entityMap.has(resource)) {
        entityMap.set(resource, { resource, actions: new Map() })
      }
      // Deduplicate by action — first code wins
      const entry = entityMap.get(resource)!
      if (!entry.actions.has(action)) {
        entry.actions.set(action, perm.code)
      }
    }

    // Categorize and build groups
    const groupMap = new Map<string, EntityRow[]>()

    for (const [resource, { actions }] of entityMap) {
      const cat = categorizeResource(resource, getEntity)
      if (!groupMap.has(cat)) groupMap.set(cat, [])

      // Resolve label: metadata store first, then fallback map, then raw resource key
      let label: string | undefined
      if (metaLoaded) {
        const metaLabel = getLabel(resource, "plural")
        if (metaLabel !== resource) label = metaLabel
      }
      if (!label) {
        label = RESOURCE_FALLBACK_LABELS[resource] ?? resource
      }

      // Sort actions in a logical order
      const actionOrder = ["list", "read", "create", "update", "delete", "post", "unpost", "manage", "users", "roles"]
      const sortedActions = [...actions.entries()]
        .map(([action, code]) => ({ action, code }))
        .sort((a, b) => {
          const ai = actionOrder.indexOf(a.action)
          const bi = actionOrder.indexOf(b.action)
          return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi)
        })

      groupMap.get(cat)!.push({ id: resource, label, actions: sortedActions })
    }

    // Sort entities alphabetically within each group
    for (const [, entities] of groupMap) {
      entities.sort((a, b) => a.label.localeCompare(b.label, "ru"))
    }

    // Build ordered categories
    return CATEGORY_ORDER
      .filter((cat) => groupMap.has(cat))
      .map((cat) => ({
        id: cat,
        label: CATEGORY_META[cat].label,
        icon: CATEGORY_META[cat].icon,
        defaultOpen: CATEGORY_META[cat].defaultOpen,
        entities: groupMap.get(cat)!,
      }))
  }, [data, metaLoaded, getLabel, getEntity])

  // ── Search filtering ──

  const filteredCategories = useMemo(() => {
    if (!search.trim()) return categories
    const q = search.toLowerCase().trim()
    return categories
      .map((cat) => ({
        ...cat,
        entities: cat.entities.filter((e) => e.label.toLowerCase().includes(q)),
      }))
      .filter((cat) => cat.entities.length > 0)
  }, [categories, search])

  // When searching, expand all matching groups; when cleared, restore defaults
  useEffect(() => {
    if (search.trim()) {
      setOpenCategories(new Set(filteredCategories.map((c) => c.id)))
    } else {
      setOpenCategories(new Set(
        CATEGORY_ORDER.filter((cat) => CATEGORY_META[cat]?.defaultOpen)
      ))
    }
  }, [search, filteredCategories])

  const toggleCategory = (id: string) => {
    setOpenCategories((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  // ── Loading / Error states ──

  if (loading) {
    return (
      <div className="flex items-center gap-2 py-8 justify-center text-xs text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        Загрузка матрицы доступа…
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

  const roles = data.roles

  return (
    <TooltipProvider delayDuration={150}>
      <div className="space-y-3">
        {/* Search */}
        <div className="relative">
          <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Поиск по сущности…"
            className="h-8 pl-8 text-xs"
          />
        </div>

        {/* Matrix */}
        <ScrollArea className="rounded-md border h-[calc(100vh-260px)] min-h-[400px] w-full">
          <table className="w-full text-xs border-collapse min-w-[max-content]">
            {/* Sticky header */}
            <thead className="sticky top-0 z-40 bg-background/95 backdrop-blur-sm shadow-sm">
              <tr className="bg-muted/60 backdrop-blur-sm border-b">
                <th className="px-3 py-2.5 text-left text-[11px] font-medium text-muted-foreground sticky left-0 z-30 bg-muted/60 backdrop-blur-sm min-w-[200px] border-r">
                  Сущность
                </th>
                {roles.map((role) => (
                  <th
                    key={role.id}
                    className="px-2 py-2 text-center min-w-[90px] cursor-pointer hover:bg-muted/80 transition-colors"
                    onClick={() => setSelectedRole(role)}
                  >
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <div className="flex flex-col items-center gap-0.5">
                          <Badge variant="outline" className="text-[9px] h-4 font-normal whitespace-nowrap">
                            {role.name}
                          </Badge>
                          <span className="text-[9px] text-muted-foreground/60 font-mono">{role.code}</span>
                        </div>
                      </TooltipTrigger>
                      <TooltipContent side="bottom" className="text-[10px]">
                        Нажмите для настройки роли
                      </TooltipContent>
                    </Tooltip>
                  </th>
                ))}
              </tr>
            </thead>

            <tbody>
              {filteredCategories.length === 0 && (
                <tr>
                  <td colSpan={roles.length + 1} className="px-3 py-8 text-center text-sm text-muted-foreground">
                    Ничего не найдено
                  </td>
                </tr>
              )}

              {filteredCategories.map((category) => {
                const Icon = category.icon
                const isOpen = openCategories.has(category.id)

                return (
                  <React.Fragment key={category.id}>
                    {/* Category header row */}
                    <tr
                      className="bg-muted/30 border-b cursor-pointer hover:bg-muted/50 transition-colors"
                      onClick={() => toggleCategory(category.id)}
                    >
                      <td
                        colSpan={roles.length + 1}
                        className="px-3 py-2"
                      >
                        <div className="flex items-center gap-2">
                          <ChevronRight
                            className={cn(
                              "h-3.5 w-3.5 text-muted-foreground transition-transform duration-200",
                              isOpen && "rotate-90"
                            )}
                          />
                          <Icon className="h-3.5 w-3.5 text-muted-foreground" />
                          <span className="text-xs font-semibold text-foreground">
                            {category.label}
                          </span>
                          <Badge variant="secondary" className="text-[9px] h-4 font-normal">
                            {category.entities.length}
                          </Badge>
                        </div>
                      </td>
                    </tr>

                    {/* Entity rows — conditionally rendered */}
                    {isOpen && category.entities.map((entity) => (
                      <tr
                        key={entity.id}
                        className="border-b last:border-b-0 hover:bg-muted/15 transition-colors"
                      >
                        {/* Sticky entity name */}
                        <td className="px-3 py-2 sticky left-0 z-10 bg-background border-r">
                          <span className="text-xs font-medium text-foreground pl-5">
                            {entity.label}
                          </span>
                        </td>

                        {/* Role cells — CRUD badges */}
                        {roles.map((role) => {
                          const rolePermSet = data.rolePerms.get(role.id)
                          return (
                            <td key={role.id} className="px-1.5 py-1.5 text-center">
                              <div className="flex items-center justify-center gap-0.5 flex-wrap">
                                {entity.actions.map(({ action, code }) => {
                                  const hasAccess = rolePermSet?.has(code) ?? false
                                  const short = ACTION_SHORT[action] ?? action[0]?.toUpperCase()
                                  const full = ACTION_FULL[action] ?? action
                                  return (
                                    <Tooltip key={action}>
                                      <TooltipTrigger asChild>
                                        <span
                                          className={cn(
                                            "inline-flex items-center justify-center h-5 min-w-5 px-1 rounded text-[9px] font-semibold border transition-colors",
                                            hasAccess
                                              ? ACTION_STYLE[action] ?? "bg-emerald-500/15 text-emerald-700 border-emerald-500/25"
                                              : INACTIVE_BADGE
                                          )}
                                        >
                                          {short}
                                        </span>
                                      </TooltipTrigger>
                                      <TooltipContent side="top" className="text-[10px]">
                                        <span className={hasAccess ? "text-emerald-600 font-medium" : "text-muted-foreground"}>
                                          {full}: {hasAccess ? "Разрешено" : "Запрещено"}
                                        </span>
                                      </TooltipContent>
                                    </Tooltip>
                                  )
                                })}
                              </div>
                            </td>
                          )
                        })}
                      </tr>
                    ))}
                  </React.Fragment>
                )
              })}
            </tbody>
          </table>
          <ScrollBar orientation="horizontal" />
        </ScrollArea>

        {/* Legend */}
        <div className="flex flex-wrap items-center gap-3 text-[10px] text-muted-foreground">
          {Object.entries(ACTION_SHORT).map(([action, short]) => (
            <span key={action} className="flex items-center gap-1">
              <span className={cn(
                "inline-flex items-center justify-center h-4 min-w-4 px-0.5 rounded text-[8px] font-semibold border",
                ACTION_STYLE[action]
              )}>
                {short}
              </span>
              {ACTION_FULL[action]}
            </span>
          ))}
        </div>

        {/* Role Editor Sheet */}
        <RoleEditorSheet
          role={selectedRole}
          permissions={data.permissions}
          rolePerms={selectedRole ? data.rolePerms.get(selectedRole.id) ?? new Set() : new Set()}
          onClose={(saved) => {
            setSelectedRole(null)
            if (saved) load()
          }}
        />
      </div>
    </TooltipProvider>
  )
}
