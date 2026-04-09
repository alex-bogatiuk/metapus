/**
 * Entity metadata types — mirrors backend EntitySummary DTO.
 *
 * Used by useMetadataStore to provide metadata-driven labels
 * throughout the UI (sidebar, toolbar, tabs, access matrix, etc.).
 */

export type EntityType = "catalog" | "document"

export interface EntityPresentation {
    singular: string   // e.g. "Counterparty"
    plural: string     // e.g. "Counterparties"
    new?: string       // e.g. "New Counterparty"
    genitive?: string  // e.g. "of counterparty" (for delete confirmations)
}

export interface EntityMeta {
    key: string              // snake_case identifier, e.g. "counterparty", "goods_receipt"
    name: string             // PascalCase registry name, e.g. "Counterparty", "GoodsReceipt"
    type: EntityType         // "catalog" | "document"
    presentation: EntityPresentation
    routePrefix?: string     // URL path segment, e.g. "counterparties", "goods-receipt"
}
