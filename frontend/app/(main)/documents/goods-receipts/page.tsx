"use client"

import { useState, useMemo, useCallback, useRef } from "react"
import { useRouter } from "next/navigation"
import { CircleCheck, Circle, Plus, Copy, Pencil, Trash2, CircleCheckBig, CircleOff, Ban } from "lucide-react"
import { DataTableSkeleton } from "@/components/shared/data-table-skeleton"
import { DataToolbar } from "@/components/shared/data-toolbar"
import { FilterSidebar } from "@/components/shared/filter-sidebar"
import { DataTable, Column } from "@/components/shared/data-table"
import { ColumnChooserPopover } from "@/components/shared/column-chooser-popover"
import { DocumentDetailsPanel, type TableSection } from "@/components/shared/document-details-panel"
import { SelectAllBanner } from "@/components/shared/select-all-banner"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useDocumentBatchActions } from "@/hooks/useDocumentBatchActions"
import { useShortcut } from "@/hooks/useShortcut"
import { useScrollRestore } from "@/hooks/useScrollRestore"

import { Button } from "@/components/ui/button"
import {
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
} from "@/components/ui/context-menu"
import { useEntityListPage } from "@/hooks/useEntityListPage"
import { useColumnResize } from "@/hooks/useColumnResize"
import { useVisibleColumns } from "@/hooks/useVisibleColumns"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { ScrollSentinel } from "@/components/shared/scroll-sentinel"
import { api } from "@/lib/api"
import { fmtAmount, fmtDate, DEFAULT_DECIMAL_PLACES } from "@/lib/format"
import type { GoodsReceiptResponse } from "@/types/document"
import { useMetadataStore } from "@/stores/useMetadataStore"

// ── Filters ─────────────────────────────────────────────────────────────

// Default filters shown on page load (keys from fieldsMeta)
const defaultFilterKeys: string[] = []
const PAGE_SIZE = 100
const PREFETCH_ROOT_MARGIN = `0px 0px 2000px 0px`

// ── Columns ─────────────────────────────────────────────────────────────

const ALL_COLUMNS: Column<GoodsReceiptResponse>[] = [
  {
    key: "date",
    label: "Дата",
    sortable: true,
    width: 120,
    render: (doc) => (
      <span className="text-muted-foreground">{fmtDate(doc.date)}</span>
    ),
  },
  {
    key: "number",
    label: "Номер",
    sortable: true,
    width: 100,
    render: (doc) => (
      <span className="font-mono text-xs font-medium text-foreground">
        {doc.number}
      </span>
    ),
  },
  {
    key: "counterpartyId",
    label: "Поставщик",
    sortable: false,
    width: 200,
    render: (doc) => (
      <span className="text-xs">
        {doc.counterparty?.name || "—"}
      </span>
    ),
  },
  {
    key: "warehouseId",
    label: "Склад",
    sortable: false,
    width: 160,
    render: (doc) => (
      <span className="text-xs">
        {doc.warehouse?.name || "—"}
      </span>
    ),
  },
  {
    key: "organizationId",
    label: "Организация",
    sortable: false,
    width: 180,
    render: (doc) => (
      <span className="text-xs">
        {doc.organization?.name || "—"}
      </span>
    ),
  },
  {
    key: "currencyId",
    label: "Валюта",
    sortable: false,
    width: 80,
    render: (doc) => (
      <span className="text-xs text-muted-foreground">
        {doc.currency?.symbol || doc.currency?.name || "—"}
      </span>
    ),
  },
  {
    key: "totalAmount",
    label: "Сумма",
    align: "right",
    sortable: true,
    width: 120,
    render: (doc) => (
      <span className="font-mono text-xs text-foreground">{fmtAmount(doc.totalAmount, doc.currency?.decimalPlaces ?? DEFAULT_DECIMAL_PLACES)}</span>
    ),
  },
  {
    key: "totalVat",
    label: "НДС",
    align: "right",
    sortable: true,
    width: 100,
    render: (doc) => (
      <span className="font-mono text-xs text-muted-foreground">{fmtAmount(doc.totalVat, doc.currency?.decimalPlaces ?? DEFAULT_DECIMAL_PLACES)}</span>
    ),
  },

  {
    key: "description",
    label: "Описание",
    sortable: false,
    render: (doc) => (
      <span className="text-muted-foreground text-xs">
        {doc.description || "—"}
      </span>
    ),
  },
]

const DEFAULT_VISIBLE_KEYS = ["date", "number", "counterpartyId", "warehouseId", "totalAmount", "totalVat", "description"]

// ── Page ────────────────────────────────────────────────────────────────

export default function GoodsReceiptsListPage() {
  const router = useRouter()
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  const {
    items, loading, loadingMore, error, refresh,
    hasMore, loadMore, totalCount,
    selectedIds, isAllSelected, isIndeterminate, toggleItem, toggleAll,
    selectAllByFilter, excludedIds, activateSelectAll, clearSelection,
    sortColumn, sortDirection, handleSort,
    fieldsMeta, isPrefsLoaded, initialFilterValues, handleFilterValuesChange,
    currentFilters,
    showDeleted, toggleShowDeleted,
    focusedId, setFocusedId,
    replaceItems,
    searchQuery, setSearchQuery,
  } = useEntityListPage<GoodsReceiptResponse>({
    entityKey: "GoodsReceipt",
    api: api.goodsReceipts,
    periodField: "date",
    limit: PAGE_SIZE,
  })

  // ── Column Chooser ─────────────────────────────────────────────────────
  const { visibleColumns, orderedAllColumns, visibleKeys, toggleColumn, reorderColumns, resetColumns } = useVisibleColumns({
    entityKey: "GoodsReceipt",
    allColumns: ALL_COLUMNS,
    defaultVisibleKeys: DEFAULT_VISIBLE_KEYS,
  })
  const [columnChooserOpen, setColumnChooserOpen] = useState(false)

  // ── Column resize with persistence ──────────────────────────────────
  const storedWidths = useUserPrefsStore((s) => s.getListColumnWidths("GoodsReceipt"))
  const setListColumnWidths = useUserPrefsStore((s) => s.setListColumnWidths)

  const resizeDefs = useMemo(
    () => visibleColumns.map((col) => ({ key: col.key, width: col.width, minWidth: col.minWidth })),
    [visibleColumns]
  )

  const handleWidthsChange = useCallback(
    (widths: Record<string, number>) => setListColumnWidths("GoodsReceipt", widths),
    [setListColumnWidths]
  )

  const { colWidths, onResizeStart, isResizing } = useColumnResize({
    columns: resizeDefs,
    storedWidths,
    onWidthsChange: handleWidthsChange,
  })

  // ── Row focus & document preview ────────────────────────────────────
  const [detailDoc, setDetailDoc] = useState<GoodsReceiptResponse | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const sidebarTabRef = useRef("filters")
  const pendingDetailIdRef = useRef<string | null>(null)

  // ── Copy handler ─────────────────────────────────────────────────────
  const handleCopy = useCallback(() => {
    const targetId = focusedId ?? (selectedIds.length === 1 ? selectedIds[0] : null)
    if (targetId) {
      router.push(`/documents/goods-receipts/new?copyFrom=${targetId}`)
    }
  }, [focusedId, selectedIds, router])

  // ── Batch document actions (shared hook) ──────────────────────────────
  const {
    handlePostBatch, handleUnpostBatch, handleToggleDeletionMarkBatch,
    getBatchMenuCounts,
  } = useDocumentBatchActions({
    api: api.goodsReceipts,
    replaceItems,
    refresh,
    items,
    selectedIds,
    focusedId,
    selectAllByFilter,
    excludedIds,
    currentFilters,
    showDeleted,
    clearSelection,
  })

  // ── Keyboard shortcuts (centralized via useShortcut) ────────────────────
  const handleDeleteMark = useCallback(() => {
    const targets = selectedIds.length > 0
      ? items.filter((d) => selectedIds.includes(d.id))
      : items.filter((d) => d.id === focusedId)
    if (targets.length === 0) return
    const shouldMark = targets.some((d) => !d.deletionMark)
    handleToggleDeletionMarkBatch(targets, shouldMark)
  }, [focusedId, items, selectedIds, handleToggleDeletionMarkBatch])

  useShortcut("list.copy", "f9", "Копировать", "list", handleCopy)
  useShortcut("list.delete", "delete", "Пометить на удаление", "list", handleDeleteMark)

  // ── M5 Search: Ctrl+F → focus search input ──────────────────────────
  const searchInputRef = useRef<HTMLInputElement | null>(null)
  useShortcut("list.search", "ctrl+f", "Поиск", "list", () => {
    searchInputRef.current?.focus()
    searchInputRef.current?.select()
  })

  // ── M2 Scroll restoration on tab switch ─────────────────────────────
  useScrollRestore(scrollContainerRef)

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
      { label: "Поставщик", value: detailDoc.counterparty?.name || "—" },
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
          product: line.nomenclature?.name || "—",
          quantity: String(line.quantity),
          amount: fmtAmount(line.amount, detailDoc.currency?.decimalPlaces ?? DEFAULT_DECIMAL_PLACES),
        })),
        defaultOpen: true,
      },
    ]

    return (
      <DocumentDetailsPanel
        title={`${useMetadataStore.getState().getLabel("goods_receipt", "singular")} ${detailDoc.number} от ${fmtDate(detailDoc.date)}`}
        headerFields={headerFields}
        sections={sections}
      />
    )
  }, [focusedId, detailDoc, detailLoading])

  return (
    <div className="flex h-full flex-col">

      <DataToolbar
        title={useMetadataStore.getState().getLabel("goods_receipt", "plural")}
        onCreateHref="/documents/goods-receipts/new"
        onCopyClick={(focusedId || selectedIds.length === 1) ? handleCopy : null}
        searchValue={searchQuery}
        onSearchChange={setSearchQuery}
        searchInputRef={(el) => { searchInputRef.current = el }}
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
        onColumnChooserClick={() => setColumnChooserOpen(true)}
      />

      <div className="flex flex-1 overflow-hidden">
        <ScrollArea className="flex-1" viewportRef={scrollContainerRef}>
          {loading ? (
            <DataTableSkeleton showToolbar={false} showPrefix />
          ) : error ? (
            <div className="flex flex-col items-center justify-center gap-2 py-20 text-destructive">
              <p>{error}</p>
              <Button variant="outline" size="sm" onClick={refresh}>
                Повторить
              </Button>
            </div>
          ) : items.length === 0 ? (
            <div className="flex items-center justify-center py-20 text-muted-foreground">
              Нет документов. Создайте первое поступление товара.
            </div>
          ) : (
            <div className="animate-skeleton-fade-in">
            <SelectAllBanner
              selectedCount={selectedIds.length}
              totalCount={totalCount}
              selectAllByFilter={selectAllByFilter}
              excludedCount={excludedIds.length}
              onSelectAll={activateSelectAll}
              onClearAll={clearSelection}
            />
            <DataTable
              data={items}
              columns={visibleColumns}
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
                router.push(`/documents/goods-receipts/${doc.id}`)
              }
              colWidths={colWidths}
              onResizeStart={onResizeStart}
              isResizing={isResizing}
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
                const isBatch = selectAllByFilter || targets.length > 1
                const { postableCount, unpostableCount, markableCount, unmarkeableCount } = getBatchMenuCounts(targets)
                // In virtual select-all mode, show total count (server resolves actual IDs)
                const virtualTotal = totalCount - excludedIds.length
                const fmtCount = (n: number) =>
                  selectAllByFilter
                    ? ` (${virtualTotal.toLocaleString("ru-RU")})`
                    : isBatch ? ` (${n})` : ""

                return (
                  <>
                    <ContextMenuItem onClick={() => router.push("/documents/goods-receipts/new")}>
                      <Plus className="mr-2 h-4 w-4" />
                      Создать
                      <ContextMenuShortcut>Ins</ContextMenuShortcut>
                    </ContextMenuItem>
                    {!isBatch && (
                      <ContextMenuItem onClick={() => router.push(`/documents/goods-receipts/new?copyFrom=${doc.id}`)}>
                        <Copy className="mr-2 h-4 w-4" />
                        Скопировать
                        <ContextMenuShortcut>F9</ContextMenuShortcut>
                      </ContextMenuItem>
                    )}
                    {!isBatch && (
                      <ContextMenuItem onClick={() => router.push(`/documents/goods-receipts/${doc.id}`)}>
                        <Pencil className="mr-2 h-4 w-4" />
                        Изменить
                        <ContextMenuShortcut>F2</ContextMenuShortcut>
                      </ContextMenuItem>
                    )}
                    <ContextMenuSeparator />
                    {markableCount > 0 && (
                      <ContextMenuItem onClick={() => handleToggleDeletionMarkBatch(targets.filter((d) => !d.deletionMark), true)}>
                        <Trash2 className="mr-2 h-4 w-4" />
                        Пометить на удаление{fmtCount(markableCount)}
                        {!isBatch && <ContextMenuShortcut>Del</ContextMenuShortcut>}
                      </ContextMenuItem>
                    )}
                    {unmarkeableCount > 0 && (
                      <ContextMenuItem onClick={() => handleToggleDeletionMarkBatch(targets.filter((d) => d.deletionMark), false)}>
                        <Trash2 className="mr-2 h-4 w-4" />
                        Снять пометку удаления{fmtCount(unmarkeableCount)}
                        {!isBatch && <ContextMenuShortcut>Del</ContextMenuShortcut>}
                      </ContextMenuItem>
                    )}
                    <ContextMenuSeparator />
                    {postableCount > 0 && (
                      <ContextMenuItem onClick={() => handlePostBatch(targets)}>
                        <CircleCheckBig className="mr-2 h-4 w-4" />
                        Провести{fmtCount(postableCount)}
                      </ContextMenuItem>
                    )}
                    {unpostableCount > 0 && (
                      <ContextMenuItem onClick={() => handleUnpostBatch(targets)}>
                        <CircleOff className="mr-2 h-4 w-4" />
                        Отменить проведение{fmtCount(unpostableCount)}
                      </ContextMenuItem>
                    )}
                  </>
                )
              }}
            />
            </div>
          )}
          <ScrollSentinel
            onIntersect={loadMore}
            loading={loadingMore}
            enabled={hasMore}
            rootMargin={PREFETCH_ROOT_MARGIN}
            scrollContainer={scrollContainerRef}
          />
        </ScrollArea>

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

      {/* Column Chooser */}
      <ColumnChooserPopover
        allColumns={orderedAllColumns}
        visibleKeys={visibleKeys}
        onToggle={toggleColumn}
        onReorder={reorderColumns}
        onReset={resetColumns}
        open={columnChooserOpen}
        onOpenChange={setColumnChooserOpen}
      />
    </div >
  )
}
