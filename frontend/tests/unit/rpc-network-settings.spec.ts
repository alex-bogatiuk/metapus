import { expect, test } from "@playwright/test"
import type { IntegrationsRPCNetworkSettings } from "../../types/generated/http-contracts"
import { rpcSettingsErrors, rpcSettingsForm, rpcSettingsFromForm } from "../../lib/integrations/rpc-network-settings"

const defaults: IntegrationsRPCNetworkSettings = {
  evm: { maxHeadAgeSeconds: 30, attemptTimeoutMs: 2000, readBudgetMs: 5000, logsTimeoutMs: 5000, maxAttempts: 2, providerConcurrency: 8, backfillConcurrency: 4, requestsPerSecond: 20, nativeConcurrency: 4, prefetchBlocks: 32, maxResponseBytes: 8388608, maxBufferedBytes: 268435456 },
  alerts: { maxLagSeconds: 60, fireAfterSeconds: 60, recoveryLagSeconds: 30, recoverAfterSeconds: 120 },
}

test("alert-only changes preserve absent RPC settings and do not reset clients", () => {
  const raw: IntegrationsRPCNetworkSettings = { alerts: { maxLagSeconds: 0, fireAfterSeconds: 0, recoveryLagSeconds: 0, recoverAfterSeconds: 0 } }
  const form = { ...rpcSettingsForm(raw), fireAfterSeconds: "90" }
  expect(rpcSettingsErrors(form, defaults, "evm")).toEqual({})
  const update = rpcSettingsFromForm(form, raw, "evm")
  expect(update.evm).toBeUndefined()
  expect(update.alerts.fireAfterSeconds).toBe(90)
})

test("automatic defaults participate in cross-field budget, capacity and recovery validation", () => {
  const form = rpcSettingsForm(defaults)
  expect(rpcSettingsErrors({ ...form, readBudgetMs: "0", attemptTimeoutMs: "10000" }, defaults, "evm").readBudgetMs).toBeTruthy()
  expect(rpcSettingsErrors({ ...form, maxLagSeconds: "20", recoveryLagSeconds: "0" }, defaults, "evm").recoveryLagSeconds).toBeTruthy()
  expect(rpcSettingsErrors({ ...form, providerConcurrency: "0", backfillConcurrency: "8" }, defaults, "evm").backfillConcurrency).toBeTruthy()
  expect(rpcSettingsErrors({ ...form, maxAttempts: "1.5" }, defaults, "evm").maxAttempts).toBeTruthy()
})
