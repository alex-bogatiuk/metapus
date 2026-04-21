"use client"

import { cn } from "@/lib/utils"
import { useCompactMode } from "@/hooks/useCompactMode"

import { useState, useEffect, useMemo, useCallback } from "react"
import { useCollapsible } from "@/hooks/useCollapsible"
import { useRouter, useParams, usePathname } from "next/navigation"
import {
  Plus,
  Loader2,
  ChevronsUp,
  ChevronsDown,
  ArrowRightLeft,
  Network,
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
import { useDocumentFormSync } from "@/hooks/useDocumentFormSync"
import { useTabTitle } from "@/hooks/useTabTitle"
import { api } from "@/lib/api"
import { fromQuantity, fromMinorUnits, toQuantity, toMinorUnits, DEFAULT_DECIMAL_PLACES } from "@/lib/format"
import { useCurrencyScale } from "@/hooks/useCurrencyScale"
import { type FormLine, computeTotals, mapLinesToFormLines } from "@/lib/document-form"
import { useDocumentLineActions, useExistingPickerLines } from "@/hooks/useDocumentLineActions"
import { DocumentTotalsFooter } from "@/components/shared/document-totals-footer"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { PrintMenuButton } from "@/components/shared/print-menu-button"
import { useDocumentErrorHandler } from "@/hooks/useDocumentErrorHandler"
import { ProductPickerDialog } from "@/components/shared/product-picker-dialog"
import type { PickedItem } from "@/types/picker"
import type { GoodsIssueResponse, GoodsIssueLineRequest, UpdateGoodsIssueRequest } from "@/types/document"

const SIDEBAR_STORAGE_KEY = "metapus-form-sidebar-collapsed"
const HEADER_STORAGE_KEY = "metapus-form-header-collapsed"

// ── Edit form state (single typed object for draft persistence) ─────────

interface GoodsIssueEditFormState {
  _doc: GoodsIssueResponse | null
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
  amountIncludesVat: boolean
  description: string
  lines: FormLine[]
  nextKey: number
}

const INITIAL_EDIT_STATE: GoodsIssueEditFormState = {
  _doc: null,
  date: undefined,
  organizationId: "", organizationName: "",
  customerId: "", customerName: "",
  warehouseId: "", warehouseName: "",
  currencyId: "", currencyName: "",
  contractId: "", contractName: "",
  customerOrderNumber: "",
  amountIncludesVat: true, description: "",
  lines: [], nextKey: 1,
}

function mapDocToState(d: GoodsIssueResponse): GoodsIssueEditFormState {
  const dp = d.currency?.decimalPlaces ?? DEFAULT_DECIMAL_PLACES
  const { mapped, nextKey } = mapLinesToFormLines(d.lines ?? [], dp, { preserveAmounts: true })
  return {
    _doc: d,
    date: d.date || undefined,
    organizationId: d.organizationId,
    organizationName: d.organization?.name || "",
    customerId: d.customerId,
    customerName: d.customer?.name || "",
    warehouseId: d.warehouseId,
    warehouseName: d.warehouse?.name || "",
    currencyId: d.currencyId || "",
    currencyName: d.currency?.name || "",
    contractId: d.contractId || "",
    contractName: d.contract?.name || "",
    customerOrderNumber: d.customerOrderNumber || "",
    amountIncludesVat: d.amountIncludesVat,
    description: d.description || "",
    lines: mapped,
    nextKey,
  }
}

export default function GoodsIssueFormPage() {
  const router = useRouter()
  const params = useParams<{ id: string }>()

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const { error, setError, fieldErrors, setFieldErrors, handleError, clearErrors } = useDocumentErrorHandler()
  const [sidebarCollapsed, toggleSidebar] = useCollapsible(SIDEBAR_STORAGE_KEY, true)
  const [headerCollapsed, toggleHeader] = useCollapsible(HEADER_STORAGE_KEY, false)
  const [pickerOpen, setPickerOpen] = useState(false)

  // ── Single typed form state with automatic draft persistence + dirty sync ──
  const {
    state: f, doc, update, replace, syncFromServer, markDirty,
    clear, hasDraft, pathname, closeAndCleanup,
  } = useDocumentFormSync<GoodsIssueEditFormState, GoodsIssueResponse>(
    INITIAL_EDIT_STATE,
    mapDocToState,
    "/documents/goods-issues",
    { shouldPersist: (s) => !!(s._doc && (s.organizationId || s.customerId || s.warehouseId || s.lines.length > 0)) },
  )

  // Convenience aliases
  const date = f.date ? new Date(f.date) : undefined
  const giLabel = useMetadataStore((s) => s.getLabel("goods_issue", "singular"))
  useTabTitle(doc?.number || undefined, giLabel)

  // Dynamic currency scale for monetary fields
  const { decimalPlaces, symbol: currencySymbol } = useCurrencyScale(f.currencyId || undefined)

  // ── Fetch document (skipped if draft was restored) ──────────────────
  useEffect(() => {
    if (!params.id) return

    if (hasDraft) { setLoading(false); return }

    setLoading(true)
    api.goodsIssues.get(params.id).then((d) => {
      syncFromServer(d)
    }).catch((err) => {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    }).finally(() => {
      setLoading(false)
    })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params.id])

  // ── Line actions (generic hook) ───────────────────────────────────────
  const { addLine, handlePick: pickLines, handleUpdateField, handleUpdateRef, handleUpdateVatRate, handleRemoveLine } = useDocumentLineActions(update, markDirty, { resetAmountsOnEdit: true })
  const existingPickerLines = useExistingPickerLines(f.lines)
  const handlePick = useCallback((items: PickedItem[]) => pickLines(items, f.lines), [pickLines, f.lines])

  const totals = useMemo(() => {
    const serverLinesCount = doc?.lines?.length ?? 0
    if (doc && f.lines.length === serverLinesCount && !f.lines.some((l) => l.amount === undefined)) {
      return { totalAmount: doc.totalAmount, totalVat: doc.totalVat }
    }
    return computeTotals(f.lines, f.amountIncludesVat, decimalPlaces)
  }, [f.lines, f.amountIncludesVat, doc, decimalPlaces])

  const buildUpdatePayload = (): UpdateGoodsIssueRequest => ({
    version: doc?.version ?? 1,
    date: f.date || new Date().toISOString(),
    organizationId: f.organizationId,
    customerId: f.customerId,
    warehouseId: f.warehouseId,
    currencyId: f.currencyId || null,
    contractId: f.contractId || null,
    customerOrderNumber: f.customerOrderNumber || null,
    amountIncludesVat: f.amountIncludesVat,
    description: f.description || null,
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

  const handleSave = async (andClose: boolean) => {
    setSaving(true)
    clearErrors()
    try {
      let updated: GoodsIssueResponse
      if (doc?.posted) {
        updated = await api.goodsIssues.updateAndRepost(params.id, buildUpdatePayload())
      } else {
        updated = await api.goodsIssues.update(params.id, buildUpdatePayload())
      }
      syncFromServer(updated)
      clear()
      if (andClose) {
        closeAndCleanup()
        return
      }
    } catch (err) {
      handleError(err, "Ошибка сохранения")
      try { const r = await api.goodsIssues.get(params.id); update({ _doc: r }) } catch { /* ignore */ }
    } finally {
      setSaving(false)
    }
  }

  const handlePost = async () => {
    setSaving(true)
    clearErrors()
    try {
      await api.goodsIssues.post(params.id)
      const updated = await api.goodsIssues.get(params.id)
      syncFromServer(updated)
    } catch (err) {
      handleError(err, "Ошибка проведения", true)
    } finally {
      setSaving(false)
    }
  }

  const handleUnpost = async () => {
    setSaving(true)
    clearErrors()
    try {
      await api.goodsIssues.unpost(params.id)
      const updated = await api.goodsIssues.get(params.id)
      syncFromServer(updated)
    } catch (err) {
      handleError(err, "Ошибка отмены проведения", true)
    } finally {
      setSaving(false)
    }
  }

  const handleToggleDeletionMark = async () => {
    if (!doc) return
    setSaving(true)
    clearErrors()
    try {
      await api.goodsIssues.setDeletionMark(params.id, { marked: !doc.deletionMark })
      const updated = await api.goodsIssues.get(params.id)
      syncFromServer(updated)
    } catch (err) {
      handleError(err, "Ошибка", true)
    } finally {
      setSaving(false)
    }
  }

  const handlePostAndClose = async () => {
    setSaving(true)
    clearErrors()
    try {
      await api.goodsIssues.updateAndRepost(params.id, buildUpdatePayload())
      closeAndCleanup()
    } catch (err) {
      handleError(err, "Ошибка")
      try { const r = await api.goodsIssues.get(params.id); update({ _doc: r }) } catch { /* ignore */ }
    } finally {
      setSaving(false)
    }
  }

  const compact = useCompactMode()

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
        title={`${giLabel} ${doc?.number || ""}`}
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
        printMenu={
          <PrintMenuButton documentType="goods-issue" documentId={params.id} />
        }
        toolbarIcons={[
          ...(doc?.posted ? [{
            icon: <ArrowRightLeft className="h-3.5 w-3.5" />,
            title: "Движения документа",
            onClick: () => router.push(`/documents/goods-issues/${params.id}/movements`),
          }] : []),
          {
            icon: <Network className="h-3.5 w-3.5" />,
            title: "Связанные документы",
            onClick: () => router.push(`/documents/goods-issues/${params.id}/related`),
          },
        ]}
        extraMenuItems={[
          {
            label: "Журнал событий",
            onClick: () => router.push(`/settings/event-log?entityType=goods_issue&entityId=${params.id}`),
          },
          {
            label: doc?.deletionMark ? "Снять пометку удаления" : "Пометить на удаление",
            onClick: handleToggleDeletionMark,
            destructive: !doc?.deletionMark,
          },
        ]}
        backHref="/documents/goods-issues"
        backTargetId={params.id}
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
                <Label className="text-xs text-muted-foreground">Организация</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={f.organizationId}
                    displayName={f.organizationName}
                    apiEndpoint="/catalog/organizations"
                    placeholder="Выберите организацию"
                    error={fieldErrors.organizationId}
                    onChange={(id, name) => { update({ organizationId: id, organizationName: name }); markDirty(); setFieldErrors(prev => ({...prev, organizationId: ""})) }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Покупатель</Label>
                <div className="mt-1">
                  <ReferenceField
                    value={f.customerId}
                    displayName={f.customerName}
                    apiEndpoint="/catalog/counterparties"
                    placeholder="Выберите покупателя"
                    error={fieldErrors.customerId}
                    onChange={(id, name) => { update({ customerId: id, customerName: name }); markDirty(); setFieldErrors(prev => ({...prev, customerId: ""})) }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">Номер</Label>
                <div className="mt-1 flex items-center gap-2">
                  <Input value={doc?.number || ""} className={cn("flex-1 bg-muted/30", compact && "h-7")} readOnly />
                  <Label className="shrink-0 text-xs text-muted-foreground">от:</Label>
                  <DatePicker
                    value={date}
                    onChange={(d) => { update({ date: d?.toISOString() }); markDirty() }}
                    className={cn("w-44", compact ? "h-7" : "h-9")}
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
                    error={fieldErrors.warehouseId}
                    onChange={(id, name) => { update({ warehouseId: id, warehouseName: name }); markDirty(); setFieldErrors(prev => ({...prev, warehouseId: ""})) }}
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
                    error={fieldErrors.contractId}
                    onChange={(id, name) => { update({ contractId: id, contractName: name }); markDirty(); setFieldErrors(prev => ({...prev, contractId: ""})) }}
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
                    error={fieldErrors.currencyId}
                    onChange={(id, name) => { update({ currencyId: id, currencyName: name }); markDirty(); setFieldErrors(prev => ({...prev, currencyId: ""})) }}
                  />
                </div>
              </div>
              <div>
                <Label className="text-xs text-muted-foreground">№ заказа покупателя</Label>
                <Input className={cn("mt-1", compact && "h-7")} value={f.customerOrderNumber} onChange={(e) => { update({ customerOrderNumber: e.target.value }); markDirty() }} />
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
                <TabsList variant="line" className={cn("border-b-0 self-start", compact ? "h-9" : "h-11")}>
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
                          <th className={cn("w-24 border-b px-3 text-right text-muted-foreground font-semibold", compact ? "py-1 text-[10px]" : "py-2 text-[11px]")}>Сумма</th>
                          <th className={cn("w-24 border-b px-3 text-right text-muted-foreground font-semibold", compact ? "py-1 text-[10px]" : "py-2 text-[11px]")}>НДС</th>
                          <th className={cn("w-[100px] border-b px-3 text-left text-muted-foreground font-semibold", compact ? "py-1 text-[10px]" : "py-2 text-[11px]")}>Ставка НДС</th>
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
                    <ScrollBar orientation="horizontal" />
                  </ScrollArea>
                </div>
              </TabsContent>

              <TabsContent value="additional" className="mt-0 overflow-hidden row-start-2 col-start-1">
                <ScrollArea className="h-full">
                <div className="p-4">
                <div>
                  <Label className="text-xs text-muted-foreground">Описание</Label>
                  <Textarea rows={4} className="mt-1" value={f.description} onChange={(e) => { update({ description: e.target.value }); markDirty() }} />
                </div>
                </div>
                </ScrollArea>
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
        >


        </FormSidebar>
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
