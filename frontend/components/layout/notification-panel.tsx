// frontend/components/layout/notification-panel.tsx
"use client"

import * as React from "react"
import { useRouter } from "next/navigation"
import { Bell, CheckCheck, Inbox } from "lucide-react"

import {
    Sheet,
    SheetContent,
    SheetHeader,
    SheetTitle,
    SheetDescription,
} from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Separator } from "@/components/ui/separator"
import { useNotificationStore } from "@/stores/useNotificationStore"
import { useTabsStore } from "@/stores/useTabsStore"
import { useShortcut } from "@/hooks/useShortcut"
import {
    NotificationItem,
    groupNotificationsByDate,
} from "@/components/shared/notification-item"

// ── Loading skeleton ────────────────────────────────────────────────────

function NotificationSkeleton() {
    return (
        <div className="flex flex-col gap-2 p-3">
            {Array.from({ length: 5 }).map((_, i) => (
                <div key={i} className="flex gap-3 px-3 py-2.5">
                    <Skeleton className="h-2 w-2 rounded-full mt-1.5 shrink-0" />
                    <div className="flex-1 space-y-1.5">
                        <Skeleton className="h-4 w-3/4" />
                        <Skeleton className="h-3 w-full" />
                        <Skeleton className="h-2.5 w-20" />
                    </div>
                </div>
            ))}
        </div>
    )
}

// ── Empty state ─────────────────────────────────────────────────────────

function EmptyState() {
    return (
        <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
            <div className="flex h-14 w-14 items-center justify-center rounded-full bg-muted">
                <Inbox className="h-7 w-7 text-muted-foreground" />
            </div>
            <div className="space-y-1">
                <p className="text-sm font-medium text-muted-foreground">Нет уведомлений</p>
                <p className="text-xs text-muted-foreground/70">
                    Новые уведомления появятся здесь
                </p>
            </div>
        </div>
    )
}

// ── Main panel ──────────────────────────────────────────────────────────

export function NotificationPanel() {
    const router = useRouter()
    const openTab = useTabsStore((s) => s.openTab)

    const isPanelOpen = useNotificationStore((s) => s.isPanelOpen)
    const setPanelOpen = useNotificationStore((s) => s.setPanelOpen)
    const items = useNotificationStore((s) => s.items)
    const unreadCount = useNotificationStore((s) => s.unreadCount)
    const isLoading = useNotificationStore((s) => s.isLoading)
    const markAsRead = useNotificationStore((s) => s.markAsRead)
    const markAllAsRead = useNotificationStore((s) => s.markAllAsRead)

    // Group items by date (compact mode for panel)
    const groups = React.useMemo(() => groupNotificationsByDate(items, "compact"), [items])

    // Navigate to notification link (MDI tab integration)
    const handleNavigate = React.useCallback(
        (link: string) => {
            openTab({
                id: link,
                title: "Уведомление",
                url: link,
            })
            router.push(link)
            setPanelOpen(false)
        },
        [openTab, router, setPanelOpen]
    )

    // Navigate to full notifications page
    const handleViewAll = React.useCallback(() => {
        openTab({
            id: "/notifications",
            title: "Уведомления",
            url: "/notifications",
        })
        router.push("/notifications")
        setPanelOpen(false)
    }, [openTab, router, setPanelOpen])

    // Keyboard shortcut: Ctrl+Shift+N to toggle panel
    useShortcut(
        "nav.notifications",
        "mod+shift+n",
        "Уведомления",
        "navigation",
        () => setPanelOpen(!isPanelOpen)
    )

    return (
        <Sheet open={isPanelOpen} onOpenChange={setPanelOpen}>
            <SheetContent
                side="right"
                className="flex w-full flex-col gap-0 p-0 sm:max-w-[400px]"
            >
                {/* Header */}
                <SheetHeader className="flex-row items-center justify-between gap-2 border-b pl-4 pr-10 py-3 space-y-0">
                    <div className="flex items-center gap-2">
                        <SheetTitle className="text-base">Уведомления</SheetTitle>
                        {unreadCount > 0 && (
                            <Badge
                                variant="secondary"
                                className="h-5 min-w-5 justify-center px-1.5 text-[10px] font-bold rounded-full"
                            >
                                {unreadCount}
                            </Badge>
                        )}
                    </div>
                    <SheetDescription className="sr-only">
                        Панель уведомлений системы
                    </SheetDescription>
                    {unreadCount > 0 && (
                        <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 gap-1.5 text-xs text-muted-foreground"
                            onClick={() => markAllAsRead()}
                        >
                            <CheckCheck className="h-3.5 w-3.5" />
                            Прочитать все
                        </Button>
                    )}
                </SheetHeader>

                {/* Content */}
                <ScrollArea className="flex-1">
                    {isLoading && items.length === 0 ? (
                        <NotificationSkeleton />
                    ) : items.length === 0 ? (
                        <EmptyState />
                    ) : (
                        <div className="flex flex-col py-1">
                            {groups.map((group, gi) => (
                                <React.Fragment key={group.label}>
                                    {gi > 0 && <Separator className="my-1" />}
                                    <div className="px-4 pt-2 pb-1">
                                        <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground/60">
                                            {group.label}
                                        </span>
                                    </div>
                                    <div className="flex flex-col gap-0.5 px-1">
                                        {group.items.map((notif) => (
                                            <NotificationItem
                                                key={notif.id}
                                                notification={notif}
                                                variant="compact"
                                                onMarkAsRead={markAsRead}
                                                onNavigate={handleNavigate}
                                            />
                                        ))}
                                    </div>
                                </React.Fragment>
                            ))}
                        </div>
                    )}
                </ScrollArea>

                {/* Footer */}
                <div className="border-t px-3 py-2">
                    <Button
                        variant="ghost"
                        size="sm"
                        className="w-full gap-1.5 text-xs text-muted-foreground"
                        onClick={handleViewAll}
                    >
                        <Bell className="h-3.5 w-3.5" />
                        Все уведомления
                    </Button>
                </div>
            </SheetContent>
        </Sheet>
    )
}
