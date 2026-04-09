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

// ── Unit ───────────────────────────────────────────────────────────────

export type UnitType = "piece" | "weight" | "length" | "area" | "volume" | "time" | "pack"

export const UNIT_TYPE_LABELS: Record<UnitType, string> = {
    piece: "Штуки",
    weight: "Вес",
    length: "Длина",
    area: "Площадь",
    volume: "Объем",
    time: "Время",
    pack: "Упаковки",
}

export interface UnitResponse {
    id: string
    code: string
    name: string
    type: UnitType
    symbol: string
    internationalCode?: string | null
    baseUnitId?: string | null
    conversionFactor: string
    isBase: boolean
    description?: string | null
    deletionMark: boolean
    version: number
}

export interface CreateUnitRequest {
    code?: string
    name: string
    type: UnitType
    symbol: string
    internationalCode?: string | null
    baseUnitId?: string | null
    conversionFactor: string
    isBase: boolean
    description?: string | null
}

export interface UpdateUnitRequest {
    code?: string
    name: string
    type: UnitType
    symbol: string
    internationalCode?: string | null
    baseUnitId?: string | null
    conversionFactor: string
    isBase: boolean
    description?: string | null
    version: number
}

// ── Currency ───────────────────────────────────────────────────────────

export interface CurrencyResponse {
    id: string
    code: string
    name: string
    isoCode?: string | null
    isoNumericCode?: string | null
    symbol?: string | null
    decimalPlaces: number
    minorMultiplier: number
    isBase: boolean
    country?: string | null
    deletionMark: boolean
    version: number
}

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

export const CONTRACT_TYPE_LABELS: Record<ContractType, string> = {
    supply: "С поставщиком",
    sale: "С покупателем",
    other: "Прочее",
}

export interface ContractResponse {
    id: string
    code: string
    name: string
    counterpartyId: string
    type: ContractType
    currencyId?: string | null
    validFrom?: string | null
    validTo?: string | null
    paymentTermDays: number
    description?: string | null
    deletionMark: boolean
    version: number
}

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

// ── VATRate ────────────────────────────────────────────────────────────

export interface VATRateResponse {
    id: string
    code: string
    name: string
    rate: string
    isTaxExempt: boolean
    description?: string | null
    deletionMark: boolean
    version: number
}

export interface CreateVATRateRequest {
    code?: string
    name: string
    rate: string
    isTaxExempt: boolean
    description?: string | null
}

export interface UpdateVATRateRequest {
    code?: string
    name: string
    rate: string
    isTaxExempt: boolean
    description?: string | null
    version: number
}
