import type { JsonObject } from "@/lib/json-value"
interface CatalogFormField {
  name: string
  readOnly?: boolean
}

const SERVER_MANAGED_FIELDS = new Set([
  "id", "version", "deletionMark", "createdAt", "updatedAt",
  "createdBy", "updatedBy", "txid", "deletedAt",
])

/** Project form state onto catalog requisites; resolved references belong only to responses. */
export function buildCatalogFormPayload(
  fields: readonly CatalogFormField[],
  data: JsonObject,
  isNew: boolean,
  excludedFields: readonly string[] = [],
): JsonObject {
  const excluded = new Set(excludedFields)
  const names = fields
    .filter((field) => !field.readOnly && !SERVER_MANAGED_FIELDS.has(field.name) && !excluded.has(field.name))
    .map((field) => field.name)

  // Hidden in the form, but part of every catalog's write contract.
  names.push("attributes")
  if (!isNew) names.push("version")

  return Object.fromEntries(names
    .filter((name) => Object.prototype.hasOwnProperty.call(data, name))
    .map((name) => [name, data[name]]))
}
