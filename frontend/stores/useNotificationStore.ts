// frontend/stores/useNotificationStore.ts
import { create } from "zustand"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { NotificationResponse } from "@/types/notification"
import type { WebSocketMessage } from "@/stores/useWebSocketStore"

// ── Types ───────────────────────────────────────────────────────────────

interface NotificationState {
    /** Notification items (latest first, limited to last fetch). */
    items: NotificationResponse[]
    /** Total count of unread notifications — drives badge everywhere. */
    unreadCount: number
    /** Whether the initial fetch is in progress. */
    isLoading: boolean
    /** Whether the Sheet panel is open. */
    isPanelOpen: boolean
}

interface NotificationActions {
    /** Fetch notifications from API (latest 30, unread priority). */
    fetchNotifications: () => Promise<void>
    /** Optimistic mark-as-read with rollback on failure. */
    markAsRead: (id: string) => Promise<void>
    /** Optimistic mark-as-unread with rollback on failure. */
    markAsUnread: (id: string) => Promise<void>
    /** Optimistic mark-all-as-read with rollback on failure. */
    markAllAsRead: () => Promise<void>
    /** Optimistic delete with rollback on failure. */
    deleteNotification: (id: string) => Promise<void>
    /** Handle incoming WebSocket message (type === "notification"). */
    handleWsMessage: (msg: WebSocketMessage) => void
    /** Toggle or set the Sheet panel open/close state. */
    setPanelOpen: (open: boolean) => void
    /** Start polling timer (called once from app shell). */
    startPolling: () => void
    /** Stop polling timer (called on logout). */
    stopPolling: () => void
}

// ── Internal state (not exposed to React) ───────────────────────────────

let pollingTimer: ReturnType<typeof setInterval> | undefined

// ── Store ───────────────────────────────────────────────────────────────

export const useNotificationStore = create<NotificationState & NotificationActions>()(
    (set, get) => ({
        items: [],
        unreadCount: 0,
        isLoading: false,
        isPanelOpen: false,

        fetchNotifications: async () => {
            set({ isLoading: true })
            try {
                const res = await api.system.notifications.list({ limit: 30 })
                set({
                    items: res.items ?? [],
                    unreadCount: res.unreadCount ?? 0,
                    isLoading: false,
                })
            } catch (err) {
                console.error("[Notifications] fetch error:", err)
                set({ isLoading: false })
            }
        },

        markAsRead: async (id: string) => {
            const { items, unreadCount } = get()
            const prevItems = items
            const prevCount = unreadCount

            // Optimistic update
            set({
                items: items.map((n) => (n.id === id ? { ...n, isRead: true } : n)),
                unreadCount: Math.max(0, unreadCount - 1),
            })

            try {
                await api.system.notifications.markAsRead(id)
            } catch (err) {
                console.error("[Notifications] markAsRead error:", err)
                // Rollback
                set({ items: prevItems, unreadCount: prevCount })
            }
        },

        markAsUnread: async (id: string) => {
            const { items, unreadCount } = get()
            const prevItems = items
            const prevCount = unreadCount

            set({
                items: items.map((n) => (n.id === id ? { ...n, isRead: false } : n)),
                unreadCount: unreadCount + 1,
            })

            try {
                await api.system.notifications.markAsUnread(id)
            } catch (err) {
                console.error("[Notifications] markAsUnread error:", err)
                set({ items: prevItems, unreadCount: prevCount })
            }
        },

        markAllAsRead: async () => {
            const { items, unreadCount } = get()
            const prevItems = items
            const prevCount = unreadCount

            // Optimistic update
            set({
                items: items.map((n) => ({ ...n, isRead: true })),
                unreadCount: 0,
            })

            try {
                await api.system.notifications.markAllAsRead()
            } catch (err) {
                console.error("[Notifications] markAllAsRead error:", err)
                // Rollback
                set({ items: prevItems, unreadCount: prevCount })
            }
        },

        deleteNotification: async (id: string) => {
            const { items, unreadCount } = get()
            const prevItems = items
            const prevCount = unreadCount
            const target = items.find((n) => n.id === id)

            set({
                items: items.filter((n) => n.id !== id),
                unreadCount: target && !target.isRead ? Math.max(0, unreadCount - 1) : unreadCount,
            })

            try {
                await api.system.notifications.delete(id)
            } catch (err) {
                console.error("[Notifications] delete error:", err)
                set({ items: prevItems, unreadCount: prevCount })
            }
        },

        handleWsMessage: (msg: WebSocketMessage) => {
            if (msg.type !== "notification") return

            const notif = msg.payload as NotificationResponse | undefined
            if (!notif?.id) return

            const { items, unreadCount } = get()

            // Prepend and cap at 30
            set({
                items: [notif, ...items].slice(0, 30),
                unreadCount: unreadCount + 1,
            })

            // Show severity-aware toast
            if (notif.title) {
                const severity = notif.severity || "info"
                const toastFn = severity === "error" ? toast.error
                    : severity === "warning" ? toast.warning
                    : severity === "success" ? toast.success
                    : toast.info
                toastFn(notif.title, {
                    description: notif.message,
                })
            }
        },

        setPanelOpen: (open: boolean) => {
            set({ isPanelOpen: open })
            // Refresh data when opening the panel
            if (open) {
                get().fetchNotifications()
            }
        },

        startPolling: () => {
            // Initial fetch
            get().fetchNotifications()
            // Poll every 60s as a safety net for WS disconnects
            if (pollingTimer) clearInterval(pollingTimer)
            pollingTimer = setInterval(() => {
                get().fetchNotifications()
            }, 60_000)
        },

        stopPolling: () => {
            if (pollingTimer) {
                clearInterval(pollingTimer)
                pollingTimer = undefined
            }
            set({ items: [], unreadCount: 0, isLoading: false, isPanelOpen: false })
        },
    })
)
