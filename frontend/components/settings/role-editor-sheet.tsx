"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import {
  Shield,
  BookOpen,
  FileText,
  Database,
  BarChart3,
  Settings,
  ChevronRight,
  Loader2,
  Save,
  Trash2,
  Users,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { cn } from "@/lib/utils"
import { api, ApiError } from "@/lib/api"
import { toast } from "sonner"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type { RoleResponse, PermissionResponse } from "@/types/security"

// ── Types ────────────────────────────────────────────────────────────

interface RoleEditorSheetProps {
  /** null = closed, role object = edit, "new" = create */
  role: RoleResponse | "new" | null
  permissions: PermissionResponse[]
  rolePerms: Set<string>
  onClose: (saved: boolean) => void
}

interface EntityActions {
  id: string
  label: string
  actions: { action: string; permId: string; code: string; has: boolean }[]
  allGranted: boolean
  noneGranted: boolean
}

interface CategoryBlock {
  id: string
  label: string
  icon: React.ElementType
  entities: EntityActions[]
  totalGranted: number
  totalActions: number
}

// ── Constants ────────────────────────────────────────────────────────

const ACTION_ORDER = ["list", "read", "create", "update", "delete", "post", "unpost", "manage"]

const ACTION_LABELS: Record<string, string> = {
  list: "Список",
  read: "Чтение",
  create: "Создание",
  update: "Изменение",
  delete: "Удаление",
  post: "Проведение",
  unpost: "Отмена пров.",
  manage: "Управление",
}

const ACTION_STYLE: Record<string, string> = {
  create: "text-emerald-600",
  read: "text-blue-600",
  update: "text-amber-600",
  delete: "text-red-600",
  list: "text-sky-600",
  post: "text-violet-600",
  unpost: "text-orange-600",
  manage: "text-pink-600",
}

const CATEGORY_ORDER = ["catalog", "document", "register", "report", "admin"] as const

const CATEGORY_META: Record<string, { label: string; icon: React.ElementType }> = {
  catalog:  { label: "Справочники",       icon: BookOpen },
  document: { label: "Документы",          icon: FileText },
  register: { label: "Регистры",           icon: Database },
  report:   { label: "Отчёты",             icon: BarChart3 },
  admin:    { label: "Администрирование",  icon: Settings },
}

const FALLBACK_LABELS: Record<string, string> = {
  admin: "Система",
  "admin:users": "Пользователи (адм.)",
  "admin:roles": "Роли (адм.)",
  "report:stock": "Отчёт по остаткам",
  "report:documents": "Журнал документов",
  "register:stock": "Регистр остатков",
}

// ── Helpers ──────────────────────────────────────────────────────────

function categorize(resource: string): string {
  if (resource.startsWith("catalog")) return "catalog"
  if (resource.startsWith("document")) return "document"
  if (resource.startsWith("register")) return "register"
  if (resource.startsWith("report")) return "report"
  return "admin"
}

function parsePermCode(code: string) {
  const parts = code.split(":")
  if (parts.length >= 3) return { type: parts[0], entity: parts[1], action: parts[2] }
  if (parts.length === 2) return { type: parts[0], entity: "", action: parts[1] }
  return { type: "other", entity: "", action: parts[0] }
}

// ── Component ────────────────────────────────────────────────────────

export function RoleEditorSheet({ role, permissions, rolePerms, onClose }: RoleEditorSheetProps) {
  const getLabel = useMetadataStore((s) => s.getLabel)
  const metaLoaded = useMetadataStore((s) => s.loaded)

  const isNew = role === "new"
  const isOpen = role !== null
  const roleData = typeof role === "object" && role !== null ? role : null
  const isSystem = roleData?.isSystem ?? false
  const readOnly = isSystem

  // ── Form state ──
  const [code, setCode] = useState("")
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [grantedPerms, setGrantedPerms] = useState<Set<string>>(new Set())
  const [saving, setSaving] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [userCount, setUserCount] = useState(0)

  // ── Initialize form when role changes ──
  useEffect(() => {
    if (isNew) {
      setCode("")
      setName("")
      setDescription("")
      setGrantedPerms(new Set())
      setUserCount(0)
    } else if (roleData) {
      setCode(roleData.code)
      setName(roleData.name)
      setDescription(roleData.description ?? "")
      setGrantedPerms(new Set(rolePerms))
      setUserCount(roleData.userCount ?? 0)
    }
  }, [isNew, roleData, rolePerms])

  // ── Permission ID lookup (code → id) ──
  const permIdByCode = useMemo(() => {
    const map = new Map<string, string>()
    for (const p of permissions) map.set(p.code, p.id)
    return map
  }, [permissions])

  // ── Toggle permission ──
  const togglePerm = useCallback((code: string) => {
    if (readOnly) return
    setGrantedPerms((prev) => {
      const next = new Set(prev)
      if (next.has(code)) next.delete(code)
      else next.add(code)
      return next
    })
  }, [readOnly])

  // ── Toggle all permissions in an entity ──
  const toggleEntityAll = useCallback((codes: string[], allGranted: boolean) => {
    if (readOnly) return
    setGrantedPerms((prev) => {
      const next = new Set(prev)
      for (const c of codes) {
        if (allGranted) next.delete(c)
        else next.add(c)
      }
      return next
    })
  }, [readOnly])

  // ── Build categories ──
  const categories: CategoryBlock[] = useMemo(() => {
    if (!permissions.length) return []

    const entityMap = new Map<string, { entityKey: string; actions: Map<string, { permId: string; code: string }> }>()
    for (const perm of permissions) {
      const parsed = parsePermCode(perm.code)
      const groupKey = parsed.entity ? `${parsed.type}:${parsed.entity}` : parsed.type
      if (!entityMap.has(groupKey)) {
        entityMap.set(groupKey, { entityKey: parsed.entity || parsed.type, actions: new Map() })
      }
      const entry = entityMap.get(groupKey)!
      if (!entry.actions.has(parsed.action)) {
        entry.actions.set(parsed.action, { permId: perm.id, code: perm.code })
      }
    }

    const groupMap = new Map<string, EntityActions[]>()

    for (const [groupKey, { entityKey, actions }] of entityMap) {
      const cat = categorize(groupKey)
      if (!groupMap.has(cat)) groupMap.set(cat, [])

      let label = groupKey
      if (metaLoaded && entityKey) {
        const metaLabel = getLabel(entityKey, "plural")
        if (metaLabel !== entityKey) label = metaLabel
      }
      if (label === groupKey) label = FALLBACK_LABELS[groupKey] ?? groupKey

      const sortedActions = [...actions.entries()]
        .map(([action, { permId, code }]) => ({ action, permId, code }))
        .sort((a, b) => (ACTION_ORDER.indexOf(a.action) ?? 99) - (ACTION_ORDER.indexOf(b.action) ?? 99))

      const mapped = sortedActions.map((a) => ({
        ...a,
        has: grantedPerms.has(a.code),
      }))

      groupMap.get(cat)!.push({
        id: groupKey,
        label,
        actions: mapped,
        allGranted: mapped.every((a) => a.has),
        noneGranted: mapped.every((a) => !a.has),
      })
    }

    for (const [, entities] of groupMap) {
      entities.sort((a, b) => a.label.localeCompare(b.label, "ru"))
    }

    return CATEGORY_ORDER
      .filter((cat) => groupMap.has(cat))
      .map((cat) => {
        const entities = groupMap.get(cat)!
        const totalActions = entities.reduce((s, e) => s + e.actions.length, 0)
        const totalGranted = entities.reduce((s, e) => s + e.actions.filter((a) => a.has).length, 0)
        return {
          id: cat,
          label: CATEGORY_META[cat].label,
          icon: CATEGORY_META[cat].icon,
          entities,
          totalGranted,
          totalActions,
        }
      })
  }, [permissions, grantedPerms, metaLoaded, getLabel])

  // ── Dirty check ──
  const isDirty = useMemo(() => {
    if (isNew) return code.trim() !== "" || name.trim() !== ""
    if (!roleData) return false
    if (name !== roleData.name) return true
    if (description !== (roleData.description ?? "")) return true
    if (grantedPerms.size !== rolePerms.size) return true
    for (const p of grantedPerms) {
      if (!rolePerms.has(p)) return true
    }
    return false
  }, [isNew, roleData, code, name, description, grantedPerms, rolePerms])

  // ── Save handler ──
  const handleSave = async () => {
    if (!name.trim()) {
      toast.error("Название обязательно")
      return
    }
    setSaving(true)
    try {
      if (isNew) {
        if (!code.trim()) {
          toast.error("Код обязателен")
          setSaving(false)
          return
        }
        const created = await api.roles.create({
          code: code.trim(),
          name: name.trim(),
          description: description.trim() || undefined,
        })
        // Set permissions if any selected
        if (grantedPerms.size > 0) {
          const permIds = [...grantedPerms]
            .map((c) => permIdByCode.get(c))
            .filter(Boolean) as string[]
          if (permIds.length > 0) {
            await api.roles.setPermissions(created.id, permIds)
          }
        }
        toast.success("Роль создана")
      } else if (roleData) {
        // Update role metadata
        if (name !== roleData.name || description !== (roleData.description ?? "")) {
          await api.roles.update(roleData.id, {
            name: name.trim(),
            description: description.trim() || undefined,
          })
        }
        // Update permissions if changed
        const permsChanged = grantedPerms.size !== rolePerms.size ||
          [...grantedPerms].some((p) => !rolePerms.has(p))
        if (permsChanged && !readOnly) {
          const permIds = [...grantedPerms]
            .map((c) => permIdByCode.get(c))
            .filter(Boolean) as string[]
          await api.roles.setPermissions(roleData.id, permIds)
        }
        toast.success("Роль обновлена")
      }
      onClose(true)
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "Ошибка сохранения")
    } finally {
      setSaving(false)
    }
  }

  // ── Delete handler ──
  const handleDelete = async () => {
    if (!roleData) return
    setDeleting(true)
    try {
      const res = await api.roles.delete(roleData.id)
      toast.success(
        res.affectedUsers > 0
          ? `Роль удалена. Затронуто пользователей: ${res.affectedUsers}`
          : "Роль удалена"
      )
      setDeleteOpen(false)
      onClose(true)
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "Ошибка удаления")
    } finally {
      setDeleting(false)
    }
  }

  const totalPerms = grantedPerms.size

  return (
    <>
      <Sheet open={isOpen} onOpenChange={(o) => !o && onClose(false)}>
        <SheetContent className="w-full sm:max-w-xl p-0 flex flex-col">
          <SheetHeader className="px-6 py-4 border-b shrink-0">
            <div className="flex items-center gap-2">
              <Shield className="h-4 w-4 text-primary" />
              <SheetTitle className="text-base">
                {isNew ? "Новая роль" : roleData?.name ?? ""}
              </SheetTitle>
              {isSystem && (
                <Badge variant="secondary" className="text-[10px]">Системная</Badge>
              )}
            </div>
            <div className="flex items-center gap-3 mt-1">
              {userCount > 0 && (
                <div className="flex items-center gap-1 text-xs text-muted-foreground">
                  <Users className="h-3 w-3" />
                  {userCount} польз.
                </div>
              )}
              <Badge variant="outline" className="text-[10px] ml-auto shrink-0">
                {totalPerms} разрешений
              </Badge>
            </div>
          </SheetHeader>

          <ScrollArea className="flex-1">
            <div className="px-4 py-3 space-y-4">
              {/* ── Role fields ── */}
              <div className="space-y-3">
                <div>
                  <Label className="text-xs text-muted-foreground">Код *</Label>
                  <Input
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                    placeholder="my_role"
                    disabled={!isNew}
                    className="h-8 text-sm font-mono"
                  />
                  {!isNew && (
                    <p className="text-[10px] text-muted-foreground mt-0.5">
                      Код нельзя изменить после создания
                    </p>
                  )}
                </div>
                <div>
                  <Label className="text-xs text-muted-foreground">Название *</Label>
                  <Input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="Название роли"
                    className="h-8 text-sm"
                  />
                </div>
                <div>
                  <Label className="text-xs text-muted-foreground">Описание</Label>
                  <Textarea
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    placeholder="Необязательное описание"
                    className="text-sm min-h-[60px] resize-none"
                  />
                </div>
              </div>

              <div className="border-t" />

              {/* ── Permissions matrix ── */}
              <div>
                <h3 className="text-xs font-semibold text-foreground mb-2">Разрешения</h3>

                {categories.length === 0 ? (
                  <div className="py-8 text-center text-sm text-muted-foreground">
                    Нет доступных разрешений
                  </div>
                ) : (
                  <TooltipProvider delayDuration={200}>
                    <div className="space-y-1">
                      {categories.map((cat) => {
                        const CatIcon = cat.icon
                        return (
                          <Collapsible key={cat.id} defaultOpen={cat.totalGranted > 0 || isNew}>
                            <CollapsibleTrigger className="flex items-center gap-2 w-full px-2 py-2 rounded-md hover:bg-muted/50 transition-colors group">
                              <ChevronRight className="h-3.5 w-3.5 text-muted-foreground transition-transform duration-200 group-data-[state=open]:rotate-90" />
                              <CatIcon className="h-3.5 w-3.5 text-muted-foreground" />
                              <span className="text-xs font-semibold text-foreground">{cat.label}</span>
                              <Badge variant="secondary" className="text-[9px] h-4 font-normal ml-auto">
                                {cat.totalGranted}/{cat.totalActions}
                              </Badge>
                            </CollapsibleTrigger>
                            <CollapsibleContent>
                              <div className="ml-3 border-l pl-3 space-y-0.5 mb-2">
                                {cat.entities.map((entity) => (
                                  <div
                                    key={entity.id}
                                    className="px-2 py-2 rounded-md hover:bg-muted/30 transition-colors"
                                  >
                                    <div className="flex items-center justify-between mb-1.5">
                                      <button
                                        type="button"
                                        className={cn(
                                          "text-xs font-medium",
                                          readOnly ? "text-foreground cursor-default" : "text-foreground hover:text-primary cursor-pointer"
                                        )}
                                        onClick={() => {
                                          if (!readOnly) {
                                            toggleEntityAll(
                                              entity.actions.map((a) => a.code),
                                              entity.allGranted
                                            )
                                          }
                                        }}
                                        disabled={readOnly}
                                      >
                                        {entity.label}
                                      </button>
                                      <span className="text-[9px] text-muted-foreground tabular-nums">
                                        {entity.actions.filter((a) => a.has).length}/{entity.actions.length}
                                      </span>
                                    </div>
                                    <div className="flex items-center flex-wrap gap-x-3 gap-y-1">
                                      {entity.actions.map(({ action, code: permCode, has }) => (
                                        <Tooltip key={action}>
                                          <TooltipTrigger asChild>
                                            <label className={cn(
                                              "flex items-center gap-1",
                                              readOnly ? "cursor-default" : "cursor-pointer"
                                            )}>
                                              <Checkbox
                                                checked={has}
                                                disabled={readOnly}
                                                onCheckedChange={() => togglePerm(permCode)}
                                                className="h-3.5 w-3.5"
                                              />
                                              <span className={cn(
                                                "text-[10px] font-medium",
                                                has
                                                  ? ACTION_STYLE[action] ?? "text-foreground"
                                                  : "text-muted-foreground/50"
                                              )}>
                                                {ACTION_LABELS[action] ?? action}
                                              </span>
                                            </label>
                                          </TooltipTrigger>
                                          <TooltipContent side="top" className="text-[10px]">
                                            {ACTION_LABELS[action] ?? action}: {has ? "Разрешено" : "Запрещено"}
                                          </TooltipContent>
                                        </Tooltip>
                                      ))}
                                    </div>
                                  </div>
                                ))}
                              </div>
                            </CollapsibleContent>
                          </Collapsible>
                        )
                      })}
                    </div>
                  </TooltipProvider>
                )}
              </div>

              {readOnly && (
                <>
                  <div className="border-t mx-2" />
                  <div className="px-2 pb-2">
                    <p className="text-[11px] text-muted-foreground">
                      Системные роли нельзя изменить или удалить.
                    </p>
                  </div>
                </>
              )}
            </div>
          </ScrollArea>

          {/* ── Footer actions ── */}
          <div className="border-t px-4 py-3 flex items-center gap-2 shrink-0">
            {!isNew && !isSystem && roleData && (
              <Button
                variant="destructive"
                size="sm"
                className="text-xs"
                onClick={() => setDeleteOpen(true)}
              >
                <Trash2 className="mr-1.5 h-3.5 w-3.5" />
                Удалить
              </Button>
            )}
            <div className="flex-1" />
            <Button
              variant="outline"
              size="sm"
              className="text-xs"
              onClick={() => onClose(false)}
            >
              Отмена
            </Button>
            {!readOnly && (
              <Button
                size="sm"
                className="text-xs"
                onClick={handleSave}
                disabled={saving || (!isNew && !isDirty)}
              >
                {saving ? (
                  <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Save className="mr-1.5 h-3.5 w-3.5" />
                )}
                {isNew ? "Создать" : "Сохранить"}
              </Button>
            )}
          </div>
        </SheetContent>
      </Sheet>

      {/* ── Delete confirmation ── */}
      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Удалить роль «{roleData?.name}»?</AlertDialogTitle>
            <AlertDialogDescription>
              {userCount > 0
                ? `У этой роли ${userCount} пользователь(ей). Они потеряют все привязанные разрешения. Их сессии будут завершены.`
                : "Роль будет удалена безвозвратно. Это действие нельзя отменить."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>Отмена</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              disabled={deleting}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {deleting && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              Удалить
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
