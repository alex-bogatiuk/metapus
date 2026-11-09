import type { JsonObject } from "@/lib/json-value"
import type * as Wire from "@/types/generated/http-contracts"
/**
 * Shared types for Catalog entities.
 */

/** Attributes — arbitrary key-value map (mirrors entity.Attributes). */
export type Attributes = JsonObject

/** Request DTO for setting/clearing deletion mark. Mirrors dto.SetDeletionMarkRequest (json:"marked"). */
export interface SetDeletionMarkRequest {
  marked: boolean
}

// ── Counterparty ────────────────────────────────────────────────────────

/** Counterparty type enum — mirrors domain CounterpartyType. */
export type CounterpartyType = "customer" | "supplier" | "both" | "other"

/** Legal form enum — mirrors domain LegalForm. */
export type LegalForm = "individual" | "sole_trader" | "company" | "government"

/** Response DTO for a counterparty. */
export type CounterpartyResponse = Wire.CounterpartyResponse

/** Request DTO for creating a counterparty. */
export interface CreateCounterpartyRequest {
  code?: string
  name: string
  type: CounterpartyType
  legalForm: LegalForm
  fullName?: string | null
  inn?: string | null
  kpp?: string | null
  ogrn?: string | null
  legalAddress?: string | null
  actualAddress?: string | null
  phone?: string | null
  email?: string | null
  contactPerson?: string | null
  comment?: string | null
  parentId?: string | null
  isFolder?: boolean
  attributes?: Attributes | null
}

/** Request DTO for updating a counterparty. */
export interface UpdateCounterpartyRequest {
  code?: string
  name: string
  type: CounterpartyType
  legalForm: LegalForm
  fullName?: string | null
  inn?: string | null
  kpp?: string | null
  ogrn?: string | null
  legalAddress?: string | null
  actualAddress?: string | null
  phone?: string | null
  email?: string | null
  contactPerson?: string | null
  comment?: string | null
  parentId?: string | null
  isFolder?: boolean
  attributes?: Attributes | null
  version: number
}

// ── Organization ────────────────────────────────────────────────────────

/** Tax system enum — mirrors domain TaxSystem. */
export type TaxSystem =
  | "osno"
  | "usn_income"
  | "usn_income_expense"
  | "envd"
  | "patent"

/** Response DTO for an organization. */
export type OrganizationResponse = Wire.OrganizationResponse

/** Request DTO for creating an organization. */
export interface CreateOrganizationRequest {
  code?: string
  name: string
  fullName?: string
  inn?: string
  kpp?: string
  ogrn?: string
  legalAddress?: string
  actualAddress?: string
  phone?: string
  email?: string
  website?: string
  baseCurrencyId: string
  isDefault?: boolean
  director?: string
  accountant?: string
  logoUrl?: string
  taxSystem?: string
  vatPayer?: boolean
  fiscalYearStart?: string
}

/** Request DTO for updating an organization. */
export interface UpdateOrganizationRequest {
  id: string
  version: number
  code?: string
  name: string
  fullName?: string
  inn?: string
  kpp?: string
  ogrn?: string
  legalAddress?: string
  actualAddress?: string
  phone?: string
  email?: string
  website?: string
  baseCurrencyId: string
  isDefault?: boolean
  deletionMark?: boolean
  director?: string
  accountant?: string
  logoUrl?: string
  taxSystem?: string
  vatPayer?: boolean
  fiscalYearStart?: string
}

// ── Currency ───────────────────────────────────────────────────────────

export type CurrencyResponse = Wire.CurrencyResponse

export interface CreateCurrencyRequest {
  code?: string
  name: string
  isoCode?: string | null
  isoNumericCode?: string | null
  symbol?: string | null
  decimalPlaces: number
  isBase: boolean
  country?: string | null
}

export interface UpdateCurrencyRequest {
  code?: string
  name: string
  isoCode?: string | null
  isoNumericCode?: string | null
  symbol?: string | null
  decimalPlaces: number
  isBase: boolean
  country?: string | null
  version: number
}

// ── Contract ───────────────────────────────────────────────────────────

export type ContractType = "supply" | "sale" | "other"

export type ContractResponse = Wire.ContractResponse

export interface CreateContractRequest {
  code?: string
  name: string
  counterpartyId: string
  type: ContractType
  currencyId?: string | null
  validFrom?: string | null
  validTo?: string | null
  paymentTermDays: number
  description?: string | null
}

export interface UpdateContractRequest {
  code?: string
  name: string
  counterpartyId: string
  type: ContractType
  currencyId?: string | null
  validFrom?: string | null
  validTo?: string | null
  paymentTermDays: number
  description?: string | null
  version: number
}
