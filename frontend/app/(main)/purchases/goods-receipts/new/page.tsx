"use client"

import { useState, useEffect, useMemo } from "react"
import { useRouter } from "next/navigation"
import {
  ArrowUp,
  ArrowDown,
  Plus,
  Copy,
  Trash2,
  Paperclip,
  PanelRightClose,
  PanelRightOpen,
  Info,
} from "lucide-react"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { Checkbox } from "@/components/ui/checkbox"
import { Switch } from "@/components/ui/switch"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { useTabDirty } from "@/hooks/useTabDirty"
import { cn } from "@/lib/utils"
import { api } from "@/lib/api"
import type { GoodsReceiptLineRequest, CreateGoodsReceiptRequest } from "@/types/document"

const SIDEBAR_KEY = "metapus-form-sidebar-collapsed"

// ── Local line state (UI-friendly, converted to DTO on save) ────────────

interface FormLine {
  _key: number // local UI key
  productId: string
  unitId: string
  quantity: string   // display value, will convert to int64
  unitPrice: string  // display value (major units, e.g. "11.00"), convert to MinorUnits
  vatRateId: string
  vatPercent: string
  discountPercent: string
}

function emptyLine(key: number): FormLine {
  return { _key: key, productId: "", unitId: "", quantity: "", unitPrice: "", vatRateId: "", vatPercent: "20", discountPercent: "0" }
}

/** Convert display quantity (e.g. "5") to Quantity int64 (×10000). */
function toQuantity(s: string): number {
  return Math.round(parseFloat(s || "0") * 10000)
}

/** Convert display price (e.g. "11.50") to MinorUnits int64 (kopecks). */
function toMinorUnits(s: string): number {
  return Math.round(parseFloat(s || "0") * 100)
}

/** Format MinorUnits to display. */
function fmtAmount(minor: number): string {
  return (minor / 100).toLocaleString("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

export default function NewGoodsReceiptPage() {
  const router = useRouter()
  const { markDirty, markClean } = useTabDirty()

  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(true)
  const [nextKey, setNextKey] = useState(1)

  // ── Header fields ─────────────────────────────────────────────────────
  const [date, setDate] = useState(() => new Date().toISOString().slice(0, 10))
  const [organizationId, setOrganizationId] = useState("")
  const [supplierId, setSupplierId] = useState("")
  const [warehouseId, setWarehouseId] = useState("")
  const [currencyId, setCurrencyId] = useState("")
  const [contractId, setContractId] = useState("")
  const [supplierDocNumber, setSupplierDocNumber] = useState("")
  const [incomingNumber, setIncomingNumber] = useState("")
  const [amountIncludesVat, setAmountIncludesVat] = useState(true)
  const [description, setDescription] = useState("")

  // ── Lines ─────────────────────────────────────────────────────────────
  const [lines, setLines] = useState<FormLine[]>([])

  useEffect(() => {
    const stored = localStorage.getItem(SIDEBAR_KEY)
    if (stored !== null) setSidebarCollapsed(stored === "true")
  }, [])

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

  // ── Computed totals ───────────────────────────────────────────────────
  const totals = useMemo(() => {
    let totalAmount = 0
    let totalVat = 0
    for (const l of lines) {
      const qty = parseFloat(l.quantity || "0")
      const price = parseFloat(l.unitPrice || "0")
      const lineAmount = qty * price * 100 // in minor units
      const vatPct = parseInt(l.vatPercent || "0")
      const vat = amountIncludesVat
        ? lineAmount - lineAmount / (1 + vatPct / 100)
        : lineAmount * vatPct / 100
      totalAmount += lineAmount
      totalVat += vat
    }
    return { totalAmount: Math.round(totalAmount), totalVat: Math.round(totalVat) }
  }, [lines, amountIncludesVat])

  const buildPayload = (postImmediately: boolean): CreateGoodsReceiptRequest => ({
    date: new Date(date).toISOString(),
    organizationId,
    supplierId,
    warehouseId,
    currencyId: currencyId || undefined,
    contractId: contractId || null,
    supplierDocNumber: supplierDocNumber || undefined,
    incomingNumber: incomingNumber || null,
    amountIncludesVat,
    description: description || undefined,
    postImmediately,
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

  const handleSave = async (postImmediately: boolean, andClose: boolean) => {
    if (!supplierId || !warehouseId || !organizationId) {
      setError("Укажите поставщика, склад и организацию")
      return
    }
    if (lines.length === 0) {
      setError("Добавьте хотя бы одну строку товаров")
      return
    }
    setSaving(true)
    setError(null)
    try {
      const created = await api.goodsReceipts.create(buildPayload(postImmediately))
      markClean()
      if (andClose) {
        router.push("/purchases/goods-receipts")
      } else {
        router.replace(`/purchases/goods-receipts/${created.id}`)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка сохранения")
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title="Приходная накладная (создание)"
        primaryAction={{
          label: saving ? "Сохранение…" : "Провести и закрыть",
          variant: "default",
          onClick: () => handleSave(true, true),
        }}
        secondaryActions={[
          { label: "Записать", onClick: () => handleSave(false, false) },
          { label: "Провести", onClick: () => handleSave(true, false) },
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
                <Label className="text-xs text-muted-foreground">Организация *</Label>
                <Input className="mt-1" placeholder="ID организации" value={organizationId} onChange={(e) => { setOrganizationId(e.target.value); handleChange() }} />
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Поставщик *</Label>
                <Input className="mt-1" placeholder="ID поставщика" value={supplierId} onChange={(e) => { setSupplierId(e.target.value); handleChange() }} />
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Дата *</Label>
                <Input type="date" className="mt-1" value={date} onChange={(e) => { setDate(e.target.value); handleChange() }} />
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Склад *</Label>
                <Input className="mt-1" placeholder="ID склада" value={warehouseId} onChange={(e) => { setWarehouseId(e.target.value); handleChange() }} />
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Договор</Label>
                <Input className="mt-1" placeholder="ID договора" value={contractId} onChange={(e) => { setContractId(e.target.value); handleChange() }} />
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Валюта</Label>
                <Input className="mt-1" placeholder="ID валюты" value={currencyId} onChange={(e) => { setCurrencyId(e.target.value); handleChange() }} />
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

          <div className="flex-1 min-h-0 grid grid-rows-[auto_1fr] border-t">
            <Tabs defaultValue="goods" className="contents">
              <div className="flex flex-col border-b bg-card px-4 row-start-1 col-start-1">
                <TabsList variant="line" className="border-b-0 h-11 self-start">
                  <TabsTrigger value="goods" variant="line" className="text-xs">
                    Товары ({lines.length})
                  </TabsTrigger>
                  <TabsTrigger value="additional" variant="line" className="text-xs">Дополнительно</TabsTrigger>
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
                    <table className="w-full text-sm">
                      <thead>
                        <tr className="border-b bg-muted/70">
                          <th className="w-10 px-2 py-2 text-center text-xs font-medium text-muted-foreground">N</th>
                          <th className="min-w-[160px] px-3 py-2 text-left text-xs font-medium text-muted-foreground">Товар (ID)</th>
                          <th className="w-[120px] px-3 py-2 text-left text-xs font-medium text-muted-foreground">Ед. изм. (ID)</th>
                          <th className="w-24 px-3 py-2 text-right text-xs font-medium text-muted-foreground">Кол-во</th>
                          <th className="w-24 px-3 py-2 text-right text-xs font-medium text-muted-foreground">Цена</th>
                          <th className="w-[100px] px-3 py-2 text-left text-xs font-medium text-muted-foreground">Ставка НДС (ID)</th>
                          <th className="w-16 px-3 py-2 text-right text-xs font-medium text-muted-foreground">% НДС</th>
                          <th className="w-8" />
                        </tr>
                      </thead>
                      <tbody>
                        {lines.length === 0 && (
                          <tr>
                            <td colSpan={8} className="px-4 py-8 text-center text-sm text-muted-foreground">
                              Нажмите &quot;Добавить&quot; для добавления строки
                            </td>
                          </tr>
                        )}
                        {lines.map((line, idx) => (
                          <tr key={line._key} className="border-b hover:bg-muted/30 transition-colors">
                            <td className="px-2 py-1.5 text-center text-xs text-muted-foreground">{idx + 1}</td>
                            <td className="px-1 py-1"><Input className="h-7 text-xs" placeholder="ID номенклатуры" value={line.productId} onChange={(e) => updateLine(line._key, "productId", e.target.value)} /></td>
                            <td className="px-1 py-1"><Input className="h-7 text-xs" placeholder="ID ед. изм." value={line.unitId} onChange={(e) => updateLine(line._key, "unitId", e.target.value)} /></td>
                            <td className="px-1 py-1"><Input className="h-7 text-right font-mono text-xs" type="number" step="0.001" value={line.quantity} onChange={(e) => updateLine(line._key, "quantity", e.target.value)} /></td>
                            <td className="px-1 py-1"><Input className="h-7 text-right font-mono text-xs" type="number" step="0.01" value={line.unitPrice} onChange={(e) => updateLine(line._key, "unitPrice", e.target.value)} /></td>
                            <td className="px-1 py-1"><Input className="h-7 text-xs" placeholder="ID ставки" value={line.vatRateId} onChange={(e) => updateLine(line._key, "vatRateId", e.target.value)} /></td>
                            <td className="px-1 py-1"><Input className="h-7 text-right font-mono text-xs" type="number" value={line.vatPercent} onChange={(e) => updateLine(line._key, "vatPercent", e.target.value)} /></td>
                            <td className="px-1 py-1">
                              <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => removeLine(line._key)}>
                                <Trash2 className="h-3 w-3 text-destructive" />
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
                  <Textarea rows={4} className="mt-1" value={description} onChange={(e) => { setDescription(e.target.value); handleChange() }} placeholder="Комментарий к документу..." />
                </div>
              </TabsContent>
            </Tabs>
          </div>

          {/* Footer with totals */}
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
              <div><span className="block text-muted-foreground/70 mb-0.5">Изменено:</span><span className="text-foreground/80">—</span></div>
              <div><span className="block text-muted-foreground/70 mb-0.5">Создано:</span><span className="text-foreground/80">—</span></div>
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
