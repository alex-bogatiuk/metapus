import { z } from "zod"
import { jsonObjectSchema, jsonValueSchema, type JsonObject, type JsonValue } from "@/lib/json-value"
import { minorUnitsSchema } from "@/lib/money"
import { quantityInputSchema } from "@/lib/quantity"
import type { EntityMetadata, MetadataField } from "./metadata"

const integerValue = z.union([z.number().int().safe(), z.string().regex(/^(?:0|-[1-9]\d*|[1-9]\d*)$/)])
const decimalValue = z.union([z.number().finite(), z.string().regex(/^-?\d+(?:\.\d+)?$/)])
const recordIdentity = z.object({ id: z.string().min(1), version: z.number().int().safe() })
const typedRef = z.object({ refType: z.string(), refId: z.string() })

// Metadata describes display types, not concrete DTO codecs: integer also covers
// string CryptoAmount, and number covers custom decimals. This validator checks
// dynamic entity values; generated DTO schemas remain the stricter wire contract.
function fieldValueSchema(field: MetadataField): z.ZodType<JsonValue> {
  switch (field.type) {
    case "money": return minorUnitsSchema
    case "integer": return integerValue
    case "number": return field.valueScale === 10000 ? quantityInputSchema : decimalValue
    case "decimal": return decimalValue
    case "boolean": return z.boolean()
    case "typed_ref": return typedRef
    case "json": return jsonValueSchema
    default: return z.string()
  }
}

function validateFields(fields: MetadataField[], data: JsonObject, direction: "response" | "request") {
  for (const field of fields) {
    if (direction === "request" && field.readOnly) continue
    const value = data[field.name]
    // Nullability/omission belong to DTOs. FLS may remove a visible field value.
    if (value === undefined || value === null) continue
    if (!fieldValueSchema(field).safeParse(value).success) {
      throw new Error(`${field.label ?? field.name}: значение не соответствует типу поля`)
    }
  }
}

export function parseEntityValues(meta: EntityMetadata, value: JsonValue, direction: "response" | "request" = "response"): JsonObject {
  const data = jsonObjectSchema.parse(value)
  if (direction === "response") recordIdentity.parse(data)
  validateFields(meta.fields, data, direction)
  if (data.attributes !== undefined && data.attributes !== null) {
    const attributes = jsonObjectSchema.parse(data.attributes)
    validateFields((meta.customFields ?? []).map(field => ({ ...field, name: field.name.replace(/^attributes\./, "") })), attributes, direction)
  }
  for (const part of meta.tableParts ?? []) {
    if (data[part.name] === undefined || data[part.name] === null) continue
    const rows = z.array(jsonObjectSchema).parse(data[part.name])
    for (const row of rows) validateFields(part.columns, row, direction)
  }
  return data
}
