"use client"

import { useState, useEffect, useMemo } from "react"
import { useCollapsible } from "@/hooks/useCollapsible"
import { useRouter, useSearchParams, usePathname } from "next/navigation"
import {
  ArrowUp,
  ArrowDown,
  Plus,
  Copy,
  Trash2,
  Loader2,
  ChevronsUp,
  ChevronsDown,
} from "lucide-react"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { FormSidebar } from "@/components/shared/form-sidebar"
import { ReferenceField } from "@/components/shared/reference-field"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { Checkbox } from "@/components/ui/checkbox"
import { Switch } from "@/components/ui/switch"
import { DatePicker } from "@/components/ui/date-picker"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useFormDraft } from "@/hooks/useFormDraft"
import { cn } from "@/lib/utils"
import { api } from "@/lib/api"
import { fromQuantity, fromMinorUnits, toQuantity, toMinorUnits, moneyStep } from "@/lib/format"
import { useCurrencyScale } from "@/hooks/useCurrencyScale"
import { type FormLine, emptyLine, fetchVatRatePercent, computeTotals } from "@/lib/document-form"
import { DocumentTotalsFooter } from "@/components/shared/document-totals-footer"
import type { GoodsReceiptLineRequest, CreateGoodsReceiptRequest, GoodsReceiptResponse } from "@/types/document"

const SIDEBAR_STORAGE_KEY = "metapus-form-sidebar-collapsed"
const HEADER_STORAGE_KEY = "metapus-form-header-collapsed"

// ── Form state (single typed object for draft persistence) ────────────

interface GoodsReceiptFormState {
  date: string | undefined
  organizationId: string
  organizationName: string
  supplierId: string
  supplierName: string
  warehouseId: string
  warehouseName: string
  currencyId: string
  currencyName: string
  contractId: string
  contractName: string
  supplierDocNumber: string
  incomingNumber: string
  amountIncludesVat: boolean
  description: string
  lines: FormLine[]
  nextKey: number
}

const INITIAL_FORM_STATE: GoodsReceiptFormState = {
  date: new Date().toISOString(),
  organizationId: "", organizationName: "",
  supplierId: "", supplierName: "",
  warehouseId: "", warehouseName: "",
  currencyId: "", currencyName: "",
  contractId: "", contractName: "",
  supplierDocNumber: "", incomingNumber: "",
  amountIncludesVat: true, description: "",
  lines: [], nextKey: 1,
}

export default function NewGoodsReceiptPage() {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()
  const { markDirty, markClean } = useTabDirty()

  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [sidebarCollapsed, toggleSidebar] = useCollapsible(SIDEBAR_STORAGE_KEY, true)
  const [headerCollapsed, toggleHeader] = useCollapsible(HEADER_STORAGE_KEY, false)
  const [copyLoading, setCopyLoading] = useState(false)

  // ── Single typed form state with automatic draft persistence ────────
  const { state: f, update, replace, clear, hasDraft } = useFormDraft<GoodsReceiptFormState>(
    pathname,
    INITIAL_FORM_STATE,
    { shouldPersist: (s) => !!(s.organizationId || s.supplierId || s.warehouseId || s.lines.length > 0) },
  )

  // Convenience: Date object from ISO string
  const date = f.date ? new Date(f.date) : undefined

  // Dynamic currency scale for monetary fields
  const { decimalPlaces, symbol: currencySymbol } = useCurrencyScale(f.currencyId || undefined)

  // ── Copy from existing document (аналог 1С ОбработкаЗаполнения) ─────
  // Skipped if draft was restored (user was editing, switched tab, came back)
  useEffect(() => {
    if (hasDraft) { markDirty(); return }

    const copyFromId = searchParams.get("copyFrom")
    if (!copyFromId) return

    setCopyLoading(true)
    api.goodsReceipts.get(copyFromId)
      .then((src: GoodsReceiptResponse) => {
        const mapped = (src.lines ?? []).map((l, i): FormLine => ({
          _key: i + 1,
          productId: l.productId,
          productName: l.product?.name || "",
          unitId: l.unitId,
          unitName: l.unit?.name || "",
          quantity: fromQuantity(l.quantity),
          unitPrice: fromMinorUnits(l.unitPrice, decimalPlaces),
          vatRateId: l.vatRateId,
          vatRateName: l.vatRate?.name || "",
          vatPercent: String(l.vatPercent ?? 0),
          discountPercent: l.discountPercent || "0",
        }))
        replace({
          ...INITIAL_FORM_STATE,
          organizationId: src.organizationId,
          organizationName: src.organization?.name || "",
          supplierId: src.supplierId,
          supplierName: src.supplier?.name || "",
          warehouseId: src.warehouseId,
          warehouseName: src.warehouse?.name || "",
          currencyId: src.currencyId || "",
          currencyName: src.currency?.name || "",
          contractId: src.contractId || "",
          contractName: src.contract?.name || "",
          supplierDocNumber: src.supplierDocNumber || "",
          incomingNumber: src.incomingNumber || "",
          amountIncludesVat: src.amountIncludesVat,
          description: src.description || "",
          lines: mapped,
          nextKey: mapped.length + 1,
        })
        markDirty()
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Ошибка копирования документа")
      })
      .finally(() => setCopyLoading(false))
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleChange = () => markDirty()

  const addLine = () => {
    update({ lines: [...f.lines, emptyLine(f.nextKey)], nextKey: f.nextKey + 1 })
    markDirty()
  }

  const removeLine = (key: number) => {
    update({ lines: f.lines.filter((l) => l._key !== key) })
    markDirty()
  }

  const updateLine = (key: number, field: keyof FormLine, value: string) => {
    update({ lines: f.lines.map((l) => l._key === key ? { ...l, [field]: value } : l) })
    markDirty()
  }

  // ── Computed totals ───────────────────────────────────────────────────
  const totals = useMemo(() => computeTotals(f.lines, f.amountIncludesVat, decimalPlaces), [f.lines, f.amountIncludesVat, decimalPlaces])

  const buildPayload = (postImmediately: boolean): CreateGoodsReceiptRequest => ({
    date: f.date || new Date().toISOString(),
    organizationId: f.organizationId,
    supplierId: f.supplierId,
    warehouseId: f.warehouseId,
    currencyId: f.currencyId || undefined,
    contractId: f.contractId || null,
    supplierDocNumber: f.supplierDocNumber || undefined,
    incomingNumber: f.incomingNumber || null,
    amountIncludesVat: f.amountIncludesVat,
    description: f.description || undefined,
    postImmediately,
    lines: f.lines.map((l): GoodsReceiptLineRequest => ({
      productId: l.productId,
      unitId: l.unitId,
      quantity: toQuantity(l.quantity),
      unitPrice: toMinorUnits(l.unitPrice, decimalPlaces),
      vatRateId: l.vatRateId,
      vatPercent: parseInt(l.vatPercent || "0"),
      discountPercent: l.discountPercent || "0",
    })),
  })

  const handleSave = async (postImmediately: boolean, andClose: boolean) => {
    if (!f.supplierId || !f.warehouseId || !f.organizationId) {
      setError("Укажите поставщика, склад и организацию")
      return
    }
    if (f.lines.length === 0) {
      setError("Добавьте хотя бы одну строку товаров")
      return
    }
    setSaving(true)
    setError(null)
    try {
      const created = await api.goodsReceipts.create(buildPayload(postImmediately))
      markClean()
      clear()
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

  const isCopy = !!searchParams.get("copyFrom")

  if (copyLoading) {
    return (
      <div className="flex h-full items-center justify-center text-muted-foreground">
        <Loader2 className="mr-2 h-5 w-5 animate-spin" />
        Копирование документа…
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      <FormToolbar
        title={isCopy ? "Приходная накладная (копирование)" : "Приходная накладная (создание)"}
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
          {!headerCollapsed && <div className="border-b bg-card p-4 shrink-0">
            <div className="grid grid-cols-1 gap-x-6 gap-y-3 md:grid-cols-2 lg:grid-cols-3">
              <div>
                <Label className="text-xs text-muted-foreground">Организация *</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={f.organizationId}
                    displayName={f.organizationName}
                    apiEndpoint="/catalog/organizations"
                    placeholder="Выберите организацию"
                    onChange={(id, name) => { update({ organizationId: id, organizationName: name }); markDirty() }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Поставщик *</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={f.supplierId}
                    displayName={f.supplierName}
                    apiEndpoint="/catalog/counterparties"
                    placeholder="Выберите поставщика"
                    onChange={(id, name) => { update({ supplierId: id, supplierName: name }); markDirty() }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Дата *</Label>
                <DatePicker
                  value={date}
                  onChange={(d) => { update({ date: d?.toISOString() }); markDirty() }}
                  className="mt-1 h-9"
                />
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Склад *</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={f.warehouseId}
                    displayName={f.warehouseName}
                    apiEndpoint="/catalog/warehouses"
                    placeholder="Выберите склад"
                    onChange={(id, name) => { update({ warehouseId: id, warehouseName: name }); markDirty() }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Договор</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={f.contractId}
                    displayName={f.contractName}
                    apiEndpoint="/catalog/contracts"
                    placeholder="Выберите договор"
                    onChange={(id, name) => { update({ contractId: id, contractName: name }); markDirty() }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Валюта</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={f.currencyId}
                    displayName={f.currencyName}
                    apiEndpoint="/catalog/currencies"
                    placeholder="Выберите валюту"
                    onChange={(id, name) => { update({ currencyId: id, currencyName: name }); markDirty() }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">№ вх. документа</Label>
                <Input className="mt-1" value={f.supplierDocNumber} onChange={(e) => { update({ supplierDocNumber: e.target.value }); markDirty() }} />
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Вх. номер</Label>
                <Input className="mt-1" value={f.incomingNumber} onChange={(e) => { update({ incomingNumber: e.target.value }); markDirty() }} />
              </div>
              <div className="flex items-center gap-3 mt-5">
                <Switch checked={f.amountIncludesVat} onCheckedChange={(v) => { update({ amountIncludesVat: v }); markDirty() }} />
                <Label className="text-xs">НДС включён в сумму</Label>
              </div>
            </div>
          </div>}

          <div className="flex-1 min-h-0 grid grid-rows-[auto_1fr] border-t">
            <Tabs defaultValue="goods" className="contents">
              <div className="flex items-center border-b bg-card px-4 row-start-1 col-start-1">
                <TabsList variant="line" className="border-b-0 h-11 self-start">
                  <TabsTrigger value="goods" variant="line" className="text-xs">
                    Товары ({f.lines.length})
                  </TabsTrigger>
                  <TabsTrigger value="additional" variant="line" className="text-xs">Дополнительно</TabsTrigger>
                </TabsList>
                <Button
                  variant="ghost"
                  size="icon"
                  className="ml-auto h-7 w-7 text-muted-foreground hover:text-foreground"
                  onClick={toggleHeader}
                  title={headerCollapsed ? "Развернуть шапку" : "Свернуть шапку"}
                >
                  {headerCollapsed ? <ChevronsDown className="h-4 w-4" /> : <ChevronsUp className="h-4 w-4" />}
                </Button>
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
                          <th className="min-w-[160px] px-3 py-2 text-left text-xs font-medium text-muted-foreground">Товар</th>
                          <th className="w-[140px] px-3 py-2 text-left text-xs font-medium text-muted-foreground">Ед. изм.</th>
                          <th className="w-24 px-3 py-2 text-right text-xs font-medium text-muted-foreground">Кол-во</th>
                          <th className="w-24 px-3 py-2 text-right text-xs font-medium text-muted-foreground">Цена</th>
                          <th className="w-[120px] px-3 py-2 text-left text-xs font-medium text-muted-foreground">Ставка НДС</th>
                          <th className="w-16 px-3 py-2 text-right text-xs font-medium text-muted-foreground">% НДС</th>
                          <th className="w-8" />
                        </tr>
                      </thead>
                      <tbody>
                        {f.lines.length === 0 && (
                          <tr>
                            <td colSpan={8} className="px-4 py-8 text-center text-sm text-muted-foreground">
                              Нажмите &quot;Добавить&quot; для добавления строки
                            </td>
                          </tr>
                        )}
                        {f.lines.map((line, idx) => (
                          <tr key={line._key} className="border-b hover:bg-muted/30 transition-colors">
                            <td className="px-2 py-1.5 text-center text-xs text-muted-foreground">{idx + 1}</td>
                            <td className="px-1 py-1">
                              {/* ⚡ Perf: single setLines call instead of updateLine() + setLines() (was 2 array traversals, now 1) */}
                              <ReferenceField compact value={line.productId} displayName={line.productName} apiEndpoint="/catalog/nomenclature" placeholder="Номенклатура" onChange={(id, name) => { update({ lines: f.lines.map(l => l._key === line._key ? { ...l, productId: id, productName: name } : l) }); markDirty() }} />
                            </td>
                            <td className="px-1 py-1">
                              <ReferenceField compact value={line.unitId} displayName={line.unitName} apiEndpoint="/catalog/units" placeholder="Ед. изм." onChange={(id, name) => { update({ lines: f.lines.map(l => l._key === line._key ? { ...l, unitId: id, unitName: name } : l) }); markDirty() }} />
                            </td>
                            <td className="px-1 py-1"><Input className="h-7 text-right font-mono text-xs" type="number" step="0.001" value={line.quantity} onChange={(e) => updateLine(line._key, "quantity", e.target.value)} /></td>
                            <td className="px-1 py-1"><Input className="h-7 text-right font-mono text-xs" type="number" step={moneyStep(decimalPlaces)} value={line.unitPrice} onChange={(e) => updateLine(line._key, "unitPrice", e.target.value)} /></td>
                            <td className="px-1 py-1">
                              <ReferenceField compact value={line.vatRateId} displayName={line.vatRateName} apiEndpoint="/catalog/vat-rates" placeholder="Ставка НДС" onChange={(id, name) => {
                                update({ lines: f.lines.map(l => l._key === line._key ? { ...l, vatRateId: id, vatRateName: name } : l) })
                                markDirty()
                                if (id) {
                                  fetchVatRatePercent(id).then(pct => {
                                    update((prev) => ({ lines: prev.lines.map(l => l._key === line._key ? { ...l, vatPercent: pct } : l) }))
                                  })
                                }
                              }} />
                            </td>
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
                  <Textarea rows={4} className="mt-1" value={f.description} onChange={(e) => { update({ description: e.target.value }); markDirty() }} placeholder="Комментарий к документу..." />
                </div>
              </TabsContent>
            </Tabs>
          </div>

          {/* Footer with totals */}
          <DocumentTotalsFooter totalAmount={totals.totalAmount} totalVat={totals.totalVat} decimalPlaces={decimalPlaces} currencySymbol={currencySymbol} />
        </div>

        {/* Right Sidebar — collapsible */}
        <FormSidebar collapsed={sidebarCollapsed} onToggle={toggleSidebar} />
      </div>
    </div>
  )
}
