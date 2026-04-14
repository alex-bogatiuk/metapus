/**
 * SSE Fetch Client — POST-based Server-Sent Events
 *
 * Standard EventSource only supports GET. This module uses fetch() +
 * ReadableStream to consume SSE events from POST endpoints.
 *
 * Usage:
 *   await fetchSSE<MyEvent>("/path", body, (event) => { ... }, signal)
 */

import { useAuthStore } from "@/stores/useAuthStore"

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080/api/v1"
const TENANT_ID = process.env.NEXT_PUBLIC_TENANT_ID || ""

function buildSSEHeaders(): Record<string, string> {
    const headers: Record<string, string> = {
        "Content-Type": "application/json",
        "Accept": "text/event-stream",
    }

    const tokens = useAuthStore.getState().tokens
    if (tokens?.accessToken) {
        headers["Authorization"] = `${tokens.tokenType || "Bearer"} ${tokens.accessToken}`
    }

    if (TENANT_ID) {
        headers["X-Tenant-ID"] = TENANT_ID
    }

    return headers
}

/**
 * Fetches an SSE stream from a POST endpoint.
 *
 * @param path    API path (e.g. "/document/goods-receipt/batch-action-by-filter")
 * @param body    JSON request body
 * @param onEvent Callback invoked for each parsed SSE data event
 * @param signal  Optional AbortSignal for cancellation
 * @throws Error if the response is not ok or the stream fails
 */
export async function fetchSSE<T>(
    path: string,
    body: unknown,
    onEvent: (event: T) => void,
    signal?: AbortSignal,
): Promise<void> {
    const res = await fetch(`${API_BASE}${path}`, {
        method: "POST",
        headers: buildSSEHeaders(),
        body: JSON.stringify(body),
        signal,
    })

    if (!res.ok) {
        // Try to parse error body for user-friendly message
        let errorMessage = `Batch operation failed: ${res.status}`
        try {
            const errorBody = await res.json()
            if (errorBody?.error?.message) {
                errorMessage = errorBody.error.message
            }
        } catch {
            // Ignore parse errors
        }
        throw new Error(errorMessage)
    }

    // If server returned JSON instead of SSE (backward compat), handle gracefully
    const contentType = res.headers.get("Content-Type") || ""
    if (contentType.includes("application/json")) {
        const data = await res.json() as T
        onEvent(data)
        return
    }

    // Parse SSE stream
    const reader = res.body?.getReader()
    if (!reader) {
        throw new Error("Response body is not readable")
    }

    const decoder = new TextDecoder()
    let buffer = ""

    try {
        while (true) {
            const { done, value } = await reader.read()
            if (done) break

            buffer += decoder.decode(value, { stream: true })

            // SSE events are delimited by double newlines
            const parts = buffer.split("\n\n")
            // Keep the last (possibly incomplete) part in the buffer
            buffer = parts.pop() || ""

            for (const part of parts) {
                const trimmed = part.trim()
                if (!trimmed) continue

                // Extract data from "data: {...}" lines
                for (const line of trimmed.split("\n")) {
                    if (line.startsWith("data: ")) {
                        try {
                            const event = JSON.parse(line.slice(6)) as T
                            onEvent(event)
                        } catch {
                            // Skip malformed events
                        }
                    }
                }
            }
        }

        // Process any remaining data in buffer
        if (buffer.trim()) {
            for (const line of buffer.trim().split("\n")) {
                if (line.startsWith("data: ")) {
                    try {
                        const event = JSON.parse(line.slice(6)) as T
                        onEvent(event)
                    } catch {
                        // Skip malformed events
                    }
                }
            }
        }
    } finally {
        reader.releaseLock()
    }
}
