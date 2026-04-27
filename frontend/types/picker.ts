/**
 * Shared types for the picker dialog system.
 *
 * Two layers:
 *   1) GenericPickerDialog — metadata-driven, works for any entity
 *   2) ProductPickerDialog — specialized for nomenclature (category tree, stock, qty entry)
 *
 * Mirrors: no direct backend DTO — these are frontend-only orchestration types.
 */

// ── Picked item result ──────────────────────────────────────────────────

/** Result of picking an item from a picker dialog. */
export interface PickedItem {
    id: string
    name: string
    code?: string
    unitId?: string
    unitName?: string
    quantity: number
    price?: number
}

// ── Generic Picker Props ────────────────────────────────────────────────

export interface GenericPickerDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    /** API endpoint path, e.g. "/catalog/nomenclatures" */
    apiEndpoint: string
    /** Callback when user confirms selection */
    onPick: (items: PickedItem[]) => void
    /** Allow selecting multiple items. Default: true */
    multiSelect?: boolean
    /** Override auto-resolved title */
    title?: string
}

// ── Existing line (for pre-populating picker from document) ─────────────

/** A line from the existing document, used to initialize the picker quantities. */
export interface ExistingPickerLine {
    productId: string
    productName: string
    productCode?: string
    unitId?: string
    unitName?: string
    quantity: number
}

// ── Product Picker Props ────────────────────────────────────────────────

export interface ProductPickerDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    /** Callback when user confirms selection */
    onPick: (items: PickedItem[]) => void
    /** Pre-populate picker with existing document lines */
    existingLines?: ExistingPickerLine[]
    /** Pre-filter by warehouse (for stock data in V2) */
    warehouseId?: string
}
