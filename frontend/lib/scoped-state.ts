/** A declaration owns its typed values; the registry only keeps cleanup operations. */
export interface ScopedStateKey<T> {
  readonly id: symbol
  readonly read: (scope: string) => T | undefined
  readonly write: (scope: string, value: T) => void
  readonly clear: (scope: string) => void
  readonly reset: () => void
}

export function createScopedStateRegistry() {
  const entries = new Set<{ clear: (scope: string) => void; reset: () => void }>()
  return {
    define<T>(label: string): ScopedStateKey<T> {
      const values = new Map<string, T>()
      const key: ScopedStateKey<T> = {
        id: Symbol(label),
        read: scope => values.get(scope),
        write: (scope, value) => { values.set(scope, value) },
        clear: scope => { values.delete(scope) },
        reset: () => values.clear(),
      }
      entries.add(key)
      return key
    },
    clear(scope: string) { entries.forEach(entry => entry.clear(scope)) },
    reset() { entries.forEach(entry => entry.reset()) },
  }
}
