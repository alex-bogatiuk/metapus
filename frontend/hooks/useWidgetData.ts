"use client"

import { useCallback } from "react"
import useSWR from "swr"
import { useAuthStore } from "@/stores/useAuthStore"
import { scopeKey } from "@/lib/auth-scope"

/**
 * useWidgetData — standardized data fetching hook for dashboard widgets.
 *
 * Features:
 *  - Shared SWR cache: identical widget instances share requests and data
 *  - skipInEditMode: pauses fetching when dashboard is in edit mode
 *  - pollInterval: optional auto-refresh (e.g. every 60s)
 *  - Single retry on failure (3s delay)
 */

interface UseWidgetDataOptions {
    /** Stable cache key for the requested resource. */
    key: string
    enabled?: boolean
    /** Skip fetching when dashboard is in edit mode */
    isEditMode?: boolean
    /** Auto-refresh interval in ms (0 = disabled) */
    pollInterval?: number
}

interface UseWidgetDataReturn<T> {
    data: T | null
    loading: boolean
    error: Error | null
    refetch: () => void
}

export function useWidgetData<T>(
    fetcher: () => Promise<T>,
    options: UseWidgetDataOptions
): UseWidgetDataReturn<T> {
    const { key, isEditMode = false, pollInterval = 0, enabled = true } = options
    const scope = useAuthStore(s => s.cacheScope)
    const { data, error, isLoading, mutate } = useSWR<T>(
        scope && enabled ? [scopeKey(scope), key] : null,
        fetcher,
        {
            dedupingInterval: 2_000,
            isPaused: () => isEditMode,
            errorRetryCount: 1,
            errorRetryInterval: 3_000,
            refreshInterval: pollInterval,
            revalidateOnFocus: false,
        }
    )

    const refetch = useCallback(() => {
        void mutate().catch(() => { /* SWR exposes the error in widget state. */ })
    }, [mutate])

    return {
        data: data ?? null,
        loading: isLoading || (enabled && isEditMode && !data),
        error: error instanceof Error ? error : error ? new Error(String(error)) : null,
        refetch,
    }
}
