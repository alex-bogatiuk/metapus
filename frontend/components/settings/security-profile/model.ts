import { createTabStateKey } from "@/stores/useTabStateStore"
import type {
  FieldPolicyItem,
  PolicyRuleResponse,
  SecurityProfileResponse
} from "@/types/security"

export const RULE_ACTIONS: { value: string; label: string }[] = [
  { value: "create", label: "Создание" },
  { value: "read", label: "Чтение" },
  { value: "update", label: "Изменение" },
  { value: "delete", label: "Удаление" },
  { value: "post", label: "Проведение" },
  { value: "unpost", label: "Отмена проведения" },
]

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
