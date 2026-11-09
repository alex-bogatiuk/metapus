import { ApiError } from "@/lib/api"

const DEFAULT_POOL_RETRY_SECONDS = 30

export interface InvoiceTokenOption {
  tokenId: string
  symbol: string
  networkId: string
  network: string
}

export interface PortalNetworkOption {
  networkId: string
  network: string
  symbols: string[]
}

export interface MerchantTokenSelection {
  merchantId: string
  networkId: string
  tokenId: string
}

/**
 * Extracts unique active blockchain networks from the merchant's active currencies list,
 * preserving backend order and aggregating supported token symbols per network.
 */
export function derivePortalNetworks(
  tokens: readonly InvoiceTokenOption[]
): PortalNetworkOption[] {
  const byId = new Map<string, PortalNetworkOption>()
  for (const token of tokens) {
    if (!token.networkId) continue
    const existing = byId.get(token.networkId)
    if (existing) {
      if (!existing.symbols.includes(token.symbol)) {
        existing.symbols.push(token.symbol)
      }
    } else {
      byId.set(token.networkId, {
        networkId: token.networkId,
        network: token.network,
        symbols: [token.symbol],
      })
    }
  }
  return Array.from(byId.values())
}

/**
 * Resolves the explicitly selected network ID for the active merchant.
 * Never auto-selects a network so the merchant always makes a conscious choice.
 */
export function resolveInvoiceNetworkId(
  networks: readonly PortalNetworkOption[],
  merchantId: string | null,
  selection: MerchantTokenSelection | null
): string {
  if (
    selection?.merchantId === merchantId &&
    selection.networkId &&
    networks.some((net) => net.networkId === selection.networkId)
  ) {
    return selection.networkId
  }
  return ""
}

/**
 * Returns only active tokens belonging to the selected blockchain network.
 */
export function filterInvoiceTokensByNetwork<T extends { networkId: string }>(
  tokens: readonly T[],
  networkId: string
): T[] {
  if (!networkId) return []
  return tokens.filter((token) => token.networkId === networkId)
}

/**
 * Resolves the explicitly selected token ID within the chosen network.
 * Strict manual selection: returns "" until the user explicitly picks a token
 * in the currently selected network.
 */
export function resolveInvoiceTokenId(
  tokens: readonly { tokenId: string; networkId: string }[],
  merchantId: string | null,
  selection: MerchantTokenSelection | null
): string {
  if (
    selection?.merchantId === merchantId &&
    selection.networkId &&
    selection.tokenId &&
    tokens.some(
      (token) =>
        token.networkId === selection.networkId &&
        token.tokenId === selection.tokenId
    )
  ) {
    return selection.tokenId
  }
  return ""
}

export function walletPoolRetryDelaySeconds<T>(error: T): number | null {
  if (!(error instanceof ApiError) || error.parsedBody?.code !== "WALLET_POOL_EXHAUSTED") {
    return null
  }

  const retryAfter = error.parsedBody.details?.retryAfterSeconds
  return typeof retryAfter === "number" && Number.isSafeInteger(retryAfter) && retryAfter > 0
    ? retryAfter
    : DEFAULT_POOL_RETRY_SECONDS
}
