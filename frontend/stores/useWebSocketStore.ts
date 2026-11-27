import { getSessionGeneration, isCurrentSession, onScopeReset } from "@/lib/auth-scope"
import { create } from "zustand"
import { useAuthStore } from "@/stores/useAuthStore"
import { api } from "@/lib/api"
import { z } from "zod"
import { jsonValueSchema } from "@/lib/json-value"

export const webSocketMessageSchema = z.object({ type: z.string(), payload: jsonValueSchema, time: z.number().finite() })
export type WebSocketMessage = z.infer<typeof webSocketMessageSchema>

interface WebSocketState {
    isConnected: boolean
    lastMessage: WebSocketMessage | null
}

interface WebSocketActions {
    connect: () => Promise<void>
    disconnect: () => void
}

// Internal state not exposed to React (no re-renders)
let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | undefined
let reconnectAttempts = 0

/** Upper bound of the reconnect delay, ms. */
const MAX_RECONNECT_DELAY = 30_000

/**
 * Exponential backoff with jitter: after a backend restart every client
 * picks its own moment, so the reconnects arrive spread out, not as one burst.
 */
function scheduleReconnect(connect: () => Promise<void>): void {
    const ceiling = Math.min(1000 * 2 ** reconnectAttempts, MAX_RECONNECT_DELAY)
    reconnectAttempts += 1
    clearTimeout(reconnectTimer)
    reconnectTimer = setTimeout(() => {
        reconnectTimer = undefined
        void connect()
    }, ceiling / 2 + Math.random() * (ceiling / 2))
}

function buildWsUrl(ticket: string): string {
    const API_BASE = process.env.NEXT_PUBLIC_API_URL || "/api/v1"
    const TENANT_ID = useAuthStore.getState().cacheScope?.tenantId ?? process.env.NEXT_PUBLIC_TENANT_ID ?? ""
    const isWss = window.location.protocol === "https:"
    const protocol = isWss ? "wss:" : "ws:"

    let wsUrl = ""
    if (API_BASE.startsWith("http")) {
        wsUrl = API_BASE.replace(/^http/, isWss ? "wss" : "ws")
    } else {
        wsUrl = `${protocol}//${window.location.host}${API_BASE.startsWith("/") ? API_BASE : `/${API_BASE}`}`
    }

    let url = `${wsUrl}/ws?ticket=${encodeURIComponent(ticket)}`
    if (TENANT_ID) {
        url += `&tenant=${encodeURIComponent(TENANT_ID)}`
    }
    return url
}

export const useWebSocketStore = create<WebSocketState & WebSocketActions>()(
    (set, get) => ({
        isConnected: false,
        lastMessage: null,

        connect: async () => {
            const generation = getSessionGeneration()
            // Guard: already connected or connecting
            if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
                return
            }

            let ticket = ""
            try {
                // Obtain short-lived ticket via POST /auth/ws-ticket
                const res = await api.auth.webSocketTicket()
                if (!isCurrentSession(generation)) return
                ticket = res.ticket
            } catch {
                // The backend may be restarting: try again later.
                if (isCurrentSession(generation)) scheduleReconnect(get().connect)
                return
            }

            if (!ticket) return

            const url = buildWsUrl(ticket)
            const socket = new WebSocket(url)
            ws = socket

            socket.onopen = () => {
                if (!isCurrentSession(generation) || ws !== socket) return
                set({ isConnected: true })
                reconnectAttempts = 0
            }

            socket.onmessage = (event) => {
                if (!isCurrentSession(generation) || ws !== socket) return
                try {
                    const data = webSocketMessageSchema.parse(JSON.parse(event.data))
                    set({ lastMessage: data })
                } catch (err) {
                    if (!isCurrentSession(generation)) return
                    console.error("[WebSocket] Failed to parse message:", err)
                }
            }

            socket.onclose = (event) => {
                if (!isCurrentSession(generation) || ws !== socket) return
                set({ isConnected: false })
                ws = null
                // Reconnect unless the close was intentional. An error event
                // carries no details and is always followed by this close.
                if (event.code !== 1000 && useAuthStore.getState().isAuthenticated) scheduleReconnect(get().connect)
            }
        },

        disconnect: () => {
            clearTimeout(reconnectTimer)
            reconnectTimer = undefined
            reconnectAttempts = 0
            if (ws) {
                ws.close(1000, "Manual disconnect")
            }
            ws = null
            set({ isConnected: false, lastMessage: null })
        },
    })
)

// Auto-disconnect on logout
useAuthStore.subscribe((state, prevState) => {
    if (prevState.isAuthenticated && !state.isAuthenticated) {
        useWebSocketStore.getState().disconnect()
    }
})

onScopeReset(() => useWebSocketStore.getState().disconnect())

// Back online or back in view with the socket down: reconnect now instead of
// waiting out a backoff that grew while the network or the device slept.
if (typeof window !== "undefined") {
    const reconnectNow = () => {
        // Only a pending retry is pulled forward; a connect in flight is left alone.
        if (reconnectTimer === undefined || !useAuthStore.getState().isAuthenticated) return
        clearTimeout(reconnectTimer)
        reconnectTimer = undefined
        reconnectAttempts = 0
        void useWebSocketStore.getState().connect()
    }
    window.addEventListener("online", reconnectNow)
    document.addEventListener("visibilitychange", () => {
        if (document.visibilityState === "visible") reconnectNow()
    })
}
