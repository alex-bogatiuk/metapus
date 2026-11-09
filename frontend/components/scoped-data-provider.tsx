"use client"

import { useEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from "react"
import { SWRConfig, type State } from "swr"
import { getSessionGeneration, onScopeReset } from "@/lib/auth-scope"
import type { JsonValue } from "@/lib/json-value"

function DataScope({ children }: { children: ReactNode }) {
  const [cache] = useState(() => new Map<string, State<JsonValue, Error>>())
  useEffect(() => onScopeReset(() => cache.clear()), [cache])
  const config = useMemo(() => ({ provider: () => cache }), [cache])
  return <SWRConfig value={config}>{children}</SWRConfig>
}

/** All SWR consumers share a cache only within one authenticated scope. */
export function ScopedDataProvider({ children }: { children: ReactNode }) {
  const generation = useSyncExternalStore(onScopeReset, getSessionGeneration, () => 0)
  // Remounting also disconnects old SWR revalidators and mutation callbacks.
  // A plain token rotation leaves the generation and the drafts unchanged.
  return <DataScope key={generation}>{children}</DataScope>
}
