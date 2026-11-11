import type { JsonObject, JsonValue } from "@/lib/json-value"
/**
 * metadata-columns.ts — shared utility for auto-generating table columns
 * from backend entity metadata fields.
 *
 * Used by:
 *   - ReferencePickerDialog (picker table columns)
 *   - AutoList (auto-generated list page columns)
 *
 * List pages merge metadata (available columns) with an optional overlay
 * (render / width / default visibility). Overlay never replaces the catalog.
 */

import { z } from "zod"
import { minorUnitsSchema, formatRawMinor } from "@/lib/money"
import type { AutoListColumn } from "@/lib/entity-registry"

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
    /** When true, the field stays on the form but is omitted from list choosers. */
    listHidden?: boolean
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
 * - For reference fields: remaps key from `fooId` → `foo` for display,
 *   because list endpoints return a nested `{ id, name }` object.
 * - Limits to `maxColumns` columns for readability (pickers).
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
        .filter((f) => !SYSTEM_FIELDS_SKIP.has(f.name) && !f.listHidden)
        .slice(0, maxColumns)
        .map((f) => {
            const type = mapFieldType(f.type)
            return {
                key: listKeyForField(f),
                label: f.label ?? f.name,
                type,
                align: (type === "number" || type === "money") ? "right" as const : undefined,
            }
        })
}

/** List JSON key for a metadata field (reference `fooId` → nested `foo`). */
export function listKeyForField(field: MetadataField): string {
    const type = mapFieldType(field.type)
    if (type === "reference" && field.name.endsWith("Id")) {
        return field.name.slice(0, -2)
    }
    return field.name
}

/**
 * Map overlay/saved keys like `statusName` onto the canonical metadata key
 * (`status`) when that key exists in the available set.
 */
export function canonicalizeColumnKey(key: string, availableKeys: Iterable<string>): string {
    const available = availableKeys instanceof Set ? availableKeys : new Set(availableKeys)
    if (available.has(key)) return key
    if (key.endsWith("Name")) {
        const base = key.slice(0, -4)
        if (base && available.has(base)) return base
    }
    return key
}

export function canonicalizeColumnKeys(keys: string[], availableKeys: Iterable<string>): string[] {
    const available = availableKeys instanceof Set ? availableKeys : new Set(availableKeys)
    const seen = new Set<string>()
    const result: string[] = []
    for (const key of keys) {
        const canonical = canonicalizeColumnKey(key, available)
        if (seen.has(canonical)) continue
        seen.add(canonical)
        result.push(canonical)
    }
    return result
}

export function defaultVisibleKeysFor(
    entityType: "catalog" | "document",
    columns: { key: string }[],
): string[] {
    const preferred = entityType === "document" ? ["number", "date"] : ["code", "name"]
    const available = new Set(columns.map((c) => c.key))
    const keys = preferred.filter((key) => available.has(key))
    if (keys.length > 0) return keys
    return columns.slice(0, 4).map((c) => c.key)
}

export function resolveDefaultVisibleKeys(
    overlayKeys: string[] | undefined,
    columns: { key: string }[],
    entityType: "catalog" | "document",
): string[] {
    if (overlayKeys && overlayKeys.length > 0) {
        const available = columns.map((c) => c.key)
        const availableSet = new Set(available)
        return canonicalizeColumnKeys(overlayKeys, availableSet).filter((key) => availableSet.has(key))
    }
    return defaultVisibleKeysFor(entityType, columns)
}

export interface MergeListColumnsOptions {
    fields: MetadataField[]
    overlay?: AutoListColumn[]
    entityName: string
}

function isSkippedListField(field: MetadataField): boolean {
    if (SYSTEM_FIELDS_SKIP.has(field.name) || field.listHidden) return true
    // JSON blobs are unreadable as columns unless an overlay supplies a renderer.
    return field.type === "json"
}

function columnFromField(field: MetadataField, entityName: string): AutoListColumn {
    const type = mapFieldType(field.type)
    const isRef = type === "reference"
    return {
        key: listKeyForField(field),
        label: field.label ?? field.name,
        type,
        align: (type === "number" || type === "money") ? "right" : undefined,
        sortable: !isRef,
        enumEntity: field.type === "enum" ? entityName : undefined,
        filterKey: field.name,
    }
}

function applyOverlay(base: AutoListColumn, overlay: AutoListColumn): AutoListColumn {
    return {
        ...base,
        label: overlay.label || base.label,
        type: overlay.type ?? base.type,
        width: overlay.width ?? base.width,
        minWidth: overlay.minWidth ?? base.minWidth,
        sortable: overlay.sortable ?? base.sortable,
        align: overlay.align ?? base.align,
        className: overlay.className ?? base.className,
        render: overlay.render ?? base.render,
        enumEntity: overlay.enumEntity ?? base.enumEntity,
        refEndpoint: overlay.refEndpoint ?? base.refEndpoint,
        filterKey: overlay.filterKey ?? base.filterKey,
    }
}

/**
 * Builds the full list-column catalog: metadata fields first, overlay as
 * presentation (render / width / label) merged by canonical key.
 */
export function mergeListColumns(options: MergeListColumnsOptions): AutoListColumn[] {
    const { fields, overlay, entityName } = options
    const metaColumns: AutoListColumn[] = []
    const seenMeta = new Set<string>()
    for (const field of fields) {
        if (isSkippedListField(field)) continue
        const col = columnFromField(field, entityName)
        if (seenMeta.has(col.key)) continue
        seenMeta.add(col.key)
        metaColumns.push(col)
    }

    const metaByKey = new Map(metaColumns.map((col) => [col.key, col]))
    const used = new Set<string>()
    const result: AutoListColumn[] = []

    for (const overlayCol of overlay ?? []) {
        const canonical = canonicalizeColumnKey(overlayCol.key, metaByKey.keys())
        const metaCol = metaByKey.get(canonical)
        if (metaCol) {
            if (used.has(canonical)) continue
            result.push(applyOverlay(metaCol, overlayCol))
            used.add(canonical)
            continue
        }
        if (used.has(overlayCol.key)) continue
        result.push(overlayCol)
        used.add(overlayCol.key)
    }

    for (const col of metaColumns) {
        if (used.has(col.key)) continue
        result.push(col)
        used.add(col.key)
    }

    return result
}

/** Read a row value, including dotted keys (`attributes.foo`). */
export function getFieldValue(item: JsonObject, key: string): JsonValue | undefined {
    if (!key.includes(".")) return item[key]
    let current: JsonValue | undefined = item
    for (const part of key.split(".")) {
        if (current === null || current === undefined || typeof current !== "object" || Array.isArray(current)) {
            return undefined
        }
        current = current[part]
    }
    return current
}

/**
 * Formats a cell value for display based on column type.
 * Shared across AutoList and ReferencePickerDialog.
 */
export function formatCellValue<T>(value: T, type: ColumnDisplayType, row?: object): string {
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
        case "money": {
            const amount = minorUnitsSchema.parse(value)
            const unit = z.object({ currency: z.object({ decimalPlaces: z.int().min(0).max(18), symbol: z.string().optional() }) }).safeParse(row)
            if (!unit.success) return "—"
            return `${formatRawMinor(amount, unit.data.currency.decimalPlaces)} ${unit.data.currency.symbol ?? ""}`.trim()
        }
        case "number":
            if (typeof value === "number") {
                return value.toLocaleString("ru-RU")
            }
            return String(value)
        default:
            return String(value)
    }
}
