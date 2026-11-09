import { useMetadataStore } from "@/stores/useMetadataStore"
import type { AdvancedFilterItem } from "@/types/common"
import type { EntityMeta, EntityType } from "@/types/metadata"
import { buildEntityUrlByRoute } from "@/lib/entity-url"

/** Query params for catalog/document list requests made by reference pickers. */
export interface ReferenceListParams {
  limit?: number | string
  search?: string
  orderBy?: string
  after?: string
  filter?: AdvancedFilterItem[]
}

/**
 * Builds a list URL for a reference picker. The base endpoint stays a plain
 * catalog/document path so GET-by-id and "open record" navigation keep working;
 * filters are applied only to list queries.
 */
export function withReferenceListParams(apiEndpoint: string, params: ReferenceListParams): string {
  const qs = new URLSearchParams()
  if (params.limit !== undefined) qs.set("limit", String(params.limit))
  if (params.search?.trim()) qs.set("search", params.search.trim())
  if (params.orderBy) qs.set("orderBy", params.orderBy)
  if (params.after) qs.set("after", params.after)
  if (params.filter && params.filter.length > 0) {
    qs.set("filter", JSON.stringify(params.filter))
  }
  const encoded = qs.toString()
  return encoded ? `${apiEndpoint}?${encoded}` : apiEndpoint
}

/**
 * Resolves an API endpoint + entity ID to a frontend page URL.
 *
 * Convention:
 *   API endpoint: "/catalog/{routePrefix}"  → Frontend: "/catalogs/{routePrefix}/{id}"
 *   API endpoint: "/document/{routePrefix}" → Frontend: "/documents/{routePrefix}/{id}"
 *
 * Returns null if the endpoint doesn't match or entity is not in metadata.
 */
export function resolveReferenceUrl(apiEndpoint: string, id: string): string | null {
  if (!id || !apiEndpoint) return null

  const match = apiEndpoint.match(/\/(catalog|document)\/(.+)$/)
  if (!match) return null

  const entityType = match[1] // "catalog" | "document"
  if (entityType !== "catalog" && entityType !== "document") return null
  const routePrefix = match[2]
  // Verify entity exists in metadata (safety check)
  const entity = useMetadataStore.getState().getEntityByRoute(routePrefix)
  if (!entity) return null

  return buildEntityUrlByRoute(entity.routePrefix ?? routePrefix, entityType, id)
}

// ── Endpoint → EntityMeta resolution ────────────────────────────────────

export interface ResolvedEndpoint {
  entity: EntityMeta
  entityType: EntityType
  /** PascalCase registry name, e.g. "Counterparty", "CryptoInvoice" */
  entityName: string
}

/**
 * Resolves an API endpoint (e.g. "/catalog/counterparties" or "/document/crypto-invoices")
 * to full EntityMeta + entityType.
 *
 * Supports both catalog and document endpoints:
 *   /catalog/{routePrefix}  → entityType = "catalog"
 *   /document/{routePrefix} → entityType = "document"
 *
 * Returns null if the endpoint format is unrecognized or entity is not in metadata.
 * Used by ReferencePickerDialog to auto-discover metadata without any configuration.
 */
export function resolveEntityFromEndpoint(apiEndpoint: string): ResolvedEndpoint | null {
  if (!apiEndpoint) return null

  const match = apiEndpoint.match(/\/(catalog|document)\/(.+)$/)
  if (!match) return null

  const entityType = match[1]
  if (entityType !== "catalog" && entityType !== "document") return null
  const routePrefix = match[2]

  const entity = useMetadataStore.getState().getEntityByRoute(routePrefix)
  if (!entity) return null

  return { entity, entityType, entityName: entity.name }
}
