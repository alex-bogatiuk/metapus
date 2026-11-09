import { z } from "zod"
import * as Wire from "@/lib/generated/http-contract-schemas"
import { filterValuesSchema } from "@/lib/schemas/filters"
import { constrainContract } from "@/lib/schemas/contract-constraints"

export const viewVisibilitySchema = z.enum(["personal", "shared", "system"])

// The UI owns filters; Go intentionally stores their JSON without interpreting it.
// Response omission follows the Go facade, then defaults produce a complete UI state.
const responseConfigSchema = Wire.ListviewConfigSchema.and(z.object({
  filters: filterValuesSchema,
  columns: z.array(z.string()).optional(),
  sortColumn: z.string().optional(),
  sortDir: z.enum(["asc", "desc"]).optional(),
}))

export const listViewConfigSchema = z.object({
  filters: filterValuesSchema,
  columns: z.array(z.string()),
  sortColumn: z.string().nullable(),
  sortDir: z.enum(["asc", "desc"]),
}).strict()

export const listViewSchema = Wire.ListViewResponseSchema.and(z.object({
  visibility: viewVisibilitySchema,
  config: responseConfigSchema,
})).transform(view => ({ ...view, config: {
  filters: view.config.filters,
  columns: view.config.columns ?? [],
  sortColumn: view.config.sortColumn ?? null,
  sortDir: view.config.sortDir ?? "asc",
} }))

export const createListViewSchema = constrainContract(Wire.CreateListViewRequestSchema, z.object({
  entityType: z.string().min(1), name: z.string().min(1), visibility: viewVisibilitySchema,
  isDefault: z.boolean(), config: listViewConfigSchema,
}).strict())
export const updateListViewSchema = constrainContract(Wire.UpdateListViewRequestSchema, z.object({
  name: z.string().min(1), visibility: viewVisibilitySchema,
  isDefault: z.boolean(), config: listViewConfigSchema, version: z.number().int().positive(),
}).strict())
