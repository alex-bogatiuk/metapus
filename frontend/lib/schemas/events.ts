import { z } from "zod"
import { jsonObjectSchema } from "@/lib/json-value"
import type { NotificationResponse } from "@/types/notification"

export const notificationSchema: z.ZodType<NotificationResponse> = z.object({
  id: z.uuid(), userId: z.uuid(), title: z.string(), message: z.string(),
  severity: z.enum(["info", "warning", "error", "success"]),
  link: z.string().nullish(), isRead: z.boolean(), attributes: jsonObjectSchema.optional(),
  version: z.int(), deletionMark: z.boolean(), createdAt: z.string(), updatedAt: z.string(),
})

const count = z.int().nonnegative()
const counts = { processed: count.optional().default(0), success: count.optional().default(0), failed: count.optional().default(0), total: count }
export const batchProgressSchema = z.discriminatedUnion("type", [
  z.object({ type: z.literal("started"), ...counts }),
  z.object({ type: z.literal("progress"), ...counts }),
  z.object({ type: z.literal("completed"), ...counts }),
  z.object({ type: z.literal("cancelled"), ...counts }),
])
