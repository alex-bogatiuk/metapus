import type { listViewConfigSchema, listViewSchema, viewVisibilitySchema } from "@/lib/schemas/list-views"
import type * as Wire from "@/types/generated/http-contracts"
import type { z } from "zod"

export type ViewVisibility = z.infer<typeof viewVisibilitySchema>
export type ListViewConfig = z.infer<typeof listViewConfigSchema>
export type ListView = z.infer<typeof listViewSchema>

// Requests carry a complete UI configuration; response omission is normalized separately.
export type CreateListViewRequest = Omit<Wire.CreateListViewRequest, "config" | "visibility" | "isDefault"> & {
  config: ListViewConfig
  visibility: ViewVisibility
  isDefault: boolean
}
export type UpdateListViewRequest = Omit<Wire.UpdateListViewRequest, "config" | "visibility" | "isDefault"> & {
  config: ListViewConfig
  visibility: ViewVisibility
  isDefault: boolean
}
