"use client"

import { useEffect, useState, useCallback } from "react"
import {
  Server,
  Database,
  Clock,
  CheckCircle2,
  AlertTriangle,
  RefreshCw,
  Loader2,
  Copy,
  Check,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"

interface VersionInfo {
  version: string
  buildTime: string
  expectedSchemaVersion: number
}

interface HealthInfo {
  app: string
  version: string
  mode: string
  meta_database: Record<string, number>
  tenants: Record<string, number>
}

// ── Info Row ────────────────────────────────────────────────────────────

function InfoRow({
  icon: Icon,
  label,
  value,
  badge,
  badgeVariant = "secondary",
  mono = false,
}: {
  icon: React.ElementType
  label: string
  value: string
  badge?: string
  badgeVariant?: "default" | "secondary" | "destructive" | "outline"
  mono?: boolean
}) {
  return (
    <div className="flex items-center justify-between py-2.5">
      <div className="flex items-center gap-2.5 text-sm text-muted-foreground">
        <Icon className="h-4 w-4" />
        <span>{label}</span>
      </div>
      <div className="flex items-center gap-2">
        <span className={cn("text-sm font-medium text-foreground", mono && "font-mono text-xs")}>
          {value}
        </span>
        {badge && (
          <Badge variant={badgeVariant} className="text-[10px] px-1.5 py-0">
            {badge}
          </Badge>
        )}
      </div>
    </div>
  )
}

// ── Stat Card ───────────────────────────────────────────────────────────

function StatCard({
  label,
  value,
  description,
}: {
  label: string
  value: string | number
  description?: string
}) {
  return (
    <div className="rounded-lg border bg-card p-4 text-card-foreground">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p className="mt-1 text-2xl font-bold tabular-nums">{value}</p>
      {description && (
        <p className="mt-0.5 text-[11px] text-muted-foreground">{description}</p>
      )}
    </div>
  )
}

// ── Main Component ──────────────────────────────────────────────────────

export function SystemInfoSection() {
  const [versionInfo, setVersionInfo] = useState<VersionInfo | null>(null)
  const [healthInfo, setHealthInfo] = useState<HealthInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [ver, health] = await Promise.allSettled([
        api.system.version(),
        fetch("/health/info").then((r) => r.ok ? r.json() : null),
      ])
      if (ver.status === "fulfilled") setVersionInfo(ver.value)
      if (health.status === "fulfilled" && health.value) setHealthInfo(health.value)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  const handleCopyDiagnostics = useCallback(() => {
    const lines = [
      `Metapus ${versionInfo?.version ?? "unknown"}`,
      `Build: ${versionInfo?.buildTime ?? "unknown"}`,
      `Schema: ${versionInfo?.expectedSchemaVersion ?? "unknown"}`,
      `Mode: ${healthInfo?.mode ?? "unknown"}`,
      `Pools: ${healthInfo?.tenants?.active_pools ?? "?"}`,
      `Date: ${new Date().toISOString()}`,
    ]
    navigator.clipboard.writeText(lines.join("\n"))
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }, [versionInfo, healthInfo])

  // ── Format build time ──────────────────────────────────────────────────
  const formatBuildTime = (iso: string) => {
    if (!iso || iso === "unknown") return "—"
    try {
      const d = new Date(iso)
      return d.toLocaleString("ru-RU", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
      })
    } catch {
      return iso
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
          <span className="text-sm font-medium">Ошибка загрузки информации о системе</span>
        </div>
        <p className="mt-2 text-xs text-muted-foreground">{error}</p>
        <Button variant="outline" size="sm" className="mt-3" onClick={fetchData}>
          <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          Повторить
        </Button>
      </div>
    )
  }

  const isDev = versionInfo?.version === "dev"

  return (
    <div className="space-y-6">
      {/* Header with version badge */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10">
            <Server className="h-5 w-5 text-primary" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h3 className="text-base font-semibold text-foreground">
                Metapus
              </h3>
              <Badge
                variant={isDev ? "outline" : "default"}
                className="text-xs"
              >
                {versionInfo?.version ?? "—"}
              </Badge>
            </div>
            <p className="text-xs text-muted-foreground">
              ERP Platform • Multi-tenant Architecture
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={handleCopyDiagnostics}>
            {copied ? (
              <Check className="mr-1.5 h-3.5 w-3.5 text-green-500" />
            ) : (
              <Copy className="mr-1.5 h-3.5 w-3.5" />
            )}
            {copied ? "Скопировано" : "Копировать диагностику"}
          </Button>
          <Button variant="outline" size="sm" onClick={fetchData}>
            <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
            Обновить
          </Button>
        </div>
      </div>

      <Separator />

      {/* Version details */}
      <div>
        <h4 className="mb-1 text-sm font-semibold text-foreground">
          Версия и сборка
        </h4>
        <div className="rounded-lg border bg-card/50 px-4 divide-y divide-border">
          <InfoRow
            icon={Server}
            label="Версия сервера"
            value={versionInfo?.version ?? "—"}
            badge={isDev ? "development" : undefined}
            badgeVariant={isDev ? "outline" : "default"}
          />
          <InfoRow
            icon={Clock}
            label="Время сборки"
            value={formatBuildTime(versionInfo?.buildTime ?? "")}
          />
          <InfoRow
            icon={Database}
            label="Версия схемы БД"
            value={String(versionInfo?.expectedSchemaVersion ?? "—")}
            mono
          />
          <InfoRow
            icon={CheckCircle2}
            label="Режим работы"
            value={healthInfo?.mode === "multi-tenant" ? "Multi-tenant" : "Standalone"}
            badge={healthInfo?.mode === "multi-tenant" ? "database-per-tenant" : undefined}
          />
        </div>
      </div>

      {/* Runtime stats (only if health data available) */}
      {healthInfo && (
        <>
          <Separator />
          <div>
            <h4 className="mb-3 text-sm font-semibold text-foreground">
              Состояние сервера
            </h4>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <StatCard
                label="Пулы БД"
                value={healthInfo.tenants?.active_pools ?? 0}
                description="Активные подключения"
              />
              <StatCard
                label="Всего соединений"
                value={healthInfo.tenants?.total_conns ?? 0}
                description="К тенантным БД"
              />
              <StatCard
                label="Мета-БД соединений"
                value={healthInfo.meta_database?.total_conns ?? 0}
                description="К meta database"
              />
              <StatCard
                label="Idle соединений"
                value={healthInfo.tenants?.idle_conns ?? 0}
                description="Свободные пулы"
              />
            </div>
          </div>
        </>
      )}

      {/* Dev mode warning */}
      {isDev && (
        <>
          <Separator />
          <div className="flex items-start gap-3 rounded-lg border border-amber-500/30 bg-amber-500/5 p-4">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-500" />
            <div>
              <p className="text-sm font-medium text-amber-700 dark:text-amber-400">
                Сборка для разработки
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                Версия &quot;dev&quot; означает, что бинарник собран без ldflags.
                Для production-сборки используйте{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[11px] font-mono">
                  make build VERSION=v1.0.0
                </code>
              </p>
            </div>
          </div>
        </>
      )}

      {/* Spacer */}
      <div className="h-8" />
    </div>
  )
}
