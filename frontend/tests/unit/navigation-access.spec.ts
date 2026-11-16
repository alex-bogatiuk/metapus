import { test, expect } from "@playwright/test"
import { allowed, entityCreatePermission, withEntityPermissions } from "../../lib/navigation-access"
import { getDynamicNavItems } from "../../lib/command-nav-items"
import type { EntityMeta } from "../../types/metadata"

const probe = {
  key: "nav_probe",
  name: "NavProbe",
  type: "catalog",
  routePrefix: "nav-probes",
  presentation: { singular: "Проба", plural: "Пробы", newLabel: "Новая проба", genitive: "пробы" },
  readPermission: "catalog:nav_probe:read",
} as EntityMeta

const open = { ...probe, key: "nav_open", routePrefix: "nav-opens", readPermission: undefined } as EntityMeta

test("palette entries that open an entity need its permissions", () => {
  const items = withEntityPermissions([
    { url: "/catalogs/nav-probes" },
    { url: "/catalogs/nav-probes/new" },
    { url: "/catalogs/nav-probes", permission: "custom:explicit" },
    { url: "/catalogs/nav-opens" },
    { url: "/settings" },
  ], [probe, open])
  expect(items.map(i => i.permission)).toEqual([
    "catalog:nav_probe:read",
    "catalog:nav_probe:create",
    "custom:explicit",
    undefined,
    undefined,
  ])
  expect(entityCreatePermission(probe)).toBe("catalog:nav_probe:create")
})

test("an automatically listed entity carries its read permission", () => {
  const [item] = getDynamicNavItems([probe])
  expect(item.url).toBe("/catalogs/nav-probes")
  expect(item.permission).toBe("catalog:nav_probe:read")

  const granted = new Set(["catalog:nav_probe:read"])
  expect(allowed(item.permission, code => granted.has(code))).toBe(true)
  expect(allowed(item.permission, () => false)).toBe(false)
  expect(allowed(undefined, () => false)).toBe(true)
})
