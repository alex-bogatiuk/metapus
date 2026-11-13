import { z } from "zod"
import { filterValuesSchema } from "@/lib/schemas/filters"
import { jsonValueSchema } from "@/lib/json-value"
import { readDashboardLayout } from "@/lib/dashboard-layout"
import type { InterfacePrefs, UserPreferencesResponse } from "@/types/user-prefs"

export const interfacePrefsSchema: z.ZodType<InterfacePrefs> = z.object({
  theme: z.enum(["light", "dark", "system"]).optional(), language: z.string().optional(),
  dateFormat: z.enum(["dd.MM.yyyy", "yyyy-MM-dd", "MM/dd/yyyy"]).optional(),
  numberFormat: z.enum(["space", "comma", "none"]).optional(), showTooltips: z.boolean().optional(),
  compactMode: z.boolean().optional(), sidebarCollapsed: z.boolean().optional(),
  portalRiskBreakdown: z.boolean().optional(),
  showDeletedEntities: z.record(z.string(), z.boolean()).optional(),
})
export const columnWidthsSchema = z.record(z.string(), z.number().finite().nonnegative())
export const listColumnsSchema = z.record(z.string(), z.union([z.array(z.string()), columnWidthsSchema]))
export const preferencesSchema: z.ZodType<UserPreferencesResponse> = z.object({
  userId: z.uuid(), interface: interfacePrefsSchema,
  listFilters: z.record(z.string(), filterValuesSchema), listColumns: listColumnsSchema,
  dashboardLayout: jsonValueSchema.nullable(),
  favorites: z.array(z.object({ entityType: z.string(), entityId: z.uuid(), title: z.string(), url: z.string(), addedAt: z.string() })),
  updatedAt: z.string(),
}).transform(prefs => ({ ...prefs, dashboardLayout: prefs.dashboardLayout == null ? null : readDashboardLayout(prefs.dashboardLayout) }))
