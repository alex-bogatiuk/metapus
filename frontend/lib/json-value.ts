import { z } from "zod"

export type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue }
export type JsonObject = { [key: string]: JsonValue }
export const jsonValueSchema: z.ZodType<JsonValue> = z.lazy(() => z.union([
  z.string(), z.number().finite(), z.boolean(), z.null(), z.array(jsonValueSchema), z.record(z.string(), jsonValueSchema),
]))
export const jsonObjectSchema = z.record(z.string(), jsonValueSchema)

/** Project optional in-memory fields through the same JSON serialization as HTTP. */
export function jsonObjectSnapshot<T>(value: T): JsonObject {
  return jsonObjectSchema.parse(JSON.parse(JSON.stringify(value)))
}
