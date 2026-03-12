"use client"

import { useCallback, useEffect, useState } from "react"
import {
  Save,
  Loader2,
  Plus,
  Trash2,
  X,
  AlertTriangle,
  Check,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Separator } from "@/components/ui/separator"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
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
import { FlsFieldPicker } from "@/components/settings/fls-field-picker"
import { toast } from "sonner"
import type {
  SecurityProfileResponse,
  FieldPolicyItem,
  PolicyRuleResponse,
  CreatePolicyRuleRequest,
  AuditEntryResponse,
} from "@/types/security"

// ── Known entities for FLS dropdown ───────────────────────────────────────

const KNOWN_ENTITIES = [
  { value: "goods_receipt", label: "Поступление товаров" },
  { value: "goods_issue", label: "Расход товаров" },
]

const KNOWN_ACTIONS: { value: "read" | "write"; label: string }[] = [
  { value: "read", label: "Чтение" },
  { value: "write", label: "Запись" },
]

// ── Known RLS dimensions ──────────────────────────────────────────────────

const KNOWN_DIMENSIONS = [
  { key: "organization", label: "Организации" },
  { key: "warehouse", label: "Склады" },
  { key: "counterparty", label: "Контрагенты" },
]

// ── Interfaces ────────────────────────────────────────────────────────────

interface SecurityProfileSheetProps {
  open: boolean
  profile: SecurityProfileResponse | null
  presetData?: import("@/components/settings/profile-presets").ProfilePreset | null
  onClose: (saved: boolean) => void
}

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

// ── Main Sheet Component ──────────────────────────────────────────────────

export function SecurityProfileSheet({ open, profile, presetData, onClose }: SecurityProfileSheetProps) {
  const isNew = !profile
  const [form, setForm] = useState<FormState>(EMPTY_FORM)
  const [rules, setRules] = useState<PolicyRuleResponse[]>([])
  const [saving, setSaving] = useState(false)
  const [activeTab, setActiveTab] = useState("general")

  // Load form data from profile
  useEffect(() => {
    if (!open) return
    if (profile) {
      setForm({
        code: profile.code,
        name: profile.name,
        description: profile.description ?? "",
        dimensions: profile.dimensions ?? {},
        fieldPolicies: profile.fieldPolicies ?? [],
      })
      setRules(profile.policyRules ?? [])
    } else if (presetData) {
      setForm({
        code: presetData.code,
        name: presetData.name,
        description: presetData.description,
        dimensions: presetData.dimensions,
        fieldPolicies: presetData.fieldPolicies,
      })
      setRules([])
    } else {
      setForm(EMPTY_FORM)
      setRules([])
    }
    setActiveTab("general")
  }, [open, profile, presetData])

  const handleSave = async () => {
    if (!form.code.trim() || !form.name.trim()) {
      toast.error("Код и название обязательны")
      return
    }

    // Check for duplicate FLS policies (same entity+action)
    const flsKeys = form.fieldPolicies.map((p) => `${p.entityName}:${p.action}`)
    const flsDuplicates = flsKeys.filter((k, i) => flsKeys.indexOf(k) !== i)
    if (flsDuplicates.length > 0) {
      toast.error(`Дубликаты FLS-политик: ${[...new Set(flsDuplicates)].join(", ")}`)
      return
    }

    setSaving(true)
    try {
      if (isNew) {
        await api.security.profiles.create({
          code: form.code,
          name: form.name,
          description: form.description || undefined,
          dimensions: Object.keys(form.dimensions).length > 0 ? form.dimensions : undefined,
          fieldPolicies: form.fieldPolicies.length > 0 ? form.fieldPolicies : undefined,
        })
        toast.success("Профиль создан")
      } else {
        await api.security.profiles.update(profile!.id, {
          code: form.code,
          name: form.name,
          description: form.description || undefined,
          dimensions: form.dimensions,
          fieldPolicies: form.fieldPolicies,
        })
        toast.success("Профиль сохранён")
      }
      onClose(true)
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : "Ошибка сохранения"
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={(o) => !o && onClose(false)}>
      <SheetContent className="w-full sm:max-w-2xl p-0 flex flex-col">
        <SheetHeader className="px-6 py-4 border-b shrink-0">
          <div className="flex items-center justify-between">
            <SheetTitle className="text-base">
              {isNew ? "Новый профиль безопасности" : `Профиль: ${profile?.name}`}
            </SheetTitle>
            <Button size="sm" onClick={handleSave} disabled={saving}>
              {saving ? (
                <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
              ) : (
                <Save className="mr-1.5 h-3.5 w-3.5" />
              )}
              Сохранить
            </Button>
          </div>
        </SheetHeader>

        <Tabs value={activeTab} onValueChange={setActiveTab} className="flex-1 flex flex-col min-h-0">
          <TabsList className="mx-6 mt-3 w-fit shrink-0">
            <TabsTrigger value="general">Основное</TabsTrigger>
            <TabsTrigger value="rls">
              Видимость данных
              {Object.keys(form.dimensions).length > 0 && (
                <Badge variant="secondary" className="ml-1.5 h-4 min-w-4 px-1 text-[10px]">
                  {Object.keys(form.dimensions).length}
                </Badge>
              )}
            </TabsTrigger>
            <TabsTrigger value="fls">
              Видимость полей
              {form.fieldPolicies.length > 0 && (
                <Badge variant="secondary" className="ml-1.5 h-4 min-w-4 px-1 text-[10px]">
                  {form.fieldPolicies.length}
                </Badge>
              )}
            </TabsTrigger>
            <TabsTrigger value="rules">
              Бизнес-правила
              {rules.length > 0 && (
                <Badge variant="secondary" className="ml-1.5 h-4 min-w-4 px-1 text-[10px]">
                  {rules.length}
                </Badge>
              )}
            </TabsTrigger>
            {!isNew && (
              <TabsTrigger value="users">
                Пользователи
              </TabsTrigger>
            )}
            {!isNew && (
              <TabsTrigger value="audit">
                История
              </TabsTrigger>
            )}
          </TabsList>

          <ScrollArea className="flex-1">
            <div className="px-6 py-4">
              <TabsContent value="general" className="mt-0">
                <GeneralTab form={form} setForm={setForm} isSystem={profile?.isSystem ?? false} />
              </TabsContent>
              <TabsContent value="rls" className="mt-0">
                <RlsTab form={form} setForm={setForm} />
              </TabsContent>
              <TabsContent value="fls" className="mt-0">
                <FlsTab form={form} setForm={setForm} />
              </TabsContent>
              <TabsContent value="rules" className="mt-0">
                <RulesTab
                  profileId={profile?.id}
                  rules={rules}
                  setRules={setRules}
                />
              </TabsContent>
              {!isNew && (
                <TabsContent value="users" className="mt-0">
                  <UsersTab profileId={profile!.id} />
                </TabsContent>
              )}
              {!isNew && (
                <TabsContent value="audit" className="mt-0">
                  <AuditTab profileId={profile!.id} />
                </TabsContent>
              )}
            </div>
          </ScrollArea>
        </Tabs>
      </SheetContent>
    </Sheet>
  )
}

// ── General Tab ───────────────────────────────────────────────────────────

function GeneralTab({
  form,
  setForm,
  isSystem,
}: {
  form: FormState
  setForm: React.Dispatch<React.SetStateAction<FormState>>
  isSystem: boolean
}) {
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-4">
        <div>
          <Label className="mb-1.5 text-xs text-muted-foreground">Код</Label>
          <Input
            value={form.code}
            onChange={(e) => setForm((f) => ({ ...f, code: e.target.value }))}
            placeholder="manager_limited"
            className="h-9 text-sm font-mono"
            disabled={isSystem}
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
    </div>
  )
}

// ── RLS Tab ───────────────────────────────────────────────────────────────

function RlsTab({
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
    <div className="space-y-4">
      <div>
        <p className="text-xs text-muted-foreground mb-3">
          Пользователь увидит только данные выбранных организаций, складов и контрагентов.
          Если ограничения не заданы — доступны все записи.
        </p>
      </div>

      {Object.entries(form.dimensions).map(([key, ids]) => {
        const meta = KNOWN_DIMENSIONS.find((d) => d.key === key)
        return (
          <div key={key} className="rounded-md border p-3 space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-xs font-medium">{meta?.label ?? key}</Label>
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
              className="h-7 text-xs"
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
          ✅ Без ограничений — пользователь видит все данные. Добавьте измерение, чтобы ограничить доступ.
        </div>
      )}
    </div>
  )
}

// ── FLS Tab ───────────────────────────────────────────────────────────────

function FlsTab({
  form,
  setForm,
}: {
  form: FormState
  setForm: React.Dispatch<React.SetStateAction<FormState>>
}) {
  const addPolicy = () => {
    setForm((f) => ({
      ...f,
      fieldPolicies: [
        ...f.fieldPolicies,
        {
          entityName: KNOWN_ENTITIES[0]?.value ?? "",
          action: "read",
          allowedFields: ["*"],
          tableParts: {},
        },
      ],
    }))
  }

  const removePolicy = (idx: number) => {
    setForm((f) => ({
      ...f,
      fieldPolicies: f.fieldPolicies.filter((_, i) => i !== idx),
    }))
  }

  const updatePolicy = (idx: number, patch: Partial<FieldPolicyItem>) => {
    setForm((f) => ({
      ...f,
      fieldPolicies: f.fieldPolicies.map((p, i) =>
        i === idx ? { ...p, ...patch } : p
      ),
    }))
  }

  return (
    <div className="space-y-4">
      <div>
        <p className="text-xs text-muted-foreground mb-3">
          Выберите, какие поля документов может видеть пользователь. Скрытые поля будут пустыми.
        </p>
        <details className="text-[11px] text-muted-foreground mb-2">
          <summary className="cursor-pointer hover:text-foreground transition-colors">Синтаксис DSL</summary>
          <div className="mt-1 pl-3 space-y-0.5">
            <div><code className="px-1 bg-muted rounded">*</code> — все поля</div>
            <div><code className="px-1 bg-muted rounded">*, -unit_price</code> — все кроме unit_price</div>
            <div><code className="px-1 bg-muted rounded">quantity, amount</code> — только указанные</div>
          </div>
        </details>
      </div>

      {form.fieldPolicies.map((policy, idx) => (
        <FlsPolicyCard
          key={idx}
          policy={policy}
          onChange={(patch) => updatePolicy(idx, patch)}
          onRemove={() => removePolicy(idx)}
        />
      ))}

      <Button variant="outline" size="sm" className="h-8 text-xs" onClick={addPolicy}>
        <Plus className="mr-1.5 h-3 w-3" />
        Ограничить поля документа
      </Button>

      {form.fieldPolicies.length === 0 && (
        <div className="py-8 text-center text-xs text-muted-foreground">
          ✅ Без ограничений — все поля видимы. Добавьте политику, чтобы скрыть чувствительные данные (цены, суммы).
        </div>
      )}
    </div>
  )
}

// ── FLS Policy Card ───────────────────────────────────────────────────────

function FlsPolicyCard({
  policy,
  onChange,
  onRemove,
}: {
  policy: FieldPolicyItem
  onChange: (patch: Partial<FieldPolicyItem>) => void
  onRemove: () => void
}) {
  return (
    <div className="rounded-md border p-3 space-y-3">
      <div className="flex items-center gap-2">
        <Select
          value={policy.entityName}
          onValueChange={(v) => onChange({ entityName: v })}
        >
          <SelectTrigger className="h-8 text-xs flex-1">
            <SelectValue placeholder="Сущность" />
          </SelectTrigger>
          <SelectContent>
            {KNOWN_ENTITIES.map((e) => (
              <SelectItem key={e.value} value={e.value}>
                {e.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Select
          value={policy.action}
          onValueChange={(v) => onChange({ action: v as "read" | "write" })}
        >
          <SelectTrigger className="h-8 text-xs w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {KNOWN_ACTIONS.map((a) => (
              <SelectItem key={a.value} value={a.value}>
                {a.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" onClick={onRemove}>
          <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
        </Button>
      </div>

      <FlsFieldPicker
        entityName={policy.entityName}
        allowedFields={policy.allowedFields}
        tableParts={policy.tableParts}
        onChangeFields={(fields) => onChange({ allowedFields: fields })}
        onChangeTableParts={(parts) => onChange({ tableParts: parts })}
      />
    </div>
  )
}

// ── Rules Tab ─────────────────────────────────────────────────────────────

function RulesTab({
  profileId,
  rules,
  setRules,
}: {
  profileId: string | undefined
  rules: PolicyRuleResponse[]
  setRules: React.Dispatch<React.SetStateAction<PolicyRuleResponse[]>>
}) {
  const [adding, setAdding] = useState(false)
  const [newRule, setNewRule] = useState<CreatePolicyRuleRequest>({
    name: "",
    entityName: "",
    actions: ["update"],
    expression: "",
    effect: "deny",
  })
  const [validationResult, setValidationResult] = useState<{ valid: boolean; error?: string } | null>(null)
  const [saving, setSaving] = useState(false)

  const validateExpression = async (expr: string) => {
    if (!expr.trim()) {
      setValidationResult(null)
      return
    }
    try {
      const result = await api.security.rules.validate(expr)
      setValidationResult(result)
    } catch {
      setValidationResult({ valid: false, error: "Ошибка валидации" })
    }
  }

  const handleAddRule = async () => {
    if (!profileId) {
      toast.error("Сначала сохраните профиль, затем добавляйте правила")
      return
    }
    if (!newRule.name || !newRule.entityName || !newRule.expression) {
      toast.error("Заполните все обязательные поля")
      return
    }

    setSaving(true)
    try {
      const created = await api.security.rules.create(profileId, newRule)
      setRules((prev) => [...prev, created])
      setAdding(false)
      setNewRule({ name: "", entityName: "", actions: ["update"], expression: "", effect: "deny" })
      setValidationResult(null)
      toast.success("Правило добавлено")
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : "Ошибка создания правила"
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteRule = async (ruleId: string) => {
    if (!profileId) return
    try {
      await api.security.rules.delete(profileId, ruleId)
      setRules((prev) => prev.filter((r) => r.id !== ruleId))
      toast.success("Правило удалено")
    } catch {
      toast.error("Не удалось удалить правило")
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
      toast.error("Не удалось обновить правило")
    }
  }

  // CEL sandbox state
  const [sandboxOpen, setSandboxOpen] = useState(false)
  const [sandboxExpr, setSandboxExpr] = useState("")
  const [sandboxDoc, setSandboxDoc] = useState('{\n  "amount": 50000,\n  "status": "draft",\n  "posted": false\n}')
  const [sandboxAction, setSandboxAction] = useState("update")
  const [sandboxResult, setSandboxResult] = useState<{ result: boolean; error?: string; elapsed: string } | null>(null)
  const [sandboxTesting, setSandboxTesting] = useState(false)

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
    <div className="space-y-4">
      <div>
        <p className="text-xs text-muted-foreground mb-3">
          Дополнительные правила авторизации. Каждое правило проверяется при выполнении операции
          и может разрешить или запретить действие.
        </p>
      </div>

      {/* CEL Sandbox */}
      <div className="rounded-md border">
        <button
          className="w-full flex items-center justify-between px-3 py-2 text-xs font-medium hover:bg-muted/40 transition-colors"
          onClick={() => setSandboxOpen(!sandboxOpen)}
        >
          <span>🧪 Песочница CEL</span>
          <span className="text-muted-foreground text-[10px]">{sandboxOpen ? "Свернуть" : "Развернуть"}</span>
        </button>
        {sandboxOpen && (
          <div className="px-3 pb-3 space-y-2 border-t pt-2">
            <div>
              <Label className="text-[11px] text-muted-foreground mb-1">CEL-выражение</Label>
              <Textarea
                value={sandboxExpr}
                onChange={(e) => { setSandboxExpr(e.target.value); setSandboxResult(null) }}
                placeholder='doc.amount > 10000 && doc.posted == false'
                className="text-xs font-mono resize-none"
                rows={2}
              />
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div>
                <Label className="text-[11px] text-muted-foreground mb-1">Действие</Label>
                <Select value={sandboxAction} onValueChange={setSandboxAction}>
                  <SelectTrigger className="h-7 text-xs">
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
              <div className="flex items-end">
                <Button
                  size="sm"
                  className="h-7 text-xs w-full"
                  onClick={handleTestExpression}
                  disabled={sandboxTesting || !sandboxExpr.trim()}
                >
                  {sandboxTesting ? <Loader2 className="mr-1 h-3 w-3 animate-spin" /> : null}
                  Проверить
                </Button>
              </div>
            </div>
            <div>
              <Label className="text-[11px] text-muted-foreground mb-1">Документ (JSON)</Label>
              <Textarea
                value={sandboxDoc}
                onChange={(e) => setSandboxDoc(e.target.value)}
                className="text-xs font-mono resize-none"
                rows={4}
              />
            </div>
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
            Сначала сохраните профиль, чтобы добавлять CEL-правила.
          </p>
        </div>
      )}

      {/* Existing rules */}
      {rules.map((rule) => (
        <div
          key={rule.id}
          className={cn(
            "rounded-md border p-3 space-y-2",
            !rule.enabled && "opacity-50"
          )}
        >
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Badge variant="outline" className="text-[10px] h-4 min-w-5 justify-center font-mono">
                {rule.priority}
              </Badge>
              <h4 className="text-xs font-medium">{rule.name}</h4>
              <Badge
                variant={rule.effect === "deny" ? "destructive" : "default"}
                className="text-[10px] h-4"
              >
                {rule.effect === "deny" ? "Запрет" : "Разрешение"}
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
                onClick={() => handleDeleteRule(rule.id)}
              >
                <Trash2 className="h-3 w-3 text-muted-foreground" />
              </Button>
            </div>
          </div>
          {rule.description && (
            <p className="text-[11px] text-muted-foreground">{rule.description}</p>
          )}
          <code className="block text-[11px] font-mono bg-muted/50 rounded px-2 py-1 text-foreground">
            {rule.expression}
          </code>
        </div>
      ))}

      {/* Add rule form */}
      {adding ? (
        <div className="rounded-md border border-primary/30 p-3 space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="text-[11px] text-muted-foreground mb-1">Название</Label>
              <Input
                value={newRule.name}
                onChange={(e) => setNewRule((r) => ({ ...r, name: e.target.value }))}
                placeholder="Запрет редактирования проведённых"
                className="h-8 text-xs"
              />
            </div>
            <div>
              <Label className="text-[11px] text-muted-foreground mb-1">Сущность</Label>
              <Select
                value={newRule.entityName}
                onValueChange={(v) => setNewRule((r) => ({ ...r, entityName: v }))}
              >
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue placeholder="Выберите..." />
                </SelectTrigger>
                <SelectContent>
                  {KNOWN_ENTITIES.map((e) => (
                    <SelectItem key={e.value} value={e.value}>
                      {e.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div>
              <Label className="text-[11px] text-muted-foreground mb-1">Действия</Label>
              <Input
                value={newRule.actions.join(", ")}
                onChange={(e) =>
                  setNewRule((r) => ({
                    ...r,
                    actions: e.target.value.split(",").map((s) => s.trim()).filter(Boolean),
                  }))
                }
                placeholder="update, delete"
                className="h-8 text-xs font-mono"
              />
            </div>
            <div>
              <Label className="text-[11px] text-muted-foreground mb-1">Приоритет</Label>
              <Input
                type="number"
                value={newRule.priority ?? 100}
                onChange={(e) =>
                  setNewRule((r) => ({ ...r, priority: parseInt(e.target.value) || 100 }))
                }
                className="h-8 text-xs font-mono"
              />
            </div>
            <div>
              <Label className="text-[11px] text-muted-foreground mb-1">Эффект</Label>
              <Select
                value={newRule.effect}
                onValueChange={(v) =>
                  setNewRule((r) => ({ ...r, effect: v as "deny" | "allow" }))
                }
              >
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="deny">Запрет (deny)</SelectItem>
                  <SelectItem value="allow">Разрешение (allow)</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div>
            <Label className="text-[11px] text-muted-foreground mb-1">CEL-выражение</Label>
            <div className="relative">
              <Textarea
                value={newRule.expression}
                onChange={(e) => {
                  setNewRule((r) => ({ ...r, expression: e.target.value }))
                  setValidationResult(null)
                }}
                onBlur={(e) => validateExpression(e.target.value)}
                placeholder='doc.posted == true'
                className="text-xs font-mono resize-none pr-8"
                rows={2}
              />
              {validationResult && (
                <div className="absolute right-2 top-2">
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
          Добавить бизнес-правило
        </Button>
      )}

      {rules.length === 0 && !adding && (
        <div className="py-6 text-center text-xs text-muted-foreground">
          Основные права определяются ролями. Добавьте правило для дополнительных условий.
        </div>
      )}
    </div>
  )
}

// ── Users Tab ────────────────────────────────────────────────────────────

function UsersTab({ profileId }: { profileId: string }) {
  const [users, setUsers] = useState<{ id: string; email: string; fullName: string }[]>([])
  const [loading, setLoading] = useState(true)
  const [assigning, setAssigning] = useState(false)
  const [searchUsers, setSearchUsers] = useState<{ id: string; email: string; fullName: string }[]>([])
  const [searchQuery, setSearchQuery] = useState("")
  const [searching, setSearching] = useState(false)

  const loadUsers = useCallback(async () => {
    try {
      setLoading(true)
      const res = await api.security.profiles.listUsers(profileId)
      setUsers(res.items ?? [])
    } catch {
      toast.error("Не удалось загрузить пользователей профиля")
    } finally {
      setLoading(false)
    }
  }, [profileId])

  useEffect(() => { loadUsers() }, [loadUsers])

  const handleSearch = useCallback(async (query: string) => {
    setSearchQuery(query)
    if (!query.trim()) {
      setSearchUsers([])
      return
    }
    setSearching(true)
    try {
      const res = await api.users.list(query)
      setSearchUsers(
        (res.items ?? [])
          .filter((u) => !users.some((eu) => eu.id === u.id))
          .map((u) => ({ id: u.id, email: u.email, fullName: u.fullName }))
      )
    } catch {
      setSearchUsers([])
    } finally {
      setSearching(false)
    }
  }, [users])

  const handleAssign = async (userId: string) => {
    try {
      await api.security.profiles.assignUser(profileId, userId)
      toast.success("Пользователь привязан")
      setSearchQuery("")
      setSearchUsers([])
      setAssigning(false)
      loadUsers()
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "Ошибка привязки")
    }
  }

  const handleRemove = async (userId: string) => {
    try {
      await api.security.profiles.removeUser(profileId, userId)
      toast.success("Пользователь отвязан")
      loadUsers()
    } catch {
      toast.error("Не удалось отвязать пользователя")
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground">
        Пользователи, которым назначен этот профиль безопасности.
      </p>

      {users.map((u) => (
        <div key={u.id} className="flex items-center justify-between rounded-md border px-3 py-2">
          <div>
            <p className="text-sm font-medium">{u.fullName}</p>
            <p className="text-xs text-muted-foreground">{u.email}</p>
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6"
            onClick={() => handleRemove(u.id)}
          >
            <X className="h-3 w-3 text-muted-foreground" />
          </Button>
        </div>
      ))}

      {users.length === 0 && !assigning && (
        <div className="py-6 text-center text-xs text-muted-foreground">
          Нет привязанных пользователей
        </div>
      )}

      {assigning ? (
        <div className="rounded-md border p-3 space-y-2">
          <Input
            value={searchQuery}
            onChange={(e) => handleSearch(e.target.value)}
            placeholder="Поиск по email или имени..."
            className="h-8 text-xs"
            autoFocus
          />
          {searching && (
            <div className="flex items-center justify-center py-2">
              <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
            </div>
          )}
          {searchUsers.map((u) => (
            <button
              key={u.id}
              className="w-full flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs hover:bg-muted/40 transition-colors"
              onClick={() => handleAssign(u.id)}
            >
              <Plus className="h-3 w-3 text-muted-foreground shrink-0" />
              <span className="font-medium">{u.fullName}</span>
              <span className="text-muted-foreground ml-auto">{u.email}</span>
            </button>
          ))}
          {searchQuery && !searching && searchUsers.length === 0 && (
            <p className="text-[11px] text-muted-foreground text-center py-1">Не найдено</p>
          )}
          <Button
            variant="ghost"
            size="sm"
            className="h-6 text-[11px] text-muted-foreground"
            onClick={() => { setAssigning(false); setSearchQuery(""); setSearchUsers([]) }}
          >
            Отмена
          </Button>
        </div>
      ) : (
        <Button
          variant="outline"
          size="sm"
          className="h-8 text-xs"
          onClick={() => setAssigning(true)}
        >
          <Plus className="mr-1.5 h-3 w-3" />
          Привязать пользователя
        </Button>
      )}
    </div>
  )
}

// ── Audit Tab ─────────────────────────────────────────────────────────────

const AUDIT_ACTION_LABELS: Record<string, string> = {
  create: "Создание",
  update: "Изменение",
  delete: "Удаление",
}

function AuditTab({ profileId }: { profileId: string }) {
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
    <div className="space-y-2">
      {entries.map((entry) => (
        <div key={entry.id} className="rounded-md border p-3 space-y-1">
          <div className="flex items-center justify-between">
            <Badge variant={entry.action === "delete" ? "destructive" : "secondary"} className="text-[10px]">
              {AUDIT_ACTION_LABELS[entry.action] ?? entry.action}
            </Badge>
            <span className="text-[11px] text-muted-foreground">
              {new Date(entry.createdAt).toLocaleString("ru-RU")}
            </span>
          </div>
          {entry.userEmail && (
            <p className="text-xs text-muted-foreground">{entry.userEmail}</p>
          )}
          {entry.changes && Object.keys(entry.changes).length > 0 && (
            <div className="mt-1 rounded bg-muted/40 p-2 text-[11px] font-mono space-y-0.5">
              {Object.entries(entry.changes).map(([key, val]) => {
                const v = val as { old?: unknown; new?: unknown } | undefined
                if (v && typeof v === "object" && ("old" in v || "new" in v)) {
                  return (
                    <div key={key} className="flex gap-1.5">
                      <span className="text-muted-foreground shrink-0">{key}:</span>
                      <span className="text-red-500 line-through truncate">{String(v.old ?? "—")}</span>
                      <span className="text-green-600 truncate">{String(v.new ?? "—")}</span>
                    </div>
                  )
                }
                return (
                  <div key={key} className="flex gap-1.5">
                    <span className="text-muted-foreground">{key}:</span>
                    <span className="truncate">{String(val)}</span>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      ))}
    </div>
  )
}
