// frontend/lib/navigation-access.ts
/**
 * Permissions of navigation entries that open entities.
 *
 * The backend sends each entity's read permission in /meta/entities
 * (`readPermission`). A sidebar item or palette entry that opens an entity
 * without an explicit `permission` needs it: the user would only get 403
 * from the list otherwise. The new-record form of an entity needs its create
 * permission. Admins have every permission (usePermissions).
 */

import { buildEntityUrlByRoute } from "@/lib/entity-url"
import type { CommandNavItem } from "@/lib/navigation"
import type { EntityMeta } from "@/types/metadata"

/** The permission that opens the entity, if the backend declares one. */
export function entityReadPermission(entity: EntityMeta | undefined): string | undefined {
  return entity?.readPermission || undefined
}

/** The permission that opens the entity's new-record form. */
export function entityCreatePermission(entity: EntityMeta | undefined): string | undefined {
  const read = entityReadPermission(entity)
  return read?.endsWith(":read") ? read.slice(0, -":read".length) + ":create" : read
}

/**
 * The URLs of the entity's list: the canonical one, and the one with the
 * route prefix as declared, which palette entries also use
 * ("/documents/goods-receipt" next to "/documents/goods-receipts").
 */
function entityListUrls(entity: EntityMeta): string[] {
  if (entity.type !== "catalog" && entity.type !== "document") return []
  const prefix = entity.routePrefix ?? entity.key
  const section = entity.type === "document" ? "documents" : "catalogs"
  return [...new Set([buildEntityUrlByRoute(prefix, entity.type), `/${section}/${prefix}`])]
}

/**
 * Gives palette entries without a permission the one of the entity they
 * open: its list needs read, its "/new" form needs create. Entries with an
 * explicit permission and entries that open no entity are kept as they are.
 */
export function withEntityPermissions<T extends Pick<CommandNavItem, "url" | "permission">>(items: T[], entities: EntityMeta[]): T[] {
  const byUrl = new Map<string, string | undefined>()
  for (const entity of entities) {
    for (const url of entityListUrls(entity)) {
      byUrl.set(url, entityReadPermission(entity))
      byUrl.set(url + "/new", entityCreatePermission(entity))
    }
  }
  return items.map((item) => {
    if (item.permission) return item
    const permission = byUrl.get(item.url)
    return permission ? { ...item, permission } : item
  })
}

/** Whether the user may see an entry that requires `permission`. */
export function allowed(permission: string | undefined, has: (code: string) => boolean): boolean {
  return !permission || has(permission)
}
