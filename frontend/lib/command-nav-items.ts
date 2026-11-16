// frontend/lib/command-nav-items.ts
/**
 * Static navigation index for Command Palette search.
 *
 * Combines sidebar navigation sections, flat items, and system items
 * into a flat searchable array. Also provides a function to build
 * a dynamic index from entity metadata for entities not in the static list.
 *
 * Each item has keywords for fuzzy matching (e.g. "Поступления товаров"
 * matches "пост", "товар", "поступления").
 */

import { buildEntityUrlByRoute } from "@/lib/entity-url"
import { getRouteIcon } from "@/lib/entity-icon"
import { getNavigation, type CommandNavItem } from "@/lib/navigation"
import { entityReadPermission } from "@/lib/navigation-access"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type { EntityMeta } from "@/types/metadata"

export type { CommandNavItem } from "@/lib/navigation"

/**
 * Get the static navigation items: the platform's and every registered
 * configuration's. Always returns a fresh array.
 */
export function getStaticNavItems(): CommandNavItem[] {
  return getNavigation().commands.map((item) => ({ ...item, icon: item.icon ?? getRouteIcon(item.url) }))
}

/**
 * Build dynamic navigation items from metadata store for entities
 * not covered by the static list.
 */
export function getDynamicNavItems(entities: EntityMeta[] = useMetadataStore.getState().entities): CommandNavItem[] {
  if (entities.length === 0) return []

  // Collect URLs already in static list
  const coveredUrls = new Set(getNavigation().commands.map((i) => i.url))
  const result: CommandNavItem[] = []

  for (const entity of entities) {
    const routePrefix = entity.routePrefix ?? entity.key
    const type = entity.type as "catalog" | "document"
    const url = buildEntityUrlByRoute(routePrefix, type)

    if (coveredUrls.has(url)) continue

    const label = entity.presentation?.plural ?? entity.name
    result.push({
      id: `nav:${entity.key}`,
      label,
      url,
      icon: getRouteIcon(url), // known entities share their icon; others fall back to a section icon
      keywords: `${label.toLowerCase()} ${entity.key.replace(/_/g, " ")}`,
      section: "navigate",
      permission: entityReadPermission(entity),
    })
  }

  return result
}

// ── Section display labels ──────────────────────────────────────────────

export const COMMAND_SECTION_LABELS: Record<CommandNavItem["section"], string> = {
  navigate: "Перейти",
  create: "Создать",
  report: "Отчёты",
  system: "Система",
}
