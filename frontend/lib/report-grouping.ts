/**
 * report-grouping.ts — Pure functions for frontend grouping & totals.
 *
 * The backend returns a flat items array. This module transforms it
 * into DisplayRow[] with group headers, subtotals, and a grand total footer.
 *
 * Key design decision: grouping is done entirely on the frontend.
 * This allows users to change grouping without re-fetching data.
 * Safe for ≤5000 rows (see architecture decision #3).
 */

import type { DisplayRow, ReportTotalDef } from "@/types/report-meta"

// ── Build Display Rows ──────────────────────────────────────────────────

/**
 * Transform flat items into grouped DisplayRow[] with subtotals and footer.
 *
 * @param items       - Flat array of report data rows
 * @param groupByKeys - Active grouping keys (e.g. ["warehouseName"])
 * @param totalDefs   - Which columns to aggregate and how
 * @returns DisplayRow[] ready for ReportTable
 */
export function buildDisplayRows(
    items: Record<string, unknown>[],
    groupByKeys: string[],
    totalDefs: ReportTotalDef[],
): DisplayRow[] {
    if (items.length === 0) return []

    // No grouping — flat data rows + footer
    if (groupByKeys.length === 0) {
        const rows: DisplayRow[] = items.map((item) => ({
            kind: "data" as const,
            depth: 0,
            item,
        }))
        rows.push({
            kind: "footer",
            totals: computeTotals(items, totalDefs),
        })
        return rows
    }

    // Recursive grouping
    const rows = buildGroupLevel(items, groupByKeys, 0, totalDefs)

    // Grand total footer
    rows.push({
        kind: "footer",
        totals: computeTotals(items, totalDefs),
    })

    return rows
}

// ── Internal Helpers ────────────────────────────────────────────────────

function buildGroupLevel(
    items: Record<string, unknown>[],
    groupByKeys: string[],
    depth: number,
    totalDefs: ReportTotalDef[],
): DisplayRow[] {
    if (depth >= groupByKeys.length) {
        // Leaf level — emit data rows
        return items.map((item) => ({
            kind: "data" as const,
            depth,
            item,
        }))
    }

    const key = groupByKeys[depth]
    const groups = groupBy(items, key)
    const rows: DisplayRow[] = []

    for (const [label, groupItems] of groups) {
        const subtotals = computeTotals(groupItems, totalDefs)

        // Group header
        rows.push({
            kind: "group",
            depth,
            label: String(label),
            count: groupItems.length,
            subtotals,
        })

        // Recurse into next grouping level
        rows.push(...buildGroupLevel(groupItems, groupByKeys, depth + 1, totalDefs))

        // Subtotal row (only if there's a next group at the same level to separate)
        if (totalDefs.length > 0) {
            rows.push({
                kind: "subtotal",
                depth,
                totals: subtotals,
            })
        }
    }

    return rows
}

/**
 * Group items by a key, preserving insertion order.
 */
function groupBy(
    items: Record<string, unknown>[],
    key: string,
): Map<string, Record<string, unknown>[]> {
    const map = new Map<string, Record<string, unknown>[]>()
    for (const item of items) {
        const val = String(item[key] ?? "—")
        let group = map.get(val)
        if (!group) {
            group = []
            map.set(val, group)
        }
        group.push(item)
    }
    return map
}

/**
 * Compute aggregate totals for a set of items.
 */
function computeTotals(
    items: Record<string, unknown>[],
    totalDefs: ReportTotalDef[],
): Record<string, number> {
    const result: Record<string, number> = {}

    for (const def of totalDefs) {
        const values = items
            .map((item) => Number(item[def.column] ?? 0))
            .filter((v) => !isNaN(v))

        switch (def.func) {
            case "sum":
                result[def.column] = values.reduce((a, b) => a + b, 0)
                break
            case "count":
                result[def.column] = values.length
                break
            case "avg":
                result[def.column] = values.length > 0
                    ? values.reduce((a, b) => a + b, 0) / values.length
                    : 0
                break
            case "min":
                result[def.column] = values.length > 0 ? Math.min(...values) : 0
                break
            case "max":
                result[def.column] = values.length > 0 ? Math.max(...values) : 0
                break
        }
    }

    return result
}

// ── Utilities ───────────────────────────────────────────────────────────

/** Extract default active groupBy keys from metadata. */
export function getDefaultGroupByKeys(
    groupByDefs: { key: string; defaultActive?: boolean }[],
): string[] {
    return groupByDefs.filter((g) => g.defaultActive).map((g) => g.key)
}

/** Sort items by column key and direction (client-side). */
export function sortItems(
    items: Record<string, unknown>[],
    column: string,
    direction: "asc" | "desc",
): Record<string, unknown>[] {
    return [...items].sort((a, b) => {
        const va = a[column]
        const vb = b[column]
        let cmp = 0
        if (typeof va === "number" && typeof vb === "number") {
            cmp = va - vb
        } else {
            cmp = String(va ?? "").localeCompare(String(vb ?? ""))
        }
        return direction === "desc" ? -cmp : cmp
    })
}
