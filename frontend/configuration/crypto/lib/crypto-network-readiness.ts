import type { CryptoNetworkState, ResourceStrategy } from "@/configuration/crypto/types/crypto-network"

/** The worker publishes every 30 s; one missed cycle is tolerated. */
export const RUNTIME_FRESH_MS = 60_000
const CLOCK_SKEW_MS = 60_000

export function isRuntimeFresh(observedAt: string, now = Date.now()): boolean {
  const age = now - Date.parse(observedAt)
  return Number.isFinite(age) && age <= RUNTIME_FRESH_MS && age >= -CLOCK_SKEW_MS
}

function resourceProviderRequired(state: CryptoNetworkState, strategy: ResourceStrategy | undefined): boolean {
  return state.capabilities.externalResourceMode !== "none" && strategy !== undefined && strategy !== "burn_only"
}

/**
 * Why the network cannot process operations, or null when it can. Mirrors
 * NetworkRuntimeStatus.Ready and the resource check the API applies before
 * enabling processing: every required RPC role needs one healthy endpoint, so a
 * single degraded reserve does not stop the network.
 */
export function networkReadinessProblem(
  state: CryptoNetworkState,
  resourceStrategy: ResourceStrategy | undefined = state.policy.externalResources?.strategy,
  now = Date.now(),
): string | null {
  const runtime = state.runtime
  if (!isRuntimeFresh(runtime.observedAt, now)) return "Нет свежего состояния"
  if (runtime.operationalHold) return "Обработка приостановлена"
  if (!runtime.signerReady) return "Подпись недоступна"
  if (runtime.rpcEndpoints.some((endpoint) => endpoint.state === "wrong_chain")) return "RPC другой сети"
  const readyRoles = new Set(runtime.rpcEndpoints.filter((endpoint) => endpoint.state === "closed").map((endpoint) => endpoint.role))
  const missing = state.capabilities.requiredRpcRoles.filter((role) => !readyRoles.has(role))
  if (missing.length > 0) return `Нет готового RPC: ${missing.join(", ")}`
  if (resourceProviderRequired(state, resourceStrategy) && !runtime.resourceProviderReady) return "Ресурсы недоступны"
  return null
}

/** A ready network can still run on fewer RPC endpoints than configured. */
export function networkRPCWarning(state: CryptoNetworkState): string | null {
  const endpoints = state.runtime.rpcEndpoints
  const unhealthy = endpoints.filter((endpoint) => endpoint.state !== "closed").length
  return unhealthy > 0 ? `RPC нестабилен: ${unhealthy} из ${endpoints.length}` : null
}

export type NetworkHealth = { level: "problem" | "warning"; label: string }

/** Dashboard health of a network with processing enabled; null when healthy. */
export function networkHealth(state: CryptoNetworkState, now = Date.now()): NetworkHealth | null {
  if (!state.policy.enabled) return null
  const problem = networkReadinessProblem(state, undefined, now)
  if (problem) return { level: "problem", label: problem }
  const warning = networkRPCWarning(state)
  return warning ? { level: "warning", label: warning } : null
}
