/**
 * Entity metadata types — mirrors backend EntitySummary DTO.
 *
 * Used by useMetadataStore to provide metadata-driven labels
 * throughout the UI (sidebar, toolbar, tabs, access matrix, etc.).
 */

import type { entityActionSchema, entitySummarySchema, entityTypeSchema } from "@/lib/schemas/metadata"
import type * as Wire from "@/types/generated/http-contracts"
import type { z } from "zod"

export type EntityType = z.infer<typeof entityTypeSchema>
export type EntityPresentation = Wire.MetadataPresentation
export type DocumentCapabilities = Wire.MetadataDocumentCapabilities
export type DocumentProtection = Wire.MetadataDocumentProtection
export type EntityActionDef = z.infer<typeof entityActionSchema>
export type EntityActionScope = EntityActionDef["scope"]
export type EntityActionUI = Wire.MetadataEntityActionUI
export type EntityActionConfirmation = Wire.MetadataEntityActionConfirmation
export type EntityMeta = z.infer<typeof entitySummarySchema>

export interface ActionAvailability {
  allowed: boolean
  reasonCode?: string
  decisionMode?: "independent" | "admin_self"
}

export interface ReasonedDecisionRequest { version: number; reason: string }
export interface ReasonedDecisionResult {
  id: string
  number: string
  status: string
  version: number
  decisionMode?: "independent" | "admin_self"
  decisionPolicyVersion?: number
}

