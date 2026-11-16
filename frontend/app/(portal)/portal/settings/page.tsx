"use client"

import { useCallback, useEffect, useState } from "react"
import { usePortalStore } from "@/configuration/crypto/stores/usePortalStore"

import { formatMinorUnits } from "@/lib/format"
import type { PortalFeeItem } from "@/configuration/crypto/types/portal-api"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
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
import { Skeleton } from "@/components/ui/skeleton"
import {
  Settings,
  Save,
  Loader2,
  ShieldCheck,
  ExternalLink,
} from "lucide-react"
import { toast } from "sonner"
import Link from "next/link"
import { NetworkPolicySettings } from "@/configuration/crypto/components/portal/network-policy-settings"

import { cryptoApi } from "@/configuration/crypto/lib/api"
const TTL_OPTIONS = [
  { value: "15", label: "15 минут" },
  { value: "30", label: "30 минут" },
  { value: "60", label: "1 час" },
  { value: "120", label: "2 часа" },
  { value: "360", label: "6 часов" },
  { value: "720", label: "12 часов" },
  { value: "1440", label: "24 часа" },
  { value: "10080", label: "Неделя" },
]

const DIRECTION_LABELS: Record<string, string> = {
  processing: "Приём платежей",
  withdrawal: "Вывод средств",
}

export default function SettingsPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  // Form state
  const [defaultTtl, setDefaultTtl] = useState("60")
  const [singlePaymentPerInvoice, setSinglePaymentPerInvoice] = useState(false)
  const [dirty, setDirty] = useState(false)

  // Fee schedule state
  const [fees, setFees] = useState<PortalFeeItem[]>([])
  const [feesLoading, setFeesLoading] = useState(true)

  const fetchSettings = useCallback(async () => {
    if (!activeMerchantId) return
    setLoading(true)
    try {
      const data = await cryptoApi.portal.settings.get(activeMerchantId)
      setDefaultTtl(String(data.defaultTtlMinutes))
      setSinglePaymentPerInvoice(data.singlePaymentPerInvoice)
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
      const data = await cryptoApi.portal.settings.fees(activeMerchantId)
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
  }, [fetchSettings, fetchFees])

  const handleSave = async () => {
    if (!activeMerchantId) return
    setSaving(true)
    try {
      const resp = await cryptoApi.portal.settings.update(activeMerchantId, {
        defaultTtlMinutes: parseInt(defaultTtl),
        singlePaymentPerInvoice,
      })
      setDefaultTtl(String(resp.defaultTtlMinutes))
      setSinglePaymentPerInvoice(resp.singlePaymentPerInvoice)
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
          <Skeleton className="h-48 w-full" />
        </div>
      ) : (
        <div className="space-y-6">
          <div className="grid gap-6 md:grid-cols-2">
            {/* Webhook Settings */}
            <Card>
              <CardHeader>
                <div className="flex items-center gap-2">
                  <ShieldCheck className="size-4 text-muted-foreground" />
                  <CardTitle className="text-base">Вебхуки</CardTitle>
                </div>
                <CardDescription>
                  Endpoint registry, подписки, ротация secret и история доставок
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <p className="text-sm text-muted-foreground">
                  Signing secret показывается только один раз при создании или ротации endpoint. Предыдущий secret принимается ещё 24 часа.
                </p>
                <Button variant="outline" size="sm" asChild>
                  <Link href="/portal/developers/webhooks">
                    Открыть webhook endpoints
                    <ExternalLink className="ml-1.5 size-3.5" />
                  </Link>
                </Button>
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
                <div className="flex items-start justify-between gap-4 rounded-md border p-4">
                  <div className="space-y-1">
                    <Label htmlFor="single-payment-per-invoice">1 инвойс — 1 платёж</Label>
                    <p className="text-xs text-muted-foreground">
                      Первый обнаруженный перевод закрепляется за инвойсом. После его подтверждения
                      недоплата закрывает инвойс, а адрес освобождается. Дополнительные переводы
                      направляются оператору на проверку.
                    </p>
                  </div>
                  <Switch
                    id="single-payment-per-invoice"
                    checked={singlePaymentPerInvoice}
                    onCheckedChange={(checked) => {
                      setSinglePaymentPerInvoice(checked)
                      setDirty(true)
                    }}
                  />
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Fee Schedule */}
          <NetworkPolicySettings merchantId={activeMerchantId} />

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
