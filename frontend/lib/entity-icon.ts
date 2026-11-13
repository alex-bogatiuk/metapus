// frontend/lib/entity-icon.ts
/**
 * One icon per entity and per page, shared by tabs, the section panel, the
 * command palette and preview cards — the same thing always looks the same.
 */

import type { LucideIcon } from "lucide-react"
import {
  Activity,
  ArrowDownToLine,
  ArrowLeftRight,
  ArrowUpFromLine,
  ArrowUpRight,
  Banknote,
  BarChart3,
  Bell,
  Blocks,
  BookOpen,
  Building2,
  Coins,
  CreditCard,
  Cpu,
  FileQuestion,
  FileSignature,
  FileText,
  Fuel,
  HandCoins,
  HelpCircle,
  KeyRound,
  Landmark,
  Layers,
  LayoutDashboard,
  Link2,
  ListChecks,
  Merge,
  Network,
  PiggyBank,
  Plug,
  QrCode,
  Receipt,
  Rss,
  Scale,
  ScrollText,
  Search,
  Settings,
  ShieldCheck,
  Star,
  Store,
  Trash2,
  TrendingUp,
  Users,
  Vault,
  Wallet,
  Webhook,
  Workflow,
  Zap,
} from "lucide-react"
import { useMetadataStore } from "@/stores/useMetadataStore"

/** Keyed by backend metadata key (e.g. "crypto_invoice"). */
const entityIcons: Record<string, LucideIcon> = {
  // Business
  counterparty: Users,
  organization: Building2,
  contract: FileSignature,
  merchant: Store,

  // Money and rates
  currency: Banknote,
  token: Coins,
  rate_source: Rss,
  "exchange-rate": TrendingUp,

  // Blockchain
  blockchain_network: Blocks,
  wallet: Wallet,
  crypto_provider_account: Landmark,
  integration_account: Plug,
  integration_pool: Layers,

  // Payment intake
  crypto_invoice: Receipt,
  crypto_payment: CreditCard,

  // Wallet receipts
  crypto_custody_receipt: ArrowDownToLine,
  unidentified_receipts: FileQuestion,
  crypto_merchant_credit: HandCoins,
  crypto_platform_funding: PiggyBank,
  wallet_fundings: Fuel,

  // Payouts and treasury
  withdrawal_request: ArrowUpFromLine,
  crypto_withdrawal: ArrowUpRight,
  treasury_withdrawal: Vault,
  crypto_sweep: Merge,

  // Technical operations
  crypto_resource_rental: Zap,
  crypto_network_operation: Cpu,
  crypto_provider_balance_operation: ArrowLeftRight,

  // Financial control and reports
  crypto_adjustment: Scale,
  "report-document-journal": ScrollText,
}

/**
 * Get the Lucide icon component for a given entity key.
 * Falls back to Star for unknown entity types.
 */
export function getEntityIcon(entityType: string): LucideIcon {
  return entityIcons[entityType] ?? Star
}

/** Pages that are not metadata entities, most specific first. */
const pageIcons: [prefix: string, icon: LucideIcon][] = [
  ["/finance/exchange-rates", TrendingUp],
  ["/crypto/unidentified-receipts", FileQuestion],
  ["/crypto/wallet-fundings", Fuel],
  ["/documents/crypto-adjustments", Scale],
  ["/documents/treasury-withdrawals", Vault],
  ["/documents/withdrawal-requests", ArrowUpFromLine],
  ["/reports/document-journal", ScrollText],
  ["/reports", BarChart3],
  ["/admin/event-log", ScrollText],
  ["/admin/worker-jobs", Activity],
  ["/admin/crypto-networks", Network],
  ["/admin/automation-rules", Workflow],
  ["/admin/batch-modify", ListChecks],
  ["/admin/find-references", Search],
  ["/admin/marked-objects", Trash2],
  ["/admin/merchants", Store],
  ["/admin/security-profiles", KeyRound],
  ["/admin", ShieldCheck],
  ["/settings", Settings],
  ["/notifications", Bell],
  ["/help", HelpCircle],
  ["/portal/invoices", Receipt],
  ["/portal/deposit-addresses", QrCode],
  ["/portal/withdrawals", ArrowUpFromLine],
  ["/portal/payment-links", Link2],
  ["/portal/developers/keys", KeyRound],
  ["/portal/developers/webhooks", Webhook],
  ["/portal/settings", Settings],
  ["/portal", LayoutDashboard],
]

/** "withdrawal-requests" → "withdrawal_request", "currencies" → "currency". */
function segmentToKey(segment: string): string[] {
  const key = segment.replaceAll("-", "_")
  return [key, key.replace(/ies$/, "y"), key.replace(/s$/, "")]
}

/**
 * Icon for a page by its URL, so a tab shows what it holds before its data loads.
 * NOTE: reads useMetadataStore.getState() directly (not a hook).
 */
export function getRouteIcon(url: string): LucideIcon {
  const path = url.split(/[?#]/)[0] || "/"
  if (path === "/") return LayoutDashboard

  for (const [prefix, icon] of pageIcons) {
    if (path === prefix || path.startsWith(prefix + "/")) return icon
  }

  const [section, segment] = path.split("/").filter(Boolean)
  if ((section === "catalogs" || section === "documents") && segment) {
    const meta = useMetadataStore.getState()
    const entity = meta.getEntityByRoute(segment) ?? (segment.endsWith("s") ? meta.getEntityByRoute(segment.slice(0, -1)) : undefined)
    if (entity && entityIcons[entity.key]) return entityIcons[entity.key]
    const known = segmentToKey(segment).find((key) => entityIcons[key])
    if (known) return entityIcons[known]
    return section === "catalogs" ? BookOpen : FileText
  }
  return FileText
}
