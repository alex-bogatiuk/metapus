"use client"

import { tabStateKeys } from "@/components/settings/security-profile/model"
import { useTabState } from "@/hooks/useTabState"
import { api, ApiError } from "@/lib/api"
import { jsonObjectSchema, type JsonObject } from "@/lib/json-value"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type {
  PolicyRuleResponse
} from "@/types/security"
import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

export interface SecurityPolicyRulesProps {
  profileId?: string
  rules: PolicyRuleResponse[]
  setRules: React.Dispatch<React.SetStateAction<PolicyRuleResponse[]>>
}

export function useSecurityPolicyRules({
  profileId,
  rules,
  setRules,
}: SecurityPolicyRulesProps) {
  const metaEntities = useMetadataStore((s) => s.entities)

  const { entityCatalogs, entityDocuments, allEntities, entityLabelMap } = useMemo(() => {
    const cats: { value: string; label: string }[] = []
    const docs: { value: string; label: string }[] = []
    const labelMap: Record<string, string> = { "*": "Все сущности" }
    for (const e of metaEntities) {
      const label = e.presentation.singular ?? e.name
      const item = { value: e.key, label }
      labelMap[e.key] = label
      if (e.type === "catalog") cats.push(item)
      else docs.push(item)
    }
    const byLabel = (a: { label: string }, b: { label: string }) => a.label.localeCompare(b.label, "ru")
    cats.sort(byLabel)
    docs.sort(byLabel)
    return {
      entityCatalogs: cats,
      entityDocuments: docs,
      allEntities: [...docs, ...cats],
      entityLabelMap: labelMap,
    }
  }, [metaEntities])

  const defaultEntityKey = "*"

  const [adding, setAdding] = useTabState(tabStateKeys.celAdding, false)

  const [editingRuleId, setEditingRuleId] = useTabState(tabStateKeys.celEditingId, null)

  const [saving, setSaving] = useState(false)

  const [validationResult, setValidationResult] = useState<{ valid: boolean; error?: string } | null>(null)

  const [ruleFields, setRuleFields] = useState<{ name: string; label?: string; type: string }[]>([])

  const [newRule, setNewRule] = useTabState(tabStateKeys.celNewRule, {
    name: "",
    entityName: defaultEntityKey,
    actions: ["update"],
    expression: "",
    effect: "deny",
    priority: 10,
  })

  const [sandboxOpen, setSandboxOpen] = useState(false)

  const [sandboxEntity, setSandboxEntity] = useState("")

  const [sandboxExpr, setSandboxExpr] = useState("")

  const [sandboxDoc, setSandboxDoc] = useState("{}")

  const [sandboxAction, setSandboxAction] = useState("update")

  const [sandboxResult, setSandboxResult] = useState<{ result: boolean; error?: string; elapsed: string } | null>(null)

  const [sandboxTesting, setSandboxTesting] = useState(false)

  const [sandboxMockLoading, setSandboxMockLoading] = useState(false)

  const [sandboxFields, setSandboxFields] = useState<{ name: string; label?: string; type: string }[]>([])

  const [sandboxCheatOpen, setSandboxCheatOpen] = useState(false)

  const handleLoadMock = async (entity: string) => {
    setSandboxMockLoading(true)
    try {
      const [mock, meta] = await Promise.all([
        api.meta.getMock(entity),
        api.meta.getEntity(entity),
      ])
      setSandboxDoc(JSON.stringify(mock, null, 2))
      setSandboxFields(meta.fields ?? [])
      setSandboxResult(null)
    } catch {
      // fallback — keep current doc
    } finally {
      setSandboxMockLoading(false)
    }
  }

  const handleSandboxEntityChange = (entity: string) => {
    setSandboxEntity(entity)
    if (entity) handleLoadMock(entity)
  }

  const validateExpression = async (expr: string) => {
    if (!expr.trim()) { setValidationResult(null); return }
    try {
      const res = await api.security.rules.validate(expr)
      setValidationResult(res)
    } catch {
      setValidationResult({ valid: false, error: "Ошибка валидации" })
    }
  }

  useEffect(() => {
    const entity = newRule.entityName
    if (!entity || entity === "*") {
      setRuleFields([])
      return
    }
    let cancelled = false
    api.meta.getEntity(entity).then((meta) => {
      if (!cancelled) setRuleFields(meta.fields ?? [])
    }).catch(() => {
      if (!cancelled) setRuleFields([])
    })
    return () => { cancelled = true }
  }, [newRule.entityName])

  const handleEditRule = (rule: PolicyRuleResponse) => {
    setNewRule({
      name: rule.name,
      entityName: rule.entityName,
      actions: [...rule.actions],
      expression: rule.expression,
      effect: rule.effect as "deny" | "allow",
      priority: rule.priority,
    })
    setValidationResult(null)
    setEditingRuleId(rule.id)
    setAdding(true)
  }

  const handleSaveRule = async () => {
    if (!profileId) return
    if (!newRule.name || !newRule.expression) {
      toast.error("Название и выражение обязательны")
      return
    }
    if (newRule.actions.length === 0) {
      toast.error("Выберите хотя бы одно действие")
      return
    }
    setSaving(true)
    try {
      if (editingRuleId) {
        const updated = await api.security.rules.update(profileId, editingRuleId, {
          name: newRule.name,
          entityName: newRule.entityName,
          actions: newRule.actions,
          expression: newRule.expression,
          effect: newRule.effect,
          priority: newRule.priority,
        })
        setRules((prev) => prev.map((r) => r.id === editingRuleId ? updated : r))
        toast.success("Условие обновлено")
      } else {
        const created = await api.security.rules.create(profileId, {
          name: newRule.name,
          entityName: newRule.entityName,
          actions: newRule.actions,
          expression: newRule.expression,
          effect: newRule.effect,
          priority: newRule.priority,
          enabled: true,
        })
        setRules((prev) => [...prev, created])
        toast.success("Условие добавлено")
      }
      setAdding(false)
      setEditingRuleId(null)
      setNewRule({ name: "", entityName: defaultEntityKey, actions: ["update"], expression: "", effect: "deny", priority: 10 })
      setValidationResult(null)
    } catch (e) {
      toast.error(e instanceof ApiError ? (e as ApiError).message : "Ошибка сохранения")
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteRule = async (rule: PolicyRuleResponse) => {
    if (!profileId) return
    try {
      await api.security.rules.delete(profileId, rule.id)
      setRules((prev) => prev.filter((r) => r.id !== rule.id))
      toast.success("Условие удалено")
    } catch {
      toast.error("Не удалось удалить условие")
    }
  }

  const handleToggleRule = async (rule: PolicyRuleResponse) => {
    if (!profileId) return
    try {
      const updated = await api.security.rules.update(profileId, rule.id, {
        enabled: !rule.enabled,
      })
      setRules((prev) => prev.map((r) => (r.id === rule.id ? updated : r)))
    } catch {
      toast.error("Не удалось обновить условие")
    }
  }

  const handleTestExpression = async () => {
    if (!sandboxExpr.trim()) return
    setSandboxTesting(true)
    setSandboxResult(null)
    try {
      let doc: JsonObject = {}
      try {
        doc = jsonObjectSchema.parse(JSON.parse(sandboxDoc))
      } catch {
        setSandboxResult({ result: false, error: "Невалидный JSON документа", elapsed: "0s" })
        setSandboxTesting(false)
        return
      }
      const res = await api.security.rules.test(sandboxExpr, doc, sandboxAction)
      setSandboxResult(res)
    } catch {
      setSandboxResult({ result: false, error: "Ошибка выполнения", elapsed: "0s" })
    } finally {
      setSandboxTesting(false)
    }
  }
  return { entityCatalogs, entityDocuments, entityLabelMap, adding, setAdding, editingRuleId, setEditingRuleId, saving, validationResult, setValidationResult, ruleFields, newRule, setNewRule, sandboxOpen, setSandboxOpen, sandboxEntity, sandboxExpr, setSandboxExpr, sandboxDoc, setSandboxDoc, sandboxAction, setSandboxAction, sandboxResult, setSandboxResult, sandboxTesting, sandboxMockLoading, sandboxFields, sandboxCheatOpen, setSandboxCheatOpen, handleSandboxEntityChange, handleEditRule, handleSaveRule, handleDeleteRule, handleToggleRule, handleTestExpression }
}
