/**
 * Settings types for the system configuration page.
 * Mirrors backend sys_settings JSONB structure.
 * Organization-specific settings (requisites, accounting policy) are in catalog.ts.
 */

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
  exchangeRates: { sourceId: string | null }
  performance: PerformanceSettings
  /** The last closed day (YYYY-MM-DD in the tenant timezone), "" when none. */
  periodLock: { date: string }
  version: number
  updatedAt: string
}

export function defaultSystemSettings(): SystemSettings {
  return {
    exchangeRates: { sourceId: null },
    performance: defaultPerformanceSettings(),
    periodLock: { date: "" },
    version: 1,
    updatedAt: new Date().toISOString(),
  }
}
