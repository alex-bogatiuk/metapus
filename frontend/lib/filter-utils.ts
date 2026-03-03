/**
 * Filter utilities for building backend-compatible filter items
 * from the frontend sidebar widget state.
 *
 * Each filter entry carries an explicit operator (ComparisonOperator)
 * and a typed value, enabling per-field comparison type selection.
 */

import type { AdvancedFilterItem, ComparisonOperator } from "@/types/common"
import type { FieldType, FilterFieldMeta } from "@/components/shared/filter-config-dialog"

/**
 * Reserved FilterValues key used by the built-in period range picker.
 * Must match the constant exported from filter-sidebar.tsx.
 */
export const PERIOD_FILTER_KEY = "__period__"

// ── Key Mapping ─────────────────────────────────────────────────────────

/** Convert camelCase to snake_case: "supplierId" → "supplier_id" */
export function camelToSnake(key: string): string {
    return key.replace(/[A-Z]/g, (letter) => `_${letter.toLowerCase()}`)
}

/** Format JS Date to "YYYY-MM-DD" using LOCAL time to avoid timezone offset bugs */
export function formatLocalYYYYMMDD(d: Date): string {
    const year = d.getFullYear()
    const month = String(d.getMonth() + 1).padStart(2, "0")
    const day = String(d.getDate()).padStart(2, "0")
    return `${year}-${month}-${day}`
}

// ── Operator definitions per field type ─────────────────────────────────

export interface OperatorOption {
    value: ComparisonOperator
    label: string
}

const REF_OPERATORS: OperatorOption[] = [
    { value: "eq", label: "Равно" },
    { value: "neq", label: "Не равно" },
    { value: "in", label: "В списке" },
    { value: "nin", label: "Не в списке" },
    { value: "null", label: "Не заполнено" },
    { value: "not_null", label: "Заполнено" },
]

const STRING_OPERATORS: OperatorOption[] = [
    { value: "contains", label: "Содержит" },
    { value: "ncontains", label: "Не содержит" },
    { value: "eq", label: "Равно" },
    { value: "neq", label: "Не равно" },
    { value: "null", label: "Не заполнено" },
    { value: "not_null", label: "Заполнено" },
]

const NUMBER_OPERATORS: OperatorOption[] = [
    { value: "eq", label: "Равно" },
    { value: "neq", label: "Не равно" },
    { value: "gt", label: "Больше" },
    { value: "gte", label: "Больше или равно" },
    { value: "lt", label: "Меньше" },
    { value: "lte", label: "Меньше или равно" },
    { value: "null", label: "Не заполнено" },
    { value: "not_null", label: "Заполнено" },
]

const DATE_OPERATORS: OperatorOption[] = [
    { value: "eq", label: "Равно" },
    { value: "neq", label: "Не равно" },
    { value: "gt", label: "Больше" },
    { value: "gte", label: "Больше или равно" },
    { value: "lt", label: "Меньше" },
    { value: "lte", label: "Меньше или равно" },
    { value: "null", label: "Не заполнено" },
    { value: "not_null", label: "Заполнено" },
]

const BOOLEAN_OPERATORS: OperatorOption[] = [
    { value: "eq", label: "Равно" },
]

const ENUM_OPERATORS: OperatorOption[] = [
    { value: "eq", label: "Равно" },
    { value: "neq", label: "Не равно" },
    { value: "in", label: "В списке" },
    { value: "nin", label: "Не в списке" },
    { value: "null", label: "Не заполнено" },
    { value: "not_null", label: "Заполнено" },
]

/** Get available operators for a field type. */
export function getOperatorsForType(fieldType: FieldType): OperatorOption[] {
    switch (fieldType) {
        case "reference": return REF_OPERATORS
        case "string": return STRING_OPERATORS
        case "number": return NUMBER_OPERATORS
        case "date": return DATE_OPERATORS
        case "boolean": return BOOLEAN_OPERATORS
        case "enum": return ENUM_OPERATORS
        default: return STRING_OPERATORS
    }
}

/** Get the default operator for a field type. */
export function getDefaultOperator(fieldType: FieldType): ComparisonOperator {
    switch (fieldType) {
        case "reference": return "eq"
        case "string": return "contains"
        case "number": return "eq"
        case "date": return "eq"
        case "boolean": return "eq"
        case "enum": return "eq"
        default: return "contains"
    }
}

/** Operators that require no value input. */
export function isNullaryOperator(op: ComparisonOperator): boolean {
    return op === "null" || op === "not_null"
}

/** Operators that accept a list of values (multi-select). */
export function isListOperator(op: ComparisonOperator): boolean {
    return op === "in" || op === "nin"
}

// ── Filter Entry ────────────────────────────────────────────────────────

/**
 * A single filter entry carrying an explicit operator and typed value.
 * This is the unit stored in FilterValues.
 */
export interface FilterEntry {
    operator: ComparisonOperator
    /** string, string[], boolean, number, { from?: string; to?: string }, etc. */
    value: unknown
}

/**
 * All filter entries tracked by FilterSidebar.
 * Keyed by the filter field key (camelCase, e.g. "warehouseId").
 */
export type FilterValues = Record<string, FilterEntry>

// ── Builder ─────────────────────────────────────────────────────────────

/**
 * Convert sidebar filter entries into AdvancedFilterItem[] ready for API.
 *
 * - camelCase keys are converted to snake_case
 * - Nullary operators (null/not_null) emit items without value
 * - List operators (in/nin) emit items with array values
 * - Empty / undefined entries are skipped
 * - The reserved __period__ key is mapped to periodField (e.g. "date")
 *
 * @param values      Current filter state from FilterSidebar.
 * @param _fieldsMeta Field metadata (reserved for future use).
 * @param periodField DB column name for the built-in period filter (e.g. "date").
 */
export function buildFilterItems(
    values: FilterValues,
    _fieldsMeta: FilterFieldMeta[],
    periodField?: string
): AdvancedFilterItem[] {
    const items: AdvancedFilterItem[] = []

    for (const [key, entry] of Object.entries(values)) {
        if (!entry) continue
        const { operator, value } = entry

        // Map the reserved period key to the actual DB column
        const dbField = key === PERIOD_FILTER_KEY
            ? (periodField ?? "date")
            : camelToSnake(key)

        // Look up the field logic type from metadata
        let fieldType: string | undefined
        if (key === PERIOD_FILTER_KEY) {
            fieldType = "date"
        } else {
            const meta = _fieldsMeta.find(m => m.key === key)
            fieldType = meta?.fieldType
        }

        // Nullary operators — no value required
        if (isNullaryOperator(operator)) {
            items.push({ field: dbField, fieldType, operator })
            continue
        }

        // Skip empty values
        if (value === undefined || value === null || value === "") continue

        // List operators — value must be a non-empty array
        if (isListOperator(operator)) {
            if (Array.isArray(value) && value.length > 0) {
                // Extract IDs if value contains {id, name} objects (ReferenceOption[])
                const ids = value.map((v: unknown) =>
                    typeof v === "object" && v !== null && "id" in v
                        ? (v as { id: string }).id
                        : v
                )
                items.push({ field: dbField, fieldType, operator, value: ids })
            }
            continue
        }

        // Boolean: convert string "true"/"false" to actual boolean
        if (typeof value === "string" && (value === "true" || value === "false")) {
            items.push({ field: dbField, fieldType, operator, value: value === "true" })
            continue
        }
        if (typeof value === "boolean") {
            items.push({ field: dbField, fieldType, operator, value })
            continue
        }

        // Reference single-select: value is { id, name } — extract id
        if (typeof value === "object" && value !== null && "id" in value && "name" in value) {
            const id = (value as { id: string }).id
            if (id) {
                items.push({ field: dbField, fieldType, operator, value: id })
            }
            continue
        }

        // Date period range: value is { from?, to? } — emit two filter items
        if (typeof value === "object" && value !== null && ("from" in value || "to" in value)) {
            const range = value as { from?: string; to?: string }
            if (range.from) {
                items.push({ field: dbField, fieldType, operator: "gte", value: range.from })
            }
            if (range.to) {
                items.push({ field: dbField, fieldType, operator: "lte", value: range.to })
            }
            continue
        }

        // Scalar values (string, number)
        if (typeof value === "string" && value.trim()) {
            items.push({ field: dbField, fieldType, operator, value: value.trim() })
        } else if (typeof value === "number") {
            items.push({ field: dbField, fieldType, operator, value })
        }
    }

    return items
}

/**
 * Check if any filter entries are active.
 */
export function hasActiveFilters(values: FilterValues): boolean {
    return Object.values(values).some((entry) => {
        if (!entry) return false
        if (isNullaryOperator(entry.operator)) return true
        const v = entry.value
        if (v === undefined || v === null || v === "") return false
        if (Array.isArray(v)) return v.length > 0
        // Reference { id, name } — active if id is non-empty
        if (typeof v === "object" && v !== null && "id" in v) {
            return !!(v as { id: string }).id
        }
        // Date period { from?, to? } — active if at least one bound is set
        if (typeof v === "object" && v !== null && ("from" in v || "to" in v)) {
            const r = v as { from?: string; to?: string }
            return !!(r.from || r.to)
        }
        return true
    })
}
