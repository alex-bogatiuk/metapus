"use client"

import { PermissionGate } from "@/components/auth/permission-gate"
import { EVENT_LOG_READ } from "@/lib/system-permissions"
import { format } from "date-fns"
import { ru } from "date-fns/locale"
import {
  ChevronDown,
  FileSearch,
  Loader2,
  RefreshCw,
  Search,
  SlidersHorizontal,
  X,
} from "lucide-react"
import { useSearchParams } from "next/navigation"
import { useCallback, useRef, useState } from "react"

import { EntityPicker } from "@/components/shared/entity-picker"
import { ScrollSentinel } from "@/components/shared/scroll-sentinel"
import { UserPicker } from "@/components/shared/user-picker"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { DateTimePicker } from "@/components/ui/date-time-picker"
import { Input } from "@/components/ui/input"
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
import { useDebouncedValue } from "@/hooks/useDebouncedValue"
import { useLatestRequest } from "@/hooks/useLatestRequest"
import { useHasTabCache, useTabState } from "@/hooks/useTabState"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type {
  EventLogEntry,
  EventLogStats,
} from "@/types/event-log"
import {
  EVENT_CATEGORIES,
  EVENT_SEVERITIES,
  EVENT_SOURCES,
  EVENT_TYPES,
} from "@/types/event-log"

import { createTabStateKey } from "@/stores/useTabStateStore"
import { CategoryBadge } from "./_components/category-badge"
import { DetailDialog } from "./_components/detail-dialog"
import { SeverityBadge } from "./_components/severity-badge"
import { StatsCards } from "./_components/stats-cards"
import { TraceDialog } from "./_components/trace-dialog"

const tabStateKeys = {
  "items": createTabStateKey<EventLogEntry[]>("frontend/app/(main)/admin/event-log/page.tsx:items"),
  "stats": createTabStateKey<EventLogStats | null>("frontend/app/(main)/admin/event-log/page.tsx:stats"),
  "totalCount": createTabStateKey<number>("frontend/app/(main)/admin/event-log/page.tsx:totalCount"),
  "hasMore": createTabStateKey<boolean>("frontend/app/(main)/admin/event-log/page.tsx:hasMore"),
  "category": createTabStateKey<string>("frontend/app/(main)/admin/event-log/page.tsx:category"),
  "severity": createTabStateKey<string>("frontend/app/(main)/admin/event-log/page.tsx:severity"),
  "search": createTabStateKey<string>("frontend/app/(main)/admin/event-log/page.tsx:search"),
  "dateFrom": createTabStateKey<string>("frontend/app/(main)/admin/event-log/page.tsx:dateFrom"),
  "dateTo": createTabStateKey<string>("frontend/app/(main)/admin/event-log/page.tsx:dateTo"),
  "entityType": createTabStateKey<string>("frontend/app/(main)/admin/event-log/page.tsx:entityType"),
  "entityId": createTabStateKey<string>("frontend/app/(main)/admin/event-log/page.tsx:entityId"),
  "entityDisplayName": createTabStateKey<string>("frontend/app/(main)/admin/event-log/page.tsx:entityDisplayName"),
  "eventType": createTabStateKey<string>("frontend/app/(main)/admin/event-log/page.tsx:eventType"),
  "source": createTabStateKey<string>("frontend/app/(main)/admin/event-log/page.tsx:source"),
  "userId": createTabStateKey<string>("frontend/app/(main)/admin/event-log/page.tsx:userId"),
  "userDisplayName": createTabStateKey<string>("frontend/app/(main)/admin/event-log/page.tsx:userDisplayName"),
}


// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const SEVERITY_ROW_STYLES: Record<string, string> = {
  info: "border-l-2 border-l-blue-400",
  warning: "border-l-2 border-l-yellow-400",
  error: "border-l-2 border-l-red-500 bg-red-50/30 dark:bg-red-950/10",
  critical: "border-l-[3px] border-l-red-700 bg-red-50/50 dark:bg-red-950/20",
}

// ---------------------------------------------------------------------------
// Main page
// ---------------------------------------------------------------------------

function EventLogPageContent() {
  const searchParams = useSearchParams()
  const hasCachedItems = useHasTabCache(tabStateKeys["items"])

  // Pre-fill from URL search params (deep-linking from document/catalog forms)
  const urlEntityType = searchParams.get("entityType") ?? ""
  const urlEntityId = searchParams.get("entityId") ?? ""
  const urlUserId = searchParams.get("userId") ?? ""
  const hasUrlFilters = !!(urlEntityType || urlEntityId || urlUserId)

  const [items, setItems] = useTabState(tabStateKeys["items"], [])
  const [stats, setStats] = useTabState(tabStateKeys["stats"], null)
  const [totalCount, setTotalCount] = useTabState(tabStateKeys["totalCount"], 0)
  const [hasMore, setHasMore] = useTabState(tabStateKeys["hasMore"], false)

  const [loading, setLoading] = useState(!hasCachedItems)
  const [loadingMore, setLoadingMore] = useState(false)
  const [statsLoading, setStatsLoading] = useState(!hasCachedItems)
  const [searchFocused, setSearchFocused] = useState(false)

  // Basic filters (always visible)
  const [category, setCategory] = useTabState(tabStateKeys["category"], "")
  const [severity, setSeverity] = useTabState(tabStateKeys["severity"], "")
  const [search, setSearch] = useTabState(tabStateKeys["search"], "")
  const [dateFrom, setDateFrom] = useTabState(tabStateKeys["dateFrom"], "")
  const [dateTo, setDateTo] = useTabState(tabStateKeys["dateTo"], "")
  const debouncedSearch = useDebouncedValue(search, 400)

  // Advanced filters (collapsible panel)
  const [entityType, setEntityType] = useTabState(tabStateKeys["entityType"], urlEntityType)
  const [entityId, setEntityId] = useTabState(tabStateKeys["entityId"], urlEntityId)
  const [entityDisplayName, setEntityDisplayName] = useTabState(tabStateKeys["entityDisplayName"], "")
  const [eventType, setEventType] = useTabState(tabStateKeys["eventType"], "")
  const [source, setSource] = useTabState(tabStateKeys["source"], "")
  const [userId, setUserId] = useTabState(tabStateKeys["userId"], urlUserId)
  const [userDisplayName, setUserDisplayName] = useTabState(tabStateKeys["userDisplayName"], "")

  // Panel state
  const [advancedOpen, setAdvancedOpen] = useState(hasUrlFilters || !!(entityType || entityId || eventType || source || userId))

  // Metadata for filter chip labels
  const getLabel = useMetadataStore((s) => s.getLabel)

  // Pagination cursor
  const nextCursorRef = useRef<string>("")
  const listRequests = useLatestRequest()
  const statsRequests = useLatestRequest()
  const loadMoreBusy = useRef(false)
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  // Dialogs (transient — not cached)
  const [selectedEvent, setSelectedEvent] = useState<EventLogEntry | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [traceId, setTraceId] = useState("")
  const [traceOpen, setTraceOpen] = useState(false)

  const hasAnyAdvancedFilter = !!(entityType || entityId || eventType || source || userId)
  const hasAnyFilter = !!(category || severity || search || dateFrom || dateTo || hasAnyAdvancedFilter)

  const buildParams = useCallback(() => {
    const params: Record<string, string | undefined> = {}
    if (category) params.category = category
    if (severity) params.severity = severity
    if (debouncedSearch) params.search = debouncedSearch
    if (dateFrom) params.dateFrom = dateFrom
    if (dateTo) params.dateTo = dateTo
    if (entityType) params.entityType = entityType
    if (entityId) params.entityId = entityId
    if (eventType) params.eventType = eventType
    if (source) params.source = source
    if (userId) params.userId = userId
    return params
  }, [category, severity, debouncedSearch, dateFrom, dateTo, entityType, entityId, eventType, source, userId])

  const fetchData = useCallback(
    async () => {
      const isCurrent = listRequests.begin()
      setLoadingMore(false)
    setLoading(true)
      try {
        const result = await api.system.eventLog.list(buildParams())
        if (!isCurrent()) return
        setItems(result.items)
        setHasMore(result.hasMore)
        setTotalCount(result.totalCount ?? 0)
        nextCursorRef.current = result.nextCursor ?? ""
      } catch (err) {
        if (!isCurrent()) return
      console.error("EventLog fetch error:", err)
        setItems([])
      } finally {
        if (isCurrent()) setLoading(false)
      }
    },
    [buildParams, setItems, setHasMore, setTotalCount, listRequests]
  )

  const loadMore = useCallback(async () => {
    if (!nextCursorRef.current || loading || loadMoreBusy.current) return
    loadMoreBusy.current = true
    const isCurrent = listRequests.capture()
    setLoadingMore(true)
    try {
      const result = await api.system.eventLog.list({
        ...buildParams(),
        after: nextCursorRef.current,
      })
      if (!isCurrent()) return
      setItems((prev) => [...prev, ...result.items])
      setHasMore(result.hasMore)
      nextCursorRef.current = result.nextCursor ?? ""
    } catch (err) {
      if (!isCurrent()) return
      console.error("EventLog loadMore error:", err)
    } finally {
      loadMoreBusy.current = false
      if (isCurrent()) setLoadingMore(false)
    }
  }, [buildParams, loading, setItems, setHasMore, listRequests])

  const fetchStats = useCallback(async () => {
    const isCurrent = statsRequests.begin()
    setStatsLoading(true)
    try {
      const params: Record<string, string> = {}
      if (dateFrom) params.dateFrom = dateFrom
      if (dateTo) params.dateTo = dateTo
      const s = await api.system.eventLog.stats(params)
      if (isCurrent()) setStats(s)
    } catch {
      /* ignore */
    } finally {
      if (isCurrent()) setStatsLoading(false)
    }
  }, [dateFrom, dateTo, setStats, statsRequests])

  // Keep ref in sync so debounced search always calls latest fetchData
  // Initial load (skip if we have cached items from a tab switch)
  useCachedLoad(fetchData, hasCachedItems && !hasUrlFilters, scrollContainerRef)
  useCachedLoad(fetchStats, hasCachedItems && !hasUrlFilters)

  const handleRefresh = () => {
    if (scrollContainerRef.current) scrollContainerRef.current.scrollTop = 0
    fetchData()
    fetchStats()
  }

  const handleRowClick = (e: EventLogEntry) => {
    setSelectedEvent(e)
    setDetailOpen(true)
  }

  const handleOpenTrace = (tid: string) => {
    setTraceId(tid)
    setTraceOpen(true)
  }

  const resetAllFilters = () => {
    setCategory("")
    setSeverity("")
    setSearch("")
    setDateFrom("")
    setDateTo("")
    setEntityType("")
    setEntityId("")
    setEntityDisplayName("")
    setEventType("")
    setSource("")
    setUserId("")
    setUserDisplayName("")
  }

  const resetAdvancedFilters = () => {
    setEntityType("")
    setEntityId("")
    setEntityDisplayName("")
    setEventType("")
    setSource("")
    setUserId("")
    setUserDisplayName("")
  }

  // Count of active advanced filters (for badge)
  const advancedFilterCount = [entityType && entityId ? "entity" : "", eventType, source, userId].filter(Boolean).length
    + (entityType && !entityId ? 1 : 0)

  return (
    <div className="flex flex-1 min-h-0 flex-col gap-4 p-4 max-w-[1400px] mx-auto overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Журнал событий</h1>
          <p className="text-sm text-muted-foreground">
            Системный журнал регистрации событий
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={handleRefresh} disabled={loading}>
          <RefreshCw className={`h-4 w-4 mr-1.5 ${loading ? "animate-spin" : ""}`} />
          Обновить
        </Button>
      </div>

      {/* Stats */}
      <StatsCards stats={stats} loading={statsLoading} />

      {/* Basic Filters */}
      <div className="flex flex-wrap items-center gap-2">
        <div
          className={cn(
            "relative transition-all duration-200 ease-in-out",
            searchFocused || search
              ? "w-[400px]"
              : "w-[200px]"
          )}
        >
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Поиск по сообщению..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onFocus={() => setSearchFocused(true)}
            onBlur={() => setSearchFocused(false)}
            className="pl-9 h-9"
          />
        </div>

        <Select value={category || "all"} onValueChange={(v) => setCategory(v === "all" ? "" : v)}>
          <SelectTrigger className="w-[160px] h-9">
            <SelectValue placeholder="Категория" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все категории</SelectItem>
            {EVENT_CATEGORIES.map((c) => (
              <SelectItem key={c.value} value={c.value}>
                {c.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select value={severity || "all"} onValueChange={(v) => setSeverity(v === "all" ? "" : v)}>
          <SelectTrigger className="w-[160px] h-9">
            <SelectValue placeholder="Важность" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все уровни</SelectItem>
            {EVENT_SEVERITIES.map((s) => (
              <SelectItem key={s.value} value={s.value}>
                {s.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        {/* Date+time range filters */}
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

        {/* Reset all filters */}
        {hasAnyFilter && (
          <Button
            variant="ghost"
            size="sm"
            className="h-9 gap-1 text-muted-foreground"
            onClick={resetAllFilters}
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

      {/* Advanced Filters (collapsible) */}
      <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
        <CollapsibleTrigger asChild>
          <Button variant="ghost" size="sm" className="h-8 gap-1.5 text-muted-foreground -mt-2">
            <SlidersHorizontal className="h-3.5 w-3.5" />
            Расширенные фильтры
            {advancedFilterCount > 0 && (
              <Badge variant="secondary" className="h-5 min-w-5 px-1.5 text-[10px] font-bold rounded-full">
                {advancedFilterCount}
              </Badge>
            )}
            <ChevronDown className={`h-3.5 w-3.5 transition-transform ${advancedOpen ? "rotate-180" : ""}`} />
          </Button>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className="flex flex-col gap-2 pt-1 pb-1">
            {/* Entity Picker (two-step: type → record) */}
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground shrink-0 w-[80px]">Объект:</span>
              <EntityPicker
                entityType={entityType}
                entityId={entityId}
                displayName={entityDisplayName}
                onChange={(type, id, display) => {
                  setEntityType(type)
                  setEntityId(id)
                  setEntityDisplayName(display)
                }}
                className="flex-1"
              />
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {/* User Picker */}
              <div className="flex items-center gap-2 min-w-[300px]">
                <span className="text-xs text-muted-foreground shrink-0 w-[80px]">Пользователь:</span>
                <UserPicker
                  value={userId}
                  displayName={userDisplayName}
                  onChange={(id, display) => {
                    setUserId(id)
                    setUserDisplayName(display)
                  }}
                  className="flex-1"
                />
              </div>

              {/* Event Type */}
              <Select value={eventType || "all"} onValueChange={(v) => setEventType(v === "all" ? "" : v)}>
                <SelectTrigger className="w-[200px] h-9">
                  <SelectValue placeholder="Тип события" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Все типы</SelectItem>
                  {EVENT_TYPES.map((t) => (
                    <SelectItem key={t.value} value={t.value}>
                      {t.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              {/* Source */}
              <Select value={source || "all"} onValueChange={(v) => setSource(v === "all" ? "" : v)}>
                <SelectTrigger className="w-[180px] h-9">
                  <SelectValue placeholder="Источник" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Все источники</SelectItem>
                  {EVENT_SOURCES.map((s) => (
                    <SelectItem key={s.value} value={s.value}>
                      {s.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Reset advanced */}
            {hasAnyAdvancedFilter && (
              <Button
                variant="ghost"
                size="sm"
                className="h-9 gap-1 text-muted-foreground"
                onClick={resetAdvancedFilters}
              >
                <X className="h-3.5 w-3.5" />
                Сбросить доп.
              </Button>
            )}
          </div>

          {/* Active filter chips */}
          {hasAnyAdvancedFilter && (
            <div className="flex flex-wrap gap-1.5 pb-1">
              {entityType && entityId && entityDisplayName && (
                <Badge variant="secondary" className="gap-1 pl-2 pr-1 h-6 text-xs">
                  {getLabel(entityType, "singular")}: {entityDisplayName}
                  <button onClick={() => { setEntityType(""); setEntityId(""); setEntityDisplayName("") }} className="ml-0.5 hover:text-foreground">
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              )}
              {entityType && !entityId && (
                <Badge variant="secondary" className="gap-1 pl-2 pr-1 h-6 text-xs">
                  Объект: {getLabel(entityType, "singular")}
                  <button onClick={() => { setEntityType(""); setEntityDisplayName("") }} className="ml-0.5 hover:text-foreground">
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              )}
              {eventType && (
                <Badge variant="secondary" className="gap-1 pl-2 pr-1 h-6 text-xs">
                  Тип: {EVENT_TYPES.find((t) => t.value === eventType)?.label ?? eventType}
                  <button onClick={() => setEventType("")} className="ml-0.5 hover:text-foreground">
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              )}
              {source && (
                <Badge variant="secondary" className="gap-1 pl-2 pr-1 h-6 text-xs">
                  Источник: {EVENT_SOURCES.find((s) => s.value === source)?.label ?? source}
                  <button onClick={() => setSource("")} className="ml-0.5 hover:text-foreground">
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              )}
              {userId && (
                <Badge variant="secondary" className="gap-1 pl-2 pr-1 h-6 text-xs">
                  Пользователь: {userDisplayName || userId.substring(0, 8) + "…"}
                  <button onClick={() => { setUserId(""); setUserDisplayName("") }} className="ml-0.5 hover:text-foreground">
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              )}
            </div>
          )}
        </CollapsibleContent>
      </Collapsible>

      {/* Scrollable table area */}
      <ScrollArea className="flex-1 min-h-0 border rounded-lg" viewportRef={scrollContainerRef}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[160px]">Время</TableHead>
              <TableHead className="w-[100px]">Важность</TableHead>
              <TableHead className="w-[100px]">Категория</TableHead>
              <TableHead className="w-[160px]">Тип</TableHead>
              <TableHead>Сообщение</TableHead>
              <TableHead className="w-[140px]">Пользователь</TableHead>
              <TableHead className="w-[80px]">Длит.</TableHead>
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
                        ? "Нет событий по заданным фильтрам"
                        : "Журнал событий пуст"}
                    </p>
                    {hasAnyFilter && (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={resetAllFilters}
                      >
                        Сбросить фильтры
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ) : (
              items.map((e) => (
                <TableRow
                  key={e.id}
                  className={`cursor-pointer hover:bg-muted/50 ${SEVERITY_ROW_STYLES[e.severity] ?? ""}`}
                  onClick={() => handleRowClick(e)}
                >
                  <TableCell className="font-mono text-xs">
                    {format(new Date(e.createdAt), "dd.MM.yy HH:mm:ss", { locale: ru })}
                  </TableCell>
                  <TableCell>
                    <SeverityBadge severity={e.severity} />
                  </TableCell>
                  <TableCell>
                    <CategoryBadge category={e.category} />
                  </TableCell>
                  <TableCell className="font-mono text-xs">{e.eventType}</TableCell>
                  <TableCell className="max-w-[300px] truncate text-sm">
                    {e.message}
                  </TableCell>
                  <TableCell className="text-xs truncate">
                    {e.userEmail || e.userId || "—"}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {e.durationMs != null ? `${e.durationMs}ms` : "—"}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
        {/* Infinite scroll sentinel */}
        <ScrollSentinel
          onIntersect={loadMore}
          loading={loadingMore}
          enabled={hasMore && !loading}
          scrollContainer={scrollContainerRef}
        />
      </ScrollArea>

      {/* Dialogs */}
      <DetailDialog
        event={selectedEvent}
        open={detailOpen}
        onOpenChange={setDetailOpen}
        onOpenTrace={handleOpenTrace}
      />
      <TraceDialog traceId={traceId} open={traceOpen} onOpenChange={setTraceOpen} />
    </div>
  )
}

export default function EventLogPage() {
  return (
    <PermissionGate permission={EVENT_LOG_READ} subject="журналу событий">
      <EventLogPageContent />
    </PermissionGate>
  )
}
