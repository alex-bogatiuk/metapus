"use client"

import { useState, useEffect, useMemo, useCallback } from "react"
import { useCollapsible } from "@/hooks/useCollapsible"
import { useRouter, useParams, usePathname } from "next/navigation"
import {
  Plus,
  Loader2,
  ChevronsUp,
  ChevronsDown,
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
import { useTabDirty } from "@/hooks/useTabDirty"
import { useTabTitle } from "@/hooks/useTabTitle"
import { useFormDraft } from "@/hooks/useFormDraft"
import { api } from "@/lib/api"
import { fromQuantity, fromMinorUnits, toQuantity, toMinorUnits, DEFAULT_DECIMAL_PLACES } from "@/lib/format"
import { useCurrencyScale } from "@/hooks/useCurrencyScale"
import { type FormLine, emptyLine, fetchVatRatePercent, computeTotals } from "@/lib/document-form"
import { DocumentTotalsFooter } from "@/components/shared/document-totals-footer"
import { toast } from "sonner"
import type { GoodsReceiptResponse, GoodsReceiptLineRequest, UpdateGoodsReceiptRequest } from "@/types/document"

const SIDEBAR_STORAGE_KEY = "metapus-form-sidebar-collapsed"
const HEADER_STORAGE_KEY = "metapus-form-header-collapsed"

// ── Edit form state (single typed object for draft persistence) ─────────

interface GoodsReceiptEditFormState {
  _doc: GoodsReceiptResponse | null
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

const INITIAL_EDIT_STATE: GoodsReceiptEditFormState = {
  _doc: null,
  date: undefined,
  organizationId: "", organizationName: "",
  supplierId: "", supplierName: "",
  warehouseId: "", warehouseName: "",
  currencyId: "", currencyName: "",
  contractId: "", contractName: "",
  supplierDocNumber: "", incomingNumber: "",
  amountIncludesVat: true, description: "",
  lines: [], nextKey: 1,
}

export default function GoodsReceiptFormPage() {
  const router = useRouter()
  const pathname = usePathname()
  const params = useParams<{ id: string }>()
  const { markDirty, markClean } = useTabDirty()

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [sidebarCollapsed, toggleSidebar] = useCollapsible(SIDEBAR_STORAGE_KEY, true)
  const [headerCollapsed, toggleHeader] = useCollapsible(HEADER_STORAGE_KEY, false)

  // ── Single typed form state with automatic draft persistence ────────
  const { state: f, update, replace, clear, hasDraft } = useFormDraft<GoodsReceiptEditFormState>(
    pathname,
    INITIAL_EDIT_STATE,
    { shouldPersist: (s) => !!(s._doc && (s.organizationId || s.supplierId || s.warehouseId || s.lines.length > 0)) },
  )

  // Convenience aliases
  const doc = f._doc
  const date = f.date ? new Date(f.date) : undefined
  useTabTitle(doc?.number || undefined, "Приходная накладная")

  // Dynamic currency scale for monetary fields
  const { decimalPlaces, symbol: currencySymbol } = useCurrencyScale(f.currencyId || undefined)

  // ── Fetch document (skipped if draft was restored) ──────────────────
  useEffect(() => {
    if (!params.id) return

    if (hasDraft) { setLoading(false); return }

    setLoading(true)
    api.goodsReceipts.get(params.id).then((d) => {
      const mapped = (d.lines ?? []).map((l, i) => ({
        _key: i + 1,
        productId: l.productId,
        productName: l.product?.name || "",
        unitId: l.unitId,
        unitName: l.unit?.name || "",
        quantity: fromQuantity(l.quantity),
        unitPrice: fromMinorUnits(l.unitPrice, d.currency?.decimalPlaces ?? DEFAULT_DECIMAL_PLACES),
        vatRateId: l.vatRateId,
        vatRateName: l.vatRate?.name || "",
        vatPercent: String(l.vatPercent ?? 0),
        discountPercent: l.discountPercent || "0",
        amount: l.amount,
        vatAmount: l.vatAmount,
      }))
      replace({
        _doc: d,
        date: d.date || undefined,
        organizationId: d.organizationId,
        organizationName: d.organization?.name || "",
        supplierId: d.supplierId,
        supplierName: d.supplier?.name || "",
        warehouseId: d.warehouseId,
        warehouseName: d.warehouse?.name || "",
        currencyId: d.currencyId || "",
        currencyName: d.currency?.name || "",
        contractId: d.contractId || "",
        contractName: d.contract?.name || "",
        supplierDocNumber: d.supplierDocNumber || "",
        incomingNumber: d.incomingNumber || "",
        amountIncludesVat: d.amountIncludesVat,
        description: d.description || "",
        lines: mapped,
        nextKey: mapped.length + 1,
      })
      markClean()
    }).catch((err) => {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    }).finally(() => {
      setLoading(false)
    })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params.id])

  const handleChange = () => markDirty()

  const addLine = () => {
    update({ lines: [...f.lines, emptyLine(f.nextKey)], nextKey: f.nextKey + 1 })
    markDirty()
  }

  // ── Stable callbacks for DocumentLineRow (React.memo-safe) ──────────
  const handleUpdateField = useCallback((key: number, field: keyof FormLine, value: string) => {
    const editableFields: (keyof FormLine)[] = ["quantity", "unitPrice", "vatPercent", "discountPercent"]
    update((prev) => ({ lines: prev.lines.map((l) => {
      if (l._key !== key) return l
      const updated = { ...l, [field]: value }
      if (editableFields.includes(field)) {
        updated.amount = undefined
        updated.vatAmount = undefined
      }
      return updated
    }) }))
    markDirty()
  }, [update, markDirty])

  const handleUpdateRef = useCallback((key: number, patch: Partial<FormLine>) => {
    update((prev) => ({ lines: prev.lines.map((l) => l._key === key ? { ...l, ...patch, amount: undefined, vatAmount: undefined } : l) }))
    markDirty()
  }, [update, markDirty])

  const handleUpdateVatRate = useCallback((key: number, id: string, name: string) => {
    update((prev) => ({ lines: prev.lines.map((l) => l._key === key ? { ...l, vatRateId: id, vatRateName: name, amount: undefined, vatAmount: undefined } : l) }))
    markDirty()
    if (id) {
      fetchVatRatePercent(id).then((pct) => {
        update((prev) => ({ lines: prev.lines.map((l) => l._key === key ? { ...l, vatPercent: pct, amount: undefined, vatAmount: undefined } : l) }))
      })
    }
  }, [update, markDirty])

  const handleRemoveLine = useCallback((key: number) => {
    update((prev) => ({ lines: prev.lines.filter((l) => l._key !== key) }))
    markDirty()
  }, [update, markDirty])

  const totals = useMemo(() => {
    if (doc && !f.lines.some((l) => l.amount === undefined)) {
      return { totalAmount: doc.totalAmount, totalVat: doc.totalVat }
    }
    return computeTotals(f.lines, f.amountIncludesVat, decimalPlaces)
  }, [f.lines, f.amountIncludesVat, doc, decimalPlaces])

  const buildUpdatePayload = (): UpdateGoodsReceiptRequest => ({
    date: f.date || new Date().toISOString(),
    organizationId: f.organizationId,
    supplierId: f.supplierId,
    warehouseId: f.warehouseId,
    currencyId: f.currencyId || null,
    contractId: f.contractId || null,
    supplierDocNumber: f.supplierDocNumber || null,
    incomingNumber: f.incomingNumber || null,
    amountIncludesVat: f.amountIncludesVat,
    description: f.description || null,
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

  const handleSave = async (andClose: boolean) => {
    setSaving(true)
    setError(null)
    try {
      let updated: GoodsReceiptResponse
      if (doc?.posted) {
        updated = await api.goodsReceipts.updateAndRepost(params.id, buildUpdatePayload())
      } else {
        updated = await api.goodsReceipts.update(params.id, buildUpdatePayload())
      }
      update({ _doc: updated })
      markClean()
      clear()
      if (andClose) router.push("/purchases/goods-receipts")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка сохранения")
      try { const r = await api.goodsReceipts.get(params.id); update({ _doc: r }) } catch { /* ignore */ }
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
      update({ _doc: updated })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Ошибка проведения")
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
      update({ _doc: updated })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Ошибка отмены проведения")
    } finally {
      setSaving(false)
    }
  }

  const handleToggleDeletionMark = async () => {
    if (!doc) return
    setSaving(true)
    setError(null)
    try {
      await api.goodsReceipts.setDeletionMark(params.id, { marked: !doc.deletionMark })
      const updated = await api.goodsReceipts.get(params.id)
      // Re-map lines from the refreshed document
      const mapped = (updated.lines ?? []).map((l, i) => ({
        _key: i + 1,
        productId: l.productId,
        productName: l.product?.name || "",
        unitId: l.unitId,
        unitName: l.unit?.name || "",
        quantity: fromQuantity(l.quantity),
        unitPrice: fromMinorUnits(l.unitPrice, updated.currency?.decimalPlaces ?? DEFAULT_DECIMAL_PLACES),
        vatRateId: l.vatRateId,
        vatRateName: l.vatRate?.name || "",
        vatPercent: String(l.vatPercent ?? 0),
        discountPercent: l.discountPercent || "0",
        amount: l.amount,
        vatAmount: l.vatAmount,
      }))
      replace({
        _doc: updated,
        date: updated.date || undefined,
        organizationId: updated.organizationId,
        organizationName: updated.organization?.name || "",
        supplierId: updated.supplierId,
        supplierName: updated.supplier?.name || "",
        warehouseId: updated.warehouseId,
        warehouseName: updated.warehouse?.name || "",
        currencyId: updated.currencyId || "",
        currencyName: updated.currency?.name || "",
        contractId: updated.contractId || "",
        contractName: updated.contract?.name || "",
        supplierDocNumber: updated.supplierDocNumber || "",
        incomingNumber: updated.incomingNumber || "",
        amountIncludesVat: updated.amountIncludesVat,
        description: updated.description || "",
        lines: mapped,
        nextKey: mapped.length + 1,
      })
      markClean()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Ошибка")
    } finally {
      setSaving(false)
    }
  }

  const handlePostAndClose = async () => {
    setSaving(true)
    setError(null)
    try {
      // Single atomic call: update + (re)post on backend
      await api.goodsReceipts.updateAndRepost(params.id, buildUpdatePayload())
      markClean()
      clear()
      router.push("/purchases/goods-receipts")
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка")
      try { const r = await api.goodsReceipts.get(params.id); update({ _doc: r }) } catch { /* ignore */ }
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
        extraMenuItems={[
          {
            label: doc?.deletionMark ? "Снять пометку удаления" : "Пометить на удаление",
            onClick: handleToggleDeletionMark,
            destructive: !doc?.deletionMark,
          },
        ]}
        backHref="/purchases/goods-receipts"
        backTargetId={params.id}
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
                <Label className="text-xs text-muted-foreground">Организация</Label>
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
                <Label className="text-xs text-muted-foreground">Поставщик</Label>
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
                <Label className="text-xs text-muted-foreground">Номер</Label>
                <div className="mt-1 flex items-center gap-2">
                  <Input value={doc?.number || ""} className="flex-1 bg-muted/30" readOnly />
                  <Label className="shrink-0 text-xs text-muted-foreground">от:</Label>
                  <DatePicker
                    value={date}
                    onChange={(d) => { update({ date: d?.toISOString() }); markDirty() }}
                    className="w-44 h-9"
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Склад</Label>
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
                <Switch checked={f.amountIncludesVat} onCheckedChange={(v) => { update({ amountIncludesVat: v, lines: f.lines.map(l => ({ ...l, amount: undefined, vatAmount: undefined })) }); markDirty() }} />
                <Label className="text-xs">НДС включён в сумму</Label>
              </div>
            </div>
          </div>}

          <div className="flex-1 min-h-0 grid grid-rows-[auto_1fr]">
            <Tabs defaultValue="goods" className="contents">
              <div className="flex items-center border-b bg-card px-4 row-start-1 col-start-1">
                <TabsList variant="line" className="border-b-0 h-11 self-start">
                  <TabsTrigger value="goods" variant="line" className="text-xs">
                    Товары ({f.lines.length})
                  </TabsTrigger>
                  <TabsTrigger value="additional" variant="line" className="text-xs">
                    Дополнительно
                  </TabsTrigger>
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
                        {f.lines.length === 0 && (
                          <tr>
                            <td colSpan={9} className="px-4 py-8 text-center text-sm text-muted-foreground">
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
                            showAmounts
                          />
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              </TabsContent>

              <TabsContent value="additional" className="mt-0 p-4 overflow-auto row-start-2 col-start-1">
                <div>
                  <Label className="text-xs text-muted-foreground">Описание</Label>
                  <Textarea rows={4} className="mt-1" value={f.description} onChange={(e) => { update({ description: e.target.value }); markDirty() }} />
                </div>
              </TabsContent>
            </Tabs>
          </div>

          {/* Footer */}
          <DocumentTotalsFooter totalAmount={totals.totalAmount} totalVat={totals.totalVat} decimalPlaces={decimalPlaces} currencySymbol={currencySymbol} />
        </div>

        {/* Right Sidebar — collapsible */}
        <FormSidebar
          collapsed={sidebarCollapsed}
          onToggle={toggleSidebar}
          meta={doc ? {
            updatedAt: doc.updatedAt,
            updatedByUser: doc.updatedByUser,
            createdAt: doc.createdAt,
            createdByUser: doc.createdByUser,
          } : undefined}
        />
      </div>
    </div>
  )
}
