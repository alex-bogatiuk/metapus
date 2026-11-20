"use client"

import { useCallback, useSyncExternalStore } from "react"

type ExpandedById = Readonly<Record<string, boolean>>

const EMPTY: ExpandedById = {}

interface ExpandedStore {
  snapshot: ExpandedById | null
  listeners: Set<() => void>
}

const stores = new Map<string, ExpandedStore>()

function storeFor(storageKey: string): ExpandedStore {
  let store = stores.get(storageKey)
  if (!store) {
    store = { snapshot: null, listeners: new Set() }
    stores.set(storageKey, store)
  }
  return store
}

/** Reads the remembered state; anything that is not an id → boolean map counts as nothing remembered. */
function readStored(storageKey: string): ExpandedById {
  try {
    const raw = localStorage.getItem(storageKey)
    if (!raw) return EMPTY
    const parsed = JSON.parse(raw) as Record<string, boolean> | null
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return EMPTY
    const result: Record<string, boolean> = {}
    for (const [id, value] of Object.entries(parsed)) {
      if (typeof value === "boolean") result[id] = value
    }
    return result
  } catch {
    return EMPTY
  }
}

/**
 * Which cards of a list are expanded, remembered in this browser under `storageKey`.
 * A card nobody has toggled is expanded only when it is the list's single card, so a long
 * list opens compact and a lone card opens ready to edit.
 */
export function useExpandedCards(storageKey: string, ids: readonly string[]) {
  const store = storeFor(storageKey)
  const subscribe = useCallback((listener: () => void) => {
    store.listeners.add(listener)
    return () => {
      store.listeners.delete(listener)
    }
  }, [store])
  const getSnapshot = useCallback(() => (store.snapshot ??= readStored(storageKey)), [store, storageKey])
  const expandedById = useSyncExternalStore(subscribe, getSnapshot, () => EMPTY)

  const commit = useCallback((next: ExpandedById) => {
    store.snapshot = next
    try {
      localStorage.setItem(storageKey, JSON.stringify(next))
    } catch {
      // Private mode or full storage: the state still holds for this page.
    }
    store.listeners.forEach((listener) => listener())
  }, [store, storageKey])

  const isExpanded = useCallback((id: string) => expandedById[id] ?? ids.length === 1, [expandedById, ids.length])
  const setExpanded = useCallback((id: string, expanded: boolean) => commit({ ...getSnapshot(), [id]: expanded }), [commit, getSnapshot])
  const setAll = useCallback((expanded: boolean) => commit(Object.fromEntries(ids.map((id) => [id, expanded]))), [commit, ids])

  return {
    isExpanded,
    setExpanded,
    setAll,
    allExpanded: ids.every(isExpanded),
    allCollapsed: ids.every((id) => !isExpanded(id)),
  }
}
