"use client"

import { useState } from "react"
import useSWR from "swr"
import { useSearchParams } from "next/navigation"
import { useIntegrationProviders } from "@/hooks/useIntegrationProviders"
import { toast } from "sonner"
import { useCatalogForm } from "@/hooks/useCatalogForm"
import { usePermissions } from "@/hooks/use-permissions"
import { createFormDraftKey } from "@/stores/useFormDraftStore"
import { integrationAccounts, integrationPools, integrationRuntime } from "@/lib/api/integrations"
import type { EntityFormProps } from "@/lib/entity-registry"
import type * as Wire from "@/types/generated/http-contracts"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { Field, FieldGroup, FieldLabel, FieldDescription, FieldError, FieldSet, FieldLegend } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from "@/components/ui/table"
import { Skeleton } from "@/components/ui/skeleton"
import { AlertDialog, AlertDialogContent, AlertDialogHeader, AlertDialogTitle, AlertDialogDescription, AlertDialogFooter, AlertDialogCancel, AlertDialogAction } from "@/components/ui/alert-dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { IntegrationProviderFields } from "./provider-fields"

interface State { code: string; name: string; kind: string; provider: string; isActive: boolean; accountIds: string[]; version: number }
const draftKey = createFormDraftKey<State>("integration-pool")
const initialState: State = { code: "", name: "", kind: "", provider: "", isActive: true, accountIds: [], version: 1 }
export default function IntegrationPoolForm({ id, entityName }: EntityFormProps) {
  const { has } = usePermissions()
  const registry = useIntegrationProviders()
  const searchParams = useSearchParams()
  const canWrite = has(`catalog:integration_pool:${id && id !== "new" ? "update" : "create"}`)
  const accounts = useSWR("integration-account-options", () => integrationAccounts.list({ limit: 100 }))
  const runtime = useSWR(id && id !== "new" ? ["integration-pool-status", id] : null, () => integrationRuntime.status(id!), { refreshInterval: 10000 })
  const [reason, setReason] = useState("")
  const [busy, setBusy] = useState(false)
  const [pendingSelection, setPendingSelection] = useState<{ kind: string; provider: string } | null>(null)
  const form = useCatalogForm<State, Wire.IntegrationPoolResponse, Wire.CreateIntegrationPoolRequest, Wire.UpdateIntegrationPoolRequest>({
    draftKey, entityName, entityKey: "integration_pool", initialState: { ...initialState, kind: searchParams.get("kind") ?? "" }, api: integrationPools,
    listPath: "/catalogs/integration-pools", mapToCreate: s => ({code:s.code,name:s.name,provider:s.provider,isActive:s.isActive,accountIds:s.accountIds}), mapToUpdate: s => ({ code: s.code, name: s.name, provider: s.provider, isActive: s.isActive, accountIds: s.accountIds, version: s.version }),
    mapFromResponse: r => ({ code: r.code, name: r.name, kind: "", provider: r.provider, isActive: r.isActive, accountIds: r.accountIds, version: r.version }),
    getVersion: r => r.version, getDeletionMark: r => r.deletionMark, titleField: s => s.name,
    validate: s => !registry.data ? "Не удалось загрузить провайдеров." : !registry.providers.some(p => p.id === s.provider && (id || p.canCreate)) ? "Выберите назначение и провайдера." : !s.name.trim() ? "Укажите наименование" : s.isActive && !s.accountIds.length ? "Добавьте аккаунты или отключите пул" : null,
  })
  const definition = registry.providers.find(p => p.id === form.f.provider)
  const kind = form.f.kind || definition?.kinds[0] || ""
  const change = (patch: Partial<State>) => { form.update(patch); form.handleChange() }
  const changeSelection = (selection: { kind: string; provider: string }) => {
    if (selection.provider === form.f.provider) { change({ kind: selection.kind }); return }
    if (form.f.accountIds.length) setPendingSelection(selection)
    else change({ ...selection, accountIds: [] })
  }
  const move = (index: number, delta: number) => { const rows = [...form.f.accountIds]; [rows[index], rows[index + delta]] = [rows[index + delta], rows[index]]; change({ accountIds: rows }) }
  const run = async (action: "next" | "exhaust" | "resume") => {
    if (!id || !runtime.data) return
    setBusy(true)
    try { await integrationRuntime.command(id, { action, reason, expectedVersion: runtime.data.selectionVersion }); await runtime.mutate(); setReason(""); toast.success("Состояние пула обновлено") } catch (e) { toast.error(e instanceof Error ? e.message : "Не удалось выполнить команду"); await runtime.mutate() } finally { setBusy(false) }
  }
  const test = async () => { if (!id) return; setBusy(true); try { const result = await integrationRuntime.test(id); if (result.ready) toast.success(result.message); else toast.error(result.message); await runtime.mutate() } catch (e) { toast.error(e instanceof Error ? e.message : "Не удалось проверить подключение") } finally { setBusy(false) } }
  if (form.loading || registry.isLoading) return <Skeleton className="h-48" />
  const options = accounts.data?.items ?? []
  return <div className="flex h-full min-h-0 flex-col overflow-hidden">
    <FormToolbar title={form.formTitle} backHref="/catalogs/integration-pools" primaryAction={{ label: "Записать", onClick: () => void form.handleSave(false), disabled: form.saving || !canWrite || !registry.data }} secondaryActions={[{ label: "Записать и закрыть", onClick: () => void form.handleSave(true), disabled: form.saving || !canWrite || !registry.data }]} />
    <ScrollArea className="min-h-0 flex-1">
    <div className="max-w-4xl p-6"><FieldGroup>
      {registry.error && <Field><FieldError>Не удалось загрузить провайдеров.</FieldError><Button variant="outline" onClick={() => void registry.mutate()}>Повторить загрузку</Button></Field>}
      {form.error && <FieldError>{form.error}</FieldError>}
      <FieldSet disabled={!canWrite || form.saving || !registry.data}><FieldLegend>Пул аккаунтов</FieldLegend><FieldGroup>
        <IntegrationProviderFields prefix="pool" kinds={registry.kinds} providers={registry.providers} kind={kind} provider={form.f.provider} locked={form.isEditMode} onKindChange={nextKind => changeSelection({ kind: nextKind, provider: definition?.canCreate && definition.kinds.includes(nextKind) ? form.f.provider : "" })} onProviderChange={provider => changeSelection({ kind, provider })} />
        {form.f.provider && <>
        <Field><FieldLabel htmlFor="pool-name">Наименование</FieldLabel><Input id="pool-name" value={form.f.name} onChange={e => change({ name: e.target.value })} /></Field>
        <Field><FieldLabel htmlFor="pool-code">Код</FieldLabel><Input id="pool-code" value={form.f.code} onChange={e => change({ code: e.target.value })} /></Field>
        <Field orientation="horizontal"><Switch id="pool-active" checked={form.f.isActive} onCheckedChange={isActive => change({ isActive })} /><FieldLabel htmlFor="pool-active">Активен</FieldLabel></Field>
        <Field><FieldLabel>Добавить аккаунт</FieldLabel><Select value="" onValueChange={uid => change({ accountIds: [...form.f.accountIds, uid] })}><SelectTrigger aria-label="Добавить аккаунт"><SelectValue placeholder="Выберите аккаунт провайдера" /></SelectTrigger><SelectContent><SelectGroup>{options.filter(a => a.provider === form.f.provider && !form.f.accountIds.includes(a.id)).map(a => <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>)}</SelectGroup></SelectContent></Select><FieldDescription>Выбранный аккаунт используется до исчерпания квоты. Затем берётся следующий по списку.</FieldDescription></Field>
        <Table><TableHeader><TableRow><TableHead>Порядок</TableHead><TableHead>Аккаунт</TableHead><TableHead>Действия</TableHead></TableRow></TableHeader><TableBody>{form.f.accountIds.map((uid, index) => <TableRow key={uid}><TableCell>{index + 1}</TableCell><TableCell>{options.find(a => a.id === uid)?.name ?? uid}</TableCell><TableCell><div className="flex gap-2"><Button variant="outline" size="sm" disabled={index === 0} onClick={() => move(index, -1)} aria-label="Переместить выше">Выше</Button><Button variant="outline" size="sm" disabled={index === form.f.accountIds.length - 1} onClick={() => move(index, 1)} aria-label="Переместить ниже">Ниже</Button><Button variant="outline" size="sm" onClick={() => change({ accountIds: form.f.accountIds.filter(v => v !== uid) })}>Убрать</Button></div></TableCell></TableRow>)}</TableBody></Table>
        </>}
      </FieldGroup></FieldSet>
      {runtime.data && <FieldSet><FieldLegend>Текущее состояние</FieldLegend><FieldDescription>{({ account_selected: "Аккаунт выбран автоматически", quota_exhausted: "Квота аккаунта исчерпана", rate_limited: "Провайдер временно ограничил запросы", invalid_credentials: "Ключ не принят провайдером", credentials_expired: "Срок действия ключа истёк", manual_next: "Аккаунт переключён вручную", manual_resume: "Разрешена повторная проверка" } as Record<string, string>)[runtime.data.reason] || "Аккаунт ещё не использовался"}</FieldDescription>
        <Table><TableHeader><TableRow><TableHead>Аккаунт</TableHead><TableHead>Состояние</TableHead><TableHead>Срок / обновление квоты</TableHead></TableRow></TableHeader><TableBody>{runtime.data.accounts.map(a => <TableRow key={a.id}><TableCell>{a.name}{runtime.data?.accountId === a.id ? " · выбран" : ""}</TableCell><TableCell>{!a.isActive ? "Отключён" : !a.hasCredentials ? "Нет ключа" : a.availability === "credentials_expired" || (a.credentialsExpiresAt && Date.parse(a.credentialsExpiresAt) <= Date.now()) ? "Срок действия истёк" : a.exhaustedUntil && Date.parse(a.exhaustedUntil) > Date.now() ? "Квота исчерпана" : a.invalidSlots.length ? `Неверный ключ: ${a.invalidSlots.join(", ")}` : a.throttledUntil && Date.parse(a.throttledUntil) > Date.now() ? "Временно ограничен" : "Доступен"}</TableCell><TableCell>{a.quotaPolicy === "manual" ? a.credentialsExpiresAt ? `Действует до ${new Date(a.credentialsExpiresAt).toLocaleString()}` : "Срок не настроен" : a.nextResetAt ? new Date(a.nextResetAt).toLocaleString() : "Без календарного сброса"}</TableCell></TableRow>)}</TableBody></Table>
        {canWrite && <FieldGroup><Field><FieldLabel htmlFor="pool-command-reason">Причина ручного изменения</FieldLabel><Input id="pool-command-reason" value={reason} onChange={e => setReason(e.target.value)} maxLength={500} /></Field><div className="flex flex-wrap gap-2"><Button variant="outline" disabled={busy || reason.trim().length < 3} onClick={() => void run("next")}>Следующий аккаунт</Button><Button variant="outline" disabled={busy || reason.trim().length < 3 || !runtime.data.accountId} onClick={() => void run("exhaust")}>Квота исчерпана</Button><Button variant="outline" disabled={busy || reason.trim().length < 3} onClick={() => void run("resume")}>Разрешить повторную проверку</Button><Button variant="outline" disabled={busy} onClick={() => void test()}>Проверить подключение</Button></div></FieldGroup>}
      </FieldSet>}
    </FieldGroup></div>
    </ScrollArea>
    <AlertDialog open={pendingSelection !== null} onOpenChange={open => { if (!open) setPendingSelection(null) }}>
      <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>Изменить провайдера пула?</AlertDialogTitle><AlertDialogDescription>Выбранные аккаунты будут исключены из нового пула. Сами аккаунты останутся в справочнике.</AlertDialogDescription></AlertDialogHeader>
        <AlertDialogFooter><AlertDialogCancel>Продолжить редактирование</AlertDialogCancel><AlertDialogAction onClick={() => { if (pendingSelection) change({ ...pendingSelection, accountIds: [] }); setPendingSelection(null) }}>Очистить состав и продолжить</AlertDialogAction></AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
}
