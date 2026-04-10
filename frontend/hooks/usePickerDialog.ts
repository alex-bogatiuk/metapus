/**
 * usePickerDialog — shared hook for picker dialog data loading + keyboard navigation.
 *
 * Encapsulates:
 *   - debounced search
 *   - cursor-based infinite scroll (via ScrollSentinel)
 *   - local sort state (dialog-scoped, not URL)
 *   - keyboard row navigation (ArrowUp/Down, Enter)
 *   - picked items map (id → quantity)
 *
 * Used by GenericPickerDialog and ProductPickerDialog.
 * Pattern #3: Custom Hooks as orchestration layer.
 */

"use client"

import { useState, useCallback, useRef, useEffect, useMemo } from "react"
import { apiFetch, api } from "@/lib/api"
import type { CursorListResponse } from "@/types/common"

// ── Types ───────────────────────────────────────────────────────────────

type RowData = Record<string, unknown> & { id: string }

/** Pre-populated item for initializing picker quantities from existing document lines. */
export interface PickerInitialItem {
    id: string
    name: string
    code?: string
    unitId?: string
    unitName?: string
    quantity: number
}

interface UsePickerDialogOptions {
    /** API endpoint path, e.g. "/catalog/nomenclature" */
    apiEndpoint: string
    /** Whether the dialog is open */
    open: boolean
    /** Number of rows per page / load */
    limit?: number
    /** Additional query params (e.g. parentId filter) */
    extraParams?: Record<string, string>
    /** Initial items to pre-populate quantities (from existing document lines) */
    initialData?: PickerInitialItem[]
    /** Warehouse ID for stock balance display (from document header) */
    warehouseId?: string
}

interface UsePickerDialogResult {
    // ── Data ──
    items: RowData[]
    loading: boolean
    loadingMore: boolean
    totalCount: number
    hasMore: boolean

    // ── Search ──
    search: string
    setSearch: (value: string) => void

    // ── Sort ──
    sortField: string
    sortDir: "asc" | "desc"
    handleSort: (key: string) => void

    // ── Selection ──
    focusedId: string | null
    setFocusedId: (id: string | null) => void

    // ── Picked items (multi-select quantity map) ──
    quantities: Map<string, number>
    /** Cached metadata for picked items — survives search/filter changes */
    pickedItems: Map<string, RowData>
    setQuantity: (id: string, qty: number) => void
    clearQuantities: () => void
    pickedCount: number

    // ── Stock balances ──
    /** Map of productId → stock quantity (from warehouse). Undefined = not loaded yet. */
    balanceMap: Map<string, number>

    // ── Infinite scroll ──
    fetchMore: () => void

    // ── Keyboard ──
    handleKeyDown: (e: React.KeyboardEvent) => void

    // ── Scroll container ref ──
    scrollContainerRef: React.RefObject<HTMLDivElement | null>
    tableContainerRef: React.RefObject<HTMLDivElement | null>
}

const DEFAULT_LIMIT = 50

// ── Hook ────────────────────────────────────────────────────────────────

export function usePickerDialog({
    apiEndpoint,
    open,
    limit = DEFAULT_LIMIT,
    extraParams,
    initialData,
    warehouseId,
}: UsePickerDialogOptions): UsePickerDialogResult {
    // ── Data state ──────────────────────────────────────────────────────
    const [items, setItems] = useState<RowData[]>([])
    const [loading, setLoading] = useState(false)
    const [loadingMore, setLoadingMore] = useState(false)
    const [totalCount, setTotalCount] = useState(0)
    const [hasMore, setHasMore] = useState(false)
    const [nextCursor, setNextCursor] = useState<string | null>(null)

    // ── Search ──────────────────────────────────────────────────────────
    const [search, setSearch] = useState("")
    const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

    // ── Sort (combined state for atomic updates — avoids stale-state race) ──
    const [sortState, setSortState] = useState<{ field: string; dir: "asc" | "desc" }>({ field: "name", dir: "asc" })
    const sortField = sortState.field
    const sortDir = sortState.dir

    // ── Stock balances ──────────────────────────────────────────────────
    const [balanceMap, setBalanceMap] = useState<Map<string, number>>(new Map())

    // ── Selection ───────────────────────────────────────────────────────
    const [focusedId, setFocusedId] = useState<string | null>(null)

    // ── Quantities (multi-select) ───────────────────────────────────────
    const [quantities, setQuantities] = useState<Map<string, number>>(new Map())
    /** Cache of row data for all items that have been picked (qty > 0).
     *  This survives search/filter/category changes so the "Заказ" tab
     *  can always display the full list of picked items.
     */
    const [pickedItems, setPickedItems] = useState<Map<string, RowData>>(new Map())

    const setQuantity = useCallback((id: string, qty: number) => {
        setQuantities((prev) => {
            const next = new Map(prev)
            if (qty > 0) {
                next.set(id, qty)
            } else {
                next.delete(id)
            }
            return next
        })
        // Cache item metadata when adding
        if (qty > 0) {
            setPickedItems((prev) => {
                if (prev.has(id)) return prev
                // Find item in current loaded items
                const found = items.find((i) => i.id === id)
                if (!found) return prev
                const next = new Map(prev)
                next.set(id, found)
                return next
            })
        } else {
            // Remove from cache when qty goes to 0
            setPickedItems((prev) => {
                if (!prev.has(id)) return prev
                const next = new Map(prev)
                next.delete(id)
                return next
            })
        }
    }, [items])

    const clearQuantities = useCallback(() => {
        setQuantities(new Map())
        setPickedItems(new Map())
    }, [])

    const pickedCount = useMemo(() => quantities.size, [quantities])

    // ── Refs ────────────────────────────────────────────────────────────
    const scrollContainerRef = useRef<HTMLDivElement>(null)
    const tableContainerRef = useRef<HTMLDivElement>(null)

    // ── Stable extraParams serialization ─────────────────────────────────
    const extraParamsSerialized = useMemo(
        () => (extraParams ? JSON.stringify(extraParams) : ""),
        [extraParams],
    )

    // ── Fetch data (initial load or full reset) ─────────────────────────
    const fetchData = useCallback(async () => {
        if (!apiEndpoint) return
        setLoading(true)
        try {
            const params = new URLSearchParams()
            params.set("limit", String(limit))
            if (search.trim()) params.set("search", search.trim())
            const orderBy = sortDir === "desc" ? `-${sortField}` : sortField
            params.set("orderBy", orderBy)

            // Merge extra params
            if (extraParamsSerialized) {
                const extra = JSON.parse(extraParamsSerialized) as Record<string, string>
                for (const [k, v] of Object.entries(extra)) {
                    params.set(k, v)
                }
            }

            const result = await apiFetch<CursorListResponse<RowData>>(
                `${apiEndpoint}?${params.toString()}`,
            )
            const fetchedItems = result.items ?? []
            setItems(fetchedItems)
            setTotalCount(result.totalCount)
            setHasMore(result.hasMore)
            setNextCursor(result.nextCursor ?? null)

            if (fetchedItems.length) {
                setFocusedId(fetchedItems[0].id)
            } else {
                setFocusedId(null)
            }
        } catch {
            setItems([])
            setFocusedId(null)
        } finally {
            setLoading(false)
        }
    }, [apiEndpoint, search, sortField, sortDir, limit, extraParamsSerialized])

    // ── Load more (infinite scroll) ─────────────────────────────────────
    const fetchMore = useCallback(async () => {
        if (!apiEndpoint || !nextCursor || loadingMore) return
        setLoadingMore(true)
        try {
            const params = new URLSearchParams()
            params.set("limit", String(limit))
            if (search.trim()) params.set("search", search.trim())
            const orderBy = sortDir === "desc" ? `-${sortField}` : sortField
            params.set("orderBy", orderBy)
            params.set("after", nextCursor)

            if (extraParamsSerialized) {
                const extra = JSON.parse(extraParamsSerialized) as Record<string, string>
                for (const [k, v] of Object.entries(extra)) {
                    params.set(k, v)
                }
            }

            const result = await apiFetch<CursorListResponse<RowData>>(
                `${apiEndpoint}?${params.toString()}`,
            )
            const moreItems = result.items ?? []
            setItems((prev) => [...prev, ...moreItems])
            setHasMore(result.hasMore)
            setNextCursor(result.nextCursor ?? null)
        } catch {
            // silently fail
        } finally {
            setLoadingMore(false)
        }
    }, [apiEndpoint, nextCursor, loadingMore, search, sortField, sortDir, limit, extraParamsSerialized])

    // ── Initial load & debounced search ──────────────────────────────────
    const initialLoadRef = useRef(false)

    useEffect(() => {
        if (!open) {
            initialLoadRef.current = false
            setSearch("")
            setSortState({ field: "name", dir: "asc" })
            setItems([])
            setFocusedId(null)
            setNextCursor(null)
            setHasMore(false)
            setQuantities(new Map())
            setPickedItems(new Map())
            setBalanceMap(new Map())
            return
        }

        if (!initialLoadRef.current) {
            initialLoadRef.current = true
            // Pre-populate from existing document lines
            if (initialData && initialData.length > 0) {
                const qtyMap = new Map<string, number>()
                const itemMap = new Map<string, RowData>()
                for (const d of initialData) {
                    if (d.quantity > 0) {
                        qtyMap.set(d.id, d.quantity)
                        itemMap.set(d.id, {
                            id: d.id,
                            name: d.name,
                            code: d.code ?? "",
                            baseUnitId: d.unitId ?? "",
                            baseUnitName: d.unitName ?? "",
                        })
                    }
                }
                setQuantities(qtyMap)
                setPickedItems(itemMap)
            }
            fetchData()
            return
        }

        if (debounceRef.current) clearTimeout(debounceRef.current)
        debounceRef.current = setTimeout(() => fetchData(), 250)

        return () => {
            if (debounceRef.current) clearTimeout(debounceRef.current)
        }
    }, [open, fetchData, initialData])

    // ── Sort (atomic update — fixes toggling asc/desc) ────────────────
    const handleSort = useCallback((key: string) => {
        setSortState((prev) => {
            if (prev.field === key) {
                return { field: key, dir: prev.dir === "asc" ? "desc" : "asc" }
            }
            return { field: key, dir: "asc" }
        })
    }, [])

    // ── Keyboard ────────────────────────────────────────────────────────
    const handleKeyDown = useCallback(
        (e: React.KeyboardEvent) => {
            if (!items.length) return

            const currentIndex = focusedId
                ? items.findIndex((i) => i.id === focusedId)
                : -1

            switch (e.key) {
                case "ArrowDown": {
                    e.preventDefault()
                    const nextIdx = Math.min(currentIndex + 1, items.length - 1)
                    setFocusedId(items[nextIdx].id)
                    const row = tableContainerRef.current?.querySelector(
                        `[data-row-id="${items[nextIdx].id}"]`,
                    )
                    row?.scrollIntoView({ block: "nearest" })
                    break
                }
                case "ArrowUp": {
                    e.preventDefault()
                    const prevIdx = Math.max(currentIndex - 1, 0)
                    setFocusedId(items[prevIdx].id)
                    const row = tableContainerRef.current?.querySelector(
                        `[data-row-id="${items[prevIdx].id}"]`,
                    )
                    row?.scrollIntoView({ block: "nearest" })
                    break
                }
            }
        },
        [items, focusedId],
    )

    // ── Load stock balances when warehouse changes or dialog opens ────
    useEffect(() => {
        if (!open || !warehouseId) {
            setBalanceMap(new Map())
            return
        }
        api.stock.getBalancesByWarehouse(warehouseId).then((res) => {
            const map = new Map<string, number>()
            for (const item of res.items) {
                map.set(item.productId, item.quantity)
            }
            setBalanceMap(map)
        }).catch(() => {
            // Silently fail — balance is informational only
            setBalanceMap(new Map())
        })
    }, [open, warehouseId])

    return {
        items,
        loading,
        loadingMore,
        totalCount,
        hasMore,
        search,
        setSearch,
        sortField,
        sortDir,
        handleSort,
        focusedId,
        setFocusedId,
        quantities,
        pickedItems,
        setQuantity,
        clearQuantities,
        pickedCount,
        balanceMap,
        fetchMore,
        handleKeyDown,
        scrollContainerRef,
        tableContainerRef,
    }
}
