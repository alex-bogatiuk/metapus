"use client"

import { useRouter } from "next/navigation"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { Play, ChevronDown, Clock, FileText, Activity, Info, AlertTriangle, AlertCircle, CheckCircle2 } from "lucide-react"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import {
  Collapsible, CollapsibleContent, CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useCatalogForm } from "@/hooks/useCatalogForm"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { api } from "@/lib/api"
import Editor from "@monaco-editor/react"
import { useCelCompletions } from "@/hooks/useCelCompletions"
import { SubscriberList } from "@/components/settings/subscriber-list"
import { ScheduleButton } from "@/components/settings/schedule-configurator"
import { Checkbox } from "@/components/ui/checkbox"
import type { AutomationChannel, UpdateRuleRequest } from "@/types/automation"
import { useState, useEffect, useMemo } from "react"
import { toast } from "sonner"
import {
  INITIAL_RULE_STATE, ENTITY_EVENT_ACTIONS, TRIGGER_TYPE_OPTIONS, SEVERITY_OPTIONS,
  mapRuleToUpdate, mapRuleFromResponse, validateRule,
  type RuleFormState, type SubscriberFormEntry,
} from "@/lib/automation-rule-form"

const TRIGGER_ICONS: Record<string, typeof Clock> = {
  entity_event: FileText,
  business_event: Activity,
  scheduled: Clock,
}

export default function EditAutomationRulePage() {
  const router = useRouter()
  const [channels, setChannels] = useState<AutomationChannel[]>([])
  const [settingsOpen, setSettingsOpen] = useState(false)
  const allEntities = useMetadataStore(s => s.entities)
  const documentEntities = useMemo(() => allEntities.filter(e => e.type === "document"), [allEntities])

  useEffect(() => {
    api.automation.channels.list().then(setChannels).catch(console.error)
  }, [])

  const { f, update, handleChange, handleSave, saving, error, loading, deletionMark, entityLabel } = useCatalogForm<RuleFormState, unknown, unknown, UpdateRuleRequest>({
    entityName: "╨Я╤А╨░╨▓╨╕╨╗╨╛ ╨░╨▓╤В╨╛╨╝╨░╤В╨╕╨╖╨░╤Ж╨╕╨╕",
    initialState: INITIAL_RULE_STATE,
    api: {
      get: api.automation.rules.get,
      update: (id: string, data: UpdateRuleRequest) => api.automation.rules.update(id, data),
    },
    listPath: "/admin/automation-rules",
    validate: validateRule,
    mapFromResponse: (r: unknown) => mapRuleFromResponse(r as Record<string, unknown>),
    mapToUpdate: mapRuleToUpdate,
    getVersion: (r: unknown) => (r as Record<string, unknown>).version as number ?? 1,
    titleField: (s) => s.name || undefined,
  })

  const { handleEditorMount } = useCelCompletions(f.targetEntities)

  // тФАтФА Test тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА
  const [isTesting, setIsTesting] = useState(false)

  const handleTest = async () => {
    setIsTesting(true)
    try {
      const payload = {
        action: f.triggerType === "scheduled" ? "scheduled" : "posted",
        entityType: f.triggerType === "scheduled" ? "automation" : "document",
        doc: f.triggerType === "scheduled"
          ? { ruleName: f.name, ruleId: f.id || "test" }
          : { id: "test-doc-123", number: "╨Я╨в-000042", totalAmount: 150000, docTotal: 150000 },
      }
      const res = await api.automation.rules.test({
        conditionCel: f.conditionCel || undefined,
        actionTemplate: f.actionTemplate,
        payload,
      })

      if (f.triggerType !== "scheduled" && !res.conditionMatched) {
        toast.error(`╨г╤Б╨╗╨╛╨▓╨╕╨╡ ╨╜╨╡ ╨▓╤Л╨┐╨╛╨╗╨╜╨╡╨╜╨╛. ${res.conditionError || "╨а╨╡╨╖╤Г╨╗╤М╤В╨░╤В: false"}`)
      } else if (res.renderError) {
        toast.error(`╨Ю╤И╨╕╨▒╨║╨░ ╤И╨░╨▒╨╗╨╛╨╜╨░: ${res.renderError}`)
      } else {
        toast.success("╨и╨░╨▒╨╗╨╛╨╜ ╨╛╤В╤А╨╡╨╜╨┤╨╡╤А╨╡╨╜", {
          description: (
            <pre className="text-[10px] mt-2 bg-black text-white p-2 rounded-md overflow-x-auto max-h-[200px]">
              {res.renderedPayload}
            </pre>
          ),
          duration: 10000,
        })
      }
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : "╨Ю╤И╨╕╨▒╨║╨░"
      toast.error("╨Ю╤И╨╕╨▒╨║╨░ ╤В╨╡╤Б╤В╨╕╤А╨╛╨▓╨░╨╜╨╕╤П: " + msg)
    } finally {
      setIsTesting(false)
    }
  }

  // тФАтФА Trigger-specific handlers тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА

  const handleTriggerTypeChange = (value: string) => {
    const tt = value as RuleFormState["triggerType"]
    const updates: Partial<RuleFormState> = { triggerType: tt }

    if (tt === "scheduled") {
      updates.cronExpression = "0 0 9 * * *"
      updates.eventType = ""
      updates.conditionCel = ""
      updates.targetEntities = []
    } else if (tt === "entity_event") {
      updates.eventType = "posted"
      updates.targetEntities = []
      updates.cronExpression = ""
    } else {
      updates.cronExpression = ""
      updates.targetEntities = []
    }

    update(updates as Partial<RuleFormState>)
    handleChange()
  }

  const handleToggleEntity = (entityKey: string, checked: boolean) => {
    const current = f.targetEntities ?? []
    const next = checked
      ? [...current, entityKey]
      : current.filter(k => k !== entityKey)
    update({ targetEntities: next } as Partial<RuleFormState>)
    handleChange()
  }

  const handleWildcardToggle = () => {
    update({ targetEntities: [] } as Partial<RuleFormState>)
    handleChange()
  }

  const handleCronChange = (cronExpression: string) => {
    update({ cronExpression } as Partial<RuleFormState>)
    handleChange()
  }

  const handleSubscribersChange = (subs: SubscriberFormEntry[]) => {
    update({ subscribers: subs } as Partial<RuleFormState>)
    handleChange()
  }

  if (loading) {
    return <div className="p-4 text-sm text-muted-foreground">╨Ч╨░╨│╤А╤Г╨╖╨║╨░...</div>
  }

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title={f.name || "╨Э╨╛╨▓╨╛╨╡"}
        status={deletionMark ? { label: "╨г╨┤╨░╨╗╤С╨╜", variant: "destructive" } : undefined}
        primaryAction={{
          label: saving ? "╨б╨╛╤Е╤А╨░╨╜╨╡╨╜╨╕╨╡тАж" : "╨Ч╨░╨┐╨╕╤Б╨░╤В╤М ╨╕ ╨╖╨░╨║╤А╤Л╤В╤М",
          variant: "default",
          onClick: () => handleSave(true),
        }}
        secondaryActions={[
          { label: "╨Ч╨░╨┐╨╕╤Б╨░╤В╤М", onClick: () => handleSave(false) },
        ]}
        backHref="/admin/automation-rules"
        onClose={() => router.push("/admin/automation-rules")}
      >
        <Button variant="outline" size="sm" onClick={handleTest} disabled={isTesting}>
          <Play className="h-4 w-4 mr-2" />
          {isTesting ? "╨в╨╡╤Б╤ВтАж" : "╨в╨╡╤Б╤В ╤И╨░╨▒╨╗╨╛╨╜╨░"}
        </Button>
      </FormToolbar>

      {error && (
        <div className="bg-destructive/10 border-b border-destructive/20 px-4 py-2 text-sm text-destructive">{error}</div>
      )}

      <ScrollArea className="flex-1">
        <div className="p-6 max-w-5xl space-y-6">
          {/* тФАтФА Header: Name + Active тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА */}
          <div className="flex items-center justify-between gap-4">
            <div className="flex-1">
              <Label className="text-xs text-muted-foreground">╨Э╨░╨╕╨╝╨╡╨╜╨╛╨▓╨░╨╜╨╕╨╡ *</Label>
              <Input
                className="mt-1"
                value={f.name}
                onChange={(e) => { update({ name: e.target.value }); handleChange() }}
              />
            </div>
            <div className="flex items-center space-x-2 pt-4">
              <Switch checked={f.isActive} onCheckedChange={(v) => { update({ isActive: v }); handleChange() }} />
              <Label className="text-sm">╨Р╨║╤В╨╕╨▓╨╜╨╛</Label>
            </div>
          </div>

          {/* тФАтФА Trigger Section тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА */}
          <div className="space-y-4 rounded-lg border p-4">
            <div className="flex items-center gap-2">
              {(() => { const TIcon = TRIGGER_ICONS[f.triggerType] ?? FileText; return <TIcon className="h-4 w-4 text-muted-foreground" /> })()}
              <Label className="text-sm font-semibold">╨в╤А╨╕╨│╨│╨╡╤А</Label>
            </div>

            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <div>
                <Label className="text-xs text-muted-foreground">╨в╨╕╨┐ ╤В╤А╨╕╨│╨│╨╡╤А╨░ *</Label>
                <Select value={f.triggerType} onValueChange={handleTriggerTypeChange}>
                  <SelectTrigger className="mt-1"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {TRIGGER_TYPE_OPTIONS.map(opt => (
                      <SelectItem key={opt.value} value={opt.value} description={opt.description}>
                        {opt.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              {f.triggerType === "entity_event" && (
                <div>
                  <Label className="text-xs text-muted-foreground">╨б╨╛╨▒╤Л╤В╨╕╨╡ *</Label>
                  <Select value={f.eventType} onValueChange={(v) => { update({ eventType: v }); handleChange() }}>
                    <SelectTrigger className="mt-1"><SelectValue placeholder="╨Т╤Л╨▒╨╡╤А╨╕╤В╨╡ ╤Б╨╛╨▒╤Л╤В╨╕╨╡" /></SelectTrigger>
                    <SelectContent>
                      {ENTITY_EVENT_ACTIONS.map((a) => (
                        <SelectItem key={a.value} value={a.value}>{a.label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}

              {f.triggerType === "business_event" && (
                <div>
                  <Label className="text-xs text-muted-foreground">╨в╨╕╨┐ ╤Б╨╛╨▒╤Л╤В╨╕╤П *</Label>
                  <Input
                    className="mt-1"
                    placeholder="business.currency_rates_loaded"
                    value={f.eventType}
                    onChange={(e) => { update({ eventType: e.target.value }); handleChange() }}
                  />
                </div>
              )}

              {f.triggerType === "scheduled" && (
                <div>
                  <Label className="text-xs text-muted-foreground">╨а╨░╤Б╨┐╨╕╤Б╨░╨╜╨╕╨╡ *</Label>
                  <ScheduleButton
                    value={f.cronExpression}
                    onChange={handleCronChange}
                    className="w-full mt-1"
                  />
                </div>
              )}
            </div>

            {/* Entity multi-select тАФ for entity_event */}
            {f.triggerType === "entity_event" && (
              <div className="mt-3">
                <Label className="text-xs text-muted-foreground">╨б╤Г╤Й╨╜╨╛╤Б╤В╨╕</Label>
                <div className="mt-2 space-y-2 rounded-md border p-3">
                  <label className="flex items-center gap-2 text-sm cursor-pointer">
                    <Checkbox
                      checked={f.targetEntities.length === 0}
                      onCheckedChange={() => handleWildcardToggle()}
                    />
                    <span className="text-muted-foreground">╨Ы╤О╨▒╨░╤П ╤Б╤Г╤Й╨╜╨╛╤Б╤В╤М</span>
                  </label>
                  {documentEntities.map(ent => (
                    <label key={ent.key} className="flex items-center gap-2 text-sm cursor-pointer">
                      <Checkbox
                        checked={f.targetEntities.includes(ent.key)}
                        onCheckedChange={(checked) => handleToggleEntity(ent.key, !!checked)}
                      />
                      {ent.presentation.singular}
                    </label>
                  ))}
                </div>
              </div>
            )}
          </div>

          {/* тФАтФА Reaction + Subscribers тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА */}
          <div className="space-y-4 rounded-lg border p-4">
            <Label className="text-sm font-semibold">╨Ф╨╡╨╣╤Б╤В╨▓╨╕╨╡</Label>

            <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
              <div>
                <Label className="text-xs text-muted-foreground">╨в╨╕╨┐ ╨┤╨╡╨╣╤Б╤В╨▓╨╕╤П *</Label>
                <Select value={f.reactionType} onValueChange={(v) => { update({ reactionType: v as RuleFormState["reactionType"] }); handleChange() }}>
                  <SelectTrigger className="mt-1"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="notify" description="UI ╤Г╨▓╨╡╨┤╨╛╨╝╨╗╨╡╨╜╨╕╤П + ╨▓╨╜╨╡╤И╨╜╨╕╨╡ ╨║╨░╨╜╨░╨╗╤Л">╨г╨▓╨╡╨┤╨╛╨╝╨╗╨╡╨╜╨╕╨╡</SelectItem>
                    <SelectItem value="webhook_call" description="HTTP POST/PUT/GET">Webhook API</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              {/* Severity selector тАФ shown for notify reaction type */}
              {f.reactionType === "notify" && (
                <div>
                  <Label className="text-xs text-muted-foreground">╨Т╨░╨╢╨╜╨╛╤Б╤В╤М</Label>
                  <Select value={f.notifSeverity || "info"} onValueChange={(v) => { update({ notifSeverity: v }); handleChange() }}>
                    <SelectTrigger className="mt-1">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {SEVERITY_OPTIONS.map(opt => {
                        const SevIcon = opt.value === "error" ? AlertCircle
                          : opt.value === "warning" ? AlertTriangle
                          : opt.value === "success" ? CheckCircle2
                          : Info
                        return (
                          <SelectItem key={opt.value} value={opt.value} description={opt.description}>
                            <span className="flex items-center gap-1.5">
                              <SevIcon className="h-3.5 w-3.5" />
                              {opt.label}
                            </span>
                          </SelectItem>
                        )
                      })}
                    </SelectContent>
                  </Select>
                </div>
              )}
            </div>

            <SubscriberList
              subscribers={f.subscribers}
              channels={channels}
              reactionType={f.reactionType}
              onChange={handleSubscribersChange}
            />
          </div>

          {/* тФАтФА CEL Condition тАФ hidden for scheduled тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА */}
          {f.triggerType !== "scheduled" && (
            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground font-semibold">╨г╤Б╨╗╨╛╨▓╨╕╨╡ ╨▓╤Л╨┐╨╛╨╗╨╜╨╡╨╜╨╕╤П (CEL)</Label>
              <div className="border rounded-md overflow-hidden h-[120px]">
                <Editor
                  defaultLanguage="go"
                  value={f.conditionCel || ""}
                  onChange={(v) => { update({ conditionCel: v || "" }); handleChange() }}
                  onMount={handleEditorMount}
                  options={{ minimap: { enabled: false }, lineNumbers: "off", scrollBeyondLastLine: false, fontSize: 13, quickSuggestions: true, suggestOnTriggerCharacters: true }}
                />
              </div>
              <p className="text-[10px] text-muted-foreground">
                ╨Ш╤Б╨┐╨╛╨╗╤М╨╖╤Г╨╣╤В╨╡ <code>doc.</code> ╨┤╨╗╤П ╨░╨▓╤В╨╛╨╖╨░╨┐╨╛╨╗╨╜╨╡╨╜╨╕╤П ╨┐╨╛╨╗╨╡╨╣ ╤Б╤Г╤Й╨╜╨╛╤Б╤В╨╕. ╨Я╤А╨╕╨╝╨╡╤А: <code>humanAmounts.totalAmount &gt; 10000 &amp;&amp; action == &apos;posted&apos;</code>
              </p>
            </div>
          )}

          {/* тФАтФА Message Template тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА */}
          <div className="space-y-2">
            <Label className="text-xs text-muted-foreground font-semibold">╨в╨╡╨║╤Б╤В ╤Б╨╛╨╛╨▒╤Й╨╡╨╜╨╕╤П (Go Text Template)</Label>
            <div className="border rounded-md overflow-hidden h-[200px]">
              <Editor
                defaultLanguage="handlebars"
                value={f.actionTemplate || ""}
                onChange={(v) => { update({ actionTemplate: v || "" }); handleChange() }}
                options={{ minimap: { enabled: false }, scrollBeyondLastLine: false, fontSize: 13 }}
              />
            </div>
            <p className="text-[10px] text-muted-foreground">
              {f.triggerType === "scheduled"
                ? <>╨Ф╨╛╤Б╤В╤Г╨┐╨╜╨╛: <code>{"{{ .doc.ruleName }}"}</code>, <code>{"{{ .doc.ruleId }}"}</code>. ╨Ю╨▒╨╛╨│╨░╤Й╨╡╨╜╨╕╨╡ ╨┤╨░╨╜╨╜╤Л╨╝╨╕ ╨╛╤В╤З╤С╤В╨╛╨▓ тАФ ╨▓ ╤Б╨╗╨╡╨┤╤Г╤О╤Й╨╡╨╣ ╨╕╤В╨╡╤А╨░╤Ж╨╕╨╕.</>
                : <>╨Ф╨╛╤Б╤В╤Г╨┐╨╜╨╛: <code>{"{{ .doc }}"}</code>, <code>{"{{ .action }}"}</code>. ╨д╤Г╨╜╨║╤Ж╨╕╨╕: <code>{"{{ .doc | json }}"}</code></>
              }
            </p>
          </div>

          {/* тФАтФА Additional Settings (Collapsible) тФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФАтФА */}
          <Collapsible open={settingsOpen} onOpenChange={setSettingsOpen}>
            <CollapsibleTrigger className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors">
              <ChevronDown className={`h-4 w-4 transition-transform ${settingsOpen ? "" : "-rotate-90"}`} />
              ╨Ф╨╛╨┐╨╛╨╗╨╜╨╕╤В╨╡╨╗╤М╨╜╤Л╨╡ ╨╜╨░╤Б╤В╤А╨╛╨╣╨║╨╕
            </CollapsibleTrigger>
            <CollapsibleContent className="mt-3">
              <div className="grid grid-cols-1 gap-4 md:grid-cols-3 rounded-lg border p-4">
                <div>
                  <Label className="text-xs text-muted-foreground">╨Ю╨┐╨╕╤Б╨░╨╜╨╕╨╡</Label>
                  <Textarea
                    className="mt-1 h-20"
                    placeholder="╨Ю╨┐╨╕╤Б╨░╨╜╨╕╨╡ ╨┐╤А╨░╨▓╨╕╨╗╨░тАж"
                    value={f.description}
                    onChange={(e) => { update({ description: e.target.value }); handleChange() }}
                  />
                </div>
                <div className="space-y-3">
                  <div>
                    <Label className="text-xs text-muted-foreground">╨Я╤А╨╕╨╛╤А╨╕╤В╨╡╤В</Label>
                    <Input type="number" className="mt-1" value={f.priority} onChange={(e) => { update({ priority: Number(e.target.value) }); handleChange() }} />
                  </div>
                  <div>
                    <Label className="text-xs text-muted-foreground">╨Ь╨░╨║╤Б. ╤А╨╡╤В╤А╨░╨╡╨▓</Label>
                    <Input type="number" className="mt-1" value={f.maxRetries} onChange={(e) => { update({ maxRetries: Number(e.target.value) }); handleChange() }} />
                  </div>
                </div>
                <div className="space-y-3">
                  <div>
                    <Label className="text-xs text-muted-foreground">Cooldown (╤Б╨╡╨║.)</Label>
                    <Input type="number" className="mt-1" value={f.cooldownSeconds} onChange={(e) => { update({ cooldownSeconds: Number(e.target.value) }); handleChange() }} />
                    <p className="text-[10px] text-muted-foreground mt-1">0 = ╨▒╨╡╨╖ ╨╛╨│╤А╨░╨╜╨╕╤З╨╡╨╜╨╕╨╣</p>
                  </div>
                  <div>
                    <Label className="text-xs text-muted-foreground">╨д╨╛╤А╨╝╨░╤В</Label>
                    <Select value={f.messageFormat} onValueChange={(v) => { update({ messageFormat: v as RuleFormState["messageFormat"] }); handleChange() }}>
                      <SelectTrigger className="mt-1"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="text">Text</SelectItem>
                        <SelectItem value="markdown">Markdown</SelectItem>
                        <SelectItem value="html">HTML</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
              </div>
            </CollapsibleContent>
          </Collapsible>

        </div>
      </ScrollArea>
    </div>
  )
}