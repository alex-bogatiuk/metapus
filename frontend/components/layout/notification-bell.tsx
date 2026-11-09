// frontend/components/layout/notification-bell.tsx
"use client"

import * as React from "react"
import { Bell } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useWebsocket } from "@/hooks/useWebsocket"
import { useNotificationStore } from "@/stores/useNotificationStore"
import { cn } from "@/lib/utils"

/**
 * NotificationBell — thin trigger button + badge.
 *
 * All notification logic (fetch, WS handling, optimistic UI) is delegated
 * to useNotificationStore. This component only:
 * 1. Connects to WS and forwards messages to store
 * 2. Starts/stops the polling timer on mount/unmount
 * 3. Shows unreadCount badge
 * 4. Toggles the NotificationPanel on click
 */
export function NotificationBell() {
    const { lastMessage } = useWebsocket()
    const unreadCount = useNotificationStore((s) => s.unreadCount)
    const isPanelOpen = useNotificationStore((s) => s.isPanelOpen)
    const setPanelOpen = useNotificationStore((s) => s.setPanelOpen)
    const handleWsMessage = useNotificationStore((s) => s.handleWsMessage)
    const startPolling = useNotificationStore((s) => s.startPolling)
    const stopPolling = useNotificationStore((s) => s.stopPolling)

    // Track whether we just received a new notification (for bounce animation)
    const [justReceived, setJustReceived] = React.useState(false)

    // Start polling on mount, stop on unmount
    React.useEffect(() => {
        startPolling()
        return () => stopPolling()
    }, [startPolling, stopPolling])

    // Forward WS notifications to store
    React.useEffect(() => {
        if (lastMessage?.type === "notification") {
            handleWsMessage(lastMessage)

            // Trigger bounce animation for 1.5s
            setJustReceived(true)
            const timeout = setTimeout(() => setJustReceived(false), 1500)
            return () => clearTimeout(timeout)
        }
    }, [lastMessage, handleWsMessage])

    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Button
                    id="notification-bell"
                    variant="ghost"
                    size="icon"
                    className={cn(
                        "relative h-8 w-8",
                        isPanelOpen && "bg-accent text-accent-foreground"
                    )}
                    onClick={() => setPanelOpen(!isPanelOpen)}
                    aria-label={`Уведомления${unreadCount > 0 ? ` (${unreadCount} непрочитанных)` : ""}`}
                >
                    <Bell className="h-4 w-4" />
                    {unreadCount > 0 && (
                        <span
                            className={cn(
                                "absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[9px] font-bold text-destructive-foreground ring-2 ring-background",
                                justReceived && "animate-bounce"
                            )}
                        >
                            {unreadCount > 99 ? "99+" : unreadCount}
                        </span>
                    )}
                </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">Уведомления</TooltipContent>
        </Tooltip>
    )
}
