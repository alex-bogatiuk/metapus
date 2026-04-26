import { create } from "zustand"
import { useAuthStore } from "@/stores/useAuthStore"

export interface WebSocketMessage<T = unknown> {
    type: string
    payload: T
    time: number
}

interface WebSocketState {
    isConnected: boolean
    lastMessage: WebSocketMessage | null
}

interface WebSocketActions {
    connect: () => void
    disconnect: () => void
}

// Internal state not exposed to React (no re-renders)
let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | undefined
let reconnectAttempts = 0

function buildWsUrl(token: string): string {
    const API_BASE = process.env.NEXT_PUBLIC_API_URL || "/api/v1"
    const TENANT_ID = process.env.NEXT_PUBLIC_TENANT_ID || ""
    const isWss = window.location.protocol === "https:"
    const protocol = isWss ? "wss:" : "ws:"

    let wsUrl = ""
    if (API_BASE.startsWith("http")) {
        wsUrl = API_BASE.replace(/^http/, isWss ? "wss" : "ws")
    } else {
        wsUrl = `${protocol}//${window.location.host}${API_BASE.startsWith("/") ? API_BASE : `/${API_BASE}`}`
    }

    let url = `${wsUrl}/ws?token=${encodeURIComponent(token)}`
    if (TENANT_ID) {
        url += `&tenant=${encodeURIComponent(TENANT_ID)}`
    }
    return url
}

export const useWebSocketStore = create<WebSocketState & WebSocketActions>()(
    (set, get) => ({
        isConnected: false,
        lastMessage: null,

        connect: () => {
            // Guard: already connected or connecting
            if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
                return
            }

            const token = useAuthStore.getState().tokens?.accessToken
            if (!token) return

            const url = buildWsUrl(token)
            const socket = new WebSocket(url)
            ws = socket

            socket.onopen = () => {
                set({ isConnected: true })
                reconnectAttempts = 0
                console.log("[WebSocket] Connected")
            }

            socket.onmessage = (event) => {
                try {
                    const data: WebSocketMessage = JSON.parse(event.data)
                    set({ lastMessage: data })
                } catch (err) {
                    console.error("[WebSocket] Failed to parse message:", err)
                }
            }

            socket.onclose = (event) => {
                set({ isConnected: false })
                ws = null
                console.log(`[WebSocket] Closed: ${event.code} ${event.reason}`)

                // Auto-reconnect unless intentional close
                if (event.code !== 1000) {
                    const token = useAuthStore.getState().tokens?.accessToken
                    if (token) {
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
            if (ws && ws.readyState === WebSocket.OPEN) {
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
