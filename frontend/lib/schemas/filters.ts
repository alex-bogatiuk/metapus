import { z } from "zod"

export const comparisonOperatorSchema = z.enum(["eq", "neq", "gt", "gte", "lt", "lte", "in", "nin", "contains", "ncontains", "between", "null", "not_null", "in_hierarchy", "nin_hierarchy"])
const scalarSchema = z.union([z.string(), z.number().finite(), z.boolean(), z.null()])
export const filterReferenceSchema = z.object({ id: z.string(), name: z.string(), code: z.string().optional() })
export const filterRangeSchema = z.object({ from: z.union([z.string(), z.number().finite()]).optional(), to: z.union([z.string(), z.number().finite()]).optional() }).strict()
export const filterValueSchema = z.union([
  scalarSchema, filterReferenceSchema, filterRangeSchema, z.array(z.union([scalarSchema, filterReferenceSchema])),
]).optional()
export const filterEntrySchema = z.object({ operator: comparisonOperatorSchema, value: filterValueSchema })
export const filterValuesSchema = z.record(z.string(), filterEntrySchema)
export type FilterValue = z.infer<typeof filterValueSchema>
export type FilterEntry = z.infer<typeof filterEntrySchema>
export type FilterValues = z.infer<typeof filterValuesSchema>
