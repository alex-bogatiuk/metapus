"use client"

import { useCallback, useRef, useState } from "react"
import type {
    AdvancedFilterItem,
    BatchActionResponse,
    BatchActionType,
    BatchProgressEvent,
} from "@/types/common"
import { toast } from "sonner"

// ── Types ───────────────────────────────────────────────────────────────

/** Minimal document shape required by the hook (Pattern #7 — strict typing). */
interface DocumentLike {
    id: string
    posted: boolean
    deletionMark: boolean
}

/** Narrow API surface — only the methods the hook actually calls. */
interface BatchCapableApi<T> {
    get: (id: string) => Promise<T>
    list?: (params?: { limit?: number; filter?: AdvancedFilterItem[] }) => Promise<{ items: T[] }>
    batchAction: (ids: string[], action: BatchActionType) => Promise<BatchActionResponse>
    batchActionByFilter: (req: import("@/types/common").BatchActionByFilterRequest) => Promise<BatchActionResponse>
    /** Base API path (used by SSE streaming). */
    _basePath: string
}

interface UseDocumentBatchActionsOptions<T extends DocumentLike> {
    /** Document API instance (e.g. api.goodsReceipts). */
    api: BatchCapableApi<T>
    /** Replace items in-place without scroll reset. */
    replaceItems: (updated: T[]) => void
    /** Full list refresh (used for large batches). */
    refresh: () => void
    /** Current items (needed for keyboard shortcut targets). */
    items: T[]
    /** Currently selected IDs. */
    selectedIds: string[]
    /** Focused item ID (for single-item actions). */
    focusedId: string | null
    /** Whether virtual "select all by filter" is active. */
    selectAllByFilter: boolean
    /** IDs excluded from virtual select (user manually unchecked). */
    excludedIds: string[]
    /** Current resolved filter items (for filter-based batch). */
    currentFilters: AdvancedFilterItem[]
    /** Whether showing deleted entities. */
    showDeleted: boolean
    /** Clear selection (called after filter-based batch completes). */
    clearSelection: () => void
}

interface UseDocumentBatchActionsReturn<T extends DocumentLike> {
    /** Post selected/targeted documents. */
    handlePostBatch: (docs: T[]) => Promise<void>
    /** Unpost selected/targeted documents. */
    handleUnpostBatch: (docs: T[]) => Promise<void>
    /** Set or clear deletion mark on documents. */
    handleToggleDeletionMarkBatch: (docs: T[], mark: boolean) => Promise<void>
    /** Whether a batch action is currently in progress. */
    batchBusy: boolean
    /** Build context menu items for DataTable's renderContextMenu. */
    getBatchMenuCounts: (targets: T[]) => {
        postableCount: number
        unpostableCount: number
        markableCount: number
        unmarkeableCount: number
    }
}

// ── Constants ───────────────────────────────────────────────────────────

/**
 * Threshold: if N ≤ this value we do point-update (individual GET per doc),
 * otherwise we call `refresh()` which re-fetches the entire list.
 * Point-update preserves scroll & focus; refresh is an O(1) HTTP call.
 */
const POINT_UPDATE_THRESHOLD = 20

// ── Hook ────────────────────────────────────────────────────────────────

/**
 * Universal hook for batch document operations (post, unpost, deletion mark).
 *
 * Supports two modes:
 *  1. ID-based: sends explicit IDs (normal multi-select)
 *  2. Filter-based: sends current filter (virtual "select all by filter", Gmail-style)
 *
 * Patterns applied: #3 (Custom Hooks), #6 (Composition over Copy-Paste), #7 (Strict Typing)
 */
export function useDocumentBatchActions<T extends DocumentLike>(
    options: UseDocumentBatchActionsOptions<T>,
): UseDocumentBatchActionsReturn<T> {
    const {
        api: docApi, replaceItems, refresh,
        selectAllByFilter, excludedIds, currentFilters, showDeleted,
        clearSelection,
    } = options
    const [batchBusy, setBatchBusy] = useState(false)
    const busyRef = useRef(false)

    // ── Adaptive refetch ────────────────────────────────────────────────
    const refetchAfterBatch = useCallback(
        async (ids: string[]) => {
            if (ids.length > POINT_UPDATE_THRESHOLD || !docApi.list) {
                // Large batch → single list refresh (no N+1)
                refresh()
            } else {
                // Small batch → point-update (preserves scroll)
                // ERP Rule: NEVER do API calls in a loop. Use a single list request with 'in' operator.
                try {
                    const res = await docApi.list({
                        limit: ids.length,
                        filter: [{ field: "id", operator: "in", value: ids }],
                    })
                    if (res.items && res.items.length > 0) {
                        replaceItems(res.items)
                    }
                } catch (err) {
                    refresh() // Fallback to full refresh on error
                }
            }
        },
        [docApi, replaceItems, refresh],
    )

    // ── Generic batch executor (ID-based) ───────────────────────────────
    const executeBatch = useCallback(
        async (
            ids: string[],
            action: BatchActionType,
            successMsg: (result: BatchActionResponse) => string,
        ) => {
            if (ids.length === 0 || busyRef.current) return
            busyRef.current = true
            setBatchBusy(true)

            try {
                const result = await docApi.batchAction(ids, action)

                // Refetch affected documents (adaptive strategy)
                await refetchAfterBatch(ids)

                // Toast — always show for all operations (including single)
                if (result.failed > 0) {
                    toast.warning(
                        `${successMsg(result)}, ошибок: ${result.failed}`,
                    )
                } else {
                    toast.success(successMsg(result))
                }
            } catch (err) {
                toast.error(
                    err instanceof Error ? err.message : "Действие отклонено. Проверьте права доступа и состояние документа.",
                )
            } finally {
                busyRef.current = false
                setBatchBusy(false)
            }
        },
        [docApi, refetchAfterBatch],
    )

    // ── Filter-based batch executor with SSE progress ────────
    const executeFilterBatch = useCallback(
        async (
            action: BatchActionType,
            successLabel: string,
        ) => {
            if (busyRef.current) return
            busyRef.current = true
            setBatchBusy(true)

            const abortController = new AbortController()
            const toastId = toast.loading("Подготовка...", {
                cancel: {
                    label: "Отменить",
                    onClick: () => abortController.abort(),
                },
                duration: Infinity,
            })

            try {
                const { fetchSSE } = await import("@/lib/sse-fetch")
                const basePath = docApi._basePath

                await fetchSSE<BatchProgressEvent>(
                    `${basePath}/batch-action-by-filter`,
                    {
                        filter: currentFilters,
                        action,
                        excludeIds: excludedIds.length > 0 ? excludedIds : undefined,
                        includeDeleted: showDeleted || undefined,
                    },
                    (event) => {
                        switch (event.type) {
                            case "started":
                                toast.loading(
                                    `${successLabel}: 0 / ${event.total.toLocaleString("ru-RU")}`,
                                    {
                                        id: toastId,
                                        cancel: {
                                            label: "Отменить",
                                            onClick: () => abortController.abort(),
                                        },
                                        duration: Infinity,
                                    },
                                )
                                break
                            case "progress": {
                                const pct = Math.round((event.processed / event.total) * 100)
                                toast.loading(
                                    `${successLabel}: ${event.processed.toLocaleString("ru-RU")} / ${event.total.toLocaleString("ru-RU")} (${pct}%)`,
                                    {
                                        id: toastId,
                                        cancel: {
                                            label: "Отменить",
                                            onClick: () => abortController.abort(),
                                        },
                                        duration: Infinity,
                                    },
                                )
                                break
                            }
                            case "completed":
                                if (event.failed > 0) {
                                    toast.warning(
                                        `${successLabel}: ${event.success.toLocaleString("ru-RU")}, ошибок: ${event.failed.toLocaleString("ru-RU")}`,
                                        { id: toastId },
                                    )
                                } else {
                                    toast.success(
                                        `${successLabel}: ${event.success.toLocaleString("ru-RU")}`,
                                        { id: toastId },
                                    )
                                }
                                break
                            case "cancelled":
                                toast.warning(
                                    `Отменено. ${successLabel}: ${event.success.toLocaleString("ru-RU")} из ${event.total.toLocaleString("ru-RU")}`,
                                    { id: toastId },
                                )
                                break
                        }
                    },
                    abortController.signal,
                )


                refresh()
                clearSelection()
            } catch (err) {
                if (abortController.signal.aborted) {
                    // User cancelled — toast already updated by "cancelled" event or we handle here
                    toast.warning("Операция отменена пользователем", { id: toastId })
                } else {
                    toast.error(
                        err instanceof Error ? err.message : "Действие отклонено. Проверьте права доступа и состояние документа.",
                        { id: toastId },
                    )
                }
                refresh()
                clearSelection()
            } finally {
                busyRef.current = false
                setBatchBusy(false)
            }
        },
        [docApi, currentFilters, excludedIds, showDeleted, refresh, clearSelection],
    )

    // ── Post batch ──────────────────────────────────────────────────────
    const handlePostBatch = useCallback(
        async (docs: T[]) => {
            if (selectAllByFilter) {
                await executeFilterBatch("post", "Проведено")
                return
            }
            const toPost = docs.filter((d) => !d.deletionMark)
            if (toPost.length === 0) return
            await executeBatch(
                toPost.map((d) => d.id),
                "post",
                (r) => `Проведено: ${r.success}`,
            )
        },
        [executeBatch, executeFilterBatch, selectAllByFilter],
    )

    // ── Unpost batch ────────────────────────────────────────────────────
    const handleUnpostBatch = useCallback(
        async (docs: T[]) => {
            if (selectAllByFilter) {
                await executeFilterBatch("unpost", "Отменено проведение")
                return
            }
            const toUnpost = docs.filter((d) => d.posted)
            if (toUnpost.length === 0) return
            await executeBatch(
                toUnpost.map((d) => d.id),
                "unpost",
                (r) => `Отменено проведение: ${r.success}`,
            )
        },
        [executeBatch, executeFilterBatch, selectAllByFilter],
    )

    // ── Deletion mark batch ─────────────────────────────────────────────
    const handleToggleDeletionMarkBatch = useCallback(
        async (docs: T[], mark: boolean) => {
            if (selectAllByFilter) {
                const action = mark ? "setDeletionMark" as const : "clearDeletionMark" as const
                await executeFilterBatch(
                    action,
                    mark ? "Помечено на удаление" : "Снято пометок",
                )
                return
            }
            const ids = docs.map((d) => d.id)
            if (ids.length === 0) return
            const action = mark
                ? ("setDeletionMark" as const)
                : ("clearDeletionMark" as const)
            await executeBatch(
                ids,
                action,
                (r) =>
                    mark
                        ? `Помечено на удаление: ${r.success}`
                        : `Снято пометок: ${r.success}`,
            )
        },
        [executeBatch, executeFilterBatch, selectAllByFilter],
    )

    // ── Accurate counts for context menu ────────────────────────────────
    const getBatchMenuCounts = useCallback((targets: T[]) => {
        return {
            // All non-deleted docs are postable (already-posted = repost, like 1C)
            postableCount: targets.filter(
                (d) => !d.deletionMark,
            ).length,
            unpostableCount: targets.filter((d) => d.posted).length,
            markableCount: targets.filter((d) => !d.deletionMark).length,
            unmarkeableCount: targets.filter((d) => d.deletionMark).length,
        }
    }, [])

    return {
        handlePostBatch,
        handleUnpostBatch,
        handleToggleDeletionMarkBatch,
        batchBusy,
        getBatchMenuCounts,
    }
}
