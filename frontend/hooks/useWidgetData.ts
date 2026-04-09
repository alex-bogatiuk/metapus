"use client"

import { useState, useCallback, useEffect, useRef, useMemo } from "react"

/**
 * useWidgetData — standardized data fetching hook for dashboard widgets.
 *
 * Features:
 *  - AbortController: cancels in-flight request on unmount or deps change
 *  - skipInEditMode: pauses fetching when dashboard is in edit mode
 *  - pollInterval: optional auto-refresh (e.g. every 60s)
 *  - Single retry on failure (3s delay)
 */

interface UseWidgetDataOptions {
    /** Dependency array — refetch when these change */
    deps?: unknown[]
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
    fetcher: (signal: AbortSignal) => Promise<T>,
    options: UseWidgetDataOptions = {}
): UseWidgetDataReturn<T> {
    const { deps = [], isEditMode = false, pollInterval = 0 } = options

    const [data, setData] = useState<T | null>(null)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<Error | null>(null)
    const retryCount = useRef(0)
    const controllerRef = useRef<AbortController | null>(null)
    const fetcherRef = useRef(fetcher)
    fetcherRef.current = fetcher

    // eslint-disable-next-line react-hooks/exhaustive-deps
    const depsKey = useMemo(() => JSON.stringify(deps), [...deps])

    const fetchData = useCallback(
        (signal: AbortSignal) => {
            setLoading(true)
            setError(null)

            fetcherRef.current(signal)
                .then((result) => {
                    if (!signal.aborted) {
                        setData(result)
                        setLoading(false)
                        retryCount.current = 0
                    }
                })
                .catch((err) => {
                    if (signal.aborted) return
                    if (retryCount.current < 1) {
                        retryCount.current++
                        setTimeout(() => {
                            if (!signal.aborted) fetchData(signal)
                        }, 3000)
                    } else {
                        setError(err instanceof Error ? err : new Error(String(err)))
                        setLoading(false)
                    }
                })
        },
        // eslint-disable-next-line react-hooks/exhaustive-deps -- refetch when deps change
        [depsKey]
    )

    useEffect(() => {
        if (isEditMode) return

        const controller = new AbortController()
        controllerRef.current = controller
        fetchData(controller.signal)

        let intervalId: ReturnType<typeof setInterval> | null = null
        if (pollInterval > 0) {
            intervalId = setInterval(() => {
                if (!controller.signal.aborted) {
                    fetchData(controller.signal)
                }
            }, pollInterval)
        }

        return () => {
            controller.abort()
            if (intervalId) clearInterval(intervalId)
        }
    }, [fetchData, isEditMode, pollInterval])

    const refetch = useCallback(() => {
        if (controllerRef.current) controllerRef.current.abort()
        const controller = new AbortController()
        controllerRef.current = controller
        retryCount.current = 0
        fetchData(controller.signal)
    }, [fetchData])

    return { data, loading, error, refetch }
}
