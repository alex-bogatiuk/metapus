import { z } from "zod"
import { minorUnitsSchema } from "@/lib/money"
import { ResolvedRefResponseSchema } from "@/lib/generated/http-contract-schemas"

export const resolvedRefSchema = ResolvedRefResponseSchema.and(z.object({
  entityType: z.enum(["catalog", "document"]), amount: minorUnitsSchema.optional(),
  decimalPlaces: z.int().min(0).max(18).optional(),
})).refine(value => value.amount === undefined || (!!value.currencyId && !!value.currencyCode && value.decimalPlaces !== undefined),
  "Сумма ссылки должна содержать доступную валютную единицу")
export const resolvedRefsSchema = z.array(resolvedRefSchema)
