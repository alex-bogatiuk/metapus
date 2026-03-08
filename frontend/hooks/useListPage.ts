"use client"

import { useState, useEffect, useMemo, useCallback, useRef } from "react"
import { useSearchParams } from "next/navigation"
import { useListSelection } from "@/hooks/useListSelection"
import { useUrlSort } from "@/hooks/useUrlSort"
import { useTabState, useHasTabCache } from "@/hooks/useTabState"
import type { ListParams, ListResponse } from "@/types/common"
import type { UseListSelectionReturn } from "@/hooks/useListSelection"

interface UseListPageOptions<T extends { id: string }> {
  /** API fetcher function (e.g. api.warehouses.list). */
  fetcher: (params?: ListParams) => Promise<ListResponse<T>>
  /** Max items to fetch. Default 200. */
  limit?: number
}

interface UseListPageReturn<T extends { id: string }> {
  items: T[]
  loading: boolean
  error: string | null
  /** Re-fetch data from API. */
  refresh: () => void
  /** Selection state from useListSelection. */
  selection: UseListSelectionReturn
  /** Sort state from useUrlSort. */
  sortColumn: string | null
  sortDirection: "asc" | "desc"
  handleSort: (column: string) => void
  focusedId: string | null
}

/**
 * Generic hook for catalog/document list pages.
 *
 * Encapsulates the repeated pattern of:
 *  - items/loading/error state + fetchData callback
 *  - visibleIds + useListSelection
 *  - useUrlSort
 *
 * Usage:
 * ```tsx
 * const list = useListPage({ fetcher: api.warehouses.list })
 * // list.items, list.loading, list.error, list.refresh
 * // list.selection.selectedIds, .isAllSelected, .toggleItem, .toggleAll
 * // list.sortColumn, list.sortDirection, list.handleSort
 * ```
 */
export function useListPage<T extends { id: string }>(
  options: UseListPageOptions<T>,
): UseListPageReturn<T> {
  const { fetcher, limit = 200 } = options
  const searchParams = useSearchParams()
  const aroundId = searchParams.get("around")

  // ── Sorting (read early — orderBy needed by fetch callback) ──────
  const { sortColumn, sortDirection, handleSort, orderBy } = useUrlSort()
  const orderByRef = useRef<string | undefined>(orderBy)

  // Tab-cached state (persists across tab switches)
  const [items, setItems] = useTabState<T[]>("items", [])
  const [focusedId, setFocusedId] = useTabState<string | null>("focusedId", aroundId)

  // Transient state (not cached — derived from cache hit)
  const hasCachedItems = useHasTabCache("items")
  const [loading, setLoading] = useState(!hasCachedItems)
  const [error, setError] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetcher({ limit, orderBy: orderByRef.current, around: aroundId || undefined })
      setItems(res.items ?? [])
      setFocusedId(aroundId)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    } finally {
      setLoading(false)
    }
  }, [fetcher, limit, aroundId, setItems, setFocusedId])

  // Skip initial fetch if we have cached items from a previous tab visit
  const skipInitRef = useRef(hasCachedItems)
  useEffect(() => {
    if (skipInitRef.current) {
      skipInitRef.current = false
      return
    }
    fetchData()
  }, [fetchData])

  // Re-fetch when sort changes
  const prevOrderByRef = useRef(orderBy)
  useEffect(() => {
    if (prevOrderByRef.current === orderBy) return
    prevOrderByRef.current = orderBy
    orderByRef.current = orderBy
    fetchData()
  }, [orderBy, fetchData])

  const visibleIds = useMemo(() => items.map((d) => d.id), [items])
  const selection = useListSelection(visibleIds)

  return {
    items,
    loading,
    error,
    refresh: fetchData,
    selection,
    sortColumn,
    sortDirection,
    handleSort,
    focusedId,
  }
}
