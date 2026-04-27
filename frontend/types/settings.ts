/**
 * Settings types for the system configuration page.
 * Mirrors backend sys_settings JSONB structure.
 * Organization-specific settings (requisites, accounting policy) are in catalog.ts.
 */

// ── Numbering ───────────────────────────────────────────────────────────

export interface NumberingSettings {
  autoNumbering: boolean
  numberPrefix: string
}

export function defaultNumberingSettings(): NumberingSettings {
  return {
    autoNumbering: true,
    numberPrefix: "",
  }
}

// ── Performance ─────────────────────────────────────────────────────────

export interface PerformanceSettings {
  /** Number of documents processed in parallel during batch operations (1–10). */
  batchConcurrency: number
}

export function defaultPerformanceSettings(): PerformanceSettings {
  return {
    batchConcurrency: 5,
  }
}

// ── Warehouse ───────────────────────────────────────────────────────────

export interface WarehouseSettings {
  /** Costing method: "fifo" or "weighted_average". */
  inventoryMethod: "fifo" | "weighted_average"
  /** Prevent posting when stock would go below zero. */
  negativeStockControl: boolean
  /** Automatically post goods receipts upon saving. */
  autoPostReceipts: boolean
}

export function defaultWarehouseSettings(): WarehouseSettings {
  return {
    inventoryMethod: "fifo",
    negativeStockControl: true,
    autoPostReceipts: false,
  }
}

// ── Sales ────────────────────────────────────────────────────────────────

export interface SalesSettings {
  /** Default payment deadline in days for new invoices. */
  defaultPaymentTermDays: number
  /** Automatically reserve stock when sales order is confirmed. */
  autoReserveStock: boolean
}

export function defaultSalesSettings(): SalesSettings {
  return {
    defaultPaymentTermDays: 30,
    autoReserveStock: false,
  }
}

// ── Purchasing ──────────────────────────────────────────────────────────

export interface PurchasingSettings {
  /** Default payment deadline in days for purchase orders. */
  defaultPaymentTermDays: number
  /** Require manager approval for purchase orders. */
  requireApproval: boolean
}

export function defaultPurchasingSettings(): PurchasingSettings {
  return {
    defaultPaymentTermDays: 30,
    requireApproval: false,
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

// ── Combined ────────────────────────────────────────────────────────────

export interface SystemSettings {
  numbering: NumberingSettings
  performance: PerformanceSettings
  warehouse: WarehouseSettings
  sales: SalesSettings
  purchasing: PurchasingSettings
  version: number
  updatedAt: string
}

export function defaultSystemSettings(): SystemSettings {
  return {
    numbering: defaultNumberingSettings(),
    performance: defaultPerformanceSettings(),
    warehouse: defaultWarehouseSettings(),
    sales: defaultSalesSettings(),
    purchasing: defaultPurchasingSettings(),
    version: 1,
    updatedAt: new Date().toISOString(),
  }
}
