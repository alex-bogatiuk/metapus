import type { FilterValue } from "@/lib/schemas/filters"
import type { FilterFieldMetadata as FilterFieldMeta } from "@/lib/schemas/metadata"
/**
 * Filter metadata and list-cell context for the shared filter mechanism.
 * Mirrors backend FilterFieldMeta from GET /meta/:name/filters.
 */

export type { FilterFieldMetadata as FilterFieldMeta } from "@/lib/schemas/metadata"
export type FieldType = FilterFieldMeta["fieldType"]

/**
 * Current list cell, already resolved onto a filter field.
 * `field === null` means the column is not filterable.
 */
export interface ColumnFilterCell {
  label: string
  field: FilterFieldMeta | null
  value: FilterValue
}
