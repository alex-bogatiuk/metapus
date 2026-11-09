import { useEffect, useState, useSyncExternalStore } from "react"
import { getSessionGeneration, onScopeReset, assertCurrentSession, isCurrentSession } from "@/lib/auth-scope"
import { apiFetch } from "@/lib/api"
import type { JsonValue } from "@/lib/json-value"
import { resolvedRefsSchema } from "@/lib/schemas/references"
import { ResolveRefsRequestSchema } from "@/lib/generated/http-contract-schemas"
import type { TypedRef, ResolvedRef } from "@/types/common"

const resolveCache = new Map<string, ResolvedRef>()
const cacheKey = (refType: string, refId: string) => `${refType}:${refId}`

/** Resolves references atomically across chunks. A valid empty response means
 * unavailable references; transport/contract failures reject the operation. */
export async function resolveRefs(refs: TypedRef[]): Promise<Map<string, ResolvedRef>> {
  const generation = getSessionGeneration()
  const result = new Map<string, ResolvedRef>()
  const uncached: TypedRef[] = []
  const requested = new Set<string>()
  for (const ref of refs) {
    if (!ref.refType || !ref.refId) continue
    const key = cacheKey(ref.refType, ref.refId)
    const cached = resolveCache.get(key)
    if (cached) result.set(ref.refId, cached)
    else if (!requested.has(key)) {
      requested.add(key)
      uncached.push(ref)
    }
  }

  const resolved: ResolvedRef[] = []
  for (let offset = 0; offset < uncached.length; offset += 100) {
    const body = ResolveRefsRequestSchema.parse({ refs: uncached.slice(offset, offset + 100) })
    const value = await apiFetch<JsonValue>("/resolve-refs", { method: "POST", body: JSON.stringify(body) })
    assertCurrentSession(generation)
    const batch = resolvedRefsSchema.parse(value)
    const batchKeys = new Set(body.refs.map(ref => cacheKey(ref.refType, ref.refId)))
    if (batch.some(ref => !batchKeys.has(cacheKey(ref.refType, ref.refId)))) throw new Error("Unexpected resolved reference")
    resolved.push(...batch)
  }
  assertCurrentSession(generation)
  // Publish only after every chunk has succeeded in the same session.
  for (const ref of resolved) {
    resolveCache.set(cacheKey(ref.refType, ref.refId), ref)
    result.set(ref.refId, ref)
  }
  return result
}

export async function resolveSingleRef(ref: TypedRef): Promise<ResolvedRef | null> {
  if (!ref.refType || !ref.refId) return null
  const map = await resolveRefs([ref])
  return map.get(ref.refId) ?? null
}

interface ResolutionState {
  key: string
  generation: number
  attempt: number
  resolved: ResolvedRef | null
  error: Error | null
}

export function useResolvedRef(ref: TypedRef | null | undefined): {
  presentation: string
  loading: boolean
  resolved: ResolvedRef | null
  error: Error | null
  retry: () => void
} {
  const generation = useSyncExternalStore(onScopeReset, getSessionGeneration, () => 0)
  const [attempt, setAttempt] = useState(0)
  const [state, setState] = useState<ResolutionState | null>(null)
  const refType = ref?.refType ?? ""
  const refId = ref?.refId ?? ""
  const key = refType && refId ? cacheKey(refType, refId) : ""

  useEffect(() => {
    if (!key) return
    let cancelled = false
    const publish = (resolved: ResolvedRef | null, error: Error | null) => {
      if (!cancelled && isCurrentSession(generation)) setState({ key, generation, attempt, resolved, error })
    }
    void resolveSingleRef({ refType, refId }).then(
      resolved => publish(resolved, null),
      error => publish(null, new Error("Не удалось загрузить представление ссылки", { cause: error })),
    )
    return () => { cancelled = true }
  }, [key, refType, refId, generation, attempt])

  const current = state?.key === key && state.generation === generation && state.attempt === attempt ? state : null
  const resolved = key ? current?.resolved ?? resolveCache.get(key) ?? null : null
  return {
    presentation: resolved?.presentation ?? "",
    loading: !!key && !current && !resolved,
    resolved,
    error: key ? current?.error ?? null : null,
    retry: () => setAttempt(value => value + 1),
  }
}

export function invalidateResolveCache(refType?: string, refId?: string): void {
  if (refType && refId) resolveCache.delete(cacheKey(refType, refId))
  else resolveCache.clear()
}

onScopeReset(() => resolveCache.clear())
