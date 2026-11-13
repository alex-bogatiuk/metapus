import { z } from "zod"
import * as Wire from "@/lib/generated/http-contract-schemas"
import type * as Contract from "@/types/generated/http-contracts"

export const entityTypeSchema = z.enum(["catalog", "document", "register"])
export const metadataFieldTypeSchema = z.enum(["string", "text", "integer", "number", "decimal", "boolean", "date", "datetime", "reference", "typed_ref", "enum", "money", "json"])
export const entityActionSchema = Wire.MetadataEntityActionDefSchema.and(z.object({
  scope: z.enum(["item", "collection"]),
  input: z.object({ kind: z.literal("reasonedDecision"), summaryFields: z.array(z.string()).optional(), reasonMaxBytes: z.int().positive() }).optional(),
}))
export const metadataFieldSchema = Wire.MetadataFieldDefSchema.and(z.object({ type: metadataFieldTypeSchema }))
export type MetadataField = z.infer<typeof metadataFieldSchema>
type MetadataAction = z.infer<typeof entityActionSchema>
export type EntitySummary = Omit<Contract.EntitySummaryResponse, "type" | "actions"> & {
  type: z.infer<typeof entityTypeSchema>
  actions?: MetadataAction[]
}
export const entitySummarySchema: z.ZodType<EntitySummary> = Wire.EntitySummaryResponseSchema.and(z.object({
  type: entityTypeSchema, actions: z.array(entityActionSchema).optional(),
}))
export type EntityMetadata = Omit<Contract.EntityMetadataResponse, "type" | "fields" | "tableParts" | "actions"> & {
  type: z.infer<typeof entityTypeSchema>
  fields: MetadataField[]
  tableParts?: { name: string; label?: string; columns: MetadataField[] }[]
  actions?: MetadataAction[]
}
const baseEntityMetadataSchema: z.ZodType<EntityMetadata> = Wire.EntityMetadataResponseSchema.and(z.object({
  type: entityTypeSchema,
  fields: z.array(metadataFieldSchema),
  tableParts: z.array(z.object({ name: z.string(), label: z.string().optional(), columns: z.array(metadataFieldSchema) })).optional(),
  actions: z.array(entityActionSchema).optional(),
}))

const _cryptoResourceIntegerFields = new Set(["requestedAmount", "delegatedAmount", "reservedAmount", "consumedAmount"])

export const entityMetadataSchema: z.ZodType<EntityMetadata> = baseEntityMetadataSchema.transform((meta): EntityMetadata => {
  if (meta.name === "CryptoResourceRental" || meta.key === "crypto_resource_rental") {
    return {
      ...meta,
      fields: meta.fields.map(f => _cryptoResourceIntegerFields.has(f.name) && f.type === "money" ? { ...f, type: "integer" } : f),
    }
  }
  return meta
})

export const filterFieldTypeSchema = z.enum(["string", "number", "money", "date", "boolean", "reference", "typed_ref", "enum"])
export type FilterFieldMetadata = Omit<Contract.FilterFieldMetadataResponse, "fieldType" | "refFields"> & {
  fieldType: z.infer<typeof filterFieldTypeSchema>
  refFields?: FilterFieldMetadata[]
}
export const filterFieldMetadataSchema: z.ZodType<FilterFieldMetadata> = z.lazy(() => Wire.FilterFieldMetadataResponseSchema.and(z.object({
  fieldType: filterFieldTypeSchema, refFields: z.array(filterFieldMetadataSchema).optional(),
})))
