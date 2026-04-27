"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { useTabState, useHasTabCache } from "@/hooks/useTabState"
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
  Pencil,
  ChevronsUpDown,
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
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { cn } from "@/lib/utils"
import { api, ApiError } from "@/lib/api"
import { RlsDimensionPicker } from "@/components/settings/rls-dimension-picker"
import { FlsFieldMatrix } from "@/components/settings/fls-field-picker"
import { CelEditor, preloadMonaco } from "@/components/settings/cel-editor"
import { toast } from "sonner"
import { useTabTitle } from "@/hooks/useTabTitle"
import { getActionLabel, formatAuditChanges, type AuditEventLine } from "@/lib/audit-format"
import { useProfilePresetStore } from "@/stores/useProfilePresetStore"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type {
  SecurityProfileResponse,
  FieldPolicyItem,
  PolicyRuleResponse,
  CreatePolicyRuleRequest,
  AuditEntryResponse,
} from "@/types/security"

// тФАтФА Known actions for FLS dropdowns тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

const KNOWN_ACTIONS: { value: "read" | "write"; label: string }[] = [
  { value: "read", label: "╨Я╤А╨╛╤Б╨╝╨╛╤В╤А ╨┐╨╛╨╗╤П" },
  { value: "write", label: "╨а╨╡╨┤╨░╨║╤В╨╕╤А╨╛╨▓╨░╨╜╨╕╨╡ ╨┐╨╛╨╗╤П" },
]

// тФАтФА Known actions for CEL rule toggles тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

const RULE_ACTIONS: { value: string; label: string }[] = [
  { value: "create", label: "╨б╨╛╨╖╨┤╨░╨╜╨╕╨╡" },
  { value: "read", label: "╨з╤В╨╡╨╜╨╕╨╡" },
  { value: "update", label: "╨Ш╨╖╨╝╨╡╨╜╨╡╨╜╨╕╨╡" },
  { value: "delete", label: "╨г╨┤╨░╨╗╨╡╨╜╨╕╨╡" },
  { value: "post", label: "╨Я╤А╨╛╨▓╨╡╨┤╨╡╨╜╨╕╨╡" },
  { value: "unpost", label: "╨Ю╤В╨╝╨╡╨╜╨░ ╨┐╤А╨╛╨▓╨╡╨┤╨╡╨╜╨╕╤П" },
]

// тФАтФА RLS dimensions resolved from metadata store inside RlsSection тФАтФАтФАтФАтФАтФАтФАтФАтФА

const RLS_DIMENSION_KEYS = ["organization", "warehouse", "counterparty"] as const


// тФАтФА Form state тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

interface FormState {
  code: string
  name: string
  description: string
  dimensions: Record<string, string[]>
  entityDimensions: Record<string, Record<string, string[]>>
  fieldPolicies: FieldPolicyItem[]
}

const EMPTY_FORM: FormState = {
  code: "",
  name: "",
  description: "",
  dimensions: {},
  entityDimensions: {},
  fieldPolicies: [],
}

// тФАтФА Role-Profile Affinity Hint тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

interface AffinityRule {
  roleHint: string
  label: string
  match: (form: FormState) => boolean
}

const AFFINITY_RULES: AffinityRule[] = [
  {
    roleHint: "viewer",
    label: "╨в╨╛╨╗╤М╨║╨╛ ╨┐╤А╨╛╤Б╨╝╨╛╤В╤А",
    match: (f) =>
      f.fieldPolicies.some((p) => p.action === "read" && p.allowedFields.some((a) => a.startsWith("-"))),
  },
  {
    roleHint: "manager",
    label: "╨Ь╨╡╨╜╨╡╨┤╨╢╨╡╤А",
    match: (f) =>
      Object.keys(f.dimensions).includes("organization") && f.fieldPolicies.length === 0,
  },
  {
    roleHint: "accountant",
    label: "╨С╤Г╤Е╨│╨░╨╗╤В╨╡╤А",
    match: (f) =>
      Object.keys(f.dimensions).includes("warehouse") && f.fieldPolicies.length === 0,
  },
  {
    roleHint: "warehouse",
    label: "╨Ъ╨╗╨░╨┤╨╛╨▓╤Й╨╕╨║",
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
    <div className="rounded-md border border-border bg-muted/50 px-3 py-2.5">
      <p className="text-[11px] text-foreground font-medium mb-1">╨а╨╡╨║╨╛╨╝╨╡╨╜╨┤╤Г╨╡╨╝╤Л╨╡ ╤А╨╛╨╗╨╕ ╨┤╨╗╤П ╤Н╤В╨╛╨│╨╛ ╨┐╤А╨╛╤Д╨╕╨╗╤П</p>
      <div className="flex flex-wrap gap-1.5">
        {matched.map((m) => (
          <Badge key={m.roleHint} variant="outline" className="text-[10px] h-5 bg-background border-border text-foreground">
            {m.label}
            <span className="ml-1 text-muted-foreground font-mono">{m.roleHint}</span>
          </Badge>
        ))}
      </div>
      <p className="text-[10px] text-muted-foreground mt-1.5">
        ╨Я╨╛╨┤╤Б╨║╨░╨╖╨║╨░ ╨╜╨░ ╨╛╤Б╨╜╨╛╨▓╨╡ ╨╜╨░╤Б╤В╤А╨╛╨╡╨╜╨╜╤Л╤Е ╨╛╨│╤А╨░╨╜╨╕╤З╨╡╨╜╨╕╨╣ (RLS/FLS). ╨Э╨░╨╖╨╜╨░╤З╤М╤В╨╡ ╤А╨╛╨╗╤М ╨┐╨╛╨╗╤М╨╖╨╛╨▓╨░╤В╨╡╨╗╤О ╨╜╨░ ╨▓╨║╨╗╨░╨┤╨║╨╡ ┬л╨Я╨╛╨╗╤М╨╖╨╛╨▓╨░╤В╨╡╨╗╨╕ ╨╕ ╤А╨╛╨╗╨╕┬╗.
      </p>
    </div>
  )
}

// тФАтФА Main Page тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

export default function SecurityProfilePage() {
  const params = useParams()
  const router = useRouter()
  const profileId = params.id as string
  const isNew = profileId === "new"

  const [profile, setProfile] = useTabState<SecurityProfileResponse | null>("profile", null)
  const [form, setForm] = useTabState<FormState>("form", EMPTY_FORM)
  const [rules, setRules] = useTabState<PolicyRuleResponse[]>("rules", [])
  const hasCachedForm = useHasTabCache("form")
  const [loading, setLoading] = useState(!isNew && !hasCachedForm)
  const [saving, setSaving] = useState(false)
  const [activeTab, setActiveTab] = useTabState("activeTab", "general")

  useTabTitle(
    profile ? profile.name : isNew ? "╨Э╨╛╨▓╤Л╨╣ ╨┐╤А╨╛╤Д╨╕╨╗╤М" : undefined,
    "╨Я╤А╨╛╤Д╨╕╨╗╤М ╨▒╨╡╨╖╨╛╨┐╨░╤Б╨╜╨╛╤Б╤В╨╕"
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
      entityDimensions: preset.entityDimensions ?? {},
      fieldPolicies: preset.fieldPolicies,
    })
    useProfilePresetStore.getState().clear()
  }, [isNew, setForm])

  // Load profile data тАФ skip if returning to a cached tab
  useEffect(() => {
    if (isNew || hasCachedForm) return
    let cancelled = false
    api.security.profiles.get(profileId).then((data) => {
      if (cancelled) return
      setProfile(data)
      setForm({
        code: data.code,
        name: data.name,
        description: data.description ?? "",
        dimensions: data.dimensions ?? {},
        entityDimensions: data.entityDimensions ?? {},
        fieldPolicies: data.fieldPolicies ?? [],
      })
      setRules(data.policyRules ?? [])
      setLoading(false)
    }).catch(() => {
      if (!cancelled) {
        toast.error("╨Э╨╡ ╤Г╨┤╨░╨╗╨╛╤Б╤М ╨╖╨░╨│╤А╤Г╨╖╨╕╤В╤М ╨┐╤А╨╛╤Д╨╕╨╗╤М")
        setLoading(false)
      }
    })
    return () => { cancelled = true }
  }, [profileId, isNew, hasCachedForm, setProfile, setForm, setRules])

  const handleSave = useCallback(async () => {
    if (!form.code || !form.name) {
      toast.error("╨Ъ╨╛╨┤ ╨╕ ╨╜╨░╨╖╨▓╨░╨╜╨╕╨╡ ╨╛╨▒╤П╨╖╨░╤В╨╡╨╗╤М╨╜╤Л")
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
          entityDimensions: Object.keys(form.entityDimensions).length > 0 ? form.entityDimensions : undefined,
          fieldPolicies: form.fieldPolicies,
        })
        toast.success("╨Я╤А╨╛╤Д╨╕╨╗╤М ╤Б╨╛╨╖╨┤╨░╨╜")
        router.replace(`/admin/security-profiles/${created.id}`)
      } else {
        const updated = await api.security.profiles.update(profileId, {
          code: form.code,
          name: form.name,
          description: form.description || undefined,
          dimensions: form.dimensions,
          entityDimensions: Object.keys(form.entityDimensions).length > 0 ? form.entityDimensions : undefined,
          fieldPolicies: form.fieldPolicies,
        })
        setProfile(updated)
        toast.success("╨Я╤А╨╛╤Д╨╕╨╗╤М ╤Б╨╛╤Е╤А╨░╨╜╤С╨╜")
      }
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "╨Э╨╡ ╤Г╨┤╨░╨╗╨╛╤Б╤М ╤Б╨╛╤Е╤А╨░╨╜╨╕╤В╤М ╨┐╤А╨╛╤Д╨╕╨╗╤М")
    } finally {
      setSaving(false)
    }
  }, [form, isNew, profileId, router, setProfile])

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
                  {isNew ? "╨Э╨╛╨▓╤Л╨╣ ╨┐╤А╨╛╤Д╨╕╨╗╤М ╨▒╨╡╨╖╨╛╨┐╨░╤Б╨╜╨╛╤Б╤В╨╕" : profile?.name}
                </h1>
                {profile?.isSystem && (
                  <Badge variant="secondary" className="text-[10px]">╨б╨╕╤Б╤В╨╡╨╝╨╜╤Л╨╣</Badge>
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
            ╨б╨╛╤Е╤А╨░╨╜╨╕╤В╤М
          </Button>
        </div>
      </div>

      {/* Content */}
      <ScrollArea className="flex-1">
        <div className="max-w-5xl mx-auto px-6 py-6">
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="w-fit mb-6">
              <TabsTrigger value="general">╨Ю╤Б╨╜╨╛╨▓╨╜╨╛╨╡</TabsTrigger>
              <TabsTrigger value="rls">
                ╨Ф╨╛╤Б╤В╤Г╨┐ ╨║ ╨┤╨░╨╜╨╜╤Л╨╝ (RLS)
                {(Object.keys(form.dimensions).length + Object.keys(form.entityDimensions).length) > 0 && (
                  <span className="ml-1.5 inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-foreground/10 px-1 text-[10px] font-medium text-foreground">
                    {Object.keys(form.dimensions).length + Object.keys(form.entityDimensions).length}
                  </span>
                )}
              </TabsTrigger>
              <TabsTrigger value="fls">
                ╨Ф╨╛╤Б╤В╤Г╨┐╨╜╨╛╤Б╤В╤М ╨┐╨╛╨╗╨╡╨╣ (FLS)
                {form.fieldPolicies.length > 0 && (
                  <span className="ml-1.5 inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-foreground/10 px-1 text-[10px] font-medium text-foreground">
                    {form.fieldPolicies.length}
                  </span>
                )}
              </TabsTrigger>
              <TabsTrigger value="rules">
                ╨С╨╕╨╖╨╜╨╡╤Б ╨┐╤А╨░╨▓╨╕╨╗╨░ (CEL)
                {rules.filter((r) => r.enabled).length > 0 && (
                  <span className="ml-1.5 inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-foreground/10 px-1 text-[10px] font-medium text-foreground">
                    {rules.filter((r) => r.enabled).length}
                  </span>
                )}
              </TabsTrigger>
              {!isNew && (
                <TabsTrigger value="users">
                  ╨Я╨╛╨╗╤М╨╖╨╛╨▓╨░╤В╨╡╨╗╨╕
                  {(profile?.userCount ?? 0) > 0 && (
                    <span className="ml-1.5 inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-foreground/10 px-1 text-[10px] font-medium text-foreground">
                      {profile!.userCount}
                    </span>
                  )}
                </TabsTrigger>
              )}
              {!isNew && <TabsTrigger value="audit">╨Ш╤Б╤В╨╛╤А╨╕╤П</TabsTrigger>}
            </TabsList>

            <TabsContent value="general" className="mt-0">
              <div className="max-w-xl space-y-4">
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <Label className="text-xs text-muted-foreground">╨Ъ╨╛╨┤</Label>
                    <Input
                      value={form.code}
                      onChange={(e) => setForm((f) => ({ ...f, code: e.target.value }))}
                      placeholder="manager_limited"
                      className="mt-1 h-9 text-sm font-mono"
                      disabled={profile?.isSystem}
                    />
                  </div>
                  <div>
                    <Label className="text-xs text-muted-foreground">╨Э╨░╨╖╨▓╨░╨╜╨╕╨╡</Label>
                    <Input
                      value={form.name}
                      onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                      placeholder="╨Ь╨╡╨╜╨╡╨┤╨╢╨╡╤А (╨╛╨│╤А╨░╨╜╨╕╤З╨╡╨╜╨╜╤Л╨╣)"
                      className="mt-1 h-9 text-sm"
                    />
                  </div>
                </div>
                <div>
                  <Label className="text-xs text-muted-foreground">╨Ю╨┐╨╕╤Б╨░╨╜╨╕╨╡</Label>
                  <Textarea
                    value={form.description}
                    onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
                    placeholder="╨Ю╨┐╨╕╤Б╨░╨╜╨╕╨╡ ╨┐╤А╨╛╤Д╨╕╨╗╤П ╨▒╨╡╨╖╨╛╨┐╨░╤Б╨╜╨╛╤Б╤В╨╕..."
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
      </ScrollArea>
    </div>
  )
}

// тФАтФА RLS Section тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

function RlsSection({
  form,
  setForm,
}: {
  form: FormState
  setForm: React.Dispatch<React.SetStateAction<FormState>>
}) {
  const getLabel = useMetadataStore((s) => s.getLabel)
  const metaEntities = useMetadataStore((s) => s.entities)

  const KNOWN_DIMENSIONS = useMemo(
    () => RLS_DIMENSION_KEYS.map((key) => ({ key, label: getLabel(key, "plural") })),
    [getLabel]
  )

  // Build entity options for per-entity overrides
  const { entityCatalogs, entityDocuments, allEntityMap } = useMemo(() => {
    const cats: { value: string; label: string }[] = []
    const docs: { value: string; label: string }[] = []
    const labelMap: Record<string, string> = {}
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
    return { entityCatalogs: cats, entityDocuments: docs, allEntityMap: labelMap }
  }, [metaEntities])

  // тФАтФА Global dimensions тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

  const addGlobalDimension = (key: string) => {
    setForm((f) => ({
      ...f,
      dimensions: { ...f.dimensions, [key]: [] },
    }))
  }

  const removeGlobalDimension = (key: string) => {
    setForm((f) => {
      const dims = { ...f.dimensions }
      delete dims[key]
      return { ...f, dimensions: dims }
    })
  }

  const updateGlobalDimensionIds = (key: string, ids: string[]) => {
    setForm((f) => ({
      ...f,
      dimensions: { ...f.dimensions, [key]: ids },
    }))
  }

  const availableGlobalDimensions = KNOWN_DIMENSIONS.filter(
    (d) => !(d.key in form.dimensions)
  )

  // тФАтФА Per-entity dimensions тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

  const entityDimKeys = Object.keys(form.entityDimensions)

  const addEntityOverride = (entityKey: string) => {
    setForm((f) => ({
      ...f,
      entityDimensions: { ...f.entityDimensions, [entityKey]: {} },
    }))
  }

  const removeEntityOverride = (entityKey: string) => {
    setForm((f) => {
      const ed = { ...f.entityDimensions }
      delete ed[entityKey]
      return { ...f, entityDimensions: ed }
    })
  }

  const addEntityDimension = (entityKey: string, dimKey: string) => {
    setForm((f) => ({
      ...f,
      entityDimensions: {
        ...f.entityDimensions,
        [entityKey]: { ...f.entityDimensions[entityKey], [dimKey]: [] },
      },
    }))
  }

  const removeEntityDimension = (entityKey: string, dimKey: string) => {
    setForm((f) => {
      const dims = { ...f.entityDimensions[entityKey] }
      delete dims[dimKey]
      const ed = { ...f.entityDimensions }
      if (Object.keys(dims).length === 0) {
        delete ed[entityKey]
      } else {
        ed[entityKey] = dims
      }
      return { ...f, entityDimensions: ed }
    })
  }

  const updateEntityDimensionIds = (entityKey: string, dimKey: string, ids: string[]) => {
    setForm((f) => ({
      ...f,
      entityDimensions: {
        ...f.entityDimensions,
        [entityKey]: { ...f.entityDimensions[entityKey], [dimKey]: ids },
      },
    }))
  }

  const availableEntitiesForOverride = [...entityDocuments, ...entityCatalogs].filter(
    (e) => !entityDimKeys.includes(e.value)
  )

  const hasAny = Object.keys(form.dimensions).length > 0 || entityDimKeys.length > 0

  return (
    <div className="max-w-3xl space-y-6">
      <p className="text-xs text-muted-foreground">
        ╨Ю╨│╤А╨░╨╜╨╕╤З╤М╤В╨╡ ╨┤╨╛╤Б╤В╤Г╨┐ ╨┐╨╛ ╨╛╤А╨│╨░╨╜╨╕╨╖╨░╤Ж╨╕╤П╨╝, ╤Б╨║╨╗╨░╨┤╨░╨╝ ╨╕╨╗╨╕ ╨║╨╛╨╜╤В╤А╨░╨│╨╡╨╜╤В╨░╨╝.
        ╨Я╨╛╨╗╤М╨╖╨╛╨▓╨░╤В╨╡╨╗╤М ╨▓╨╕╨┤╨╕╤В ╤В╨╛╨╗╤М╨║╨╛ ╨┤╨╛╨║╤Г╨╝╨╡╨╜╤В╤Л, ╨╛╤В╨╜╨╛╤Б╤П╤Й╨╕╨╡╤Б╤П ╨║ ╨▓╤Л╨▒╤А╨░╨╜╨╜╤Л╨╝ ╨╖╨╜╨░╤З╨╡╨╜╨╕╤П╨╝.
        ╨Х╤Б╨╗╨╕ ╨╛╨│╤А╨░╨╜╨╕╤З╨╡╨╜╨╕╤П ╨╜╨╡ ╨╖╨░╨┤╨░╨╜╤Л тАФ ╨┤╨╛╤Б╤В╤Г╨┐╨╜╤Л ╨▓╤Б╨╡ ╨╖╨░╨┐╨╕╤Б╨╕.
      </p>

      {/* тФАтФА Section 1: Global dimensions тФАтФА */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-medium">╨Ю╨▒╤Й╨╕╨╡ ╨╛╨│╤А╨░╨╜╨╕╤З╨╡╨╜╨╕╤П</h3>
          <span className="text-[11px] text-muted-foreground">тАФ ╨┐╤А╨╕╨╝╨╡╨╜╤П╤О╤В╤Б╤П ╨║╨╛ ╨▓╤Б╨╡╨╝ ╤Б╤Г╤Й╨╜╨╛╤Б╤В╤П╨╝</span>
        </div>

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
                  onClick={() => removeGlobalDimension(key)}
                >
                  <X className="h-3 w-3" />
                </Button>
              </div>
              <RlsDimensionPicker
                dimensionKey={key}
                selectedIds={ids}
                onChange={(newIds) => updateGlobalDimensionIds(key, newIds)}
              />
            </div>
          )
        })}

        {availableGlobalDimensions.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {availableGlobalDimensions.map((d) => (
              <Button
                key={d.key}
                variant="outline"
                size="sm"
                className="h-8 text-xs"
                onClick={() => addGlobalDimension(d.key)}
              >
                <Plus className="mr-1 h-3 w-3" />
                {d.label}
              </Button>
            ))}
          </div>
        )}

        {Object.keys(form.dimensions).length === 0 && (
          <div className="rounded-md border border-dashed p-4 text-center text-xs text-muted-foreground">
            ╨С╨╡╨╖ ╨╛╨▒╤Й╨╕╤Е ╨╛╨│╤А╨░╨╜╨╕╤З╨╡╨╜╨╕╨╣. ╨Ф╨╛╨▒╨░╨▓╤М╤В╨╡ ╨╕╨╖╨╝╨╡╤А╨╡╨╜╨╕╨╡, ╤З╤В╨╛╨▒╤Л ╨╛╨│╤А╨░╨╜╨╕╤З╨╕╤В╤М ╨┤╨╛╤Б╤В╤Г╨┐ ╨┤╨╗╤П ╨▓╤Б╨╡╤Е ╤Б╤Г╤Й╨╜╨╛╤Б╤В╨╡╨╣.
          </div>
        )}
      </div>

      <Separator />

      {/* тФАтФА Section 2: Per-entity overrides тФАтФА */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-medium">╨Ю╨│╤А╨░╨╜╨╕╤З╨╡╨╜╨╕╤П ╨┐╨╛ ╤Б╤Г╤Й╨╜╨╛╤Б╤В╤П╨╝</h3>
          <span className="text-[11px] text-muted-foreground">тАФ ╨┐╨╡╤А╨╡╨╛╨┐╤А╨╡╨┤╨╡╨╗╤П╤О╤В ╨╛╨▒╤Й╨╕╨╡ ╨┤╨╗╤П ╨║╨╛╨╜╨║╤А╨╡╤В╨╜╨╛╨╣ ╤Б╤Г╤Й╨╜╨╛╤Б╤В╨╕</span>
        </div>

        {entityDimKeys.map((entityKey) => {
          const entityLabel = allEntityMap[entityKey] ?? entityKey
          const entityDims = form.entityDimensions[entityKey] ?? {}
          const availableDimsForEntity = KNOWN_DIMENSIONS.filter(
            (d) => !(d.key in entityDims)
          )

          return (
            <div key={entityKey} className="rounded-md border">
              <div className="flex items-center justify-between px-4 py-2.5 bg-muted/30">
                <div className="flex items-center gap-2">
                  <h4 className="text-sm font-medium">{entityLabel}</h4>
                  <Badge variant="outline" className="text-[10px] h-4 font-mono">
                    {entityKey}
                  </Badge>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-6 w-6"
                  onClick={() => removeEntityOverride(entityKey)}
                >
                  <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
                </Button>
              </div>
              <div className="px-4 pb-4 pt-3 space-y-3">
                {Object.entries(entityDims).map(([dimKey, ids]) => {
                  const dimMeta = KNOWN_DIMENSIONS.find((d) => d.key === dimKey)
                  return (
                    <div key={dimKey} className="space-y-2">
                      <div className="flex items-center justify-between">
                        <Label className="text-xs font-medium text-muted-foreground">{dimMeta?.label ?? dimKey}</Label>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-5 w-5"
                          onClick={() => removeEntityDimension(entityKey, dimKey)}
                        >
                          <X className="h-2.5 w-2.5" />
                        </Button>
                      </div>
                      <RlsDimensionPicker
                        dimensionKey={dimKey}
                        selectedIds={ids}
                        onChange={(newIds) => updateEntityDimensionIds(entityKey, dimKey, newIds)}
                      />
                    </div>
                  )
                })}

                {availableDimsForEntity.length > 0 && (
                  <div className="flex flex-wrap gap-1.5">
                    {availableDimsForEntity.map((d) => (
                      <Button
                        key={d.key}
                        variant="outline"
                        size="sm"
                        className="h-7 text-[11px]"
                        onClick={() => addEntityDimension(entityKey, d.key)}
                      >
                        <Plus className="mr-1 h-2.5 w-2.5" />
                        {d.label}
                      </Button>
                    ))}
                  </div>
                )}

                {Object.keys(entityDims).length === 0 && (
                  <p className="text-[11px] text-muted-foreground">
                    ╨Ф╨╛╨▒╨░╨▓╤М╤В╨╡ ╨╕╨╖╨╝╨╡╤А╨╡╨╜╨╕╨╡ ╨┤╨╗╤П ╤Н╤В╨╛╨╣ ╤Б╤Г╤Й╨╜╨╛╤Б╤В╨╕.
                  </p>
                )}
              </div>
            </div>
          )
        })}

        {/* Add entity override */}
        {availableEntitiesForOverride.length > 0 && (
          <EntityOverrideAdder
            documents={entityDocuments.filter((e) => !entityDimKeys.includes(e.value))}
            catalogs={entityCatalogs.filter((e) => !entityDimKeys.includes(e.value))}
            onAdd={addEntityOverride}
          />
        )}

        {entityDimKeys.length === 0 && (
          <div className="rounded-md border border-dashed p-4 text-center text-xs text-muted-foreground">
            ╨Э╨╡╤В ╨╕╨╜╨┤╨╕╨▓╨╕╨┤╤Г╨░╨╗╤М╨╜╤Л╤Е ╨╛╨│╤А╨░╨╜╨╕╤З╨╡╨╜╨╕╨╣. ╨Ф╨╛╨▒╨░╨▓╤М╤В╨╡ ╤Б╤Г╤Й╨╜╨╛╤Б╤В╤М, ╤З╤В╨╛╨▒╤Л ╨┐╨╡╤А╨╡╨╛╨┐╤А╨╡╨┤╨╡╨╗╨╕╤В╤М ╨╛╨▒╤Й╨╕╨╡ ╨┐╤А╨░╨▓╨╕╨╗╨░ ╨┤╨╗╤П ╨╜╨╡╤С.
          </div>
        )}
      </div>

      {!hasAny && (
        <div className="rounded-md border border-blue-200 bg-blue-50/50 px-3 py-2.5">
          <p className="text-[11px] text-blue-700">
            <strong>Fail-open:</strong> ╨▒╨╡╨╖ ╨╛╨│╤А╨░╨╜╨╕╤З╨╡╨╜╨╕╨╣ ╨┐╨╛╨╗╤М╨╖╨╛╨▓╨░╤В╨╡╨╗╤М ╨╕╨╝╨╡╨╡╤В ╨┐╨╛╨╗╨╜╤Л╨╣ ╨┤╨╛╤Б╤В╤Г╨┐ ╨║╨╛ ╨▓╤Б╨╡╨╝ ╨┤╨░╨╜╨╜╤Л╨╝.
            ╨Ф╨╛╨▒╨░╨▓╤М╤В╨╡ ╨╕╨╖╨╝╨╡╤А╨╡╨╜╨╕╨╡, ╤З╤В╨╛╨▒╤Л ╨╛╨│╤А╨░╨╜╨╕╤З╨╕╤В╤М ╨▓╨╕╨┤╨╕╨╝╨╛╤Б╤В╤М.
          </p>
        </div>
      )}
    </div>
  )
}

// тФАтФА Entity override adder (combobox) тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

function EntityOverrideAdder({
  documents,
  catalogs,
  onAdd,
}: {
  documents: { value: string; label: string }[]
  catalogs: { value: string; label: string }[]
  onAdd: (entityKey: string) => void
}) {
  const [open, setOpen] = useState(false)

  if (documents.length === 0 && catalogs.length === 0) return null

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm" className="h-8 text-xs">
          <Plus className="mr-1.5 h-3 w-3" />
          ╨Ф╨╛╨▒╨░╨▓╨╕╤В╤М ╤Б╤Г╤Й╨╜╨╛╤Б╤В╤М
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[280px] p-0" align="start">
        <Command>
          <CommandInput placeholder="╨Я╨╛╨╕╤Б╨║тАж" className="h-8 text-xs" />
          <CommandList>
            <CommandEmpty className="py-4 text-center text-xs text-muted-foreground">╨Э╨╡ ╨╜╨░╨╣╨┤╨╡╨╜╨╛</CommandEmpty>
            {documents.length > 0 && (
              <CommandGroup heading="╨Ф╨╛╨║╤Г╨╝╨╡╨╜╤В╤Л">
                {documents.map((e) => (
                  <CommandItem
                    key={e.value}
                    value={e.label}
                    onSelect={() => { onAdd(e.value); setOpen(false) }}
                    className="text-xs"
                  >
                    {e.label}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {catalogs.length > 0 && (
              <CommandGroup heading="╨б╨┐╤А╨░╨▓╨╛╤З╨╜╨╕╨║╨╕">
                {catalogs.map((e) => (
                  <CommandItem
                    key={e.value}
                    value={e.label}
                    onSelect={() => { onAdd(e.value); setOpen(false) }}
                    className="text-xs"
                  >
                    {e.label}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

// тФАтФА FLS Section тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

function FlsSection({
  form,
  setForm,
}: {
  form: FormState
  setForm: React.Dispatch<React.SetStateAction<FormState>>
}) {
  const metaEntities = useMetadataStore((s) => s.entities)

  // Build grouped entity options using e.key (snake_case) тАФ matches DB format
  // Sorted alphabetically within each group
  const { catalogs, documents } = useMemo(() => {
    const cats: { value: string; label: string }[] = []
    const docs: { value: string; label: string }[] = []
    for (const e of metaEntities) {
      const item = { value: e.key, label: e.presentation.singular ?? e.name }
      if (e.type === "catalog") cats.push(item)
      else docs.push(item)
    }
    const byLabel = (a: { label: string }, b: { label: string }) => a.label.localeCompare(b.label, "ru")
    cats.sort(byLabel)
    docs.sort(byLabel)
    return { catalogs: cats, documents: docs }
  }, [metaEntities])

  const allEntities = useMemo(() => [...catalogs, ...documents], [catalogs, documents])

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
      const rest = f.fieldPolicies.filter((p) => p.entityName !== entity)
      const next = [...rest]
      if (readPolicy) next.push(readPolicy)
      if (writePolicy) next.push(writePolicy)
      return { ...f, fieldPolicies: next }
    })
  }

  const addEntity = (entityValue: string) => {
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

  const availableCatalogs = catalogs.filter((e) => !entityNames.includes(e.value))
  const availableDocuments = documents.filter((e) => !entityNames.includes(e.value))

  return (
    <div className="max-w-3xl space-y-4">
      <p className="text-xs text-muted-foreground mb-3">
        ╨б╨║╤А╨╛╨╣╤В╨╡ ╤З╤Г╨▓╤Б╤В╨▓╨╕╤В╨╡╨╗╤М╨╜╤Л╨╡ ╨┐╨╛╨╗╤П (╤Ж╨╡╨╜╤Л, ╤Б╤Г╨╝╨╝╤Л) ╨╕╨╗╨╕ ╨╖╨░╨┐╤А╨╡╤В╨╕╤В╨╡ ╨╕╤Е ╤А╨╡╨┤╨░╨║╤В╨╕╤А╨╛╨▓╨░╨╜╨╕╨╡.
        ╨б╨╜╨╕╨╝╨╕╤В╨╡ ╨│╨░╨╗╨╛╤З╨║╤Г ╤Б ╨┐╨╛╨╗╤П, ╨║╨╛╤В╨╛╤А╨╛╨╡ ╨╜╤Г╨╢╨╜╨╛ ╤Б╨║╤А╤Л╤В╤М. ╨С╨╡╨╖ ╨╛╨│╤А╨░╨╜╨╕╤З╨╡╨╜╨╕╨╣ тАФ ╨▓╤Б╨╡ ╨┐╨╛╨╗╤П ╨▓╨╕╨┤╨╕╨╝╤Л.
      </p>

      {entityNames.map((entity) => {
        const label = allEntities.find((e) => e.value === entity)?.label ?? entity
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

      {/* Grouped "Add entity" buttons */}
      {(availableDocuments.length > 0 || availableCatalogs.length > 0) && (
        <div className="space-y-3">
          {availableDocuments.length > 0 && (
            <div>
              <p className="text-[11px] font-medium text-muted-foreground mb-1.5">╨Ф╨╛╨║╤Г╨╝╨╡╨╜╤В╤Л</p>
              <div className="flex flex-wrap gap-2">
                {availableDocuments.map((e) => (
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
            </div>
          )}
          {availableCatalogs.length > 0 && (
            <div>
              <p className="text-[11px] font-medium text-muted-foreground mb-1.5">╨б╨┐╤А╨░╨▓╨╛╤З╨╜╨╕╨║╨╕</p>
              <div className="flex flex-wrap gap-2">
                {availableCatalogs.map((e) => (
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
            </div>
          )}
        </div>
      )}

      {entityNames.length === 0 && (
        <div className="py-8 text-center text-xs text-muted-foreground">
          ╨Т╤Б╨╡ ╨┐╨╛╨╗╤П ╨┤╨╛╤Б╤В╤Г╨┐╨╜╤Л. ╨Ф╨╛╨▒╨░╨▓╤М╤В╨╡ ╤Б╤Г╤Й╨╜╨╛╤Б╤В╤М, ╤З╤В╨╛╨▒╤Л ╨╜╨░╤Б╤В╤А╨╛╨╕╤В╤М ╤Б╨║╤А╤Л╤В╨╕╨╡ ╨┐╨╛╨╗╨╡╨╣.
        </div>
      )}
    </div>
  )
}

// тФАтФА Entity Combobox (searchable) тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

function EntityCombobox({
  value,
  onChange,
  documents,
  catalogs,
  placeholder = "╨Т╤Л╨▒╨╡╤А╨╕╤В╨╡ ╤Б╤Г╤Й╨╜╨╛╤Б╤В╤МтАж",
  withWildcard = false,
  className,
}: {
  value: string
  onChange: (v: string) => void
  documents: { value: string; label: string }[]
  catalogs: { value: string; label: string }[]
  placeholder?: string
  withWildcard?: boolean
  className?: string
}) {
  const [open, setOpen] = useState(false)

  const allItems = [
    ...(withWildcard ? [{ value: "*", label: "╨Т╤Б╨╡ ╤Б╤Г╤Й╨╜╨╛╤Б╤В╨╕" }] : []),
    ...documents,
    ...catalogs,
  ]
  const selectedLabel = allItems.find((e) => e.value === value)?.label

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className={cn("h-8 text-xs justify-between font-normal", !selectedLabel && "text-muted-foreground", className)}
        >
          <span className="truncate">{selectedLabel ?? placeholder}</span>
          <ChevronsUpDown className="ml-2 h-3 w-3 shrink-0 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[280px] p-0" align="start">
        <Command>
          <CommandInput placeholder="╨Я╨╛╨╕╤Б╨║тАж" className="h-8 text-xs" />
          <CommandList>
            <CommandEmpty className="py-4 text-center text-xs text-muted-foreground">╨Э╨╡ ╨╜╨░╨╣╨┤╨╡╨╜╨╛</CommandEmpty>
            {withWildcard && (
              <CommandGroup>
                <CommandItem
                  value="*"
                  onSelect={() => { onChange("*"); setOpen(false) }}
                  className="text-xs"
                >
                  <Check className={cn("mr-2 h-3 w-3", value === "*" ? "opacity-100" : "opacity-0")} />
                  ╨Т╤Б╨╡ ╤Б╤Г╤Й╨╜╨╛╤Б╤В╨╕
                </CommandItem>
              </CommandGroup>
            )}
            {documents.length > 0 && (
              <CommandGroup heading="╨Ф╨╛╨║╤Г╨╝╨╡╨╜╤В╤Л">
                {documents.map((e) => (
                  <CommandItem
                    key={e.value}
                    value={e.label}
                    onSelect={() => { onChange(e.value); setOpen(false) }}
                    className="text-xs"
                  >
                    <Check className={cn("mr-2 h-3 w-3", value === e.value ? "opacity-100" : "opacity-0")} />
                    {e.label}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {catalogs.length > 0 && (
              <CommandGroup heading="╨б╨┐╤А╨░╨▓╨╛╤З╨╜╨╕╨║╨╕">
                {catalogs.map((e) => (
                  <CommandItem
                    key={e.value}
                    value={e.label}
                    onSelect={() => { onChange(e.value); setOpen(false) }}
                    className="text-xs"
                  >
                    <Check className={cn("mr-2 h-3 w-3", value === e.value ? "opacity-100" : "opacity-0")} />
                    {e.label}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

// тФАтФА Rules Section (CEL) тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

function RulesSection({
  profileId,
  rules,
  setRules,
}: {
  profileId?: string
  rules: PolicyRuleResponse[]
  setRules: React.Dispatch<React.SetStateAction<PolicyRuleResponse[]>>
}) {
  const metaEntities = useMetadataStore((s) => s.entities)

  // Build grouped entity options using e.key (snake_case) тАФ matches DB format
  // Sorted alphabetically within each group
  const { entityCatalogs, entityDocuments, allEntities, entityLabelMap } = useMemo(() => {
    const cats: { value: string; label: string }[] = []
    const docs: { value: string; label: string }[] = []
    const labelMap: Record<string, string> = { "*": "╨Т╤Б╨╡ ╤Б╤Г╤Й╨╜╨╛╤Б╤В╨╕" }
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

  // Default entityName: wildcard (all entities)
  const defaultEntityKey = "*"

  const [adding, setAdding] = useTabState("cel.adding", false)
  const [editingRuleId, setEditingRuleId] = useTabState<string | null>("cel.editingId", null)
  const [saving, setSaving] = useState(false)
  const [validationResult, setValidationResult] = useState<{ valid: boolean; error?: string } | null>(null)
  const [ruleFields, setRuleFields] = useState<{ name: string; label?: string; type: string }[]>([])
  const [newRule, setNewRule] = useTabState<{
    name: string
    entityName: string
    actions: string[]
    expression: string
    effect: "deny" | "allow"
    priority: number
  }>("cel.newRule", {
    name: "",
    entityName: defaultEntityKey,
    actions: ["update"],
    expression: "",
    effect: "deny",
    priority: 10,
  })

  // CEL sandbox state
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
      // fallback тАФ keep current doc
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
      setValidationResult({ valid: false, error: "╨Ю╤И╨╕╨▒╨║╨░ ╨▓╨░╨╗╨╕╨┤╨░╤Ж╨╕╨╕" })
    }
  }

  // Load entity fields for autocomplete in rule editor
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
      toast.error("╨Э╨░╨╖╨▓╨░╨╜╨╕╨╡ ╨╕ ╨▓╤Л╤А╨░╨╢╨╡╨╜╨╕╨╡ ╨╛╨▒╤П╨╖╨░╤В╨╡╨╗╤М╨╜╤Л")
      return
    }
    if (newRule.actions.length === 0) {
      toast.error("╨Т╤Л╨▒╨╡╤А╨╕╤В╨╡ ╤Е╨╛╤В╤П ╨▒╤Л ╨╛╨┤╨╜╨╛ ╨┤╨╡╨╣╤Б╤В╨▓╨╕╨╡")
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
        toast.success("╨г╤Б╨╗╨╛╨▓╨╕╨╡ ╨╛╨▒╨╜╨╛╨▓╨╗╨╡╨╜╨╛")
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
        toast.success("╨г╤Б╨╗╨╛╨▓╨╕╨╡ ╨┤╨╛╨▒╨░╨▓╨╗╨╡╨╜╨╛")
      }
      setAdding(false)
      setEditingRuleId(null)
      setNewRule({ name: "", entityName: defaultEntityKey, actions: ["update"], expression: "", effect: "deny", priority: 10 })
      setValidationResult(null)
    } catch (e) {
      toast.error(e instanceof ApiError ? (e as ApiError).message : "╨Ю╤И╨╕╨▒╨║╨░ ╤Б╨╛╤Е╤А╨░╨╜╨╡╨╜╨╕╤П")
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteRule = async (rule: PolicyRuleResponse) => {
    if (!profileId) return
    try {
      await api.security.rules.delete(profileId, rule.id)
      setRules((prev) => prev.filter((r) => r.id !== rule.id))
      toast.success("╨г╤Б╨╗╨╛╨▓╨╕╨╡ ╤Г╨┤╨░╨╗╨╡╨╜╨╛")
    } catch {
      toast.error("╨Э╨╡ ╤Г╨┤╨░╨╗╨╛╤Б╤М ╤Г╨┤╨░╨╗╨╕╤В╤М ╤Г╤Б╨╗╨╛╨▓╨╕╨╡")
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
      toast.error("╨Э╨╡ ╤Г╨┤╨░╨╗╨╛╤Б╤М ╨╛╨▒╨╜╨╛╨▓╨╕╤В╤М ╤Г╤Б╨╗╨╛╨▓╨╕╨╡")
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
        setSandboxResult({ result: false, error: "╨Э╨╡╨▓╨░╨╗╨╕╨┤╨╜╤Л╨╣ JSON ╨┤╨╛╨║╤Г╨╝╨╡╨╜╤В╨░", elapsed: "0s" })
        setSandboxTesting(false)
        return
      }
      const res = await api.security.rules.test(sandboxExpr, doc, sandboxAction)
      setSandboxResult(res)
    } catch {
      setSandboxResult({ result: false, error: "╨Ю╤И╨╕╨▒╨║╨░ ╨▓╤Л╨┐╨╛╨╗╨╜╨╡╨╜╨╕╤П", elapsed: "0s" })
    } finally {
      setSandboxTesting(false)
    }
  }

  return (
    <div className="max-w-3xl space-y-4">
      <p className="text-xs text-muted-foreground mb-3">
        ╨Я╤А╨╛╨│╤А╨░╨╝╨╝╨╕╤А╤Г╨╡╨╝╤Л╨╡ ╤Г╤Б╨╗╨╛╨▓╨╕╤П ╨┤╨╗╤П ╤В╨╛╨╜╨║╨╛╨╣ ╨╜╨░╤Б╤В╤А╨╛╨╣╨║╨╕ ╨┐╤А╨░╨▓ ╨┤╨╛╤Б╤В╤Г╨┐╨░.
        ╨Э╨░╨┐╤А╨╕╨╝╨╡╤А: ╨╖╨░╨┐╤А╨╡╤В╨╕╤В╤М ╤А╨╡╨┤╨░╨║╤В╨╕╤А╨╛╨▓╨░╨╜╨╕╨╡ ╨┐╤А╨╛╨▓╨╡╨┤╤С╨╜╨╜╤Л╤Е ╨┤╨╛╨║╤Г╨╝╨╡╨╜╤В╨╛╨▓ ╨╕╨╗╨╕ ╨╛╨│╤А╨░╨╜╨╕╤З╨╕╤В╤М ╤Б╤Г╨╝╨╝╤Г ╨╛╨┐╨╡╤А╨░╤Ж╨╕╨╕.
      </p>

      {/* CEL Sandbox */}
      <div className="rounded-md border">
        <button
          className="w-full flex items-center justify-between px-4 py-2.5 text-xs font-medium hover:bg-muted/40 transition-colors"
          onClick={() => setSandboxOpen(!sandboxOpen)}
        >
          <span>ЁЯзк ╨Я╤А╨╛╨▓╨╡╤А╨║╨░ ╨▓╤Л╤А╨░╨╢╨╡╨╜╨╕╤П</span>
          <ChevronRight className={cn("h-3.5 w-3.5 text-muted-foreground transition-transform", sandboxOpen && "rotate-90")} />
        </button>
        {sandboxOpen && (
          <div className="px-4 pb-4 space-y-3 border-t pt-3">
            {/* Entity selector + auto-mock */}
            <div className="flex items-center gap-2">
              <div className="flex-1">
                <Label className="text-xs text-muted-foreground mb-1">╨б╤Г╤Й╨╜╨╛╤Б╤В╤М</Label>
                <EntityCombobox
                  value={sandboxEntity}
                  onChange={handleSandboxEntityChange}
                  documents={entityDocuments}
                  catalogs={entityCatalogs}
                  placeholder="╨Т╤Л╨▒╨╡╤А╨╕╤В╨╡ ╤Б╤Г╤Й╨╜╨╛╤Б╤В╤МтАж"
                  className="w-full"
                />
              </div>
              {sandboxMockLoading && (
                <div className="pt-4">
                  <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                </div>
              )}
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div>
                <Label className="text-xs text-muted-foreground mb-1">CEL-╨▓╤Л╤А╨░╨╢╨╡╨╜╨╕╨╡</Label>
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
                  <Label className="text-xs text-muted-foreground">╨Ф╨╛╨║╤Г╨╝╨╡╨╜╤В (JSON)</Label>
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
                <Label className="text-xs text-muted-foreground mb-1">╨Ф╨╡╨╣╤Б╤В╨▓╨╕╨╡</Label>
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
                ╨Я╤А╨╛╨▓╨╡╤А╨╕╤В╤М
              </Button>
              {sandboxEntity && sandboxFields.length > 0 && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 text-xs ml-auto"
                  onClick={() => setSandboxCheatOpen(!sandboxCheatOpen)}
                >
                  <BookOpen className="mr-1 h-3 w-3" />
                  {sandboxCheatOpen ? "╨б╨║╤А╤Л╤В╤М ╨┐╨╛╨╗╤П" : "╨Ф╨╛╤Б╤В╤Г╨┐╨╜╤Л╨╡ ╨┐╨╛╨╗╤П"}
                </Button>
              )}
            </div>

            {/* Field cheat sheet */}
            {sandboxCheatOpen && sandboxFields.length > 0 && (
              <div className="rounded-md bg-muted/40 p-3">
                <p className="text-[11px] font-medium text-muted-foreground mb-2">╨Я╨╛╨╗╤П ╨┤╨╛╨║╤Г╨╝╨╡╨╜╤В╨░ (doc.*)</p>
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
                    ? "bg-success/10 text-success"
                    : "bg-warning/10 text-warning"
              )}>
                {sandboxResult.error ? (
                  <><AlertTriangle className="h-3.5 w-3.5 shrink-0" /> {sandboxResult.error}</>
                ) : sandboxResult.result ? (
                  <><Check className="h-3.5 w-3.5 shrink-0" /> ╨а╨╡╨╖╤Г╨╗╤М╤В╨░╤В: true</>
                ) : (
                  <><X className="h-3.5 w-3.5 shrink-0" /> ╨а╨╡╨╖╤Г╨╗╤М╤В╨░╤В: false</>
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
            ╨б╨╜╨░╤З╨░╨╗╨░ ╤Б╨╛╤Е╤А╨░╨╜╨╕╤В╨╡ ╨┐╤А╨╛╤Д╨╕╨╗╤М, ╤З╤В╨╛╨▒╤Л ╨┤╨╛╨▒╨░╨▓╨╗╤П╤В╤М ╤Г╤Б╨╗╨╛╨▓╨╕╤П.
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
                {rule.effect === "deny" ? "╨Ч╨░╨┐╤А╨╡╤В╨╕╤В╤М" : "╨а╨░╨╖╤А╨╡╤И╨╕╤В╤М"}
              </Badge>
              <Badge variant="outline" className="text-[10px] h-4 font-mono">
                {entityLabelMap[rule.entityName] ?? rule.entityName}
              </Badge>
              {rule.actions.map((a) => (
                <Badge key={a} variant="secondary" className="text-[10px] h-4">
                  {RULE_ACTIONS.find((ra) => ra.value === a)?.label ?? a}
                </Badge>
              ))}
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
                onClick={() => handleEditRule(rule)}
              >
                <Pencil className="h-3 w-3 text-muted-foreground" />
              </Button>
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
              <Label className="text-xs text-muted-foreground mb-1">╨Э╨░╨╖╨▓╨░╨╜╨╕╨╡</Label>
              <Input
                value={newRule.name}
                onChange={(e) => setNewRule((r) => ({ ...r, name: e.target.value }))}
                placeholder="╨Ч╨░╨┐╤А╨╡╤В ╨┐╤А╨╛╨▓╨╡╨┤╨╡╨╜╨╕╤П"
                className="h-8 text-xs"
              />
            </div>
            <div>
              <Label className="text-xs text-muted-foreground mb-1">╨б╤Г╤Й╨╜╨╛╤Б╤В╤М</Label>
              <EntityCombobox
                value={newRule.entityName}
                onChange={(v) => setNewRule((r) => ({ ...r, entityName: v }))}
                documents={entityDocuments}
                catalogs={entityCatalogs}
                withWildcard
                className="w-full"
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label className="text-xs text-muted-foreground">╨Ф╨╡╨╣╤Б╤В╨▓╨╕╤П</Label>
            <div className="flex flex-wrap gap-1.5">
              {RULE_ACTIONS.map((a) => {
                const selected = newRule.actions.includes(a.value)
                return (
                  <button
                    key={a.value}
                    type="button"
                    className={cn(
                      "inline-flex items-center rounded-md border px-2.5 py-1 text-[11px] font-medium transition-colors",
                      selected
                        ? "border-primary bg-primary/10 text-primary"
                        : "border-input bg-background text-muted-foreground hover:bg-muted/50"
                    )}
                    onClick={() => {
                      setNewRule((r) => ({
                        ...r,
                        actions: selected
                          ? r.actions.filter((v) => v !== a.value)
                          : [...r.actions, a.value],
                      }))
                    }}
                  >
                    {selected && <Check className="mr-1 h-3 w-3" />}
                    {a.label}
                  </button>
                )
              })}
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="text-xs text-muted-foreground mb-1">╨н╤Д╤Д╨╡╨║╤В</Label>
              <Select
                value={newRule.effect}
                onValueChange={(v) => setNewRule((r) => ({ ...r, effect: v as "deny" | "allow" }))}
              >
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="deny">╨Ч╨░╨┐╤А╨╡╤В╨╕╤В╤М ╨╛╨┐╨╡╤А╨░╤Ж╨╕╤О</SelectItem>
                  <SelectItem value="allow">╨п╨▓╨╜╨╛ ╤А╨░╨╖╤А╨╡╤И╨╕╤В╤М</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label className="text-xs text-muted-foreground mb-1">╨Я╤А╨╕╨╛╤А╨╕╤В╨╡╤В</Label>
              <Input
                type="number"
                value={newRule.priority}
                onChange={(e) => setNewRule((r) => ({ ...r, priority: parseInt(e.target.value) || 10 }))}
                className="h-8 text-xs font-mono"
              />
            </div>
          </div>

          <div>
            <Label className="text-xs text-muted-foreground mb-1">CEL-╨▓╤Л╤А╨░╨╢╨╡╨╜╨╕╨╡</Label>
            <div className="relative">
              <div className="rounded-md border overflow-hidden">
                <CelEditor
                  value={newRule.expression}
                  onChange={(v) => {
                    setNewRule((r) => ({ ...r, expression: v }))
                    setValidationResult(null)
                  }}
                  fields={ruleFields}
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
                setEditingRuleId(null)
                setValidationResult(null)
              }}
            >
              ╨Ю╤В╨╝╨╡╨╜╨╕╤В╤М
            </Button>
            <Button
              size="sm"
              className="h-7 text-xs"
              onClick={handleSaveRule}
              disabled={saving}
            >
              {saving && <Loader2 className="mr-1 h-3 w-3 animate-spin" />}
              {editingRuleId ? "╨б╨╛╤Е╤А╨░╨╜╨╕╤В╤М" : "╨Ф╨╛╨▒╨░╨▓╨╕╤В╤М"}
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
          ╨Ф╨╛╨▒╨░╨▓╨╕╤В╤М ╤Г╤Б╨╗╨╛╨▓╨╕╨╡
        </Button>
      )}

      {rules.length === 0 && !adding && (
        <div className="py-6 text-center text-xs text-muted-foreground">
          ╨Э╨╡╤В ╨┤╨╛╨┐╨╛╨╗╨╜╨╕╤В╨╡╨╗╤М╨╜╤Л╤Е ╤Г╤Б╨╗╨╛╨▓╨╕╨╣. ╨Ю╤Б╨╜╨╛╨▓╨╜╤Л╨╡ ╨┐╤А╨░╨▓╨░ ╨╛╨┐╤А╨╡╨┤╨╡╨╗╤П╤О╤В╤Б╤П ╤А╨╛╨╗╤П╨╝╨╕.
        </div>
      )}
    </div>
  )
}

// тФАтФА Users Section тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

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
        ╨Э╨╡╤В ╨┐╤А╨╕╨▓╤П╨╖╨░╨╜╨╜╤Л╤Е ╨┐╨╛╨╗╤М╨╖╨╛╨▓╨░╤В╨╡╨╗╨╡╨╣
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

// тФАтФА Audit Section тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

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
        ╨Э╨╡╤В ╨╖╨░╨┐╨╕╤Б╨╡╨╣ ╨▓ ╨╢╤Г╤А╨╜╨░╨╗╨╡ ╨╕╨╖╨╝╨╡╨╜╨╡╨╜╨╕╨╣
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
                      line.variant === "added" && "text-success",
                      line.variant === "removed" && "text-destructive",
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