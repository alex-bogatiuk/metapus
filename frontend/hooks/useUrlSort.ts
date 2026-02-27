"use client"

import { useCallback, useMemo } from "react"
import { useRouter, useSearchParams, usePathname } from "next/navigation"
import type { SortDirection } from "@/types"

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

    const sortParams = useMemo(
        () => ({ column: sortColumn, direction: sortDirection }),
        [sortColumn, sortDirection]
    )

    return { sortColumn, sortDirection, handleSort, sortParams }
}
