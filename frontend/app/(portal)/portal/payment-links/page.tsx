"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { usePortalStore } from "@/stores/usePortalStore"
import { api } from "@/lib/api"
import { getNetworkColor } from "@/lib/blockchain"
import {
  derivePortalNetworks,
  filterInvoiceTokensByNetwork,
  resolveInvoiceNetworkId,
  resolveInvoiceTokenId,
  type MerchantTokenSelection,
} from "@/lib/portal-invoice-state"
import { cn } from "@/lib/utils"
import type {
  PortalCurrencyItem,
  PortalPaymentLinkItem,
  CreatePaymentLinkRequest,
  PortalPaymentLinkCreateResponse,
} from "@/types/portal-api"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Skeleton } from "@/components/ui/skeleton"
import { Plus, Copy, Check, Link2, ExternalLink, QrCode } from "lucide-react"
import { toast } from "sonner"

const TTL_OPTIONS = [
  { value: "default", label: "По умолчанию (настройка мерчанта)" },
  { value: "15", label: "15 минут" },
  { value: "30", label: "30 минут" },
  { value: "60", label: "1 час" },
  { value: "360", label: "6 часов" },
  { value: "1440", label: "24 часа" },
  { value: "10080", label: "Неделя" },
]

export default function PaymentLinksPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  const [links, setLinks] = useState<PortalPaymentLinkItem[]>([])
  const [tokens, setTokens] = useState<PortalCurrencyItem[]>([])
  const [loading, setLoading] = useState(true)
  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [copiedId, setCopiedId] = useState<string | null>(null)

  const networks = useMemo(() => derivePortalNetworks(tokens), [tokens])
  const [tokenSelection, setTokenSelection] = useState<MerchantTokenSelection | null>(null)
  const selectedNetworkId = resolveInvoiceNetworkId(networks, activeMerchantId, tokenSelection)
  const networkTokens = useMemo(
    () => filterInvoiceTokensByNetwork(tokens, selectedNetworkId),
    [tokens, selectedNetworkId]
  )
  const selectedTokenId = resolveInvoiceTokenId(tokens, activeMerchantId, tokenSelection)
  const activeToken = networkTokens.find((t) => t.tokenId === selectedTokenId)

  // Create form state
  const [form, setForm] = useState<CreatePaymentLinkRequest>({
    tokenId: "",
    amount: "",
    description: "",
    reusable: false,
    maxUses: 0,
    ttlMinutes: undefined,
  })

  // Result after creation
  const [createdLink, setCreatedLink] = useState<PortalPaymentLinkCreateResponse | null>(null)

  const fetchLinks = useCallback(async () => {
    if (!activeMerchantId) return
    setLoading(true)
    try {
      const [linksResp, currenciesResp] = await Promise.all([
        api.portal.paymentLinks.list({ merchantId: activeMerchantId }),
        api.portal.currencies(activeMerchantId),
      ])
      setLinks(linksResp.items)
      setTokens(currenciesResp.items)
    } catch {
      toast.error("Не удалось загрузить ссылки")
    } finally {
      setLoading(false)
    }
  }, [activeMerchantId])

  useEffect(() => { fetchLinks() }, [fetchLinks])

  const handleCreate = async () => {
    if (!activeMerchantId || !selectedTokenId || !form.amount) return
    setCreating(true)
    try {
      const resp = await api.portal.paymentLinks.create(activeMerchantId, {
        ...form,
        tokenId: selectedTokenId,
      })
      setCreatedLink(resp)
      setCreateOpen(false)
      setTokenSelection(null)
      setForm({ tokenId: "", amount: "", description: "", reusable: false, maxUses: 0, ttlMinutes: undefined })
      await fetchLinks()
      toast.success("Платёжная ссылка создана")
    } catch {
      toast.error("Ошибка при создании ссылки")
    } finally {
      setCreating(false)
    }
  }

  const handleCopy = async (url: string, id: string) => {
    await navigator.clipboard.writeText(url)
    setCopiedId(id)
    setTimeout(() => setCopiedId(null), 2000)
  }

  if (!activeMerchantId) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
        <Link2 className="size-10 mb-3 opacity-40" />
        <p>Выберите мерчанта для управления платёжными ссылками</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Платёжные ссылки</h1>
          <p className="text-sm text-muted-foreground">
            Создавайте ссылки для приёма оплаты без API интеграции
          </p>
        </div>
        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogTrigger asChild>
            <Button size="sm">
              <Plus className="size-4 mr-1.5" />
              Создать ссылку
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Новая платёжная ссылка</DialogTitle>
              <DialogDescription>
                Ссылка создаёт инвойс автоматически при открытии
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-2">
              <div className="space-y-2">
                <Label>Сеть *</Label>
                <Select
                  value={selectedNetworkId}
                  onValueChange={(networkId) => {
                    setTokenSelection({
                      merchantId: activeMerchantId,
                      networkId,
                      tokenId: "",
                    })
                    setForm((prev) => ({ ...prev, tokenId: "" }))
                  }}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Выберите блокчейн-сеть" />
                  </SelectTrigger>
                  <SelectContent>
                    {networks.map((net) => (
                      <SelectItem key={net.networkId} value={net.networkId}>
                        <span className="flex items-center gap-2">
                          <span
                            className={cn(
                              "size-2 rounded-full shrink-0",
                              getNetworkColor(net.network)
                            )}
                          />
                          <span>{net.network}</span>
                          <span className="text-xs text-muted-foreground">
                            ({net.symbols.join(", ")})
                          </span>
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>Токен *</Label>
                <Select
                  value={selectedTokenId}
                  disabled={!selectedNetworkId}
                  onValueChange={(tokenId) => {
                    setTokenSelection({
                      merchantId: activeMerchantId,
                      networkId: selectedNetworkId,
                      tokenId,
                    })
                    setForm((prev) => ({ ...prev, tokenId }))
                  }}
                >
                  <SelectTrigger disabled={!selectedNetworkId}>
                    <SelectValue
                      placeholder={
                        selectedNetworkId ? "Выберите токен" : "Сначала выберите сеть"
                      }
                    />
                  </SelectTrigger>
                  <SelectContent>
                    {networkTokens.map((token) => (
                      <SelectItem key={token.tokenId} value={token.tokenId}>
                        {token.symbol}
                        <span className="ml-1.5 text-xs text-muted-foreground">
                          {token.network}
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>Сумма (minor units)</Label>
                <Input
                  placeholder="10000000"
                  value={form.amount}
                  onChange={(e) => setForm({ ...form, amount: e.target.value.replace(/\D/g, "") })}
                />
                <p className="text-[11px] text-muted-foreground">
                  {activeToken
                    ? `Точность ${activeToken.symbol}: ${activeToken.decimalPlaces} знаков после запятой`
                    : "Для USDT: 10000000 = 10.00 USDT (6 десятичных знаков)"}
                </p>
              </div>
              <div className="space-y-2">
                <Label>Описание</Label>
                <Textarea
                  placeholder="Оплата заказа..."
                  value={form.description}
                  onChange={(e) => setForm({ ...form, description: e.target.value })}
                  rows={2}
                />
              </div>
              <div className="space-y-2">
                <Label>TTL инвойса</Label>
                <Select
                  value={form.ttlMinutes ? String(form.ttlMinutes) : "default"}
                  onValueChange={(v) => setForm({ ...form, ttlMinutes: v === "default" ? undefined : parseInt(v) })}
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
              </div>
              <div className="flex items-center justify-between rounded-lg border p-3">
                <div>
                  <Label className="text-sm">Многоразовая</Label>
                  <p className="text-[11px] text-muted-foreground">
                    Одна ссылка — много инвойсов
                  </p>
                </div>
                <Switch
                  checked={form.reusable}
                  onCheckedChange={(checked) => setForm({ ...form, reusable: checked, maxUses: checked ? 0 : 1 })}
                />
              </div>
              {form.reusable && (
                <div className="space-y-2">
                  <Label>Лимит использований (0 = безлимит)</Label>
                  <Input
                    type="number"
                    min={0}
                    value={form.maxUses || ""}
                    onChange={(e) => setForm({ ...form, maxUses: parseInt(e.target.value) || 0 })}
                  />
                </div>
              )}
            </div>
            <DialogFooter>
              <Button
                onClick={handleCreate}
                disabled={creating || !form.tokenId || !form.amount}
              >
                {creating ? "Создание..." : "Создать"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {/* Created link result */}
      {createdLink && (
        <Card className="border-emerald-200 dark:border-emerald-800 bg-emerald-50/50 dark:bg-emerald-950/20">
          <CardContent className="pt-4">
            <div className="flex items-center gap-3">
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium text-emerald-700 dark:text-emerald-400 mb-1">
                  ✅ Ссылка создана
                </p>
                <code className="text-xs bg-background rounded px-2 py-1 break-all block">
                  {createdLink.payUrl}
                </code>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleCopy(createdLink.payUrl, "created")}
              >
                {copiedId === "created" ? <Check className="size-4" /> : <Copy className="size-4" />}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setCreatedLink(null)}
              >
                Закрыть
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Ваши ссылки</CardTitle>
          <CardDescription>
            Каждая ссылка при открытии создаёт новый инвойс с заданными параметрами
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="space-y-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : links.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              <Link2 className="size-8 mx-auto mb-2 opacity-30" />
              <p className="text-sm">Нет созданных ссылок</p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Описание</TableHead>
                  <TableHead>Сумма</TableHead>
                  <TableHead>Тип</TableHead>
                  <TableHead>Использований</TableHead>
                  <TableHead>Статус</TableHead>
                  <TableHead>TTL</TableHead>
                  <TableHead className="w-[80px]" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {links.map((link) => (
                  <TableRow key={link.id}>
                    <TableCell>
                      <div>
                        <p className="font-medium text-sm">
                          {link.description || "Без описания"}
                        </p>
                        <code className="text-[10px] text-muted-foreground">
                          {link.publicRef}
                        </code>
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-sm">
                      {link.amount} <span className="text-muted-foreground text-xs">{link.symbol}</span>
                    </TableCell>
                    <TableCell>
                      <Badge variant={link.reusable ? "default" : "secondary"} className="text-[10px]">
                        {link.reusable ? "Многоразовая" : "Одноразовая"}
                      </Badge>
                    </TableCell>
                    <TableCell className="tabular-nums text-sm">
                      {link.currentUses}
                      {link.reusable && link.maxUses > 0 ? ` / ${link.maxUses}` : ""}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={link.status === "active" ? "default" : "secondary"}
                        className="text-[10px]"
                      >
                        {link.status === "active" ? "Активна" : "Отключена"}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {link.ttlMinutes}m
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-7"
                          onClick={() => handleCopy(link.payUrl, link.id)}
                          title="Копировать ссылку"
                        >
                          {copiedId === link.id ? (
                            <Check className="size-3.5 text-emerald-500" />
                          ) : (
                            <Copy className="size-3.5" />
                          )}
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-7"
                          asChild
                          title="Открыть ссылку"
                        >
                          <a href={link.payUrl} target="_blank" rel="noopener noreferrer">
                            <ExternalLink className="size-3.5" />
                          </a>
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
