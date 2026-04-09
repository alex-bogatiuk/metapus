"use client"

import { useState, useCallback, useRef } from "react"
import type { CursorListResponse, CursorListParams, AdvancedFilterItem } from "@/types/common"

// ── Types ───────────────────────────────────────────────────────────────

export interface UseCursorListOptions<T> {
  /** API list function. */
  fetcher: (params?: CursorListParams) => Promise<CursorListResponse<T>>
  /** Items per page. Default 50. */
  limit?: number
}

export interface UseCursorListReturn<T> {
  /** All loaded items (growing array). */
  items: T[]
  /** True while any fetch is in progress. */
  loading: boolean
  /** Error message (null if OK). */
  error: string | null
  /** Total count (only set on first page load). */
  totalCount: number
  /** True if there are more items to load forward. */
  hasMore: boolean
  /** True if there are items before the current window. */
  hasPrev: boolean
  /** Index of the target item after an "around" load. */
  targetIndex: number | null
  /** Load the next page (scroll down). */
  loadMore: () => Promise<void>
  /** Load the previous page (scroll up). */
  loadPrev: () => Promise<void>
  /** Reset and fetch initial page with optional filters. */
  reset: (params?: {
    filter?: AdvancedFilterItem[]
    includeDeleted?: boolean
    orderBy?: string
    search?: string
  }) => Promise<void>
  /** Teleport to a specific item by ID. */
  loadAround: (targetId: string) => Promise<void>
}

// ── Hook ────────────────────────────────────────────────────────────────

/**
 * Generic cursor-based pagination hook.
 *
 * Manages a growing array of items with forward/backward cursor navigation
 * and "around" teleportation for "show in list" functionality.
 *
 * Usage:
 * ```tsx
 * const list = useCursorList({ fetcher: api.goodsReceipts.list, limit: 50 })
 * // list.items, list.loadMore(), list.loadPrev(), list.reset(...)
 * ```
 */
export function useCursorList<T>(
  options: UseCursorListOptions<T>,
): UseCursorListReturn<T> {
  const { fetcher, limit = 50 } = options

  const [items, setItems] = useState<T[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [totalCount, setTotalCount] = useState(0)
  const [hasMore, setHasMore] = useState(false)
  const [hasPrev, setHasPrev] = useState(false)
  const [targetIndex, setTargetIndex] = useState<number | null>(null)

  // Store cursors for next/prev navigation
  const nextCursorRef = useRef<string | undefined>(undefined)
  const prevCursorRef = useRef<string | undefined>(undefined)
  // Store current filter params for cursor requests
  const currentParamsRef = useRef<Omit<CursorListParams, "after" | "before" | "around" | "limit">>({})

  const applyResult = useCallback((
    res: CursorListResponse<T>,
    mode: "replace" | "append" | "prepend",
  ) => {
    setItems(prev => {
      switch (mode) {
        case "replace": return res.items ?? []
        case "append": return [...prev, ...(res.items ?? [])]
        case "prepend": return [...(res.items ?? []), ...prev]
      }
    })

    setHasMore(res.hasMore)
    setHasPrev(res.hasPrev)
    setTargetIndex(res.targetIndex ?? null)

    if (mode === "replace") {
      // First page — totalCount is set by backend
      setTotalCount(res.totalCount)
      nextCursorRef.current = res.nextCursor
      prevCursorRef.current = res.prevCursor
    } else if (mode === "append") {
      nextCursorRef.current = res.nextCursor
      // prevCursor from append doesn't replace our window's start
    } else if (mode === "prepend") {
      prevCursorRef.current = res.prevCursor
      // nextCursor from prepend doesn't replace our window's end
    }
  }, [])

  // ── Reset: fetch first page ─────────────────────────────────────────
  const reset = useCallback(async (params?: {
    filter?: AdvancedFilterItem[]
    includeDeleted?: boolean
    orderBy?: string
    search?: string
  }) => {
    const p = params ?? {}
    currentParamsRef.current = p
    setLoading(true)
    setError(null)
    setTargetIndex(null)
    try {
      const res = await fetcher({ limit, ...p })
      applyResult(res, "replace")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    } finally {
      setLoading(false)
    }
  }, [fetcher, limit, applyResult])

  // ── Load more (forward / scroll down) ────────────────────────────────
  const loadMore = useCallback(async () => {
    if (!nextCursorRef.current || loading) return
    setLoading(true)
    setError(null)
    try {
      const res = await fetcher({
        limit,
        ...currentParamsRef.current,
        after: nextCursorRef.current,
      })
      applyResult(res, "append")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    } finally {
      setLoading(false)
    }
  }, [fetcher, limit, loading, applyResult])

  // ── Load prev (backward / scroll up) ─────────────────────────────────
  const loadPrev = useCallback(async () => {
    if (!prevCursorRef.current || loading) return
    setLoading(true)
    setError(null)
    try {
      const res = await fetcher({
        limit,
        ...currentParamsRef.current,
        before: prevCursorRef.current,
      })
      applyResult(res, "prepend")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    } finally {
      setLoading(false)
    }
  }, [fetcher, limit, loading, applyResult])

  // ── Load around (teleportation) ──────────────────────────────────────
  const loadAround = useCallback(async (targetId: string) => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetcher({
        limit,
        ...currentParamsRef.current,
        around: targetId,
      })
      applyResult(res, "replace")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    } finally {
      setLoading(false)
    }
  }, [fetcher, limit, applyResult])

  return {
    items,
    loading,
    error,
    totalCount,
    hasMore,
    hasPrev,
    targetIndex,
    loadMore,
    loadPrev,
    reset,
    loadAround,
  }
}
