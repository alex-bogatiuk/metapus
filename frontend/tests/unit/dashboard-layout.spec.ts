import { test, expect } from "@playwright/test"
import { defaultDashboardLayout, normalizeDashboardLayout, persistedDashboardLayout, reflowLayout, compactLayout, type GridItem } from "../../lib/dashboard-layout"
import { allWidgetDefinitions } from "../../lib/widget-registry"
import { preferencesSchema } from "../../lib/schemas/preferences"
import { networkHealth, networkProblem } from "../../lib/admin-dashboard"
import type { CryptoNetworkState } from "../../types/crypto-network"

test("retired server and local catalogs migrate to fresh independent layouts", () => {
  const old = { version: 57, widgets: [{ instanceId: "cash", widgetType: "kpi", x: 0, y: 0, w: 3, h: 1, config: { metric: "cash-balance" } }] }
  const a = normalizeDashboardLayout(old)
  expect(a.catalogVersion).toBe(2)
  expect(new Set(a.widgets.map(w => w.widgetType)).size).toBe(8)
  expect(persistedDashboardLayout({ layout: old })).toEqual(a)
  a.widgets.pop()
  expect(defaultDashboardLayout().widgets).toHaveLength(12)
  expect(allWidgetDefinitions.map(w => w.type)).not.toContain("kpi")
})
test("new layouts preserve custom placement, independent configs and intentional empty pages", () => {
  const layout = defaultDashboardLayout()
  layout.version = 94
  layout.widgets[0].config.metric = "outflows"
  layout.widgets[0].y = 7
  expect(normalizeDashboardLayout(layout)).toEqual(layout)
  expect(normalizeDashboardLayout({ catalogVersion: 2, version: 10, widgets: [] }).widgets).toEqual([])
})
test("invalid positions, duplicate IDs and invalid configs cannot restore a broken layout", () => {
  const layout = defaultDashboardLayout()
  for (const value of [ { ...layout, widgets: [layout.widgets[0], layout.widgets[0]] },
    { ...layout, widgets: [{ ...layout.widgets[0], y: null }] },
    { ...layout, widgets: [{ ...layout.widgets[0], widgetType: "constructor" }] },
    { ...layout, widgets: [{ ...layout.widgets[0], y: Infinity }] },
    { ...layout, widgets: [{ ...layout.widgets[0], config: { metric: "cash-balance" } }] } ]) {
    expect(normalizeDashboardLayout(value)).toEqual(defaultDashboardLayout())
  }
})
test("network counters distinguish disabled, stale, operational hold and healthy states", () => {
  const now = Date.parse("2026-10-03T12:00:00Z")
  const state = { capabilities: { requiredRpcRoles: ["rpc"], externalResourceMode: "none" }, policy: { enabled: true }, runtime: { observedAt: "2026-10-03T12:00:00Z", operationalHold: false, signerReady: true, rpcEndpoints: [{ role: "rpc", state: "closed" }] } } as CryptoNetworkState
  expect(networkProblem(state, now)).toBeNull()
  expect(networkProblem({ ...state, policy: { ...state.policy, enabled: false }, runtime: { ...state.runtime, signerReady: false } }, now)).toBeNull()
  expect(networkProblem({ ...state, runtime: { ...state.runtime, observedAt: "2026-10-03T11:58:30Z" } }, now)).toBe("Нет свежего состояния")
  expect(networkProblem({ ...state, runtime: { ...state.runtime, operationalHold: true } }, now)).toBe("Обработка приостановлена")
})
test("a degraded reserve RPC warns without counting the network as broken", () => {
  const now = Date.parse("2026-10-03T12:00:00Z")
  const endpoints = [
    { role: "rpc", providerId: "a", state: "closed" },
    { role: "rpc", providerId: "b", state: "closed" },
    { role: "rpc", providerId: "c", state: "degraded" },
  ]
  const state = { capabilities: { requiredRpcRoles: ["rpc"], externalResourceMode: "none" }, policy: { enabled: true }, runtime: { observedAt: "2026-10-03T12:00:00Z", operationalHold: false, signerReady: true, rpcEndpoints: endpoints } } as CryptoNetworkState
  expect(networkHealth(state, now)).toEqual({ level: "warning", label: "RPC нестабилен: 1 из 3" })
  expect(networkProblem(state, now)).toBeNull()

  const noneReady = { ...state, runtime: { ...state.runtime, rpcEndpoints: endpoints.map(e => ({ ...e, state: "open" })) } } as CryptoNetworkState
  expect(networkProblem(noneReady, now)).toBe("Нет готового RPC: rpc")
  const tron = { ...state, capabilities: { requiredRpcRoles: ["fullnode", "solidity"], externalResourceMode: "optional" }, runtime: { ...state.runtime, rpcEndpoints: [{ role: "fullnode", state: "closed" }, { role: "solidity", state: "degraded" }] } } as CryptoNetworkState
  expect(networkProblem(tron, now)).toBe("Нет готового RPC: solidity")
  const wrongChain = { ...state, runtime: { ...state.runtime, rpcEndpoints: [...endpoints, { role: "rpc", state: "wrong_chain" }] } } as CryptoNetworkState
  expect(networkProblem(wrongChain, now)).toBe("RPC другой сети")
  const rental = { ...tron, policy: { enabled: true, externalResources: { strategy: "prefer_rental" } }, runtime: { ...tron.runtime, rpcEndpoints: [{ role: "fullnode", state: "closed" }, { role: "solidity", state: "closed" }], resourceProviderReady: false } } as CryptoNetworkState
  expect(networkProblem(rental, now)).toBe("Ресурсы недоступны")
  expect(networkProblem({ ...rental, policy: { enabled: true, externalResources: { strategy: "burn_only" } } } as CryptoNetworkState, now)).toBeNull()
})


test("JSONB key ordering does not trigger another layout migration", () => {
  const layout = defaultDashboardLayout()
  const reorder = <T>(value: T): T => JSON.parse(JSON.stringify(value, (_key, data) => {
    if (data && !Array.isArray(data) && typeof data === "object") return Object.fromEntries(Object.entries(data).reverse())
    return data
  }))
  const preferences = { userId: "10000000-0000-4000-8000-000000000001", interface: {}, listFilters: {}, listColumns: {}, favorites: [], updatedAt: "2026-10-03T12:00:00Z", dashboardLayout: reorder(layout) }
  expect(preferencesSchema.parse(preferences).dashboardLayoutNeedsSave).toBe(false)
  expect(preferencesSchema.parse({ ...preferences, dashboardLayout: { version: 3, widgets: [{ instanceId: "old", widgetType: "tasks", x: 0, y: 0, w: 4, h: 2, config: {} }] } }).dashboardLayoutNeedsSave).toBe(true)
})

/** Every cell from the top of the grid to its last row is covered exactly once. */
function assertNoGaps(items: GridItem[], cols: number) {
  const height = Math.max(...items.map(i => i.y + i.h))
  const cells = Array.from({ length: height }, () => Array<number>(cols).fill(0))
  for (const i of items) for (let y = i.y; y < i.y + i.h; y++) for (let x = i.x; x < i.x + i.w; x++) cells[y][x]++
  expect(cells.flat().every(c => c === 1)).toBe(true)
}

test("narrow dashboard grids reflow the desktop layout without gaps", () => {
  const desktop = defaultDashboardLayout().widgets.map(w => ({ i: w.instanceId, x: w.x, y: w.y, w: w.w, h: w.h }))
  for (const cols of [4, 1]) {
    const narrow = reflowLayout(desktop, cols)
    expect(narrow.map(i => i.i).sort()).toEqual(desktop.map(i => i.i).sort())
    assertNoGaps(narrow, cols)
  }
  // Counters pair up; the queue is wide and takes the row; reading order is kept.
  const two = reflowLayout(desktop, 4)
  expect(two.slice(0, 5).map(i => [i.i, i.x, i.w])).toEqual([
    ["default-withdrawals", 0, 2], ["default-inbound", 2, 2], ["default-outflows", 0, 2], ["default-networks", 2, 2], ["default-queue", 0, 4],
  ])
  // A widget without an equally tall partner takes the full row instead of leaving a gap.
  const odd = reflowLayout([{ i: "a", x: 0, y: 0, w: 3, h: 1 }, { i: "b", x: 3, y: 0, w: 6, h: 3 }, { i: "c", x: 0, y: 3, w: 12, h: 2 }], 4)
  expect(odd).toEqual([{ i: "a", x: 0, y: 0, w: 4, h: 1 }, { i: "b", x: 0, y: 1, w: 4, h: 3 }, { i: "c", x: 0, y: 4, w: 4, h: 2 }])
  assertNoGaps(odd, 4)
})

test("calm widgets shrink their band only when no neighbour needs the height", () => {
  const desktop = defaultDashboardLayout().widgets.map(w => ({ i: w.instanceId, x: w.x, y: w.y, w: w.w, h: w.h }))
  const byId = (items: GridItem[]) => Object.fromEntries(items.map(i => [i.i, i]))

  // An empty queue next to a busy health card keeps the band: shrinking it would leave a gap.
  const queueOnly = byId(compactLayout(desktop, { "default-queue": 1 }))
  expect(queueOnly["default-queue"]).toMatchObject({ y: 1, h: 3 })
  expect(queueOnly["default-obligations"]).toMatchObject({ y: 4 })

  // When both ask for less, the band takes the taller request and everything below moves up.
  const both = compactLayout(desktop, { "default-queue": 1, "default-health": 2 })
  expect(byId(both)["default-queue"]).toMatchObject({ y: 1, h: 2 })
  expect(byId(both)["default-health"]).toMatchObject({ y: 1, h: 2 })
  expect(byId(both)["default-obligations"]).toMatchObject({ y: 3 })
  assertNoGaps(both, 12)

  // A band with stacked widgets (receipts beside fees over results) keeps its shape.
  const stacked = byId(compactLayout(desktop, { "default-fees": 1 }))
  expect(stacked["default-fees"]).toMatchObject({ h: 2 })
  expect(stacked["default-results"].y - stacked["default-fees"].y).toBe(2)

  // Narrow grids give every row exactly what its widgets need; a widget never grows.
  const narrow = reflowLayout(desktop, 4, { "default-queue": 1, "default-health": 2 })
  expect(byId(narrow)["default-queue"]).toMatchObject({ w: 4, h: 1 })
  expect(byId(narrow)["default-health"].h).toBe(3)
  expect(reflowLayout([{ i: "a", x: 0, y: 0, w: 3, h: 1 }], 1, { a: 5 })[0].h).toBe(1)
  assertNoGaps(narrow, 4)
})
