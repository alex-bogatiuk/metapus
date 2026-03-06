"use client"

import { useEffect, useState, useMemo, useCallback, useRef } from "react"
import { useListSelection } from "@/hooks/useListSelection"
import { useUrlSort } from "@/hooks/useUrlSort"
import { useEntityFiltersMeta } from "@/hooks/useEntityFiltersMeta"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { buildFilterItems, type FilterValues } from "@/lib/filter-utils"
import type { FilterFieldMeta } from "@/components/shared/filter-config-dialog"
import type { AdvancedFilterItem } from "@/types/common"

// ── Types ───────────────────────────────────────────────────────────────

interface ListResponse<T> {
  items: T[]
  total?: number
}

interface DocumentListApi<T> {
  list: (params?: {
    limit?: number
    offset?: number
    filter?: AdvancedFilterItem[]
    includeDeleted?: boolean
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
  error: string | null
  refresh: () => void
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

  // ── Filter metadata from backend ─────────────────────────────────────
  const { fieldsMeta } = useEntityFiltersMeta(entityKey)

  // ── State ────────────────────────────────────────────────────────────
  const [items, setItems] = useState<T[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [focusedId, setFocusedId] = useState<string | null>(null)

  const filterValuesRef = useRef<FilterValues>({})

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
          offset: 0,
          filter: advancedFilters.length > 0 ? advancedFilters : undefined,
          includeDeleted: showDeletedRef.current || undefined,
        })
        setItems(res.items ?? [])
      } catch (err) {
        setError(err instanceof Error ? err.message : "Ошибка загрузки данных")
      } finally {
        setLoading(false)
      }
    },
    [docApi, fieldsMeta, periodField, limit],
  )

  // ── Init (wait for prefs) ────────────────────────────────────────────
  const initialized = useRef(false)

  useEffect(() => {
    if (isPrefsLoaded && !initialized.current) {
      initialized.current = true
      const initial = initialListFilters ?? {}
      filterValuesRef.current = initial
      fetchData(initial)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isPrefsLoaded, fetchData])

  // ── Filter change handler ────────────────────────────────────────────
  const handleFilterValuesChange = useCallback(
    (values: FilterValues) => {
      filterValuesRef.current = values
      setListFilters(entityKey, values)
      fetchData(values)
    },
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

  // ── Selection ────────────────────────────────────────────────────────
  const visibleIds = useMemo(() => items.map((d) => d.id), [items])
  const { selectedIds, isAllSelected, isIndeterminate, toggleItem, toggleAll } =
    useListSelection(visibleIds)

  // ── Sorting ──────────────────────────────────────────────────────────
  const { sortColumn, sortDirection, handleSort } = useUrlSort()

  return {
    items,
    loading,
    error,
    refresh,
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
  }
}
