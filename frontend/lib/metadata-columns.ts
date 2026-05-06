/**
 * metadata-columns.ts — shared utility for auto-generating table columns
 * from backend entity metadata fields.
 *
 * Used by:
 *   - ReferencePickerDialog (picker table columns)
 *   - AutoList (auto-generated list page columns)
 *
 * Eliminates duplication of field filtering, type mapping, and column
 * construction logic across components.
 */

// ── System fields excluded from auto-generated columns ──────────────────
// These are internal/audit fields that should not appear in user-facing tables.
export const SYSTEM_FIELDS_SKIP = new Set([
    "id",
    "version",
    "deletionMark",
    "attributes",
    "createdAt",
    "updatedAt",
    "createdBy",
    "updatedBy",
    "deletedAt",
    "txid",
    "postedVersion",
])

// ── Field type for display ──────────────────────────────────────────────

export type ColumnDisplayType = "string" | "number" | "money" | "date" | "boolean" | "reference"

/**
 * Maps backend metadata field type string to a display column type.
 * Used by both AutoList and ReferencePickerDialog.
 */
export function mapFieldType(backendType: string): ColumnDisplayType {
    switch (backendType) {
        case "boolean":
            return "boolean"
        case "integer":
        case "int":
        case "number":
            return "number"
        case "decimal":
        case "money":
            return "money"
        case "date":
        case "datetime":
        case "timestamp":
            return "date"
        case "reference":
        case "uuid":
        case "typed_ref":
            return "reference"
        default:
            return "string"
    }
}

// ── Metadata field shape (from GET /meta/:name) ─────────────────────────

export interface MetadataField {
    name: string
    label?: string
    type: string
    referenceType?: string
}

// ── Auto-generated column definition ────────────────────────────────────

export interface AutoColumn {
    /** JSON field key to read from row data */
    key: string
    /** Column header label */
    label: string
    /** Display type for formatting */
    type: ColumnDisplayType
    /** Text alignment */
    align?: "left" | "right" | "center"
}

/**
 * Builds table columns from backend metadata fields.
 *
 * Key behaviors:
 * - Filters out system fields (id, version, deletionMark, etc.)
 * - For reference fields: remaps key from `fooId` → `fooName` for display,
 *   because list endpoints return resolved `{fieldName}Name` alongside the UUID.
 * - Limits to `maxColumns` columns for readability.
 * - Sets `align: "right"` for numeric/money columns.
 *
 * @param fields - FieldDef[] from GET /meta/:entityName
 * @param maxColumns - max number of columns to include (default: 6)
 */
export function buildColumnsFromFields(
    fields: MetadataField[],
    maxColumns = 6,
): AutoColumn[] {
    return fields
        .filter((f) => !SYSTEM_FIELDS_SKIP.has(f.name))
        .slice(0, maxColumns)
        .map((f) => {
            const type = mapFieldType(f.type)

            // For reference fields, backend returns a resolved nested object:
            //   { merchantId: "uuid", merchant: { id: "uuid", name: "Test Merchant" } }
            // We use the nested object key (without "Id" suffix) as the column key,
            // and formatCellValue handles extracting `.name` from the object.
            let key = f.name
            if (type === "reference" && key.endsWith("Id")) {
                key = key.slice(0, -2)
            }

            return {
                key,
                label: f.label ?? f.name,
                type,
                align: (type === "number" || type === "money") ? "right" as const : undefined,
            }
        })
}

/**
 * Formats a cell value for display based on column type.
 * Shared across AutoList and ReferencePickerDialog.
 */
export function formatCellValue(value: unknown, type: ColumnDisplayType): string {
    if (value === null || value === undefined) return "—"

    // Reference fields: backend returns nested RefDisplay { id, name, code? }
    if (type === "reference") {
        if (typeof value === "object" && value !== null && "name" in value) {
            return String((value as { name: string }).name)
        }
        // Fallback for flat string values (e.g., pre-resolved names)
        if (typeof value === "string") return value
        return "—"
    }

    switch (type) {
        case "boolean":
            return value ? "✓" : "—"
        case "date":
            if (typeof value === "string") {
                try {
                    return new Date(value).toLocaleDateString("ru-RU")
                } catch {
                    return String(value)
                }
            }
            return String(value)
        case "money":
            if (typeof value === "number") {
                return value.toLocaleString("ru-RU", { minimumFractionDigits: 2 })
            }
            return String(value)
        case "number":
            if (typeof value === "number") {
                return value.toLocaleString("ru-RU")
            }
            return String(value)
        default:
            return String(value)
    }
}
