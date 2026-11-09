// frontend/lib/entity-url.ts
/**
 * Canonical URL builder for entities.
 *
 * Single Source of Truth for constructing entity URLs throughout the app.
 * All entities use /catalogs/{routePrefix} and /documents/{routePrefix}.
 *
 * Next.js catch-all routes ([routePrefix]) handle entities without
 * dedicated page components, while specific routes (e.g. /catalogs/counterparties)
 * take priority for entities with custom pages.
 */

import { useMetadataStore } from "@/stores/useMetadataStore"

/**
 * Normalize routePrefix to plural form for URL consistency.
 * Backend stores singular prefixes for documents (e.g. "crypto-invoice"),
 * but URLs use plural form (e.g. "/documents/crypto-invoices").
 */
function pluralizePrefix(prefix: string): string {
  return prefix.endsWith("s") ? prefix : prefix + "s"
}

function sectionForType(type: "catalog" | "document"): string {
  return type === "document" ? "documents" : "catalogs"
}

/**
 * Build canonical URL for an entity by its registry key.
 *
 * @example
 *   buildEntityUrl("crypto_invoice")           → "/documents/crypto-invoices"
 *   buildEntityUrl("crypto_invoice", id)       → "/documents/crypto-invoices/{id}"
 *   buildEntityUrl("crypto_invoice", undefined, "new") → "/documents/crypto-invoices/new"
 *   buildEntityUrl("counterparty")            → "/catalogs/counterparties"
 *   buildEntityUrl("currency")                → "/catalogs/currencies"
 */
export function buildEntityUrl(
  entityKey: string,
  id?: string,
  action?: "new",
): string | null {
  const entity = useMetadataStore.getState().getEntity(entityKey)
  if (!entity?.routePrefix || entity.type === "register") return null

  return buildEntityUrlByRoute(entity.routePrefix, entity.type, id, action)
}

/**
 * Build canonical URL from routePrefix and entity type directly.
 * Useful in sidebar/widgets where you know the type upfront.
 *
 * All entities route through /catalogs/ or /documents/ uniformly.
 * Next.js dynamic [routePrefix] catch-all handles entities without
 * dedicated pages, while specific routes take priority automatically.
 *
 * @example
 *   buildEntityUrlByRoute("crypto-invoice", "document") → "/documents/crypto-invoices"
 *   buildEntityUrlByRoute("tokens", "catalog")         → "/catalogs/tokens"
 *   buildEntityUrlByRoute("currencies", "catalog")     → "/catalogs/currencies"
 */
export function buildEntityUrlByRoute(
  routePrefix: string,
  entityType: "catalog" | "document",
  id?: string,
  action?: "new",
): string {
  const section = sectionForType(entityType)
  const prefix = pluralizePrefix(routePrefix)

  const base = `/${section}/${prefix}`
  if (action === "new") return `${base}/new`
  if (id) return `${base}/${id}`
  return base
}

/**
 * Build list URL for a document type key (e.g. "crypto_invoice" → "/documents/crypto-invoices").
 * Returns "#" if metadata is not available.
 */
export function buildDocumentListUrl(entityKey: string): string {
  return buildEntityUrl(entityKey) ?? "#"
}

// ── Reverse URL parsing ──────────────────────────────────────────────────

/**
 * Static fallback: pluralized route segment → entity key.
 * Used when metadata store hasn't loaded yet.
 */
const _routeSegmentToEntity: Record<string, string> = {
  counterparties: "counterparty",
  organizations: "organization",
  contracts: "contract",
  currencies: "currency",
  "blockchain-networks": "blockchain_network",
  tokens: "token",
  merchants: "merchant",
  wallets: "wallet",
  "crypto-invoices": "crypto_invoice",
  "crypto-payments": "crypto_payment",
  "crypto-withdrawals": "crypto_withdrawal",
  "withdrawal-requests": "withdrawal_request",
  "treasury-withdrawals": "treasury_withdrawal",
  "crypto-sweeps": "crypto_sweep",
  "crypto-resource-rentals": "crypto_resource_rental",
  "crypto-network-operations": "crypto_network_operation",
  "crypto-provider-accounts": "crypto_provider_account",
  "crypto-provider-balance-operations": "crypto_provider_balance_operation",
}

/**
 * Parse entity key from a canonical URL.
 *
 * @example
 *   parseEntityTypeFromUrl("/catalogs/counterparties/uuid") → "counterparty"
 *   parseEntityTypeFromUrl("/documents/crypto-invoices/uuid") → "crypto_invoice"
 *   parseEntityTypeFromUrl("/settings")                      → undefined
 */
export function parseEntityTypeFromUrl(url: string): string | undefined {
  const segments = url.split("/").filter(Boolean)
  // Expected: ["catalogs"|"documents", routePrefix, ...rest]
  if (segments.length < 2) return undefined

  const section = segments[0]
  if (section !== "catalogs" && section !== "documents") return undefined

  const routeSegment = segments[1]

  // Try metadata store first (dynamic, includes extensions)
  const { byRoute } = useMetadataStore.getState()
  // byRoute is indexed by singular routePrefix (e.g. "goods-receipt")
  // URL has pluralized segment, so try both forms
  const singular = routeSegment.endsWith("s")
    ? routeSegment.slice(0, -1)
    : routeSegment
  const fromMeta = byRoute[singular] ?? byRoute[routeSegment]
  if (fromMeta) return fromMeta.key

  // Static fallback
  return _routeSegmentToEntity[routeSegment]
}
