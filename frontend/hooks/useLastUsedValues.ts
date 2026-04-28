import { useCallback, useMemo } from "react"

const STORAGE_PREFIX = "metapus-last-used-"

/**
 * Persist and recall last-used field values per entity type (M1: Sticky Defaults).
 *
 * When creating a new document, common fields like warehouse, organization,
 * and currency should be pre-filled with values from the last successfully
 * saved document of the same type.
 *
 * Usage:
 * ```tsx
 * const defaults = useLastUsedDefaults("goods_receipt")
 * // defaults.warehouseId, defaults.warehouseName, etc.
 *
 * // After successful save:
 * saveLastUsed("goods_receipt", { warehouseId: f.warehouseId, warehouseName: f.warehouseName, ... })
 * ```
 */

/** Read stored defaults for a given entity type. */
export function useLastUsedDefaults<T>(
  entityType: string,
): Partial<T> {
  return useMemo(() => {
    try {
      const raw = localStorage.getItem(STORAGE_PREFIX + entityType)
      if (!raw) return {} as Partial<T>
      return JSON.parse(raw) as Partial<T>
    } catch {
      return {} as Partial<T>
    }
  // Read once on mount — stable reference
  }, [entityType])
}

/** Persist last-used values for a given entity type. Call after successful save. */
export function saveLastUsed(
  entityType: string,
  fields: Record<string, string>,
): void {
  try {
    // Only persist non-empty values
    const cleaned: Record<string, string> = {}
    for (const [key, value] of Object.entries(fields)) {
      if (value) cleaned[key] = value
    }
    if (Object.keys(cleaned).length > 0) {
      localStorage.setItem(STORAGE_PREFIX + entityType, JSON.stringify(cleaned))
    }
  } catch {
    // localStorage quota exceeded — silently ignore
  }
}

/**
 * Returns a convenience function that saves specified fields after a successful
 * form save. Wrap it once, call it from handleSave.
 */
export function useSaveLastUsed(entityType: string) {
  return useCallback(
    (fields: Record<string, string>) => saveLastUsed(entityType, fields),
    [entityType],
  )
}
