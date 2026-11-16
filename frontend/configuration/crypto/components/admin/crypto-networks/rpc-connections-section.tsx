"use client"

import { Fragment, useEffect, useRef, useState } from "react"
import Link from "next/link"
import useSWR from "swr"
import { ArrowDown, ArrowUp, ChevronDown, ChevronRight, LoaderCircle, Settings2 } from "lucide-react"
import { toast } from "sonner"

import { api, ApiError, shouldRetainMutationKey } from "@/lib/api"
import { usePermissions } from "@/hooks/use-permissions"
import type { CryptoNetworkState, CryptoRuntimeEndpoint } from "@/configuration/crypto/types/crypto-network"
import type { CryptoRPCConfigurationResponse, UpdateCryptoRPCConfigurationRequest } from "@/types/generated/http-contracts"
import { moveRPCConnection, orderRPCConnections, rpcConfigurationChanges, rpcCoverage, rpcReasonLabel, type RPCConnection } from "@/lib/integrations/rpc-configuration"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldGroup, FieldLabel, FieldLegend, FieldSet } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { rpcSettingsForm, rpcSettingsFromForm, rpcSettingsFormsEqual, rpcSettingsErrors, type RPCSettingsForm } from "@/configuration/crypto/lib/integrations/rpc-network-settings"
import { RPCSettingsFields } from "./rpc-settings-fields"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { endpointLabels, formatTimestamp, isRuntimeFresh, stateLabels, stateVariants } from "./types-and-helpers"

interface RPCConnectionsSectionProps {
  state: CryptoNetworkState
  onDirtyChange: (dirty: boolean) => void
}
interface PendingRPCCommand { request: UpdateCryptoRPCConfigurationRequest; key: string }

function RuntimeBadge({ endpoint }: { endpoint?: CryptoRuntimeEndpoint }) {
  if (!endpoint) return <Badge variant="outline">Ожидается проверка</Badge>
  return <Badge variant={stateVariants[endpoint.state] ?? "outline"}>{stateLabels[endpoint.state] ?? "Состояние неизвестно"}</Badge>
}

function RuntimeOnly({ endpoints }: { endpoints: CryptoRuntimeEndpoint[] }) {
  return <Table><TableHeader><TableRow><TableHead>Роль</TableHead><TableHead>Провайдер</TableHead><TableHead>Состояние</TableHead><TableHead className="text-right">Ошибки</TableHead></TableRow></TableHeader>
    <TableBody>{endpoints.length ? endpoints.map((endpoint, index) => <TableRow key={endpoint.endpointId ?? `${endpoint.role}:${endpoint.providerId}:${index}`}>
      <TableCell>{endpointLabels[endpoint.role] ?? endpoint.role}</TableCell><TableCell>{endpoint.providerId}</TableCell><TableCell><RuntimeBadge endpoint={endpoint} /></TableCell><TableCell className="text-right tabular-nums">{endpoint.failures}</TableCell>
    </TableRow>) : <TableRow><TableCell colSpan={4}>Воркер ещё не опубликовал статус RPC</TableCell></TableRow>}</TableBody></Table>
}

export function RPCConnectionsSection({ state, onDirtyChange }: RPCConnectionsSectionProps) {
  const networkId = state.network.id
  const { has } = usePermissions()
  const canRead = has("catalog:crypto_rpc_endpoint:read")
  const { data, error, isLoading, mutate } = useSWR(
    canRead ? ["admin-rpc-configuration", networkId] : null,
    () => api.cryptoNetworks.rpcConfiguration(networkId), { refreshInterval: 15_000, revalidateOnFocus: true },
  )
  const [baseline, setBaseline] = useState<CryptoRPCConfigurationResponse | null>(null)
  const [draft, setDraft] = useState<RPCConnection[]>([])
  const [settingsDraft, setSettingsDraft] = useState<RPCSettingsForm | null>(null)
  const [showDisabled, setShowDisabled] = useState(false)
  const [expanded, setExpanded] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const busyRef = useRef(false)
  const [pending, setPending] = useState<PendingRPCCommand | null>(null)
  const [submitError, setSubmitError] = useState("")
  const editing = baseline !== null
  const changes = baseline ? rpcConfigurationChanges(baseline.endpoints, draft) : []
  const settingsChanged = !!baseline && !!settingsDraft && !rpcSettingsFormsEqual(settingsDraft, rpcSettingsForm(baseline.settings))
  const settingsErrors = data && settingsDraft ? rpcSettingsErrors(settingsDraft, data.defaultSettings, data.family) : {}
  const intervalInvalid = draft.some(row => !Number.isSafeInteger(row.minRequestIntervalMs) || row.minRequestIntervalMs < 0 || row.minRequestIntervalMs > 30_000 || row.minRequestIntervalMs > 0 && row.minRequestIntervalMs < 100)
  const invalid = Object.keys(settingsErrors).length > 0 || intervalInvalid
  const dirty = changes.length > 0 || settingsChanged || pending !== null
  const stale = !!baseline && !!data && data.revision !== baseline.revision
  const rows = orderRPCConnections(editing ? draft : data?.endpoints ?? [])
  const coverage = rpcCoverage(rows, data?.requirements ?? [])
  const insufficient = coverage.some(role => role.configuredProviders < role.requiredProviders)
  const disabledCount = rows.filter(row => !row.isActive).length
  const fresh = isRuntimeFresh(state.runtime.observedAt)
  const applied = !!data?.effectiveRevision && fresh && state.runtime.rpcEndpoints.length > 0 && state.runtime.rpcEndpoints.every(endpoint => endpoint.appliedRevision === data.effectiveRevision)
  const configurationFailed = state.runtime.rpcEndpoints.some(endpoint => endpoint.providerId === "configuration" && endpoint.state === "invalid")

  useEffect(() => { onDirtyChange(dirty); return () => onDirtyChange(false) }, [dirty, onDirtyChange])

  const begin = () => {
    if (!data?.canManage || busyRef.current) return
    setBaseline(data)
    setDraft(orderRPCConnections(data.endpoints))
    setSettingsDraft(rpcSettingsForm(data.settings))
    setShowDisabled(true)
    setSubmitError("")
  }
  const cancel = () => {
    if (busyRef.current || pending) return
    setBaseline(null)
    setDraft([])
    setSettingsDraft(null)
    setSubmitError("")
  }
  const submit = async () => {
    if (!baseline || !data?.canManage || !has("catalog:crypto_rpc_endpoint:update") || busyRef.current || (!pending && ((!changes.length && !settingsChanged) || stale || insufficient || invalid))) return
    const command = pending ?? { request: { expectedRevision: baseline.revision, endpoints: changes, ...(settingsChanged && settingsDraft ? { settings: rpcSettingsFromForm(settingsDraft, baseline.settings, data.family) } : {}) }, key: crypto.randomUUID() }
    busyRef.current = true
    setBusy(true)
    setSubmitError("")
    try {
      const updated = await api.cryptoNetworks.updateRPCConfiguration(networkId, command.request, command.key)
      await mutate(updated, { revalidate: false })
      setPending(null)
      setBaseline(null)
      setDraft([])
      setSettingsDraft(null)
      setShowDisabled(false)
      toast.success("Настройки RPC сохранены. Ожидается обновление состояния.")
    } catch (cause) {
      if (shouldRetainMutationKey(cause)) {
        setPending(command)
        setSubmitError("Результат запроса не подтверждён. Повторите тот же запрос; настройки черновика сохранены.")
      } else {
        setPending(null)
        setSubmitError(cause instanceof ApiError && cause.status === 409
          ? "Конфигурация изменена другим администратором. Загрузите актуальные настройки перед повторным редактированием."
          : cause instanceof ApiError && cause.parsedBody?.details?.reasonCode === "rpc_coverage_insufficient"
            ? "Недостаточно независимых RPC для обязательной роли. Проверьте включённые подключения."
            : cause instanceof Error ? cause.message : "Не удалось сохранить настройки RPC")
      }
      await mutate().catch(() => undefined)
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }

  if (!canRead) return <FieldSet className="min-w-0 max-w-full"><FieldLegend>RPC-подключения</FieldLegend><RuntimeOnly endpoints={state.runtime.rpcEndpoints} /></FieldSet>
  if (isLoading) return <Skeleton className="h-28" />
  if (error || !data) return <Alert variant="destructive"><AlertTitle>Настройки RPC недоступны</AlertTitle><AlertDescription>
    <span>Обновите данные, чтобы проверить подключения.</span><Button type="button" variant="outline" size="sm" onClick={() => void mutate()}>Обновить RPC</Button>
  </AlertDescription></Alert>
  if (data.source !== "database" && !editing) return <FieldSet className="min-w-0 max-w-full"><FieldLegend>RPC-подключения</FieldLegend>
    <FieldDescription>RPC этой сети настроены через файл. Управление доступно после переноса конфигурации в БД.</FieldDescription>
    <RuntimeOnly endpoints={state.runtime.rpcEndpoints} /></FieldSet>

  return <FieldSet className="min-w-0 max-w-full" aria-label={`RPC-подключения ${state.network.code}`}>
    <div className="flex flex-wrap items-center justify-between gap-2">
      <FieldLegend>RPC-подключения</FieldLegend>
      {!editing && data.canManage && has("catalog:crypto_rpc_endpoint:update") ? <Button type="button" variant="outline" size="sm" onClick={begin}><Settings2 data-icon="inline-start" />Настроить RPC</Button> : null}
    </div>
    <FieldDescription>{editing ? "Изменения пока в черновике и вступят в силу после применения. " : ""}Порядок задаёт предпочтение при выборе источника. При ошибках используются доступные подключения; проверка данных может обращаться к нескольким независимым провайдерам.</FieldDescription>
    <FieldGroup className="min-w-0 gap-3">
      <div className="flex flex-wrap items-center gap-2" aria-live="polite">
        {coverage.map(role => <Badge key={role.role} variant={role.configuredProviders >= role.requiredProviders ? "outline" : "destructive"}>
          {endpointLabels[role.role] ?? role.role}: {role.configuredProviders}/{role.requiredProviders} независимых
        </Badge>)}
        <Badge className="max-w-full whitespace-normal" variant={!fresh || configurationFailed ? "warning" : "outline"}>
          {!fresh ? "Состояние устарело" : configurationFailed ? "Не удалось применить конфигурацию" : applied ? "RPC подтверждены наблюдаемым воркером" : "Ожидается обновление состояния"}
        </Badge>
      </div>
      <Field orientation="horizontal">
        <Switch id={`rpc-show-disabled-${networkId}`} checked={showDisabled} onCheckedChange={setShowDisabled} />
        <FieldLabel htmlFor={`rpc-show-disabled-${networkId}`}>Показывать отключённые ({disabledCount})</FieldLabel>
      </Field>
      {stale && !pending ? <Alert variant="destructive"><AlertTitle>Настройки изменились</AlertTitle><AlertDescription>
        <span>Черновик сохранён на экране. Для продолжения загрузите актуальный список.</span>
        <Button type="button" variant="outline" size="sm" disabled={busy} onClick={cancel}>Загрузить актуальные настройки</Button>
      </AlertDescription></Alert> : null}
      {submitError ? <Alert variant="destructive"><AlertTitle>Настройки не подтверждены</AlertTitle><AlertDescription>{submitError}</AlertDescription></Alert> : null}
      {editing && insufficient ? <Alert variant="destructive"><AlertTitle>Недостаточно независимых источников</AlertTitle><AlertDescription>Включите подключения обязательных ролей. Несколько аккаунтов одного оператора считаются одним источником.</AlertDescription></Alert> : null}
      <RPCSettingsFields family={data.family} form={settingsDraft ?? rpcSettingsForm(data.settings)} defaults={data.defaultSettings} editing={editing} disabled={busy || !!pending || stale} errors={settingsErrors} onChange={(field, raw) => setSettingsDraft(current => current ? { ...current, [field]: raw } : current)} />
      <div className="min-w-0 max-w-full overflow-hidden rounded-md border">
        <Table>
          <TableHeader><TableRow><TableHead className="text-right">Порядок</TableHead><TableHead>Роль</TableHead><TableHead>Подключение</TableHead><TableHead>Использовать</TableHead><TableHead>Состояние</TableHead><TableHead className="text-right">Ошибки</TableHead></TableRow></TableHeader>
          <TableBody>
            {rows.filter(row => showDisabled || row.isActive).map(row => {
              const group = rows.filter(item => item.role === row.role)
              const saved = data.endpoints.find(item => item.id === row.id)
              const position = group.findIndex(item => item.id === row.id)
              const observed = state.runtime.rpcEndpoints.find(endpoint => endpoint.endpointId === row.id)
              const legacy = state.runtime.rpcEndpoints.find(endpoint => !endpoint.endpointId && endpoint.role === row.role && endpoint.providerId === row.providerId)
              const historical = observed ?? legacy
              const current = fresh && observed?.appliedRevision === data.effectiveRevision ? observed : undefined
              return <Fragment key={row.id}>
                <TableRow>
                  <TableCell className="text-right tabular-nums">
                    <div className="flex items-center justify-end gap-1">
                      <span>{position + 1}</span>
                      {editing ? <>
                        <Button type="button" variant="ghost" size="icon" aria-label={`Выше: ${row.providerId}`} disabled={busy || !!pending || stale || position === 0} onClick={() => setDraft(moveRPCConnection(draft, row.id, -1))}><ArrowUp /></Button>
                        <Button type="button" variant="ghost" size="icon" aria-label={`Ниже: ${row.providerId}`} disabled={busy || !!pending || stale || position === group.length - 1} onClick={() => setDraft(moveRPCConnection(draft, row.id, 1))}><ArrowDown /></Button>
                      </> : null}
                    </div>
                  </TableCell>
                  <TableCell>{endpointLabels[row.role] ?? row.role}</TableCell>
                  <TableCell>
                    <div className="flex flex-col gap-1"><Link href={`/catalogs/crypto-rpc-endpoints/${row.id}`} className="hover:underline">{row.name}</Link><span className="text-xs text-muted-foreground">{row.providerId} · {row.trustGroup}</span></div>
                    {editing ? <Field className="mt-2 gap-1"><FieldLabel htmlFor={`rpc-interval-${row.id}`}>Интервал, мс</FieldLabel><Input id={`rpc-interval-${row.id}`} type="number" min={0} max={30000} step={100} value={row.minRequestIntervalMs} disabled={busy || !!pending || stale} aria-invalid={row.minRequestIntervalMs !== 0 && (row.minRequestIntervalMs < 100 || row.minRequestIntervalMs > 30000 || !Number.isSafeInteger(row.minRequestIntervalMs))} onChange={event => setDraft(draft.map(item => item.id === row.id ? { ...item, minRequestIntervalMs: Number(event.target.value) } : item))} /><FieldDescription>0 — по лимитам сети и пула</FieldDescription></Field> : <FieldDescription>{row.minRequestIntervalMs ? `Интервал: ${row.minRequestIntervalMs} мс` : "Интервал: по лимитам сети и пула"}</FieldDescription>}
                  </TableCell>
                  <TableCell>{editing ? <Switch aria-label={`Использовать ${row.providerId}`} checked={row.isActive} disabled={busy || !!pending || stale} onCheckedChange={isActive => setDraft(draft.map(item => item.id === row.id ? { ...item, isActive } : item))} /> : row.isActive ? "Включено" : "Отключено"}</TableCell>
                  <TableCell>
                    <div className="flex items-center gap-1">
                      {editing && saved?.isActive !== row.isActive ? <Badge variant="warning">{row.isActive ? "Будет включён" : "Будет отключён"}</Badge> : !row.isActive ? <Badge variant="secondary">Отключён администратором</Badge> : <RuntimeBadge endpoint={current} />}
                      {historical ? <Button type="button" variant="ghost" size="icon" aria-label={`Диагностика ${row.providerId}`} onClick={() => setExpanded(expanded === row.id ? null : row.id)}>{expanded === row.id ? <ChevronDown /> : <ChevronRight />}</Button> : null}
                    </div>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{row.isActive && current ? current.failures : "—"}</TableCell>
                </TableRow>
                {expanded === row.id && historical ? <TableRow><TableCell colSpan={6}>
                  <div className="flex flex-col gap-2 p-2">
                    <p className="text-sm text-muted-foreground">Наблюдение: {formatTimestamp(state.runtime.observedAt)}{!current || !row.isActive ? " · предыдущее состояние" : ""}. {rpcReasonLabel(historical.lastError)}</p>
                    {historical.operations?.length ? <Table><TableHeader><TableRow><TableHead>Метод</TableHead><TableHead>Причина</TableHead><TableHead>Повторная попытка после</TableHead><TableHead className="text-right">Серия ошибок</TableHead></TableRow></TableHeader><TableBody>
                      {historical.operations.map(operation => <TableRow key={operation.operation}><TableCell>{operation.operation}</TableCell><TableCell>{rpcReasonLabel(operation.reasonCode)}</TableCell><TableCell>{operation.retryAt ? formatTimestamp(operation.retryAt) : "При следующем обращении"}</TableCell><TableCell className="text-right tabular-nums">{operation.failures}</TableCell></TableRow>)}
                    </TableBody></Table> : null}
                  </div>
                </TableCell></TableRow> : null}
              </Fragment>
            })}
            {!rows.some(row => showDisabled || row.isActive) ? <TableRow><TableCell colSpan={6}>Нет включённых RPC-подключений{disabledCount ? ". Включите показ отключённых." : ". Добавьте подключение в справочнике."}</TableCell></TableRow> : null}
          </TableBody>
        </Table>
      </div>
      {editing ? <div className="flex flex-wrap items-center justify-end gap-2">
        <Button type="button" variant="outline" disabled={busy || !!pending} onClick={cancel}>Отменить</Button>
        <Button type="button" disabled={busy || !data.canManage || !has("catalog:crypto_rpc_endpoint:update") || (!pending && (!dirty || stale || insufficient || invalid))} onClick={() => void submit()}>
          {busy ? <LoaderCircle className="animate-spin" data-icon="inline-start" /> : null}{pending ? "Повторить запрос" : "Применить RPC"}
        </Button>
      </div> : null}
      <FieldDescription>Ошибки показывают текущую серию отказов. Снимок относится к наблюдаемому воркеру; проверка всех процессов этим статусом не подтверждается.</FieldDescription>
    </FieldGroup>
  </FieldSet>
}
