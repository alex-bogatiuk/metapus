// hooks/useVisibleColumns.ts — manages per-user column visibility AND order in list pages.
// Connects ALL_COLUMNS registry with saved preferences from useUserPrefsStore.
// Supports DnD column reordering via reorderColumns().

"use client"

import { useMemo, useCallback } from "react"
import type { Column } from "@/components/shared/data-table"
import { canonicalizeColumnKeys } from "@/lib/metadata-columns"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"

// ── Types ───────────────────────────────────────────────────────────────

export interface UseVisibleColumnsOptions<T> {
    /** Entity key for preferences persistence (e.g. "CryptoInvoice"). */
    entityKey: string
    /** Full registry of all possible columns. */
    allColumns: Column<T>[]
    /** Column keys visible by default (when user has no saved preferences). */
    defaultVisibleKeys: string[]
}

export interface UseVisibleColumnsResult<T> {
    /** Filtered columns array for DataTable — ordered by user preference. */
    visibleColumns: Column<T>[]
    /** All columns ordered for ColumnChooserPopover: visible first (in order), then hidden. */
    orderedAllColumns: Column<T>[]
    /** Full column registry (original order, for reference). */
    allColumns: Column<T>[]
    /** Current set of visible column keys (in display order). */
    visibleKeys: string[]
    /** Toggle a single column on/off. When toggling ON, appends to end. */
    toggleColumn: (key: string) => void
    /** Reorder visible columns (called from DnD). Receives new ordered visible keys. */
    reorderColumns: (newOrderedKeys: string[]) => void
    /** Reset to default visible columns. */
    resetColumns: () => void
    /** Check if a column key is currently visible. */
    isColumnVisible: (key: string) => boolean
}

// ── Hook ────────────────────────────────────────────────────────────────

export function useVisibleColumns<T>({
    entityKey,
    allColumns,
    defaultVisibleKeys,
}: UseVisibleColumnsOptions<T>): UseVisibleColumnsResult<T> {

    // Direct property access — returns undefined (stable reference) when no saved
    // columns. Using getListColumns() would return a new [] on every render,
    // causing infinite re-render via visibleColumns → resizeDefs → useColumnResize.
    const savedColumns = useUserPrefsStore((s) => s.listColumns[entityKey])
    const setListColumns = useUserPrefsStore((s) => s.setListColumns)

    // Effective visible keys: saved preferences → default.
    // The saved array preserves USER ORDER (from DnD reorder).
    // Overlay aliases like statusName → status are rewritten so old prefs survive.
    const visibleKeys = useMemo(() => {
        const allKeys = allColumns.map((c) => c.key)
        const allKeySet = new Set(allKeys)
        const source = savedColumns && savedColumns.length > 0 ? savedColumns : defaultVisibleKeys
        return canonicalizeColumnKeys(source, allKeySet).filter((k) => allKeySet.has(k))
    }, [savedColumns, defaultVisibleKeys, allColumns])

    // Filtered & ORDERED columns for DataTable — respects visibleKeys order.
    const visibleColumns = useMemo(() => {
        const colMap = new Map(allColumns.map((c) => [c.key, c]))
        return visibleKeys
            .map((key) => colMap.get(key))
            .filter((col): col is Column<T> => col !== undefined)
    }, [allColumns, visibleKeys])

    // All columns ordered for ColumnChooserPopover dialog:
    // visible ones first (in user order), then hidden (in original allColumns order).
    const orderedAllColumns = useMemo(() => {
        const visibleSet = new Set(visibleKeys)
        const colMap = new Map(allColumns.map((c) => [c.key, c]))
        const visible = visibleKeys
            .map((key) => colMap.get(key))
            .filter((col): col is Column<T> => col !== undefined)
        const hidden = allColumns.filter((col) => !visibleSet.has(col.key))
        return [...visible, ...hidden]
    }, [allColumns, visibleKeys])

    const toggleColumn = useCallback(
        (key: string) => {
            const keySet = new Set(visibleKeys)
            if (keySet.has(key)) {
                // Remove — preserve order of remaining keys.
                setListColumns(entityKey, visibleKeys.filter((k) => k !== key))
            } else {
                // Add — append to end of current order.
                setListColumns(entityKey, [...visibleKeys, key])
            }
        },
        [visibleKeys, entityKey, setListColumns],
    )

    const reorderColumns = useCallback(
        (newOrderedKeys: string[]) => {
            setListColumns(entityKey, newOrderedKeys)
        },
        [entityKey, setListColumns],
    )

    const resetColumns = useCallback(() => {
        setListColumns(entityKey, defaultVisibleKeys)
    }, [entityKey, defaultVisibleKeys, setListColumns])

    const isColumnVisible = useCallback(
        (key: string) => visibleKeys.includes(key),
        [visibleKeys],
    )

    return {
        visibleColumns,
        orderedAllColumns,
        allColumns,
        visibleKeys,
        toggleColumn,
        reorderColumns,
        resetColumns,
        isColumnVisible,
    }
}
