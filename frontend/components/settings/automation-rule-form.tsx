"use client"

import { useEffect, useId, useMemo, useState, useSyncExternalStore } from "react"
import dynamic from "next/dynamic"
import { Input } from "@/components/ui/input"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { Button } from "@/components/ui/button"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Checkbox } from "@/components/ui/checkbox"
import { SubscriberList } from "@/components/settings/subscriber-list"
import { ScheduleButton } from "@/components/settings/schedule-configurator"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { useCelCompletions } from "@/hooks/useCelCompletions"
import { api } from "@/lib/api"
import { getSessionGeneration, onScopeReset, isCurrentSession } from "@/lib/auth-scope"
import type { AutomationChannel, PeriodType } from "@/types/automation"
import { ENTITY_EVENT_ACTIONS, TRIGGER_TYPE_OPTIONS, SEVERITY_OPTIONS, PERIOD_TYPE_OPTIONS, type RuleFormState, type SubscriberFormEntry } from "@/lib/automation-rule-form"
import { ChevronDown, Clock, FileText, Activity, Info, AlertTriangle, AlertCircle, CheckCircle2, BarChart3 } from "lucide-react"

const TRIGGER_ICONS: Record<string, typeof Clock> = { entity_event: FileText, business_event: Activity, scheduled: Clock }
const Editor = dynamic(() => import("@monaco-editor/react"), {
  ssr: false,
  loading: () => <div className="p-3 text-xs text-muted-foreground">Загрузка редактора…</div>,
})

interface Resources {
  attempt: number
  generation: number
  channels: AutomationChannel[]
  datasets: { key: string; name: string }[]
  error: string | null
}

interface AutomationRuleFormProps {
  value: RuleFormState
  onChange: (patch: Partial<RuleFormState>) => void
}

export function AutomationRuleForm({ value: f, onChange: update }: AutomationRuleFormProps) {
  const fieldId = useId()
  const generation = useSyncExternalStore(onScopeReset, getSessionGeneration, () => 0)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const [resources, setResources] = useState<Resources | null>(null)
  const allEntities = useMetadataStore(s => s.entities)
  const documentEntities = useMemo(() => allEntities.filter(e => e.type === "document"), [allEntities])
  const { handleEditorMount } = useCelCompletions(f.targetEntities)

  useEffect(() => {
    let cancelled = false
    void Promise.all([api.automation.channels.list(), api.automation.meta.datasets()]).then(
      ([channels, datasets]) => { if (!cancelled && isCurrentSession(generation)) setResources({ attempt, generation, channels, datasets, error: null }) },
      () => { if (!cancelled && isCurrentSession(generation)) setResources({ attempt, generation, channels: [], datasets: [], error: "Не удалось загрузить каналы и отчёты" }) },
    )
    return () => { cancelled = true }
  }, [attempt, generation])
  const current = resources?.attempt === attempt && resources.generation === generation ? resources : null
  const channels = current?.channels ?? []
  const datasets = current?.datasets ?? []

  const handleTriggerTypeChange = (value: string) => {
    const triggerType = value as RuleFormState["triggerType"]
    update({
      triggerType,
      targetEntities: [],
      cronExpression: triggerType === "scheduled" ? "0 0 9 * * *" : "",
      ...(triggerType === "scheduled" ? { eventType: "", conditionCel: "" } : {}),
      ...(triggerType === "entity_event" ? { eventType: "posted" } : {}),
    })
  }
  const handleToggleEntity = (key: string, checked: boolean) => {
    update({ targetEntities: checked ? [...f.targetEntities, key] : f.targetEntities.filter(value => value !== key) })
  }
  const handleCronChange = (cronExpression: string) => update({ cronExpression })
  const handleSubscribersChange = (subscribers: SubscriberFormEntry[]) => update({ subscribers })

  return (
      <ScrollArea className="flex-1">
        <FieldGroup className="p-6 max-w-5xl gap-6">
          {current?.error && (
            <Alert variant="destructive"><AlertDescription className="flex items-center gap-2">
              {current.error}
              <Button type="button" variant="outline" size="sm" onClick={() => setAttempt(value => value + 1)}>Повторить</Button>
            </AlertDescription></Alert>
          )}
          {!current && <p role="status" className="text-sm text-muted-foreground">Загрузка каналов и отчётов…</p>}
          {/* ── Header: Name + Active ────────────────────────────────── */}
          <div className="flex items-center justify-between gap-4">
            <Field className="flex-1">
              <FieldLabel htmlFor={`${fieldId}-field-1`} className="text-xs text-muted-foreground">Наименование *</FieldLabel>
              <Input id={`${fieldId}-field-1`}
                className="mt-1"
                value={f.name}
                placeholder="Например: Крупное поступление → Telegram"
                onChange={(e) => { update({ name: e.target.value }) }}
              />
            </Field>
            <div className="flex items-center gap-2 pt-4">
              <Switch id={`${fieldId}-active`} checked={f.isActive} onCheckedChange={(v) => { update({ isActive: v }) }} />
              <FieldLabel htmlFor={`${fieldId}-active`} className="text-sm">Активно</FieldLabel>
            </div>
          </div>

          {/* ── Trigger Section ──────────────────────────────────────── */}
          <div className="flex flex-col gap-4 rounded-lg border p-4">
            <div className="flex items-center gap-2">
              {(() => { const TIcon = TRIGGER_ICONS[f.triggerType] ?? FileText; return <TIcon className="h-4 w-4 text-muted-foreground" /> })()}
              <h3 className="text-sm font-semibold">Триггер</h3>
            </div>

            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              {/* Trigger type */}
              <Field>
                <FieldLabel htmlFor={`${fieldId}-field-2`} className="text-xs text-muted-foreground">Тип триггера *</FieldLabel>
                <Select value={f.triggerType} onValueChange={handleTriggerTypeChange}>
                  <SelectTrigger id={`${fieldId}-field-2`} className="mt-1">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent><SelectGroup>
                    {TRIGGER_TYPE_OPTIONS.map(opt => (
                      <SelectItem key={opt.value} value={opt.value} description={opt.description}>
                        {opt.label}
                      </SelectItem>
                    ))}
                  </SelectGroup></SelectContent>
                </Select>
              </Field>

              {/* Event type — for entity_event */}
              {f.triggerType === "entity_event" && (
                <Field>
                  <FieldLabel htmlFor={`${fieldId}-field-3`} className="text-xs text-muted-foreground">Событие *</FieldLabel>
                  <Select value={f.eventType} onValueChange={(v) => { update({ eventType: v }) }}>
                    <SelectTrigger id={`${fieldId}-field-3`} className="mt-1"><SelectValue placeholder="Выберите событие" /></SelectTrigger>
                    <SelectContent><SelectGroup>
                      {ENTITY_EVENT_ACTIONS.map((a) => (
                        <SelectItem key={a.value} value={a.value}>{a.label}</SelectItem>
                      ))}
                    </SelectGroup></SelectContent>
                  </Select>
                </Field>
              )}

              {/* Free-text event — for business_event */}
              {f.triggerType === "business_event" && (
                <Field>
                  <FieldLabel htmlFor={`${fieldId}-field-4`} className="text-xs text-muted-foreground">Тип события *</FieldLabel>
                  <Input id={`${fieldId}-field-4`}
                    className="mt-1"
                    placeholder="business.currency_rates_loaded"
                    value={f.eventType}
                    onChange={(e) => { update({ eventType: e.target.value }) }}
                  />
                </Field>
              )}

              {/* Schedule configurator — for scheduled trigger */}
              {f.triggerType === "scheduled" && (
                <Field aria-labelledby={`${fieldId}-field-5`}>
                  <FieldLabel id={`${fieldId}-field-5`} className="text-xs text-muted-foreground">Расписание *</FieldLabel>
                  <ScheduleButton
                    value={f.cronExpression}
                    onChange={handleCronChange}
                    className="w-full mt-1"
                  />
                </Field>
              )}
            </div>

            {/* Entity multi-select — for entity_event */}
            {f.triggerType === "entity_event" && (
              <Field aria-labelledby={`${fieldId}-field-6`} className="mt-3">
                <FieldLabel id={`${fieldId}-field-6`} className="text-xs text-muted-foreground">Сущности</FieldLabel>
                <div className="mt-2 flex flex-col gap-2 rounded-md border p-3">
                  <label className="flex items-center gap-2 text-sm cursor-pointer">
                    <Checkbox
                      checked={f.targetEntities.length === 0}
                      onCheckedChange={() => update({ targetEntities: [] })}
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
              </Field>
            )}
          </div>

          {/* ── Reaction Section ──────────────────────────────────────── */}
          <div className="flex flex-col gap-4 rounded-lg border p-4">
            <h3 className="text-sm font-semibold">Действие</h3>

            <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
              <Field>
                <FieldLabel htmlFor={`${fieldId}-field-7`} className="text-xs text-muted-foreground">Тип действия *</FieldLabel>
                <Select value={f.reactionType} onValueChange={(v) => { update({ reactionType: v as RuleFormState["reactionType"] }) }}>
                  <SelectTrigger id={`${fieldId}-field-7`} className="mt-1"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectGroup>
                    <SelectItem value="notify" description="UI уведомления + внешние каналы">Уведомление</SelectItem>
                    <SelectItem value="webhook_call" description="HTTP POST/PUT/GET">Webhook API</SelectItem>
                    <SelectItem value="generate_report" description="XLSX отчёт → Email/Telegram">
                      <span className="flex items-center gap-1.5">
                        <BarChart3 className="h-3.5 w-3.5" />
                        Сформировать отчёт
                      </span>
                    </SelectItem>
                  </SelectGroup></SelectContent>
                </Select>
              </Field>

              {/* Severity selector — shown for notify reaction type */}
              {f.reactionType === "notify" && (
                <Field>
                  <FieldLabel htmlFor={`${fieldId}-field-8`} className="text-xs text-muted-foreground">Важность</FieldLabel>
                  <Select value={f.notifSeverity || "info"} onValueChange={(v) => { update({ notifSeverity: v }) }}>
                    <SelectTrigger id={`${fieldId}-field-8`} className="mt-1">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent><SelectGroup>
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
                    </SelectGroup></SelectContent>
                  </Select>
                </Field>
              )}
            </div>

            {/* Subscribers */}
            <SubscriberList
              subscribers={f.subscribers}
              channels={channels}
              reactionType={f.reactionType}
              onChange={handleSubscribersChange}
            />

            {/* Report Config — shown for generate_report reaction */}
            {f.reactionType === "generate_report" && (
              <div className="mt-4 flex flex-col gap-4 rounded-md border border-dashed border-primary/30 bg-primary/5 p-4">
                <div className="flex items-center gap-2">
                  <BarChart3 className="h-4 w-4 text-primary" />
                  <h3 className="text-sm font-semibold text-primary">Настройка отчёта</h3>
                </div>

                <p className="text-sm text-muted-foreground">Отчёт выполняется с текущими правами пользователя, сохранившего правило.</p>

                <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                  {/* Dataset selector */}
                  <Field>
                    <FieldLabel htmlFor={`${fieldId}-field-9`} className="text-xs text-muted-foreground">Набор данных *</FieldLabel>
                    <Select value={f.reportDatasetKey} onValueChange={(v) => { update({ reportDatasetKey: v }) }}>
                      <SelectTrigger id={`${fieldId}-field-9`} className="mt-1"><SelectValue placeholder="Выберите отчёт" /></SelectTrigger>
                      <SelectContent><SelectGroup>
                        {datasets.map(ds => (
                          <SelectItem key={ds.key} value={ds.key}>{ds.name}</SelectItem>
                        ))}
                      </SelectGroup></SelectContent>
                    </Select>
                  </Field>

                  {/* Period type */}
                  <Field>
                    <FieldLabel htmlFor={`${fieldId}-field-10`} className="text-xs text-muted-foreground">Период *</FieldLabel>
                    <Select value={f.reportPeriodType} onValueChange={(v) => { update({ reportPeriodType: v as PeriodType }) }}>
                      <SelectTrigger id={`${fieldId}-field-10`} className="mt-1"><SelectValue /></SelectTrigger>
                      <SelectContent><SelectGroup>
                        {PERIOD_TYPE_OPTIONS.map(opt => (
                          <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                        ))}
                      </SelectGroup></SelectContent>
                    </Select>
                  </Field>

                  {/* Custom days — shown only for custom_days period */}
                  {f.reportPeriodType === "custom_days" && (
                    <Field>
                      <FieldLabel htmlFor={`${fieldId}-field-11`} className="text-xs text-muted-foreground">Количество дней *</FieldLabel>
                      <Input id={`${fieldId}-field-11`}
                        type="number"
                        className="mt-1"
                        min={1}
                        value={f.reportCustomDays}
                        onChange={(e) => { update({ reportCustomDays: Number(e.target.value) }) }}
                      />
                    </Field>
                  )}

                  {/* Timezone override */}
                  <Field>
                    <FieldLabel htmlFor={`${fieldId}-field-12`} className="text-xs text-muted-foreground">Часовой пояс</FieldLabel>
                    <Input id={`${fieldId}-field-12`}
                      className="mt-1"
                      placeholder="По умолчанию из настроек"
                      value={f.reportTimezone}
                      onChange={(e) => { update({ reportTimezone: e.target.value }) }}
                    />
                    <p className="text-[10px] text-muted-foreground mt-1">Например: Europe/Moscow. Пусто = из настроек тенанта.</p>
                  </Field>
                </div>

                {/* Skip empty toggle */}
                <div className="flex items-center gap-2">
                  <Switch id={`${fieldId}-skip-empty`} checked={f.reportSkipEmpty} onCheckedChange={(v) => { update({ reportSkipEmpty: v }) }} />
                  <FieldLabel htmlFor={`${fieldId}-skip-empty`} className="text-sm">Пропускать пустые отчёты</FieldLabel>
                </div>
              </div>
            )}
          </div>

          {/* ── CEL Condition — hidden for scheduled ────────────────── */}
          {f.triggerType !== "scheduled" && (
            <Field aria-labelledby={`${fieldId}-field-13`} className="flex flex-col gap-2">
              <FieldLabel id={`${fieldId}-field-13`} className="text-xs text-muted-foreground font-semibold">Условие выполнения (CEL)</FieldLabel>
              <div className="border rounded-md overflow-hidden h-[120px]">
                <Editor
                  defaultLanguage="go"
                  value={f.conditionCel || ""}
                  onChange={(v) => { update({ conditionCel: v || "" }) }}
                  onMount={handleEditorMount}
                  options={{ minimap: { enabled: false }, lineNumbers: "off", scrollBeyondLastLine: false, fontSize: 13, quickSuggestions: true, suggestOnTriggerCharacters: true }}
                />
              </div>
              <p className="text-[10px] text-muted-foreground">
                Используйте <code>doc.</code> для автозаполнения полей сущности. Суммы проверяйте в минимальных единицах валюты. Пример: <code>int(doc.totalAmount) &gt; 1000000 &amp;&amp; action == &apos;posted&apos;</code>
              </p>
            </Field>
          )}

          {/* ── Message Template ──────────────────────────────────────── */}
          <Field aria-labelledby={`${fieldId}-field-14`} className="flex flex-col gap-2">
            <FieldLabel id={`${fieldId}-field-14`} className="text-xs text-muted-foreground font-semibold">Текст сообщения (Go Text Template)</FieldLabel>
            <div className="border rounded-md overflow-hidden h-[200px]">
              <Editor
                defaultLanguage="handlebars"
                value={f.actionTemplate || ""}
                onChange={(v) => { update({ actionTemplate: v || "" }) }}
                options={{ minimap: { enabled: false }, scrollBeyondLastLine: false, fontSize: 13 }}
              />
            </div>
            <p className="text-[10px] text-muted-foreground">
              {f.triggerType === "scheduled"
                ? <>Доступно: <code>{"{{ .doc.ruleName }}"}</code>, <code>{"{{ .doc.ruleId }}"}</code>. </>
                : <>Доступно: <code>{"{{ .doc }}"}</code>, <code>{"{{ .action }}"}</code>. Функции: <code>{"{{ .doc | json }}"}</code>, <code>{"{{ money .doc.totalAmount .currency.decimalPlaces }}"}</code></>
              }
            </p>
          </Field>

          {/* ── Additional Settings (Collapsible) ─────────────────────── */}
          <Collapsible open={settingsOpen} onOpenChange={setSettingsOpen}>
            <CollapsibleTrigger className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors">
              <ChevronDown className={`h-4 w-4 transition-transform ${settingsOpen ? "" : "-rotate-90"}`} />
              Дополнительные настройки
            </CollapsibleTrigger>
            <CollapsibleContent className="mt-3">
              <div className="grid grid-cols-1 gap-4 md:grid-cols-3 rounded-lg border p-4">
                <Field>
                  <FieldLabel htmlFor={`${fieldId}-field-15`} className="text-xs text-muted-foreground">Описание</FieldLabel>
                  <Textarea id={`${fieldId}-field-15`}
                    className="mt-1 h-20"
                    placeholder="Описание правила…"
                    value={f.description}
                    onChange={(e) => { update({ description: e.target.value }) }}
                  />
                </Field>
                <div className="flex flex-col gap-3">
                  <Field>
                    <FieldLabel htmlFor={`${fieldId}-field-16`} className="text-xs text-muted-foreground">Приоритет</FieldLabel>
                    <Input id={`${fieldId}-field-16`} type="number" className="mt-1" value={f.priority} onChange={(e) => { update({ priority: Number(e.target.value) }) }} />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor={`${fieldId}-field-17`} className="text-xs text-muted-foreground">Макс. ретраев</FieldLabel>
                    <Input id={`${fieldId}-field-17`} type="number" className="mt-1" value={f.maxRetries} onChange={(e) => { update({ maxRetries: Number(e.target.value) }) }} />
                  </Field>
                </div>
                <div className="flex flex-col gap-3">
                  <Field>
                    <FieldLabel htmlFor={`${fieldId}-field-18`} className="text-xs text-muted-foreground">Cooldown (сек.)</FieldLabel>
                    <Input id={`${fieldId}-field-18`} type="number" className="mt-1" value={f.cooldownSeconds} onChange={(e) => { update({ cooldownSeconds: Number(e.target.value) }) }} />
                    <p className="text-[10px] text-muted-foreground mt-1">0 = без ограничений</p>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor={`${fieldId}-field-19`} className="text-xs text-muted-foreground">Формат</FieldLabel>
                    <Select value={f.messageFormat} onValueChange={(v) => { update({ messageFormat: v as RuleFormState["messageFormat"] }) }}>
                      <SelectTrigger id={`${fieldId}-field-19`} className="mt-1"><SelectValue /></SelectTrigger>
                      <SelectContent><SelectGroup>
                        <SelectItem value="text">Text</SelectItem>
                        <SelectItem value="markdown">Markdown</SelectItem>
                        <SelectItem value="html">HTML</SelectItem>
                      </SelectGroup></SelectContent>
                    </Select>
                  </Field>
                </div>
              </div>
            </CollapsibleContent>
          </Collapsible>

        </FieldGroup>
      </ScrollArea>
  )
}
