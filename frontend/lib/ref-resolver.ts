/**
 * Ref Resolver — batch resolve TypedRefs into human-readable presentations.
 *
 * Uses POST /api/v1/resolve-refs endpoint with client-side caching.
 * Analogous to 1C's "ПолучитьПредставление()" for composite type fields.
 *
 * Usage:
 *   import { resolveRefs, useResolvedRef } from "@/lib/ref-resolver"
 *
 *   // Batch (imperative):
 *   const map = await resolveRefs([{ refType: "GoodsReceipt", refId: "uuid" }])
 *   map.get("uuid")?.presentation // → "Поступление товаров ПТ-00042 от 15.03.2026"
 *
 *   // Hook (declarative):
 *   const { presentation, loading } = useResolvedRef({ refType: "Counterparty", refId: "uuid" })
 */

import { useState, useEffect } from "react"
import { apiFetch } from "@/lib/api"
import type { TypedRef, ResolvedRef } from "@/types/common"

// ── Client-side cache ──────────────────────────────────────────────────
// Key: "refType:refId" → ResolvedRef
const _resolveCache = new Map<string, ResolvedRef>()

function cacheKey(refType: string, refId: string): string {
    return `${refType}:${refId}`
}

/**
 * Batch resolve typed references into presentations.
 * Deduplicates against cache — only fetches unknown refs.
 * Returns a Map keyed by refId for easy lookup.
 */
export async function resolveRefs(refs: TypedRef[]): Promise<Map<string, ResolvedRef>> {
    const result = new Map<string, ResolvedRef>()
    if (!refs.length) return result

    // Check cache first, collect uncached
    const uncached: TypedRef[] = []
    for (const ref of refs) {
        if (!ref.refType || !ref.refId) continue
        const key = cacheKey(ref.refType, ref.refId)
        const cached = _resolveCache.get(key)
        if (cached) {
            result.set(ref.refId, cached)
        } else {
            uncached.push(ref)
        }
    }

    // Fetch uncached from API
    if (uncached.length > 0) {
        try {
            const resolved = await apiFetch<ResolvedRef[]>("/resolve-refs", {
                method: "POST",
                body: JSON.stringify({ refs: uncached }),
            })
            for (const r of resolved) {
                const key = cacheKey(r.refType, r.refId)
                _resolveCache.set(key, r)
                result.set(r.refId, r)
            }
        } catch {
            // Silently fail — presentation will be empty
        }
    }

    return result
}

/**
 * Resolve a single TypedRef. Returns cached result if available.
 */
export async function resolveSingleRef(ref: TypedRef): Promise<ResolvedRef | null> {
    if (!ref.refType || !ref.refId) return null
    const key = cacheKey(ref.refType, ref.refId)
    const cached = _resolveCache.get(key)
    if (cached) return cached

    const map = await resolveRefs([ref])
    return map.get(ref.refId) ?? null
}

/**
 * React hook: resolve a TypedRef into a presentation string.
 * Auto-fetches on mount / when ref changes. Caches results.
 */
export function useResolvedRef(ref: TypedRef | null | undefined): {
    presentation: string
    loading: boolean
    resolved: ResolvedRef | null
} {
    const [resolved, setResolved] = useState<ResolvedRef | null>(() => {
        if (!ref?.refType || !ref?.refId) return null
        return _resolveCache.get(cacheKey(ref.refType, ref.refId)) ?? null
    })
    const [loading, setLoading] = useState(false)

    useEffect(() => {
        let isMounted = true
        void Promise.resolve().then(() => {
            if (!isMounted) return

            if (!ref?.refType || !ref?.refId) {
                setResolved(null)
                return
            }

            // Check cache synchronously
            const cached = _resolveCache.get(cacheKey(ref.refType, ref.refId))
            if (cached) {
                setResolved(cached)
                return
            }

            setLoading(true)

            resolveSingleRef({ refType: ref.refType, refId: ref.refId }).then((r) => {
                if (isMounted) {
                    setResolved(r)
                    setLoading(false)
                }
            }).catch(() => {
                if (isMounted) setLoading(false)
            })
        })

        return () => { isMounted = false }
    }, [ref?.refType, ref?.refId])

    return {
        presentation: resolved?.presentation ?? "",
        loading,
        resolved,
    }
}

/**
 * Invalidate the resolve cache (e.g. after entity update).
 */
export function invalidateResolveCache(refType?: string, refId?: string): void {
    if (refType && refId) {
        _resolveCache.delete(cacheKey(refType, refId))
    } else {
        _resolveCache.clear()
    }
}
