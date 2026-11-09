import { z } from "zod"

export function cursorListSchema<T>(item: z.ZodType<T>) {
  return z.object({
    items: z.array(item), nextCursor: z.string().optional(), prevCursor: z.string().optional(),
    hasMore: z.boolean(), hasPrev: z.boolean(), targetIndex: z.int().optional(), totalCount: z.int().nullable(),
  })
}
