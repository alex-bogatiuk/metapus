/**
 * Shared types for Catalog entities.
 * Mirrors: internal/infrastructure/http/v1/dto/nomenclature.go
 */

// ── Nomenclature ────────────────────────────────────────────────────────

/** Nomenclature type enum — mirrors domain NomenclatureType. */
export type NomenclatureType =
    | "goods"
    | "service"
    | "work"
    | "material"
    | "semi"
    | "product"

/** Labels for NomenclatureType values (for UI display). */
export const NOMENCLATURE_TYPE_LABELS: Record<NomenclatureType, string> = {
    goods: "Товар",
    service: "Услуга",
    work: "Работа",
    material: "Материал",
    semi: "Полуфабрикат",
    product: "Продукция",
}

/** Attributes — arbitrary key-value map (mirrors entity.Attributes). */
export type Attributes = Record<string, unknown>

/** Response DTO for a nomenclature item. */
export interface NomenclatureResponse {
    id: string
    code: string
    name: string
    type: NomenclatureType
    article?: string | null
    barcode?: string | null
    baseUnitId?: string | null
    defaultVatRateId?: string | null
    weight: string
    volume: string
    description?: string | null
    manufacturerId?: string | null
    countryOfOrigin?: string | null
    isWeighed: boolean
    trackSerial: boolean
    trackBatch: boolean
    imageUrl?: string | null
    parentId?: string | null
    isFolder: boolean
    deletionMark: boolean
    version: number
    attributes?: Attributes | null
}

/** Request DTO for creating a nomenclature item. */
export interface CreateNomenclatureRequest {
    code?: string
    name: string
    type: NomenclatureType
    article?: string | null
    barcode?: string | null
    baseUnitId?: string | null
    baseUnitName?: string
    defaultVatRateId?: string | null
    weight?: string
    volume?: string
    description?: string | null
    manufacturerId?: string | null
    countryOfOrigin?: string | null
    isWeighed?: boolean
    trackSerial?: boolean
    trackBatch?: boolean
    imageUrl?: string | null
    parentId?: string | null
    isFolder?: boolean
    attributes?: Attributes | null
}

/** Request DTO for updating a nomenclature item. */
export interface UpdateNomenclatureRequest {
    code?: string
    name: string
    type: NomenclatureType
    article?: string | null
    barcode?: string | null
    baseUnitId?: string | null
    defaultVatRateId?: string | null
    weight?: string
    volume?: string
    description?: string | null
    manufacturerId?: string | null
    countryOfOrigin?: string | null
    isWeighed?: boolean
    trackSerial?: boolean
    trackBatch?: boolean
    imageUrl?: string | null
    parentId?: string | null
    isFolder?: boolean
    attributes?: Attributes | null
    version: number
}

/** Request DTO for setting/clearing deletion mark. Mirrors dto.SetDeletionMarkRequest (json:"marked"). */
export interface SetDeletionMarkRequest {
    marked: boolean
}

// ── Counterparty ────────────────────────────────────────────────────────

/** Counterparty type enum — mirrors domain CounterpartyType. */
export type CounterpartyType = "customer" | "supplier" | "both" | "other"

export const COUNTERPARTY_TYPE_LABELS: Record<CounterpartyType, string> = {
    customer: "Покупатель",
    supplier: "Поставщик",
    both: "Покупатель и Поставщик",
    other: "Прочие",
}

/** Legal form enum — mirrors domain LegalForm. */
export type LegalForm = "individual" | "sole_trader" | "company" | "government"

export const LEGAL_FORM_LABELS: Record<LegalForm, string> = {
    individual: "Физлицо",
    sole_trader: "ИП",
    company: "Юрлицо",
    government: "Гос. орган",
}

/** Response DTO for a counterparty. */
export interface CounterpartyResponse {
    id: string
    code: string
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
    isFolder: boolean
    deletionMark: boolean
    version: number
    attributes?: Attributes | null
}

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

// ── Warehouse ───────────────────────────────────────────────────────────

/** Warehouse type enum — mirrors domain WarehouseType. */
export type WarehouseType = "main" | "distribution" | "retail" | "production" | "transit"

export const WAREHOUSE_TYPE_LABELS: Record<WarehouseType, string> = {
    main: "Основной",
    distribution: "Распределительный",
    retail: "Розничный",
    production: "Производственный",
    transit: "Транзитный",
}

/** Response DTO for a warehouse. */
export interface WarehouseResponse {
    id: string
    code: string
    name: string
    type: WarehouseType
    address?: string | null
    isActive: boolean
    allowNegativeStock: boolean
    isDefault: boolean
    organizationId?: string
    description?: string | null
    parentId?: string | null
    isFolder: boolean
    deletionMark: boolean
    version: number
    attributes?: Attributes | null
}

/** Request DTO for creating a warehouse. */
export interface CreateWarehouseRequest {
    code?: string
    name: string
    type: WarehouseType
    address?: string | null
    isActive?: boolean
    allowNegativeStock?: boolean
    isDefault?: boolean
    organizationId?: string
    description?: string | null
    parentId?: string | null
    isFolder?: boolean
    attributes?: Attributes | null
}

/** Request DTO for updating a warehouse. */
export interface UpdateWarehouseRequest {
    code?: string
    name: string
    type: WarehouseType
    address?: string | null
    isActive?: boolean
    allowNegativeStock?: boolean
    isDefault?: boolean
    organizationId?: string
    description?: string | null
    parentId?: string | null
    isFolder?: boolean
    attributes?: Attributes | null
    version: number
}

// ── Organization ────────────────────────────────────────────────────────

/** Response DTO for an organization. */
export interface OrganizationResponse {
    id: string
    version: number
    code: string
    name: string
    fullName: string
    inn: string
    kpp: string
    baseCurrencyId: string
    isDefault: boolean
    deletionMark: boolean
}

/** Request DTO for creating an organization. */
export interface CreateOrganizationRequest {
    code?: string
    name: string
    fullName?: string
    inn?: string
    kpp?: string
    baseCurrencyId: string
    isDefault?: boolean
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
    baseCurrencyId: string
    isDefault?: boolean
    deletionMark?: boolean
}
