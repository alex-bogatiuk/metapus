import { z } from "zod"
import { jsonValueSchema } from "@/lib/json-value"
import type { FieldTreeNode, ReportMeta } from "@/types/report-meta"
import type { ReportMetadataResponse } from "@/types/generated/http-contracts"

const moneySources = z.object({ amountType: z.literal("crypto").optional(), currencyId: z.string().min(1), currencyCode: z.string().min(1), decimalPlaces: z.string().min(1) })
const enumValue = z.object({ value: z.string(), label: z.string() })
const fieldTreeNode: z.ZodType<FieldTreeNode> = z.lazy(() => z.object({
    key: z.string(), name: z.string(), label: z.string(), type: z.string(), kind: z.string(),
    children: z.array(fieldTreeNode).optional(), sortable: z.boolean().optional(), refRoute: z.string().optional(), enumValues: z.array(enumValue).optional(),
}))

// Empty metadata collections are omitted by Go JSON v2. Normalize them before
// the UI reads defaults, grouping or totals; closed controls still use enums.
export const reportMetadataSchema = z.object({
    key: z.string(), name: z.string(), description: z.string().optional(),
    filters: z.array(z.object({ key: z.string(), type: z.enum(["date", "period", "reference", "boolean", "enum", "string"]), label: z.string(), required: z.boolean().optional(), ref: z.string().optional(), multi: z.boolean().optional(), default: jsonValueSchema.optional() })),
    columns: z.array(z.object({
        key: z.string(), label: z.string(), type: z.enum(["string", "quantity", "money", "date", "reference", "boolean"]), money: moneySources.optional(),
        align: z.enum(["left", "center", "right"]).optional(), sortable: z.boolean().optional(), defaultHidden: z.boolean().optional(),
        format: z.enum(["number", "currency", "percent"]).optional(), refIdKey: z.string().optional(), refRoute: z.string().optional(),
    })),
    groupBy: z.array(z.object({ key: z.string(), label: z.string(), defaultActive: z.boolean().optional() })).default([]),
    totals: z.array(z.object({ column: z.string(), func: z.enum(["sum", "count", "avg", "min", "max"]), label: z.string().optional(), money: moneySources.optional() })).default([]),
    exportFormats: z.array(z.string()).default([]), scopeDimensions: z.array(z.string()).default([]),
    defaultSort: z.object({ column: z.string(), direction: z.enum(["asc", "desc"]) }).optional(), availableFields: z.array(fieldTreeNode).optional(),
}) satisfies z.ZodType<ReportMeta> & z.ZodType<ReportMetadataResponse>
