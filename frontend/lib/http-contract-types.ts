import type { z } from "zod"

export interface HTTPRouteContract {
  scope: string
  method: string
  path: string
  request: z.ZodType | null
  requestFields: readonly string[] | null
  /** Fields one element of each array-of-objects request field accepts (document table parts). */
  requestParts?: Readonly<Record<string, readonly string[]>>
  response: z.ZodType | null
  optionalRequest: boolean
  /**
   * An add-on's part of a route of its base (an extension of a catalog): its
   * request and response hold only the fields the add-on adds, and
   * registerHTTPRoutes merges them into the base's route.
   */
  extends?: boolean
}
