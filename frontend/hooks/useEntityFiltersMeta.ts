import { useState, useEffect } from "react"
import { api, readCachedMetaFilters } from "@/lib/api"
import type { FilterFieldMeta } from "@/components/shared/filter-config-dialog"

/**
 * Fetches filter field metadata for a given entity from the backend.
 * This is the single source of truth for filter configuration —
 * the backend drives the structure, so adding a new field to the Go struct
 * + label map is enough; the frontend adapts automatically.
 *
 * @param entityName - metadata registry name, e.g. "GoodsReceipt"
 */
export function useEntityFiltersMeta(entityName: string) {
    const initialEntityName = entityName.trim()
    const initialFieldsMeta = initialEntityName
        ? readCachedMetaFilters(initialEntityName)
        : undefined

    const [fieldsMeta, setFieldsMeta] = useState<FilterFieldMeta[]>(() => initialFieldsMeta ?? [])
    const [loading, setLoading] = useState(() => Boolean(initialEntityName) && initialFieldsMeta === undefined)
    const [error, setError] = useState<string | null>(null)

    useEffect(() => {
        let cancelled = false
        const normalizedEntityName = entityName.trim()

        if (!normalizedEntityName) {
            setFieldsMeta([])
            setLoading(false)
            setError(null)
            return () => { cancelled = true }
        }

        const cached = readCachedMetaFilters(normalizedEntityName)
        if (cached !== undefined) {
            setFieldsMeta(cached)
            setLoading(false)
            setError(null)
            return () => { cancelled = true }
        }

        async function fetchMeta() {
            setFieldsMeta([])
            setLoading(true)
            setError(null)
            try {
                const meta = await api.meta.getFilters(normalizedEntityName)
                if (!cancelled) {
                    setFieldsMeta(meta)
                }
            } catch (err) {
                if (!cancelled) {
                    setError(err instanceof Error ? err.message : "Failed to load filter metadata")
                }
            } finally {
                if (!cancelled) {
                    setLoading(false)
                }
            }
        }

        fetchMeta()
        return () => { cancelled = true }
    }, [entityName])

    return { fieldsMeta, loading, error }
}

/**
 * Helper hook to dynamically format enum values using backend metadata.
 */
export function useEnumFormatter(entityName: string) {
    const { fieldsMeta } = useEntityFiltersMeta(entityName)

    return function formatEnum(fieldKey: string, value: string): string {
        if (!value) return value
        const fieldMeta = fieldsMeta.find(f => f.key === fieldKey)
        if (!fieldMeta || !fieldMeta.enumValues) return value
        const enumObj = fieldMeta.enumValues.find(e => e.value === value)
        return enumObj ? enumObj.label : value
    }
}
