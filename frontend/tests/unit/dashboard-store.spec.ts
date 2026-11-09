import { test, expect } from "@playwright/test"
import { api } from "../../lib/api"
import { useDashboardStore } from "../../stores/useDashboardStore"
import { transitionScope } from "../../lib/auth-scope"
import { defaultDashboardLayout } from "../../lib/dashboard-layout"
import type { UserPreferencesResponse } from "../../types/user-prefs"

const prefs: UserPreferencesResponse = { userId: "10000000-0000-4000-8000-000000000001", interface: {}, listFilters: {}, listColumns: {}, favorites: [], dashboardLayout: defaultDashboardLayout(), updatedAt: "2026-10-03T12:00:00Z" }
const scope = (id: string) => ({ tenantId: "test", principalId: id, sessionId: id, userVersion: "1", policyVersion: "1" })
test("a layout response from the previous session cannot restore its data", async () => {
  const original = api.preferences.get
  let resolve!: (value: UserPreferencesResponse) => void
  api.preferences.get = () => new Promise(done => { resolve = done })
  try {
    transitionScope(scope("user-a"))
    const loading = useDashboardStore.getState().loadLayout()
    transitionScope(scope("user-b"))
    const old = defaultDashboardLayout(); old.widgets[0].config.metric = "outflows"
    resolve({ ...prefs, dashboardLayout: old })
    await loading
    expect(useDashboardStore.getState().isLoaded).toBe(false)
    expect(useDashboardStore.getState().layout).toEqual(defaultDashboardLayout())
  } finally { api.preferences.get = original; transitionScope(null) }
})
test("layout writes are serialized and keep the latest pending-save state", async () => {
  const original = api.preferences.saveDashboardLayout
  const calls: number[] = []
  const finish: (() => void)[] = []
  api.preferences.saveDashboardLayout = layout => { calls.push(layout.version); return new Promise(done => finish.push(done)) }
  try {
    useDashboardStore.setState(useDashboardStore.getInitialState(), true)
    useDashboardStore.getState().setEditMode(true)
    const first = useDashboardStore.getState().saveLayout()
    await new Promise(resolve => setImmediate(resolve))
    useDashboardStore.getState().removeWidget("default-withdrawals")
    const second = useDashboardStore.getState().saveLayout()
    expect(calls).toEqual([1])
    finish[0]()
    await first
    await new Promise(resolve => setImmediate(resolve))
    expect(calls).toEqual([1, 2])
    expect(useDashboardStore.getState().isSaving).toBe(true)
    finish[1]()
    expect(await second).toBe(true)
    expect(useDashboardStore.getState().isSaving).toBe(false)
  } finally { api.preferences.saveDashboardLayout = original; useDashboardStore.setState(useDashboardStore.getInitialState(), true) }
})
