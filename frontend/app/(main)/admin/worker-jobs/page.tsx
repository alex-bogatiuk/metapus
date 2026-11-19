"use client"

import { PermissionGate } from "@/components/auth/permission-gate"
import { WORKER_JOB_READ } from "@/lib/system-permissions"
import { format } from "date-fns"
import { ru } from "date-fns/locale"
import {
  Activity,
  CheckCircle2,
  Clock,
  FileSearch,
  Loader2,
  RefreshCw,
  X,
  XCircle,
} from "lucide-react"
import { useCallback, useRef, useState } from "react"

import { ScrollSentinel } from "@/components/shared/scroll-sentinel"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { DateTimePicker } from "@/components/ui/date-time-picker"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useCachedLoad } from "@/hooks/useCachedLoad"
import { useLatestRequest } from "@/hooks/useLatestRequest"
import { useHasTabCache, useTabState } from "@/hooks/useTabState"
import { api } from "@/lib/api"
import type { WorkerJob, WorkerJobStats } from "@/types/worker-job"
import {
  JOB_CATEGORIES,
  JOB_STATUSES,
  KNOWN_JOB_NAMES,
} from "@/types/worker-job"

// ---------------------------------------------------------------------------
// Status badge
// ---------------------------------------------------------------------------

const STATUS_ROW_STYLES: Record<string, string> = {
  running: "border-l-2 border-l-blue-400",
  success: "",
  skipped: "opacity-60",
  error: "border-l-2 border-l-red-500 bg-red-50/30 dark:bg-red-950/10",
}

function StatusBadge({ status }: { status: string }) {
  const meta = JOB_STATUSES.find((s) => s.value === status)
  const color = meta?.color ?? "text-muted-foreground"
  return (
    <span className={`text-xs font-medium ${color}`}>
      {meta?.label ?? status}
    </span>
  )
}

function formatDuration(ms?: number) {
  if (ms == null) return "—"
  if (ms < 1000) return `${ms}мс`
  return `${(ms / 1000).toFixed(1)}с`
}

// ---------------------------------------------------------------------------
// Stats cards
// ---------------------------------------------------------------------------

interface StatsCardsProps {
  stats: WorkerJobStats | null
  loading: boolean
}

function StatsCards({ stats, loading }: StatsCardsProps) {
  const cards = [
    {
      label: "Всего (24ч)",
      value: stats?.total ?? 0,
      icon: Activity,
      color: "text-blue-600",
      bg: "bg-blue-50 dark:bg-blue-950/30",
    },
    {
      label: "Успешно",
      value: stats?.success ?? 0,
      icon: CheckCircle2,
      color: "text-green-600",
      bg: "bg-green-50 dark:bg-green-950/30",
    },
    {
      label: "Ошибки",
      value: stats?.error ?? 0,
      icon: XCircle,
      color: "text-red-600",
      bg: "bg-red-50 dark:bg-red-950/30",
    },
    {
      label: "Ср. время",
      value: stats ? formatDuration(stats.avgDuration) : "—",
      icon: Clock,
      color: "text-purple-600",
      bg: "bg-purple-50 dark:bg-purple-950/30",
    },
  ]

  return (
    <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
      {cards.map((card) => (
        <div
          key={card.label}
          className="border rounded-lg p-3 flex items-center gap-3"
        >
          <div className={`${card.bg} p-2 rounded-md`}>
            <card.icon className={`h-4 w-4 ${card.color}`} />
          </div>
          <div>
            <p className="text-xs text-muted-foreground">{card.label}</p>
            {loading ? (
              <div className="h-5 w-12 bg-muted animate-pulse rounded mt-0.5" />
            ) : (
              <p className="font-semibold text-sm">
                {typeof card.value === "number"
                  ? card.value.toLocaleString("ru-RU")
                  : card.value}
              </p>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Detail dialog
// ---------------------------------------------------------------------------

import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { createTabStateKey } from "@/stores/useTabStateStore"

const tabStateKeys = {
  "items": createTabStateKey<WorkerJob[]>("frontend/app/(main)/admin/worker-jobs/page.tsx:items"),
  "stats": createTabStateKey<WorkerJobStats | null>("frontend/app/(main)/admin/worker-jobs/page.tsx:stats"),
  "totalCount": createTabStateKey<number>("frontend/app/(main)/admin/worker-jobs/page.tsx:totalCount"),
  "hasMore": createTabStateKey<boolean>("frontend/app/(main)/admin/worker-jobs/page.tsx:hasMore"),
  "jobName": createTabStateKey<string>("frontend/app/(main)/admin/worker-jobs/page.tsx:jobName"),
  "jobCategory": createTabStateKey<string>("frontend/app/(main)/admin/worker-jobs/page.tsx:jobCategory"),
  "status": createTabStateKey<string>("frontend/app/(main)/admin/worker-jobs/page.tsx:status"),
  "dateFrom": createTabStateKey<string>("frontend/app/(main)/admin/worker-jobs/page.tsx:dateFrom"),
  "dateTo": createTabStateKey<string>("frontend/app/(main)/admin/worker-jobs/page.tsx:dateTo"),
}


function DetailDialog({
  job,
  open,
  onOpenChange,
}: {
  job: WorkerJob | null
  open: boolean
  onOpenChange: (v: boolean) => void
}) {
  if (!job) return null
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="font-mono text-sm">{job.jobName}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3 text-sm">
          <Row label="Категория" value={job.jobCategory} />
          <Row label="Статус" value={<StatusBadge status={job.status} />} />
          <Row
            label="Начало"
            value={format(new Date(job.startedAt), "dd.MM.yyyy HH:mm:ss", { locale: ru })}
          />
          {job.finishedAt && (
            <Row
              label="Конец"
              value={format(new Date(job.finishedAt), "dd.MM.yyyy HH:mm:ss", { locale: ru })}
            />
          )}
          <Row label="Длительность" value={formatDuration(job.durationMs)} />
          {job.itemsProcessed != null && (
            <Row label="Обработано" value={String(job.itemsProcessed)} />
          )}
          {job.errorMessage && (
            <div>
              <p className="text-xs text-muted-foreground mb-1">Ошибка</p>
              <p className="text-xs text-red-600 bg-red-50 dark:bg-red-950/20 rounded p-2 break-all">
                {job.errorMessage}
              </p>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2">
      <span className="text-muted-foreground w-32 shrink-0">{label}:</span>
      <span className="font-medium">{value}</span>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Main page
// ---------------------------------------------------------------------------

function WorkerJobsPageContent() {
  const hasCachedItems = useHasTabCache(tabStateKeys["items"])

  const [items, setItems] = useTabState(tabStateKeys["items"], [])
  const [stats, setStats] = useTabState(tabStateKeys["stats"], null)
  const [totalCount, setTotalCount] = useTabState(tabStateKeys["totalCount"], 0)
  const [hasMore, setHasMore] = useTabState(tabStateKeys["hasMore"], false)

  const [loading, setLoading] = useState(!hasCachedItems)
  const [loadingMore, setLoadingMore] = useState(false)
  const [statsLoading, setStatsLoading] = useState(!hasCachedItems)

  // Filters
  const [jobName, setJobName] = useTabState(tabStateKeys["jobName"], "")
  const [jobCategory, setJobCategory] = useTabState(tabStateKeys["jobCategory"], "")
  const [status, setStatus] = useTabState(tabStateKeys["status"], "")
  const [dateFrom, setDateFrom] = useTabState(tabStateKeys["dateFrom"], "")
  const [dateTo, setDateTo] = useTabState(tabStateKeys["dateTo"], "")

  const nextCursorRef = useRef<string>("")
  const listRequests = useLatestRequest()
  const statsRequests = useLatestRequest()
  const loadMoreBusy = useRef(false)
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  const [selectedJob, setSelectedJob] = useState<WorkerJob | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)

  const hasAnyFilter = !!(jobName || jobCategory || status || dateFrom || dateTo)

  const buildParams = useCallback(() => ({
    ...(jobName ? { jobName } : {}),
    ...(jobCategory ? { jobCategory } : {}),
    ...(status ? { status } : {}),
    ...(dateFrom ? { dateFrom } : {}),
    ...(dateTo ? { dateTo } : {}),
  }), [jobName, jobCategory, status, dateFrom, dateTo])

  const fetchData = useCallback(async () => {
    const isCurrent = listRequests.begin()
    setLoadingMore(false)
    setLoading(true)
    try {
      const result = await api.system.workerJobs.list(buildParams())
      if (!isCurrent()) return
      setItems(result.items)
      setHasMore(result.hasMore)
      setTotalCount(result.totalCount)
      nextCursorRef.current = result.nextCursor ?? ""
    } catch (err) {
      if (!isCurrent()) return
      console.error("WorkerJobs fetch error:", err)
      setItems([])
    } finally {
      if (isCurrent()) setLoading(false)
    }
  }, [buildParams, setItems, setHasMore, setTotalCount, listRequests])

  const loadMore = useCallback(async () => {
    if (!nextCursorRef.current || loading || loadMoreBusy.current) return
    loadMoreBusy.current = true
    const isCurrent = listRequests.capture()
    setLoadingMore(true)
    try {
      const result = await api.system.workerJobs.list({
        ...buildParams(),
        after: nextCursorRef.current,
      })
      if (!isCurrent()) return
      setItems((prev) => [...prev, ...result.items])
      setHasMore(result.hasMore)
      nextCursorRef.current = result.nextCursor ?? ""
    } catch (err) {
      if (!isCurrent()) return
      console.error("WorkerJobs loadMore error:", err)
    } finally {
      loadMoreBusy.current = false
      if (isCurrent()) setLoadingMore(false)
    }
  }, [buildParams, loading, setItems, setHasMore, listRequests])

  const fetchStats = useCallback(async () => {
    const isCurrent = statsRequests.begin()
    setStatsLoading(true)
    try {
      const s = await api.system.workerJobs.stats()
      if (isCurrent()) setStats(s)
    } catch {
      /* ignore */
    } finally {
      if (isCurrent()) setStatsLoading(false)
    }
  }, [setStats, statsRequests])

  useCachedLoad(fetchData, hasCachedItems, scrollContainerRef)
  useCachedLoad(fetchStats, hasCachedItems)

  const handleRefresh = () => {
    if (scrollContainerRef.current) scrollContainerRef.current.scrollTop = 0
    fetchData()
    fetchStats()
  }

  const resetFilters = () => {
    setJobName("")
    setJobCategory("")
    setStatus("")
    setDateFrom("")
    setDateTo("")
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col gap-4 p-4 max-w-[1400px] mx-auto overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Задачи воркера</h1>
          <p className="text-sm text-muted-foreground">
            Журнал выполнения фоновых задач (хранится 7 дней)
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={handleRefresh} disabled={loading}>
          <RefreshCw className={`h-4 w-4 mr-1.5 ${loading ? "animate-spin" : ""}`} />
          Обновить
        </Button>
      </div>

      {/* Stats */}
      <StatsCards stats={stats} loading={statsLoading} />

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-2">
        <Select value={jobName || "all"} onValueChange={(v) => setJobName(v === "all" ? "" : v)}>
          <SelectTrigger className="w-[220px] h-9">
            <SelectValue placeholder="Все задачи" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все задачи</SelectItem>
            {KNOWN_JOB_NAMES.map((j) => (
              <SelectItem key={j.value} value={j.value}>
                {j.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={jobCategory || "all"} onValueChange={(v) => setJobCategory(v === "all" ? "" : v)}>
          <SelectTrigger className="w-[160px] h-9">
            <SelectValue placeholder="Категория" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все категории</SelectItem>
            {JOB_CATEGORIES.map((c) => (
              <SelectItem key={c.value} value={c.value}>
                {c.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={status || "all"} onValueChange={(v) => setStatus(v === "all" ? "" : v)}>
          <SelectTrigger className="w-[160px] h-9">
            <SelectValue placeholder="Статус" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все статусы</SelectItem>
            {JOB_STATUSES.map((s) => (
              <SelectItem key={s.value} value={s.value}>
                {s.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <DateTimePicker
          value={dateFrom ? new Date(dateFrom) : undefined}
          onChange={(d) => setDateFrom(d ? d.toISOString() : "")}
          placeholder="С даты"
          className="w-[200px] h-9"
        />
        <DateTimePicker
          value={dateTo ? new Date(dateTo) : undefined}
          onChange={(d) => setDateTo(d ? d.toISOString() : "")}
          placeholder="По дату"
          className="w-[200px] h-9"
        />

        {hasAnyFilter && (
          <Button
            variant="ghost"
            size="sm"
            className="h-9 gap-1 text-muted-foreground"
            onClick={resetFilters}
          >
            <X className="h-3.5 w-3.5" />
            Сбросить
          </Button>
        )}

        {totalCount > 0 && (
          <span className="text-sm text-muted-foreground ml-auto">
            Всего: {totalCount.toLocaleString("ru-RU")}
          </span>
        )}
      </div>

      {/* Table */}
      <ScrollArea className="flex-1 min-h-0 border rounded-lg" viewportRef={scrollContainerRef}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[160px]">Начало</TableHead>
              <TableHead className="w-[90px]">Статус</TableHead>
              <TableHead className="w-[180px]">Задача</TableHead>
              <TableHead className="w-[120px]">Категория</TableHead>
              <TableHead className="w-[90px]">Длит.</TableHead>
              <TableHead className="w-[80px]">Обраб.</TableHead>
              <TableHead>Ошибка</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={7} className="text-center py-12">
                  <Loader2 className="h-6 w-6 animate-spin mx-auto text-muted-foreground" />
                </TableCell>
              </TableRow>
            ) : items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} className="py-16">
                  <div className="flex flex-col items-center gap-2 text-muted-foreground">
                    <FileSearch className="h-10 w-10 opacity-40" />
                    <p className="text-sm">
                      {hasAnyFilter
                        ? "Нет задач по заданным фильтрам"
                        : "Журнал задач пуст"}
                    </p>
                    {hasAnyFilter && (
                      <Button variant="outline" size="sm" onClick={resetFilters}>
                        Сбросить фильтры
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ) : (
              items.map((job) => (
                <TableRow
                  key={job.id}
                  className={`cursor-pointer hover:bg-muted/50 ${STATUS_ROW_STYLES[job.status] ?? ""}`}
                  onClick={() => { setSelectedJob(job); setDetailOpen(true) }}
                >
                  <TableCell className="font-mono text-xs">
                    {format(new Date(job.startedAt), "dd.MM.yy HH:mm:ss", { locale: ru })}
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={job.status} />
                  </TableCell>
                  <TableCell className="font-mono text-xs">{job.jobName}</TableCell>
                  <TableCell>
                    <Badge variant="outline" className="text-xs">
                      {JOB_CATEGORIES.find((c) => c.value === job.jobCategory)?.label ?? job.jobCategory}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {formatDuration(job.durationMs)}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {job.itemsProcessed != null ? job.itemsProcessed.toLocaleString("ru-RU") : "—"}
                  </TableCell>
                  <TableCell className="text-xs text-red-500 truncate max-w-[200px]">
                    {job.errorMessage ?? ""}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
        <ScrollSentinel
          onIntersect={loadMore}
          loading={loadingMore}
          enabled={hasMore && !loading}
          scrollContainer={scrollContainerRef}
        />
      </ScrollArea>

      <DetailDialog
        job={selectedJob}
        open={detailOpen}
        onOpenChange={setDetailOpen}
      />
    </div>
  )
}

export default function WorkerJobsPage() {
  return (
    <PermissionGate permission={WORKER_JOB_READ} subject="задачам воркера">
      <WorkerJobsPageContent />
    </PermissionGate>
  )
}
