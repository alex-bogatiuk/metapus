"use client"

import { useEffect, useState, useCallback, useRef } from "react"
import {
  Download,
  CheckCircle2,
  Circle,
  Loader2,
  Undo2,
  AlertTriangle,
  Wifi,
  WifiOff,
  Server,
  Database,
  ArrowRightLeft,
  Package,
  RefreshCw,
  Sparkles,
  RotateCcw,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

// --- Types ---

interface UpdaterStatus {
  phase: string
  phaseDetail: string
  targetImage: string
  targetTag: string
  oldContainerId: string
  newContainerId: string
  startedAt: string | null
  completedAt: string | null
  lastError: string
  pullCurrent: number
  pullTotal: number
  logLength: number
}

interface AvailableInfo {
  available: boolean
  currentImage: string
  currentVersion: string
  latestImage: string
  latestVersion: string
}

// --- Phase definitions ---

const PHASES = [
  { key: "pulling", label: "Скачивание образа", icon: Download },
  { key: "starting", label: "Запуск нового контейнера", icon: Server },
  { key: "health_wait", label: "Проверка здоровья", icon: Wifi },
  { key: "switching", label: "Переключение трафика", icon: ArrowRightLeft },
  { key: "migrating", label: "Миграция БД", icon: Database },
  { key: "done", label: "Завершение", icon: CheckCircle2 },
] as const

const PHASE_ORDER = PHASES.map((p) => p.key)

function getPhaseIndex(phase: string): number {
  return PHASE_ORDER.indexOf(phase as typeof PHASE_ORDER[number])
}

// --- Component ---

interface UpdateSectionProps {
  updaterUrl: string
}

export function UpdateSection({ updaterUrl }: UpdateSectionProps) {
  const [status, setStatus] = useState<UpdaterStatus | null>(null)
  const [available, setAvailable] = useState<AvailableInfo | null>(null)
  const [connected, setConnected] = useState(false)
  const [tagInput, setTagInput] = useState("")
  const [loading, setLoading] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [rollbackOpen, setRollbackOpen] = useState(false)
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null)
  // Remember the "from" version when update starts
  const [previousVersion, setPreviousVersion] = useState<string>("")

  // Fetch status
  const fetchStatus = useCallback(async () => {
    try {
      const resp = await fetch(`${updaterUrl}/updater/status`)
      if (!resp.ok) throw new Error("status error")
      const data: UpdaterStatus = await resp.json()
      setStatus(data)
      setConnected(true)
    } catch {
      setConnected(false)
      setStatus(null)
    }
  }, [updaterUrl])

  // Fetch available info
  const fetchAvailable = useCallback(async () => {
    try {
      const resp = await fetch(`${updaterUrl}/updater/available`)
      if (!resp.ok) return
      const data: AvailableInfo = await resp.json()
      setAvailable(data)
    } catch {
      // ignore
    }
  }, [updaterUrl])

  // Poll while active
  useEffect(() => {
    fetchStatus()
    fetchAvailable()

    pollingRef.current = setInterval(() => {
      fetchStatus()
    }, 2000)

    const availableInterval = setInterval(() => {
      fetchAvailable()
    }, 60_000)

    return () => {
      if (pollingRef.current) clearInterval(pollingRef.current)
      clearInterval(availableInterval)
    }
  }, [fetchStatus, fetchAvailable])

  // Auto-fill tag when new version discovered
  useEffect(() => {
    if (available?.available && available.latestVersion && !tagInput) {
      setTagInput(available.latestVersion)
    }
  }, [available?.available, available?.latestVersion]) // eslint-disable-line react-hooks/exhaustive-deps

  // Start update
  const handleStart = async () => {
    if (!tagInput.trim()) return
    setLoading(true)
    setConfirmOpen(false)
    // Remember the current version before starting the update
    if (available?.currentVersion) {
      setPreviousVersion(available.currentVersion)
    }
    try {
      const resp = await fetch(`${updaterUrl}/updater/start`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ tag: tagInput.trim() }),
      })
      if (!resp.ok) {
        const err = await resp.json()
        alert(err.error || "Failed to start update")
      }
    } catch (e) {
      alert("Connection error")
    } finally {
      setLoading(false)
    }
  }

  // Rollback
  const handleRollback = async () => {
    setLoading(true)
    setRollbackOpen(false)
    try {
      const resp = await fetch(`${updaterUrl}/updater/rollback`, {
        method: "POST",
      })
      if (!resp.ok) {
        const err = await resp.json()
        alert(err.error || "Failed to rollback")
      }
    } catch {
      alert("Connection error")
    } finally {
      setLoading(false)
    }
  }

  // Reset (return to idle after failure)
  const handleReset = async () => {
    setLoading(true)
    try {
      const resp = await fetch(`${updaterUrl}/updater/reset`, {
        method: "POST",
      })
      if (!resp.ok) {
        const err = await resp.json()
        alert(err.error || "Failed to reset")
      }
      // Refresh UI state
      await fetchAvailable()
      await fetchStatus()
    } catch {
      alert("Connection error")
    } finally {
      setLoading(false)
    }
  }

  const isIdle = !status || status.phase === "idle"
  const isActive = status && !["idle", "done", "failed"].includes(status.phase)
  const isFailed = status?.phase === "failed"
  const isDone = status?.phase === "done"
  const currentPhaseIdx = status ? getPhaseIndex(status.phase) : -1
  const pullPercent =
    status && status.pullTotal > 0
      ? Math.round((status.pullCurrent / status.pullTotal) * 100)
      : 0

  // --- Version display logic ---
  // After a successful update: show targetTag as the new version
  // In idle: show currentVersion from /available endpoint (with image tag fallback on backend)
  const displayCurrentVersion = (() => {
    if (isDone && status?.targetTag) {
      return status.targetTag
    }
    return available?.currentVersion || "unknown"
  })()

  // Show "oldVersion → newVersion" during active update or after completion
  const showTransition = (isActive || isDone || isFailed) && status?.targetTag
  const fromVersion = previousVersion || available?.currentVersion || "?"

  return (
    <Card className="border-border/50">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Package className="h-5 w-5 text-muted-foreground" />
            <CardTitle className="text-base">Обновление системы</CardTitle>
          </div>
          <Badge
            variant="outline"
            className={
              connected
                ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-600"
                : "border-red-500/30 bg-red-500/10 text-red-600"
            }
          >
            {connected ? (
              <>
                <Wifi className="mr-1 h-3 w-3" /> Подключён
              </>
            ) : (
              <>
                <WifiOff className="mr-1 h-3 w-3" /> Нет связи
              </>
            )}
          </Badge>
        </div>
        <CardDescription>
          {showTransition ? (
            <>
              Обновление: <strong>{fromVersion}</strong>{" "}
              <ArrowRightLeft className="inline h-3 w-3 mx-1" />{" "}
              <strong>{status?.targetTag}</strong>
            </>
          ) : (
            <>
              Текущая версия: <strong>{displayCurrentVersion}</strong>
            </>
          )}
        </CardDescription>
      </CardHeader>

      <CardContent className="space-y-4">
        {/* Update available banner */}
        {isIdle && connected && available?.available && available.latestVersion && (
          <div className="flex items-center gap-2 rounded-md border border-blue-500/30 bg-blue-500/5 px-3 py-2 text-sm text-blue-600">
            <Sparkles className="h-4 w-4 shrink-0 animate-pulse" />
            <span>
              Доступна новая версия: <strong>{available.latestVersion}</strong>
            </span>
          </div>
        )}

        {/* Idle state — show tag input */}
        {isIdle && connected && (
          <div className="flex items-center gap-2">
            <Input
              placeholder="Тег версии (напр. v1.5.0)"
              value={tagInput}
              onChange={(e) => setTagInput(e.target.value)}
              className="max-w-xs"
              onKeyDown={(e) => {
                if (e.key === "Enter" && tagInput.trim()) setConfirmOpen(true)
              }}
            />
            <Button
              onClick={() => setConfirmOpen(true)}
              disabled={!tagInput.trim() || loading}
              size="sm"
            >
              <ArrowRightLeft className="mr-1.5 h-3.5 w-3.5" />
              Обновить
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => fetchAvailable()}
              title="Проверить обновления"
            >
              <RefreshCw className="h-3.5 w-3.5" />
            </Button>
          </div>
        )}

        {/* Active update — show phases */}
        {(isActive || isDone || isFailed) && status && (
          <div className="space-y-2">
            {PHASES.map((phase, idx) => {
              const Icon = phase.icon
              const isCompleted = currentPhaseIdx > idx || isDone
              const isCurrent =
                currentPhaseIdx === idx && !isDone && !isFailed

              return (
                <div
                  key={phase.key}
                  className={`flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors ${
                    isCurrent
                      ? "bg-primary/5 text-primary font-medium"
                      : isCompleted
                        ? "text-emerald-600"
                        : "text-muted-foreground"
                  }`}
                >
                  {isCompleted ? (
                    <CheckCircle2 className="h-4 w-4 text-emerald-500 shrink-0" />
                  ) : isCurrent ? (
                    <Loader2 className="h-4 w-4 animate-spin shrink-0" />
                  ) : (
                    <Circle className="h-4 w-4 shrink-0 opacity-30" />
                  )}
                  <Icon className="h-4 w-4 shrink-0" />
                  <span>{phase.label}</span>

                  {/* Pull progress */}
                  {phase.key === "pulling" && isCurrent && status.pullTotal > 0 && (
                    <div className="ml-auto flex items-center gap-2 min-w-[140px]">
                      <Progress value={pullPercent} className="h-1.5 w-20" />
                      <span className="text-xs tabular-nums">
                        {formatBytes(status.pullCurrent)} / {formatBytes(status.pullTotal)}
                      </span>
                    </div>
                  )}

                  {/* Phase detail (sub-status) */}
                  {isCurrent && status.phaseDetail && phase.key !== "pulling" && (
                    <span className="ml-auto text-xs text-muted-foreground tabular-nums">
                      {status.phaseDetail}
                    </span>
                  )}
                  {phase.key === "pulling" && isCurrent && status.phaseDetail && !status.pullTotal && (
                    <span className="ml-auto text-xs text-muted-foreground">
                      {status.phaseDetail}
                    </span>
                  )}
                </div>
              )
            })}

            {/* Target info */}
            {status.targetTag && (
              <div className="mt-2 text-xs text-muted-foreground px-3">
                Целевой образ: {status.targetImage}
              </div>
            )}
          </div>
        )}

        {/* Failed state */}
        {isFailed && status && (
          <>
            <div className="rounded-md border border-red-500/30 bg-red-500/5 px-3 py-2 text-sm text-red-600">
              <AlertTriangle className="inline mr-1.5 h-4 w-4" />
              {status.lastError}
            </div>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={handleReset}
                disabled={loading}
              >
                <RotateCcw className="mr-1.5 h-3.5 w-3.5" />
                Повторить
              </Button>
              {status.oldContainerId && (
                <Button
                  variant="outline"
                  size="sm"
                  className="text-destructive border-destructive/30"
                  onClick={() => setRollbackOpen(true)}
                  disabled={loading}
                >
                  <Undo2 className="mr-1.5 h-3.5 w-3.5" />
                  Откатить обновление
                </Button>
              )}
            </div>
          </>
        )}

        {/* Done state */}
        {isDone && status && (
          <>
            <div className="rounded-md border border-emerald-500/30 bg-emerald-500/5 px-3 py-2 text-sm text-emerald-600">
              <CheckCircle2 className="inline mr-1.5 h-4 w-4" />
              Обновлено: {fromVersion} → {status.targetTag}
              {status.completedAt && (
                <span className="ml-1 text-xs opacity-70">
                  ({new Date(status.completedAt).toLocaleTimeString()})
                </span>
              )}
            </div>
            <p className="text-xs text-muted-foreground px-1">
              Проверьте работоспособность системы. Если всё в порядке — подтвердите обновление. 
              При обнаружении проблем — выполните откат к предыдущей версии.
            </p>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                onClick={handleReset}
                disabled={loading}
              >
                {loading && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
                <CheckCircle2 className="mr-1.5 h-3.5 w-3.5" />
                Подтвердить
              </Button>
              {status.oldContainerId && (
                <Button
                  variant="outline"
                  size="sm"
                  className="text-destructive border-destructive/30"
                  onClick={() => setRollbackOpen(true)}
                  disabled={loading}
                >
                  <Undo2 className="mr-1.5 h-3.5 w-3.5" />
                  Откатить обновление
                </Button>
              )}
            </div>
          </>
        )}

        {/* Not connected */}
        {!connected && (
          <div className="text-sm text-muted-foreground">
            Updater Agent не подключён. Убедитесь, что сервис запущен в Docker.
          </div>
        )}
      </CardContent>

      {/* Start Confirm Dialog */}
      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <ArrowRightLeft className="h-5 w-5 text-primary" />
              Подтвердите обновление
            </DialogTitle>
            <DialogDescription>
              Система будет обновлена{" "}
              {available?.currentVersion && available.currentVersion !== "unknown" && (
                <>с версии <strong>{available.currentVersion}</strong>{" "}</>
              )}
              до версии <strong>{tagInput}</strong>.
              В процессе будет скачан новый Docker-образ, запущен новый контейнер,
              переключён трафик и выполнена миграция БД.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmOpen(false)}>
              Отменить
            </Button>
            <Button onClick={handleStart} disabled={loading}>
              {loading && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              Обновить
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Rollback Confirm Dialog */}
      <Dialog open={rollbackOpen} onOpenChange={setRollbackOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Undo2 className="h-5 w-5 text-destructive" />
              Подтвердите откат
            </DialogTitle>
            <DialogDescription>
              Система будет возвращена к предыдущей версии контейнера
              {previousVersion && <> (<strong>{previousVersion}</strong>)</>}.
              Если миграция БД была запущена, она также будет откачена.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRollbackOpen(false)} disabled={loading}>
              Отменить
            </Button>
            <Button variant="destructive" onClick={handleRollback} disabled={loading}>
              {loading && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              Откатить
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  )
}

// --- Helpers ---

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}
