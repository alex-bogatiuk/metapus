"use client"

import { cn } from "@/lib/utils"
import { useCompactMode } from "@/hooks/useCompactMode"

import { useState, useEffect, useMemo, useCallback } from "react"
import { useCollapsible } from "@/hooks/useCollapsible"
import { useRouter, useSearchParams, usePathname } from "next/navigation"
import {
  Plus,
  Loader2,
  ChevronsUp,
  ChevronsDown,
  Search,
} from "lucide-react"
import { FormToolbar } from "@/components/shared/form-toolbar"
import { FormSidebar } from "@/components/shared/form-sidebar"
import { ReferenceField } from "@/components/shared/reference-field"
import { DocumentLineRow } from "@/components/shared/document-line-row"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { Switch } from "@/components/ui/switch"
import { DatePicker } from "@/components/ui/date-picker"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useCloseTab } from "@/hooks/useCloseTab"
import { useTabStateStore } from "@/stores/useTabStateStore"
import { useFormDraft } from "@/hooks/useFormDraft"
import { api } from "@/lib/api"
import { fromQuantity, fromMinorUnits, toQuantity, toMinorUnits } from "@/lib/format"
import { useCurrencyScale } from "@/hooks/useCurrencyScale"
import { type FormLine, emptyLine, fetchVatRatePercent, computeTotals, linesToExistingPickerLines, mergePickedIntoLines } from "@/lib/document-form"
import { DocumentTotalsFooter } from "@/components/shared/document-totals-footer"
import type { GoodsIssueLineRequest, CreateGoodsIssueRequest, GoodsIssueResponse } from "@/types/document"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { ProductPickerDialog } from "@/components/shared/product-picker-dialog"
import type { PickedItem } from "@/types/picker"

const SIDEBAR_STORAGE_KEY = "metapus-form-sidebar-collapsed"
const HEADER_STORAGE_KEY = "metapus-form-header-collapsed"

// ── Form state (single typed object for draft persistence) ────────────

interface GoodsIssueFormState {
  date: string | undefined
  organizationId: string
  organizationName: string
  customerId: string
  customerName: string
  warehouseId: string
  warehouseName: string
  currencyId: string
  currencyName: string
  contractId: string
  contractName: string
  customerOrderNumber: string
  basisType: string
  basisId: string
  amountIncludesVat: boolean
  description: string
  lines: FormLine[]
  nextKey: number
}

const INITIAL_FORM_STATE: GoodsIssueFormState = {
  date: new Date().toISOString(),
  organizationId: "", organizationName: "",
  customerId: "", customerName: "",
  warehouseId: "", warehouseName: "",
  currencyId: "", currencyName: "",
  contractId: "", contractName: "",
  customerOrderNumber: "",
  basisType: "", basisId: "",
  amountIncludesVat: true, description: "",
  lines: [], nextKey: 1,
}

export default function NewGoodsIssuePage() {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()
  const { markDirty, markClean } = useTabDirty()
  const { closeOne } = useCloseTab()

  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [sidebarCollapsed, toggleSidebar] = useCollapsible(SIDEBAR_STORAGE_KEY, true)
  const [headerCollapsed, toggleHeader] = useCollapsible(HEADER_STORAGE_KEY, false)
  const [copyLoading, setCopyLoading] = useState(false)
  const [pickerOpen, setPickerOpen] = useState(false)

  // ── Single typed form state with automatic draft persistence ────────
  const { state: f, update, replace, clear, hasDraft } = useFormDraft<GoodsIssueFormState>(
    pathname,
    INITIAL_FORM_STATE,
    { shouldPersist: (s) => !!(s.organizationId || s.customerId || s.warehouseId || s.lines.length > 0) },
  )

  // Convenience: Date object from ISO string
  const date = f.date ? new Date(f.date) : undefined

  // Dynamic currency scale for monetary fields
  const { decimalPlaces, symbol: currencySymbol } = useCurrencyScale(f.currencyId || undefined)

  // ── Copy from existing document ─────────────────────────────────────
  useEffect(() => {
    if (hasDraft) { return }

    const copyFromId = searchParams.get("copyFrom")
    const basisType = searchParams.get("basisType")
    const basisId = searchParams.get("basisId")
    
    if (copyFromId) {
      setCopyLoading(true)
      api.goodsIssues.get(copyFromId)
      .then((src: GoodsIssueResponse) => {
        const mapped = (src.lines ?? []).map((l, i): FormLine => ({
          _key: i + 1,
          productId: l.productId,
          productName: l.product?.name || "",
          productCode: l.product?.code || "",
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
          customerId: src.customerId,
          customerName: src.customer?.name || "",
          warehouseId: src.warehouseId,
          warehouseName: src.warehouse?.name || "",
          currencyId: src.currencyId || "",
          currencyName: src.currency?.name || "",
          contractId: src.contractId || "",
          contractName: src.contract?.name || "",
          customerOrderNumber: src.customerOrderNumber || "",
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
    } else if (basisType === "GoodsReceipt" && basisId) {
      setCopyLoading(true)
      api.goodsReceipts.get(basisId)
        .then((src) => {
          const mapped = (src.lines ?? []).map((l, i): FormLine => ({
            _key: i + 1,
            productId: l.productId,
            productName: l.product?.name || "",
            productCode: l.product?.code || "",
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
            warehouseId: src.warehouseId,
            warehouseName: src.warehouse?.name || "",
            currencyId: src.currencyId || "",
            currencyName: src.currency?.name || "",
            amountIncludesVat: src.amountIncludesVat,
            basisType,
            basisId,
            lines: mapped,
            nextKey: mapped.length + 1,
          })
          markDirty()
        })
        .catch((err) => {
          setError(err instanceof Error ? err.message : "Ошибка загрузки документа-основания")
        })
        .finally(() => setCopyLoading(false))
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const addLine = () => {
    update({ lines: [...f.lines, emptyLine(f.nextKey)], nextKey: f.nextKey + 1 })
    markDirty()
  }

  const existingPickerLines = useMemo(() => linesToExistingPickerLines(f.lines), [f.lines])

  const handlePick = useCallback((items: PickedItem[]) => {
    const knownIds = new Set(existingPickerLines.map((l) => l.productId))
    update((prev) => mergePickedIntoLines(prev.lines, items, prev.nextKey, knownIds))
    markDirty()
  }, [update, markDirty, existingPickerLines])

  // ── Stable callbacks for DocumentLineRow (React.memo-safe) ──────────
  const handleUpdateField = useCallback((key: number, field: keyof FormLine, value: string) => {
    update((prev) => ({ lines: prev.lines.map((l) => l._key === key ? { ...l, [field]: value } : l) }))
    markDirty()
  }, [update, markDirty])

  const handleUpdateRef = useCallback((key: number, patch: Partial<FormLine>) => {
    update((prev) => ({ lines: prev.lines.map((l) => l._key === key ? { ...l, ...patch } : l) }))
    markDirty()
  }, [update, markDirty])

  const handleUpdateVatRate = useCallback((key: number, id: string, name: string) => {
    update((prev) => ({ lines: prev.lines.map((l) => l._key === key ? { ...l, vatRateId: id, vatRateName: name } : l) }))
    markDirty()
    if (id) {
      fetchVatRatePercent(id).then((pct) => {
        update((prev) => ({ lines: prev.lines.map((l) => l._key === key ? { ...l, vatPercent: pct } : l) }))
      })
    }
  }, [update, markDirty])

  const handleRemoveLine = useCallback((key: number) => {
    update((prev) => ({ lines: prev.lines.filter((l) => l._key !== key) }))
    markDirty()
  }, [update, markDirty])

  // ── Computed totals ───────────────────────────────────────────────────
  const totals = useMemo(() => computeTotals(f.lines, f.amountIncludesVat, decimalPlaces), [f.lines, f.amountIncludesVat, decimalPlaces])

  const buildPayload = (postImmediately: boolean): CreateGoodsIssueRequest => ({
    date: f.date || new Date().toISOString(),
    organizationId: f.organizationId,
    customerId: f.customerId,
    warehouseId: f.warehouseId,
    currencyId: f.currencyId || undefined,
    contractId: f.contractId || null,
    customerOrderNumber: f.customerOrderNumber || undefined,
    basisType: f.basisType || undefined,
    basisId: f.basisId || undefined,
    amountIncludesVat: f.amountIncludesVat,
    description: f.description || undefined,
    postImmediately,
    lines: f.lines.map((l): GoodsIssueLineRequest => ({
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
    if (!f.customerId || !f.warehouseId || !f.organizationId) {
      setError("Укажите покупателя, склад и организацию")
      return
    }
    if (f.lines.length === 0) {
      setError("Добавьте хотя бы одну строку товаров")
      return
    }
    setSaving(true)
    setError(null)
    try {
      const created = await api.goodsIssues.create(buildPayload(postImmediately))
      markClean()
      clear()
      if (andClose) {
        useTabStateStore.getState().clearTab("/documents/goods-issues")
        closeOne(pathname)
        return
      } else {
        router.replace(`/documents/goods-issues/${created.id}`)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка сохранения")
    } finally {
      setSaving(false)
    }
  }

  const isCopy = !!searchParams.get("copyFrom")
  const compact = useCompactMode()

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
        title={`${useMetadataStore.getState().getLabel("goods_issue", "singular")} (${isCopy ? "копирование" : "создание"})`}
        primaryAction={{
          label: saving ? "Сохранение…" : "Провести и закрыть",
          variant: "default",
          onClick: () => handleSave(true, true),
        }}
        secondaryActions={[
          { label: "Записать", onClick: () => handleSave(false, false) },
          { label: "Провести", onClick: () => handleSave(true, false) },
        ]}
        backHref="/documents/goods-issues"
        onClose={() => router.push("/documents/goods-issues")}
      />

      {error && (
        <div className="bg-destructive/10 border-b border-destructive/20 px-4 py-2 text-sm text-destructive">
          {error}
        </div>
      )}

      <div className="flex flex-1 overflow-hidden">
        <div className="flex flex-1 flex-col overflow-hidden relative">
          {!headerCollapsed && <div className={cn("border-b bg-card shrink-0", compact ? "p-2" : "p-4")}>
            <div className={cn("grid grid-cols-1 gap-x-6 md:grid-cols-2 lg:grid-cols-3", compact ? "gap-y-1.5" : "gap-y-3")}>
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
                <Label className="text-xs text-muted-foreground">Покупатель *</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={f.customerId}
                    displayName={f.customerName}
                    apiEndpoint="/catalog/counterparties"
                    placeholder="Выберите покупателя"
                    onChange={(id, name) => { update({ customerId: id, customerName: name }); markDirty() }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Дата *</Label>
                <DatePicker
                  value={date}
                  onChange={(d) => { update({ date: d?.toISOString() }); markDirty() }}
                  className={cn("mt-1", compact ? "h-7" : "h-9")}
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
                <Label className="text-xs text-muted-foreground">№ заказа покупателя</Label>
                <Input className={cn("mt-1", compact && "h-7")} value={f.customerOrderNumber} onChange={(e) => { update({ customerOrderNumber: e.target.value }); markDirty() }} />
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
                <TabsList variant="line" className={cn("border-b-0 self-start", compact ? "h-9" : "h-11")}>
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
                    <Button variant="outline" size="sm" onClick={() => setPickerOpen(true)}>
                      <Search className="mr-1 h-3 w-3" />
                      Подбор
                    </Button>
                  </div>
                  <ScrollArea className="flex-1">
                    <table className="w-full text-sm border-separate border-spacing-0">
                      <thead className="sticky top-0 z-10 bg-muted/90 backdrop-blur-sm">
                        <tr>
                          <th className={cn("w-10 border-b px-2 text-center text-muted-foreground font-semibold", compact ? "py-1 text-[10px]" : "py-2 text-[11px]")}>N</th>
                          <th className={cn("min-w-[160px] border-b px-3 text-left text-muted-foreground font-semibold", compact ? "py-1 text-[10px]" : "py-2 text-[11px]")}>Товар</th>
                          <th className={cn("w-[140px] border-b px-3 text-left text-muted-foreground font-semibold", compact ? "py-1 text-[10px]" : "py-2 text-[11px]")}>Ед. изм.</th>
                          <th className={cn("w-24 border-b px-3 text-right text-muted-foreground font-semibold", compact ? "py-1 text-[10px]" : "py-2 text-[11px]")}>Кол-во</th>
                          <th className={cn("w-24 border-b px-3 text-right text-muted-foreground font-semibold", compact ? "py-1 text-[10px]" : "py-2 text-[11px]")}>Цена</th>
                          <th className={cn("w-[120px] border-b px-3 text-left text-muted-foreground font-semibold", compact ? "py-1 text-[10px]" : "py-2 text-[11px]")}>Ставка НДС</th>
                          <th className={cn("w-16 border-b px-3 text-right text-muted-foreground font-semibold", compact ? "py-1 text-[10px]" : "py-2 text-[11px]")}>% НДС</th>
                          <th className="w-8 border-b" />
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
                          <DocumentLineRow
                            key={line._key}
                            line={line}
                            rowNumber={idx + 1}
                            decimalPlaces={decimalPlaces}
                            amountIncludesVat={f.amountIncludesVat}
                            onUpdateField={handleUpdateField}
                            onUpdateRef={handleUpdateRef}
                            onUpdateVatRate={handleUpdateVatRate}
                            onRemove={handleRemoveLine}
                          />
                        ))}
                      </tbody>
                    </table>
                    <ScrollBar orientation="horizontal" />
                  </ScrollArea>
                </div>
              </TabsContent>

              <TabsContent value="additional" className="mt-0 overflow-hidden row-start-2 col-start-1">
                <ScrollArea className="h-full">
                <div className="p-4">
                <div>
                  <Label className="text-xs text-muted-foreground">Описание</Label>
                  <Textarea rows={4} className="mt-1" value={f.description} onChange={(e) => { update({ description: e.target.value }); markDirty() }} placeholder="Комментарий к документу..." />
                </div>
                </div>
                </ScrollArea>
              </TabsContent>
            </Tabs>
          </div>

          {/* Footer with totals */}
          <DocumentTotalsFooter totalAmount={totals.totalAmount} totalVat={totals.totalVat} decimalPlaces={decimalPlaces} currencySymbol={currencySymbol} />
        </div>

        <FormSidebar collapsed={sidebarCollapsed} onToggle={toggleSidebar} />
      </div>

      <ProductPickerDialog
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        onPick={handlePick}
        existingLines={existingPickerLines}
        warehouseId={f.warehouseId || undefined}
      />
    </div>
  )
}
