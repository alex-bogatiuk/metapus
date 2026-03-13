"use client"

import { useCallback, useEffect, useState } from "react"
import { useParams, useRouter } from "next/navigation"
import {
  Save,
  Loader2,
  Plus,
  Trash2,
  X,
  AlertTriangle,
  Check,
  ArrowLeft,
  ChevronRight,
  BookOpen,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Separator } from "@/components/ui/separator"
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { ScrollArea } from "@/components/ui/scroll-area"
import { cn } from "@/lib/utils"
import { api, ApiError } from "@/lib/api"
import { RlsDimensionPicker } from "@/components/settings/rls-dimension-picker"
import { FlsFieldMatrix } from "@/components/settings/fls-field-picker"
import { CelEditor, preloadMonaco } from "@/components/settings/cel-editor"
import { toast } from "sonner"
import { useTabTitle } from "@/hooks/useTabTitle"
import { getActionLabel, formatAuditChanges, type AuditEventLine } from "@/lib/audit-format"
import { useProfilePresetStore } from "@/stores/useProfilePresetStore"
import type {
  SecurityProfileResponse,
  FieldPolicyItem,
  PolicyRuleResponse,
  CreatePolicyRuleRequest,
  AuditEntryResponse,
} from "@/types/security"

// ── Known entities for FLS dropdown ───────────────────────────────────────

const KNOWN_ENTITIES = [
  { value: "GoodsReceipt", label: "Поступление товаров" },
  { value: "GoodsIssue", label: "Реализация товаров" },
]

const KNOWN_ACTIONS: { value: "read" | "write"; label: string }[] = [
  { value: "read", label: "Просмотр поля" },
  { value: "write", label: "Редактирование поля" },
]

// ── Known RLS dimensions ──────────────────────────────────────────────────

const KNOWN_DIMENSIONS = [
  { key: "organization", label: "Организации" },
  { key: "warehouse", label: "Склады" },
  { key: "counterparty", label: "Контрагенты" },
]


// ── Form state ──────────────────────────────────────────────────────────

interface FormState {
  code: string
  name: string
  description: string
  dimensions: Record<string, string[]>
  fieldPolicies: FieldPolicyItem[]
}

const EMPTY_FORM: FormState = {
  code: "",
  name: "",
  description: "",
  dimensions: {},
  fieldPolicies: [],
}

// ── Role-Profile Affinity Hint ──────────────────────────────────────────

interface AffinityRule {
  roleHint: string
  label: string
  match: (form: FormState) => boolean
}

const AFFINITY_RULES: AffinityRule[] = [
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
    roleHint: "accountant",
    label: "Бухгалтер",
    match: (f) =>
      Object.keys(f.dimensions).includes("warehouse") && f.fieldPolicies.length === 0,
  },
  {
    roleHint: "warehouse",
    label: "Кладовщик",
    match: (f) =>
      Object.keys(f.dimensions).includes("warehouse") &&
      f.fieldPolicies.some((p) => p.allowedFields.some((a) => a.startsWith("-"))),
  },
]

function RoleAffinityHint({ form }: { form: FormState }) {
  const hasDims = Object.keys(form.dimensions).length > 0
  const hasFls = form.fieldPolicies.length > 0
  if (!hasDims && !hasFls) return null

  const matched = AFFINITY_RULES.filter((r) => r.match(form))
  if (matched.length === 0) return null

  return (
    <div className="rounded-md border border-blue-200 bg-blue-50/50 px-3 py-2.5">
      <p className="text-[11px] text-blue-800 font-medium mb-1">Рекомендуемые роли для этого профиля</p>
      <div className="flex flex-wrap gap-1.5">
        {matched.map((m) => (
          <Badge key={m.roleHint} variant="outline" className="text-[10px] h-5 bg-white border-blue-200 text-blue-700">
            {m.label}
            <span className="ml-1 text-blue-400 font-mono">{m.roleHint}</span>
          </Badge>
        ))}
      </div>
      <p className="text-[10px] text-blue-600/70 mt-1.5">
        Подсказка на основе настроенных ограничений (RLS/FLS). Назначьте роль пользователю на вкладке «Пользователи и роли».
      </p>
    </div>
  )
}

// ── Main Page ────────────────────────────────────────────────────────────

export default function SecurityProfilePage() {
  const params = useParams()
  const router = useRouter()
  const profileId = params.id as string
  const isNew = profileId === "new"

  const [profile, setProfile] = useState<SecurityProfileResponse | null>(null)
  const [form, setForm] = useState<FormState>(EMPTY_FORM)
  const [rules, setRules] = useState<PolicyRuleResponse[]>([])
  const [loading, setLoading] = useState(!isNew)
  const [saving, setSaving] = useState(false)
  const [activeTab, setActiveTab] = useState("general")

  useTabTitle(
    profile ? profile.name : isNew ? "Новый профиль" : undefined,
    "Профиль безопасности"
  )

  // Preload Monaco editor CDN while user is on other tabs
  useEffect(() => { preloadMonaco() }, [])

  // Apply preset for new profiles
  useEffect(() => {
    if (!isNew) return
    const preset = useProfilePresetStore.getState().preset
    if (!preset) return
    setForm({
      code: preset.code,
      name: preset.name,
      description: preset.description,
      dimensions: preset.dimensions,
      fieldPolicies: preset.fieldPolicies,
    })
    useProfilePresetStore.getState().clear()
  }, [isNew])

  // Load profile data
  useEffect(() => {
    if (isNew) return
    let cancelled = false
    api.security.profiles.get(profileId).then((data) => {
      if (cancelled) return
      setProfile(data)
      setForm({
        code: data.code,
        name: data.name,
        description: data.description ?? "",
        dimensions: data.dimensions ?? {},
        fieldPolicies: data.fieldPolicies ?? [],
      })
      setRules(data.policyRules ?? [])
      setLoading(false)
    }).catch(() => {
      if (!cancelled) {
        toast.error("Не удалось загрузить профиль")
        setLoading(false)
      }
    })
    return () => { cancelled = true }
  }, [profileId, isNew])

  const handleSave = useCallback(async () => {
    if (!form.code || !form.name) {
      toast.error("Код и название обязательны")
      return
    }
    setSaving(true)
    try {
      if (isNew) {
        const created = await api.security.profiles.create({
          code: form.code,
          name: form.name,
          description: form.description || undefined,
          dimensions: form.dimensions,
          fieldPolicies: form.fieldPolicies,
        })
        toast.success("Профиль создан")
        router.replace(`/settings/security-profiles/${created.id}`)
      } else {
        const updated = await api.security.profiles.update(profileId, {
          code: form.code,
          name: form.name,
          description: form.description || undefined,
          dimensions: form.dimensions,
          fieldPolicies: form.fieldPolicies,
        })
        setProfile(updated)
        toast.success("Профиль сохранён")
      }
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "Ошибка сохранения")
    } finally {
      setSaving(false)
    }
  }, [form, isNew, profileId, router])

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="shrink-0 border-b bg-background px-6 py-3">
        <div className="flex items-center justify-between max-w-5xl mx-auto">
          <div className="flex items-center gap-3">
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              onClick={() => router.push("/settings")}
            >
              <ArrowLeft className="h-4 w-4" />
            </Button>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-base font-semibold">
                  {isNew ? "Новый профиль безопасности" : profile?.name}
                </h1>
                {profile?.isSystem && (
                  <Badge variant="secondary" className="text-[10px]">Системный</Badge>
                )}
              </div>
              {!isNew && (
                <p className="text-xs text-muted-foreground font-mono">{profile?.code}</p>
              )}
            </div>
          </div>
          <Button size="sm" onClick={handleSave} disabled={saving}>
            {saving ? (
              <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
            ) : (
              <Save className="mr-1.5 h-3.5 w-3.5" />
            )}
            Сохранить
          </Button>
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-auto">
        <div className="max-w-5xl mx-auto px-6 py-6">
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="w-fit mb-6">
              <TabsTrigger value="general">Основное</TabsTrigger>
              <TabsTrigger value="rls">
                Доступ к данным
                {Object.keys(form.dimensions).length > 0 && (
                  <Badge variant="secondary" className="ml-1.5 h-4 min-w-4 px-1 text-[10px]">
                    {Object.keys(form.dimensions).length}
                  </Badge>
                )}
              </TabsTrigger>
              <TabsTrigger value="fls">
                Скрытие полей
                {form.fieldPolicies.length > 0 && (
                  <Badge variant="secondary" className="ml-1.5 h-4 min-w-4 px-1 text-[10px]">
                    {form.fieldPolicies.length}
                  </Badge>
                )}
              </TabsTrigger>
              <TabsTrigger value="rules">
                Сложные условия (CEL)
                {rules.length > 0 && (
                  <Badge variant="secondary" className="ml-1.5 h-4 min-w-4 px-1 text-[10px]">
                    {rules.length}
                  </Badge>
                )}
              </TabsTrigger>
              {!isNew && (
                <TabsTrigger value="users">
                  Пользователи
                  {(profile?.userCount ?? 0) > 0 && (
                    <Badge variant="secondary" className="ml-1.5 h-4 min-w-4 px-1 text-[10px]">
                      {profile!.userCount}
                    </Badge>
                  )}
                </TabsTrigger>
              )}
              {!isNew && <TabsTrigger value="audit">История</TabsTrigger>}
            </TabsList>

            <TabsContent value="general" className="mt-0">
              <div className="max-w-xl space-y-4">
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <Label className="mb-1.5 text-xs text-muted-foreground">Код</Label>
                    <Input
                      value={form.code}
                      onChange={(e) => setForm((f) => ({ ...f, code: e.target.value }))}
                      placeholder="manager_limited"
                      className="h-9 text-sm font-mono"
                      disabled={profile?.isSystem}
                    />
                  </div>
                  <div>
                    <Label className="mb-1.5 text-xs text-muted-foreground">Название</Label>
                    <Input
                      value={form.name}
                      onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                      placeholder="Менеджер (ограниченный)"
                      className="h-9 text-sm"
                    />
                  </div>
                </div>
                <div>
                  <Label className="mb-1.5 text-xs text-muted-foreground">Описание</Label>
                  <Textarea
                    value={form.description}
                    onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
                    placeholder="Описание профиля безопасности..."
                    className="text-sm resize-none"
                    rows={3}
                  />
                </div>
                <RoleAffinityHint form={form} />
              </div>
            </TabsContent>

            <TabsContent value="rls" className="mt-0">
              <RlsSection form={form} setForm={setForm} />
            </TabsContent>

            <TabsContent value="fls" className="mt-0">
              <FlsSection form={form} setForm={setForm} />
            </TabsContent>

            <TabsContent value="rules" className="mt-0">
              <RulesSection
                profileId={isNew ? undefined : profileId}
                rules={rules}
                setRules={setRules}
              />
            </TabsContent>

            {!isNew && (
              <TabsContent value="users" className="mt-0">
                <UsersSection profileId={profileId} />
              </TabsContent>
            )}

            {!isNew && (
              <TabsContent value="audit" className="mt-0">
                <AuditSection profileId={profileId} />
              </TabsContent>
            )}
          </Tabs>
        </div>
      </div>
    </div>
  )
}

// ── RLS Section ──────────────────────────────────────────────────────────

function RlsSection({
  form,
  setForm,
}: {
  form: FormState
  setForm: React.Dispatch<React.SetStateAction<FormState>>
}) {
  const addDimension = (key: string) => {
    setForm((f) => ({
      ...f,
      dimensions: { ...f.dimensions, [key]: [] },
    }))
  }

  const removeDimension = (key: string) => {
    setForm((f) => {
      const dims = { ...f.dimensions }
      delete dims[key]
      return { ...f, dimensions: dims }
    })
  }

  const updateDimensionIds = (key: string, ids: string[]) => {
    setForm((f) => ({
      ...f,
      dimensions: { ...f.dimensions, [key]: ids },
    }))
  }

  const availableDimensions = KNOWN_DIMENSIONS.filter(
    (d) => !(d.key in form.dimensions)
  )

  return (
    <div className="max-w-2xl space-y-4">
      <p className="text-xs text-muted-foreground mb-3">
        Ограничьте доступ по организациям, складам или контрагентам.
        Пользователь будет видеть только документы, относящиеся к выбранным значениям.
        Если ограничения не заданы — доступны все записи.
      </p>

      {Object.entries(form.dimensions).map(([key, ids]) => {
        const meta = KNOWN_DIMENSIONS.find((d) => d.key === key)
        return (
          <div key={key} className="rounded-md border p-4 space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-sm font-medium">{meta?.label ?? key}</Label>
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6"
                onClick={() => removeDimension(key)}
              >
                <X className="h-3 w-3" />
              </Button>
            </div>
            <RlsDimensionPicker
              dimensionKey={key}
              selectedIds={ids}
              onChange={(newIds) => updateDimensionIds(key, newIds)}
            />
          </div>
        )
      })}

      {availableDimensions.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {availableDimensions.map((d) => (
            <Button
              key={d.key}
              variant="outline"
              size="sm"
              className="h-8 text-xs"
              onClick={() => addDimension(d.key)}
            >
              <Plus className="mr-1 h-3 w-3" />
              {d.label}
            </Button>
          ))}
        </div>
      )}

      {Object.keys(form.dimensions).length === 0 && (
        <div className="py-8 text-center text-xs text-muted-foreground">
          Без ограничений — пользователь видит все данные. Добавьте измерение, чтобы ограничить доступ.
        </div>
      )}
    </div>
  )
}

// ── FLS Section ──────────────────────────────────────────────────────────

function FlsSection({
  form,
  setForm,
}: {
  form: FormState
  setForm: React.Dispatch<React.SetStateAction<FormState>>
}) {
  // Group policies by entity
  const entityNames = [...new Set(form.fieldPolicies.map((p) => p.entityName))]

  const getPolicy = (entity: string, action: "read" | "write"): FieldPolicyItem | undefined =>
    form.fieldPolicies.find((p) => p.entityName === entity && p.action === action)

  const handleEntityChange = (
    entity: string,
    readPolicy: FieldPolicyItem | undefined,
    writePolicy: FieldPolicyItem | undefined,
  ) => {
    setForm((f) => {
      // Remove old policies for this entity
      const rest = f.fieldPolicies.filter((p) => p.entityName !== entity)
      // Add back non-undefined policies
      const next = [...rest]
      if (readPolicy) next.push(readPolicy)
      if (writePolicy) next.push(writePolicy)
      return { ...f, fieldPolicies: next }
    })
  }

  const addEntity = (entityValue: string) => {
    // Add a default read policy with all fields allowed — the matrix will show up
    setForm((f) => ({
      ...f,
      fieldPolicies: [
        ...f.fieldPolicies,
        { entityName: entityValue, action: "read" as const, allowedFields: ["*"], tableParts: {} },
      ],
    }))
  }

  const removeEntity = (entity: string) => {
    setForm((f) => ({
      ...f,
      fieldPolicies: f.fieldPolicies.filter((p) => p.entityName !== entity),
    }))
  }

  const availableEntities = KNOWN_ENTITIES.filter((e) => !entityNames.includes(e.value))

  return (
    <div className="max-w-3xl space-y-4">
      <p className="text-xs text-muted-foreground mb-3">
        Скройте чувствительные поля (цены, суммы) или запретите их редактирование.
        Снимите галочку с поля, которое нужно скрыть. Без ограничений — все поля видимы.
      </p>

      {entityNames.map((entity) => {
        const label = KNOWN_ENTITIES.find((e) => e.value === entity)?.label ?? entity
        return (
          <div key={entity} className="rounded-md border p-4 space-y-3">
            <div className="flex items-center justify-between">
              <h4 className="text-sm font-medium">{label}</h4>
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 shrink-0"
                onClick={() => removeEntity(entity)}
              >
                <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
              </Button>
            </div>
            <FlsFieldMatrix
              entityName={entity}
              readPolicy={getPolicy(entity, "read")}
              writePolicy={getPolicy(entity, "write")}
              onChange={(r, w) => handleEntityChange(entity, r, w)}
            />
          </div>
        )
      })}

      {availableEntities.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {availableEntities.map((e) => (
            <Button
              key={e.value}
              variant="outline"
              size="sm"
              className="h-8 text-xs"
              onClick={() => addEntity(e.value)}
            >
              <Plus className="mr-1.5 h-3 w-3" />
              {e.label}
            </Button>
          ))}
        </div>
      )}

      {entityNames.length === 0 && (
        <div className="py-8 text-center text-xs text-muted-foreground">
          Все поля доступны. Добавьте документ, чтобы настроить скрытие полей.
        </div>
      )}
    </div>
  )
}

// ── Rules Section (CEL) ──────────────────────────────────────────────────

function RulesSection({
  profileId,
  rules,
  setRules,
}: {
  profileId?: string
  rules: PolicyRuleResponse[]
  setRules: React.Dispatch<React.SetStateAction<PolicyRuleResponse[]>>
}) {
  const [adding, setAdding] = useState(false)
  const [saving, setSaving] = useState(false)
  const [validationResult, setValidationResult] = useState<{ valid: boolean; error?: string } | null>(null)
  const [newRule, setNewRule] = useState<{
    name: string
    entityName: string
    actions: string
    expression: string
    effect: "deny" | "allow"
    priority: number
  }>({
    name: "",
    entityName: "goods_receipt",
    actions: "update",
    expression: "",
    effect: "deny",
    priority: 10,
  })

  // CEL sandbox state
  const [sandboxOpen, setSandboxOpen] = useState(false)
  const [sandboxEntity, setSandboxEntity] = useState("goods_receipt")
  const [sandboxExpr, setSandboxExpr] = useState("")
  const [sandboxDoc, setSandboxDoc] = useState('{\n  "amount": 50000,\n  "status": "draft",\n  "posted": false\n}')
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
    handleLoadMock(entity)
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

  const handleAddRule = async () => {
    if (!profileId) return
    if (!newRule.name || !newRule.expression) {
      toast.error("Название и выражение обязательны")
      return
    }
    setSaving(true)
    try {
      const created = await api.security.rules.create(profileId, {
        name: newRule.name,
        entityName: newRule.entityName,
        actions: newRule.actions.split(",").map((s) => s.trim()).filter(Boolean),
        expression: newRule.expression,
        effect: newRule.effect,
        priority: newRule.priority,
        enabled: true,
      })
      setRules((prev) => [...prev, created])
      setAdding(false)
      setNewRule({ name: "", entityName: "goods_receipt", actions: "update", expression: "", effect: "deny", priority: 10 })
      setValidationResult(null)
      toast.success("Условие добавлено")
    } catch (e) {
      toast.error(e instanceof ApiError ? (e as ApiError).message : "Ошибка создания")
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
      let doc: Record<string, unknown> = {}
      try {
        doc = JSON.parse(sandboxDoc)
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

  return (
    <div className="max-w-3xl space-y-4">
      <p className="text-xs text-muted-foreground mb-3">
        Программируемые условия для тонкой настройки прав доступа.
        Например: запретить редактирование проведённых документов или ограничить сумму операции.
      </p>

      {/* CEL Sandbox */}
      <div className="rounded-md border">
        <button
          className="w-full flex items-center justify-between px-4 py-2.5 text-xs font-medium hover:bg-muted/40 transition-colors"
          onClick={() => setSandboxOpen(!sandboxOpen)}
        >
          <span>🧪 Проверка выражения</span>
          <ChevronRight className={cn("h-3.5 w-3.5 text-muted-foreground transition-transform", sandboxOpen && "rotate-90")} />
        </button>
        {sandboxOpen && (
          <div className="px-4 pb-4 space-y-3 border-t pt-3">
            {/* Entity selector + auto-mock */}
            <div className="flex items-center gap-2">
              <div className="flex-1">
                <Label className="text-xs text-muted-foreground mb-1">Сущность</Label>
                <Select value={sandboxEntity} onValueChange={handleSandboxEntityChange}>
                  <SelectTrigger className="h-8 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {KNOWN_ENTITIES.map((e) => (
                      <SelectItem key={e.value} value={e.value}>{e.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {sandboxMockLoading && (
                <div className="pt-4">
                  <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                </div>
              )}
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div>
                <Label className="text-xs text-muted-foreground mb-1">CEL-выражение</Label>
                <div className="rounded-md border overflow-hidden">
                  <CelEditor
                    value={sandboxExpr}
                    onChange={(v) => { setSandboxExpr(v); setSandboxResult(null) }}
                    fields={sandboxFields}
                    height={140}
                  />
                </div>
              </div>
              <div>
                <div className="flex items-center justify-between mb-1">
                  <Label className="text-xs text-muted-foreground">Документ (JSON)</Label>
                </div>
                <Textarea
                  value={sandboxDoc}
                  onChange={(e) => setSandboxDoc(e.target.value)}
                  className="text-xs font-mono resize-none"
                  rows={7}
                />
              </div>
            </div>
            <div className="flex items-end gap-3">
              <div className="w-40">
                <Label className="text-xs text-muted-foreground mb-1">Действие</Label>
                <Select value={sandboxAction} onValueChange={setSandboxAction}>
                  <SelectTrigger className="h-8 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="create">create</SelectItem>
                    <SelectItem value="read">read</SelectItem>
                    <SelectItem value="update">update</SelectItem>
                    <SelectItem value="delete">delete</SelectItem>
                    <SelectItem value="post">post</SelectItem>
                    <SelectItem value="unpost">unpost</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <Button
                size="sm"
                className="h-8 text-xs"
                onClick={handleTestExpression}
                disabled={sandboxTesting || !sandboxExpr.trim()}
              >
                {sandboxTesting ? <Loader2 className="mr-1 h-3 w-3 animate-spin" /> : null}
                Проверить
              </Button>
              {sandboxEntity && sandboxFields.length > 0 && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 text-xs ml-auto"
                  onClick={() => setSandboxCheatOpen(!sandboxCheatOpen)}
                >
                  <BookOpen className="mr-1 h-3 w-3" />
                  {sandboxCheatOpen ? "Скрыть поля" : "Доступные поля"}
                </Button>
              )}
            </div>

            {/* Field cheat sheet */}
            {sandboxCheatOpen && sandboxFields.length > 0 && (
              <div className="rounded-md bg-muted/40 p-3">
                <p className="text-[11px] font-medium text-muted-foreground mb-2">Поля документа (doc.*)</p>
                <div className="grid grid-cols-2 sm:grid-cols-3 gap-x-4 gap-y-0.5">
                  {sandboxFields.filter((f) => f.name !== "id" && f.name !== "version" && f.name !== "createdAt" && f.name !== "updatedAt").map((f) => (
                    <div key={f.name} className="flex items-center gap-1.5 text-[11px]">
                      <code className="font-mono text-primary">doc.{f.name}</code>
                      <span className="text-muted-foreground">({f.type})</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {sandboxResult && (
              <div className={cn(
                "rounded-md px-3 py-2 text-xs flex items-center gap-2",
                sandboxResult.error
                  ? "bg-destructive/10 text-destructive"
                  : sandboxResult.result
                    ? "bg-emerald-500/10 text-emerald-700"
                    : "bg-amber-500/10 text-amber-700"
              )}>
                {sandboxResult.error ? (
                  <><AlertTriangle className="h-3.5 w-3.5 shrink-0" /> {sandboxResult.error}</>
                ) : sandboxResult.result ? (
                  <><Check className="h-3.5 w-3.5 shrink-0" /> Результат: true</>
                ) : (
                  <><X className="h-3.5 w-3.5 shrink-0" /> Результат: false</>
                )}
                <span className="ml-auto text-[10px] text-muted-foreground">{sandboxResult.elapsed}</span>
              </div>
            )}
          </div>
        )}
      </div>

      {!profileId && (
        <div className="rounded-md border border-amber-500/30 bg-amber-500/5 p-3 flex items-start gap-2">
          <AlertTriangle className="h-4 w-4 text-amber-600 shrink-0 mt-0.5" />
          <p className="text-xs text-amber-700">
            Сначала сохраните профиль, чтобы добавлять условия.
          </p>
        </div>
      )}

      {/* Existing rules */}
      {rules.map((rule) => (
        <div
          key={rule.id}
          className={cn(
            "rounded-md border p-4 space-y-2",
            !rule.enabled && "opacity-50"
          )}
        >
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Badge variant="outline" className="text-[10px] h-4 min-w-5 justify-center font-mono">
                {rule.priority}
              </Badge>
              <h4 className="text-sm font-medium">{rule.name}</h4>
              <Badge
                variant={rule.effect === "deny" ? "destructive" : "default"}
                className="text-[10px] h-4"
              >
                {rule.effect === "deny" ? "Запретить" : "Разрешить"}
              </Badge>
              <Badge variant="outline" className="text-[10px] h-4 font-mono">
                {rule.entityName}:{rule.actions.join(",")}
              </Badge>
            </div>
            <div className="flex items-center gap-1">
              <Switch
                checked={rule.enabled}
                onCheckedChange={() => handleToggleRule(rule)}
                className="scale-75"
              />
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6"
                onClick={() => handleDeleteRule(rule)}
              >
                <Trash2 className="h-3 w-3 text-muted-foreground" />
              </Button>
            </div>
          </div>
          <p className="text-xs font-mono text-muted-foreground bg-muted/40 rounded px-2 py-1">
            {rule.expression}
          </p>
        </div>
      ))}

      {/* Add rule form */}
      {adding ? (
        <div className="rounded-md border border-primary/30 p-4 space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="text-xs text-muted-foreground mb-1">Название</Label>
              <Input
                value={newRule.name}
                onChange={(e) => setNewRule((r) => ({ ...r, name: e.target.value }))}
                placeholder="Запрет проведения"
                className="h-8 text-xs"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground mb-1">Сущность</Label>
              <Select
                value={newRule.entityName}
                onValueChange={(v) => setNewRule((r) => ({ ...r, entityName: v }))}
              >
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {KNOWN_ENTITIES.map((e) => (
                    <SelectItem key={e.value} value={e.value}>{e.label}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div>
              <Label className="text-xs text-muted-foreground mb-1">Действия</Label>
              <Input
                value={newRule.actions}
                onChange={(e) => setNewRule((r) => ({ ...r, actions: e.target.value }))}
                placeholder="update, delete"
                className="h-8 text-xs font-mono"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground mb-1">Эффект</Label>
              <Select
                value={newRule.effect}
                onValueChange={(v) => setNewRule((r) => ({ ...r, effect: v as "deny" | "allow" }))}
              >
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="deny">Запретить операцию</SelectItem>
                  <SelectItem value="allow">Явно разрешить</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label className="text-xs text-muted-foreground mb-1">Приоритет</Label>
              <Input
                type="number"
                value={newRule.priority}
                onChange={(e) => setNewRule((r) => ({ ...r, priority: parseInt(e.target.value) || 10 }))}
                className="h-8 text-xs font-mono"
              />
            </div>
          </div>

          <div>
            <Label className="text-xs text-muted-foreground mb-1">CEL-выражение</Label>
            <div className="relative">
              <div className="rounded-md border overflow-hidden">
                <CelEditor
                  value={newRule.expression}
                  onChange={(v) => {
                    setNewRule((r) => ({ ...r, expression: v }))
                    setValidationResult(null)
                  }}
                  fields={sandboxFields}
                  placeholder="doc.posted == true"
                  height={56}
                />
              </div>
              {validationResult && (
                <div className="absolute right-2 top-2 z-10">
                  {validationResult.valid ? (
                    <Check className="h-4 w-4 text-emerald-500" />
                  ) : (
                    <AlertTriangle className="h-4 w-4 text-destructive" />
                  )}
                </div>
              )}
            </div>
            {validationResult && !validationResult.valid && validationResult.error && (
              <p className="text-[11px] text-destructive mt-1">{validationResult.error}</p>
            )}
          </div>

          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs"
              onClick={() => {
                setAdding(false)
                setValidationResult(null)
              }}
            >
              Отмена
            </Button>
            <Button
              size="sm"
              className="h-7 text-xs"
              onClick={handleAddRule}
              disabled={saving}
            >
              {saving && <Loader2 className="mr-1 h-3 w-3 animate-spin" />}
              Добавить
            </Button>
          </div>
        </div>
      ) : (
        <Button
          variant="outline"
          size="sm"
          className="h-8 text-xs"
          onClick={() => setAdding(true)}
          disabled={!profileId}
        >
          <Plus className="mr-1.5 h-3 w-3" />
          Добавить условие
        </Button>
      )}

      {rules.length === 0 && !adding && (
        <div className="py-6 text-center text-xs text-muted-foreground">
          Нет дополнительных условий. Основные права определяются ролями.
        </div>
      )}
    </div>
  )
}

// ── Users Section ────────────────────────────────────────────────────────

function UsersSection({ profileId }: { profileId: string }) {
  const [users, setUsers] = useState<{ id: string; email: string; fullName: string }[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    api.security.profiles.listUsers(profileId).then((res) => {
      if (!cancelled) {
        setUsers(res.items ?? [])
        setLoading(false)
      }
    }).catch(() => {
      if (!cancelled) setLoading(false)
    })
    return () => { cancelled = true }
  }, [profileId])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (users.length === 0) {
    return (
      <div className="text-center text-sm text-muted-foreground py-8">
        Нет привязанных пользователей
      </div>
    )
  }

  return (
    <div className="max-w-xl space-y-2">
      {users.map((u) => (
        <div key={u.id} className="rounded-md border px-4 py-2.5 flex items-center gap-3">
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium truncate">{u.fullName || u.email}</p>
            <p className="text-xs text-muted-foreground truncate">{u.email}</p>
          </div>
        </div>
      ))}
    </div>
  )
}

// ── Audit Section ────────────────────────────────────────────────────────

function AuditSection({ profileId }: { profileId: string }) {
  const [entries, setEntries] = useState<AuditEntryResponse[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    api.security.profiles.auditHistory(profileId, 100).then((res) => {
      if (!cancelled) {
        setEntries(res.items ?? [])
        setLoading(false)
      }
    }).catch(() => {
      if (!cancelled) setLoading(false)
    })
    return () => { cancelled = true }
  }, [profileId])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (entries.length === 0) {
    return (
      <div className="text-center text-sm text-muted-foreground py-8">
        Нет записей в журнале изменений
      </div>
    )
  }

  return (
    <div className="max-w-2xl space-y-2">
      {entries.map((entry) => {
        const lines = formatAuditChanges(entry.action, entry.changes as Record<string, unknown> | undefined)
        return (
          <div key={entry.id} className="rounded-md border p-3 space-y-1">
            <div className="flex items-center justify-between">
              <Badge variant={entry.action === "delete" ? "destructive" : "secondary"} className="text-[10px]">
                {getActionLabel(entry.action)}
              </Badge>
              <span className="text-[11px] text-muted-foreground">
                {new Date(entry.createdAt).toLocaleString("ru-RU")}
              </span>
            </div>
            {entry.userEmail && (
              <p className="text-xs text-muted-foreground">{entry.userEmail}</p>
            )}
            {lines.length > 0 && (
              <div className="mt-1 rounded bg-muted/40 p-2 text-[11px] space-y-0.5">
                {lines.map((line, i) => (
                  <div
                    key={i}
                    className={cn(
                      "flex gap-1.5",
                      line.variant === "added" && "text-emerald-700",
                      line.variant === "removed" && "text-red-600",
                      line.variant === "changed" && "text-foreground",
                      line.variant === "neutral" && "text-muted-foreground",
                    )}
                  >
                    <span className="truncate">{line.text}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
