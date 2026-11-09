/**
 * SSE Fetch Client — POST-based Server-Sent Events
 *
 * Standard EventSource only supports GET. This module uses fetch() +
 * ReadableStream to consume SSE events from POST endpoints.
 *
 * Usage:
 *   await fetchSSE("/path", body, eventSchema, (event) => { ... }, signal)
 */

import { apiFetchResponse } from "@/lib/api"
import { assertCurrentSession, getSessionGeneration } from "@/lib/auth-scope"
import type { z } from "zod"
import type { JsonObject } from "@/lib/json-value"

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
    body: JsonObject,
    schema: z.ZodType<T>,
    onEvent: (event: T) => void,
    signal?: AbortSignal,
): Promise<void> {
    const generation = getSessionGeneration()
    const res = await apiFetchResponse(path, { method: "POST", body: JSON.stringify(body), headers: { Accept: "text/event-stream" }, signal })

    // If server returned JSON instead of SSE (backward compat), handle gracefully
    const contentType = res.headers.get("Content-Type") || ""
    if (contentType.includes("application/json")) {
        const data = schema.parse(await res.json())
        assertCurrentSession(generation)
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
            assertCurrentSession(generation)
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
                            const event = schema.safeParse(JSON.parse(line.slice(6)))
                            if (event.success) onEvent(event.data)
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
                        const event = schema.safeParse(JSON.parse(line.slice(6)))
                        if (event.success) onEvent(event.data)
                    } catch {
                        // Skip malformed events
                    }
                }
            }
        }
    } finally {
        await reader.cancel().catch(() => undefined)
        reader.releaseLock()
    }
}
