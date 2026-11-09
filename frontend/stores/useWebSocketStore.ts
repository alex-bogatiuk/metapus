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
            } catch (err) {
                if (!isCurrentSession(generation)) return
                console.error("[WebSocket] Failed to obtain ticket:", err)
                // Schedule reconnect
                const timeout = Math.min(1000 * Math.pow(2, reconnectAttempts), 30000)
                reconnectAttempts += 1
                clearTimeout(reconnectTimer)
                reconnectTimer = setTimeout(() => get().connect(), timeout)
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
                console.log("[WebSocket] Connected")
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
                console.log(`[WebSocket] Closed: ${event.code} ${event.reason}`)

                // Auto-reconnect unless intentional close
                if (event.code !== 1000) {
                    if (useAuthStore.getState().isAuthenticated) {
                        const timeout = Math.min(1000 * Math.pow(2, reconnectAttempts), 30000)
                        reconnectAttempts += 1
                        clearTimeout(reconnectTimer)
                        reconnectTimer = setTimeout(() => get().connect(), timeout)
                    }
                }
            }

            socket.onerror = () => {
                // onerror is always followed by onclose (per spec).
                // Browser hides error details for security — the Event is always {}.
                // Reconnect logic is in onclose, so this is purely informational.
                console.warn("[WebSocket] Connection error (reconnect handled by onclose)")
            }
        },

        disconnect: () => {
            clearTimeout(reconnectTimer)
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
