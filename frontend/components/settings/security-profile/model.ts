import { createTabStateKey } from "@/stores/useTabStateStore"
import type {
  FieldPolicyItem,
  PolicyRuleResponse,
  SecurityProfileResponse
} from "@/types/security"

export const KNOWN_ACTIONS: { value: "read" | "write"; label: string }[] = [
  { value: "read", label: "Просмотр поля" },
  { value: "write", label: "Редактирование поля" },
]

export const RULE_ACTIONS: { value: string; label: string }[] = [
  { value: "create", label: "Создание" },
  { value: "read", label: "Чтение" },
  { value: "update", label: "Изменение" },
  { value: "delete", label: "Удаление" },
  { value: "post", label: "Проведение" },
  { value: "unpost", label: "Отмена проведения" },
]

export const RLS_DIMENSION_KEYS = ["organization", "merchant", "counterparty"] as const

export interface FormState {
  code: string
  name: string
  description: string
  dimensions: Record<string, string[]>
  entityDimensions: Record<string, Record<string, string[]>>
  fieldPolicies: FieldPolicyItem[]
}

export const EMPTY_FORM: FormState = {
  code: "",
  name: "",
  description: "",
  dimensions: {},
  entityDimensions: {},
  fieldPolicies: [],
}

export interface NewRuleState {
  name: string
  entityName: string
  actions: string[]
  expression: string
  effect: "deny" | "allow"
  priority: number
}

export const tabStateKeys = {
  profile: createTabStateKey<SecurityProfileResponse | null>("frontend/app/admin/security-profiles:profile"),
  form: createTabStateKey<FormState>("frontend/app/admin/security-profiles:form"),
  rules: createTabStateKey<PolicyRuleResponse[]>("frontend/app/admin/security-profiles:rules"),
  activeTab: createTabStateKey<string>("frontend/app/admin/security-profiles:activeTab"),
  celAdding: createTabStateKey<boolean>("frontend/app/admin/security-profiles:celAdding"),
  celEditingId: createTabStateKey<string | null>("frontend/app/admin/security-profiles:celEditingId"),
  celNewRule: createTabStateKey<NewRuleState>("frontend/app/admin/security-profiles:celNewRule"),
}

export interface AffinityRule {
  roleHint: string
  label: string
  match: (form: FormState) => boolean
}

export const AFFINITY_RULES: AffinityRule[] = [
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
  {
    roleHint: "merchant_operator",
    label: "Оператор мерчанта",
    match: (f) =>
      Object.keys(f.dimensions).includes("merchant") && f.fieldPolicies.length === 0,
  },
  {
    roleHint: "compliance",
    label: "Комплаенс",
    match: (f) =>
      Object.keys(f.dimensions).includes("merchant") &&
      f.fieldPolicies.some((p) => p.allowedFields.some((a) => a.startsWith("-"))),
  },
]
