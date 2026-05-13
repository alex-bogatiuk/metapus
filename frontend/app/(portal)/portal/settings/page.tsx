"use client"

import { useCallback, useEffect, useState } from "react"
import { usePortalStore } from "@/stores/usePortalStore"
import { api } from "@/lib/api"
import type { PortalSettingsResponse } from "@/types/portal-api"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Settings, Save, Loader2 } from "lucide-react"
import { toast } from "sonner"

const TTL_OPTIONS = [
  { value: "15", label: "15 минут" },
  { value: "30", label: "30 минут" },
  { value: "60", label: "1 час" },
  { value: "120", label: "2 часа" },
  { value: "360", label: "6 часов" },
  { value: "720", label: "12 часов" },
  { value: "1440", label: "24 часа" },
]

export default function SettingsPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  const [settings, setSettings] = useState<PortalSettingsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  // Form state
  const [webhookUrl, setWebhookUrl] = useState("")
  const [defaultTtl, setDefaultTtl] = useState("60")
  const [dirty, setDirty] = useState(false)

  const fetchSettings = useCallback(async () => {
    if (!activeMerchantId) return
    setLoading(true)
    try {
      const data = await api.portal.settings.get(activeMerchantId)
      setSettings(data)
      setWebhookUrl(data.webhookUrl)
      setDefaultTtl(String(data.defaultTtlMinutes))
      setDirty(false)
    } catch {
      toast.error("Не удалось загрузить настройки")
    } finally {
      setLoading(false)
    }
  }, [activeMerchantId])

  useEffect(() => { fetchSettings() }, [fetchSettings])

  const handleSave = async () => {
    if (!activeMerchantId) return
    setSaving(true)
    try {
      const resp = await api.portal.settings.update(activeMerchantId, {
        webhookUrl,
        defaultTtlMinutes: parseInt(defaultTtl),
      })
      setSettings(resp)
      setDirty(false)
      toast.success("Настройки сохранены")
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Ошибка сохранения"
      toast.error(msg)
    } finally {
      setSaving(false)
    }
  }

  if (!activeMerchantId) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
        <Settings className="size-10 mb-3 opacity-40" />
        <p>Выберите мерчанта для настройки</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Настройки</h1>
          <p className="text-sm text-muted-foreground">
            Управление параметрами мерчанта
          </p>
        </div>
        <Button
          size="sm"
          onClick={handleSave}
          disabled={saving || !dirty}
        >
          {saving ? (
            <Loader2 className="size-4 mr-1.5 animate-spin" />
          ) : (
            <Save className="size-4 mr-1.5" />
          )}
          Сохранить
        </Button>
      </div>

      {loading ? (
        <div className="space-y-4">
          <Skeleton className="h-48 w-full" />
          <Skeleton className="h-32 w-full" />
        </div>
      ) : (
        <div className="grid gap-6 md:grid-cols-2">
          {/* Webhook Settings */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Вебхуки</CardTitle>
              <CardDescription>
                URL для получения уведомлений о статусах инвойсов
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="webhook-url">Webhook URL</Label>
                <Input
                  id="webhook-url"
                  placeholder="https://example.com/webhook"
                  value={webhookUrl}
                  onChange={(e) => {
                    setWebhookUrl(e.target.value)
                    setDirty(true)
                  }}
                />
                <p className="text-[11px] text-muted-foreground">
                  Должен быть публичным HTTPS URL. Локальные и приватные адреса запрещены.
                </p>
              </div>
            </CardContent>
          </Card>

          {/* Invoice Defaults */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Инвойсы</CardTitle>
              <CardDescription>
                Значения по умолчанию для новых инвойсов
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="space-y-2">
                <Label>TTL по умолчанию</Label>
                <Select
                  value={defaultTtl}
                  onValueChange={(v) => {
                    setDefaultTtl(v)
                    setDirty(true)
                  }}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {TTL_OPTIONS.map((opt) => (
                      <SelectItem key={opt.value} value={opt.value}>
                        {opt.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-[11px] text-muted-foreground">
                  Время жизни инвойса, если не указано явно при создании
                </p>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  )
}
