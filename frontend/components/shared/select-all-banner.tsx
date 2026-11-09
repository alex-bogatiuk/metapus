"use client"

import { CheckCircle2 } from "lucide-react"

interface SelectAllBannerProps {
    /** Number of items selected on current page */
    selectedCount: number
    /** Total items matching filter (from API totalCount) */
    totalCount: number
    /** Whether virtual select-all is active */
    selectAllByFilter: boolean
    /** Number of items excluded from virtual select */
    excludedCount: number
    /** Callback to activate virtual select-all */
    onSelectAll: () => void
    /** Callback to deactivate (revert to page selection) */
    onClearAll: () => void
}

/**
 * Gmail-style "Select All" banner.
 *
 * Shown between toolbar and table when page items are selected but
 * not all matching items are. Offers to extend selection to ALL items
 * matching the current filter.
 *
 * States:
 * 1. Page selected → "Выбрано N на странице. Выбрать все M?"
 * 2. All by filter → "Выбраны все M (Очистить)"
 */
export function SelectAllBanner({
    selectedCount,
    totalCount,
    selectAllByFilter,
    excludedCount,
    onSelectAll,
    onClearAll,
}: SelectAllBannerProps) {
    // Don't show if no items selected or total fits on one page
    if (!selectAllByFilter && selectedCount === 0) return null
    if (!selectAllByFilter && selectedCount >= totalCount) return null
    if (totalCount <= 0) return null

    if (selectAllByFilter) {
        const effectiveCount = totalCount - excludedCount
        return (
            <div className="flex items-center justify-center gap-2 py-1.5 px-4 bg-primary/5 border-b text-sm text-muted-foreground">
                <CheckCircle2 className="h-4 w-4 text-primary" />
                <span>
                    Выбраны все{" "}
                    <strong className="text-foreground">
                        {effectiveCount.toLocaleString("ru-RU")}
                    </strong>
                    {excludedCount > 0 && (
                        <span className="text-muted-foreground">
                            {" "}(исключено: {excludedCount})
                        </span>
                    )}
                </span>
                <button
                    className="ml-2 text-primary hover:underline cursor-pointer font-medium"
                    onClick={onClearAll}
                >
                    Очистить выбор
                </button>
            </div>
        )
    }

    return (
        <div className="flex items-center justify-center gap-2 py-1.5 px-4 bg-muted/50 border-b text-sm text-muted-foreground">
            <span>
                Выбрано{" "}
                <strong className="text-foreground">
                    {selectedCount.toLocaleString("ru-RU")}
                </strong>{" "}
                на странице.
            </span>
            <button
                className="text-primary hover:underline cursor-pointer font-medium"
                onClick={onSelectAll}
            >
                Выбрать все {totalCount.toLocaleString("ru-RU")}
            </button>
        </div>
    )
}
