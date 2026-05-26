"use client"

import { useCallback, useEffect, useState } from "react"
import { usePortalStore } from "@/stores/usePortalStore"
import { api } from "@/lib/api"
import { formatMinorUnits } from "@/lib/format"
import type { PortalSettingsResponse, PortalFeeItem } from "@/types/portal-api"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Settings,
  Save,
  Loader2,
  Eye,
  EyeOff,
  RotateCw,
  Copy,
  Check,
  ShieldCheck,
  ExternalLink,
} from "lucide-react"
import { toast } from "sonner"
import Link from "next/link"

const TTL_OPTIONS = [
  { value: "15", label: "15 минут" },
  { value: "30", label: "30 минут" },
  { value: "60", label: "1 час" },
  { value: "120", label: "2 часа" },
  { value: "360", label: "6 часов" },
  { value: "720", label: "12 часов" },
  { value: "1440", label: "24 часа" },
]

const DIRECTION_LABELS: Record<string, string> = {
  processing: "Приём платежей",
  withdrawal: "Вывод средств",
}

export default function SettingsPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  const [settings, setSettings] = useState<PortalSettingsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  // Form state
  const [webhookUrl, setWebhookUrl] = useState("")
  const [defaultTtl, setDefaultTtl] = useState("60")
  const [dirty, setDirty] = useState(false)

  // Webhook secret state
  const [secret, setSecret] = useState<string | null>(null)
  const [secretVisible, setSecretVisible] = useState(false)
  const [revealingSecret, setRevealingSecret] = useState(false)
  const [rotatingSecret, setRotatingSecret] = useState(false)
  const [copiedSecret, setCopiedSecret] = useState(false)

  // Fee schedule state
  const [fees, setFees] = useState<PortalFeeItem[]>([])
  const [feesLoading, setFeesLoading] = useState(true)

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

  const fetchFees = useCallback(async () => {
    if (!activeMerchantId) return
    setFeesLoading(true)
    try {
      const data = await api.portal.settings.fees(activeMerchantId)
      setFees(data.items)
    } catch {
      setFees([])
    } finally {
      setFeesLoading(false)
    }
  }, [activeMerchantId])

  useEffect(() => {
    fetchSettings()
    fetchFees()
    // Reset secret state when merchant changes
    setSecret(null)
    setSecretVisible(false)
  }, [fetchSettings, fetchFees])

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

  const handleRevealSecret = async () => {
    if (!activeMerchantId) return
    setRevealingSecret(true)
    try {
      const resp = await api.portal.settings.revealSecret(activeMerchantId)
      setSecret(resp.secret)
      setSecretVisible(true)
    } catch {
      toast.error("Не удалось получить секрет")
    } finally {
      setRevealingSecret(false)
    }
  }

  const handleRotateSecret = async () => {
    if (!activeMerchantId) return
    setRotatingSecret(true)
    try {
      const resp = await api.portal.settings.rotateSecret(activeMerchantId)
      setSecret(resp.secret)
      setSecretVisible(true)
      toast.success("Секрет успешно обновлён")
    } catch {
      toast.error("Не удалось обновить секрет")
    } finally {
      setRotatingSecret(false)
    }
  }

  const handleCopySecret = () => {
    if (!secret) return
    navigator.clipboard?.writeText(secret)
    setCopiedSecret(true)
    setTimeout(() => setCopiedSecret(false), 2000)
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
          <Skeleton className="h-48 w-full" />
        </div>
      ) : (
        <div className="space-y-6">
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

          {/* Webhook Secret */}
          <Card>
            <CardHeader>
              <div className="flex items-center gap-2">
                <ShieldCheck className="size-4 text-muted-foreground" />
                <CardTitle className="text-base">Signing Secret</CardTitle>
              </div>
              <CardDescription>
                Секрет для верификации подписи вебхуков (HMAC-SHA256).{" "}
                <Link
                  href="/portal/developers/webhooks/verify"
                  className="text-primary hover:underline inline-flex items-center gap-0.5"
                >
                  Гайд по верификации
                  <ExternalLink className="size-3" />
                </Link>
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {secret && secretVisible ? (
                <div className="flex items-center gap-2">
                  <code className="flex-1 rounded-md border bg-muted/50 px-3 py-2 font-mono text-sm break-all">
                    {secret}
                  </code>
                  <Button variant="outline" size="icon" onClick={handleCopySecret}>
                    {copiedSecret ? (
                      <Check className="size-4 text-green-500" />
                    ) : (
                      <Copy className="size-4" />
                    )}
                  </Button>
                  <Button variant="outline" size="icon" onClick={() => setSecretVisible(false)}>
                    <EyeOff className="size-4" />
                  </Button>
                </div>
              ) : (
                <div className="flex items-center gap-2">
                  <code className="flex-1 rounded-md border bg-muted/50 px-3 py-2 font-mono text-sm text-muted-foreground">
                    whsec_••••••••••••••••••••••••••••••••
                  </code>
                  <Button
                    variant="outline"
                    size="icon"
                    onClick={handleRevealSecret}
                    disabled={revealingSecret}
                  >
                    {revealingSecret ? (
                      <Loader2 className="size-4 animate-spin" />
                    ) : (
                      <Eye className="size-4" />
                    )}
                  </Button>
                </div>
              )}

              <AlertDialog>
                <AlertDialogTrigger asChild>
                  <Button variant="outline" size="sm" disabled={rotatingSecret}>
                    {rotatingSecret ? (
                      <Loader2 className="size-4 mr-1.5 animate-spin" />
                    ) : (
                      <RotateCw className="size-4 mr-1.5" />
                    )}
                    Ротировать секрет
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>Ротировать webhook секрет?</AlertDialogTitle>
                    <AlertDialogDescription>
                      Будет сгенерирован новый секрет. Старый секрет <strong>немедленно</strong> перестанет
                      работать. Все вебхуки, подписанные старым секретом, будут отклонены вашим сервером.
                      Обновите секрет на вашей стороне <strong>сразу после ротации</strong>.
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>Отмена</AlertDialogCancel>
                    <AlertDialogAction onClick={handleRotateSecret}>
                      Ротировать
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            </CardContent>
          </Card>

          {/* Fee Schedule */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Тарифы</CardTitle>
              <CardDescription>
                Текущие комиссии для ваших операций
              </CardDescription>
            </CardHeader>
            <CardContent>
              {feesLoading ? (
                <div className="space-y-2">
                  <Skeleton className="h-8 w-full" />
                  <Skeleton className="h-8 w-full" />
                  <Skeleton className="h-8 w-full" />
                </div>
              ) : fees.length === 0 ? (
                <p className="text-sm text-muted-foreground py-4 text-center">
                  Используются стандартные комиссии платформы
                </p>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Токен</TableHead>
                      <TableHead>Операция</TableHead>
                      <TableHead className="text-right">Фикс.</TableHead>
                      <TableHead className="text-right">%</TableHead>
                      <TableHead className="text-right">Мин</TableHead>
                      <TableHead className="text-right">Макс</TableHead>
                      <TableHead />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {fees.map((fee, i) => (
                      <TableRow key={i}>
                        <TableCell className="font-medium">
                          {fee.tokenSymbol}
                          <span className="text-muted-foreground text-xs ml-1.5">
                            {fee.network}
                          </span>
                        </TableCell>
                        <TableCell>
                          {DIRECTION_LABELS[fee.direction] ?? fee.direction}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {fee.fixedFee !== "0"
                            ? formatMinorUnits(fee.fixedFee, fee.decimalPlaces)
                            : "—"}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {fee.percentBp > 0
                            ? `${(fee.percentBp / 100).toFixed(2)}%`
                            : "—"}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {fee.minFee !== "0"
                            ? formatMinorUnits(fee.minFee, fee.decimalPlaces)
                            : "—"}
                        </TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {fee.maxFee !== "0"
                            ? formatMinorUnits(fee.maxFee, fee.decimalPlaces)
                            : "—"}
                        </TableCell>
                        <TableCell>
                          {fee.isCustom && (
                            <Badge variant="secondary" className="text-[10px]">
                              Кастомная
                            </Badge>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  )
}
