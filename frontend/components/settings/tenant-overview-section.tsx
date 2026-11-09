"use client"

import { useCallback, useEffect, useState } from "react"
import {
  AlertTriangle,
  CheckCircle2,
  Database,
  Loader2,
  Pause,
  Play,
  RefreshCw,
  Users,
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
import { api } from "@/lib/api"
import type { TenantListResponse } from "@/lib/api"
import { cn } from "@/lib/utils"

function StatCard({
  label,
  value,
  icon: Icon,
  variant = "default",
}: {
  label: string
  value: string | number
  icon: React.ElementType
  variant?: "default" | "success" | "warning"
}) {
  const variantClasses = {
    default: "border-border bg-card",
    success: "border-green-500/30 bg-green-500/5",
    warning: "border-amber-500/30 bg-amber-500/5",
  }
  const iconClasses = {
    default: "text-muted-foreground",
    success: "text-green-500",
    warning: "text-amber-500",
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

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    active: "border-green-500/30 bg-green-500/10 text-green-600",
    suspended: "border-red-500/30 bg-red-500/10 text-red-600",
    provisioning: "border-amber-500/30 bg-amber-500/10 text-amber-600",
    provision_failed: "border-red-500/30 bg-red-500/10 text-red-600",
    deleted: "text-muted-foreground",
  }
  const labels: Record<string, string> = {
    active: "Активен",
    suspended: "Приостановлен",
    provisioning: "Создаётся",
    provision_failed: "Ошибка создания",
    deleted: "Удалён",
  }
  const Icon = status === "active" ? Play : status === "suspended" ? Pause : undefined

  return (
    <Badge variant="outline" className={cn("gap-1 text-[10px]", colors[status])}>
      {Icon ? <Icon className="h-3 w-3" /> : null}
      {labels[status] ?? status}
    </Badge>
  )
}

function SchemaBadge({ version, upToDate }: { version: number; upToDate: boolean }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="font-mono text-xs tabular-nums">{version}</span>
      {upToDate ? (
        <CheckCircle2 className="h-3.5 w-3.5 text-green-500" />
      ) : (
        <AlertTriangle className="h-3.5 w-3.5 text-amber-500" />
      )}
    </div>
  )
}

export function TenantOverviewSection() {
  const [data, setData] = useState<TenantListResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setData(await api.admin.tenants.list())
    } catch (fetchError) {
      setError(fetchError instanceof Error ? fetchError.message : "Не удалось загрузить данные")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchData()
  }, [fetchData])

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
        <Button variant="outline" size="sm" className="mt-3" onClick={() => void fetchData()}>
          <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          Повторить
        </Button>
      </div>
    )
  }

  if (!data) return null

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10">
            <Database className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h3 className="text-base font-semibold text-foreground">Состояние тенантов</h3>
            <p className="text-xs text-muted-foreground">Версии схем доступны только для просмотра</p>
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={() => void fetchData()}>
          <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          Обновить
        </Button>
      </div>

      <Separator />

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatCard label="Всего тенантов" value={data.total} icon={Users} />
        <StatCard label="Активных" value={data.activeCount} icon={Play} variant="success" />
        <StatCard
          label="Устаревших схем"
          value={data.outdatedCount}
          icon={AlertTriangle}
          variant={data.outdatedCount > 0 ? "warning" : "success"}
        />
        <StatCard label="Ожидаемая схема" value={data.expectedSchema} icon={Database} />
      </div>

      <Separator />

      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[180px]">Slug</TableHead>
              <TableHead>Название</TableHead>
              <TableHead className="w-[130px]">Статус</TableHead>
              <TableHead className="w-[90px]">Схема</TableHead>
              <TableHead className="w-[150px]">База данных</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.items.map((tenant) => (
              <TableRow key={tenant.id}>
                <TableCell className="font-mono text-xs font-medium">{tenant.slug}</TableCell>
                <TableCell className="text-sm">{tenant.displayName}</TableCell>
                <TableCell><StatusBadge status={tenant.status} /></TableCell>
                <TableCell>
                  <SchemaBadge version={tenant.schemaVersion} upToDate={tenant.schemaUpToDate} />
                </TableCell>
                <TableCell className="font-mono text-[11px] text-muted-foreground">
                  {tenant.dbName}
                </TableCell>
              </TableRow>
            ))}
            {data.items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="h-24 text-center text-muted-foreground">
                  Нет тенантов
                </TableCell>
              </TableRow>
            ) : null}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
