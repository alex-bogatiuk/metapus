import { z } from "zod"
import { jsonObjectSchema } from "@/lib/json-value"
import type { DatasetQueryResponse } from "@/types/generated/http-contracts"

// Aggregates may exceed int64 even though each stored amount must fit it.
const moneyTotalSchema = z.object({
    amount: z.string().regex(/^(?:0|-[1-9]\d*|[1-9]\d*)$/),
    currencyId: z.string().min(1), currencyCode: z.string().min(1), decimalPlaces: z.int().min(0).max(18),
})
const totalsSchema = z.record(z.string(), z.union([z.number().finite(), z.array(moneyTotalSchema)]))
export const displayRowSchema = z.discriminatedUnion("kind", [
    z.object({ kind: z.literal("group"), depth: z.int().nonnegative().default(0), label: z.string().default(""), count: z.int().nonnegative().default(0), totals: totalsSchema.default({}) }),
    z.object({ kind: z.literal("data"), depth: z.int().nonnegative().default(0), item: jsonObjectSchema }),
    z.object({ kind: z.literal("subtotal"), depth: z.int().nonnegative().default(0), totals: totalsSchema.default({}) }),
    z.object({ kind: z.literal("footer"), totals: totalsSchema.default({}) }),
])
export const groupedReportResponseSchema = z.object({ rows: z.array(displayRowSchema), totalItems: z.int().nonnegative() })
export const reportResultSchema = z.object({ items: z.array(jsonObjectSchema), totalItems: z.int().nonnegative() }) satisfies z.ZodType<DatasetQueryResponse>
