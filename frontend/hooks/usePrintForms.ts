// frontend/hooks/usePrintForms.ts
// Orchestration hook for loading and grouping print forms from backend.
// Forms are CODE IS METADATA — cached after first load (staleTime: Infinity).

import { useMemo, useSyncExternalStore, useCallback } from "react"
import { apiFetch } from "@/lib/api"
import type { PrintFormSummary } from "@/types/print"

// ── Module-level cache + subscriber pattern ────────────────────────────

interface CacheEntry {
  forms: PrintFormSummary[]
  loading: boolean
}

const cache = new Map<string, CacheEntry>()
const listeners = new Set<() => void>()

function notify() {
  listeners.forEach((l) => l())
}

function getEntry(documentType: string): CacheEntry {
  return cache.get(documentType) ?? { forms: [], loading: true }
}

/** Trigger fetch if not already cached. Safe to call multiple times. */
function ensureFetched(documentType: string) {
  if (cache.has(documentType)) return

  // Mark as loading immediately (prevents duplicate fetches)
  cache.set(documentType, { forms: [], loading: true })
  notify()

  apiFetch<PrintFormSummary[]>(`/document/${documentType}/print-forms`)
    .then((data) => {
      cache.set(documentType, { forms: data, loading: false })
    })
    .catch(() => {
      cache.set(documentType, { forms: [], loading: false })
    })
    .finally(notify)
}

/**
 * Loads available print forms for a document type from backend.
 * Forms are grouped by category ("standard" / "custom").
 * Results are cached in memory — forms are CODE IS METADATA and don't change at runtime.
 *
 * Uses useSyncExternalStore to avoid setState-in-effect lint violations.
 */
export function usePrintForms(documentType: string) {
  // Trigger fetch on first call (idempotent — checks cache.has internally)
  ensureFetched(documentType)

  const subscribe = useCallback((onStoreChange: () => void) => {
    listeners.add(onStoreChange)
    return () => { listeners.delete(onStoreChange) }
  }, [])

  const getSnapshot = useCallback(() => getEntry(documentType), [documentType])

  const entry = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)

  const standard = useMemo(() => entry.forms.filter((f) => f.category === "standard"), [entry.forms])
  const custom = useMemo(() => entry.forms.filter((f) => f.category === "custom"), [entry.forms])

  return { forms: entry.forms, standard, custom, loading: entry.loading }
}
