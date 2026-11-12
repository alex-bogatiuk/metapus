import { test, expect } from "@playwright/test"
import { defaultDashboardLayout, normalizeDashboardLayout, persistedDashboardLayout } from "../../lib/dashboard-layout"
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
