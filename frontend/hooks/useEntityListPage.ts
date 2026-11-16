"use client"

import type { FilterFieldMeta } from "@/components/shared/filter-config-dialog"
import { useDebouncedValue } from "@/hooks/useDebouncedValue"
import { useEntityFiltersMeta } from "@/hooks/useEntityFiltersMeta"
import { useLatestRequest } from "@/hooks/useLatestRequest"
import { useListSelection } from "@/hooks/useListSelection"
import { useHasTabCache, useTabState } from "@/hooks/useTabState"
import { useUrlSort } from "@/hooks/useUrlSort"
import { buildFilterItems, type FilterValues } from "@/lib/filter-utils"
import { ApiError } from "@/lib/api"
import { createTabStateKey, useTabStateStore, type TabStateKey } from "@/stores/useTabStateStore"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import type { AdvancedFilterItem } from "@/types/common"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { toast } from "sonner"

const tabStateKeys = {
  hasMore: createTabStateKey<boolean>("entityList.hasMore"),
  hasPrev: createTabStateKey<boolean>("entityList.hasPrev"),
  totalCount: createTabStateKey<number>("entityList.totalCount"),
  focusedId: createTabStateKey<string | null>("entityList.focusedId"),
  focusedColumnKey: createTabStateKey<string | null>("entityList.focusedColumnKey"),
  searchQuery: createTabStateKey<string>("entityList.searchQuery"),
}
interface ListCacheFields { nextCursor: string | undefined; prevCursor: string | undefined; filterValues: FilterValues }
const cacheKeys: { [K in keyof ListCacheFields]: TabStateKey<ListCacheFields[K]> } = {
  nextCursor: createTabStateKey("entityList.nextCursor"),
  prevCursor: createTabStateKey("entityList.prevCursor"),
  filterValues: createTabStateKey("entityList.filterValues"),
}

// ── Types ───────────────────────────────────────────────────────────────

interface ListResponse<T> {
  items: T[]
  totalCount?: number | null
  nextCursor?: string
  prevCursor?: string
  hasMore?: boolean
  hasPrev?: boolean
  targetIndex?: number
}

interface EntityListApi<T> {
  list: (params?: {
    limit?: number
    search?: string
    orderBy?: string
    filter?: AdvancedFilterItem[]
    includeDeleted?: boolean
    after?: string
    before?: string
    around?: string
    skipCount?: boolean
  }) => Promise<ListResponse<T>>
}

interface UseEntityListPageOptions<T extends { id: string }> {
  rowsState: TabStateKey<T[]>
  /** Entity key for metadata & prefs (e.g. "CryptoInvoice", "token"). */
  entityKey: string
  /** API object with `list` method. */
  api: EntityListApi<T>
  /** Period field key for filter sidebar (e.g. "date"). Omit for catalogs without date filter. */
  periodField?: string
  /** Default limit for list queries. */
  limit?: number
  /** Derived filter fields translated by the caller's API fetcher. */
  additionalFilterFields?: readonly FilterFieldMeta[]
}

interface UseEntityListPageReturn<T extends { id: string }> {
  items: T[]
  loading: boolean
  hasCachedItems: boolean
  refreshing: boolean
  loadingMore: boolean
  error: string | null
  refresh: () => void
  // Cursor pagination
  hasMore: boolean
  hasPrev: boolean
  totalCount: number
  loadMore: () => void
  loadPrev: () => void
  // Selection
  selectedIds: string[]
  isAllSelected: boolean
  isIndeterminate: boolean
  toggleItem: (id: string, shiftKey: boolean) => void
  toggleAll: () => void
  // Virtual select-all (Gmail-style)
  selectAllByFilter: boolean
  excludedIds: string[]
  activateSelectAll: () => void
  clearSelection: () => void
  // Sorting
  sortColumn: string | null
  sortDirection: "asc" | "desc"
  handleSort: (column: string) => void
  // Filters
  fieldsMeta: FilterFieldMeta[]
  fieldsMetaLoading: boolean
  isPrefsLoaded: boolean
  initialFilterValues: FilterValues
  handleFilterValuesChange: (values: FilterValues) => void
  /** Current resolved advanced filter items (for filter-based batch operations). */
  currentFilters: AdvancedFilterItem[]
  // Show deleted
  showDeleted: boolean
  toggleShowDeleted: () => void
  // Current row — independent of checkbox multi-select (1C CurrentRow).
  focusedId: string | null
  setFocusedId: (id: string | null) => void
  /** Click a row to make it current. Does not change checkbox selection. */
  handleRowClick: (item: T) => void
  /** Current list column (the last clicked cell). Used by Alt+F column filter. */
  focusedColumnKey: string | null
  setFocusedColumnKey: (key: string | null) => void
  handleCellClick: (item: T, columnKey: string) => void
  // Around / teleportation
  targetIndex: number | null
  /** One-shot request to scroll to and focus a freshly loaded target row. */
  revealTarget: { id: string } | null
  // In-place updates (avoids full list refresh)
  /** Replace matching items by ID in the current list (preserves scroll & focus). */
  replaceItems: (updated: T[]) => void
  // Search
  /** Current search text (instant, not debounced). */
  searchQuery: string
  /** Update search text — triggers debounced refetch. */
  setSearchQuery: (q: string) => void
}

// ── Hook ────────────────────────────────────────────────────────────────

/**
 * Universal hook for entity list pages (catalogs AND documents).
 *
 * Encapsulates:
 *  - Data fetching with metadata-driven advanced filters
 *  - User prefs persistence (filter values, show deleted)
 *  - Cursor-based pagination (infinite scroll)
 *  - Selection, sorting, current-row/column focus (click → focusedId + focusedColumnKey)
 *  - Filter metadata from backend
 *
 * Usage:
 * ```tsx
 * // Documents
 * const list = useEntityListPage({ entityKey: "CryptoInvoice", api: api.cryptoInvoices, periodField: "date" })
 *
 * // Catalogs (no period field)
 * const list = useEntityListPage({ entityKey: "counterparty", api: api.counterparties })
 * ```
 */
export function useEntityListPage<T extends { id: string }>(
  options: UseEntityListPageOptions<T>,
): UseEntityListPageReturn<T> {
  const { entityKey, api: entityApi, periodField, limit = 100 } = options
  const listEntities = entityApi.list
  const requests = useLatestRequest()

  // ── URL search params (for ?around= teleportation) ─────────────────
  const pathname = usePathname()
  const router = useRouter()
  const searchParams = useSearchParams()
  const aroundId = searchParams.get("around")

  // ── Sorting (read early — orderBy needed by fetch callbacks) ───────
  const { sortColumn, sortDirection, handleSort, orderBy } = useUrlSort()

  // ── Filter metadata from backend ─────────────────────────────────────
  const { fieldsMeta: storedFieldsMeta, loading: fieldsMetaLoading } = useEntityFiltersMeta(entityKey)
  const fieldsMeta = useMemo(() => options.additionalFilterFields?.length
    ? [...storedFieldsMeta, ...options.additionalFilterFields]
    : storedFieldsMeta, [storedFieldsMeta, options.additionalFilterFields])

  // ── Tab-cached state (persists across tab switches) ────────────────
  const [items, setItems] = useTabState(options.rowsState, [])
  const [hasMore, setHasMore] = useTabState(tabStateKeys.hasMore, false)
  const [hasPrev, setHasPrev] = useTabState(tabStateKeys.hasPrev, false)
  const [totalCount, setTotalCount] = useTabState(tabStateKeys.totalCount, 0)
  const [focusedId, setFocusedId] = useTabState(tabStateKeys.focusedId, null)
  const [focusedColumnKey, setFocusedColumnKey] = useTabState(tabStateKeys.focusedColumnKey, null)

  const handleRowClick = useCallback((item: T) => {
    setFocusedId(item.id)
  }, [setFocusedId])

  const handleCellClick = useCallback((item: T, columnKey: string) => {
    setFocusedId(item.id)
    setFocusedColumnKey(columnKey)
  }, [setFocusedId, setFocusedColumnKey])

  // Check if we have cached data (for skipping initial fetch)
  const hasCachedItems = useHasTabCache(options.rowsState)

  // ── Transient state (not cached — derived from cache hit) ──────────
  const [loading, setLoading] = useState(!hasCachedItems)
  const [refreshing, setRefreshing] = useState(false)
  const [hasLoadedItems, setHasLoadedItems] = useState(hasCachedItems)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [targetIndex, setTargetIndex] = useState<number | null>(null)
  const [revealTarget, setRevealTarget] = useState<{ id: string } | null>(null)
  const hasListContentRef = useRef(hasCachedItems)

  // ── Search (M5) ─────────────────────────────────────────────────────
  const [searchQuery, setSearchQuery] = useTabState(tabStateKeys.searchQuery, "")
  const debouncedSearch = useDebouncedValue(searchQuery, 300)
  const searchRef = useRef(debouncedSearch)
  searchRef.current = debouncedSearch

  // ── Refs — initialize from tab cache if available ──────────────────
  const getTabCache = <K extends keyof ListCacheFields>(key: K) => useTabStateStore.getState().get(pathname, cacheKeys[key])
  const setTabCache = useCallback(<K extends keyof ListCacheFields>(key: K, value: ListCacheFields[K]) => {
    useTabStateStore.getState().set(pathname, cacheKeys[key], value)
  }, [pathname])

  const nextCursorRef = useRef(getTabCache("nextCursor"))
  const prevCursorRef = useRef(getTabCache("prevCursor"))
  const busyRef = useRef(false)
  const orderByRef = useRef<string | undefined>(orderBy)
  const [filterValues, setFilterValues] = useTabState(cacheKeys.filterValues, {})
  const filterValuesRef = useRef<FilterValues>(filterValues)

  // ── User prefs ───────────────────────────────────────────────────────
  const isPrefsLoaded = useUserPrefsStore((s) => s.isLoaded)
  const initialListFilters = useUserPrefsStore((s) => s.listFilters[entityKey])
  const setListFilters = useUserPrefsStore((s) => s.setListFilters)
  const showDeleted = useUserPrefsStore(
    (s) => s.interface.showDeletedEntities?.[entityKey] ?? false,
  )
  const updateInterface = useUserPrefsStore((s) => s.updateInterface)
  const showDeletedRef = useRef(showDeleted)
  showDeletedRef.current = showDeleted

  // ── Filter fingerprint (for skipCount optimization) ──────────────────
  // Tracks a deterministic hash of {filterValues, showDeleted}.
  // When only sort changes, fingerprint stays the same → skipCount=true.
  const filterFingerprintRef = useRef<string>("")

  const computeFingerprint = useCallback((filterValues: FilterValues, deleted: boolean): string => {
    const entries = Object.entries(filterValues)
      .filter(([, v]) => v !== undefined && v !== null)
      .sort(([a], [b]) => a.localeCompare(b))
    return JSON.stringify([entries, deleted])
  }, [])

  // ── Fetch ────────────────────────────────────────────────────────────
  const beginPrimaryLoad = useCallback(() => {
    setLoadingMore(false)
    setError(null)
    if (hasListContentRef.current) {
      setLoading(false)
      setRefreshing(true)
    } else {
      setRefreshing(false)
      setLoading(true)
    }
  }, [])

  const finishPrimaryLoad = useCallback(() => {
    setLoading(false)
    setRefreshing(false)
  }, [])

  const markItemsLoaded = useCallback(() => {
    hasListContentRef.current = true
    setHasLoadedItems(true)
  }, [])

  const fetchData = useCallback(
    async (filterValues?: FilterValues, opts?: { skipCount?: boolean; around?: string }) => {
      const isCurrent = requests.begin()
      beginPrimaryLoad()
      // Pagination must not reuse the previous window while a new query is loading.
      nextCursorRef.current = undefined
      prevCursorRef.current = undefined
      setTabCache("nextCursor", undefined)
      setTabCache("prevCursor", undefined)

      // Compute fingerprint and decide whether to skip COUNT
      const currentFP = computeFingerprint(filterValues ?? {}, showDeletedRef.current)
      const shouldSkipCount = opts?.skipCount ?? false
      // If fingerprint changed → must recount. Otherwise respect caller's hint.
      const fpChanged = currentFP !== filterFingerprintRef.current
      filterFingerprintRef.current = currentFP
      const skipCount = fpChanged ? false : shouldSkipCount

      try {
        const advancedFilters = filterValues
          ? buildFilterItems(filterValues, fieldsMeta, periodField)
          : []
        const params = {
          limit,
          orderBy: orderByRef.current,
          filter: advancedFilters.length > 0 ? advancedFilters : undefined,
          includeDeleted: showDeletedRef.current || undefined,
          skipCount: skipCount || undefined,
          search: searchRef.current || undefined,
        }
        let res: ListResponse<T>
        let targetFound = false
        try {
          res = await listEntities({ ...params, around: opts?.around })
          targetFound = !!opts?.around && res.items.some((item) => item.id === opts.around)
        } catch (err) {
          if (!isCurrent()) return
          if (!opts?.around || !(err instanceof ApiError) || err.status !== 404) throw err
          res = await listEntities(params)
        }
        if (!isCurrent()) return
        if (opts?.around && !targetFound) {
          toast.info("Не удалось показать запись в текущем списке. Возможно, она скрыта фильтрами или недоступна.")
        }
        setItems(res.items ?? [])
        setHasMore(res.hasMore ?? false)
        setHasPrev(res.hasPrev ?? false)
        // Only update totalCount when backend actually returned it
        if (res.totalCount != null) {
          setTotalCount(res.totalCount)
        }
        setTargetIndex(targetFound ? res.targetIndex ?? null : null)
        setRevealTarget(targetFound && opts?.around ? { id: opts.around } : null)
        if (opts?.around) setFocusedId(targetFound ? opts.around : null)
        nextCursorRef.current = res.nextCursor
        prevCursorRef.current = res.prevCursor
        setTabCache("nextCursor", res.nextCursor)
        setTabCache("prevCursor", res.prevCursor)
        markItemsLoaded()
        if (opts?.around) {
          // Consume only this command; preserve sort, other params and any newer navigation.
          const url = new URL(window.location.href)
          if (url.pathname === pathname && url.searchParams.get("around") === opts.around) {
            url.searchParams.delete("around")
            router.replace(`${url.pathname}${url.search}${url.hash}`, { scroll: false })
          }
          // Around windows may omit the count. Refresh it without delaying row reveal.
          if (targetFound && res.totalCount == null) {
            void listEntities({ ...params, limit: 1, skipCount: undefined }).then((count) => {
              if (isCurrent() && count.totalCount != null) setTotalCount(count.totalCount)
            }).catch(() => { /* A count failure must not hide successfully loaded rows. */ })
          }
        }
      } catch (err) {
        if (isCurrent()) {
          setError(err instanceof Error ? err.message : "Не удалось загрузить список. Проверьте соединение или обновите страницу.")
          if (opts?.around && hasListContentRef.current) toast.error("Не удалось показать запись. Обновите список.")
        }
      } finally {
        if (isCurrent()) finishPrimaryLoad()
      }
    },

    [listEntities, fieldsMeta, periodField, limit, computeFingerprint, setItems, setHasMore, setHasPrev, setTotalCount, setFocusedId, beginPrimaryLoad, finishPrimaryLoad, markItemsLoaded, setTabCache, requests, pathname, router],
  )

  // One loader handles initial, around, search and sort requests, including cached tabs.
  const initialized = useRef(hasCachedItems)
  const handledAround = useRef<string | null>(null)
  const previousQuery = useRef({ orderBy, search: debouncedSearch })

  useEffect(() => {
    if (!isPrefsLoaded || fieldsMetaLoading) return
    const previous = previousQuery.current
    const queryChanged = previous.orderBy !== orderBy || previous.search !== debouncedSearch
    previousQuery.current = { orderBy, search: debouncedSearch }
    orderByRef.current = orderBy
    const target = aroundId && handledAround.current !== aroundId ? aroundId : undefined
    if (!aroundId) handledAround.current = null
    if (initialized.current && !queryChanged && !target) return

    const values = initialized.current ? filterValuesRef.current : initialListFilters ?? {}
    if (!initialized.current) {
      filterValuesRef.current = values
      setFilterValues(values)
    }
    initialized.current = true
    if (target) handledAround.current = target
    requests.invalidate()
    let cancelled = false
    // Let effect cleanup cancel a StrictMode replay before sending its request.
    queueMicrotask(() => {
      if (!cancelled) void fetchData(values, {
        around: target,
        skipCount: !target && queryChanged && previous.search === debouncedSearch,
      })
    })
    return () => {
      cancelled = true
      // A StrictMode replay must restart an invalidated request, even with cached rows.
      if (target) handledAround.current = null
      if (!hasListContentRef.current) initialized.current = false
    }
  }, [isPrefsLoaded, fieldsMetaLoading, aroundId, orderBy, debouncedSearch, initialListFilters, fetchData, setFilterValues, requests])

  // ── Filter change handler ────────────────────────────────────────────
  const handleFilterValuesChange = useCallback(
    (values: FilterValues) => {
      filterValuesRef.current = values
      setFilterValues(values)
      setListFilters(entityKey, values)
      fetchData(values)
    },

    [fetchData, setListFilters, entityKey, setFilterValues],
  )

  // ── Refresh ──────────────────────────────────────────────────────────
  const refresh = useCallback(() => {
    void fetchData(filterValuesRef.current, { around: aroundId ?? undefined })
  }, [fetchData, aroundId])

  // ── Toggle show deleted ──────────────────────────────────────────────
  const toggleShowDeleted = useCallback(() => {
    const current = useUserPrefsStore.getState().interface.showDeletedEntities ?? {}
    updateInterface({ showDeletedEntities: { ...current, [entityKey]: !showDeleted } })
    setTimeout(() => fetchData(filterValuesRef.current), 0)
  }, [entityKey, showDeleted, updateInterface, fetchData])

  // ── Load more (forward cursor) ──────────────────────────────────────
  const loadMore = useCallback(async () => {
    if (!nextCursorRef.current || busyRef.current) return
    const isCurrent = requests.capture()
    busyRef.current = true
    setLoadingMore(true)
    try {
      const advancedFilters = filterValuesRef.current
        ? buildFilterItems(filterValuesRef.current, fieldsMeta, periodField)
        : []
      const res = await listEntities({
        limit,
        orderBy: orderByRef.current,
        filter: advancedFilters.length > 0 ? advancedFilters : undefined,
        includeDeleted: showDeletedRef.current || undefined,
        after: nextCursorRef.current,
        search: searchRef.current || undefined,
      })
      if (!isCurrent()) return
      setItems((prev) => {
        const existingIds = new Set(prev.map((i) => i.id))
        const newItems = (res.items ?? []).filter((i) => !existingIds.has(i.id))
        return [...prev, ...newItems]
      })
      setHasMore(res.hasMore ?? false)
      nextCursorRef.current = res.nextCursor
      setTabCache("nextCursor", res.nextCursor)
    } catch (err) {
      if (isCurrent()) setError(err instanceof Error ? err.message : "Не удалось загрузить список. Проверьте соединение или обновите страницу.")
    } finally {
      busyRef.current = false
      if (isCurrent()) setLoadingMore(false)
    }

  }, [listEntities, fieldsMeta, periodField, limit, setItems, setHasMore, setTabCache, requests])

  // ── Load prev (backward cursor) ─────────────────────────────────────
  const loadPrev = useCallback(async () => {
    if (!prevCursorRef.current || busyRef.current) return
    const isCurrent = requests.capture()
    busyRef.current = true
    setLoadingMore(true)
    try {
      const advancedFilters = filterValuesRef.current
        ? buildFilterItems(filterValuesRef.current, fieldsMeta, periodField)
        : []
      const res = await listEntities({
        limit,
        orderBy: orderByRef.current,
        filter: advancedFilters.length > 0 ? advancedFilters : undefined,
        includeDeleted: showDeletedRef.current || undefined,
        before: prevCursorRef.current,
        search: searchRef.current || undefined,
      })
      if (!isCurrent()) return
      setItems((prev) => {
        const existingIds = new Set(prev.map((i) => i.id))
        const newItems = (res.items ?? []).filter((i) => !existingIds.has(i.id))
        return [...newItems, ...prev]
      })
      setHasPrev(res.hasPrev ?? false)
      prevCursorRef.current = res.prevCursor
      setTabCache("prevCursor", res.prevCursor)
    } catch (err) {
      if (isCurrent()) setError(err instanceof Error ? err.message : "Не удалось загрузить список. Проверьте соединение или обновите страницу.")
    } finally {
      busyRef.current = false
      if (isCurrent()) setLoadingMore(false)
    }

  }, [listEntities, fieldsMeta, periodField, limit, setItems, setHasPrev, setTabCache, requests])

  // ── Selection ────────────────────────────────────────────────────────
  const visibleIds = useMemo(() => items.map((d) => d.id), [items])
  const {
    selectedIds, isAllSelected, isIndeterminate, toggleItem, toggleAll,
    selectAllByFilter, excludedIds, activateSelectAll, clearSelection,
  } = useListSelection(visibleIds)

  // ── In-place item replacement (no scroll reset) ───────────────────
  const replaceItems = useCallback(
    (updated: T[]) => {
      if (updated.length === 0) return
      const map = new Map(updated.map((u) => [u.id, u]))
      setItems((prev) => prev.map((item) => map.get(item.id) ?? item))
    },
    [setItems],
  )

  // ── Current filters (for filter-based batch operations) ─────────────
  const currentFilters = useMemo(
    () => buildFilterItems(filterValues, fieldsMeta, periodField),

    [filterValues, fieldsMeta, periodField],
  )

  return {
    items,
    loading,
    hasCachedItems: hasLoadedItems,
    refreshing,
    loadingMore,
    error,
    refresh,
    hasMore,
    hasPrev,
    totalCount,
    loadMore,
    loadPrev,
    selectedIds,
    isAllSelected,
    isIndeterminate,
    toggleItem,
    toggleAll,
    selectAllByFilter,
    excludedIds,
    activateSelectAll,
    clearSelection,
    sortColumn,
    sortDirection,
    handleSort,
    fieldsMeta,
    fieldsMetaLoading,
    isPrefsLoaded,
    initialFilterValues: initialListFilters ?? {},
    handleFilterValuesChange,
    currentFilters,
    showDeleted,
    toggleShowDeleted,
    focusedId,
    setFocusedId,
    handleRowClick,
    focusedColumnKey,
    setFocusedColumnKey,
    handleCellClick,
    targetIndex,
    revealTarget,
    replaceItems,
    searchQuery,
    setSearchQuery,
  }
}
