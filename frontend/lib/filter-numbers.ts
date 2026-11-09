import { minorUnitsSchema } from "@/lib/money"
import { quantityInputSchema } from "@/lib/quantity"
import { filterRangeSchema, type FilterEntry, type FilterValue } from "@/lib/schemas/filters"
import type { FilterFieldMeta } from "@/types/filter"

export function isExactFilterField(field: FilterFieldMeta): boolean {
    return field.fieldType === "money" || (field.fieldType === "number" && field.valueScale === 10000)
}

/** Validate a complete value without rounding or turning a decimal into a JS number. */
export function exactFilterValueError(field: FilterFieldMeta, value: FilterValue): string | undefined {
    if (!isExactFilterField(field) || value === undefined || value === null || value === "") return undefined
    if (field.fieldType === "money") {
        return minorUnitsSchema.safeParse(value).success
            ? undefined : "Введите целое число минимальных единиц в допустимом диапазоне"
    }
    return quantityInputSchema.safeParse(value).success ? undefined
        : "Введите количество в допустимом диапазоне: не более четырёх знаков после точки"
}

export function exactFilterEntryError(field: FilterFieldMeta, entry: FilterEntry): string | undefined {
    if (entry.operator === "null" || entry.operator === "not_null") return undefined
    if (entry.operator === "between") {
        const range = filterRangeSchema.safeParse(entry.value)
        if (!range.success) return isExactFilterField(field) ? "Укажите границы диапазона" : undefined
        return exactFilterValueError(field, range.data.from) ?? exactFilterValueError(field, range.data.to)
    }
    if (Array.isArray(entry.value)) {
        for (const value of entry.value) {
            const error = exactFilterValueError(field, value)
            if (error) return error
        }
        return undefined
    }
    return exactFilterValueError(field, entry.value)
}
