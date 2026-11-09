"use client"

import { useState } from "react"
import { Plus, Trash2, Pencil, Bot, KeyRound, CheckCircle2, XCircle, Send, Webhook } from "lucide-react"
import { formatDistanceToNow } from "date-fns"
import { ru } from "date-fns/locale"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { AutomationAccount, CreateAccountRequest, UpdateAccountRequest } from "@/types/automation"
import { ACCOUNT_TYPE_META, ACCOUNT_STATUS_MAP, getCredentialLabel } from "@/lib/automation-helpers"

import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Dialog, DialogContent, DialogDescription,
  DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import {
  Select, SelectContent, SelectItem,
  SelectTrigger, SelectValue,
} from "@/components/ui/select"
import {
  Tooltip, TooltipContent, TooltipTrigger,
} from "@/components/ui/tooltip"

interface AccountsTabProps {
  accounts: AutomationAccount[]
  onRefresh: () => Promise<void>
}

export function AccountsTab({ accounts, onRefresh }: AccountsTabProps) {
  const [dialogMode, setDialogMode] = useState<"create" | "edit" | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [name, setName] = useState("")
  const [accountType, setAccountType] = useState<"telegram" | "webhook" | "email">("telegram")
  const [token, setToken] = useState("")
  const [saving, setSaving] = useState(false)

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
    setToken("")
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
      await onRefresh()
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Не удалось сохранить настройки"
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  const handleDeleteAccount = async (acc: AutomationAccount) => {
    try {
      await api.automation.accounts.delete(acc.id)
      toast.success("Аккаунт удалён")
      onRefresh()
    } catch {
      toast.error("Не удалось удалить. Возможно, канал используется в правилах.")
    }
  }

  return (
    <div className="space-y-4">
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

      {/* Account Dialog */}
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
                placeholder="Секретный ключ API"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                type="password"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={closeDialog}>Отменить</Button>
            <Button onClick={handleSaveAccount} disabled={saving}>
              {saving ? "Сохранение…" : dialogMode === "create" ? "Создать" : "Сохранить"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
