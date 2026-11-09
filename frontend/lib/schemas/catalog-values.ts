import { z } from "zod"

// Form choices are narrower than string-valued HTTP responses: FLS may mask a
// response field to its zero value. Validate choices when building mutations.
export const counterpartyTypeSchema = z.enum(["customer", "supplier", "both", "other"])
export const legalFormSchema = z.enum(["individual", "sole_trader", "company", "government"])
