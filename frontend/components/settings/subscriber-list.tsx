"use client"

import { useState, useEffect } from "react"
import { Plus, X, Radio, User, Shield } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import { UserPicker } from "@/components/shared/user-picker"
import { api } from "@/lib/api"
import type { AutomationChannel } from "@/types/automation"
import type { SubscriberFormEntry } from "@/lib/automation-rule-form"

interface SubscriberListProps {
  subscribers: SubscriberFormEntry[]
  channels: AutomationChannel[]
  /** Current reaction type — hides channel option when "notify" (internal only) */
  reactionType?: string
  onChange: (subscribers: SubscriberFormEntry[]) => void
}

const SUB_TYPE_META: Record<string, { label: string; icon: typeof Radio }> = {
  channel: { label: "Канал", icon: Radio },
  user:    { label: "Пользователь", icon: User },
  role:    { label: "Роль", icon: Shield },
}

interface RoleOption {
  id: string
  code: string
  name: string
}

export function SubscriberList({ subscribers, channels, reactionType, onChange }: SubscriberListProps) {
  const [roles, setRoles] = useState<RoleOption[]>([])

  // Load roles on mount (for role subscriber type)
  useEffect(() => {
    api.roles.list()
      .then((res) => {
        setRoles(
          (res.items ?? []).map((r) => ({
            id: r.id,
            code: r.code,
            name: r.name,
          }))
        )
      })
      .catch(console.error)
  }, [])

  // All subscriber types are available for all reaction types.
  // "notify" supports both internal (user/role → UI) and external (channel → Telegram/Email).
  const allowedTypes = (["channel", "user", "role"] as const)
  const defaultSubType = channels.length > 0 ? "channel" : "user"

  const handleAdd = () => {
    if (defaultSubType === "channel") {
      const first = channels[0]
      onChange([
        ...subscribers,
        {
          subscriberType: "channel",
          channelId: first?.id ?? "",
          deliveryMethod: "push",
          displayName: first?.name ?? "",
        },
      ])
    } else {
      onChange([
        ...subscribers,
        {
          subscriberType: "user",
          deliveryMethod: "push",
          displayName: "",
        },
      ])
    }
  }

  const handleRemove = (idx: number) => {
    onChange(subscribers.filter((_, i) => i !== idx))
  }

  const handleTypeChange = (idx: number, type: "channel" | "user" | "role") => {
    const updated = [...subscribers]
    updated[idx] = {
      subscriberType: type,
      channelId: type === "channel" ? (channels[0]?.id ?? "") : undefined,
      userId: undefined,
      roleName: undefined,
      deliveryMethod: "push",
      displayName: type === "channel" ? (channels[0]?.name ?? "") : "",
    }
    onChange(updated)
  }

  const handleChannelChange = (idx: number, channelId: string) => {
    const ch = channels.find(c => c.id === channelId)
    const updated = [...subscribers]
    updated[idx] = {
      ...updated[idx],
      channelId,
      displayName: ch?.name ?? "",
    }
    onChange(updated)
  }

  const handleUserChange = (idx: number, userId: string, displayName: string) => {
    const updated = [...subscribers]
    updated[idx] = {
      ...updated[idx],
      userId,
      displayName,
    }
    onChange(updated)
  }

  const handleRoleChange = (idx: number, roleName: string) => {
    const role = roles.find(r => r.code === roleName)
    const updated = [...subscribers]
    updated[idx] = {
      ...updated[idx],
      roleName,
      displayName: role?.name ?? roleName,
    }
    onChange(updated)
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <Label className="text-xs text-muted-foreground font-semibold">
          Подписчики *
        </Label>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          onClick={handleAdd}
        >
          <Plus className="h-3.5 w-3.5 mr-1" />
          Добавить
        </Button>
      </div>

      {subscribers.length === 0 ? (
        <div className="rounded-lg border border-dashed p-6 text-center">
          <Radio className="h-8 w-8 mx-auto text-muted-foreground/40 mb-2" />
          <p className="text-xs text-muted-foreground">
            Нет подписчиков. Добавьте канал доставки, пользователя или роль.
          </p>
        </div>
      ) : (
        <div className="rounded-md border divide-y">
          {subscribers.map((sub, idx) => {
            const meta = SUB_TYPE_META[sub.subscriberType] ?? SUB_TYPE_META.channel
            const Icon = meta.icon

            return (
              <div key={idx} className="flex items-center gap-2 px-3 py-2">
                <Icon className="h-3.5 w-3.5 text-muted-foreground shrink-0" />

                {/* Subscriber type */}
                <Select
                  value={sub.subscriberType}
                  onValueChange={(v) => handleTypeChange(idx, v as "channel" | "user" | "role")}
                >
                  <SelectTrigger className="h-8 w-[140px] text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {allowedTypes.map((t) => (
                      <SelectItem key={t} value={t}>
                        {SUB_TYPE_META[t]?.label ?? t}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>

                {/* Channel selector */}
                {sub.subscriberType === "channel" && (
                  <Select
                    value={sub.channelId ?? ""}
                    onValueChange={(v) => handleChannelChange(idx, v)}
                  >
                    <SelectTrigger className="h-8 flex-1 text-xs">
                      <SelectValue placeholder="Выберите канал…" />
                    </SelectTrigger>
                    <SelectContent>
                      {channels.map(ch => (
                        <SelectItem key={ch.id} value={ch.id}>
                          {ch.name}
                          <span className="ml-2 text-muted-foreground">
                            ({ch.accountType})
                          </span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}

                {/* User picker */}
                {sub.subscriberType === "user" && (
                  <UserPicker
                    value={sub.userId ?? ""}
                    displayName={sub.displayName}
                    onChange={(userId, displayName) => handleUserChange(idx, userId, displayName)}
                    placeholder="Выберите пользователя…"
                    className="h-8 flex-1 text-xs"
                  />
                )}

                {/* Role selector */}
                {sub.subscriberType === "role" && (
                  <Select
                    value={sub.roleName ?? ""}
                    onValueChange={(v) => handleRoleChange(idx, v)}
                  >
                    <SelectTrigger className="h-8 flex-1 text-xs">
                      <SelectValue placeholder="Выберите роль…" />
                    </SelectTrigger>
                    <SelectContent>
                      {roles.map(role => (
                        <SelectItem key={role.code} value={role.code}>
                          {role.name}
                          <span className="ml-2 text-muted-foreground">
                            ({role.code})
                          </span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}

                {/* Remove */}
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7 shrink-0"
                  onClick={() => handleRemove(idx)}
                >
                  <X className="h-3.5 w-3.5 text-muted-foreground hover:text-destructive" />
                </Button>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
