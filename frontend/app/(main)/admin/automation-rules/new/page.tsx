"use client"

import { PermissionGate } from "@/components/auth/permission-gate"
import { SYSTEM_ADMIN } from "@/lib/system-permissions"
import { useRouter } from "next/navigation"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { AutomationRuleForm } from "@/components/settings/automation-rule-form"
import { useCatalogForm } from "@/hooks/useCatalogForm"
import { api } from "@/lib/api"
import { createFormDraftKey } from "@/stores/useFormDraftStore"
import { INITIAL_RULE_STATE, validateRule, mapRuleToCreate, type RuleFormState } from "@/lib/automation-rule-form"
import type { CreateRuleRequest } from "@/types/automation"

const formKey = createFormDraftKey<RuleFormState>("frontend/app/(main)/admin/automation-rules/new/page.tsx")

function NewAutomationRulePageContent() {
  const router = useRouter()
  const { f, update, handleChange, handleSave, saving, error } = useCatalogForm<RuleFormState, never, CreateRuleRequest, never>({
    draftKey: formKey,
    entityName: "Правило автоматизации",
    initialState: INITIAL_RULE_STATE,
    api: {
      create: (data: CreateRuleRequest) => api.automation.rules.create(data),
    },
    listPath: "/admin/automation-rules",
    validate: validateRule,
    mapToCreate: mapRuleToCreate,
    titleField: (s) => s.name || undefined,
  })

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title="Правило автоматизации (создание)"
        primaryAction={{
          label: saving ? "Сохранение…" : "Записать и закрыть",
          variant: "default",
          onClick: () => handleSave(true),
        }}
        secondaryActions={[
          { label: "Записать", onClick: () => handleSave(false) },
        ]}
        backHref="/admin/automation-rules"
        onClose={() => router.push("/admin/automation-rules")}
      />

      {error && (
        <div className="bg-destructive/10 border-b border-destructive/20 px-4 py-2 text-sm text-destructive">{error}</div>
      )}

      <AutomationRuleForm value={f} onChange={patch => { update(patch); handleChange() }} />
    </div>
  )
}

export default function NewAutomationRulePage() {
  return (
    <PermissionGate permission={SYSTEM_ADMIN} subject="правилам автоматизации">
      <NewAutomationRulePageContent />
    </PermissionGate>
  )
}
