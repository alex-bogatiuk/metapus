"use client"

import { useState, useEffect, useMemo, useCallback } from "react"
import { useListSelection } from "@/hooks/useListSelection"
import { useUrlSort } from "@/hooks/useUrlSort"
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

  const [items, setItems] = useState<T[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetcher({ limit, offset: 0 })
      setItems(res.items ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    } finally {
      setLoading(false)
    }
  }, [fetcher, limit])

  useEffect(() => { fetchData() }, [fetchData])

  const visibleIds = useMemo(() => items.map((d) => d.id), [items])
  const selection = useListSelection(visibleIds)
  const { sortColumn, sortDirection, handleSort } = useUrlSort()

  return {
    items,
    loading,
    error,
    refresh: fetchData,
    selection,
    sortColumn,
    sortDirection,
    handleSort,
  }
}
