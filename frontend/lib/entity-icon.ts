// frontend/lib/entity-icon.ts
/**
 * One icon per entity and per page, shared by tabs, the section panel, the
 * command palette and preview cards — the same thing always looks the same.
 */

import type { LucideIcon } from "lucide-react"
import {
  Activity,
  Banknote,
  BarChart3,
  Bell,
  BookOpen,
  Building2,
  FileSignature,
  FileText,
  HelpCircle,
  KeyRound,
  Layers,
  LayoutDashboard,
  ListChecks,
  Plug,
  Rss,
  ScrollText,
  Search,
  Settings,
  ShieldCheck,
  Star,
  Trash2,
  TrendingUp,
  Users,
  Workflow,
} from "lucide-react"
import { useMetadataStore } from "@/stores/useMetadataStore"

/** Keyed by backend metadata key (e.g. "counterparty"). */
const entityIcons: Record<string, LucideIcon> = {
  // Business
  counterparty: Users,
  organization: Building2,
  contract: FileSignature,

  // Money and rates
  currency: Banknote,
  rate_source: Rss,
  "exchange-rate": TrendingUp,

  // Integrations
  integration_account: Plug,
  integration_pool: Layers,

  // Reports
  "report-document-journal": ScrollText,
}

/**
 * Get the Lucide icon component for a given entity key.
 * Falls back to Star for unknown entity types.
 */
export function getEntityIcon(entityType: string): LucideIcon {
  return entityIcons[entityType] ?? Star
}

/** Pages that are not metadata entities; the longest matching prefix wins. */
let pageIcons: [prefix: string, icon: LucideIcon][] = ([
  ["/finance/exchange-rates", TrendingUp],
  ["/reports/document-journal", ScrollText],
  ["/reports", BarChart3],
  ["/admin/event-log", ScrollText],
  ["/admin/worker-jobs", Activity],
  ["/admin/automation-rules", Workflow],
  ["/admin/batch-modify", ListChecks],
  ["/admin/find-references", Search],
  ["/admin/marked-objects", Trash2],
  ["/admin/security-profiles", KeyRound],
  ["/admin", ShieldCheck],
  ["/settings", Settings],
  ["/notifications", Bell],
  ["/help", HelpCircle],
] satisfies [string, LucideIcon][]).sort(([a], [b]) => b.length - a.length)

/**
 * Adds a configuration's icons: `entities` by metadata key, `pages` by URL
 * prefix for pages that are not metadata entities.
 */
export function registerIcons(icons: { entities?: Record<string, LucideIcon>; pages?: Record<string, LucideIcon> }): void {
  Object.assign(entityIcons, icons.entities)
  const pages = new Map(pageIcons)
  for (const [prefix, icon] of Object.entries(icons.pages ?? {})) pages.set(prefix, icon)
  pageIcons = [...pages].sort(([a], [b]) => b.length - a.length)
}

/** "rate-sources" → "rate_source", "currencies" → "currency". */
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
