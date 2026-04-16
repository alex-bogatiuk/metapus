"use client"

import { useEffect, useState, useMemo, useCallback, useRef } from "react"
import { usePathname, useSearchParams } from "next/navigation"
import { useListSelection } from "@/hooks/useListSelection"
import { useUrlSort } from "@/hooks/useUrlSort"
import { useEntityFiltersMeta } from "@/hooks/useEntityFiltersMeta"
import { useTabState, useHasTabCache } from "@/hooks/useTabState"
import { useTabStateStore } from "@/stores/useTabStateStore"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { buildFilterItems, type FilterValues } from "@/lib/filter-utils"
import type { FilterFieldMeta } from "@/components/shared/filter-config-dialog"
import type { AdvancedFilterItem } from "@/types/common"

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
  /** Entity key for metadata & prefs (e.g. "GoodsReceipt", "nomenclature"). */
  entityKey: string
  /** API object with `list` method. */
  api: EntityListApi<T>
  /** Period field key for filter sidebar (e.g. "date"). Omit for catalogs without date filter. */
  periodField?: string
  /** Default limit for list queries. */
  limit?: number
}

interface UseEntityListPageReturn<T extends { id: string }> {
  items: T[]
  loading: boolean
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
  isPrefsLoaded: boolean
  initialFilterValues: FilterValues
  handleFilterValuesChange: (values: FilterValues) => void
  /** Current resolved advanced filter items (for filter-based batch operations). */
  currentFilters: AdvancedFilterItem[]
  // Show deleted
  showDeleted: boolean
  toggleShowDeleted: () => void
  // Focus
  focusedId: string | null
  setFocusedId: (id: string | null) => void
  // Around / teleportation
  targetIndex: number | null
  // In-place updates (avoids full list refresh)
  /** Replace matching items by ID in the current list (preserves scroll & focus). */
  replaceItems: (updated: T[]) => void
}

// ── Hook ────────────────────────────────────────────────────────────────

/**
 * Universal hook for entity list pages (catalogs AND documents).
 *
 * Encapsulates:
 *  - Data fetching with metadata-driven advanced filters
 *  - User prefs persistence (filter values, show deleted)
 *  - Cursor-based pagination (infinite scroll)
 *  - Selection, sorting, focus state
 *  - Filter metadata from backend
 *
 * Usage:
 * ```tsx
 * // Documents
 * const list = useEntityListPage({ entityKey: "GoodsReceipt", api: api.goodsReceipts, periodField: "date" })
 *
 * // Catalogs (no period field)
 * const list = useEntityListPage({ entityKey: "nomenclature", api: api.nomenclature })
 * ```
 */
export function useEntityListPage<T extends { id: string }>(
  options: UseEntityListPageOptions<T>,
): UseEntityListPageReturn<T> {
  const { entityKey, api: entityApi, periodField, limit = 100 } = options

  // ── URL search params (for ?around= teleportation) ─────────────────
  const pathname = usePathname()
  const searchParams = useSearchParams()
  const aroundId = searchParams.get("around")

  // ── Sorting (read early — orderBy needed by fetch callbacks) ───────
  const { sortColumn, sortDirection, handleSort, orderBy } = useUrlSort()

  // ── Filter metadata from backend ─────────────────────────────────────
  const { fieldsMeta } = useEntityFiltersMeta(entityKey)

  // ── Tab-cached state (persists across tab switches) ────────────────
  const [items, setItems] = useTabState<T[]>("items", [])
  const [hasMore, setHasMore] = useTabState("hasMore", false)
  const [hasPrev, setHasPrev] = useTabState("hasPrev", false)
  const [totalCount, setTotalCount] = useTabState("totalCount", 0)
  const [focusedId, setFocusedId] = useTabState<string | null>("focusedId", null)

  // Check if we have cached data (for skipping initial fetch)
  const hasCachedItems = useHasTabCache("items")

  // ── Transient state (not cached — derived from cache hit) ──────────
  const [loading, setLoading] = useState(!hasCachedItems)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [targetIndex, setTargetIndex] = useState<number | null>(null)

  // ── Refs — initialize from tab cache if available ──────────────────
  const getTabCache = (key: string) => useTabStateStore.getState().get(pathname, key)
  const setTabCache = (key: string, value: unknown) => useTabStateStore.getState().set(pathname, key, value)

  const nextCursorRef = useRef<string | undefined>(getTabCache("nextCursor") as string | undefined)
  const prevCursorRef = useRef<string | undefined>(getTabCache("prevCursor") as string | undefined)
  const busyRef = useRef(false)
  const orderByRef = useRef<string | undefined>(orderBy)
  const filterValuesRef = useRef<FilterValues>(
    (getTabCache("filterValues") as FilterValues) ?? {},
  )

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
  const fetchData = useCallback(
    async (filterValues?: FilterValues, opts?: { skipCount?: boolean }) => {
      setLoading(true)
      setError(null)

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
        const res = await entityApi.list({
          limit,
          orderBy: orderByRef.current,
          filter: advancedFilters.length > 0 ? advancedFilters : undefined,
          includeDeleted: showDeletedRef.current || undefined,
          skipCount: skipCount || undefined,
        })
        setItems(res.items ?? [])
        setHasMore(res.hasMore ?? false)
        setHasPrev(res.hasPrev ?? false)
        // Only update totalCount when backend actually returned it
        if (res.totalCount != null) {
          setTotalCount(res.totalCount)
        }
        setTargetIndex(null)
        nextCursorRef.current = res.nextCursor
        prevCursorRef.current = res.prevCursor
        setTabCache("nextCursor", res.nextCursor)
        setTabCache("prevCursor", res.prevCursor)
      } catch (err) {
        setError(err instanceof Error ? err.message : "Ошибка загрузки данных")
      } finally {
        setLoading(false)
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [entityApi, fieldsMeta, periodField, limit, computeFingerprint, setItems, setHasMore, setHasPrev, setTotalCount],
  )

  // ── Init (wait for prefs) ────────────────────────────────────────────
  // If we have cached data from a previous tab visit, skip the initial fetch
  const initialized = useRef(hasCachedItems)

  useEffect(() => {
    if (isPrefsLoaded && !initialized.current) {
      initialized.current = true
      const initial = initialListFilters ?? {}
      filterValuesRef.current = initial
      setTabCache("filterValues", initial)

      if (aroundId) {
        // Teleport: fetch items around the target ID
        setLoading(true)
        setError(null)
        entityApi.list({
          limit,
          orderBy: orderByRef.current,
          includeDeleted: showDeletedRef.current || undefined,
          around: aroundId,
        }).then((res) => {
          setItems(res.items ?? [])
          setHasMore(res.hasMore ?? false)
          setHasPrev(res.hasPrev ?? false)
          if (res.totalCount != null) {
            setTotalCount(res.totalCount)
          }
          setTargetIndex(res.targetIndex ?? null)
          nextCursorRef.current = res.nextCursor
          prevCursorRef.current = res.prevCursor
          setTabCache("nextCursor", res.nextCursor)
          setTabCache("prevCursor", res.prevCursor)
          setFocusedId(aroundId)
        }).catch((err) => {
          setError(err instanceof Error ? err.message : "Ошибка загрузки данных")
        }).finally(() => {
          setLoading(false)
        })
      } else {
        fetchData(initial)
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isPrefsLoaded, fetchData])

  // ── Filter change handler ────────────────────────────────────────────
  const handleFilterValuesChange = useCallback(
    (values: FilterValues) => {
      filterValuesRef.current = values
      setTabCache("filterValues", values)
      setListFilters(entityKey, values)
      fetchData(values)
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [fetchData, setListFilters, entityKey],
  )

  // ── Refresh ──────────────────────────────────────────────────────────
  const refresh = useCallback(() => {
    fetchData(filterValuesRef.current)
  }, [fetchData])

  // ── Toggle show deleted ──────────────────────────────────────────────
  const toggleShowDeleted = useCallback(() => {
    const current = useUserPrefsStore.getState().interface.showDeletedEntities ?? {}
    updateInterface({ showDeletedEntities: { ...current, [entityKey]: !showDeleted } })
    setTimeout(() => fetchData(filterValuesRef.current), 0)
  }, [entityKey, showDeleted, updateInterface, fetchData])

  // ── Load more (forward cursor) ──────────────────────────────────────
  const loadMore = useCallback(async () => {
    if (!nextCursorRef.current || busyRef.current) return
    busyRef.current = true
    setLoadingMore(true)
    try {
      const advancedFilters = filterValuesRef.current
        ? buildFilterItems(filterValuesRef.current, fieldsMeta, periodField)
        : []
      const res = await entityApi.list({
        limit,
        orderBy: orderByRef.current,
        filter: advancedFilters.length > 0 ? advancedFilters : undefined,
        includeDeleted: showDeletedRef.current || undefined,
        after: nextCursorRef.current,
      })
      setItems((prev) => {
        const existingIds = new Set(prev.map((i) => i.id))
        const newItems = (res.items ?? []).filter((i) => !existingIds.has(i.id))
        return [...prev, ...newItems]
      })
      setHasMore(res.hasMore ?? false)
      nextCursorRef.current = res.nextCursor
      setTabCache("nextCursor", res.nextCursor)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка загрузки данных")
    } finally {
      busyRef.current = false
      setLoadingMore(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entityApi, fieldsMeta, periodField, limit, setItems, setHasMore])

  // ── Load prev (backward cursor) ─────────────────────────────────────
  const loadPrev = useCallback(async () => {
    if (!prevCursorRef.current || busyRef.current) return
    busyRef.current = true
    setLoadingMore(true)
    try {
      const advancedFilters = filterValuesRef.current
        ? buildFilterItems(filterValuesRef.current, fieldsMeta, periodField)
        : []
      const res = await entityApi.list({
        limit,
        orderBy: orderByRef.current,
        filter: advancedFilters.length > 0 ? advancedFilters : undefined,
        includeDeleted: showDeletedRef.current || undefined,
        before: prevCursorRef.current,
      })
      setItems((prev) => {
        const existingIds = new Set(prev.map((i) => i.id))
        const newItems = (res.items ?? []).filter((i) => !existingIds.has(i.id))
        return [...newItems, ...prev]
      })
      setHasPrev(res.hasPrev ?? false)
      prevCursorRef.current = res.prevCursor
      setTabCache("prevCursor", res.prevCursor)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка загрузки данных")
    } finally {
      busyRef.current = false
      setLoadingMore(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entityApi, fieldsMeta, periodField, limit, setItems, setHasPrev])

  // ── Re-fetch on sort change (reset cursors — they encode sort order) ─
  const prevOrderByRef = useRef(orderBy)
  useEffect(() => {
    if (prevOrderByRef.current === orderBy) return
    prevOrderByRef.current = orderBy
    orderByRef.current = orderBy
    // Cursors are invalid after sort change — reset to first page
    nextCursorRef.current = undefined
    prevCursorRef.current = undefined
    setTabCache("nextCursor", undefined)
    setTabCache("prevCursor", undefined)
    // Sort change doesn't affect total count → hint to skip COUNT
    fetchData(filterValuesRef.current, { skipCount: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [orderBy, fetchData])

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
    () => buildFilterItems(filterValuesRef.current, fieldsMeta, periodField),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [items, fieldsMeta, periodField],
  )

  return {
    items,
    loading,
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
    isPrefsLoaded,
    initialFilterValues: initialListFilters ?? {},
    handleFilterValuesChange,
    currentFilters,
    showDeleted,
    toggleShowDeleted,
    focusedId,
    setFocusedId,
    targetIndex,
    replaceItems,
  }
}
