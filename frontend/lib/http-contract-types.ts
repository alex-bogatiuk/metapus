import type { z } from "zod"

export interface HTTPRouteContract {
  scope: "api" | "portal"
  method: string
  path: string
  request: z.ZodType | null
  requestFields: readonly string[] | null
  response: z.ZodType | null
  optionalRequest: boolean
}
