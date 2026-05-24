// frontend/lib/blockchain.ts
// Shared blockchain utilities for the merchant portal.
// Extracted from invoices, withdrawals, and invoice detail pages.

/** Network name → Tailwind dot color class. */
export const NETWORK_COLORS: Record<string, string> = {
  tron: "bg-red-500",
  ethereum: "bg-blue-500",
  bsc: "bg-yellow-500",
  solana: "bg-purple-500",
  polygon: "bg-violet-500",
  bitcoin: "bg-orange-500",
}

/** Returns the Tailwind background class for a network's indicator dot. */
export function getNetworkColor(network: string): string {
  const lower = network.toLowerCase()
  for (const [key, cls] of Object.entries(NETWORK_COLORS)) {
    if (lower.includes(key)) return cls
  }
  return "bg-muted-foreground"
}

/** Returns a block explorer URL for a transaction hash, or null if network is unknown. */
export function getExplorerUrl(
  txHash: string,
  network: string
): string | null {
  const lower = network.toLowerCase()
  if (lower.includes("tron"))
    return `https://tronscan.org/#/transaction/${txHash}`
  if (lower.includes("ethereum")) return `https://etherscan.io/tx/${txHash}`
  if (lower.includes("bsc")) return `https://bscscan.com/tx/${txHash}`
  if (lower.includes("polygon")) return `https://polygonscan.com/tx/${txHash}`
  if (lower.includes("solana")) return `https://solscan.io/tx/${txHash}`
  return null
}

/**
 * Truncate a blockchain address for display: `0x1234…abcd`.
 * @param addr     Full address string (empty/undefined → "—")
 * @param prefixLen Characters to show at the start (default 6)
 * @param suffixLen Characters to show at the end (default 4)
 */
export function truncateAddress(
  addr: string | undefined,
  prefixLen = 6,
  suffixLen = 4
): string {
  if (!addr) return "—"
  if (addr.length <= prefixLen + suffixLen + 2) return addr
  return `${addr.slice(0, prefixLen)}…${addr.slice(-suffixLen)}`
}
