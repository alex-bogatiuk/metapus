"use client"

import type { JsonObject } from "@/lib/json-value"

import { useMemo } from "react"
import type { Column } from "@/components/shared/data-table"
import type { ColumnFilterCell, FilterFieldMeta } from "@/types/filter"
import {
    extractFilterValue,
    resolveFilterFieldFromColumn,
} from "@/lib/list-column-filter"

interface UseColumnFilterContextOptions<T extends { id: string }> {
    focusedColumnKey: string | null
    focusedId: string | null
    items: T[]
    columns: Column<T>[]
    fieldsMeta: FilterFieldMeta[]
}

export function useColumnFilterContext<T extends { id: string }>({
    focusedColumnKey,
    focusedId,
    items,
    columns,
    fieldsMeta,
}: UseColumnFilterContextOptions<T>): ColumnFilterCell | null {
    return useMemo(() => {
        if (!focusedColumnKey) return null
        const column = columns.find((col) => col.key === focusedColumnKey)
        const label = column?.label ?? focusedColumnKey
        const field = resolveFilterFieldFromColumn(
            column ?? { key: focusedColumnKey },
            fieldsMeta,
        )
        const item = items.find((row) => row.id === focusedId)
        const value = field && item
            ? extractFilterValue(item as JsonObject, focusedColumnKey, field)
            : undefined
        return { label, field, value }
    }, [columns, fieldsMeta, focusedColumnKey, focusedId, items])
}
