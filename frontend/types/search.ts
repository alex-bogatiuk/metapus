/**
 * Global Data Search types — matches backend search.SearchResponse / search.SearchResult.
 */

export interface SearchResultItem {
    /** Entity category: "catalog" | "document" */
    entityType: string
    /** PascalCase entity name, e.g. "Counterparty", "GoodsReceipt" */
    entityName: string
    /** snake_case entity key, e.g. "counterparty", "goods_receipt" */
    entityKey: string
    /** UUID of the matched record */
    entityId: string
    /** Display title (name for catalogs, number for documents) */
    title: string
    /** Optional secondary text (code for catalogs) */
    subtitle: string
    /** Frontend route path, e.g. "/catalogs/counterparties/{id}" */
    url: string
}

export interface SearchResponse {
    query: string
    results: SearchResultItem[]
}

/** Single label:value pair in the entity preview card. */
export interface PreviewField {
    label: string
    value: string
}

/** API response for GET /api/v1/search/preview. */
export interface PreviewResponse {
    entityType: string
    entityKey: string
    entityName: string
    title: string
    subtitle?: string
    fields: PreviewField[]
    references?: Record<string, string>
    url: string
}
