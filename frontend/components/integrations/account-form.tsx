"use client"

import { useRef, useState } from "react"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { CalendarClock, Check, ChevronDown, ChevronsUpDown, HelpCircle, LockKeyhole, Plus, Trash2 } from "lucide-react"
import { useIntegrationProviders } from "@/hooks/useIntegrationProviders"
import { useCatalogForm } from "@/hooks/useCatalogForm"
import { useCloseTab } from "@/hooks/useCloseTab"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useShortcut } from "@/hooks/useShortcut"
import { usePermissions } from "@/hooks/use-permissions"
import { createFormDraftKey, useFormDraftStore } from "@/stores/useFormDraftStore"
import { integrationAccounts } from "@/lib/api/integrations"
import { cn } from "@/lib/utils"
import { credentialProfile, emptyCredentialKeys, expiryInputValue, expiryRequestValue, nextQuotaPeriod, normalizeCredentialKey, prepareCredentialKeys, quotaTimezones, validQuotaTimezone, type CredentialKey } from "@/lib/integrations/account-settings"
import type { EntityFormProps } from "@/lib/entity-registry"
import type * as Wire from "@/types/generated/http-contracts"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { Field, FieldGroup, FieldLabel, FieldDescription, FieldError, FieldSet, FieldLegend } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Button, buttonVariants } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog"
import { AlertDialog, AlertDialogContent, AlertDialogHeader, AlertDialogTitle, AlertDialogDescription, AlertDialogFooter, AlertDialogCancel, AlertDialogAction } from "@/components/ui/alert-dialog"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { ScrollArea } from "@/components/ui/scroll-area"
import { AccountPoolDialog } from "./account-pool-dialog"

import { IntegrationProviderFields } from "./provider-fields"

interface State {
  code: string; name: string; kind: string; provider: string; externalAccountRef: string; isActive: boolean
  credentialsExpiresAt: string; credentialsExpiryInput: string; resetDay: string; resetTimezone: string; version: number; hasCredentials: boolean
}
interface SecretDraft { provider: string; keys: CredentialKey[] }
const draftKey = createFormDraftKey<State>("integration-account")
const initialState: State = { code: "", name: "", kind: "", provider: "", externalAccountRef: "", isActive: true, credentialsExpiresAt: "", credentialsExpiryInput: "", resetDay: "1", resetTimezone: "UTC", version: 1, hasCredentials: false }
const fromResponse = (r: Wire.IntegrationAccountResponse): State => ({ code: r.code, name: r.name, kind: "", provider: r.provider, externalAccountRef: r.externalAccountRef, isActive: r.isActive, credentialsExpiresAt: r.credentialsExpiresAt ?? "", credentialsExpiryInput: expiryInputValue(r.credentialsExpiresAt), resetDay: String(r.resetDay), resetTimezone: r.resetTimezone, version: r.version, hasCredentials: r.hasCredentials })
const listPath = "/catalogs/integration-accounts"

export default function IntegrationAccountForm({ id, entityName }: EntityFormProps) {
  const registry = useIntegrationProviders()
  const searchParams = useSearchParams()
  const emptyKeys = (provider: string) => emptyCredentialKeys(provider, registry.providers)
  const prepareKeys = (provider: string, input: readonly CredentialKey[]) => prepareCredentialKeys(provider, input, registry.providers)
  // Secrets never enter useCatalogForm, useFormDraft or browser storage.
  const [secretDraft, setSecretDraft] = useState<SecretDraft>({ provider: "", keys: [] })
  const [keyAction, setKeyAction] = useState<"keep" | "replace" | "delete">("keep")
  const [keyErrors, setKeyErrors] = useState<Record<string, string>>({})
  const [normalized, setNormalized] = useState<Record<number, boolean>>({})
  const [savedConfigured, setSavedConfigured] = useState<boolean | null>(null)
  const [helpOpen, setHelpOpen] = useState(false)
  const [zoneOpen, setZoneOpen] = useState(false)
  const [zoneSearch, setZoneSearch] = useState("")
  const [pendingSelection, setPendingSelection] = useState<{ kind: string; provider: string } | null>(null)
  const [confirmation, setConfirmation] = useState<"delete" | "discard" | "leave" | null>(null)
  const [resetting, setResetting] = useState(false)
  const metadataChanged = useRef(false)
  const dirtyBeforeKeys = useRef(false)
  const { has } = usePermissions()
  const { isDirty, markClean } = useTabDirty()
  const { closeOne } = useCloseTab()
  const pathname = usePathname()
  const router = useRouter()
  const clearDraft = useFormDraftStore(s => s.clearDraft)
  const canWrite = has(`catalog:integration_account:${id && id !== "new" ? "update" : "create"}`)
  const resetSecrets = () => { setSecretDraft({ provider: "", keys: [] }); setKeyAction("keep"); setKeyErrors({}); setNormalized({}) }
  const credentials = (provider: string): Wire.IntegrationsCredentialsInput | undefined => keyAction === "delete" ? { keys: [] } : keyAction === "replace" ? { keys: prepareKeys(provider, secretDraft.provider === provider ? secretDraft.keys : emptyKeys(provider)).keys } : undefined
  const fields = (s: State): Wire.CreateIntegrationAccountRequest => ({ code: s.code, name: s.name, provider: s.provider, externalAccountRef: s.externalAccountRef, isActive: s.isActive, resetDay: Number(s.resetDay), resetTimezone: s.resetTimezone, credentials: credentials(s.provider), credentialsExpiresAt: registry.providers.find(p => p.id === s.provider)?.quotaPolicy === "manual" ? (keyAction === "replace" || !id || id === "new" ? expiryRequestValue(s.credentialsExpiryInput) : s.credentialsExpiresAt || null) : null })
  const safeApi = { ...integrationAccounts,
    create: async (request: Wire.CreateIntegrationAccountRequest) => { const result = await integrationAccounts.create(request); resetSecrets(); return result },
    update: async (uid: string, request: Wire.UpdateIntegrationAccountRequest) => {
      const result = await integrationAccounts.update(uid, request)
      setSavedConfigured(result.hasCredentials); metadataChanged.current = false; resetSecrets()
      return result
    },
  }
  const form = useCatalogForm<State, Wire.IntegrationAccountResponse, Wire.CreateIntegrationAccountRequest, Wire.UpdateIntegrationAccountRequest>({
    draftKey, entityName, entityKey: "integration_account", initialState: { ...initialState, kind: searchParams.get("kind") ?? "" }, api: safeApi, listPath, replaceOnSave: true, checkDraftOnLoad: true,
    mapToCreate: fields, mapToUpdate: s => ({ ...fields(s), version: s.version }), mapFromResponse: fromResponse,
    getVersion: r => r.version, getDeletionMark: r => r.deletionMark, titleField: s => s.name,
    fieldRules: [
      { field: "credentialsExpiryInput", validate: s => {
        if (registry.providers.find(p => p.id === s.provider)?.quotaPolicy !== "manual" || keyAction === "delete" || (id && id !== "new" && keyAction !== "replace")) return null
        const value = expiryRequestValue(s.credentialsExpiryInput)
        if (!value || Date.parse(value) <= Date.now()) return "Укажите новую дату окончания в будущем (UTC)."
        return s.credentialsExpiresAt && Date.parse(value) === Date.parse(s.credentialsExpiresAt) ? "Для продления укажите новую дату окончания." : null
      } },
      { field: "kind", validate: s => !id && !s.kind && !s.provider ? "Выберите назначение интеграции." : null },
      { field: "provider", validate: s => registry.providers.some(p => p.id === s.provider && (id || p.canCreate) && (id || p.kinds.includes(s.kind || p.kinds[0] || ""))) ? null : "Выберите провайдера выбранного назначения." },
      { field: "name", validate: s => s.name.trim() ? null : "Укажите наименование аккаунта." },
      { field: "resetDay", validate: s => Number.isInteger(Number(s.resetDay)) && Number(s.resetDay) >= 1 && Number(s.resetDay) <= 31 ? null : "Укажите целое число от 1 до 31." },
      { field: "resetTimezone", validate: s => validQuotaTimezone(s.resetTimezone) ? null : "Выберите часовой пояс из списка." },
    ],
    validate: s => {
      if (!registry.data) return "Не удалось загрузить список провайдеров. Повторите загрузку."
      const needsKeys = keyAction === "replace" || ((!id || id === "new") && !!credentialProfile(s.provider, registry.providers))
      const errors = needsKeys ? prepareKeys(s.provider, secretDraft.provider === s.provider ? secretDraft.keys : emptyKeys(s.provider)).errors : {}
      setKeyErrors(errors)
      const first = Object.keys(errors)[0]
      if (!first) return null
      requestAnimationFrame(() => { const input = document.getElementById(`integration-${first}`); input?.focus(); input?.scrollIntoView({ block: "center", behavior: "smooth" }) })
      return "Проверьте поля в разделе «Ключи доступа»."
    },
  })
  const provider = form.f.provider
  const definition = registry.providers.find(p => p.id === provider)
  const kind = form.f.kind || definition?.kinds[0] || ""
  const profile = credentialProfile(provider, registry.providers)
  const providerLabel = definition?.label ?? provider
  const keys = secretDraft.provider === provider && secretDraft.keys.length ? secretDraft.keys : emptyKeys(provider)
  const configured = savedConfigured ?? form.f.hasCredentials
  const editingKeys = keyAction === "replace" || !configured
  const busy = form.saving || resetting || !registry.data
  const upcoming = definition?.quotaPolicy === "package" ? "Без календарного сброса" : definition?.quotaPolicy === "daily_utc" ? "Ежедневно, 00:00 UTC" : nextQuotaPeriod(Number(form.f.resetDay), form.f.resetTimezone)
  const timezoneOptions = quotaTimezones(form.f.resetTimezone).filter(zone => zone.toLowerCase().includes(zoneSearch.trim().toLowerCase()))

  const change = (patch: Partial<State>) => { metadataChanged.current = true; form.update(patch); form.handleChange() }
  const beginKeyEdit = () => { if (keyAction === "keep") dirtyBeforeKeys.current = isDirty; setKeyAction("replace"); form.handleChange() }
  const replaceKeys = () => {
    beginKeyEdit(); if (definition?.quotaPolicy === "manual") form.update({ credentialsExpiryInput: "" }); setSecretDraft({ provider, keys: emptyKeys(provider) }); setKeyErrors({}); setNormalized({})
    requestAnimationFrame(() => document.getElementById("integration-key-value-0")?.focus())
  }
  const cancelKeys = () => { form.update({ credentialsExpiryInput: expiryInputValue(form.f.credentialsExpiresAt) }); resetSecrets(); form.clearErrors(); if (!dirtyBeforeKeys.current && !metadataChanged.current) markClean() }
  const secretChange = (index: number, patch: Partial<CredentialKey>) => {
    beginKeyEdit(); setSecretDraft({ provider, keys: keys.map((key, i) => i === index ? { ...key, ...patch } : key) })
    setKeyErrors(previous => { const next = { ...previous }; delete next[`key-name-${index}`]; delete next[`key-value-${index}`]; delete next.credentials; return next })
    setNormalized(previous => ({ ...previous, [index]: false }))
  }
  const normalizeToken = (index: number, raw: string) => {
    const result = normalizeCredentialKey(raw, keys[index].name, definition?.credentials)
    if (!result.ok) {
      // Keep invalid pasted input in memory so a later save cannot silently
      // submit the previous, valid token that the user intended to replace.
      secretChange(index, { value: raw })
      setKeyErrors(previous => ({ ...previous, [`key-value-${index}`]: result.error })); return
    }
    secretChange(index, { value: result.value }); setNormalized(previous => ({ ...previous, [index]: result.fromUrl }))
  }
  const applySelection = (selection: { kind: string; provider: string }) => {
    change(selection); resetSecrets(); setPendingSelection(null); form.clearErrors()
  }
  const changeSelection = (selection: { kind: string; provider: string }) => {
    if (selection.provider === provider) { change({ kind: selection.kind }); return }
    if (keys.some(key => key.value)) setPendingSelection(selection)
    else applySelection(selection)
  }
  const changeKind = (nextKind: string) => changeSelection({ kind: nextKind, provider: definition?.canCreate && definition.kinds.includes(nextKind) ? provider : "" })
  const changeProvider = (nextProvider: string) => changeSelection({ kind, provider: nextProvider })
  const leave = () => { if (busy) return; if (isDirty) setConfirmation("leave"); else router.push(listPath) }
  const discard = async () => {
    setConfirmation(null); setResetting(true)
    if (await form.reset()) { resetSecrets(); setSavedConfigured(null); metadataChanged.current = false }
    setResetting(false)
  }
  const leaveWithoutSaving = () => {
    resetSecrets(); markClean(); clearDraft(pathname); setConfirmation(null)
    if (!closeOne(pathname)) router.push(listPath)
  }
  const save = (andClose = false) => { if (canWrite && !busy && (!form.isEditMode || isDirty)) void form.handleSave(andClose) }
  const shortcutsEnabled = canWrite && !busy && !form.loading && !helpOpen && !confirmation && !pendingSelection
  useShortcut("integration-account.save", "mod+s", "Записать аккаунт", "editing", () => save(), { enabled: shortcutsEnabled, priority: 10 })
  useShortcut("integration-account.save-close", "mod+shift+s", "Записать и закрыть аккаунт", "editing", () => save(true), { enabled: shortcutsEnabled, priority: 10 })
  const fieldError = (key: string) => form.fieldErrors[key] ? <FieldError id={`integration-${key}-error`}>{form.fieldErrors[key]}</FieldError> : null
  const keyError = (key: string) => keyErrors[key] ? <FieldError id={`integration-${key}-error`}>{keyErrors[key]}</FieldError> : null

  if (form.loading || registry.isLoading) return <Skeleton className="h-48" />
  return <div className="flex h-full min-h-0 flex-col overflow-hidden">
    <FormToolbar title={form.formTitle} backHref={listPath} onClose={leave} onHelp={() => setHelpOpen(true)}
      primaryAction={{ label: form.saving && !resetting ? "Сохраняем…" : "Записать", onClick: () => save(), disabled: busy || !canWrite || (form.isEditMode && !isDirty) }}
      secondaryActions={[{ label: "Записать и закрыть", onClick: () => save(true), disabled: busy || !canWrite || (form.isEditMode && !isDirty) }, ...(isDirty ? [{ label: "Отменить изменения", onClick: () => setConfirmation("discard"), disabled: busy }] : [])]}/>
    <ScrollArea className="min-h-0 flex-1">
      <div className="max-w-5xl p-4 sm:p-6"><FieldGroup className="gap-6">
        {registry.error && <Alert variant="destructive"><AlertTitle>Не удалось загрузить провайдеров</AlertTitle><AlertDescription><Button variant="outline" onClick={() => void registry.mutate()}>Повторить загрузку</Button></AlertDescription></Alert>}
        {form.error && <Alert variant="destructive"><AlertTitle>Не удалось завершить действие</AlertTitle><AlertDescription>{form.error}</AlertDescription></Alert>}
        <FieldSet disabled={!canWrite || busy} className="gap-4"><FieldLegend>Учётная запись</FieldLegend>
          <FieldGroup className="grid gap-4 md:grid-cols-2">
            <IntegrationProviderFields prefix="integration" kinds={registry.kinds} providers={registry.providers} kind={kind} provider={provider} locked={form.isEditMode} error={form.fieldErrors.provider} kindError={form.fieldErrors.kind} onKindChange={changeKind} onProviderChange={changeProvider} />
            {provider && <>
            <Field className="gap-2 md:col-span-2" data-invalid={!!form.fieldErrors.name}><FieldLabel htmlFor="integration-name">Наименование <span aria-hidden="true">*</span></FieldLabel><Input id="integration-name" aria-label="Наименование" required value={form.f.name} {...form.fieldProps("name")} aria-invalid={!!form.fieldErrors.name} aria-describedby={form.fieldErrors.name ? "integration-name-error" : undefined} onChange={e => change({ name: e.target.value })}/>{fieldError("name")}</Field>
            <Field className="gap-2"><FieldLabel htmlFor="integration-reference">ID аккаунта у провайдера</FieldLabel><Input id="integration-reference" autoComplete="off" value={form.f.externalAccountRef} onChange={e => change({ externalAccountRef: e.target.value })} aria-describedby="integration-reference-help"/><FieldDescription id="integration-reference-help">Все ключи этого аккаунта используют общую квоту.</FieldDescription></Field>
            <Field orientation="horizontal" className="flex-wrap justify-between md:col-span-2"><div className="flex min-w-0 flex-1 flex-col gap-1"><FieldLabel htmlFor="integration-active">Использование в интеграциях</FieldLabel><FieldDescription id="integration-active-help">Изменение применится после записи. Эта настройка не подтверждает доступность провайдера.</FieldDescription></div><div className="flex items-center gap-2"><span className="text-sm text-muted-foreground">{form.f.isActive ? "Включено" : "Отключено"}</span><Switch id="integration-active" checked={form.f.isActive} aria-describedby="integration-active-help" onCheckedChange={isActive => change({ isActive })}/></div></Field>
            <Collapsible className="md:col-span-2"><CollapsibleTrigger asChild><Button type="button" variant="ghost" size="sm"><ChevronDown data-icon="inline-start"/>Служебный код</Button></CollapsibleTrigger><CollapsibleContent className="pt-3"><Field className="gap-2"><FieldLabel htmlFor="integration-code">Код</FieldLabel><Input id="integration-code" value={form.f.code} onChange={e => change({ code: e.target.value })}/>{!form.isEditMode && !form.f.code && <FieldDescription>Назначим автоматически при записи.</FieldDescription>}</Field></CollapsibleContent></Collapsible>
            </>}
          </FieldGroup>
        </FieldSet>
        {provider && <>
        <Separator/>
        <FieldSet disabled={!canWrite || busy} className="gap-4"><FieldLegend>Ключи доступа</FieldLegend>
          {profile && <FieldDescription>{profile.description}</FieldDescription>}
          {(configured || !editingKeys) && <div className="flex flex-wrap items-center gap-3"><LockKeyhole className="size-5 shrink-0 text-muted-foreground" aria-hidden="true"/><div className="flex min-w-0 flex-1 flex-col gap-1"><p className="text-sm font-medium">{keyAction === "delete" ? "Ключи будут удалены после записи" : configured ? "Ключи сохранены" : "Ключи не добавлены"}</p><p className="text-sm text-muted-foreground">{keyAction === "delete" ? "До записи сохранённые ключи не изменятся." : configured ? "Их значения скрыты. Повторный ввод не требуется." : "Добавьте ключи для запросов к провайдеру."}</p></div>{keyAction === "keep" && <Button type="button" variant="outline" size="sm" onClick={replaceKeys}>{configured ? profile?.replaceLabel ?? "Заменить ключи…" : "Добавить ключи"}</Button>}{keyAction === "delete" && <Button type="button" variant="outline" size="sm" onClick={cancelKeys}>Отменить удаление</Button>}</div>}
          {editingKeys && keyAction !== "delete" && <FieldGroup className="gap-4">
            {configured && <Alert><AlertTitle>Замена всех ключей</AlertTitle><AlertDescription>После записи новый набор заменит все сохранённые ключи. Добавьте все ключи, которые должны остаться у аккаунта.</AlertDescription></Alert>}
            {!profile && <FieldDescription>{definition?.credentials.description || "Для основного подключения используйте имя default."}</FieldDescription>}
            {keyErrors.credentials && <FieldError>{keyErrors.credentials}</FieldError>}
            {keys.map((key, index) => <FieldGroup key={index} className={cn(profile ? "gap-2" : "grid gap-3 md:grid-cols-[1fr_1.5fr_auto]")}>
              {!profile && <Field className="gap-2" data-invalid={!!keyErrors[`key-name-${index}`]}><FieldLabel htmlFor={`integration-key-name-${index}`}>Имя ключа {index + 1}</FieldLabel><Input id={`integration-key-name-${index}`} autoComplete="off" value={key.name} aria-invalid={!!keyErrors[`key-name-${index}`]} aria-describedby={keyErrors[`key-name-${index}`] ? `integration-key-name-${index}-error` : undefined} onChange={e => secretChange(index, { name: e.target.value })}/>{keyError(`key-name-${index}`)}</Field>}
              <Field className="gap-2" data-invalid={!!keyErrors[`key-value-${index}`]}><FieldLabel htmlFor={`integration-key-value-${index}`}>{profile?.fields[index]?.label ?? "Значение ключа"}{profile && <span aria-hidden="true">*</span>}</FieldLabel><Input id={`integration-key-value-${index}`} type="password" autoComplete="new-password" spellCheck={false} value={key.value} required={!!profile}
                aria-label={profile?.fields[index]?.label ?? "Значение ключа"} aria-invalid={!!keyErrors[`key-value-${index}`]} aria-describedby={`${profile ? `integration-key-help-${index} ` : ""}${keyErrors[`key-value-${index}`] ? `integration-key-value-${index}-error` : ""}`.trim() || undefined}
                onChange={e => secretChange(index, { value: e.target.value })} onPaste={profile ? e => { e.preventDefault(); normalizeToken(index, e.clipboardData.getData("text")) } : undefined}
                onBlur={profile && key.value ? () => normalizeToken(index, key.value) : undefined}/>
                {profile && <FieldDescription id={`integration-key-help-${index}`}>{profile.fields[index]?.description}</FieldDescription>}
                {normalized[index] && <p role="status" className="flex items-center gap-1 text-xs text-muted-foreground"><Check className="size-3" aria-hidden="true"/>Из ссылки извлечён токен. Адрес не сохраняется.</p>}{keyError(`key-value-${index}`)}
              </Field>
              {!profile && <Button type="button" variant="ghost" size="icon" className="md:mt-6" aria-label={`Удалить строку ключа ${index + 1}`} disabled={keys.length === 1} onClick={() => { beginKeyEdit(); setSecretDraft({ provider, keys: keys.filter((_, i) => i !== index) }); setKeyErrors({}) }}><Trash2 data-icon="inline-start"/></Button>}
            </FieldGroup>)}
            <div className="flex flex-wrap gap-2">{!profile && <Button type="button" variant="outline" size="sm" disabled={keys.length >= 32} onClick={() => { beginKeyEdit(); setSecretDraft({ provider, keys: [...keys, { name: "", value: "" }] }) }}><Plus data-icon="inline-start"/>Добавить ключ</Button>}{configured && <Button type="button" variant="ghost" size="sm" onClick={cancelKeys}>Отменить замену</Button>}</div>
          </FieldGroup>}
          {configured && keyAction === "keep" && <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-3"><p className="text-xs text-muted-foreground">Удаление затронет все ключи этого аккаунта.</p><Button type="button" variant="ghost" size="sm" onClick={() => setConfirmation("delete")}><Trash2 data-icon="inline-start"/>Удалить все ключи…</Button></div>}
          <p className="flex items-start gap-2 text-xs text-muted-foreground"><HelpCircle className="size-4 shrink-0" aria-hidden="true"/>Проверка подключения доступна в карточке пула. Наличие сохранённых ключей не подтверждает доступность провайдера.</p>
        </FieldSet>
        <Separator/>
        {definition?.quotaPolicy === "manual" ? <FieldSet disabled={!canWrite || busy} className="gap-4"><FieldLegend>Срок действия ключа</FieldLegend>
          <Field data-invalid={!!form.fieldErrors.credentialsExpiryInput}>
            <FieldLabel htmlFor="integration-credentialsExpiryInput">Действует до (UTC)</FieldLabel>
            <Input id="integration-credentialsExpiryInput" {...form.fieldProps("credentialsExpiryInput")} type="datetime-local" step={1} value={form.f.credentialsExpiryInput} readOnly={configured && keyAction !== "replace"} aria-invalid={!!form.fieldErrors.credentialsExpiryInput} aria-describedby={form.fieldErrors.credentialsExpiryInput ? "integration-credentialsExpiryInput-error" : "integration-expiry-help"} onChange={e => { beginKeyEdit(); form.update({ credentialsExpiryInput: e.target.value }); form.handleChange() }} />
            <FieldDescription id="integration-expiry-help">Часовой пояс UTC. Продление требует нового ключа и новой даты. После окончания срока доступ автоматически не восстановится.</FieldDescription>
            {fieldError("credentialsExpiryInput")}
          </Field>
          {form.f.credentialsExpiresAt && <FieldDescription>Сохранённый срок: {new Date(form.f.credentialsExpiresAt).toLocaleString()} · {Intl.DateTimeFormat().resolvedOptions().timeZone}</FieldDescription>}
          {form.f.credentialsExpiresAt && Date.parse(form.f.credentialsExpiresAt) <= Date.now() && <Alert variant="destructive"><AlertTitle>Срок действия истёк</AlertTitle><AlertDescription>Для восстановления сохраните новый ключ и новую дату окончания.</AlertDescription></Alert>}
        </FieldSet> : <FieldSet disabled={!canWrite || busy} className="gap-4"><FieldLegend>Период квоты</FieldLegend>{definition?.quotaPolicy === "monthly" && <FieldGroup className="grid gap-4 md:grid-cols-2">
          <Field className="gap-2" data-invalid={!!form.fieldErrors.resetDay}><FieldLabel htmlFor="integration-resetDay">День обновления квоты</FieldLabel><Input id="integration-resetDay" type="number" min={1} max={31} step={1} inputMode="numeric" value={form.f.resetDay} {...form.fieldProps("resetDay")} aria-invalid={!!form.fieldErrors.resetDay} aria-describedby={form.fieldErrors.resetDay ? "integration-resetDay-error" : "integration-day-help"} onChange={e => change({ resetDay: e.target.value })}/><FieldDescription id="integration-day-help">Число месяца от 1 до 31.</FieldDescription>{fieldError("resetDay")}</Field>
          <Field className="gap-2" data-field="resetTimezone" data-invalid={!!form.fieldErrors.resetTimezone}><FieldLabel htmlFor="integration-timezone">Часовой пояс провайдера</FieldLabel><Popover open={zoneOpen} onOpenChange={value => { setZoneOpen(value); setZoneSearch("") }}><PopoverTrigger asChild><Button type="button" id="integration-timezone" variant="outline" role="combobox" aria-expanded={zoneOpen} aria-invalid={!!form.fieldErrors.resetTimezone} className="w-full justify-between"><span className="truncate">{form.f.resetTimezone}</span><ChevronsUpDown data-icon="inline-end"/></Button></PopoverTrigger><PopoverContent align="start" className="w-80 max-w-[calc(100vw-2rem)] p-0"><Command shouldFilter={false}><CommandInput placeholder="Поиск часового пояса…" aria-label="Поиск часового пояса" value={zoneSearch} onValueChange={setZoneSearch}/><CommandList><CommandEmpty>Часовой пояс не найден</CommandEmpty><CommandGroup>{timezoneOptions.map(zone => <CommandItem key={zone} value={zone} onSelect={() => { change({ resetTimezone: zone }); setZoneOpen(false); setZoneSearch("") }}>{zone === form.f.resetTimezone && <Check data-icon="inline-start"/>}{zone}</CommandItem>)}</CommandGroup></CommandList></Command></PopoverContent></Popover><FieldDescription>Определяет местное время начала периода.</FieldDescription>{fieldError("resetTimezone")}</Field>
        </FieldGroup>}
          <div className="flex flex-wrap items-center gap-3 rounded-lg bg-muted/50 p-4" aria-live="polite"><CalendarClock className="size-5 shrink-0 text-muted-foreground" aria-hidden="true"/><div className="flex min-w-0 flex-1 flex-col gap-1"><p className="text-xs text-muted-foreground">Следующий период</p><p className="break-words text-sm font-medium tabular-nums">{definition?.quotaPolicy !== "monthly" ? upcoming : upcoming ? `${upcoming} · ${form.f.resetTimezone}` : "Укажите корректный день и часовой пояс"}</p></div>{isDirty && <Badge variant="outline">После записи</Badge>}</div>
          <FieldDescription>{definition?.quotaPolicy === "package" ? "Проверки расходуются из приобретённого пакета. Новый месяц не восстанавливает остаток." : definition?.quotaPolicy === "daily_utc" ? "Провайдер обновляет суточную квоту в 00:00 UTC. Дата оплаты подписки на этот период не влияет." : "Период начинается ежемесячно в 00:00. Если выбранного числа нет в месяце, используется последний день месяца."}</FieldDescription>
        </FieldSet>}
        {id && id !== "new" && configured && has("catalog:integration_pool:read") && has("catalog:integration_pool:update") && <AccountPoolDialog accountId={id} accountName={form.f.name} provider={provider} providerLabel={providerLabel} disabled={busy || isDirty || keyAction !== "keep"}/>}
        </>}
      </FieldGroup></div>
    </ScrollArea>
    <AlertDialog open={confirmation !== null} onOpenChange={open => { if (!open) setConfirmation(null) }}><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>{confirmation === "delete" ? "Удалить все ключи аккаунта?" : confirmation === "leave" ? "Закрыть без записи?" : "Отменить изменения?"}</AlertDialogTitle><AlertDialogDescription>{confirmation === "delete" ? "После записи все ключи будут удалены из Metapus. Для повторного использования потребуется ввести их заново. Ключи у провайдера не удаляются." : "Несохранённые изменения и введённые значения ключей будут потеряны. Сохранённые настройки останутся прежними."}</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>Продолжить редактирование</AlertDialogCancel><AlertDialogAction className={confirmation === "delete" ? buttonVariants({ variant: "destructive" }) : undefined} onClick={() => {
      if (confirmation === "delete") { dirtyBeforeKeys.current = isDirty; resetSecrets(); setKeyAction("delete"); form.handleChange(); setConfirmation(null) }
      else if (confirmation === "leave") leaveWithoutSaving()
      else void discard()
    }}>{confirmation === "delete" ? "Удалить после записи" : confirmation === "leave" ? "Закрыть без записи" : "Отменить изменения"}</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>
    <AlertDialog open={pendingSelection !== null} onOpenChange={open => { if (!open) setPendingSelection(null) }}><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>Изменить назначение или провайдера?</AlertDialogTitle><AlertDialogDescription>Введённые ключи будут очищены. Для другого провайдера потребуются другие реквизиты.</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>Продолжить редактирование</AlertDialogCancel><AlertDialogAction onClick={() => { if (pendingSelection) applySelection(pendingSelection) }}>Очистить ключи и продолжить</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>
    <Dialog open={helpOpen} onOpenChange={setHelpOpen}><DialogContent className="max-h-[90dvh] overflow-y-auto"><DialogHeader><DialogTitle>Настройка аккаунта интеграции</DialogTitle><DialogDescription>Ключи, квота и использование аккаунта</DialogDescription></DialogHeader><div className="flex flex-col gap-4 text-sm"><p>Переключатель разрешает системе выбирать аккаунт для запросов. Все изменения применяются после записи.</p><p>Ключи одного внешнего аккаунта используют общую квоту. Их значения скрыты после сохранения.</p><p>Если профиль предусматривает несколько ключей, при замене нужно ввести весь набор. Поддерживаемые ссылки преобразуются в ключи по правилам профиля; адреса не сохраняются.</p><p>День и часовой пояс задают начало расчётного периода в Metapus. Проверка подключения доступна в карточке пула и использует его RPC-привязку.</p><p>Сохранение аккаунта не добавляет его в пул автоматически. После записи выберите «Добавить в пул…».</p></div></DialogContent></Dialog>
  </div>
}
