import { test, expect } from "@playwright/test"
import { readFileSync } from "node:fs"
import { z } from "zod"
import { api } from "../../lib/api"
import { jsonObjectSchema } from "../../lib/json-value"
import { buildDisplayRows } from "../../lib/report-grouping"
import { groupedReportResponseSchema, displayRowSchema } from "../../lib/schemas/report-results"
import { reportVariantSchema, restoreVariantState } from "../../lib/schemas/report-variants"
import { ids } from "../helpers"

test("browser grouping matches the Go JSON facade, omitted depth and exact currency totals", () => {
    const fixture = z.object({
        items: z.array(jsonObjectSchema), groupBy: z.array(z.string()), rows: z.array(displayRowSchema),
        totals: z.array(z.object({ column: z.string(), func: z.enum(["sum", "count", "avg", "min", "max"]), money: z.object({ currencyId: z.string(), currencyCode: z.string(), decimalPlaces: z.string() }).optional() })),
    }).parse(JSON.parse(readFileSync("../internal/domain/reports/tabular/testdata/grouping.json", "utf8")))
    expect(buildDisplayRows(fixture.items, fixture.groupBy, fixture.totals)).toEqual(fixture.rows)
    expect(groupedReportResponseSchema.parse({ rows: fixture.rows, totalItems: 4 }).rows).toEqual(fixture.rows)
    expect(groupedReportResponseSchema.safeParse({ rows: [{ kind: "data", item: [] }], totalItems: 1 }).success).toBe(false)
    expect(displayRowSchema.safeParse({ kind: "footer", totals: { amount: [{ amount: 9007199254740992 }] } }).success).toBe(false)
})

test("report variants validate the complete configuration before restoring a tab", async () => {
    const wire = {
        id: ids.token, datasetKey: "document-journal", name: "Exact", authorId: ids.user, visibility: "personal", isDefault: false,
        config: { selectedFields: ["total_amount"], visibleColumns: ["total_amount"], groupBy: [], sortColumn: null, sortDirection: "asc", filters: { posted: true, __advancedState: { total_amount: { operator: "eq", value: "9007199254740993" } } }, advancedFilters: [] },
        version: 1, createdAt: "2026-09-13T00:00:00Z", updatedAt: "2026-09-13T00:00:00Z",
    }
    expect(restoreVariantState(wire)).toMatchObject({ filters: { posted: true }, advancedState: wire.config.filters.__advancedState })
    const nativeFetch = globalThis.fetch
    try {
        globalThis.fetch = async () => Response.json([wire])
        expect(await api.reports.variants.list("document-journal")).toEqual([reportVariantSchema.parse(wire)])
        const invalid = { ...wire, config: { ...wire.config, filters: { __advancedState: { total_amount: { operator: "typo", value: "1" } } } } }
        expect(() => restoreVariantState(invalid)).toThrow()
        globalThis.fetch = async () => Response.json([invalid])
        await expect(api.reports.variants.list("document-journal")).rejects.toThrow()
    } finally { globalThis.fetch = nativeFetch }
})
