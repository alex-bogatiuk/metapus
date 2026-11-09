"use client"

import { FormEvent, useCallback, useEffect, useState } from "react"
import Link from "next/link"
import {
  BookOpen,
  CheckCircle2,
  Clipboard,
  Loader2,
  Pencil,
  Play,
  RefreshCw,
  RotateCw,
  Send,
  Trash2,
  XCircle,
} from "lucide-react"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api } from "@/lib/api"
import { usePortalStore } from "@/stores/usePortalStore"
import type {
  PortalMerchantEvent,
  PortalWebhookDeliveryItem,
  PortalWebhookEndpoint,
} from "@/types/portal-api"

const BASE_EVENTS = [
  "invoice.created",
  "invoice.paid",
  "invoice.confirmed",
  "invoice.completed",
  "invoice.overpaid",
  "invoice.underpaid",
  "invoice.expired",
  "invoice.cancelled",
  "invoice.reversed",
  "invoice.late_payment",
]

const EVENT_GROUPS = [
  { label: "Инвойсы", events: BASE_EVENTS },
  {
    label: "Платежи (технические)",
    events: [
      "payment.detected",
      "payment.confirming",
      "payment.confirmed",
      "payment.reorged",
      "payment.replaced",
    ],
  },
  { label: "Выводы", events: ["withdrawal.confirmed", "withdrawal.failed", "withdrawal.partially_completed"] },
  { label: "Риск средств", events: ["wallet.risk_changed"] },
  { label: "Депозитные адреса", events: ["deposit_address.rotated"] },
]

const ALL_EVENTS = EVENT_GROUPS.flatMap((group) => group.events)

const PAGE_SIZE = 20
const PORTAL_ROLE_OWNER = 1
const PORTAL_ROLE_MANAGER = 2

function formatDate(value?: string) {
  if (!value) return "—"
  return new Date(value).toLocaleString("ru-RU", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  })
}

function shortId(value: string) {
  return value.length > 18 ? `${value.slice(0, 15)}…` : value
}

function EventSubscriptionPicker({
  idPrefix,
  selected,
  onChange,
}: {
  idPrefix: string
  selected: string[]
  onChange: (events: string[]) => void
}) {
  const toggle = (eventType: string, checked: boolean) => {
    if (checked) {
      onChange(ALL_EVENTS.filter((event) => event === eventType || selected.includes(event)))
      return
    }
    onChange(selected.filter((event) => event !== eventType))
  }

  return (
    <div className="flex flex-col gap-3 rounded-lg border p-3">
      {EVENT_GROUPS.map((group) => (
        <div key={group.label} className="flex flex-col gap-2">
          <p className="text-xs font-medium text-muted-foreground">{group.label}</p>
          <div className="grid gap-2 sm:grid-cols-2">
            {group.events.map((eventType) => {
              const inputId = `${idPrefix}-${eventType.replaceAll(".", "-")}`
              return (
                <div key={eventType} className="flex items-center gap-2">
                  <Checkbox
                    id={inputId}
                    checked={selected.includes(eventType)}
                    onCheckedChange={(checked) => toggle(eventType, checked === true)}
                  />
                  <label htmlFor={inputId} className="cursor-pointer font-mono text-xs">
                    {eventType}
                  </label>
                </div>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}

type EndpointDraft = Pick<PortalWebhookEndpoint, "id" | "name" | "url" | "enabledEvents" | "isDefault">

export default function WebhooksPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  const merchants = usePortalStore((s) => s.merchants)
  const activeMerchant = merchants.find((merchant) => merchant.id === activeMerchantId)
  const canReadRegistry = activeMerchant?.role === PORTAL_ROLE_OWNER || activeMerchant?.role === PORTAL_ROLE_MANAGER
  const canManageRegistry = activeMerchant?.role === PORTAL_ROLE_OWNER
  const [endpoints, setEndpoints] = useState<PortalWebhookEndpoint[]>([])
  const [events, setEvents] = useState<PortalMerchantEvent[]>([])
  const [eventTotal, setEventTotal] = useState(0)
  const [eventPage, setEventPage] = useState(0)
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)
  const [busyEndpoint, setBusyEndpoint] = useState<string | null>(null)
  const [name, setName] = useState("")
  const [url, setUrl] = useState("")
  const [isDefault, setIsDefault] = useState(false)
  const [selectedEvents, setSelectedEvents] = useState<string[]>([...BASE_EVENTS])
  const [endpointDraft, setEndpointDraft] = useState<EndpointDraft | null>(null)
  const [deleteCandidate, setDeleteCandidate] = useState<PortalWebhookEndpoint | null>(null)
  const [revealedSecret, setRevealedSecret] = useState<string | null>(null)
  const [selectedEvent, setSelectedEvent] = useState<PortalMerchantEvent | null>(null)
  const [deliveries, setDeliveries] = useState<PortalWebhookDeliveryItem[]>([])
  const [deliveriesLoading, setDeliveriesLoading] = useState(false)
  const [redelivering, setRedelivering] = useState(false)

  const loadRegistry = useCallback(async () => {
    if (!activeMerchantId || !canReadRegistry) {
      setEndpoints([])
      setEvents([])
      setEventTotal(0)
      setLoading(false)
      return
    }
    setLoading(true)
    try {
      const [endpointResult, eventResult] = await Promise.all([
        api.portal.webhookEndpoints.list(activeMerchantId),
        api.portal.merchantEvents(activeMerchantId, {
          limit: PAGE_SIZE,
          offset: eventPage * PAGE_SIZE,
        }),
      ])
      setEndpoints(endpointResult.items)
      setEvents(eventResult.items)
      setEventTotal(eventResult.total)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Не удалось загрузить вебхуки")
    } finally {
      setLoading(false)
    }
  }, [activeMerchantId, canReadRegistry, eventPage])

  useEffect(() => {
    void loadRegistry()
  }, [loadRegistry])

  const openEvent = async (event: PortalMerchantEvent) => {
    if (!activeMerchantId || !canReadRegistry) return
    setSelectedEvent(event)
    setDeliveriesLoading(true)
    try {
      const result = await api.portal.eventDeliveries(activeMerchantId, event.id)
      setDeliveries(result.items)
    } catch (error) {
      setDeliveries([])
      toast.error(error instanceof Error ? error.message : "Не удалось загрузить попытки")
    } finally {
      setDeliveriesLoading(false)
    }
  }

  const createEndpoint = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!activeMerchantId || !canManageRegistry) return
    setCreating(true)
    try {
      const endpoint = await api.portal.webhookEndpoints.create(activeMerchantId, {
        name: name.trim(),
        url: url.trim(),
        enabledEvents: selectedEvents,
        isDefault,
      })
      setRevealedSecret(endpoint.secret ?? null)
      setName("")
      setUrl("")
      setIsDefault(false)
      setSelectedEvents([...BASE_EVENTS])
      toast.success("Webhook endpoint создан")
      await loadRegistry()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Не удалось создать endpoint")
    } finally {
      setCreating(false)
    }
  }

  const copySecret = async () => {
    if (!revealedSecret) return
    await navigator.clipboard.writeText(revealedSecret)
    toast.success("Secret скопирован")
  }

  const testEndpoint = async (endpoint: PortalWebhookEndpoint) => {
    if (!activeMerchantId || !canManageRegistry) return
    setBusyEndpoint(endpoint.id)
    try {
      const result = await api.portal.webhookEndpoints.test(activeMerchantId, endpoint.id)
      if (result.success) {
        toast.success(`Тест доставлен${result.statusCode ? `: HTTP ${result.statusCode}` : ""}`)
      } else {
        toast.error(result.error ?? "Тестовая доставка не удалась")
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Тестовая доставка не удалась")
    } finally {
      setBusyEndpoint(null)
    }
  }

  const rotateSecret = async (endpoint: PortalWebhookEndpoint) => {
    if (!activeMerchantId || !canManageRegistry) return
    setBusyEndpoint(endpoint.id)
    try {
      const updated = await api.portal.webhookEndpoints.rotateSecret(activeMerchantId, endpoint.id)
      setRevealedSecret(updated.secret ?? null)
      toast.success("Secret обновлён. Предыдущий действует ещё 24 часа")
      await loadRegistry()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Не удалось обновить secret")
    } finally {
      setBusyEndpoint(null)
    }
  }

  const toggleEndpoint = async (endpoint: PortalWebhookEndpoint) => {
    if (!activeMerchantId || !canManageRegistry) return
    setBusyEndpoint(endpoint.id)
    try {
      await api.portal.webhookEndpoints.update(activeMerchantId, endpoint.id, {
        version: endpoint.version,
        isActive: !endpoint.isActive,
      })
      await loadRegistry()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Не удалось изменить endpoint")
    } finally {
      setBusyEndpoint(null)
    }
  }

  const startEditingEndpoint = (endpoint: PortalWebhookEndpoint) => {
    setEndpointDraft({
      id: endpoint.id,
      name: endpoint.name,
      url: endpoint.url,
      enabledEvents: [...endpoint.enabledEvents],
      isDefault: endpoint.isDefault,
    })
  }

  const saveEndpoint = async () => {
    if (!activeMerchantId || !canManageRegistry || !endpointDraft) return
    if (endpointDraft.enabledEvents.length === 0) {
      toast.error("Выберите хотя бы один тип события")
      return
    }
    setBusyEndpoint(endpointDraft.id)
    try {
      await api.portal.webhookEndpoints.update(activeMerchantId, endpointDraft.id, {
        version: endpoints.find((endpoint) => endpoint.id === endpointDraft.id)?.version ?? 0,
        name: endpointDraft.name.trim(),
        url: endpointDraft.url.trim(),
        enabledEvents: endpointDraft.enabledEvents,
        isDefault: endpointDraft.isDefault,
      })
      setEndpointDraft(null)
      toast.success("Endpoint обновлён")
      await loadRegistry()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Не удалось обновить endpoint")
    } finally {
      setBusyEndpoint(null)
    }
  }

  const deleteEndpoint = async () => {
    if (!activeMerchantId || !canManageRegistry || !deleteCandidate) return
    const endpoint = deleteCandidate
    setBusyEndpoint(endpoint.id)
    try {
      await api.portal.webhookEndpoints.remove(activeMerchantId, endpoint.id)
      if (endpointDraft?.id === endpoint.id) setEndpointDraft(null)
      setDeleteCandidate(null)
      toast.success("Endpoint удалён")
      await loadRegistry()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Не удалось удалить endpoint")
    } finally {
      setBusyEndpoint(null)
    }
  }

  const redeliver = async () => {
    if (!activeMerchantId || !canManageRegistry || !selectedEvent) return
    const endpoint = endpoints.find((item) => item.isActive && item.isDefault)
      ?? endpoints.find((item) => item.isActive)
    if (!endpoint) {
      toast.error("Сначала добавьте активный webhook endpoint")
      return
    }
    setRedelivering(true)
    try {
      await api.portal.redeliverEvent(activeMerchantId, selectedEvent.id, endpoint.id)
      toast.success(`Событие поставлено в очередь для ${endpoint.name}`)
      await openEvent(selectedEvent)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Не удалось переотправить событие")
    } finally {
      setRedelivering(false)
    }
  }

  const pageCount = Math.max(1, Math.ceil(eventTotal / PAGE_SIZE))

  if (activeMerchantId && !activeMerchant) {
    return <Skeleton className="h-48 w-full" />
  }

  if (activeMerchant && !canReadRegistry) {
    return (
      <Alert>
        <AlertTitle>Недостаточно прав</AlertTitle>
        <AlertDescription>
          История webhook доступна владельцу и менеджеру мерчанта. Viewer остаётся в режиме без доступа.
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Вебхуки</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Endpoint’ы, события и история доставки. Канонический статус всегда доступен через GET invoice.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" asChild>
            <Link href="/portal/developers/webhooks/verify" target="_blank" rel="noopener noreferrer">
              <BookOpen data-icon="inline-start" />
              Руководство по вебхукам
              <span className="sr-only"> (откроется в новой вкладке)</span>
            </Link>
          </Button>
          <Button variant="outline" onClick={() => void loadRegistry()} disabled={loading || !activeMerchantId}>
            <RefreshCw data-icon="inline-start" className={loading ? "animate-spin" : undefined} />
            Обновить
          </Button>
        </div>
      </div>

      {!activeMerchantId && (
        <Alert>
          <AlertDescription>Выберите мерчанта, чтобы настроить доставку событий.</AlertDescription>
        </Alert>
      )}

      {revealedSecret && (
        <Alert>
          <CheckCircle2 />
          <AlertTitle>Сохраните секрет подписи</AlertTitle>
          <AlertDescription className="flex flex-col gap-3">
            <p>Секрет нужен вашему серверу для проверки подписи входящих вебхуков. Сохраните его в менеджере секретов: повторно показать его нельзя.</p>
            <div className="flex min-w-0 items-center gap-2">
              <code className="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs">
                {revealedSecret}
              </code>
              <Button type="button" variant="outline" size="sm" onClick={() => void copySecret()}>
                <Clipboard data-icon="inline-start" />
                Копировать
              </Button>
              <Button type="button" variant="ghost" size="sm" onClick={() => setRevealedSecret(null)}>
                Скрыть
              </Button>
            </div>
            <Button variant="link" size="sm" className="self-start" asChild>
              <Link href="/portal/developers/webhooks/verify" target="_blank" rel="noopener noreferrer">
                Руководство по вебхукам
                <span className="sr-only"> (откроется в новой вкладке)</span>
              </Link>
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <div className="grid gap-6 xl:grid-cols-[minmax(19rem,0.8fr)_minmax(0,1.2fr)]">
        {canManageRegistry ? <Card>
          <CardHeader>
            <CardTitle>Новый endpoint</CardTitle>
            <CardDescription>
              Только HTTPS на портах 443 или 8443. Выберите события, которые нужны этой интеграции.
            </CardDescription>
          </CardHeader>
          <form onSubmit={createEndpoint}>
            <CardContent>
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="webhook-name">Название</FieldLabel>
                  <Input id="webhook-name" value={name} onChange={(event) => setName(event.target.value)} placeholder="Production" required maxLength={100} />
                </Field>
                <Field>
                  <FieldLabel htmlFor="webhook-url">Endpoint URL</FieldLabel>
                  <Input
                    id="webhook-url"
                    type="url"
                    value={url}
                    onChange={(event) => setUrl(event.target.value)}
                    placeholder="https://example.com/webhooks/metapus"
                    required
                    pattern="https://.*"
                    autoCapitalize="none"
                    autoCorrect="off"
                  />
                  <FieldDescription>Redirects запрещены; адрес проверяется перед каждой попыткой.</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel>Подписки</FieldLabel>
                  <EventSubscriptionPicker idPrefix="new-webhook-event" selected={selectedEvents} onChange={setSelectedEvents} />
                  <FieldDescription>Для выдачи товара достаточно `invoice.completed`; payment events нужны только для технического UI.</FieldDescription>
                </Field>
                <Field orientation="horizontal">
                  <Checkbox id="webhook-default" checked={isDefault} onCheckedChange={(checked) => setIsDefault(checked === true)} />
                  <FieldContent>
                    <FieldLabel htmlFor="webhook-default">Endpoint по умолчанию</FieldLabel>
                    <FieldDescription>Используется для invoice-level notificationUrl и ручной переотправки.</FieldDescription>
                  </FieldContent>
                </Field>
              </FieldGroup>
            </CardContent>
            <CardFooter>
              <Button type="submit" disabled={creating || !activeMerchantId || selectedEvents.length === 0}>
                {creating ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Send data-icon="inline-start" />}
                Создать endpoint
              </Button>
            </CardFooter>
          </form>
        </Card> : (
          <Alert>
            <AlertTitle>Режим чтения</AlertTitle>
            <AlertDescription>
              Менеджер может просматривать endpoints, события и доставки. Создание, ротация и ручная доставка доступны только Owner.
            </AlertDescription>
          </Alert>
        )}

        <Card>
          <CardHeader>
            <CardTitle>Endpoint’ы</CardTitle>
            <CardDescription>У каждого endpoint собственный signing secret и набор подписок.</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {loading ? (
              Array.from({ length: 2 }).map((_, index) => <Skeleton key={index} className="h-28 w-full" />)
            ) : endpoints.length === 0 ? (
              <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">Endpoint’ов пока нет.</div>
            ) : endpoints.map((endpoint) => (
              <div key={endpoint.id} className="flex flex-col gap-3 rounded-lg border p-4">
                {endpointDraft?.id === endpoint.id ? (
                  <div className="flex flex-col gap-4">
                    <div className="grid gap-3 md:grid-cols-2">
                      <Field>
                        <FieldLabel htmlFor={`edit-webhook-name-${endpoint.id}`}>Название</FieldLabel>
                        <Input
                          id={`edit-webhook-name-${endpoint.id}`}
                          value={endpointDraft.name}
                          onChange={(event) => setEndpointDraft({ ...endpointDraft, name: event.target.value })}
                          required
                          maxLength={100}
                        />
                      </Field>
                      <Field>
                        <FieldLabel htmlFor={`edit-webhook-url-${endpoint.id}`}>Endpoint URL</FieldLabel>
                        <Input
                          id={`edit-webhook-url-${endpoint.id}`}
                          type="url"
                          value={endpointDraft.url}
                          onChange={(event) => setEndpointDraft({ ...endpointDraft, url: event.target.value })}
                          required
                          pattern="https://.*"
                          autoCapitalize="none"
                          autoCorrect="off"
                        />
                      </Field>
                    </div>
                    <EventSubscriptionPicker
                      idPrefix={`edit-webhook-event-${endpoint.id}`}
                      selected={endpointDraft.enabledEvents}
                      onChange={(enabledEvents) => setEndpointDraft({ ...endpointDraft, enabledEvents })}
                    />
                    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                      <div className="flex items-center gap-2">
                        <Checkbox
                          id={`edit-webhook-default-${endpoint.id}`}
                          checked={endpointDraft.isDefault}
                          onCheckedChange={(checked) => setEndpointDraft({ ...endpointDraft, isDefault: checked === true })}
                        />
                        <label htmlFor={`edit-webhook-default-${endpoint.id}`} className="cursor-pointer text-sm">Endpoint по умолчанию</label>
                      </div>
                      <div className="flex gap-2">
                        <Button type="button" variant="ghost" onClick={() => setEndpointDraft(null)} disabled={busyEndpoint === endpoint.id}>Отмена</Button>
                        <Button type="button" onClick={() => void saveEndpoint()} disabled={busyEndpoint === endpoint.id || endpointDraft.enabledEvents.length === 0 || !endpointDraft.name.trim() || !endpointDraft.url.trim()}>
                          {busyEndpoint === endpoint.id && <Loader2 data-icon="inline-start" className="animate-spin" />}
                          Сохранить
                        </Button>
                      </div>
                    </div>
                  </div>
                ) : <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="font-medium">{endpoint.name}</p>
                      <Badge variant={endpoint.isActive ? "default" : "secondary"}>{endpoint.isActive ? "Активен" : "Отключён"}</Badge>
                      {endpoint.isDefault && <Badge variant="outline">Default</Badge>}
                    </div>
                    <p className="mt-1 truncate font-mono text-xs text-muted-foreground" title={endpoint.url}>{endpoint.url}</p>
                    <p className="mt-2 text-xs text-muted-foreground">
                      Secret v{endpoint.secretVersion} · {endpoint.enabledEvents.length} событий
                      {endpoint.previousSecretValidUntil ? ` · dual-signature до ${formatDate(endpoint.previousSecretValidUntil)}` : ""}
                    </p>
                    <div className="mt-3 flex flex-wrap gap-1.5">
                      {endpoint.enabledEvents.map((eventType) => <Badge key={eventType} variant="secondary" className="font-mono text-[10px]">{eventType}</Badge>)}
                    </div>
                  </div>
                  {canManageRegistry ? <div className="flex flex-wrap gap-2">
                    <Button type="button" variant="outline" size="sm" disabled={busyEndpoint === endpoint.id || !endpoint.isActive} onClick={() => void testEndpoint(endpoint)}>
                      <Play data-icon="inline-start" />
                      Тест
                    </Button>
                    <Button type="button" variant="outline" size="sm" disabled={busyEndpoint === endpoint.id} onClick={() => void rotateSecret(endpoint)}>
                      <RotateCw data-icon="inline-start" />
                      Secret
                    </Button>
                    <Button type="button" variant="outline" size="sm" disabled={busyEndpoint === endpoint.id} onClick={() => startEditingEndpoint(endpoint)}>
                      <Pencil data-icon="inline-start" />
                      Изменить
                    </Button>
                    <Button type="button" variant="ghost" size="sm" disabled={busyEndpoint === endpoint.id} onClick={() => void toggleEndpoint(endpoint)}>
                      {endpoint.isActive ? "Отключить" : "Включить"}
                    </Button>
                    <Button type="button" variant="ghost" size="sm" className="text-destructive hover:text-destructive" disabled={busyEndpoint === endpoint.id} onClick={() => setDeleteCandidate(endpoint)}>
                      <Trash2 data-icon="inline-start" />
                      Удалить
                    </Button>
                  </div> : <Badge variant="outline">Только чтение</Badge>}
                </div>}
              </div>
            ))}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>События</CardTitle>
          <CardDescription>Payload неизменяем. Повторная доставка сохраняет event ID и получает новый delivery ID и подпись.</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Тип</TableHead><TableHead>Event ID</TableHead><TableHead>Invoice</TableHead>
                <TableHead>Версия</TableHead><TableHead>Создано</TableHead><TableHead className="text-right">Действие</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                Array.from({ length: 5 }).map((_, row) => (
                  <TableRow key={row}>{Array.from({ length: 6 }).map((__, cell) => <TableCell key={cell}><Skeleton className="h-4 w-full" /></TableCell>)}</TableRow>
                ))
              ) : events.length === 0 ? (
                <TableRow><TableCell colSpan={6} className="h-32 text-center text-muted-foreground">Merchant events пока нет.</TableCell></TableRow>
              ) : events.map((event) => (
                <TableRow key={event.id} data-state={selectedEvent?.id === event.id ? "selected" : undefined}>
                  <TableCell><Badge variant="outline" className="font-mono">{event.type}</Badge></TableCell>
                  <TableCell className="font-mono text-xs" title={event.id}>{shortId(event.id)}</TableCell>
                  <TableCell className="font-mono text-xs" title={event.invoiceId}>{event.invoiceId ? shortId(event.invoiceId) : "—"}</TableCell>
                  <TableCell className="tabular-nums">{event.resourceVersion}</TableCell>
                  <TableCell className="text-muted-foreground">{formatDate(event.createdAt)}</TableCell>
                  <TableCell className="text-right"><Button type="button" variant="ghost" size="sm" onClick={() => void openEvent(event)}>Открыть</Button></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
        {eventTotal > PAGE_SIZE && (
          <CardFooter className="justify-between pt-6">
            <p className="text-sm text-muted-foreground">Страница {eventPage + 1} из {pageCount}</p>
            <div className="flex gap-2">
              <Button type="button" variant="outline" size="sm" disabled={eventPage === 0} onClick={() => setEventPage((page) => page - 1)}>Назад</Button>
              <Button type="button" variant="outline" size="sm" disabled={eventPage + 1 >= pageCount} onClick={() => setEventPage((page) => page + 1)}>Далее</Button>
            </div>
          </CardFooter>
        )}
      </Card>

      <AlertDialog open={deleteCandidate !== null} onOpenChange={(open) => { if (!open) setDeleteCandidate(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Удалить webhook endpoint?</AlertDialogTitle>
            <AlertDialogDescription>
              Новые события больше не будут отправляться на {deleteCandidate?.name}. Уже созданные retry jobs сохранят snapshot URL и secret до завершения 72-часового окна.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Отмена</AlertDialogCancel>
            <AlertDialogAction onClick={() => void deleteEndpoint()} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
              Удалить
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {selectedEvent && (
        <div className="grid gap-6 xl:grid-cols-2">
          <Card>
            <CardHeader><CardTitle className="font-mono text-base">{selectedEvent.type}</CardTitle><CardDescription>{selectedEvent.id}</CardDescription></CardHeader>
            <CardContent><pre className="max-h-[32rem] overflow-auto rounded-lg bg-muted p-4 font-mono text-xs leading-relaxed">{JSON.stringify(selectedEvent.payload, null, 2)}</pre></CardContent>
            <CardFooter className="justify-between gap-3">
              <p className="text-xs text-muted-foreground">Ручная доставка использует текущий endpoint и secret.</p>
              {canManageRegistry && <Button type="button" onClick={() => void redeliver()} disabled={redelivering}>
                {redelivering ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Send data-icon="inline-start" />}
                Переотправить
              </Button>}
            </CardFooter>
          </Card>

          <Card>
            <CardHeader><CardTitle>Попытки доставки</CardTitle><CardDescription>Status, latency, класс ошибки и следующая попытка.</CardDescription></CardHeader>
            <CardContent className="flex flex-col gap-3">
              {deliveriesLoading ? (
                Array.from({ length: 3 }).map((_, index) => <Skeleton key={index} className="h-24 w-full" />)
              ) : deliveries.length === 0 ? (
                <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">Попыток доставки пока нет.</div>
              ) : deliveries.map((delivery) => {
                const success = delivery.statusCode !== null && delivery.statusCode >= 200 && delivery.statusCode < 300
                return (
                  <div key={delivery.id} className="rounded-lg border p-4">
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex items-center gap-2">
                        {success ? <CheckCircle2 className="text-success" /> : <XCircle className="text-destructive" />}
                        <div>
                          <p className="text-sm font-medium">Попытка {delivery.attempt} · {delivery.statusCode ? `HTTP ${delivery.statusCode}` : delivery.errorClass || "Network error"}</p>
                          <p className="text-xs text-muted-foreground">{formatDate(delivery.createdAt)} · {delivery.responseTimeMs ?? "—"} ms</p>
                        </div>
                      </div>
                      <Badge variant={success ? "default" : "destructive"}>{success ? "Доставлено" : "Ошибка"}</Badge>
                    </div>
                    {(delivery.errorMessage || delivery.responseBody || delivery.nextRetryAt) && (
                      <div className="mt-3 flex flex-col gap-1 rounded-md bg-muted p-3 font-mono text-xs">
                        {delivery.errorMessage && <p>{delivery.errorMessage}</p>}
                        {delivery.responseBody && <p className="break-all">{delivery.responseBody}</p>}
                        {delivery.nextRetryAt && <p>nextRetryAt: {formatDate(delivery.nextRetryAt)}</p>}
                      </div>
                    )}
                  </div>
                )
              })}
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  )
}
