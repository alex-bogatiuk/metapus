"use client"

import { useState, useRef, useCallback, useMemo } from "react"

export interface UseListSelectionReturn {
    selectedIds: string[]
    isAllSelected: boolean
    isIndeterminate: boolean
    /** True when user activated virtual "select all by filter" mode. */
    selectAllByFilter: boolean
    /** IDs explicitly excluded from virtual select all (user unchecked). */
    excludedIds: string[]
    toggleItem: (id: string, shiftKey: boolean) => void
    toggleAll: () => void
    clearSelection: () => void
    /** Activate virtual "select all by filter" mode. */
    activateSelectAll: () => void
}

/**
 * Hook to manage selection state for a list of items.
 *
 * Features:
 *  - Select / deselect individual items
 *  - Select-all / deselect-all (toggleAll)
 *  - Shift+click range selection (OS-style)
 *  - Virtual "select all by filter" mode (Gmail-style):
 *    When active, all items matching the current filter are logically selected.
 *    Individual items can be unchecked (added to excludedIds).
 *
 * @param visibleIds – ordered array of ids currently displayed in the list.
 *                     Range selection works according to this order.
 */
export function useListSelection(visibleIds: string[]): UseListSelectionReturn {
    const [selectedIds, setSelectedIds] = useState<string[]>([])
    const [selectAllByFilter, setSelectAllByFilter] = useState(false)
    const [excludedIds, setExcludedIds] = useState<string[]>([])

    // We use a ref so it doesn't trigger re-renders; it's only read during click.
    const lastClickedIdRef = useRef<string | null>(null)

    // ⚡ Perf: O(1) lookup Set — avoids O(N²) from .includes() in isAllSelected / isIndeterminate.
    const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds])
    const excludedSet = useMemo(() => new Set(excludedIds), [excludedIds])

    const isAllSelected = useMemo(() => {
        if (selectAllByFilter) {
            // In virtual mode: all visible are "selected" unless excluded
            return visibleIds.length > 0 && visibleIds.every((id) => !excludedSet.has(id))
        }
        return visibleIds.length > 0 && visibleIds.every((id) => selectedSet.has(id))
    }, [visibleIds, selectedSet, excludedSet, selectAllByFilter])

    const isIndeterminate = useMemo(() => {
        if (selectAllByFilter) {
            // Indeterminate when some visible items are excluded
            return visibleIds.some((id) => excludedSet.has(id)) &&
                !visibleIds.every((id) => excludedSet.has(id))
        }
        return !isAllSelected && visibleIds.some((id) => selectedSet.has(id))
    }, [visibleIds, selectedSet, excludedSet, isAllSelected, selectAllByFilter])

    const toggleItem = useCallback(
        (id: string, shiftKey: boolean) => {
            if (selectAllByFilter) {
                // In virtual mode: toggling an item adds/removes it from excludedIds
                setExcludedIds((prev) => {
                    const set = new Set(prev)
                    if (set.has(id)) {
                        set.delete(id)
                    } else {
                        set.add(id)
                    }
                    return Array.from(set)
                })
                // Sync selectedIds so checkbox visually reflects the exclusion
                setSelectedIds((prev) =>
                    prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
                )
                lastClickedIdRef.current = id
                return
            }

            if (shiftKey && lastClickedIdRef.current !== null) {
                // ── Range selection ──────────────────────────────
                const lastIdx = visibleIds.indexOf(lastClickedIdRef.current)
                const curIdx = visibleIds.indexOf(id)

                if (lastIdx !== -1 && curIdx !== -1) {
                    const start = Math.min(lastIdx, curIdx)
                    const end = Math.max(lastIdx, curIdx)
                    const rangeIds = visibleIds.slice(start, end + 1)

                    setSelectedIds((prev) => {
                        const set = new Set(prev)
                        rangeIds.forEach((rid) => set.add(rid))
                        return Array.from(set)
                    })

                    // Update last clicked to the current item
                    lastClickedIdRef.current = id
                    return
                }
            }

            // ── Normal toggle ────────────────────────────────
            setSelectedIds((prev) =>
                prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
            )
            lastClickedIdRef.current = id
        },
        [visibleIds, selectAllByFilter]
    )

    const toggleAll = useCallback(() => {
        if (selectAllByFilter) {
            // Deactivate virtual mode
            setSelectAllByFilter(false)
            setExcludedIds([])
            setSelectedIds([])
            return
        }

        if (isAllSelected) {
            // Deselect all visible items (keep items from other pages if any)
            // ⚡ Perf: use Set for O(1) lookups instead of O(N) .includes() per item
            const visibleSet = new Set(visibleIds)
            setSelectedIds((prev) => prev.filter((id) => !visibleSet.has(id)))
        } else {
            // Select all visible items (merge with already selected)
            setSelectedIds((prev) => {
                const set = new Set(prev)
                visibleIds.forEach((id) => set.add(id))
                return Array.from(set)
            })
        }
    }, [isAllSelected, visibleIds, selectAllByFilter])

    const clearSelection = useCallback(() => {
        setSelectedIds([])
        setSelectAllByFilter(false)
        setExcludedIds([])
        lastClickedIdRef.current = null
    }, [])

    const activateSelectAll = useCallback(() => {
        setSelectAllByFilter(true)
        setExcludedIds([])
        // Also select all visible items (for consistent local state)
        setSelectedIds(visibleIds.slice())
    }, [visibleIds])

    return {
        selectedIds,
        isAllSelected,
        isIndeterminate,
        selectAllByFilter,
        excludedIds,
        toggleItem,
        toggleAll,
        clearSelection,
        activateSelectAll,
    }
}
