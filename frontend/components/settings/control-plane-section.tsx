"use client"

import { useEffect, useState, useCallback, useRef } from "react"
import {
  Cloud,
  Database,
  Users,
  AlertTriangle,
  CheckCircle2,
  ArrowUpCircle,
  RefreshCw,
  Loader2,
  Pause,
  Play,
  Server,
  Wrench,
  Undo2,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api } from "@/lib/api"
import type { TenantSummary, TenantListResponse } from "@/lib/api"
import { cn } from "@/lib/utils"

const POLL_INTERVAL_MS = 3000

// ── Stat Card ───────────────────────────────────────────────────────────

function StatCard({
  label,
  value,
  icon: Icon,
  variant = "default",
}: {
  label: string
  value: string | number
  icon: React.ElementType
  variant?: "default" | "success" | "warning" | "danger"
}) {
  const variantClasses = {
    default: "border-border bg-card",
    success: "border-green-500/30 bg-green-500/5",
    warning: "border-amber-500/30 bg-amber-500/5",
    danger: "border-red-500/30 bg-red-500/5",
  }
  const iconClasses = {
    default: "text-muted-foreground",
    success: "text-green-500",
    warning: "text-amber-500",
    danger: "text-red-500",
  }

  return (
    <div className={cn("rounded-lg border p-4", variantClasses[variant])}>
      <div className="flex items-center justify-between">
        <p className="text-xs font-medium text-muted-foreground">{label}</p>
        <Icon className={cn("h-4 w-4", iconClasses[variant])} />
      </div>
      <p className="mt-2 text-2xl font-bold tabular-nums">{value}</p>
    </div>
  )
}

// ── Version Group Badge ─────────────────────────────────────────────────

function VersionGroupBadge({ group }: { group: string }) {
  if (!group) {
    return (
      <Badge variant="outline" className="text-[10px] text-muted-foreground">
        по умолчанию
      </Badge>
    )
  }
  return (
    <Badge variant="secondary" className="text-[10px] font-mono">
      {group}
    </Badge>
  )
}

// ── Schema Badge ────────────────────────────────────────────────────────

function SchemaBadge({
  version,
  upToDate,
  isUpdating,
}: {
  version: number
  upToDate: boolean
  isUpdating: boolean
}) {
  if (isUpdating) {
    return (
      <div className="flex items-center gap-1.5">
        <span className="text-xs font-mono tabular-nums">{version}</span>
        <Loader2 className="h-3.5 w-3.5 animate-spin text-blue-500" />
      </div>
    )
  }
  return (
    <div className="flex items-center gap-1.5">
      <span className="text-xs font-mono tabular-nums">{version}</span>
      {upToDate ? (
        <CheckCircle2 className="h-3.5 w-3.5 text-green-500" />
      ) : (
        <AlertTriangle className="h-3.5 w-3.5 text-amber-500" />
      )}
    </div>
  )
}

// ── Status Badge ────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    active: "bg-green-500/10 text-green-600 border-green-500/30",
    suspended: "bg-red-500/10 text-red-600 border-red-500/30",
    pending: "bg-amber-500/10 text-amber-600 border-amber-500/30",
    updating: "bg-blue-500/10 text-blue-600 border-blue-500/30",
    migration_failed: "bg-red-500/10 text-red-600 border-red-500/30",
  }
  const icons: Record<string, React.ElementType> = {
    active: Play,
    suspended: Pause,
    updating: Wrench,
    migration_failed: AlertTriangle,
  }
  const labels: Record<string, string> = {
    active: "Активен",
    suspended: "Приостановлен",
    updating: "Обновляется...",
    migration_failed: "Сбой миграции",
  }
  const Icon = icons[status]

  return (
    <Badge variant="outline" className={cn("gap-1 text-[10px]", colors[status] ?? "")}>
      {Icon && <Icon className={cn("h-3 w-3", status === "updating" && "animate-spin")} />}
      {labels[status] ?? status}
    </Badge>
  )
}

// ── Promote Dialog ──────────────────────────────────────────────────────

function PromoteDialog({
  tenant,
  open,
  onOpenChange,
  onPromoted,
}: {
  tenant: TenantSummary | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onPromoted: () => void
}) {
  const [versionGroup, setVersionGroup] = useState("")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (tenant) setVersionGroup(tenant.versionGroup || "")
  }, [tenant])

  const handlePromote = async () => {
    if (!tenant || !versionGroup.trim()) return
    setSaving(true)
    setError(null)
    try {
      await api.admin.tenants.promote(tenant.id, versionGroup.trim())
      onOpenChange(false)
      onPromoted()
    } catch (err) {
      setError(err instanceof Error ? err.message : "Не удалось назначить группу версий")
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ArrowUpCircle className="h-5 w-5 text-primary" />
            Назначить группу версий
          </DialogTitle>
          <DialogDescription>
            Тенант <strong>{tenant?.slug}</strong> будет переключён на указанную группу версий.
            Запросы будут маршрутизироваться на сервер с соответствующей VERSION_GROUP.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-2">
          <div>
            <Label htmlFor="version-group" className="text-xs text-muted-foreground">
              Группа версий
            </Label>
            <Input
              id="version-group"
              value={versionGroup}
              onChange={(e) => setVersionGroup(e.target.value)}
              placeholder="v1.3.0"
              className="mt-1 font-mono text-sm"
            />
            <p className="mt-1 text-[11px] text-muted-foreground">
              Должна совпадать с VERSION_GROUP серверного инстанса
            </p>
          </div>
          {tenant?.versionGroup && (
            <p className="text-xs text-muted-foreground">
              Текущая группа: <code className="rounded bg-muted px-1 py-0.5 font-mono">{tenant.versionGroup}</code>
            </p>
          )}
          {error && (
            <p className="text-xs text-destructive">{error}</p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            Отменить
          </Button>
          <Button onClick={handlePromote} disabled={saving || !versionGroup.trim()}>
            {saving ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
            Назначить
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ── Main Component ──────────────────────────────────────────────────────

export function ControlPlaneSection() {
  const [data, setData] = useState<TenantListResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [promoteTarget, setPromoteTarget] = useState<TenantSummary | null>(null)
  const [promoteOpen, setPromoteOpen] = useState(false)
  const [updatingIds, setUpdatingIds] = useState<Set<string>>(new Set())
  const [rollbackTarget, setRollbackTarget] = useState<TenantSummary | null>(null)
  const [rollbackOpen, setRollbackOpen] = useState(false)
  const [rollbackLoading, setRollbackLoading] = useState(false)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Silent refresh (no loading spinner) for polling
  const refreshData = useCallback(async () => {
    try {
      const res = await api.admin.tenants.list()
      setData(res)

      // Track which tenants are updating
      const updating = new Set(
        res.items.filter((t) => t.status === "updating" || t.status === "migration_failed").map((t) => t.id)
      )
      setUpdatingIds(updating)

      return updating.size
    } catch {
      return 0
    }
  }, [])

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.admin.tenants.list()
      setData(res)

      const updating = new Set(
        res.items.filter((t) => t.status === "updating").map((t) => t.id)
      )
      setUpdatingIds(updating)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Не удалось загрузить данные")
    } finally {
      setLoading(false)
    }
  }, [])

  // Start/stop polling based on whether any tenant is updating
  useEffect(() => {
    if (updatingIds.size > 0 && !pollRef.current) {
      pollRef.current = setInterval(async () => {
        const stillUpdating = await refreshData()
        if (stillUpdating === 0 && pollRef.current) {
          clearInterval(pollRef.current)
          pollRef.current = null
        }
      }, POLL_INTERVAL_MS)
    }

    return () => {
      if (pollRef.current) {
        clearInterval(pollRef.current)
        pollRef.current = null
      }
    }
  }, [updatingIds.size, refreshData])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  const handlePromote = (tenant: TenantSummary) => {
    setPromoteTarget(tenant)
    setPromoteOpen(true)
  }

  const handleTriggerUpdate = async (tenant: TenantSummary) => {
    try {
      await api.admin.tenants.triggerUpdate(tenant.id)
      setUpdatingIds((prev) => new Set([...prev, tenant.id]))
      await refreshData()
    } catch (err) {
      await refreshData()
      alert(err instanceof Error ? err.message : "Не удалось запустить обновление")
    }
  }

  const handleRetryUpdate = async (tenant: TenantSummary) => {
    try {
      await api.admin.tenants.retryUpdate(tenant.id)
      setUpdatingIds((prev) => new Set([...prev, tenant.id]))
      await refreshData()
    } catch (err) {
      await refreshData()
      alert(err instanceof Error ? err.message : "Не удалось запустить миграцию")
    }
  }

  const handleRollbackUpdate = async () => {
    if (!rollbackTarget) return
    setRollbackLoading(true)
    try {
      await api.admin.tenants.rollbackUpdate(rollbackTarget.id)
      setRollbackOpen(false)
      setRollbackTarget(null)
      await refreshData()
    } catch (err) {
      alert(err instanceof Error ? err.message : "Не удалось выполнить откат миграции")
    } finally {
      setRollbackLoading(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-6">
        <div className="flex items-center gap-2 text-destructive">
          <AlertTriangle className="h-5 w-5" />
          <span className="text-sm font-medium">Ошибка загрузки данных</span>
        </div>
        <p className="mt-2 text-xs text-muted-foreground">{error}</p>
        <Button variant="outline" size="sm" className="mt-3" onClick={fetchData}>
          <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          Повторить
        </Button>
      </div>
    )
  }

  if (!data) return null

  const versionGroupEntries = Object.entries(data.versionGroups).sort(([a], [b]) => a.localeCompare(b))
  const hasUpdating = updatingIds.size > 0

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10">
            <Cloud className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h3 className="text-base font-semibold text-foreground">
              Панель управления тенантами
            </h3>
            <p className="text-xs text-muted-foreground">
              Управление группами версий и мониторинг схем БД
              {hasUpdating && (
                <span className="ml-2 inline-flex items-center gap-1 text-blue-500">
                  <Loader2 className="h-3 w-3 animate-spin" />
                  Обновление...
                </span>
              )}
            </p>
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={fetchData}>
          <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          Обновить
        </Button>
      </div>

      <Separator />

      {/* Stats */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatCard
          label="Всего тенантов"
          value={data.total}
          icon={Users}
        />
        <StatCard
          label="Активных"
          value={data.activeCount}
          icon={Play}
          variant="success"
        />
        <StatCard
          label="Устаревших схем"
          value={data.outdatedCount}
          icon={AlertTriangle}
          variant={data.outdatedCount > 0 ? "warning" : "success"}
        />
        <StatCard
          label="Ожидаемая схема"
          value={data.expectedSchema}
          icon={Database}
        />
      </div>

      {/* Version groups summary */}
      {versionGroupEntries.length > 0 && (
        <>
          <Separator />
          <div>
            <h4 className="mb-3 text-sm font-semibold text-foreground">
              Группы версий
            </h4>
            <div className="flex flex-wrap gap-2">
              {versionGroupEntries.map(([group, count]) => (
                <div
                  key={group}
                  className="flex items-center gap-2 rounded-lg border bg-card/50 px-3 py-2"
                >
                  <Server className="h-3.5 w-3.5 text-muted-foreground" />
                  <span className="text-xs font-mono font-medium">{group}</span>
                  <Badge variant="secondary" className="text-[10px]">
                    {count}
                  </Badge>
                </div>
              ))}
            </div>
          </div>
        </>
      )}

      <Separator />

      {/* Tenants table */}
      <div>
        <h4 className="mb-3 text-sm font-semibold text-foreground">
          Список тенантов
        </h4>
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[180px]">Slug</TableHead>
                <TableHead>Название</TableHead>
                <TableHead className="w-[110px]">Статус</TableHead>
                <TableHead className="w-[80px]">Схема</TableHead>
                <TableHead className="w-[120px]">Группа версий</TableHead>
                <TableHead className="w-[100px]">БД</TableHead>
                <TableHead className="w-[160px] text-right">Действия</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.items.map((tenant) => {
                const isUpdating = tenant.status === "updating"
                const isMigrationFailed = tenant.status === "migration_failed"
                const canUpdate = tenant.status === "active" && !tenant.schemaUpToDate

                return (
                  <TableRow key={tenant.id} className={cn(isUpdating && "bg-blue-500/[0.02]")}>
                    <TableCell className="font-mono text-xs font-medium">
                      {tenant.slug}
                    </TableCell>
                    <TableCell className="text-sm">
                      {tenant.displayName}
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={tenant.status} />
                    </TableCell>
                    <TableCell>
                      <SchemaBadge
                        version={tenant.schemaVersion}
                        upToDate={tenant.schemaUpToDate}
                        isUpdating={isUpdating}
                      />
                    </TableCell>
                    <TableCell>
                      <VersionGroupBadge group={tenant.versionGroup} />
                    </TableCell>
                    <TableCell className="font-mono text-[11px] text-muted-foreground">
                      {tenant.dbName}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        {canUpdate && (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 px-2 text-xs text-amber-600 hover:text-amber-700 hover:bg-amber-500/10"
                            onClick={() => handleTriggerUpdate(tenant)}
                          >
                            <Database className="mr-1 h-3.5 w-3.5" />
                            Обновить БД
                          </Button>
                        )}
                        {isUpdating && (
                          <span className="flex items-center gap-1 text-[11px] text-blue-500">
                            <Loader2 className="h-3 w-3 animate-spin" />
                            Миграция...
                          </span>
                        )}
                        {isMigrationFailed && (
                          <>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-7 px-2 text-xs text-amber-600 hover:text-amber-700 hover:bg-amber-500/10"
                              onClick={() => handleRetryUpdate(tenant)}
                            >
                              <RefreshCw className="mr-1 h-3.5 w-3.5" />
                              Повторить
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-7 px-2 text-xs text-destructive hover:text-destructive hover:bg-destructive/10"
                              onClick={() => { setRollbackTarget(tenant); setRollbackOpen(true) }}
                            >
                              <Undo2 className="mr-1 h-3.5 w-3.5" />
                              Откатить
                            </Button>
                          </>
                        )}
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2 text-xs"
                          onClick={() => handlePromote(tenant)}
                          disabled={isUpdating || isMigrationFailed}
                        >
                          <ArrowUpCircle className="mr-1 h-3.5 w-3.5" />
                          Версия
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
              {data.items.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7} className="h-24 text-center text-muted-foreground">
                    Нет тенантов
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      </div>

      {/* Spacer */}
      <div className="h-8" />

      {/* Promote Dialog */}
      <PromoteDialog
        tenant={promoteTarget}
        open={promoteOpen}
        onOpenChange={setPromoteOpen}
        onPromoted={fetchData}
      />

      {/* Rollback Confirm Dialog */}
      <Dialog open={rollbackOpen} onOpenChange={setRollbackOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Undo2 className="h-5 w-5 text-destructive" />
              Подтвердите откат миграции
            </DialogTitle>
            <DialogDescription>
              Тенант <strong>{rollbackTarget?.slug}</strong> будет возвращён к состоянию схемы БД
              до начала обновления. Все изменения, применённые в рамках этого обновления, будут отменены.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRollbackOpen(false)} disabled={rollbackLoading}>
              Отменить
            </Button>
            <Button variant="destructive" onClick={handleRollbackUpdate} disabled={rollbackLoading}>
              {rollbackLoading ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
              Откатить
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
