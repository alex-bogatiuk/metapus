// hooks/useColumnResize.ts — drag-resize logic for DataTable columns.
// Encapsulates mousedown → mousemove → mouseup sequence using native DOM events
// for smooth, re-render-free dragging. Returns computed pixel widths and handlers.

"use client"

import { useState, useCallback, useRef, useEffect } from "react"

// ── Types ───────────────────────────────────────────────────────────────

export interface ColumnResizeDef {
    /** Unique column key (maps to stored widths). */
    key: string
    /** Default width in pixels if no stored value exists. */
    width?: number
    /** Minimum allowed width in pixels. Default = 60. */
    minWidth?: number
}

export interface UseColumnResizeOptions {
    /** Column definitions — must be stable (useMemo). */
    columns: ColumnResizeDef[]
    /** Externally stored widths (from user prefs). Keys = column keys. */
    storedWidths?: Record<string, number>
    /** Called when drag ends with the new widths map. Debounce outside if needed. */
    onWidthsChange?: (widths: Record<string, number>) => void
}

export interface UseColumnResizeResult {
    /** Computed pixel width per column, in the same order as `columns`. */
    colWidths: number[]
    /** Attach to the drag handle's onMouseDown for a given column index. */
    onResizeStart: (colIndex: number, e: React.MouseEvent) => void
    /** True while a drag is in progress (used to disable text selection). */
    isResizing: boolean
}

// ── Constants ───────────────────────────────────────────────────────────

const DEFAULT_WIDTH = 150
const DEFAULT_MIN_WIDTH = 60

// ── Hook ────────────────────────────────────────────────────────────────

export function useColumnResize({
    columns,
    storedWidths,
    onWidthsChange,
}: UseColumnResizeOptions): UseColumnResizeResult {

    // Compute initial widths from stored → column default → global default.
    const computeInitial = useCallback(() => {
        return columns.map((col) =>
            storedWidths?.[col.key] ?? col.width ?? DEFAULT_WIDTH
        )
    }, [columns, storedWidths])

    const [colWidths, setColWidths] = useState<number[]>(computeInitial)

    // Re-sync when stored widths or columns change (e.g. prefs loaded from server).
    useEffect(() => {
        setColWidths(computeInitial())
    }, [computeInitial])

    const [isResizing, setIsResizing] = useState(false)

    // Refs for drag state (avoids stale closures in native event handlers).
    const dragRef = useRef<{
        colIndex: number
        startX: number
        startWidth: number
    } | null>(null)

    // Single mutable ref to hold latest values — updated only inside useEffect,
    // never during render, to satisfy react-hooks/refs.
    const latestRef = useRef({
        colWidths,
        columns,
        onWidthsChange,
    })

    useEffect(() => {
        latestRef.current = { colWidths, columns, onWidthsChange }
    }, [colWidths, columns, onWidthsChange])

    // ── Stable native DOM handlers ──────────────────────────────────────
    // Both handlers are created once (empty deps) and read mutable state
    // exclusively through refs. The handleMouseUp→handleMouseMove dependency
    // is also resolved by storing both in a shared object ref.

    const handlersRef = useRef({ move: (_e: MouseEvent) => {}, up: () => {} })

    useEffect(() => {
        const move = (e: MouseEvent) => {
            if (!dragRef.current) return
            const { colIndex, startX, startWidth } = dragRef.current
            const delta = e.clientX - startX
            const minW = latestRef.current.columns[colIndex]?.minWidth ?? DEFAULT_MIN_WIDTH
            const newWidth = Math.max(minW, startWidth + delta)

            setColWidths((prev) => {
                const next = [...prev]
                next[colIndex] = newWidth
                return next
            })
        }

        const up = () => {
            if (!dragRef.current) return
            dragRef.current = null
            // Defer isResizing reset — the browser fires `click` on <th> synchronously
            // after `mouseup`. We need isResizing to still be true when that click fires
            // so the sort guard in DataTable can block it.
            setTimeout(() => setIsResizing(false), 0)

            document.removeEventListener("mousemove", handlersRef.current.move)
            document.removeEventListener("mouseup", handlersRef.current.up)
            document.body.style.cursor = ""
            document.body.style.userSelect = ""

            // Notify parent about final widths (for persistence).
            const { columns: cols, colWidths: widths, onWidthsChange: onChange } = latestRef.current
            const widthsMap: Record<string, number> = {}
            cols.forEach((col, i) => {
                widthsMap[col.key] = widths[i]
            })
            onChange?.(widthsMap)
        }

        handlersRef.current = { move, up }

        // Cleanup: remove potentially dangling listeners on unmount.
        return () => {
            document.removeEventListener("mousemove", move)
            document.removeEventListener("mouseup", up)
        }
    }, [])

    // ── Public handler ──────────────────────────────────────────────────

    const onResizeStart = useCallback(
        (colIndex: number, e: React.MouseEvent) => {
            e.preventDefault()
            e.stopPropagation()

            dragRef.current = {
                colIndex,
                startX: e.clientX,
                startWidth: latestRef.current.colWidths[colIndex],
            }
            setIsResizing(true)

            document.body.style.cursor = "col-resize"
            document.body.style.userSelect = "none"

            document.addEventListener("mousemove", handlersRef.current.move)
            document.addEventListener("mouseup", handlersRef.current.up)
        },
        []
    )

    return { colWidths, onResizeStart, isResizing }
}
