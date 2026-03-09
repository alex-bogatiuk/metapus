"use client"

import { useEffect, useState, useMemo, useCallback, useRef } from "react"
import { useRouter } from "next/navigation"
import { CircleCheck, Circle, Loader2, Plus, Copy, Pencil, Trash2, CircleCheckBig, CircleOff, Ban } from "lucide-react"
import { DataToolbar } from "@/components/shared/data-toolbar"
import { FilterSidebar } from "@/components/shared/filter-sidebar"
import { DataTable, Column } from "@/components/shared/data-table"
import { DocumentDetailsPanel, type TableSection } from "@/components/shared/document-details-panel"

import { Button } from "@/components/ui/button"
import {
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
} from "@/components/ui/context-menu"
import { useDocumentListPage } from "@/hooks/useDocumentListPage"
import { ScrollSentinel } from "@/components/shared/scroll-sentinel"
import { api } from "@/lib/api"
import { fmtAmount, fmtDate, DEFAULT_DECIMAL_PLACES } from "@/lib/format"
import { toast } from "sonner"
import type { GoodsReceiptResponse } from "@/types/document"

// ── Filters ─────────────────────────────────────────────────────────────

// Default filters shown on page load (keys from fieldsMeta)
const defaultFilterKeys: string[] = []
const PAGE_SIZE = 100
const PREFETCH_ROOT_MARGIN = `0px 0px 2000px 0px`

// ── Columns ─────────────────────────────────────────────────────────────

const columns: Column<GoodsReceiptResponse>[] = [
  {
    key: "date",
    label: "Дата",
    sortable: true,
    render: (doc) => (
      <span className="text-muted-foreground">{fmtDate(doc.date)}</span>
    ),
  },
  {
    key: "number",
    label: "Номер",
    sortable: true,
    render: (doc) => (
      <span className="font-mono text-xs font-medium text-foreground">
        {doc.number}
      </span>
    ),
  },
  {
    key: "supplierId",
    label: "Поставщик",
    sortable: false,
    render: (doc) => (
      <span className="text-xs truncate max-w-[180px] block">
        {doc.supplier?.name || "—"}
      </span>
    ),
  },
  {
    key: "warehouseId",
    label: "Склад",
    sortable: false,
    render: (doc) => (
      <span className="text-xs truncate max-w-[140px] block">
        {doc.warehouse?.name || "—"}
      </span>
    ),
  },
  {
    key: "totalAmount",
    label: "Сумма",
    align: "right",
    sortable: true,
    render: (doc) => (
      <span className="font-mono text-xs text-foreground">{fmtAmount(doc.totalAmount, doc.currency?.decimalPlaces ?? DEFAULT_DECIMAL_PLACES)}</span>
    ),
  },
  {
    key: "totalVat",
    label: "НДС",
    align: "right",
    sortable: true,
    render: (doc) => (
      <span className="font-mono text-xs text-muted-foreground">{fmtAmount(doc.totalVat, doc.currency?.decimalPlaces ?? DEFAULT_DECIMAL_PLACES)}</span>
    ),
  },

  {
    key: "description",
    label: "Описание",
    sortable: false,
    render: (doc) => (
      <span className="text-muted-foreground text-xs truncate max-w-[200px] block">
        {doc.description || "—"}
      </span>
    ),
  },
]

// ── Page ────────────────────────────────────────────────────────────────

export default function GoodsReceiptsListPage() {
  const router = useRouter()
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  const {
    items, loading, loadingMore, error, refresh,
    hasMore, loadMore,
    selectedIds, isAllSelected, isIndeterminate, toggleItem, toggleAll,
    sortColumn, sortDirection, handleSort,
    fieldsMeta, isPrefsLoaded, initialFilterValues, handleFilterValuesChange,
    showDeleted, toggleShowDeleted,
    focusedId, setFocusedId,
    replaceItems,
  } = useDocumentListPage<GoodsReceiptResponse>({
    entityKey: "GoodsReceipt",
    api: api.goodsReceipts,
    periodField: "date",
    limit: PAGE_SIZE,
  })

  // ── Row focus & document preview ────────────────────────────────────
  const [detailDoc, setDetailDoc] = useState<GoodsReceiptResponse | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const sidebarTabRef = useRef("filters")
  const pendingDetailIdRef = useRef<string | null>(null)

  // ── Copy handler ─────────────────────────────────────────────────────
  const handleCopy = useCallback(() => {
    if (focusedId) {
      router.push(`/purchases/goods-receipts/new?copyFrom=${focusedId}`)
    }
  }, [focusedId, router])

  // ── Helpers: re-fetch specific docs & patch in-place ────────────────
  const refetchAndPatch = useCallback(async (ids: string[]) => {
    const results = await Promise.allSettled(
      ids.map((id) => api.goodsReceipts.get(id)),
    )
    const updated = results
      .filter((r): r is PromiseFulfilledResult<GoodsReceiptResponse> => r.status === "fulfilled")
      .map((r) => r.value)
    if (updated.length > 0) replaceItems(updated)
  }, [replaceItems])

  // ── Document actions (batch-aware, point-update) ─────────────────
  const handlePostBatch = useCallback(async (docs: GoodsReceiptResponse[]) => {
    const toPost = docs.filter((d) => !d.posted && !d.deletionMark)
    if (toPost.length === 0) return
    const ids = toPost.map((d) => d.id)
    try {
      await Promise.allSettled(ids.map((id) => api.goodsReceipts.post(id)))
      await refetchAndPatch(ids)
      if (toPost.length > 1) toast.success(`Проведено: ${toPost.length}`)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Ошибка проведения")
    }
  }, [refetchAndPatch])

  const handleUnpostBatch = useCallback(async (docs: GoodsReceiptResponse[]) => {
    const toUnpost = docs.filter((d) => d.posted)
    if (toUnpost.length === 0) return
    const ids = toUnpost.map((d) => d.id)
    try {
      await Promise.allSettled(ids.map((id) => api.goodsReceipts.unpost(id)))
      await refetchAndPatch(ids)
      if (toUnpost.length > 1) toast.success(`Отменено проведение: ${toUnpost.length}`)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Ошибка отмены проведения")
    }
  }, [refetchAndPatch])

  const handleToggleDeletionMarkBatch = useCallback(async (docs: GoodsReceiptResponse[], mark: boolean) => {
    const ids = docs.map((d) => d.id)
    try {
      await Promise.allSettled(
        ids.map((id) => api.goodsReceipts.setDeletionMark(id, { marked: mark })),
      )
      await refetchAndPatch(ids)
      if (docs.length > 1) toast.success(mark ? `Помечено на удаление: ${docs.length}` : `Снято пометок: ${docs.length}`)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Ошибка")
    }
  }, [refetchAndPatch])

  // ── Keyboard shortcuts: F9 = copy, Delete = toggle deletion mark ────
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "F9") {
        e.preventDefault()
        if (focusedId) handleCopy()
      }
      if (e.key === "Delete") {
        e.preventDefault()
        // Batch-aware: if items are selected, toggle for all selected;
        // otherwise toggle for the focused row only.
        const targets = selectedIds.length > 0
          ? items.filter((d) => selectedIds.includes(d.id))
          : items.filter((d) => d.id === focusedId)
        if (targets.length === 0) return
        const shouldMark = targets.some((d) => !d.deletionMark)
        handleToggleDeletionMarkBatch(targets, shouldMark)
      }
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [focusedId, handleCopy, items, selectedIds, handleToggleDeletionMarkBatch])

  const fetchDetail = useCallback((id: string) => {
    pendingDetailIdRef.current = null
    setDetailLoading(true)
    api.goodsReceipts.get(id)
      .then((full) => setDetailDoc(full))
      .catch(() => setDetailDoc(null))
      .finally(() => setDetailLoading(false))
  }, [])

  const handleRowClick = useCallback((doc: GoodsReceiptResponse) => {
    setFocusedId(doc.id)
    if (sidebarTabRef.current === "details") {
      fetchDetail(doc.id)
    } else {
      pendingDetailIdRef.current = doc.id
    }
  }, [setFocusedId, fetchDetail])

  const handleSidebarTabChange = useCallback((tab: string) => {
    sidebarTabRef.current = tab
    if (tab === "details" && pendingDetailIdRef.current) {
      fetchDetail(pendingDetailIdRef.current)
    }
  }, [fetchDetail])

  const detailsContent = useMemo(() => {
    if (!focusedId) return undefined

    if (detailLoading || !detailDoc) {
      return (
        <DocumentDetailsPanel
          title=""
          loading={detailLoading}
        />
      )
    }

    const headerFields = [
      { label: "Поставщик", value: detailDoc.supplier?.name || "—" },
      { label: "Склад", value: detailDoc.warehouse?.name || "—" },
      { label: "Организация", value: detailDoc.organization?.name || "—" },
      ...(detailDoc.description
        ? [{ label: "Комментарий", value: detailDoc.description }]
        : []),
      { label: "Сумма", value: fmtAmount(detailDoc.totalAmount, detailDoc.currency?.decimalPlaces ?? DEFAULT_DECIMAL_PLACES) },
      { label: "НДС", value: fmtAmount(detailDoc.totalVat, detailDoc.currency?.decimalPlaces ?? DEFAULT_DECIMAL_PLACES) },
    ]

    const sections: TableSection[] = [
      {
        title: "Товары",
        columns: [
          { key: "product", label: "Номенклатура" },
          { key: "quantity", label: "Кол-во", align: "right" as const },
          { key: "amount", label: "Сумма", align: "right" as const },
        ],
        rows: (detailDoc.lines ?? []).map((line) => ({
          product: line.product?.name || "—",
          quantity: String(line.quantity),
          amount: fmtAmount(line.amount, detailDoc.currency?.decimalPlaces ?? DEFAULT_DECIMAL_PLACES),
        })),
        defaultOpen: true,
      },
    ]

    return (
      <DocumentDetailsPanel
        title={`Приходная накладная ${detailDoc.number} от ${fmtDate(detailDoc.date)}`}
        headerFields={headerFields}
        sections={sections}
      />
    )
  }, [focusedId, detailDoc, detailLoading])

  return (
    <div className="flex h-full flex-col">

      <DataToolbar
        title="Приходные накладные"
        onCreateHref="/purchases/goods-receipts/new"
        onCopyClick={focusedId ? handleCopy : null}
        extraButtons={
          <Button variant="outline" size="sm" onClick={refresh}>
            Обновить
          </Button>
        }
        menuItems={[
          {
            label: "Помеченные на удаление",
            checked: showDeleted,
            onClick: toggleShowDeleted,
          },
        ]}
      />

      <div className="flex flex-1 overflow-hidden">
        <div ref={scrollContainerRef} className="flex-1 overflow-auto">
          {loading ? (
            <div className="flex items-center justify-center py-20 text-muted-foreground">
              <Loader2 className="mr-2 h-5 w-5 animate-spin" />
              Загрузка…
            </div>
          ) : error ? (
            <div className="flex flex-col items-center justify-center gap-2 py-20 text-destructive">
              <p>{error}</p>
              <Button variant="outline" size="sm" onClick={refresh}>
                Повторить
              </Button>
            </div>
          ) : items.length === 0 ? (
            <div className="flex items-center justify-center py-20 text-muted-foreground">
              Нет документов. Создайте первую приходную накладную.
            </div>
          ) : (
            <DataTable
              data={items}
              columns={columns}
              selectedIds={selectedIds}
              isAllSelected={isAllSelected}
              isIndeterminate={isIndeterminate}
              onToggleAll={toggleAll}
              onToggleItem={toggleItem}
              sortColumn={sortColumn}
              sortDirection={sortDirection}
              onSort={handleSort}
              focusedId={focusedId}
              onRowClick={handleRowClick}
              onRowDoubleClick={(doc) =>
                router.push(`/purchases/goods-receipts/${doc.id}`)
              }
              renderPrefix={(doc) =>
                doc.deletionMark ? (
                  <Ban className="h-4 w-4 text-destructive" />
                ) : doc.posted ? (
                  <CircleCheck className="h-4 w-4 text-success" />
                ) : (
                  <Circle className="h-4 w-4 text-muted-foreground" />
                )
              }
              rowClassName={(doc) =>
                doc.deletionMark ? "opacity-60 line-through decoration-destructive/40" : undefined
              }
              renderContextMenu={(doc, targets) => {
                const isBatch = targets.length > 1
                const suffix = isBatch ? ` (${targets.length})` : ""
                const hasUnposted = targets.some((d) => !d.posted && !d.deletionMark)
                const hasPosted = targets.some((d) => d.posted)
                const hasUnmarked = targets.some((d) => !d.deletionMark)
                const hasMarked = targets.some((d) => d.deletionMark)

                return (
                  <>
                    <ContextMenuItem onClick={() => router.push("/purchases/goods-receipts/new")}>
                      <Plus className="mr-2 h-4 w-4" />
                      Создать
                      <ContextMenuShortcut>Ins</ContextMenuShortcut>
                    </ContextMenuItem>
                    {!isBatch && (
                      <ContextMenuItem onClick={() => router.push(`/purchases/goods-receipts/new?copyFrom=${doc.id}`)}>
                        <Copy className="mr-2 h-4 w-4" />
                        Скопировать
                        <ContextMenuShortcut>F9</ContextMenuShortcut>
                      </ContextMenuItem>
                    )}
                    {!isBatch && (
                      <ContextMenuItem onClick={() => router.push(`/purchases/goods-receipts/${doc.id}`)}>
                        <Pencil className="mr-2 h-4 w-4" />
                        Изменить
                        <ContextMenuShortcut>F2</ContextMenuShortcut>
                      </ContextMenuItem>
                    )}
                    <ContextMenuSeparator />
                    {hasUnmarked && (
                      <ContextMenuItem onClick={() => handleToggleDeletionMarkBatch(targets.filter((d) => !d.deletionMark), true)}>
                        <Trash2 className="mr-2 h-4 w-4" />
                        Пометить на удаление{suffix}
                        {!isBatch && <ContextMenuShortcut>Del</ContextMenuShortcut>}
                      </ContextMenuItem>
                    )}
                    {hasMarked && (
                      <ContextMenuItem onClick={() => handleToggleDeletionMarkBatch(targets.filter((d) => d.deletionMark), false)}>
                        <Trash2 className="mr-2 h-4 w-4" />
                        Снять пометку удаления{suffix}
                        {!isBatch && <ContextMenuShortcut>Del</ContextMenuShortcut>}
                      </ContextMenuItem>
                    )}
                    <ContextMenuSeparator />
                    {hasUnposted && (
                      <ContextMenuItem onClick={() => handlePostBatch(targets)}>
                        <CircleCheckBig className="mr-2 h-4 w-4" />
                        Провести{suffix}
                      </ContextMenuItem>
                    )}
                    {hasPosted && (
                      <ContextMenuItem onClick={() => handleUnpostBatch(targets)}>
                        <CircleOff className="mr-2 h-4 w-4" />
                        Отменить проведение{suffix}
                      </ContextMenuItem>
                    )}
                  </>
                )
              }}
            />
          )}
          <ScrollSentinel
            onIntersect={loadMore}
            loading={loadingMore}
            enabled={hasMore}
            rootMargin={PREFETCH_ROOT_MARGIN}
            scrollContainer={scrollContainerRef}
          />
        </div>

        {isPrefsLoaded && (
          <FilterSidebar
            key="goods-receipt-filters"
            showGroups={false}
            showDetails
            fieldsMeta={fieldsMeta}
            defaultSelectedKeys={defaultFilterKeys}
            periodField="date"
            onFilterValuesChange={handleFilterValuesChange}
            initialFilterValues={initialFilterValues}
            detailsContent={detailsContent}
            onActiveTabChange={handleSidebarTabChange}
          />
        )}
      </div>
    </div >
  )
}
