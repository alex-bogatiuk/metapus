"use client"

import { useCallback, useMemo } from "react"
import { useRouter, useSearchParams, usePathname } from "next/navigation"
import type { SortDirection } from "@/types"

/** Convert camelCase to snake_case (e.g. "totalAmount" → "total_amount"). */
function toSnakeCase(s: string): string {
    return s.replace(/[A-Z]/g, (ch) => "_" + ch.toLowerCase())
}

/**
 * Build backend `orderBy` value from frontend sort state.
 * Returns undefined when no column is selected (use server default).
 *
 * Examples:
 *   ("date", "desc")        → "-date"
 *   ("totalAmount", "asc")  → "total_amount"
 *   (null, "asc")           → undefined
 */
export function buildOrderBy(
    column: string | null,
    direction: SortDirection,
): string | undefined {
    if (!column) return undefined
    const snakeCol = toSnakeCase(column)
    return direction === "desc" ? `-${snakeCol}` : snakeCol
}

/**
 * Hook that stores sort state (column + direction) in URL search params.
 *
 * URL format: `?sort=name&dir=asc`
 *
 * This ensures:
 *  - Back/Forward buttons restore the sort state
 *  - Shareable links preserve sorting
 */
export function useUrlSort() {
    const router = useRouter()
    const pathname = usePathname()
    const searchParams = useSearchParams()

    const sortColumn = searchParams.get("sort") ?? null
    const sortDirection: SortDirection =
        (searchParams.get("dir") as SortDirection) === "desc" ? "desc" : "asc"

    const handleSort = useCallback(
        (key: string) => {
            const params = new URLSearchParams(searchParams.toString())

            if (sortColumn === key) {
                // Toggle direction
                params.set("dir", sortDirection === "asc" ? "desc" : "asc")
            } else {
                params.set("sort", key)
                params.set("dir", "asc")
            }

            router.replace(`${pathname}?${params.toString()}`, { scroll: false })
        },
        [router, pathname, searchParams, sortColumn, sortDirection]
    )

    /** Backend-ready orderBy string (e.g. "-date", "name"). undefined = use server default. */
    const orderBy = useMemo(
        () => buildOrderBy(sortColumn, sortDirection),
        [sortColumn, sortDirection]
    )

    const sortParams = useMemo(
        () => ({ column: sortColumn, direction: sortDirection }),
        [sortColumn, sortDirection]
    )

    return { sortColumn, sortDirection, handleSort, orderBy, sortParams }
}
