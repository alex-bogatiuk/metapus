"use client"

import { useState, useRef, useCallback, useMemo } from "react"

export interface UseListSelectionReturn {
    selectedIds: string[]
    isAllSelected: boolean
    isIndeterminate: boolean
    toggleItem: (id: string, shiftKey: boolean) => void
    toggleAll: () => void
    clearSelection: () => void
}

/**
 * Hook to manage selection state for a list of items.
 *
 * Features:
 *  - Select / deselect individual items
 *  - Select-all / deselect-all (toggleAll)
 *  - Shift+click range selection (OS-style)
 *
 * @param visibleIds – ordered array of ids currently displayed in the list.
 *                     Range selection works according to this order.
 */
export function useListSelection(visibleIds: string[]): UseListSelectionReturn {
    const [selectedIds, setSelectedIds] = useState<string[]>([])

    // We use a ref so it doesn't trigger re-renders; it's only read during click.
    const lastClickedIdRef = useRef<string | null>(null)

    // ⚡ Perf: O(1) lookup Set — avoids O(N²) from .includes() in isAllSelected / isIndeterminate.
    const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds])

    const isAllSelected = useMemo(
        () => visibleIds.length > 0 && visibleIds.every((id) => selectedSet.has(id)),
        [visibleIds, selectedSet]
    )

    const isIndeterminate = useMemo(
        () => !isAllSelected && visibleIds.some((id) => selectedSet.has(id)),
        [visibleIds, selectedSet, isAllSelected]
    )

    const toggleItem = useCallback(
        (id: string, shiftKey: boolean) => {
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
        [visibleIds]
    )

    const toggleAll = useCallback(() => {
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
    }, [isAllSelected, visibleIds])

    const clearSelection = useCallback(() => {
        setSelectedIds([])
        lastClickedIdRef.current = null
    }, [])

    return {
        selectedIds,
        isAllSelected,
        isIndeterminate,
        toggleItem,
        toggleAll,
        clearSelection,
    }
}
