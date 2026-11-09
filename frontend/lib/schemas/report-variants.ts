import { z } from "zod"
import { jsonObjectSchema, jsonValueSchema } from "@/lib/json-value"
import { comparisonOperatorSchema, filterValuesSchema } from "@/lib/schemas/filters"
import type { VariantResponse } from "@/types/generated/http-contracts"

const variantFiltersSchema = jsonObjectSchema.superRefine((value, context) => {
    if (value.__advancedState !== undefined && !filterValuesSchema.safeParse(value.__advancedState).success) {
        context.addIssue({ code: "custom", path: ["__advancedState"], message: "Некорректные сохранённые фильтры" })
    }
})

export const advancedFilterItemSchema = z.object({
    field: z.string().min(1), fieldType: z.string().optional(),
    operator: comparisonOperatorSchema.exclude(["between"]),
    value: jsonValueSchema.default(null), scale: z.number().int().optional(),
})
export const variantConfigSchema = z.object({
    selectedFields: z.array(z.string()), visibleColumns: z.array(z.string()), groupBy: z.array(z.string()),
    sortColumn: z.string().nullable(), sortDirection: z.enum(["asc", "desc"]),
    filters: variantFiltersSchema, advancedFilters: z.array(advancedFilterItemSchema),
})
export const reportVariantSchema = z.object({
    id: z.uuid(), datasetKey: z.string().min(1), name: z.string().min(1), authorId: z.uuid().nullable(),
    visibility: z.enum(["personal", "shared", "system"]), isDefault: z.boolean(),
    config: variantConfigSchema, version: z.number().int(),
    createdAt: z.iso.datetime({ offset: true }), updatedAt: z.iso.datetime({ offset: true }),
}) satisfies z.ZodType<VariantResponse>

/** Validate the whole saved variant before applying any part to the current tab. */
export function restoreVariantState<T>(value: T) {
    const variant = reportVariantSchema.parse(value)
    const { __advancedState, ...filters } = variant.config.filters
    const advancedState = __advancedState === undefined ? {} : filterValuesSchema.parse(__advancedState)
    return { config: variant.config, filters, advancedState }
}
