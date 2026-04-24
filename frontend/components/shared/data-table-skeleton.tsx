// components/shared/data-table-skeleton.tsx
"use client"

import { Skeleton } from "@/components/ui/skeleton"

/**
 * Default number of ghost rows rendered by DataTableSkeleton.
 * Extracted as a constant for easy tuning later (e.g. from user prefs or viewport height).
 */
export const DEFAULT_SKELETON_ROWS = 8

/**
 * Pre-defined width patterns for skeleton cells.
 * Cycling through these creates a realistic varied appearance (Fiori Ghost Loading style).
 */
const CELL_WIDTH_PATTERNS = [
    ["w-32", "w-20", "w-16", "w-24", "w-12"],
    ["w-40", "w-16", "w-12", "w-20", "w-28"],
    ["w-24", "w-28", "w-20", "w-16", "w-24"],
    ["w-36", "w-12", "w-24", "w-20", "w-16"],
    ["w-28", "w-24", "w-16", "w-28", "w-20"],
    ["w-32", "w-20", "w-28", "w-12", "w-24"],
    ["w-20", "w-32", "w-12", "w-24", "w-16"],
    ["w-36", "w-16", "w-20", "w-28", "w-12"],
]

interface DataTableSkeletonProps {
    /** Number of skeleton rows to render. */
    rows?: number
    /** Number of columns to simulate. */
    columns?: number
    /** Show checkbox column placeholder. */
    showCheckbox?: boolean
    /** Show prefix icon placeholder (e.g. posted status icon). */
    showPrefix?: boolean
    /** Show toolbar skeleton above the table. */
    showToolbar?: boolean
}

export function DataTableSkeleton({
    rows = DEFAULT_SKELETON_ROWS,
    columns = 5,
    showCheckbox = true,
    showPrefix = false,
    showToolbar = true,
}: DataTableSkeletonProps) {
    const effectiveCols = Math.min(columns, 8)

    return (
        <div className="flex h-full flex-col">
            {/* Toolbar skeleton */}
            {showToolbar && (
                <div className="flex items-center justify-between border-b bg-card px-4 py-2">
                    <div className="flex items-center gap-2">
                        <Skeleton className="h-8 w-20" />
                        <Skeleton className="h-8 w-24" />
                    </div>
                    <Skeleton className="h-8 w-60" />
                </div>
            )}

            {/* Table skeleton */}
            <div className="flex-1 p-0">
                <div className="flex flex-col gap-0">
                    {/* Header row */}
                    <div className="flex items-center gap-4 border-b bg-muted/70 px-4 py-3">
                        {showCheckbox && <Skeleton className="h-4 w-4 rounded" />}
                        {showPrefix && <Skeleton className="h-4 w-4 rounded-full" />}
                        {Array.from({ length: effectiveCols }).map((_, i) => (
                            <Skeleton key={`hdr-${i}`} className="h-3 w-20" />
                        ))}
                    </div>

                    {/* Data rows */}
                    {Array.from({ length: rows }).map((_, rowIdx) => {
                        const pattern = CELL_WIDTH_PATTERNS[rowIdx % CELL_WIDTH_PATTERNS.length]
                        return (
                            <div
                                key={rowIdx}
                                className="flex items-center gap-4 border-b px-4 py-3"
                            >
                                {showCheckbox && <Skeleton className="h-4 w-4 rounded" />}
                                {showPrefix && <Skeleton className="h-4 w-4 rounded-full" />}
                                {Array.from({ length: effectiveCols }).map((_, colIdx) => (
                                    <Skeleton
                                        key={`${rowIdx}-${colIdx}`}
                                        className={`h-4 ${pattern[colIdx % pattern.length]}`}
                                    />
                                ))}
                            </div>
                        )
                    })}
                </div>
            </div>
        </div>
    )
}
