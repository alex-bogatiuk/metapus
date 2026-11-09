import type { JsonObject } from "@/lib/json-value"
import type { AdvancedFilterItem } from "@/types/common"

export type VariantVisibility = 'personal' | 'shared' | 'system'

export interface VariantConfig {
  selectedFields: string[]
  visibleColumns: string[]
  groupBy: string[]
  sortColumn: string | null
  sortDirection: 'asc' | 'desc'
  filters: JsonObject
  advancedFilters: AdvancedFilterItem[]
}

export interface ReportVariant {
  id: string
  datasetKey: string
  name: string
  authorId: string | null
  visibility: VariantVisibility
  isDefault: boolean
  config: VariantConfig
  version: number
  createdAt: string
  updatedAt: string
}

export interface CreateVariantRequest {
  datasetKey: string
  name: string
  visibility: VariantVisibility
  isDefault: boolean
  config: VariantConfig
}

export interface UpdateVariantRequest {
  name: string
  visibility: VariantVisibility
  isDefault: boolean
  config: VariantConfig
  version: number
}
