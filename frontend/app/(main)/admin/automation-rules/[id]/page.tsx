"use client"

import { useRouter } from "next/navigation"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { AutomationRuleForm } from "@/components/settings/automation-rule-form"
import { useCatalogForm } from "@/hooks/useCatalogForm"
import { api } from "@/lib/api"
import { createFormDraftKey } from "@/stores/useFormDraftStore"
import { INITIAL_RULE_STATE, validateRule, mapRuleToUpdate, mapRuleFromResponse, type RuleFormState } from "@/lib/automation-rule-form"
import type { JsonObject } from "@/lib/json-value"
import { Button } from "@/components/ui/button"
import { Play } from "lucide-react"
import type { AutomationRule, UpdateRuleRequest } from "@/types/automation"
import { useState } from "react"
import { toast } from "sonner"

const formKey = createFormDraftKey<RuleFormState>("frontend/app/(main)/admin/automation-rules/[id]/page.tsx")

export default function EditAutomationRulePage() {
  const router = useRouter()
  const { f, update, handleChange, handleSave, saving, error, loading, deletionMark } = useCatalogForm<RuleFormState, AutomationRule, never, UpdateRuleRequest>({
    draftKey: formKey,
    entityName: "Правило автоматизации",
    initialState: INITIAL_RULE_STATE,
    api: {
      get: api.automation.rules.get,
      update: (id: string, data: UpdateRuleRequest) => api.automation.rules.update(id, data),
    },
    listPath: "/admin/automation-rules",
    validate: validateRule,
    mapFromResponse: mapRuleFromResponse,
    mapToUpdate: mapRuleToUpdate,
    getVersion: r => r.version,
    titleField: (s) => s.name || undefined,
  })

  // ── Test ────────────────────────────────────────────────────────────────
  const [isTesting, setIsTesting] = useState(false)

  const handleTest = async () => {
    setIsTesting(true)
    try {
      const payload: JsonObject = {
        action: f.triggerType === "scheduled" ? "scheduled" : "posted",
        entityType: f.triggerType === "scheduled" ? "automation" : "document",
        doc: f.triggerType === "scheduled"
          ? { ruleName: f.name, ruleId: f.id || "test" }
          : { id: "test-doc-123", number: "ПТ-000042", totalAmount: "150000", docTotal: "150000" },
        currency: { decimalPlaces: 2, code: "RUB" },
        humanAmounts: { totalAmount: "1500.00", docTotal: "1500.00" },
      }
      const res = await api.automation.rules.test({
        conditionCel: f.conditionCel || undefined,
        actionTemplate: f.actionTemplate,
        payload,
      })

      if (f.triggerType !== "scheduled" && !res.conditionMatched) {
        toast.error(`Условие не выполнено. ${res.conditionError || "Результат: false"}`)
      } else if (res.renderError) {
        toast.error(`Ошибка шаблона: ${res.renderError}`)
      } else {
        toast.success("Шаблон отрендерен", {
          description: (
            <pre className="text-[10px] mt-2 bg-muted text-foreground p-2 rounded-md overflow-x-auto max-h-[200px]">
              {res.renderedPayload}
            </pre>
          ),
          duration: 10000,
        })
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Ошибка"
      toast.error("Ошибка тестирования: " + msg)
    } finally {
      setIsTesting(false)
    }
  }

  if (loading) {
    return <div className="p-4 text-sm text-muted-foreground">Загрузка...</div>
  }

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title={f.name || "Новое"}
        status={deletionMark ? { label: "Удалён", variant: "destructive" } : undefined}
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
      >
        <Button variant="outline" size="sm" onClick={handleTest} disabled={isTesting}>
          <Play className="h-4 w-4" />
          {isTesting ? "Тест…" : "Тест шаблона"}
        </Button>
      </FormToolbar>

      {error && (
        <div className="bg-destructive/10 border-b border-destructive/20 px-4 py-2 text-sm text-destructive">{error}</div>
      )}

      <AutomationRuleForm value={f} onChange={patch => { update(patch); handleChange() }} />
    </div>
  )
}
