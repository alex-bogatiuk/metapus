import { useEffect } from "react"
import { useWebSocketStore } from "@/stores/useWebSocketStore"
import type { WebSocketMessage } from "@/stores/useWebSocketStore"

export type { WebSocketMessage }

/**
 * Hook that connects to the singleton WebSocket on mount and returns
 * the shared connection state. Multiple components calling this hook
 * share the same underlying WS connection.
 */
export function useWebsocket() {
    const isConnected = useWebSocketStore((s) => s.isConnected)
    const lastMessage = useWebSocketStore((s) => s.lastMessage)
    const connect = useWebSocketStore((s) => s.connect)

    useEffect(() => {
        connect()
        // No disconnect on unmount — connection is global (singleton).
        // Disconnect happens only on logout (handled by store subscription).
    }, [connect])

    return { isConnected, lastMessage }
}
