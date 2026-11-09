import type { JsonObject } from "@/lib/json-value"
import { exactFilterEntryError } from "@/lib/filter-numbers"
/**
 * Column-context list filter (1C current-cell → отбор).
 *
 * List column keys (network, statusName) are not always filter field keys
 * (networkId, status). Prefer column.filterKey; suffix heuristics are fallback.
 */

import type { FilterFieldMeta } from "@/types/filter"
import {
    getDefaultOperator,
    indexFilterFields,
    isBetweenOperator,
    isListOperator,
    isNullaryOperator,
    type FilterEntry,
} from "@/lib/filter-utils"
import type { ComparisonOperator } from "@/types/common"
import { getFieldValue } from "@/lib/metadata-columns"
import { filterValueSchema, filterRangeSchema, type FilterValue } from "@/lib/schemas/filters"

export interface RefFilterValue {
    id: string
    name: string
}

export type ListColumnFilterRef = {
    key: string
    filterKey?: string
}

function candidateFilterKeys(columnKey: string): string[] {
    const keys = [columnKey]
    if (columnKey.endsWith("Name") && columnKey.length > 4) {
        keys.push(columnKey.slice(0, -4))
    }
    if (!columnKey.endsWith("Id")) {
        keys.push(`${columnKey}Id`)
    }
    return keys
}

/** Resolve a visible list column to a filterable field, or null if none. */
export function resolveFilterFieldFromColumn(
    column: string | ListColumnFilterRef,
    fieldsMeta: FilterFieldMeta[],
): FilterFieldMeta | null {
    const col = typeof column === "string" ? { key: column } : column
    if (!col.key && !col.filterKey) return null
    const map = indexFilterFields(fieldsMeta)
    const preferred = col.filterKey || col.key
    const direct = map.get(preferred)
    if (direct) return direct
    for (const key of candidateFilterKeys(col.key)) {
        const field = map.get(key)
        if (field) return field
    }
    return null
}

function isRefDisplay<T>(value: T): value is T & RefFilterValue {
    if (typeof value !== "object" || value === null) return false
    return "id" in value && typeof value.id === "string" && value.id.length > 0 && "name" in value && typeof value.name === "string"
}

function refName<T>(value: T, fallback: string): string {
    if (typeof value === "object" && value !== null) {
        if ("name" in value && typeof value.name === "string" && value.name) return value.name
        if ("code" in value && typeof value.code === "string" && value.code) return value.code
    }
    return fallback
}

function nestedKeyForIdField(fieldKey: string, columnKey: string): string {
    if (fieldKey.endsWith("Id") && fieldKey.length > 2) return fieldKey.slice(0, -2)
    return columnKey
}

function extractReferenceValue(
    item: JsonObject,
    columnKey: string,
    field: FilterFieldMeta,
): RefFilterValue | undefined {
    const nested = getFieldValue(item, nestedKeyForIdField(field.key, columnKey))
    if (isRefDisplay(nested)) {
        return { id: nested.id, name: refName(nested, nested.id) }
    }

    const byColumn = getFieldValue(item, columnKey)
    if (isRefDisplay(byColumn)) {
        return { id: byColumn.id, name: refName(byColumn, byColumn.id) }
    }

    const byField = getFieldValue(item, field.key)
    if (typeof byField === "string" && byField) {
        return { id: byField, name: refName(nested, byField) }
    }
    return undefined
}

function extractScalar(item: JsonObject, columnKey: string, fieldKey: string): FilterValue {
    const primary = getFieldValue(item, fieldKey)
    if (primary !== undefined && primary !== null && primary !== "") return filterValueSchema.safeParse(primary).data
    return filterValueSchema.safeParse(getFieldValue(item, columnKey)).data
}

/** Read a filter-ready value from the clicked cell. */
export function extractFilterValue(
    item: JsonObject,
    columnKey: string,
    field: FilterFieldMeta,
): FilterValue {
    switch (field.fieldType) {
        case "reference":
            return extractReferenceValue(item, columnKey, field)
        case "boolean": {
            const value = extractScalar(item, columnKey, field.key)
            return typeof value === "boolean" ? value : undefined
        }
        case "money": {
            const value = extractScalar(item, columnKey, field.key)
            return typeof value === "string" ? value : undefined
        }
        case "number": {
            const value = extractScalar(item, columnKey, field.key)
            if (field.valueScale === 10000 && typeof value === "string") return value
            if (typeof value === "number" && Number.isFinite(value)) return value
            if (typeof value === "string" && value.trim()) {
                const parsed = Number(value)
                return Number.isFinite(parsed) ? parsed : undefined
            }
            return undefined
        }
        case "date": {
            const value = extractScalar(item, columnKey, field.key)
            return typeof value === "string" && value ? value.slice(0, 10) : undefined
        }
        default: {
            const value = extractScalar(item, columnKey, field.key)
            if (value === undefined || value === null) return undefined
            if (typeof value === "object") return undefined
            const text = String(value).trim()
            return text === "" || text === "—" ? undefined : text
        }
    }
}

export function buildColumnFilterEntry(field: FilterFieldMeta, value: FilterValue): FilterEntry {
    return {
        operator: getDefaultOperator(field.fieldType),
        value,
    }
}

export function isColumnFilterValueReady(
    field: FilterFieldMeta,
    value: FilterValue,
    operator?: ComparisonOperator,
): boolean {
    if (exactFilterEntryError(field, { operator: operator ?? "eq", value })) return false
    if (operator && isNullaryOperator(operator)) return true
    if (operator && isListOperator(operator)) {
        return Array.isArray(value) && value.length > 0
    }
    if (operator && isBetweenOperator(operator)) {
        if (typeof value !== "object" || value === null) return false
        const range = filterRangeSchema.safeParse(value).data ?? {}
        const filled = (v: FilterValue) => v !== undefined && v !== null && v !== ""
        return filled(range.from) || filled(range.to)
    }
    if (value === undefined || value === null || value === "") return false
    if (field.fieldType === "boolean") return typeof value === "boolean"
    if (field.fieldType === "reference") {
        return isRefDisplay(value)
    }
    if (typeof value === "string") return value.trim().length > 0
    if (typeof value === "number") return Number.isFinite(value)
    return true
}
