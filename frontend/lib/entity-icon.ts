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
  Warehouse,
  Building2,
  Package,
  ClipboardCheck,
  ClipboardMinus,
  Star,
  FileText,
} from "lucide-react"

const _entityIconMap: Record<string, LucideIcon> = {
  counterparty:  Users,
  warehouse:     Warehouse,
  organization:  Building2,
  nomenclature:  Package,
  goods_receipt:  ClipboardCheck,
  goods_issue:    ClipboardMinus,
  contract:      FileText,
}

/**
 * Get the Lucide icon component for a given entity type.
 * Falls back to Star for unknown entity types.
 */
export function getEntityIcon(entityType: string): LucideIcon {
  return _entityIconMap[entityType] ?? Star
}
