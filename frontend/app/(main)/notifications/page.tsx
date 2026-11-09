// frontend/app/(main)/notifications/page.tsx
"use client"

import {
  Bell,
  CheckCheck,
  Inbox,
  RefreshCw,
} from "lucide-react"
import { useRouter } from "next/navigation"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import {
  NotificationItem,
  collapseNotifications,
  groupNotificationsByDate,
} from "@/components/shared/notification-item"
import { ScrollSentinel } from "@/components/shared/scroll-sentinel"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useCachedLoad } from "@/hooks/useCachedLoad"
import { useLatestRequest } from "@/hooks/useLatestRequest"
import { useHasTabCache, useTabState } from "@/hooks/useTabState"
import { api } from "@/lib/api"
import { notificationSchema } from "@/lib/schemas/events"
import { cn } from "@/lib/utils"
import { useNotificationStore } from "@/stores/useNotificationStore"
import { useTabsStore } from "@/stores/useTabsStore"
import { createTabStateKey } from "@/stores/useTabStateStore"
import { useWebSocketStore } from "@/stores/useWebSocketStore"
import type { NotificationResponse } from "@/types/notification"

const tabStateKeys = {
  "items": createTabStateKey<NotificationResponse[]>("frontend/app/(main)/notifications/page.tsx:items"),
  "totalCount": createTabStateKey<number>("frontend/app/(main)/notifications/page.tsx:totalCount"),
  "unreadCount": createTabStateKey<number>("frontend/app/(main)/notifications/page.tsx:unreadCount"),
  "hasMore": createTabStateKey<boolean>("frontend/app/(main)/notifications/page.tsx:hasMore"),
  "filter": createTabStateKey<"all" | "unread">("frontend/app/(main)/notifications/page.tsx:filter"),
}


// ── Constants ───────────────────────────────────────────────────────────

const PAGE_SIZE = 30

// ── Loading Skeleton ────────────────────────────────────────────────────

function NotificationListSkeleton() {
    return (
        <div className="flex flex-col gap-3">
            {Array.from({ length: 6 }).map((_, i) => (
                <div key={i} className="flex gap-4 rounded-lg border px-4 py-3.5">
                    <Skeleton className="h-2.5 w-2.5 rounded-full mt-1 shrink-0" />
                    <div className="flex-1 space-y-2">
                        <div className="flex items-center justify-between">
                            <Skeleton className="h-4 w-2/3" />
                            <Skeleton className="h-3 w-10" />
                        </div>
                        <Skeleton className="h-3.5 w-full" />
                        <Skeleton className="h-3.5 w-4/5" />
                    </div>
                </div>
            ))}
        </div>
    )
}

// ── Empty State ─────────────────────────────────────────────────────────

function NotificationEmptyState({ hasFilter }: { hasFilter: boolean }) {
    return (
        <div className="flex flex-col items-center justify-center gap-4 py-20 text-center">
            <div className="flex h-16 w-16 items-center justify-center rounded-full bg-muted">
                <Inbox className="h-8 w-8 text-muted-foreground" />
            </div>
            <div className="space-y-1.5">
                <p className="text-base font-medium text-muted-foreground">
                    {hasFilter ? "Нет непрочитанных уведомлений" : "Нет уведомлений"}
                </p>
                <p className="text-sm text-muted-foreground/70">
                    {hasFilter
                        ? "Все уведомления прочитаны"
                        : "Новые уведомления от системы автоматизации появятся здесь"}
                </p>
            </div>
        </div>
    )
}

// ── Stats Bar ───────────────────────────────────────────────────────────

function StatsBar({
    total,
    unread,
    loading,
}: {
    total: number
    unread: number
    loading: boolean
}) {
    if (loading) {
        return (
            <div className="flex items-center gap-4">
                <Skeleton className="h-5 w-24" />
                <Skeleton className="h-5 w-20" />
            </div>
        )
    }
    return (
        <div className="flex items-center gap-4 text-sm text-muted-foreground">
            <span>Всего: <span className="font-medium text-foreground">{total}</span></span>
            {unread > 0 && (
                <span>Непрочитанных: <span className="font-medium text-primary">{unread}</span></span>
            )}
        </div>
    )
}

// ── Main Page ───────────────────────────────────────────────────────────

export default function NotificationsPage() {
    const router = useRouter()
    const openTab = useTabsStore((s) => s.openTab)
    const globalUnreadCount = useNotificationStore((s) => s.unreadCount)
    const fetchGlobal = useNotificationStore((s) => s.fetchNotifications)

    const hasCachedItems = useHasTabCache(tabStateKeys["items"])
    const [items, setItems] = useTabState(tabStateKeys["items"], [])
    const [totalCount, setTotalCount] = useTabState(tabStateKeys["totalCount"], 0)
    const [unreadCount, setUnreadCount] = useTabState(tabStateKeys["unreadCount"], 0)
    const [hasMore, setHasMore] = useTabState(tabStateKeys["hasMore"], false)

    const [loading, setLoading] = useState(!hasCachedItems)
    const [loadingMore, setLoadingMore] = useState(false)
    const [filter, setFilter] = useTabState(tabStateKeys["filter"], "all")

    const scrollRef = useRef<HTMLDivElement>(null)
    const listRequests = useLatestRequest()
    const loadMoreBusy = useRef(false)

    const updateTabTitle = useTabsStore((s) => s.updateTabTitle)

    // P6: Set proper tab title on mount
    useEffect(() => {
        updateTabTitle("/notifications", "Уведомления")
    }, [updateTabTitle])

    // ── Fetch ───────────────────────────────────────────────────────────

    const fetchData = useCallback(
        async (unreadOnly?: boolean) => {
            const isCurrent = listRequests.begin()
            setLoadingMore(false)
            setLoading(true)
            try {
                const res = await api.system.notifications.list({
                    limit: PAGE_SIZE,
                    unreadOnly: unreadOnly ?? (filter === "unread" ? true : undefined),
                })
                if (!isCurrent()) return
                setItems(res.items ?? [])
                setUnreadCount(res.unreadCount ?? 0)
                setTotalCount(res.items?.length ?? 0)
                setHasMore((res.items?.length ?? 0) >= PAGE_SIZE)
            } catch (err) {
                if (!isCurrent()) return
                console.error("[NotificationsPage] fetch error:", err)
                setItems([])
            } finally {
                if (isCurrent()) setLoading(false)
            }
        },
        [filter, setItems, setUnreadCount, setTotalCount, setHasMore, listRequests]
    )

    const loadMore = useCallback(async () => {
        if (loading || loadMoreBusy.current || !hasMore) return
        loadMoreBusy.current = true
        const isCurrent = listRequests.capture()
        setLoadingMore(true)
        try {
            const currentItems = items
            const res = await api.system.notifications.list({
                limit: PAGE_SIZE,
                offset: currentItems.length,
                unreadOnly: filter === "unread" ? true : undefined,
            })
            if (!isCurrent()) return
            const newItems = res.items ?? []
            setItems(previous => {
                const existingIds = new Set(previous.map(item => item.id))
                return [...previous, ...newItems.filter(item => !existingIds.has(item.id))]
            })
            setHasMore(newItems.length >= PAGE_SIZE)
        } catch (err) {
            if (isCurrent()) console.error("[NotificationsPage] loadMore error:", err)
        } finally {
            loadMoreBusy.current = false
            if (isCurrent()) setLoadingMore(false)
        }
    }, [items, filter, loading, hasMore, setItems, setHasMore, listRequests])

    useCachedLoad(fetchData, hasCachedItems, scrollRef)

    // ── P7: Real-time WS insertion ─────────────────────────────────────

    const lastMessage = useWebSocketStore((s) => s.lastMessage)

    useEffect(() => {
        if (!lastMessage || lastMessage.type !== "notification") return
        const notif = notificationSchema.safeParse(lastMessage.payload).data
        if (!notif?.id) return

        // Avoid duplicates
        setItems((prev) => {
            if (prev.some((n) => n.id === notif.id)) return prev
            return [notif, ...prev]
        })
        setUnreadCount((prev) => prev + 1)
        setTotalCount((prev) => prev + 1)
    }, [lastMessage, setItems, setUnreadCount, setTotalCount])

    // ── Actions ─────────────────────────────────────────────────────────

    const handleRefresh = () => {
        if (scrollRef.current) scrollRef.current.scrollTop = 0
        fetchData()
    }

    const handleMarkAsRead = async (id: string) => {
        setItems((prev) => prev.map((n) => (n.id === id ? { ...n, isRead: true } : n)))
        setUnreadCount((prev) => Math.max(0, prev - 1))
        try {
            await api.system.notifications.markAsRead(id)
            fetchGlobal()
        } catch {
            fetchData()
        }
    }

    const handleMarkAsUnread = async (id: string) => {
        setItems((prev) => prev.map((n) => (n.id === id ? { ...n, isRead: false } : n)))
        setUnreadCount((prev) => prev + 1)
        try {
            await api.system.notifications.markAsUnread(id)
            fetchGlobal()
        } catch {
            fetchData()
        }
    }

    const handleDelete = async (id: string) => {
        const target = items.find((n) => n.id === id)
        setItems((prev) => prev.filter((n) => n.id !== id))
        if (target && !target.isRead) {
            setUnreadCount((prev) => Math.max(0, prev - 1))
        }
        try {
            await api.system.notifications.delete(id)
            fetchGlobal()
        } catch {
            fetchData()
        }
    }

    const handleMarkAllAsRead = async () => {
        setItems((prev) => prev.map((n) => ({ ...n, isRead: true })))
        setUnreadCount(0)
        try {
            await api.system.notifications.markAllAsRead()
            fetchGlobal()
        } catch {
            fetchData()
        }
    }

    const handleNavigate = (link: string) => {
        openTab({
            id: link,
            title: "Уведомление",
            url: link,
        })
        router.push(link)
    }

    // ── Batch actions (for grouped notifications) ───────────────────────

    const handleBatchMarkAsRead = async (ids: string[]) => {
        const idsSet = new Set(ids)
        setItems((prev) => prev.map((n) => (idsSet.has(n.id) ? { ...n, isRead: true } : n)))
        const unreadInBatch = items.filter((n) => idsSet.has(n.id) && !n.isRead).length
        setUnreadCount((prev) => Math.max(0, prev - unreadInBatch))
        try {
            await Promise.all(ids.map((id) => api.system.notifications.markAsRead(id)))
            fetchGlobal()
        } catch {
            fetchData()
        }
    }

    const handleBatchDelete = async (ids: string[]) => {
        const idsSet = new Set(ids)
        const unreadInBatch = items.filter((n) => idsSet.has(n.id) && !n.isRead).length
        setItems((prev) => prev.filter((n) => !idsSet.has(n.id)))
        setUnreadCount((prev) => Math.max(0, prev - unreadInBatch))
        try {
            await Promise.all(ids.map((id) => api.system.notifications.delete(id)))
            fetchGlobal()
        } catch {
            fetchData()
        }
    }

    // Group items by date, then collapse duplicates within each group
    const groups = useMemo(() => {
        const dateGroups = groupNotificationsByDate(items, "full")
        return dateGroups.map((group) => ({
            ...group,
            collapsed: collapseNotifications(group.items),
        }))
    }, [items])

    return (
        <div className="flex flex-1 min-h-0 flex-col gap-4 p-4 max-w-5xl w-full mx-auto overflow-hidden">
            {/* Header */}
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                    <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10">
                        <Bell className="h-5 w-5 text-primary" />
                    </div>
                    <div>
                        <h1 className="text-xl font-bold">Уведомления</h1>
                        <StatsBar
                            total={items.length}
                            unread={unreadCount}
                            loading={loading && items.length === 0}
                        />
                    </div>
                </div>
                <div className="flex items-center gap-2">
                    {unreadCount > 0 && (
                        <Button
                            variant="outline"
                            size="sm"
                            className="gap-1.5"
                            onClick={handleMarkAllAsRead}
                        >
                            <CheckCheck className="h-4 w-4" />
                            Прочитать все
                        </Button>
                    )}
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={handleRefresh}
                        disabled={loading}
                    >
                        <RefreshCw className={cn("h-4 w-4 mr-1.5", loading && "animate-spin")} />
                        Обновить
                    </Button>
                </div>
            </div>

            {/* P8: Tabs filter instead of Select */}
            <Tabs value={filter} onValueChange={(v) => setFilter(v as "all" | "unread")}>
                <TabsList className="w-fit">
                    <TabsTrigger value="all" className="gap-1.5">
                        Все
                        {items.length > 0 && (
                            <Badge variant="secondary" className="h-5 min-w-5 justify-center px-1.5 text-[10px] rounded-full">
                                {items.length}
                            </Badge>
                        )}
                    </TabsTrigger>
                    <TabsTrigger value="unread" className="gap-1.5">
                        Непрочитанные
                        {globalUnreadCount > 0 && (
                            <Badge variant="secondary" className="h-5 min-w-5 justify-center px-1.5 text-[10px] rounded-full">
                                {globalUnreadCount}
                            </Badge>
                        )}
                    </TabsTrigger>
                </TabsList>
            </Tabs>

            {/* Content */}
            <ScrollArea className="flex-1 min-h-0" viewportRef={scrollRef}>
                {loading && items.length === 0 ? (
                    <NotificationListSkeleton />
                ) : items.length === 0 ? (
                    <NotificationEmptyState hasFilter={filter !== "all"} />
                ) : (
                    <div className="flex flex-col gap-1 pr-4">
                        {groups.map((group, gi) => (
                            <div key={group.label}>
                                {gi > 0 && <Separator className="my-3" />}
                                <div className="sticky top-0 z-10 bg-workspace-surface/95 backdrop-blur-sm pb-2 pt-1">
                                    <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground/60">
                                        {group.label}
                                    </span>
                                </div>
                                <div className="flex flex-col gap-2">
                                    {group.collapsed.map((notif) => (
                                        <NotificationItem
                                            key={notif.id}
                                            notification={notif}
                                            variant="full"
                                            onMarkAsRead={handleMarkAsRead}
                                            onMarkAsUnread={handleMarkAsUnread}
                                            onDelete={handleDelete}
                                            onBatchMarkAsRead={handleBatchMarkAsRead}
                                            onBatchDelete={handleBatchDelete}
                                            onNavigate={handleNavigate}
                                        />
                                    ))}
                                </div>
                            </div>
                        ))}
                    </div>
                )}

                {/* Infinite scroll */}
                <ScrollSentinel
                    onIntersect={loadMore}
                    loading={loadingMore}
                    enabled={hasMore && !loading}
                    scrollContainer={scrollRef}
                />

                {/* P10: End-of-list indicator */}
                {!hasMore && items.length > 0 && !loading && (
                    <p className="text-center text-xs text-muted-foreground/50 py-6">
                        Все уведомления загружены
                    </p>
                )}
            </ScrollArea>
        </div>
    )
}
