"use client"

import type { JsonObject } from "@/lib/json-value"

import { useMemo, useState } from "react"
import { Plus, Trash2, Pencil, Bot, Radio } from "lucide-react"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { AutomationAccount, AutomationChannel, CreateChannelRequest, UpdateChannelRequest } from "@/types/automation"
import { ACCOUNT_TYPE_META, getChannelDestinationFields } from "@/lib/automation-helpers"

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
  Table, TableBody, TableCell,
  TableHead, TableHeader, TableRow,
} from "@/components/ui/table"
import {
  Tooltip, TooltipContent, TooltipTrigger,
} from "@/components/ui/tooltip"

interface ChannelsTabProps {
  channels: AutomationChannel[]
  accounts: AutomationAccount[]
  onRefresh: () => Promise<void>
}

export function ChannelsTab({ channels, accounts, onRefresh }: ChannelsTabProps) {
  const [chDialogMode, setChDialogMode] = useState<"create" | "edit" | null>(null)
  const [chEditingId, setChEditingId] = useState<string | null>(null)
  const [chName, setChName] = useState("")
  const [chAccountId, setChAccountId] = useState("")
  const [chIsActive, setChIsActive] = useState(true)
  const [chDestination, setChDestination] = useState<Record<string, string>>({})
  const [chSaving, setChSaving] = useState(false)

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
    setChName("")
    setChAccountId(accounts.length > 0 ? accounts[0].id : "")
    setChIsActive(true)
    setChDestination({})
  }

  const openChEditDialog = (ch: AutomationChannel) => {
    setChDialogMode("edit")
    setChEditingId(ch.id)
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
      const dest: JsonObject = { ...chDestination }
      if (chDialogMode === "create") {
        const payload: CreateChannelRequest = {
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
      await onRefresh()
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Не удалось сохранить настройки"
      toast.error(msg)
    } finally {
      setChSaving(false)
    }
  }

  const handleDeleteChannel = async (ch: AutomationChannel) => {
    try {
      await api.automation.channels.delete(ch.id)
      toast.success("Канал удалён")
      onRefresh()
    } catch {
      toast.error("Не удалось удалить канал. Возможно, он используется в правилах.")
    }
  }

  return (
    <div className="space-y-4">
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

      {/* Channel Dialog */}
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
            <div className="space-y-2">
              <Label>Наименование</Label>
              <Input
                placeholder="Например: Алерты казначейства"
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
                  setChDestination({})
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

            {/* Dynamic destination fields */}
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
            <Button variant="outline" onClick={closeChDialog}>Отменить</Button>
            <Button onClick={handleSaveChannel} disabled={chSaving || !chName || !chAccountId}>
              {chSaving ? "Сохранение…" : chDialogMode === "create" ? "Создать" : "Сохранить"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
