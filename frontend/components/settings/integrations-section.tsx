"use client"

import { useCallback, useEffect, useState, useMemo } from "react"
import { useRouter } from "next/navigation"
import {
  Plus, Trash2, Pencil, Bot, Webhook, CheckCircle2,
  XCircle, Clock, Send, RefreshCw,
  KeyRound, Radio, FileText, Activity,
} from "lucide-react"
import { formatDistanceToNow } from "date-fns"
import { ru } from "date-fns/locale"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { AutomationAccount, AutomationChannel, AutomationRule, AutomationHistoryEntry } from "@/types/automation"
import type { CreateAccountRequest, UpdateAccountRequest, CreateChannelRequest, UpdateChannelRequest } from "@/types/automation"
import { ACCOUNT_TYPE_META, ACCOUNT_STATUS_MAP, HISTORY_STATUS_MAP, getCredentialLabel, getChannelDestinationFields } from "@/lib/automation-helpers"

import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Dialog, DialogContent, DialogDescription,
  DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select, SelectContent, SelectItem,
  SelectTrigger, SelectValue,
} from "@/components/ui/select"
import {
  Table, TableBody, TableCell,
  TableHead, TableHeader, TableRow,
} from "@/components/ui/table"
import {
  Tooltip, TooltipContent, TooltipProvider, TooltipTrigger,
} from "@/components/ui/tooltip"

// ── Event Types (action labels from automation-rule-form.ts) ──────────────

import { getActionLabel } from "@/lib/automation-rule-form"
import { cronToConfig, getScheduleDescription } from "@/lib/cron-config"
import { useMetadataStore } from "@/stores/useMetadataStore"

const TRIGGER_ICONS = { entity_event: FileText, business_event: Activity, scheduled: Clock } as const
const TRIGGER_LABELS: Record<string, string> = {
  entity_event: "Событие",
  business_event: "Бизнес",
  scheduled: "Расписание",
}

/** Format rule trigger for display in list. */
function formatTriggerLabel(rule: AutomationRule): string {
  if (rule.triggerType === "scheduled" && rule.eventType.startsWith("cron:")) {
    const expr = rule.eventType.slice(5)
    const config = cronToConfig(expr)
    return getScheduleDescription(config)
  }

  const actionLabel = getActionLabel(rule.eventType)

  if (rule.triggerType === "entity_event") {
    const entities = rule.targetEntities ?? []
    if (entities.length === 0) {
      return `${actionLabel} → Любая сущность`
    }
    const store = useMetadataStore.getState()
    const names = entities.map(k => store.getEntity(k)?.presentation.singular ?? k)
    return `${actionLabel} → ${names.join(", ")}`
  }

  return actionLabel
}

// Status helpers imported from @/lib/automation-helpers

// ── Main Component ────────────────────────────────────────────────────────

export function IntegrationsSection() {
  const router = useRouter()
  const [accounts, setAccounts] = useState<AutomationAccount[]>([])
  const [channels, setChannels] = useState<AutomationChannel[]>([])
  const [rules, setRules] = useState<AutomationRule[]>([])
  const [history, setHistory] = useState<AutomationHistoryEntry[]>([])
  const [loading, setLoading] = useState(true)

  // Dialog state — Account
  const [dialogMode, setDialogMode] = useState<"create" | "edit" | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)

  // Form state for service account dialog
  const [name, setName] = useState("")
  const [accountType, setAccountType] = useState<"telegram" | "webhook" | "email">("telegram")
  const [token, setToken] = useState("")
  const [saving, setSaving] = useState(false)

  // Dialog state — Channel
  const [chDialogMode, setChDialogMode] = useState<"create" | "edit" | null>(null)
  const [chEditingId, setChEditingId] = useState<string | null>(null)
  const [chCode, setChCode] = useState("")
  const [chName, setChName] = useState("")
  const [chAccountId, setChAccountId] = useState("")
  const [chIsActive, setChIsActive] = useState(true)
  const [chDestination, setChDestination] = useState<Record<string, string>>({})
  const [chSaving, setChSaving] = useState(false)

  // ── Fetchers ────────────────────────────────────────────────────────────

  const fetchAccounts = useCallback(async () => {
    try {
      const data = await api.automation.accounts.list()
      setAccounts(data)
    } catch (e) {
      console.error(e)
    }
  }, [])

  const fetchChannels = useCallback(async () => {
    try {
      const data = await api.automation.channels.list()
      setChannels(data)
    } catch (e) {
      console.error(e)
    }
  }, [])

  const fetchRules = useCallback(async () => {
    try {
      const data = await api.automation.rules.list()
      setRules(Array.isArray(data) ? data : [])
    } catch (e) {
      console.error(e)
    }
  }, [])

  const fetchHistory = useCallback(async () => {
    try {
      const resp = await api.automation.history.list({ limit: 20 })
      setHistory(resp.items ?? [])
    } catch (e) {
      console.error(e)
    }
  }, [])

  useEffect(() => {
    Promise.all([fetchAccounts(), fetchChannels(), fetchRules(), fetchHistory()]).finally(() =>
      setLoading(false)
    )
  }, [fetchAccounts, fetchChannels, fetchRules, fetchHistory])

  // ── Dialog handlers ─────────────────────────────────────────────────────

  const openCreateDialog = () => {
    setDialogMode("create")
    setEditingId(null)
    setName("")
    setAccountType("telegram")
    setToken("")
  }

  const openEditDialog = (acc: AutomationAccount) => {
    setDialogMode("edit")
    setEditingId(acc.id)
    setName(acc.name)
    setAccountType(acc.accountType as "telegram" | "webhook" | "email")
    setToken("") // token is never returned from API — user re-enters if changing
  }

  const closeDialog = () => {
    setDialogMode(null)
    setEditingId(null)
  }

  const handleSaveAccount = async () => {
    if (!name) return
    if (dialogMode === "create" && accountType === "telegram" && !token) return

    setSaving(true)
    try {
      if (dialogMode === "create") {
        const payload: CreateAccountRequest = {
          name,
          accountType,
          isActive: true,
          config: {},
          credentials: token || undefined,
        }
        await api.automation.accounts.create(payload)
        toast.success("Аккаунт подключён")
      } else if (editingId) {
        const existing = accounts.find(a => a.id === editingId)
        const payload: UpdateAccountRequest = {
          name,
          config: existing?.config ?? {},
          isActive: true,
          version: existing?.version ?? 1,
        }
        await api.automation.accounts.update(editingId, payload)
        if (token) {
          await api.automation.accounts.updateCredentials(editingId, token)
        }
        toast.success("Аккаунт обновлён")
      }
      closeDialog()
      await fetchAccounts()
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : "Ошибка сохранения"
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteAccount = async (acc: AutomationAccount) => {
    try {
      await api.automation.accounts.delete(acc.id)
      toast.success("Аккаунт удалён")
      fetchAccounts()
    } catch {
      toast.error("Не удалось удалить. Возможно, канал используется в правилах.")
    }
  }

  const handleDeleteRule = async (rule: AutomationRule) => {
    try {
      await api.automation.rules.delete(rule.id)
      toast.success("Правило удалено")
      fetchRules()
    } catch {
      toast.error("Не удалось удалить правило")
    }
  }

  // ── Channel Dialog handlers ────────────────────────────────────────────

  const selectedChAccountType = useMemo(() => {
    if (!chAccountId) return null
    const acc = accounts.find(a => a.id === chAccountId)
    return acc ? (acc.accountType as "telegram" | "email" | "webhook") : null
  }, [chAccountId, accounts])

  const chDestFields = useMemo(() => {
    if (!selectedChAccountType) return []
    return getChannelDestinationFields(selectedChAccountType)
  }, [selectedChAccountType])

  const openChCreateDialog = () => {
    setChDialogMode("create")
    setChEditingId(null)
    setChCode("")
    setChName("")
    setChAccountId(accounts.length > 0 ? accounts[0].id : "")
    setChIsActive(true)
    setChDestination({})
  }

  const openChEditDialog = (ch: AutomationChannel) => {
    setChDialogMode("edit")
    setChEditingId(ch.id)
    setChCode(ch.code)
    setChName(ch.name)
    setChAccountId(ch.accountId)
    setChIsActive(ch.isActive)
    const dest: Record<string, string> = {}
    for (const [k, v] of Object.entries(ch.destination || {})) {
      dest[k] = String(v)
    }
    setChDestination(dest)
  }

  const closeChDialog = () => {
    setChDialogMode(null)
    setChEditingId(null)
  }

  const handleSaveChannel = async () => {
    if (!chName || !chAccountId) return
    setChSaving(true)
    try {
      const dest: Record<string, unknown> = { ...chDestination }
      if (chDialogMode === "create") {
        const payload: CreateChannelRequest = {
          code: chCode,
          name: chName,
          accountId: chAccountId,
          destination: dest,
          isActive: chIsActive,
        }
        await api.automation.channels.create(payload)
        toast.success("Канал создан")
      } else if (chEditingId) {
        const existing = channels.find(c => c.id === chEditingId)
        const payload: UpdateChannelRequest = {
          name: chName,
          accountId: chAccountId,
          destination: dest,
          isActive: chIsActive,
          version: existing?.version ?? 1,
        }
        await api.automation.channels.update(chEditingId, payload)
        toast.success("Канал обновлён")
      }
      closeChDialog()
      await fetchChannels()
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : "Ошибка сохранения"
      toast.error(msg)
    } finally {
      setChSaving(false)
    }
  }

  const handleDeleteChannel = async (ch: AutomationChannel) => {
    try {
      await api.automation.channels.delete(ch.id)
      toast.success("Канал удалён")
      fetchChannels()
    } catch {
      toast.error("Не удалось удалить канал. Возможно, он используется в правилах.")
    }
  }

  // ── Helpers ─────────────────────────────────────────────────────────────

  // ── Render ──────────────────────────────────────────────────────────────

  if (loading) {
    return <div className="text-sm text-muted-foreground py-4">Загрузка...</div>
  }

  return (
    <TooltipProvider>
      <Tabs defaultValue="rules" className="space-y-4">
        <div className="flex items-center justify-between">
          <TabsList>
            <TabsTrigger value="rules" className="gap-1.5">
              <Send className="h-3.5 w-3.5" />
              Правила
              {rules.length > 0 && (
                <Badge variant="secondary" className="h-5 min-w-5 px-1.5 text-[10px] rounded-full">
                  {rules.length}
                </Badge>
              )}
            </TabsTrigger>
            <TabsTrigger value="channels" className="gap-1.5">
              <Radio className="h-3.5 w-3.5" />
              Каналы
              {channels.length > 0 && (
                <Badge variant="secondary" className="h-5 min-w-5 px-1.5 text-[10px] rounded-full">
                  {channels.length}
                </Badge>
              )}
            </TabsTrigger>
            <TabsTrigger value="accounts" className="gap-1.5">
              <KeyRound className="h-3.5 w-3.5" />
              Аккаунты
              {accounts.length > 0 && (
                <Badge variant="secondary" className="h-5 min-w-5 px-1.5 text-[10px] rounded-full">
                  {accounts.length}
                </Badge>
              )}
            </TabsTrigger>
            <TabsTrigger value="history" className="gap-1.5">
              <Clock className="h-3.5 w-3.5" />
              Журнал
            </TabsTrigger>
          </TabsList>
        </div>

        {/* ── TAB: Accounts ─────────────────────────────────────────── */}

        <TabsContent value="accounts" className="space-y-4">
          <div className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">
              Сервисные аккаунты для доставки (Telegram Bot, SMTP, Webhook)
            </p>
            <Button size="sm" onClick={openCreateDialog}>
              <Plus className="h-4 w-4 mr-1.5" />
              Добавить аккаунт
            </Button>
          </div>

          {accounts.length === 0 ? (
            <div className="rounded-lg border border-dashed p-10 text-center">
              <KeyRound className="h-10 w-10 mx-auto text-muted-foreground/40 mb-3" />
              <p className="text-sm text-muted-foreground">
                Нет подключённых аккаунтов.
              </p>
              <p className="text-xs text-muted-foreground mt-1">
                Добавьте Telegram Bot, Email SMTP или Webhook аккаунт.
              </p>
              <Button size="sm" variant="outline" className="mt-4" onClick={openCreateDialog}>
                <Plus className="h-4 w-4 mr-1.5" />
                Добавить первый аккаунт
              </Button>
            </div>
          ) : (
            <div className="space-y-3">
              {accounts.map((acc) => {
                const meta = ACCOUNT_TYPE_META[acc.accountType] ?? { label: acc.accountType, icon: Bot }
                const Icon = meta.icon
                const status = ACCOUNT_STATUS_MAP[acc.status] ?? ACCOUNT_STATUS_MAP.active

                return (
                  <div
                    key={acc.id}
                    className="rounded-lg border p-4 hover:bg-muted/20 transition-colors"
                  >
                    <div className="flex items-start justify-between">
                      <div className="flex items-start gap-3">
                        <div className="mt-0.5 rounded-md bg-muted p-2">
                          <Icon className="h-4 w-4 text-muted-foreground" />
                        </div>
                        <div className="space-y-1">
                          <div className="flex items-center gap-2">
                            <span className="font-medium text-sm">{acc.name}</span>
                            <Badge variant={status.variant} className="text-[10px]">
                              {status.label}
                            </Badge>
                            <Badge variant="outline" className="text-[10px]">{meta.label}</Badge>
                          </div>
                          <div className="flex items-center gap-4 text-[11px] text-muted-foreground mt-1">
                            <span>Каналов: {acc.channelCount ?? 0}</span>
                            {acc.lastSuccessAt && (
                              <span className="flex items-center gap-1">
                                <CheckCircle2 className="h-3 w-3 text-green-500" />
                                Посл. отправка: {formatDistanceToNow(new Date(acc.lastSuccessAt), { addSuffix: true, locale: ru })}
                              </span>
                            )}
                            {acc.lastError && (
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <span className="flex items-center gap-1 text-destructive cursor-help">
                                    <XCircle className="h-3 w-3" />
                                    Ошибка
                                  </span>
                                </TooltipTrigger>
                                <TooltipContent side="bottom" className="max-w-xs">
                                  <p className="text-xs">{acc.lastError}</p>
                                </TooltipContent>
                              </Tooltip>
                            )}
                          </div>
                        </div>
                      </div>

                      <div className="flex items-center gap-1 shrink-0">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => openEditDialog(acc)}>
                              <Pencil className="h-3.5 w-3.5 text-muted-foreground" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>Изменить</TooltipContent>
                        </Tooltip>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => handleDeleteAccount(acc)}>
                              <Trash2 className="h-3.5 w-3.5 text-muted-foreground hover:text-destructive" />
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>Удалить</TooltipContent>
                        </Tooltip>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </TabsContent>

        {/* ── TAB: Channels ────────────────────────────────────────────── */}

        <TabsContent value="channels" className="space-y-4">
          <div className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">
              Канал = конкретная точка доставки (чат, email, URL) привязанная к аккаунту
            </p>
            <Button size="sm" onClick={openChCreateDialog} disabled={accounts.length === 0}>
              <Plus className="h-4 w-4 mr-1.5" />
              Добавить канал
            </Button>
          </div>

          {accounts.length === 0 && channels.length === 0 ? (
            <div className="rounded-lg border border-dashed p-10 text-center">
              <Radio className="h-10 w-10 mx-auto text-muted-foreground/40 mb-3" />
              <p className="text-sm text-muted-foreground">Нет каналов доставки.</p>
              <p className="text-xs text-muted-foreground mt-1">
                Сначала создайте аккаунт на вкладке «Аккаунты», затем добавьте каналы доставки.
              </p>
            </div>
          ) : channels.length === 0 ? (
            <div className="rounded-lg border border-dashed p-10 text-center">
              <Radio className="h-10 w-10 mx-auto text-muted-foreground/40 mb-3" />
              <p className="text-sm text-muted-foreground">Нет каналов доставки.</p>
              <p className="text-xs text-muted-foreground mt-1">
                Добавьте канал, чтобы привязать точку доставки к аккаунту.
              </p>
              <Button size="sm" variant="outline" className="mt-4" onClick={openChCreateDialog}>
                <Plus className="h-4 w-4 mr-1.5" />
                Добавить первый канал
              </Button>
            </div>
          ) : (
            <div className="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Наименование</TableHead>
                    <TableHead>Аккаунт</TableHead>
                    <TableHead>Destination</TableHead>
                    <TableHead className="w-[80px]">Статус</TableHead>
                    <TableHead className="w-[80px]" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {channels.map((ch) => (
                    <TableRow key={ch.id}>
                      <TableCell className="font-medium text-sm">{ch.name}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">{ch.accountName ?? "—"}</TableCell>
                      <TableCell className="text-xs text-muted-foreground font-mono">
                        {Object.entries(ch.destination || {}).map(([k, v]) => `${k}: ${v}`).join(", ") || "—"}
                      </TableCell>
                      <TableCell>
                        <Badge variant={ch.isActive ? "default" : "secondary"} className="text-[10px]">
                          {ch.isActive ? "Активен" : "Откл."}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-0.5">
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button variant="ghost" size="icon" className="h-7 w-7" onClick={() => openChEditDialog(ch)}>
                                <Pencil className="h-3.5 w-3.5 text-muted-foreground" />
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>Изменить</TooltipContent>
                          </Tooltip>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button
                                variant="ghost" size="icon" className="h-7 w-7"
                                onClick={() => handleDeleteChannel(ch)}
                              >
                                <Trash2 className="h-3.5 w-3.5 text-muted-foreground hover:text-destructive" />
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>Удалить</TooltipContent>
                          </Tooltip>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </TabsContent>

        {/* ── TAB: Rules ─────────────────────────────────────────────── */}

        <TabsContent value="rules" className="space-y-4">
          <div className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">
              Правило определяет, при каком событии и куда отправлять уведомление
            </p>
            <Button size="sm" onClick={() => router.push("/settings/automation-rules/new")}>
              <Plus className="h-4 w-4 mr-1.5" />
              Создать правило
            </Button>
          </div>

          {rules.length === 0 ? (
            <div className="rounded-lg border border-dashed p-10 text-center">
              <Send className="h-10 w-10 mx-auto text-muted-foreground/40 mb-3" />
              <p className="text-sm text-muted-foreground">
                Нет правил автоматизации.
              </p>
              <p className="text-xs text-muted-foreground mt-1">
                Правило определяет, при каком событии отправлять уведомление.
              </p>
            </div>
          ) : (
            <div className="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Наименование</TableHead>
                    <TableHead>Триггер</TableHead>
                    <TableHead>Подписчики</TableHead>
                    <TableHead className="w-[80px]">Статус</TableHead>
                    <TableHead className="w-[50px]" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rules.map((rule) => {
                    const TrigIcon = TRIGGER_ICONS[rule.triggerType as keyof typeof TRIGGER_ICONS] ?? FileText
                    return (
                      <TableRow
                        key={rule.id}
                        className="cursor-pointer hover:bg-muted/50"
                        onClick={() => router.push(`/settings/automation-rules/${rule.id}`)}
                      >
                        <TableCell className="font-medium text-sm">{rule.name}</TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          <div className="flex items-center gap-1.5">
                            <TrigIcon className="h-3.5 w-3.5 shrink-0" />
                            <span className="truncate" title={formatTriggerLabel(rule)}>
                              {formatTriggerLabel(rule)}
                            </span>
                          </div>
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {(rule.subscribers?.length || 0)} подп.
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant={rule.isActive ? "default" : "secondary"}
                            className="text-[10px]"
                          >
                            {rule.isActive ? "Активно" : "Откл."}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7"
                            onClick={(e) => {
                              e.stopPropagation()
                              handleDeleteRule(rule)
                            }}
                          >
                            <Trash2 className="h-3.5 w-3.5 text-muted-foreground hover:text-destructive" />
                          </Button>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </TabsContent>

        {/* ── TAB: History ────────────────────────────────────────────── */}

        <TabsContent value="history" className="space-y-4">
          <div className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">
              Последние отправки уведомлений
            </p>
            <Button variant="outline" size="sm" onClick={fetchHistory}>
              <RefreshCw className="h-3.5 w-3.5 mr-1.5" />
              Обновить
            </Button>
          </div>

          {history.length === 0 ? (
            <div className="rounded-lg border border-dashed p-10 text-center">
              <Clock className="h-10 w-10 mx-auto text-muted-foreground/40 mb-3" />
              <p className="text-sm text-muted-foreground">Журнал отправок пуст</p>
              <p className="text-xs text-muted-foreground mt-1">
                Здесь появятся записи после первого срабатывания правил.
              </p>
            </div>
          ) : (
            <div className="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-[160px]">Время</TableHead>
                    <TableHead>Правило</TableHead>
                    <TableHead>Событие</TableHead>
                    <TableHead className="w-[80px]">Результат</TableHead>
                    <TableHead>Ошибка</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {history.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell className="text-xs text-muted-foreground font-mono">
                        {formatDistanceToNow(new Date(item.createdAt), { addSuffix: true, locale: ru })}
                      </TableCell>
                      <TableCell className="text-sm">
                        {item.ruleName || item.ruleId.substring(0, 8) + "…"}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {getActionLabel(item.eventType)}
                      </TableCell>
                      <TableCell>
                        {(() => {
                          const st = HISTORY_STATUS_MAP[item.status] ?? HISTORY_STATUS_MAP.pending
                          return (
                            <Badge variant={st.variant} className="text-[10px]">
                              {item.status === "success" && <CheckCircle2 className="h-3 w-3 mr-1" />}
                              {item.status === "error" && <XCircle className="h-3 w-3 mr-1" />}
                              {st.label}
                            </Badge>
                          )
                        })()}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground max-w-[200px] truncate">
                        {item.errorText ? (
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <span className="text-destructive cursor-help">{item.errorText}</span>
                            </TooltipTrigger>
                            <TooltipContent side="bottom" className="max-w-sm">
                              <p className="text-xs break-all">{item.errorText}</p>
                            </TooltipContent>
                          </Tooltip>
                        ) : (
                          "—"
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </TabsContent>
      </Tabs>

      {/* ── Account Dialog (Create / Edit) ──────────────────────────── */}

      <Dialog open={dialogMode !== null} onOpenChange={(open) => !open && closeDialog()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {dialogMode === "create" ? "Новый аккаунт" : "Изменить аккаунт"}
            </DialogTitle>
            <DialogDescription>
              {dialogMode === "create"
                ? "Укажите тип аккаунта и секретный ключ для подключения"
                : "Измените параметры. Оставьте секрет пустым, если не хотите его менять."}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-2">
              <Label>Наименование</Label>
              <Input
                placeholder="Например: Telegram — Основной бот"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label>Тип аккаунта</Label>
              <Select
                value={accountType}
                onValueChange={(v) => setAccountType(v as "telegram" | "webhook" | "email")}
                disabled={dialogMode === "edit"}
              >
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="telegram">
                    <span className="flex items-center gap-2"><Bot className="h-3.5 w-3.5" /> Telegram Bot</span>
                  </SelectItem>
                  <SelectItem value="email">
                    <span className="flex items-center gap-2"><Send className="h-3.5 w-3.5" /> Email SMTP</span>
                  </SelectItem>
                  <SelectItem value="webhook">
                    <span className="flex items-center gap-2"><Webhook className="h-3.5 w-3.5" /> Webhook</span>
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>
                {getCredentialLabel(accountType)}
                {dialogMode === "edit" && (
                  <span className="text-muted-foreground font-normal ml-1">(оставьте пустым, если без изменений)</span>
                )}
              </Label>
              <Input
                placeholder="Введите секретный ключ..."
                value={token}
                onChange={(e) => setToken(e.target.value)}
                type="password"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={closeDialog}>Отмена</Button>
            <Button onClick={handleSaveAccount} disabled={saving}>
              {saving ? "Сохранение…" : dialogMode === "create" ? "Создать" : "Сохранить"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ── Channel Dialog (Create / Edit) ─────────────────────────────── */}

      <Dialog open={chDialogMode !== null} onOpenChange={(open) => !open && closeChDialog()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {chDialogMode === "create" ? "Новый канал" : "Изменить канал"}
            </DialogTitle>
            <DialogDescription>
              {chDialogMode === "create"
                ? "Укажите аккаунт и параметры точки доставки"
                : "Измените параметры канала доставки"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            {chDialogMode === "create" && (
              <div className="space-y-2">
                <Label>Код</Label>
                <Input
                  placeholder="Например: tg-main-chat"
                  value={chCode}
                  onChange={(e) => setChCode(e.target.value)}
                />
              </div>
            )}
            <div className="space-y-2">
              <Label>Наименование</Label>
              <Input
                placeholder="Например: Основной чат закупок"
                value={chName}
                onChange={(e) => setChName(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label>Аккаунт</Label>
              <Select
                value={chAccountId}
                onValueChange={(v) => {
                  setChAccountId(v)
                  setChDestination({}) // Reset destination when account changes
                }}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Выберите аккаунт" />
                </SelectTrigger>
                <SelectContent>
                  {accounts.map((acc) => {
                    const meta = ACCOUNT_TYPE_META[acc.accountType] ?? { label: acc.accountType, icon: Bot }
                    return (
                      <SelectItem key={acc.id} value={acc.id}>
                        <span className="flex items-center gap-2">
                          <meta.icon className="h-3.5 w-3.5" />
                          {acc.name}
                          <span className="text-xs text-muted-foreground">({meta.label})</span>
                        </span>
                      </SelectItem>
                    )
                  })}
                </SelectContent>
              </Select>
            </div>

            {/* Dynamic destination fields based on account type */}
            {chDestFields.length > 0 && (
              <div className="space-y-3 rounded-md border p-3">
                <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Параметры доставки</p>
                {chDestFields.map((field) => (
                  <div key={field.key} className="space-y-1.5">
                    <Label className="text-sm">
                      {field.label}
                      {field.required && <span className="text-destructive ml-0.5">*</span>}
                    </Label>
                    {field.type === "select" && field.options ? (
                      <Select
                        value={chDestination[field.key] ?? ""}
                        onValueChange={(v) => setChDestination(prev => ({ ...prev, [field.key]: v }))}
                      >
                        <SelectTrigger><SelectValue placeholder={field.placeholder} /></SelectTrigger>
                        <SelectContent>
                          {field.options.map((opt) => (
                            <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    ) : (
                      <Input
                        placeholder={field.placeholder}
                        value={chDestination[field.key] ?? ""}
                        onChange={(e) => setChDestination(prev => ({ ...prev, [field.key]: e.target.value }))}
                        type={field.type === "password" ? "password" : "text"}
                      />
                    )}
                  </div>
                ))}
              </div>
            )}

            <div className="flex items-center gap-2">
              <input
                type="checkbox"
                id="ch-is-active"
                checked={chIsActive}
                onChange={(e) => setChIsActive(e.target.checked)}
                className="h-4 w-4 rounded border-gray-300"
              />
              <Label htmlFor="ch-is-active" className="text-sm cursor-pointer">Активен</Label>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={closeChDialog}>Отмена</Button>
            <Button onClick={handleSaveChannel} disabled={chSaving || !chName || !chAccountId}>
              {chSaving ? "Сохранение…" : chDialogMode === "create" ? "Создать" : "Сохранить"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </TooltipProvider>
  )
}

