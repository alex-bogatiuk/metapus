// frontend/lib/entity-icon.ts
/**
 * Shared mapping from entity type key to Lucide icon.
 *
 * Used by FavoritesPopover, RecentPopover, and any future component
 * that needs to display an icon representing an entity type.
 */

import type { LucideIcon } from "lucide-react"
import {
  Users,
  Building2,
  Star,
  FileText,
  Bitcoin,
  Coins,
  Store,
  Wallet,
  Receipt,
  CreditCard,
  ArrowUpRight,
  RefreshCw,
  Gauge,
  Landmark,
} from "lucide-react"

const _entityIconMap: Record<string, LucideIcon> = {
  counterparty:       Users,
  organization:       Building2,
  contract:           FileText,
  currency:           Coins,

  // Crypto processing
  blockchain_network: Bitcoin,
  token:              Coins,
  merchant:           Store,
  wallet:             Wallet,
  crypto_invoice:     Receipt,
  crypto_payment:     CreditCard,
  crypto_withdrawal:  ArrowUpRight,
  withdrawal_request: FileText,
  treasury_withdrawal: Bitcoin,
  crypto_sweep:       RefreshCw,
  crypto_resource_rental: Gauge,
  crypto_network_operation: RefreshCw,
  crypto_provider_balance_operation: Landmark,
  crypto_provider_account: Landmark,
}

/**
 * Get the Lucide icon component for a given entity type.
 * Falls back to Star for unknown entity types.
 */
export function getEntityIcon(entityType: string): LucideIcon {
  return _entityIconMap[entityType] ?? Star
}
