import { useMetadataStore } from "@/stores/useMetadataStore"
import type { EntityMeta, EntityType } from "@/types/metadata"

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
  const routePrefix = match[2]
  // Verify entity exists in metadata (safety check)
  const entity = useMetadataStore.getState().getEntityByRoute(routePrefix)
  if (!entity) return null

  const frontendPrefix = entityType === "document" ? "documents" : "catalogs"
  return `/${frontendPrefix}/${routePrefix}/${id}`
}

// ── Endpoint → EntityMeta resolution ────────────────────────────────────

export interface ResolvedEndpoint {
  entity: EntityMeta
  entityType: EntityType
  /** PascalCase registry name, e.g. "Counterparty", "GoodsReceipt" */
  entityName: string
}

/**
 * Resolves an API endpoint (e.g. "/catalog/counterparties" or "/document/goods-receipt")
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

  const entityType = match[1] as EntityType
  const routePrefix = match[2]

  const entity = useMetadataStore.getState().getEntityByRoute(routePrefix)
  if (!entity) return null

  return { entity, entityType, entityName: entity.name }
}
