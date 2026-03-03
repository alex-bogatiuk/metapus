"use client"

import React, { useMemo } from "react"
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react"
import { Checkbox } from "@/components/ui/checkbox"
import { cn } from "@/lib/utils"

// ── Types ───────────────────────────────────────────────────────────────

export interface Column<T> {
    /** Unique key matching a field in T (used for sorting). */
    key: string
    /** Column header label. */
    label: string
    /** Text alignment. Default = 'left'. */
    align?: "left" | "right" | "center"
    /** Whether column is sortable. Default = false. */
    sortable?: boolean
    /** Custom cell renderer. Falls back to `String(item[key])`. */
    render?: (item: T) => React.ReactNode
    /** Extra className applied to both th and td. */
    className?: string
}

export interface DataTableProps<T extends { id: string }> {
    /** Items to display. */
    data: T[]
    /** Column definitions. */
    columns: Column<T>[]

    // ── Selection (from useListSelection) ──────────────────────────────
    selectedIds: string[]
    isAllSelected: boolean
    isIndeterminate: boolean
    onToggleAll: () => void
    onToggleItem: (id: string, shiftKey: boolean) => void

    // ── Sorting (controlled, from useUrlSort) ─────────────────────────
    /** Currently sorted column key. */
    sortColumn?: string | null
    /** Current sort direction. */
    sortDirection?: "asc" | "desc"
    /** Called when user clicks a sortable column header. */
    onSort?: (key: string) => void

    // ── Row actions ────────────────────────────────────────────────────
    /** Called on double-click of a row. */
    onRowDoubleClick?: (item: T) => void

    // ── Optional prefix column (e.g. status icon) ─────────────────────
    renderPrefix?: (item: T) => React.ReactNode
}

// ── Helpers ─────────────────────────────────────────────────────────────

type SortDir = "asc" | "desc"

function toComparableDate(value: unknown): number | null {
    if (typeof value !== "string") return null

    const raw = value.trim()

    // Limit date parsing to ISO-like values to avoid false positives.
    if (!/^\d{4}-\d{2}-\d{2}(?:[T\s].*)?$/.test(raw)) return null

    const ts = Date.parse(raw)
    return Number.isNaN(ts) ? null : ts
}

function toComparableNumber(value: unknown): number | null {
    if (typeof value === "number") {
        return Number.isFinite(value) ? value : null
    }

    if (typeof value !== "string") return null

    const normalized = value.trim().replace(/\s/g, "").replace(",", ".")
    if (!/^-?\d+(?:\.\d+)?$/.test(normalized)) return null

    const parsed = Number(normalized)
    return Number.isFinite(parsed) ? parsed : null
}

function compareValues(a: unknown, b: unknown, dir: SortDir): number {
    const valA = a ?? ""
    const valB = b ?? ""

    // Compare ISO-like date strings first.
    const dateA = toComparableDate(valA)
    const dateB = toComparableDate(valB)
    if (dateA !== null && dateB !== null) {
        return dir === "asc" ? dateA - dateB : dateB - dateA
    }

    // Then compare strict numeric values.
    const numA = toComparableNumber(valA)
    const numB = toComparableNumber(valB)
    if (numA !== null && numB !== null) {
        return dir === "asc" ? numA - numB : numB - numA
    }

    // Fallback to string comparison
    const strA = String(valA).toLowerCase()
    const strB = String(valB).toLowerCase()
    const cmp = strA.localeCompare(strB, "ru", { numeric: true })
    return dir === "asc" ? cmp : -cmp
}

// ── Component ───────────────────────────────────────────────────────────

export function DataTable<T extends { id: string }>({
    data,
    columns,
    selectedIds,
    isAllSelected,
    isIndeterminate,
    onToggleAll,
    onToggleItem,
    sortColumn = null,
    sortDirection = "asc",
    onSort,
    onRowDoubleClick,
    renderPrefix,
}: DataTableProps<T>) {

    // Sort data
    const sortedData = useMemo(() => {
        if (!sortColumn) return data
        return [...data].sort((a, b) =>
            compareValues(
                (a as Record<string, unknown>)[sortColumn],
                (b as Record<string, unknown>)[sortColumn],
                sortDirection
            )
        )
    }, [data, sortColumn, sortDirection])

    // Determine the checked state for the header checkbox
    const headerChecked = isAllSelected
        ? true
        : isIndeterminate
            ? ("indeterminate" as const)
            : false

    const alignClass = (align?: "left" | "right" | "center") => {
        if (align === "right") return "text-right"
        if (align === "center") return "text-center"
        return "text-left"
    }

    const SortIcon = ({ colKey }: { colKey: string }) => {
        if (sortColumn !== colKey) {
            return <ArrowUpDown className="ml-1 inline h-3 w-3 opacity-0 group-hover:opacity-40" />
        }
        return sortDirection === "asc" ? (
            <ArrowUp className="ml-1 inline h-3 w-3 text-primary" />
        ) : (
            <ArrowDown className="ml-1 inline h-3 w-3 text-primary" />
        )
    }

    return (
        <table className="w-full text-sm table-fixed">
            <thead className="sticky top-0 z-10">
                <tr className="border-b bg-muted/70">
                    {/* Checkbox column — fixed width, centered */}
                    <th className="w-[40px] min-w-[40px] max-w-[40px] px-0 py-2">
                        <div className="flex items-center justify-center">
                            <Checkbox
                                checked={headerChecked}
                                onCheckedChange={onToggleAll}
                            />
                        </div>
                    </th>

                    {/* Optional prefix column (status icon, etc.) */}
                    {renderPrefix && (
                        <th className="w-[32px] min-w-[32px] max-w-[32px] px-1 py-2" />
                    )}

                    {/* Data columns */}
                    {columns.map((col) => (
                        <th
                            key={col.key}
                            className={cn(
                                "px-4 py-2 text-xs font-medium text-muted-foreground select-none",
                                alignClass(col.align),
                                col.sortable && "cursor-pointer group hover:text-foreground transition-colors",
                                col.className
                            )}
                            onClick={col.sortable && onSort ? () => onSort(col.key) : undefined}
                        >
                            {col.label}
                            {col.sortable && <SortIcon colKey={col.key} />}
                        </th>
                    ))}
                </tr>
            </thead>
            <tbody>
                {sortedData.map((item) => {
                    const isSelected = selectedIds.includes(item.id)

                    return (
                        <tr
                            key={item.id}
                            className={cn(
                                "border-b transition-colors cursor-pointer",
                                isSelected ? "bg-primary/10" : "hover:bg-primary/5"
                            )}
                            onDoubleClick={() => onRowDoubleClick?.(item)}
                        >
                            {/* Checkbox cell — same fixed width as header, centered */}
                            <td className="w-[40px] min-w-[40px] max-w-[40px] px-0 py-2">
                                <div className="flex items-center justify-center">
                                    <Checkbox
                                        checked={isSelected}
                                        onClick={(e: React.MouseEvent) => {
                                            e.stopPropagation()
                                            onToggleItem(item.id, e.shiftKey)
                                        }}
                                        onCheckedChange={() => {
                                            /* handled by onClick to capture shiftKey */
                                        }}
                                    />
                                </div>
                            </td>

                            {/* Optional prefix cell */}
                            {renderPrefix && (
                                <td className="w-[32px] min-w-[32px] max-w-[32px] px-1 py-2">
                                    {renderPrefix(item)}
                                </td>
                            )}

                            {/* Data cells */}
                            {columns.map((col) => (
                                <td
                                    key={col.key}
                                    className={cn(
                                        "px-4 py-2",
                                        alignClass(col.align),
                                        col.className
                                    )}
                                >
                                    {col.render
                                        ? col.render(item)
                                        : String((item as Record<string, unknown>)[col.key] ?? "")}
                                </td>
                            ))}
                        </tr>
                    )
                })}
            </tbody>
        </table>
    )
}
