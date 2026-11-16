/**
 * Shared list cell renderers for entity overlays (platform and configuration).
 * Pure render functions: no hooks, no state.
 */
import React from "react"
import type { JsonObject } from "@/lib/json-value"
import { getStatusColorClass } from "@/lib/status-colors"
import { formatRawMinor } from "@/lib/money"
import { COPY_VALUE_ATTR } from "@/components/shared/data-table"

export function cellCode(item: JsonObject) {
    return React.createElement("span", { className: "font-mono text-xs text-muted-foreground" }, String(item.code ?? ""))
}

export function cellName(item: JsonObject) {
    return React.createElement("span", { className: "font-medium text-foreground" }, String(item.name ?? ""))
}

export function cellMuted(key: string) {
    const Cell = (item: JsonObject) =>
        React.createElement("span", { className: "text-xs text-muted-foreground" }, String(item[key] ?? "") || "—")
    Cell.displayName = `CellMuted(${key})`
    return Cell
}

/**
 * Renders a resolved reference field. Backend returns nested RefDisplay objects:
 *   { merchantId: "uuid", merchant: { id: "uuid", name: "Test Merchant" } }
 * This renderer accesses item[refKey].name.
 */
export function cellRef(refKey: string) {
    const Cell = (item: JsonObject) => {
        const ref = item[refKey] as { name?: string } | null | undefined
        return React.createElement("span", { className: "text-xs text-muted-foreground" }, ref?.name || "—")
    }
    Cell.displayName = `CellRef(${refKey})`
    return Cell
}

export function cellMono(key: string) {
    const Cell = (item: JsonObject) =>
        React.createElement("span", { className: "font-mono text-xs text-muted-foreground" }, String(item[key] ?? "") || "—")
    Cell.displayName = `CellMono(${key})`
    return Cell
}

export function cellBoolBadge(key: string, yesLabel = "Да") {
    const Cell = (item: JsonObject) => {
        const value = item[key]
        if (value) {
            return React.createElement("span", {
                className: "inline-flex items-center rounded-md border px-2 py-0.5 text-[10px] font-semibold bg-primary text-primary-foreground",
            }, yesLabel)
        }
        return React.createElement("span", { className: "text-xs text-muted-foreground" }, "—")
    }
    Cell.displayName = `CellBoolBadge(${key})`
    return Cell
}

export function cellDate(key: string) {
    const Cell = (item: JsonObject) => {
        const value = item[key]
        if (!value) return React.createElement("span", { className: "text-xs text-muted-foreground" }, "—")
        try {
            return React.createElement("span", { className: "text-xs text-muted-foreground" },
                new Date(String(value)).toLocaleDateString())
        } catch {
            return React.createElement("span", { className: "text-xs text-muted-foreground" }, String(value))
        }
    }
    Cell.displayName = `CellDate(${key})`
    return Cell
}

export function cellTruncated(key: string, maxW = 200) {
    const Cell = (item: JsonObject) =>
        React.createElement("span", {
            className: `text-xs text-muted-foreground truncate block`,
            style: { maxWidth: maxW },
        }, String(item[key] ?? "") || "—")
    Cell.displayName = `CellTruncated(${key})`
    return Cell
}

// Crypto-specific cell renderers

export function cellHashTruncated(key: string) {
    const Cell = (item: JsonObject) => {
        const hash = String(item[key] ?? "")
        if (!hash || hash === "—") return React.createElement("span", { className: "text-xs text-muted-foreground" }, "—")
        const truncated = hash.length > 16 ? `${hash.slice(0, 8)}…${hash.slice(-6)}` : hash
        return React.createElement("span", {
            className: "font-mono text-xs text-muted-foreground cursor-help",
            title: hash,
            [COPY_VALUE_ATTR]: hash,
        }, truncated)
    }
    Cell.displayName = `CellHash(${key})`
    return Cell
}

export function cellStatusBadge(key: string) {
    const Cell = (item: JsonObject) => {
        const status = String(item[key] ?? "")
        if (!status) return React.createElement("span", { className: "text-xs text-muted-foreground" }, "—")
        const color = getStatusColorClass(status)
        return React.createElement("span", {
            className: `inline-flex items-center rounded-md border px-2 py-0.5 text-[10px] font-semibold ${color}`,
        }, status)
    }
    Cell.displayName = `CellStatus(${key})`
    return Cell
}

export function cellAmount(key: string) {
    const Cell = (item: JsonObject) => {
        const value = String(item[key] ?? "0")
        return React.createElement("span", {
            className: "font-mono text-xs text-right block tabular-nums text-foreground",
        }, value)
    }
    Cell.displayName = `CellAmount(${key})`
    return Cell
}
