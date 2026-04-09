/**
 * useRelatedDocuments — lazy-loading hook for related documents sidebar.
 *
 * Fetches related documents only when the sidebar is expanded (not collapsed).
 * Uses AbortController to cancel in-flight requests on navigation.
 *
 * @example
 *   const { groups, loading } = useRelatedDocuments({
 *     fetcher: (id) => api.goodsReceipts.getRelatedDocuments(id),
 *     documentId: params.id,
 *     enabled: !sidebarCollapsed,
 *   })
 */
import { useState, useEffect, useRef, useCallback } from "react"
import type { RelatedDocGroup, RelatedDocTreeNode, RelatedDocumentsResponse } from "@/types/common"

interface UseRelatedDocumentsOptions {
    /** API fetch function */
    fetcher: (id: string) => Promise<RelatedDocumentsResponse>
    /** Document ID to fetch related documents for */
    documentId: string
    /** When false, no fetch is initiated (lazy loading). Typically: !sidebarCollapsed */
    enabled: boolean
}

interface UseRelatedDocumentsResult {
    /** Flat groups for backward-compat rendering (flatGroups from API + tree flattened) */
    groups: RelatedDocGroup[]
    /** Subordination tree (if available) */
    tree: RelatedDocTreeNode | null
    /** Raw API response */
    data: RelatedDocumentsResponse | null
    loading: boolean
    error: string | null
    /** Manually refresh results */
    refresh: () => void
}

/** Flatten a tree into RelatedDocGroup[] for backward-compat tab rendering. */
function flattenTreeToGroups(node: RelatedDocTreeNode): RelatedDocGroup[] {
    // Group all tree nodes by entityName
    const groupMap = new Map<string, { node: RelatedDocTreeNode; items: RelatedDocTreeNode[] }>()

    function walk(n: RelatedDocTreeNode) {
        const existing = groupMap.get(n.entityName)
        if (existing) {
            existing.items.push(n)
        } else {
            groupMap.set(n.entityName, { node: n, items: [n] })
        }
        n.children?.forEach(walk)
    }
    walk(node)

    return Array.from(groupMap.values()).map(({ node, items }) => ({
        entityName: node.entityName,
        entityType: node.entityType,
        presentation: node.entityName, // best-effort fallback
        routePrefix: node.routePrefix,
        items: items.map(i => ({
            id: i.id,
            presentation: i.presentation,
            number: i.number,
            date: i.date,
            posted: i.posted,
            deletionMark: i.deletionMark,
            amount: i.amount,
            currencyId: i.currencyId,
            previewData: i.previewData,
        })),
        totalCount: items.length,
    }))
}

export function useRelatedDocuments({
    fetcher,
    documentId,
    enabled,
}: UseRelatedDocumentsOptions): UseRelatedDocumentsResult {
    const [data, setData] = useState<RelatedDocumentsResponse | null>(null)
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState<string | null>(null)
    const abortRef = useRef<AbortController | null>(null)
    const fetchedRef = useRef<string | null>(null) // track which doc ID we already fetched

    const doFetch = useCallback(() => {
        if (!documentId || !enabled) return

        // Skip if we already fetched for this document
        if (fetchedRef.current === documentId) return

        // Cancel any in-flight request
        abortRef.current?.abort()
        const controller = new AbortController()
        abortRef.current = controller

        setLoading(true)
        setError(null)

        fetcher(documentId)
            .then((res) => {
                if (controller.signal.aborted) return
                setData(res)
                fetchedRef.current = documentId
            })
            .catch((err) => {
                if (controller.signal.aborted) return
                setError(err instanceof Error ? err.message : "Ошибка загрузки связей")
            })
            .finally(() => {
                if (controller.signal.aborted) return
                setLoading(false)
            })
    }, [fetcher, documentId, enabled])

    useEffect(() => {
        let isMounted = true
        void Promise.resolve().then(() => {
            if (isMounted) doFetch()
        })
        return () => {
            isMounted = false
            abortRef.current?.abort()
        }
    }, [doFetch])

    // Reset when document ID changes
    useEffect(() => {
        let isMounted = true
        void Promise.resolve().then(() => {
            if (isMounted) {
                fetchedRef.current = null
                setData(null)
            }
        })
        return () => {
            isMounted = false
        }
    }, [documentId])

    const refresh = useCallback(() => {
        fetchedRef.current = null
        doFetch()
    }, [doFetch])

    // Build backward-compat groups from tree + flatGroups
    const groups: RelatedDocGroup[] = []
    if (data?.tree) {
        groups.push(...flattenTreeToGroups(data.tree))
    }
    if (data?.flatGroups) {
        groups.push(...data.flatGroups)
    }

    return {
        groups,
        tree: data?.tree ?? null,
        data,
        loading,
        error,
        refresh,
    }
}

