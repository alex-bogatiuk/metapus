"use client"

import * as React from "react"
import { Bell } from "lucide-react"
import { formatDistanceToNow } from "date-fns"
import { ru } from "date-fns/locale"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from "@/components/ui/popover"
import { Badge } from "@/components/ui/badge"
import { ScrollArea } from "@/components/ui/scroll-area"
import { api } from "@/lib/api"
import { useWebsocket } from "@/hooks/useWebsocket"
import { cn } from "@/lib/utils"
import type { NotificationResponse } from "@/types/notification"

export function NotificationBell() {
    const { lastMessage } = useWebsocket()
    const [isOpen, setIsOpen] = React.useState(false)
    const [items, setItems] = React.useState<NotificationResponse[]>([])
    const [unreadCount, setUnreadCount] = React.useState(0)

    const fetchNotifications = React.useCallback(async () => {
        try {
            const res = await api.system.notifications.list({ unreadOnly: true, limit: 10 })
            setItems(res.items || [])
            setUnreadCount(res.unreadCount || 0)
        } catch (err) {
            console.error("Failed to fetch notifications:", err)
        }
    }, [])

    React.useEffect(() => {
        fetchNotifications()
        const interval = setInterval(fetchNotifications, 60000)
        return () => clearInterval(interval)
    }, [fetchNotifications])

    React.useEffect(() => {
        if (lastMessage?.type === "notification") {
            // Optimistic update: add notification from WS payload directly
            const notif = lastMessage.payload as NotificationResponse | undefined
            if (notif?.id) {
                setItems((prev) => [notif, ...prev].slice(0, 10))
                setUnreadCount((prev) => prev + 1)
            }
            if (notif?.title) {
                toast(notif.title, {
                    description: notif.message,
                })
            }
        }
    }, [lastMessage])

    const markAsRead = async (id: string) => {
        try {
            // Optimistic UI
            setItems((prev) => prev.map((n) => (n.id === id ? { ...n, isRead: true } : n)))
            setUnreadCount((prev) => Math.max(0, prev - 1))
            await api.system.notifications.markAsRead(id)
        } catch (err) {
            console.error("Failed to mark as read:", err)
            fetchNotifications() // Rollback on error
        }
    }

    const markAllAsRead = async () => {
        try {
            // Optimistic UI
            setItems((prev) => prev.map((n) => ({ ...n, isRead: true })))
            setUnreadCount(0)
            await api.system.notifications.markAllAsRead()
        } catch (err) {
            console.error("Failed to mark all as read:", err)
            fetchNotifications() // Rollback on error
        }
    }

    return (
        <Popover open={isOpen} onOpenChange={setIsOpen}>
            <PopoverTrigger asChild>
                <Button variant="ghost" size="icon" className="relative h-8 w-8">
                    <Bell className="h-4 w-4" />
                    {unreadCount > 0 && (
                        <span className="absolute right-1 top-1 flex h-2.5 w-2.5 items-center justify-center rounded-full bg-destructive text-[8px] font-bold text-destructive-foreground ring-2 ring-background">
                            {unreadCount > 99 ? "99+" : unreadCount}
                        </span>
                    )}
                </Button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-80 p-0">
                <div className="flex items-center justify-between border-b px-4 py-3">
                    <span className="text-sm font-semibold">Уведомления</span>
                    {unreadCount > 0 && (
                        <Button
                            variant="link"
                            size="sm"
                            className="h-auto p-0 text-xs"
                            onClick={markAllAsRead}
                        >
                            Прочитать все
                        </Button>
                    )}
                </div>
                <ScrollArea className="h-[300px]">
                    {items.length === 0 ? (
                        <div className="flex h-full items-center justify-center p-4 text-center text-sm text-muted-foreground">
                            Нет новых уведомлений
                        </div>
                    ) : (
                        <div className="flex flex-col gap-1 p-2">
                            {items.map((notif) => (
                                <div
                                    key={notif.id}
                                    className={cn(
                                        "flex flex-col gap-1 rounded-md p-3 text-sm transition-colors hover:bg-muted/50",
                                        !notif.isRead && "bg-muted/30 cursor-pointer"
                                    )}
                                    role={!notif.isRead ? "button" : undefined}
                                    onClick={() => {
                                        if (!notif.isRead) markAsRead(notif.id)
                                    }}
                                >
                                    <div className="flex items-start justify-between gap-2">
                                        <span className="font-medium">{notif.title}</span>
                                        {!notif.isRead && (
                                            <Badge variant="default" className="h-2 w-2 rounded-full p-0" />
                                        )}
                                    </div>
                                    <span className="text-xs text-muted-foreground line-clamp-2">
                                        {notif.message}
                                    </span>
                                    <span className="text-[10px] text-muted-foreground">
                                        {formatDistanceToNow(new Date(notif.createdAt), {
                                            addSuffix: true,
                                            locale: ru,
                                        })}
                                    </span>
                                </div>
                            ))}
                        </div>
                    )}
                </ScrollArea>
                <div className="border-t p-2">
                    <Button variant="ghost" size="sm" className="w-full text-xs">
                        Все уведомления
                    </Button>
                </div>
            </PopoverContent>
        </Popover>
    )
}
