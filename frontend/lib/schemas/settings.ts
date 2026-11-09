import { z } from "zod"
import type { SystemSettings } from "@/types/settings"

export const settingsSectionSchemas = {
  exchangeRates: z.object({ sourceId: z.uuid().nullable() }).strict(),
  numbering: z.object({ autoNumbering: z.boolean(), numberPrefix: z.string() }).strict(),
  performance: z.object({ batchConcurrency: z.int().min(1).max(10) }).strict(),
}
export const systemSettingsSchema: z.ZodType<SystemSettings> = z.object({ ...settingsSectionSchemas, version: z.int(), updatedAt: z.string() })
export type SettingsSection = keyof typeof settingsSectionSchemas
export type SettingValue = string | number | boolean | null
