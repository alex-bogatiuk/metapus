/**
 * Settings types for the system configuration page.
 * Mirrors backend tenant.Settings JSONB structure.
 */

// ── Organization ────────────────────────────────────────────────────────

export interface OrganizationSettings {
  companyName: string
  shortName: string
  inn: string
  kpp: string
  ogrn: string
  legalAddress: string
  actualAddress: string
  phone: string
  email: string
  website: string
  director: string
  accountant: string
  logoUrl: string
}

export function defaultOrganizationSettings(): OrganizationSettings {
  return {
    companyName: "",
    shortName: "",
    inn: "",
    kpp: "",
    ogrn: "",
    legalAddress: "",
    actualAddress: "",
    phone: "",
    email: "",
    website: "",
    director: "",
    accountant: "",
    logoUrl: "",
  }
}

// ── Accounting ──────────────────────────────────────────────────────────

export type TaxSystem =
  | "osno"
  | "usn_income"
  | "usn_income_expense"
  | "envd"
  | "patent"

export type InventoryMethod = "fifo" | "average" | "specific"

export type VatRate = "0" | "10" | "20" | "none"

export interface AccountingSettings {
  defaultCurrency: string
  taxSystem: TaxSystem
  vatPayer: boolean
  defaultVatRate: VatRate
  inventoryMethod: InventoryMethod
  fiscalYearStart: string
  autoNumbering: boolean
  numberPrefix: string
}

export function defaultAccountingSettings(): AccountingSettings {
  return {
    defaultCurrency: "RUB",
    taxSystem: "osno",
    vatPayer: true,
    defaultVatRate: "20",
    inventoryMethod: "fifo",
    fiscalYearStart: "01-01",
    autoNumbering: true,
    numberPrefix: "",
  }
}

// ── Users & Roles ───────────────────────────────────────────────────────

export type UserStatus = "active" | "blocked" | "invited"

export interface UserRecord {
  id: string
  fullName: string
  email: string
  role: string
  status: UserStatus
  lastLogin: string | null
  createdAt: string
}

export interface RoleRecord {
  id: string
  name: string
  description: string
  permissions: string[]
  usersCount: number
  isSystem: boolean
}

// ── Interface ───────────────────────────────────────────────────────────

export type ThemeMode = "light" | "dark" | "system"
export type DateFormat = "dd.MM.yyyy" | "yyyy-MM-dd" | "MM/dd/yyyy"
export type NumberFormat = "space" | "comma" | "none"

export interface InterfaceSettings {
  theme: ThemeMode
  language: string
  dateFormat: DateFormat
  numberFormat: NumberFormat
  pageSize: number
  showTooltips: boolean
  compactMode: boolean
  sidebarCollapsed: boolean
}

export function defaultInterfaceSettings(): InterfaceSettings {
  return {
    theme: "light",
    language: "ru",
    dateFormat: "dd.MM.yyyy",
    numberFormat: "space",
    pageSize: 25,
    showTooltips: true,
    compactMode: false,
    sidebarCollapsed: false,
  }
}

// ── Combined ────────────────────────────────────────────────────────────

export interface SystemSettings {
  organization: OrganizationSettings
  accounting: AccountingSettings
  interface: InterfaceSettings
}

export function defaultSystemSettings(): SystemSettings {
  return {
    organization: defaultOrganizationSettings(),
    accounting: defaultAccountingSettings(),
    interface: defaultInterfaceSettings(),
  }
}
