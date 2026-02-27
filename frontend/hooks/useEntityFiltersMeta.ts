import { useState, useEffect } from "react"
import { api } from "@/lib/api"
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
    const [fieldsMeta, setFieldsMeta] = useState<FilterFieldMeta[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<string | null>(null)

    useEffect(() => {
        let cancelled = false

        async function fetchMeta() {
            setLoading(true)
            setError(null)
            try {
                const meta = await api.meta.getFilters(entityName)
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
