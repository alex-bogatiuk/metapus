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
  totalCount?: number
  nextCursor?: string
  prevCursor?: string
  hasMore?: boolean
  hasPrev?: boolean
  targetIndex?: number
}

interface DocumentListApi<T> {
  list: (params?: {
    limit?: number
    orderBy?: string
    filter?: AdvancedFilterItem[]
    includeDeleted?: boolean
    after?: string
    before?: string
    around?: string
  }) => Promise<ListResponse<T>>
}

interface UseDocumentListPageOptions<T extends { id: string }> {
  /** Entity key for metadata & prefs (e.g. "GoodsReceipt"). */
  entityKey: string
  /** API object with `list` method. */
  api: DocumentListApi<T>
  /** Period field key for filter sidebar (e.g. "date"). */
  periodField?: string
  /** Default limit for list queries. */
  limit?: number
}

interface UseDocumentListPageReturn<T extends { id: string }> {
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
  // Sorting
  sortColumn: string | null
  sortDirection: "asc" | "desc"
  handleSort: (column: string) => void
  // Filters
  fieldsMeta: FilterFieldMeta[]
  isPrefsLoaded: boolean
  initialFilterValues: FilterValues
  handleFilterValuesChange: (values: FilterValues) => void
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
 * Generic hook for document list pages.
 *
 * Encapsulates:
 *  - Data fetching with advanced filters
 *  - User prefs persistence (filter values, show deleted)
 *  - Selection, sorting, focus state
 *  - Filter metadata from backend
 *
 * Usage:
 * ```tsx
 * const list = useDocumentListPage({ entityKey: "GoodsReceipt", api: api.goodsReceipts })
 * ```
 */
export function useDocumentListPage<T extends { id: string }>(
  options: UseDocumentListPageOptions<T>,
): UseDocumentListPageReturn<T> {
  const { entityKey, api: docApi, periodField = "date", limit = 100 } = options

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

  // ── Fetch ────────────────────────────────────────────────────────────
  const fetchData = useCallback(
    async (filterValues?: FilterValues) => {
      setLoading(true)
      setError(null)
      try {
        const advancedFilters = filterValues
          ? buildFilterItems(filterValues, fieldsMeta, periodField)
          : []
        const res = await docApi.list({
          limit,
          orderBy: orderByRef.current,
          filter: advancedFilters.length > 0 ? advancedFilters : undefined,
          includeDeleted: showDeletedRef.current || undefined,
        })
        setItems(res.items ?? [])
        setHasMore(res.hasMore ?? false)
        setHasPrev(res.hasPrev ?? false)
        setTotalCount(res.totalCount ?? 0)
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
    [docApi, fieldsMeta, periodField, limit, setItems, setHasMore, setHasPrev, setTotalCount],
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
        docApi.list({
          limit,
          orderBy: orderByRef.current,
          includeDeleted: showDeletedRef.current || undefined,
          around: aroundId,
        }).then((res) => {
          setItems(res.items ?? [])
          setHasMore(res.hasMore ?? false)
          setHasPrev(res.hasPrev ?? false)
          setTotalCount(res.totalCount ?? 0)
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
      const res = await docApi.list({
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
  }, [docApi, fieldsMeta, periodField, limit, setItems, setHasMore])

  // ── Load prev (backward cursor) ─────────────────────────────────────
  const loadPrev = useCallback(async () => {
    if (!prevCursorRef.current || busyRef.current) return
    busyRef.current = true
    setLoadingMore(true)
    try {
      const advancedFilters = filterValuesRef.current
        ? buildFilterItems(filterValuesRef.current, fieldsMeta, periodField)
        : []
      const res = await docApi.list({
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
  }, [docApi, fieldsMeta, periodField, limit, setItems, setHasPrev])

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
    fetchData(filterValuesRef.current)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [orderBy, fetchData])

  // ── Selection ────────────────────────────────────────────────────────
  const visibleIds = useMemo(() => items.map((d) => d.id), [items])
  const { selectedIds, isAllSelected, isIndeterminate, toggleItem, toggleAll } =
    useListSelection(visibleIds)

  // ── In-place item replacement (no scroll reset) ───────────────────
  const replaceItems = useCallback(
    (updated: T[]) => {
      if (updated.length === 0) return
      const map = new Map(updated.map((u) => [u.id, u]))
      setItems((prev) => prev.map((item) => map.get(item.id) ?? item))
    },
    [setItems],
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
    sortColumn,
    sortDirection,
    handleSort,
    fieldsMeta,
    isPrefsLoaded,
    initialFilterValues: initialListFilters ?? {},
    handleFilterValuesChange,
    showDeleted,
    toggleShowDeleted,
    focusedId,
    setFocusedId,
    targetIndex,
    replaceItems,
  }
}
