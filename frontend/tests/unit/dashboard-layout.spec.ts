import { test, expect } from "@playwright/test"
import {
  defaultDashboardLayout, distributeColumns, fitRows, layoutLines, layoutWidgets, placeWidget, readDashboardLayout,
  removeWidget, rowSpan, updateWidget, type LayoutLine,
} from "../../lib/dashboard-layout"
import { allWidgetDefinitions } from "../../lib/widget-registry"
import { preferencesSchema } from "../../lib/schemas/preferences"
import { networkHealth, networkProblem } from "../../lib/admin-dashboard"
import type { CryptoNetworkState } from "../../types/crypto-network"
import type { WidgetPlacement } from "../../types/dashboard"

const ids = (rows: WidgetPlacement[][]) => rows.map(row => row.map(w => w.instanceId))

test("a layout of another catalog is replaced by the default, never converted", () => {
  const positions = { catalogVersion: 2, version: 57, widgets: [{ instanceId: "cash", widgetType: "attention-counter", x: 0, y: 0, w: 3, h: 1, config: { metric: "inbound" } }] }
  for (const stored of [positions, { version: 3, rows: [] }, null, "layout"]) expect(readDashboardLayout(stored)).toEqual(defaultDashboardLayout())
  expect(layoutWidgets(defaultDashboardLayout())).toHaveLength(12)
  expect(allWidgetDefinitions.map(w => w.type)).not.toContain("kpi")
})
test("new layouts preserve custom rows, independent configs and intentional empty pages", () => {
  const layout = defaultDashboardLayout()
  layout.version = 94
  layout.rows[0][0].config.metric = "outflows"
  layout.rows = [layout.rows[1], layout.rows[0], ...layout.rows.slice(2)]
  expect(readDashboardLayout(layout)).toEqual(layout)
  expect(readDashboardLayout({ catalogVersion: 3, version: 10, rows: [] }).rows).toEqual([])
})
test("duplicate IDs, unknown types, sizes a widget does not allow and invalid configs cannot restore a broken layout", () => {
  const layout = defaultDashboardLayout()
  const first = layout.rows[0][0]
  for (const rows of [[[first, first]], [[{ ...first, widgetType: "constructor" }]], [[{ ...first, size: "fullx3" }]], [[{ ...first, config: { metric: "cash-balance" } }]]]) {
    expect(readDashboardLayout({ ...layout, rows })).toEqual(defaultDashboardLayout())
  }
})
test("rows never ask for more than 12 columns: adding, resizing and removing keep every row valid", () => {
  const rows = defaultDashboardLayout().rows
  const counter = (id: string): WidgetPlacement => ({ instanceId: id, widgetType: "attention-counter", size: "2x1", config: { metric: "inbound" } })
  // The last row (½ + ½) is full, so a new widget starts a row; the next one joins it.
  const added = placeWidget(placeWidget(rows, counter("a")), counter("b"))
  expect(ids(added).slice(-1)).toEqual([["a", "b"]])
  // Widening a counter in a full row moves the counters that no longer fit to a row below.
  const widened = updateWidget(rows, "default-withdrawals", { size: "4x1" })
  expect(ids(widened).slice(0, 2)).toEqual([["default-withdrawals", "default-inbound", "default-outflows"], ["default-networks"]])
  expect(widened.every(row => rowSpan(row) <= 12)).toBe(true)
  // Removing the only widget of a row removes the row.
  expect(removeWidget(rows, "default-reconciliation")).toHaveLength(rows.length - 1)
  expect(fitRows([[], [counter("x")], []])).toEqual([[counter("x")]])
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


test("preferences carry the stored layout as read, whatever the JSONB key order", () => {
  const layout = defaultDashboardLayout()
  const reorder = <T>(value: T): T => JSON.parse(JSON.stringify(value, (_key, data) => {
    if (data && !Array.isArray(data) && typeof data === "object") return Object.fromEntries(Object.entries(data).reverse())
    return data
  }))
  const preferences = { userId: "10000000-0000-4000-8000-000000000001", interface: {}, listFilters: {}, listColumns: {}, favorites: [], updatedAt: "2026-10-03T12:00:00Z", dashboardLayout: reorder(layout) }
  expect(preferencesSchema.parse(preferences).dashboardLayout).toEqual(layout)
  expect(preferencesSchema.parse({ ...preferences, dashboardLayout: null }).dashboardLayout).toBeNull()
})

/** Every line fills its columns exactly, so no widget ever has empty space beside it. */
function assertFull(lines: LayoutLine[]) {
  for (const line of lines) expect(line.cells.reduce((sum, c) => sum + c.span, 0)).toBe(line.columns)
}

test("rows are drawn full at every width: proportional on desktop, in pairs on tablets, stacked on phones", () => {
  const rows = defaultDashboardLayout().rows
  const wide = layoutLines(rows, "wide")
  assertFull(wide)
  expect(wide.map(l => l.height)).toEqual([1, 3, 3, 3, 4, 2])
  // A row that is not full stretches its widgets in proportion: ⅓ + ⅓ becomes ½ + ½.
  expect(distributeColumns([4, 4])).toEqual([6, 6])
  expect(distributeColumns([3, 3, 4])).toEqual([4, 3, 5])
  expect(distributeColumns([4, 4, 4, 3])).toEqual([3, 3, 3, 3])

  const two = layoutLines(rows, "two")
  assertFull(two)
  expect(two.slice(0, 3).map(l => l.cells.map(c => c.widget.instanceId))).toEqual([
    ["default-withdrawals", "default-inbound"], ["default-outflows", "default-networks"], ["default-queue"],
  ])
  const one = layoutLines(rows, "one")
  assertFull(one)
  expect(one).toHaveLength(12)
})

test("calm widgets shrink their line only as far as the tallest neighbour allows", () => {
  const rows = defaultDashboardLayout().rows
  const height = (lines: LayoutLine[], id: string) => lines.find(l => l.cells.some(c => c.widget.instanceId === id))!.height
  // Desktop: the empty queue shares its row with a health card that still needs three rows.
  expect(height(layoutLines(rows, "wide", { "default-queue": 1 }), "default-queue")).toBe(3)
  expect(height(layoutLines(rows, "wide", { "default-queue": 1, "default-health": 2 }), "default-queue")).toBe(2)
  // Narrow: the queue has its own line and takes exactly what it asked for; nothing grows past its size.
  expect(height(layoutLines(rows, "two", { "default-queue": 1 }), "default-queue")).toBe(1)
  expect(height(layoutLines(rows, "one", { "default-withdrawals": 5 }), "default-withdrawals")).toBe(1)
})
