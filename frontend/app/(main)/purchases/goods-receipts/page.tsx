"use client"

import { useEffect, useState, useMemo, useCallback } from "react"
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
import { api } from "@/lib/api"
import { fmtAmount, fmtDate, DEFAULT_DECIMAL_PLACES } from "@/lib/format"
import { toast } from "sonner"
import type { GoodsReceiptResponse } from "@/types/document"

// ── Filters ─────────────────────────────────────────────────────────────

// Default filters shown on page load (keys from fieldsMeta)
const defaultFilterKeys: string[] = []

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

  const {
    items, loading, error, refresh,
    selectedIds, isAllSelected, isIndeterminate, toggleItem, toggleAll,
    sortColumn, sortDirection, handleSort,
    fieldsMeta, isPrefsLoaded, initialFilterValues, handleFilterValuesChange,
    showDeleted, toggleShowDeleted,
    focusedId, setFocusedId,
  } = useDocumentListPage<GoodsReceiptResponse>({
    entityKey: "GoodsReceipt",
    api: api.goodsReceipts,
    periodField: "date",
    limit: 100,
  })

  // ── Row focus & document preview ────────────────────────────────────
  const [detailDoc, setDetailDoc] = useState<GoodsReceiptResponse | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  // ── Copy handler ─────────────────────────────────────────────────────
  const handleCopy = useCallback(() => {
    if (focusedId) {
      router.push(`/purchases/goods-receipts/new?copyFrom=${focusedId}`)
    }
  }, [focusedId, router])

  // ── Document actions (for context menu) ──────────────────────────
  const handlePost = useCallback(async (id: string) => {
    try {
      await api.goodsReceipts.post(id)
      refresh()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Ошибка проведения")
    }
  }, [refresh])

  const handleUnpost = useCallback(async (id: string) => {
    try {
      await api.goodsReceipts.unpost(id)
      refresh()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Ошибка отмены проведения")
    }
  }, [refresh])

  const handleToggleDeletionMark = useCallback(async (doc: GoodsReceiptResponse) => {
    try {
      await api.goodsReceipts.setDeletionMark(doc.id, { marked: !doc.deletionMark })
      refresh()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Ошибка")
    }
  }, [refresh])

  // ── Keyboard shortcuts: F9 = copy, Delete = toggle deletion mark ────
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "F9") {
        e.preventDefault()
        if (focusedId) handleCopy()
      }
      if (e.key === "Delete") {
        e.preventDefault()
        const doc = items.find((d) => d.id === focusedId)
        if (doc) handleToggleDeletionMark(doc)
      }
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [focusedId, handleCopy, items, handleToggleDeletionMark])

  const handleRowClick = useCallback((doc: GoodsReceiptResponse) => {
    setFocusedId(doc.id)
    setDetailLoading(true)
    api.goodsReceipts.get(doc.id)
      .then((full) => setDetailDoc(full))
      .catch(() => setDetailDoc(null))
      .finally(() => setDetailLoading(false))
  }, [setFocusedId])

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
        <div className="flex-1 overflow-auto">
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
              renderContextMenu={(doc) => (
                <>
                  <ContextMenuItem onClick={() => router.push("/purchases/goods-receipts/new")}>
                    <Plus className="mr-2 h-4 w-4" />
                    Создать
                    <ContextMenuShortcut>Ins</ContextMenuShortcut>
                  </ContextMenuItem>
                  <ContextMenuItem onClick={() => router.push(`/purchases/goods-receipts/new?copyFrom=${doc.id}`)}>
                    <Copy className="mr-2 h-4 w-4" />
                    Скопировать
                    <ContextMenuShortcut>F9</ContextMenuShortcut>
                  </ContextMenuItem>
                  <ContextMenuItem onClick={() => router.push(`/purchases/goods-receipts/${doc.id}`)}>
                    <Pencil className="mr-2 h-4 w-4" />
                    Изменить
                    <ContextMenuShortcut>F2</ContextMenuShortcut>
                  </ContextMenuItem>
                  <ContextMenuSeparator />
                  <ContextMenuItem onClick={() => handleToggleDeletionMark(doc)}>
                    <Trash2 className="mr-2 h-4 w-4" />
                    {doc.deletionMark ? "Снять пометку удаления" : "Пометить на удаление"}
                    <ContextMenuShortcut>Del</ContextMenuShortcut>
                  </ContextMenuItem>
                  <ContextMenuSeparator />
                  {doc.posted ? (
                    <ContextMenuItem onClick={() => handleUnpost(doc.id)}>
                      <CircleOff className="mr-2 h-4 w-4" />
                      Отменить проведение
                    </ContextMenuItem>
                  ) : (
                    <ContextMenuItem onClick={() => handlePost(doc.id)}>
                      <CircleCheckBig className="mr-2 h-4 w-4" />
                      Провести
                    </ContextMenuItem>
                  )}
                </>
              )}
            />
          )}
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
          />
        )}
      </div>
    </div >
  )
}
