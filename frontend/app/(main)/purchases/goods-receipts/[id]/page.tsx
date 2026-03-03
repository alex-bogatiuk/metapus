"use client"

import { useState, useEffect, useMemo } from "react"
import { useRouter, useParams } from "next/navigation"
import {
  Plus,
  Trash2,
  Paperclip,
  PanelRightClose,
  PanelRightOpen,
  Info,
  Loader2,
} from "lucide-react"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { ReferenceField } from "@/components/shared/reference-field"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { DatePicker } from "@/components/ui/date-picker"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useTabTitle } from "@/hooks/useTabTitle"
import { cn } from "@/lib/utils"
import { api } from "@/lib/api"
import type { GoodsReceiptResponse, GoodsReceiptLineRequest, UpdateGoodsReceiptRequest } from "@/types/document"

const SIDEBAR_KEY = "metapus-form-sidebar-collapsed"

// ── Local line state ────────────────────────────────────────────────────

interface FormLine {
  _key: number
  productId: string
  unitId: string
  quantity: string
  unitPrice: string
  vatRateId: string
  vatPercent: string
  discountPercent: string
  // read-only display from response
  amount?: number
  vatAmount?: number
}

function emptyLine(key: number): FormLine {
  return { _key: key, productId: "", unitId: "", quantity: "", unitPrice: "", vatRateId: "", vatPercent: "20", discountPercent: "0" }
}

function toQuantity(s: string): number {
  return Math.round(parseFloat(s || "0") * 10000)
}

function toMinorUnits(s: string): number {
  return Math.round(parseFloat(s || "0") * 100)
}

function fmtAmount(minor: number): string {
  return (minor / 100).toLocaleString("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function fmtDate(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit", year: "numeric" })
}

function fromQuantity(q: number): string {
  return (q / 10000).toString()
}

function fromMinorUnits(m: number): string {
  return (m / 100).toFixed(2)
}

export default function GoodsReceiptFormPage() {
  const router = useRouter()
  const params = useParams<{ id: string }>()
  const { markDirty, markClean } = useTabDirty()

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [doc, setDoc] = useState<GoodsReceiptResponse | null>(null)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(true)
  const [nextKey, setNextKey] = useState(1)

  // ── Header fields ─────────────────────────────────────────────────────
  const [date, setDate] = useState<Date | undefined>(undefined)
  const [organizationId, setOrganizationId] = useState("")
  const [organizationName, setOrganizationName] = useState("")
  const [supplierId, setSupplierId] = useState("")
  const [supplierName, setSupplierName] = useState("")
  const [warehouseId, setWarehouseId] = useState("")
  const [warehouseName, setWarehouseName] = useState("")
  const [currencyId, setCurrencyId] = useState("")
  const [currencyName, setCurrencyName] = useState("")
  const [contractId, setContractId] = useState("")
  const [contractName, setContractName] = useState("")
  const [supplierDocNumber, setSupplierDocNumber] = useState("")
  const [incomingNumber, setIncomingNumber] = useState("")
  const [amountIncludesVat, setAmountIncludesVat] = useState(true)
  const [description, setDescription] = useState("")
  const [lines, setLines] = useState<FormLine[]>([])
  useTabTitle(doc?.number || undefined, "Приходная накладная")

  useEffect(() => {
    const stored = localStorage.getItem(SIDEBAR_KEY)
    if (stored !== null) setSidebarCollapsed(stored === "true")
  }, [])

  // ── Fetch document ────────────────────────────────────────────────────
  useEffect(() => {
    if (!params.id) return
    setLoading(true)
    api.goodsReceipts.get(params.id).then((d) => {
      setDoc(d)
      setDate(d.date ? new Date(d.date) : undefined)
      setOrganizationId(d.organizationId)
      setOrganizationName(d.organization?.name || "")
      setSupplierId(d.supplierId)
      setSupplierName(d.supplier?.name || "")
      setWarehouseId(d.warehouseId)
      setWarehouseName(d.warehouse?.name || "")
      setCurrencyId(d.currencyId || "")
      setCurrencyName(d.currency?.name || "")
      setContractId(d.contractId || "")
      setContractName(d.contract?.name || "")
      setSupplierDocNumber(d.supplierDocNumber || "")
      setIncomingNumber(d.incomingNumber || "")
      setAmountIncludesVat(d.amountIncludesVat)
      setDescription(d.description || "")
      const mapped = (d.lines ?? []).map((l, i) => ({
        _key: i + 1,
        productId: l.productId,
        unitId: l.unitId,
        quantity: fromQuantity(l.quantity),
        unitPrice: fromMinorUnits(l.unitPrice),
        vatRateId: l.vatRateId,
        vatPercent: "0", // vatPercent not in response, default
        discountPercent: l.discountPercent || "0",
        amount: l.amount,
        vatAmount: l.vatAmount,
      }))
      setLines(mapped)
      setNextKey(mapped.length + 1)
    }).catch((err) => {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    }).finally(() => {
      setLoading(false)
    })
  }, [params.id])

  const toggleSidebar = () => {
    const next = !sidebarCollapsed
    setSidebarCollapsed(next)
    localStorage.setItem(SIDEBAR_KEY, String(next))
  }

  const handleChange = () => markDirty()

  const addLine = () => {
    setLines([...lines, emptyLine(nextKey)])
    setNextKey(nextKey + 1)
    markDirty()
  }

  const removeLine = (key: number) => {
    setLines(lines.filter((l) => l._key !== key))
    markDirty()
  }

  const updateLine = (key: number, field: keyof FormLine, value: string) => {
    setLines(lines.map((l) => l._key === key ? { ...l, [field]: value } : l))
    markDirty()
  }

  const totals = useMemo(() => {
    if (doc && !lines.some((l) => l.amount === undefined)) {
      return { totalAmount: doc.totalAmount, totalVat: doc.totalVat }
    }
    let totalAmount = 0
    let totalVat = 0
    for (const l of lines) {
      const qty = parseFloat(l.quantity || "0")
      const price = parseFloat(l.unitPrice || "0")
      const lineAmount = qty * price * 100
      const vatPct = parseInt(l.vatPercent || "0")
      const vat = amountIncludesVat
        ? lineAmount - lineAmount / (1 + vatPct / 100)
        : lineAmount * vatPct / 100
      totalAmount += lineAmount
      totalVat += vat
    }
    return { totalAmount: Math.round(totalAmount), totalVat: Math.round(totalVat) }
  }, [lines, amountIncludesVat, doc])

  const buildUpdatePayload = (): UpdateGoodsReceiptRequest => ({
    date: date ? date.toISOString() : new Date().toISOString(),
    organizationId,
    supplierId,
    warehouseId,
    currencyId: currencyId || null,
    contractId: contractId || null,
    supplierDocNumber: supplierDocNumber || null,
    incomingNumber: incomingNumber || null,
    amountIncludesVat,
    description: description || null,
    lines: lines.map((l): GoodsReceiptLineRequest => ({
      productId: l.productId,
      unitId: l.unitId,
      quantity: toQuantity(l.quantity),
      unitPrice: toMinorUnits(l.unitPrice),
      vatRateId: l.vatRateId,
      vatPercent: parseInt(l.vatPercent || "0"),
      discountPercent: l.discountPercent || "0",
    })),
  })

  const handleSave = async (andClose: boolean) => {
    setSaving(true)
    setError(null)
    try {
      const updated = await api.goodsReceipts.update(params.id, buildUpdatePayload())
      setDoc(updated)
      markClean()
      if (andClose) router.push("/purchases/goods-receipts")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка сохранения")
    } finally {
      setSaving(false)
    }
  }

  const handlePost = async () => {
    setSaving(true)
    setError(null)
    try {
      await api.goodsReceipts.post(params.id)
      const updated = await api.goodsReceipts.get(params.id)
      setDoc(updated)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка проведения")
    } finally {
      setSaving(false)
    }
  }

  const handleUnpost = async () => {
    setSaving(true)
    setError(null)
    try {
      await api.goodsReceipts.unpost(params.id)
      const updated = await api.goodsReceipts.get(params.id)
      setDoc(updated)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка отмены проведения")
    } finally {
      setSaving(false)
    }
  }

  const handlePostAndClose = async () => {
    setSaving(true)
    setError(null)
    try {
      await api.goodsReceipts.update(params.id, buildUpdatePayload())
      await api.goodsReceipts.post(params.id)
      markClean()
      router.push("/purchases/goods-receipts")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка")
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center text-muted-foreground">
        <Loader2 className="mr-2 h-5 w-5 animate-spin" />
        Загрузка документа…
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title={`Приходная накладная ${doc?.number || ""}`}
        status={
          doc?.posted
            ? { label: "Проведён", variant: "success" as const }
            : doc?.deletionMark
              ? { label: "Помечен на удаление", variant: "destructive" as const }
              : { label: "Черновик", variant: "outline" as const }
        }
        primaryAction={{
          label: saving ? "Сохранение…" : "Провести и закрыть",
          variant: "default",
          onClick: handlePostAndClose,
        }}
        secondaryActions={[
          { label: "Записать", onClick: () => handleSave(false) },
          ...(doc?.posted
            ? [{ label: "Отменить проведение", onClick: handleUnpost }]
            : [{ label: "Провести", onClick: handlePost }]),
        ]}
        backHref="/purchases/goods-receipts"
        onClose={() => router.push("/purchases/goods-receipts")}
      />

      {error && (
        <div className="bg-destructive/10 border-b border-destructive/20 px-4 py-2 text-sm text-destructive">
          {error}
        </div>
      )}

      <div className="flex flex-1 overflow-hidden">
        <div className="flex flex-1 flex-col overflow-hidden relative">
          <div className="border-b bg-card p-4 shrink-0">
            <div className="grid grid-cols-1 gap-x-6 gap-y-3 md:grid-cols-2 lg:grid-cols-3">
              <div>
                <Label className="text-xs text-muted-foreground">Организация</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={organizationId}
                    displayName={organizationName}
                    apiEndpoint="/catalog/organizations"
                    placeholder="Выберите организацию"
                    onChange={(id, name) => { setOrganizationId(id); setOrganizationName(name); handleChange() }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Поставщик</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={supplierId}
                    displayName={supplierName}
                    apiEndpoint="/catalog/counterparties"
                    placeholder="Выберите поставщика"
                    onChange={(id, name) => { setSupplierId(id); setSupplierName(name); handleChange() }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Номер</Label>
                <div className="mt-1 flex items-center gap-2">
                  <Input value={doc?.number || ""} className="flex-1 bg-muted/30" readOnly />
                  <Label className="shrink-0 text-xs text-muted-foreground">от:</Label>
                  <DatePicker
                    value={date}
                    onChange={(d) => { setDate(d); handleChange() }}
                    className="w-44 h-9"
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Склад</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={warehouseId}
                    displayName={warehouseName}
                    apiEndpoint="/catalog/warehouses"
                    placeholder="Выберите склад"
                    onChange={(id, name) => { setWarehouseId(id); setWarehouseName(name); handleChange() }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Договор</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={contractId}
                    displayName={contractName}
                    apiEndpoint="/catalog/contracts"
                    placeholder="Выберите договор"
                    onChange={(id, name) => { setContractId(id); setContractName(name); handleChange() }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Валюта</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={currencyId}
                    displayName={currencyName}
                    apiEndpoint="/catalog/currencies"
                    placeholder="Выберите валюту"
                    onChange={(id, name) => { setCurrencyId(id); setCurrencyName(name); handleChange() }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">№ вх. документа</Label>
                <Input className="mt-1" value={supplierDocNumber} onChange={(e) => { setSupplierDocNumber(e.target.value); handleChange() }} />
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Вх. номер</Label>
                <Input className="mt-1" value={incomingNumber} onChange={(e) => { setIncomingNumber(e.target.value); handleChange() }} />
              </div>
              <div className="flex items-center gap-3 mt-5">
                <Switch checked={amountIncludesVat} onCheckedChange={(v) => { setAmountIncludesVat(v); handleChange() }} />
                <Label className="text-xs">НДС включён в сумму</Label>
              </div>
            </div>
          </div>

          <div className="flex-1 min-h-0 grid grid-rows-[auto_1fr]">
            <Tabs defaultValue="goods" className="contents">
              <div className="flex flex-col border-b bg-card px-4 row-start-1 col-start-1">
                <TabsList variant="line" className="border-b-0 h-11 self-start">
                  <TabsTrigger value="goods" variant="line" className="text-xs">
                    Товары ({lines.length})
                  </TabsTrigger>
                  <TabsTrigger value="additional" variant="line" className="text-xs">
                    Дополнительно
                  </TabsTrigger>
                </TabsList>
              </div>

              <TabsContent value="goods" className="mt-0 overflow-hidden row-start-2 col-start-1">
                <div className="h-full flex flex-col">
                  <div className="flex items-center gap-1 p-2 bg-card/50 border-b shrink-0">
                    <Button variant="outline" size="sm" onClick={addLine}>
                      <Plus className="mr-1 h-3 w-3" />
                      Добавить
                    </Button>
                  </div>
                  <div className="flex-1 overflow-auto">
                    <table className="w-full text-sm border-separate border-spacing-0">
                      <thead className="sticky top-0 z-10 bg-muted/90 backdrop-blur-sm">
                        <tr>
                          <th className="w-10 border-b px-2 py-2 text-center text-[11px] font-semibold text-muted-foreground">N</th>
                          <th className="min-w-[160px] border-b px-3 py-2 text-left text-[11px] font-semibold text-muted-foreground">Товар</th>
                          <th className="w-[140px] border-b px-3 py-2 text-left text-[11px] font-semibold text-muted-foreground">Ед. изм.</th>
                          <th className="w-24 border-b px-3 py-2 text-right text-[11px] font-semibold text-muted-foreground">Кол-во</th>
                          <th className="w-24 border-b px-3 py-2 text-right text-[11px] font-semibold text-muted-foreground">Цена</th>
                          <th className="w-24 border-b px-3 py-2 text-right text-[11px] font-semibold text-muted-foreground">Сумма</th>
                          <th className="w-24 border-b px-3 py-2 text-right text-[11px] font-semibold text-muted-foreground">НДС</th>
                          <th className="w-[100px] border-b px-3 py-2 text-left text-[11px] font-semibold text-muted-foreground">Ставка НДС</th>
                          <th className="w-10 border-b" />
                        </tr>
                      </thead>
                      <tbody className="divide-y">
                        {lines.length === 0 && (
                          <tr>
                            <td colSpan={9} className="px-4 py-8 text-center text-sm text-muted-foreground">
                              Нажмите &quot;Добавить&quot; для добавления строки
                            </td>
                          </tr>
                        )}
                        {lines.map((line, idx) => (
                          <tr key={line._key} className="group hover:bg-primary/5 transition-colors">
                            <td className="px-2 py-1.5 text-center text-xs text-muted-foreground">{idx + 1}</td>
                            <td className="px-1 py-1">
                              <ReferenceField
                                compact
                                value={line.productId}
                                displayName={(doc?.lines ?? []).find(l => l.productId === line.productId)?.product?.name}
                                apiEndpoint="/catalog/nomenclature"
                                placeholder="Номенклатура"
                                onChange={(id) => updateLine(line._key, "productId", id)}
                              />
                            </td>
                            <td className="px-1 py-1">
                              <ReferenceField
                                compact
                                value={line.unitId}
                                displayName={(doc?.lines ?? []).find(l => l.unitId === line.unitId)?.unit?.name}
                                apiEndpoint="/catalog/units"
                                placeholder="Ед. изм."
                                onChange={(id) => updateLine(line._key, "unitId", id)}
                              />
                            </td>
                            <td className="px-1 py-1"><Input className="h-7 text-right font-mono text-xs" type="number" step="0.001" value={line.quantity} onChange={(e) => updateLine(line._key, "quantity", e.target.value)} /></td>
                            <td className="px-1 py-1"><Input className="h-7 text-right font-mono text-xs" type="number" step="0.01" value={line.unitPrice} onChange={(e) => updateLine(line._key, "unitPrice", e.target.value)} /></td>
                            <td className="px-1 py-1 text-right font-mono text-xs text-muted-foreground">{line.amount !== undefined ? fmtAmount(line.amount) : "—"}</td>
                            <td className="px-1 py-1 text-right font-mono text-xs text-muted-foreground">{line.vatAmount !== undefined ? fmtAmount(line.vatAmount) : "—"}</td>
                            <td className="px-1 py-1">
                              <ReferenceField
                                compact
                                value={line.vatRateId}
                                displayName={(doc?.lines ?? []).find(l => l.vatRateId === line.vatRateId)?.vatRate?.name}
                                apiEndpoint="/catalog/vat-rates"
                                placeholder="Ставка НДС"
                                onChange={(id) => updateLine(line._key, "vatRateId", id)}
                              />
                            </td>
                            <td className="px-1 py-1">
                              <Button variant="ghost" size="icon" className="h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity" onClick={() => removeLine(line._key)}>
                                <Trash2 className="h-4 w-4 text-destructive/70" />
                              </Button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              </TabsContent>

              <TabsContent value="additional" className="mt-0 p-4 overflow-auto row-start-2 col-start-1">
                <div>
                  <Label className="text-xs text-muted-foreground">Описание</Label>
                  <Textarea rows={4} className="mt-1" value={description} onChange={(e) => { setDescription(e.target.value); handleChange() }} />
                </div>
              </TabsContent>
            </Tabs>
          </div>

          {/* Footer */}
          <div className="shrink-0 border-t bg-background px-4 py-2 shadow-[0_-4px_8px_-2px_rgba(0,0,0,0.05)] z-20 relative">
            <div className="flex items-center gap-6 justify-end text-xs">
              <div className="flex items-center gap-1.5">
                <span className="text-muted-foreground">НДС:</span>
                <span className="font-mono text-[11px] font-medium">{fmtAmount(totals.totalVat)} Р</span>
              </div>
              <div className="flex items-center gap-1.5">
                <span className="text-sm font-semibold">ИТОГО:</span>
                <span className="text-xl font-bold tracking-tight">{fmtAmount(totals.totalAmount)}</span>
                <span className="text-sm font-semibold text-muted-foreground">Р</span>
              </div>
            </div>
          </div>
        </div>

        {/* Right Sidebar — collapsible */}
        <div
          className={cn(
            "flex flex-col shrink-0 border-l border-border bg-card/30 transition-all duration-300 ease-in-out overflow-hidden",
            sidebarCollapsed ? "w-9" : "w-72"
          )}
        >
          <div
            className={cn(
              "flex items-center justify-center border-b shrink-0 bg-muted/20 transition-all duration-300",
              !sidebarCollapsed ? "h-0 opacity-0 pointer-events-none border-b-0" : "h-11 opacity-100"
            )}
          >
            <TooltipProvider delayDuration={300}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-foreground hover:bg-transparent" onClick={toggleSidebar}>
                    <Info className="h-4 w-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="left">Развернуть панель</TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>

          <div className={cn("flex-1 overflow-y-auto transition-opacity duration-200", sidebarCollapsed ? "opacity-0 pointer-events-none" : "opacity-100")}>
            <div className="p-4 space-y-6">
              <div>
                <div className="flex items-center justify-between text-muted-foreground mb-3">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    <Paperclip className="h-4 w-4" />
                    Файлы
                  </div>
                  <Button variant="ghost" size="icon" className="h-6 w-6"><Plus className="h-4 w-4" /></Button>
                </div>
                <div className="text-xs text-muted-foreground/60 text-center py-4">Нет прикрепленных файлов</div>
              </div>
            </div>

            <div className="p-4 border-t border-border/50 text-xs text-muted-foreground space-y-2">
              <div>
                <span className="block text-muted-foreground/70 mb-0.5">Изменено:</span>
                <span className="text-foreground/80">{doc?.updatedAt ? fmtDate(doc.updatedAt) : "—"}</span>
              </div>
              <div>
                <span className="block text-muted-foreground/70 mb-0.5">Создано:</span>
                <span className="text-foreground/80">{doc?.createdAt ? fmtDate(doc.createdAt) : "—"}</span>
              </div>
            </div>
          </div>

          <div className={cn("flex items-center border-t h-9 mt-auto shrink-0", sidebarCollapsed ? "justify-center" : "justify-end px-2")}>
            <TooltipProvider delayDuration={300}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" onClick={toggleSidebar}>
                    {sidebarCollapsed ? <PanelRightOpen className="h-4 w-4" /> : <PanelRightClose className="h-4 w-4" />}
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="left">{sidebarCollapsed ? "Показать панель" : "Скрыть панель"}</TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
        </div>
      </div>
    </div>
  )
}
