// frontend/lib/security-registry.ts
/**
 * Security profile building blocks: RLS dimensions with their option search,
 * ready-made profile presets and role-affinity hints. The platform declares
 * its own; a configuration adds its parts at start-up.
 */

import type { ElementType } from "react"
import { Briefcase, Calculator, Eye } from "lucide-react"
import { api } from "@/lib/api"
import type { FieldPolicyItem } from "@/types/security"

// ── RLS dimensions ───────────────────────────────────────────────────────

export interface DimensionOption {
  id: string
  name: string
  code?: string
}

/** An RLS dimension: a metadata key whose records a profile can be limited to. */
export interface SecurityDimension {
  key: string
  search(search: string): Promise<DimensionOption[]>
}

const dimensions = new Map<string, SecurityDimension>([
  ["organization", {
    key: "organization",
    search: async (search) => {
      const res = await api.organizations.list({ search: search || undefined, limit: 20 })
      return (res.items ?? []).map((o) => ({ id: o.id, name: o.name, code: o.code }))
    },
  }],
  ["counterparty", {
    key: "counterparty",
    search: async (search) => {
      const res = await api.counterparties.list({ search: search || undefined, limit: 20 })
      return (res.items ?? []).map((c) => ({ id: c.id, name: c.name, code: c.code }))
    },
  }],
])

/** Adds or replaces an RLS dimension. */
export function registerSecurityDimension(dimension: SecurityDimension): void {
  dimensions.set(dimension.key, dimension)
}

export function getSecurityDimensions(): SecurityDimension[] {
  return [...dimensions.values()]
}

export function getSecurityDimension(key: string): SecurityDimension | undefined {
  return dimensions.get(key)
}

// ── Profile presets ──────────────────────────────────────────────────────

export interface ProfilePreset {
  code: string
  name: string
  description: string
  icon: ElementType
  dimensions: Record<string, string[]>
  entityDimensions?: Record<string, Record<string, string[]>>
  fieldPolicies: {
    entityName: string
    action: "read" | "write"
    allowedFields: string[]
    tableParts?: Record<string, string[]>
  }[]
}

const presets: ProfilePreset[] = [
  {
    code: "viewer",
    name: "Только просмотр",
    description: "Чтение без финансовых полей (суммы, комиссии)",
    icon: Eye,
    dimensions: {},
    fieldPolicies: [],
  },
  {
    code: "manager_limited",
    name: "Менеджер",
    description: "CRUD без проведения, ограниченные организации",
    icon: Briefcase,
    dimensions: { organization: [] },
    fieldPolicies: [],
  },
  {
    code: "accountant",
    name: "Бухгалтер",
    description: "Полный доступ к документам и отчётам",
    icon: Calculator,
    dimensions: {},
    fieldPolicies: [],
  },
]

/** Extends an existing preset: its dimensions and field policies are added. */
export interface ProfilePresetExtension {
  code: string
  dimensions?: Record<string, string[]>
  fieldPolicies?: ProfilePreset["fieldPolicies"]
}

/**
 * Adds a preset after the preset with code `after` (at the end otherwise), or
 * extends the preset whose code it names.
 */
export function registerProfilePreset(preset: (ProfilePreset & { after?: string }) | ProfilePresetExtension): void {
  const existing = presets.find((p) => p.code === preset.code)
  if (existing) {
    existing.dimensions = { ...existing.dimensions, ...preset.dimensions }
    existing.fieldPolicies = [...existing.fieldPolicies, ...(preset.fieldPolicies ?? [])]
    return
  }
  if (!("name" in preset)) throw new Error(`Profile preset ${preset.code} is not registered`)
  const { after, ...full } = preset
  const anchor = after === undefined ? -1 : presets.findIndex((p) => p.code === after)
  if (anchor < 0) presets.push(full)
  else presets.splice(anchor + 1, 0, full)
}

export function getProfilePresets(): ProfilePreset[] {
  return [...presets]
}

// ── Role affinity hints ──────────────────────────────────────────────────

/** The parts of a profile draft an affinity rule looks at. */
export interface ProfileShape {
  dimensions: Record<string, string[]>
  fieldPolicies: FieldPolicyItem[]
}

export interface AffinityRule {
  roleHint: string
  label: string
  match: (form: ProfileShape) => boolean
}

const affinityRules: AffinityRule[] = [
  {
    roleHint: "viewer",
    label: "Только просмотр",
    match: (f) =>
      f.fieldPolicies.some((p) => p.action === "read" && p.allowedFields.some((a) => a.startsWith("-"))),
  },
  {
    roleHint: "manager",
    label: "Менеджер",
    match: (f) =>
      Object.keys(f.dimensions).includes("organization") && f.fieldPolicies.length === 0,
  },
]

export function registerAffinityRule(rule: AffinityRule): void {
  affinityRules.push(rule)
}

export function getAffinityRules(): AffinityRule[] {
  return [...affinityRules]
}

// ── Permission resource labels ───────────────────────────────────────────

/** Labels for permission resources that are not metadata entities. */
const resourceLabels: Record<string, string> = {
  admin: "Система",
  report_document_journal: "Журнал документов",
  report_documents: "Журнал документов",
}

export function registerResourceLabels(labels: Record<string, string>): void {
  Object.assign(resourceLabels, labels)
}

export function getResourceLabel(resource: string): string | undefined {
  return resourceLabels[resource]
}
