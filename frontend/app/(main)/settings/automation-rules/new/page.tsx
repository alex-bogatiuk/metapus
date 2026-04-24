"use client"

import { useRouter } from "next/navigation"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
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
import type { AutomationChannel, CreateRuleRequest } from "@/types/automation"
import { useState, useEffect, useMemo } from "react"
import { ChevronDown, Clock, FileText, Activity } from "lucide-react"
import {
  INITIAL_RULE_STATE, ENTITY_EVENT_ACTIONS, TRIGGER_TYPE_OPTIONS,
  mapRuleToCreate, validateRule,
  type RuleFormState, type SubscriberFormEntry,
} from "@/lib/automation-rule-form"

const TRIGGER_ICONS: Record<string, typeof Clock> = {
  entity_event: FileText,
  business_event: Activity,
  scheduled: Clock,
}

export default function NewAutomationRulePage() {
  const router = useRouter()
  const [channels, setChannels] = useState<AutomationChannel[]>([])
  const [settingsOpen, setSettingsOpen] = useState(false)
  const allEntities = useMetadataStore(s => s.entities)
  const documentEntities = useMemo(() => allEntities.filter(e => e.type === "document"), [allEntities])

  useEffect(() => {
    api.automation.channels.list().then(setChannels).catch(console.error)
  }, [])

  const { f, update, handleChange, handleSave, saving, error } = useCatalogForm<RuleFormState, unknown, CreateRuleRequest, unknown>({
    entityName: "Правило автоматизации",
    initialState: INITIAL_RULE_STATE,
    api: {
      create: (data: CreateRuleRequest) => api.automation.rules.create(data),
    },
    listPath: "/settings/automation-rules",
    validate: validateRule,
    mapToCreate: mapRuleToCreate,
    titleField: (s) => s.name || undefined,
  })

  const { handleEditorMount } = useCelCompletions(f.targetEntities)

  // ── Trigger-specific handlers ──────────────────────────────────────────

  const handleTriggerTypeChange = (value: string) => {
    const tt = value as RuleFormState["triggerType"]
    const updates: Partial<RuleFormState> = { triggerType: tt }

    // Reset trigger-specific fields
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

  const handleWildcardToggle = (checked: boolean) => {
    // Wildcard = empty array (NULL in DB)
    update({ targetEntities: checked ? [] : [] } as Partial<RuleFormState>)
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
        backHref="/settings/automation-rules"
        onClose={() => router.push("/settings/automation-rules")}
      />

      {error && (
        <div className="bg-destructive/10 border-b border-destructive/20 px-4 py-2 text-sm text-destructive">{error}</div>
      )}

      <ScrollArea className="flex-1">
        <div className="p-6 max-w-5xl space-y-6">
          {/* ── Header: Name + Active ────────────────────────────────── */}
          <div className="flex items-center justify-between gap-4">
            <div className="flex-1">
              <Label className="text-xs text-muted-foreground">Наименование *</Label>
              <Input
                className="mt-1"
                value={f.name}
                placeholder="Например: Крупное поступление → Telegram"
                onChange={(e) => { update({ name: e.target.value }); handleChange() }}
              />
            </div>
            <div className="flex items-center space-x-2 pt-4">
              <Switch checked={f.isActive} onCheckedChange={(v) => { update({ isActive: v }); handleChange() }} />
              <Label className="text-sm">Активно</Label>
            </div>
          </div>

          {/* ── Trigger Section ──────────────────────────────────────── */}
          <div className="space-y-4 rounded-lg border p-4">
            <div className="flex items-center gap-2">
              {(() => { const TIcon = TRIGGER_ICONS[f.triggerType] ?? FileText; return <TIcon className="h-4 w-4 text-muted-foreground" /> })()}
              <Label className="text-sm font-semibold">Триггер</Label>
            </div>

            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              {/* Trigger type */}
              <div>
                <Label className="text-xs text-muted-foreground">Тип триггера *</Label>
                <Select value={f.triggerType} onValueChange={handleTriggerTypeChange}>
                  <SelectTrigger className="mt-1">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {TRIGGER_TYPE_OPTIONS.map(opt => (
                      <SelectItem key={opt.value} value={opt.value} description={opt.description}>
                        {opt.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              {/* Event type — for entity_event */}
              {f.triggerType === "entity_event" && (
                <div>
                  <Label className="text-xs text-muted-foreground">Событие *</Label>
                  <Select value={f.eventType} onValueChange={(v) => { update({ eventType: v }); handleChange() }}>
                    <SelectTrigger className="mt-1"><SelectValue placeholder="Выберите событие" /></SelectTrigger>
                    <SelectContent>
                      {ENTITY_EVENT_ACTIONS.map((a) => (
                        <SelectItem key={a.value} value={a.value}>{a.label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}

              {/* Free-text event — for business_event */}
              {f.triggerType === "business_event" && (
                <div>
                  <Label className="text-xs text-muted-foreground">Тип события *</Label>
                  <Input
                    className="mt-1"
                    placeholder="business.currency_rates_loaded"
                    value={f.eventType}
                    onChange={(e) => { update({ eventType: e.target.value }); handleChange() }}
                  />
                </div>
              )}

              {/* Schedule configurator — for scheduled trigger */}
              {f.triggerType === "scheduled" && (
                <div>
                  <Label className="text-xs text-muted-foreground">Расписание *</Label>
                  <ScheduleButton
                    value={f.cronExpression}
                    onChange={handleCronChange}
                    className="w-full mt-1"
                  />
                </div>
              )}
            </div>

            {/* Entity multi-select — for entity_event */}
            {f.triggerType === "entity_event" && (
              <div className="mt-3">
                <Label className="text-xs text-muted-foreground">Сущности</Label>
                <div className="mt-2 space-y-2 rounded-md border p-3">
                  <label className="flex items-center gap-2 text-sm cursor-pointer">
                    <Checkbox
                      checked={f.targetEntities.length === 0}
                      onCheckedChange={() => handleWildcardToggle(true)}
                    />
                    <span className="text-muted-foreground">Любая сущность</span>
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

          {/* ── Reaction Section ──────────────────────────────────────── */}
          <div className="space-y-4 rounded-lg border p-4">
            <Label className="text-sm font-semibold">Действие</Label>

            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <div>
                <Label className="text-xs text-muted-foreground">Тип действия *</Label>
                <Select value={f.reactionType} onValueChange={(v) => { update({ reactionType: v as RuleFormState["reactionType"] }); handleChange() }}>
                  <SelectTrigger className="mt-1"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="notify">Telegram / Email</SelectItem>
                    <SelectItem value="webhook_call">Webhook API</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            {/* Subscribers */}
            <SubscriberList
              subscribers={f.subscribers}
              channels={channels}
              onChange={handleSubscribersChange}
            />
          </div>

          {/* ── CEL Condition — hidden for scheduled ────────────────── */}
          {f.triggerType !== "scheduled" && (
            <div className="space-y-2">
              <Label className="text-xs text-muted-foreground font-semibold">Условие выполнения (CEL)</Label>
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
                Введите <code>doc.</code> для автозаполнения полей сущности. Пример: <code>humanAmounts.totalAmount &gt; 10000 &amp;&amp; action == &apos;posted&apos;</code>
              </p>
            </div>
          )}

          {/* ── Message Template ──────────────────────────────────────── */}
          <div className="space-y-2">
            <Label className="text-xs text-muted-foreground font-semibold">Текст сообщения (Go Text Template)</Label>
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
                ? <>Доступно: <code>{"{{ .doc.ruleName }}"}</code>, <code>{"{{ .doc.ruleId }}"}</code>. Обогащение данными отчётов — в следующей итерации.</>
                : <>Доступно: <code>{"{{ .doc }}"}</code>, <code>{"{{ .action }}"}</code>. Функции: <code>{"{{ .doc | json }}"}</code>, <code>{"{{ .doc.totalAmount | currency }}"}</code></>
              }
            </p>
          </div>

          {/* ── Additional Settings (Collapsible) ─────────────────────── */}
          <Collapsible open={settingsOpen} onOpenChange={setSettingsOpen}>
            <CollapsibleTrigger className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors">
              <ChevronDown className={`h-4 w-4 transition-transform ${settingsOpen ? "" : "-rotate-90"}`} />
              Дополнительные настройки
            </CollapsibleTrigger>
            <CollapsibleContent className="mt-3">
              <div className="grid grid-cols-1 gap-4 md:grid-cols-3 rounded-lg border p-4">
                <div>
                  <Label className="text-xs text-muted-foreground">Описание</Label>
                  <Textarea
                    className="mt-1 h-20"
                    placeholder="Описание правила…"
                    value={f.description}
                    onChange={(e) => { update({ description: e.target.value }); handleChange() }}
                  />
                </div>
                <div className="space-y-3">
                  <div>
                    <Label className="text-xs text-muted-foreground">Приоритет</Label>
                    <Input type="number" className="mt-1" value={f.priority} onChange={(e) => { update({ priority: Number(e.target.value) }); handleChange() }} />
                  </div>
                  <div>
                    <Label className="text-xs text-muted-foreground">Макс. ретраев</Label>
                    <Input type="number" className="mt-1" value={f.maxRetries} onChange={(e) => { update({ maxRetries: Number(e.target.value) }); handleChange() }} />
                  </div>
                </div>
                <div className="space-y-3">
                  <div>
                    <Label className="text-xs text-muted-foreground">Cooldown (сек.)</Label>
                    <Input type="number" className="mt-1" value={f.cooldownSeconds} onChange={(e) => { update({ cooldownSeconds: Number(e.target.value) }); handleChange() }} />
                    <p className="text-[10px] text-muted-foreground mt-1">0 = без ограничений</p>
                  </div>
                  <div>
                    <Label className="text-xs text-muted-foreground">Формат</Label>
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
