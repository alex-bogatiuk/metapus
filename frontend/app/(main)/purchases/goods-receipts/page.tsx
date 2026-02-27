"use client"

import { useEffect, useState, useMemo, useCallback } from "react"
import { useRouter } from "next/navigation"
import { CircleCheck, Circle, Loader2 } from "lucide-react"
import { DataToolbar } from "@/components/shared/data-toolbar"
import { FilterSidebar } from "@/components/shared/filter-sidebar"
import { DataTable, Column } from "@/components/shared/data-table"
import type { FilterFieldMeta } from "@/components/shared/filter-config-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useListSelection } from "@/hooks/useListSelection"
import { useUrlSort } from "@/hooks/useUrlSort"
import { api } from "@/lib/api"
import type { GoodsReceiptResponse } from "@/types/document"

// ── Helpers ─────────────────────────────────────────────────────────────

/** Format MinorUnits (kopecks) to display string with 2 decimals. */
function formatAmount(minor: number): string {
  return (minor / 100).toLocaleString("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

/** Format ISO date string to dd.mm.yyyy. */
function formatDate(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit", year: "numeric" })
}

// ── Filters ─────────────────────────────────────────────────────────────

const docFilters = [
  {
    key: "period",
    label: "Период",
    type: "date-range" as const,
  },
  {
    key: "posted",
    label: "Статус",
    type: "select" as const,
    options: [
      { value: "all", label: "Все" },
      { value: "posted", label: "Проведён" },
      { value: "draft", label: "Черновик" },
    ],
    defaultValue: "all",
  },
]

// ── Document field metadata (for filter configuration dialog) ───────────

const goodsReceiptFieldsMeta: FilterFieldMeta[] = [
  { key: "number", label: "Номер", fieldType: "string" },
  { key: "date", label: "Дата", fieldType: "date" },
  { key: "incomingNumber", label: "№ вх. документа", fieldType: "string" },
  { key: "supplierId", label: "Поставщик", fieldType: "reference" },
  { key: "warehouseId", label: "Склад", fieldType: "reference" },
  { key: "posted", label: "Проведен", fieldType: "boolean" },
  { key: "deletionMark", label: "Пометка удаления", fieldType: "boolean" },
  { key: "lines.productId", label: "Номенклатура", fieldType: "reference", group: "Товары" },
  { key: "lines.quantity", label: "Количество", fieldType: "number", group: "Товары" },
  { key: "lines.unitPrice", label: "Цена", fieldType: "number", group: "Товары" },
  { key: "lines.amount", label: "Сумма", fieldType: "number", group: "Товары" },
]

// ── Columns ─────────────────────────────────────────────────────────────

const columns: Column<GoodsReceiptResponse>[] = [
  {
    key: "date",
    label: "Дата",
    sortable: true,
    render: (doc) => (
      <span className="text-muted-foreground">{formatDate(doc.date)}</span>
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
    key: "totalAmount",
    label: "Сумма",
    align: "right",
    sortable: true,
    render: (doc) => (
      <span className="font-mono text-xs text-foreground">{formatAmount(doc.totalAmount)}</span>
    ),
  },
  {
    key: "totalVat",
    label: "НДС",
    align: "right",
    sortable: true,
    render: (doc) => (
      <span className="font-mono text-xs text-muted-foreground">{formatAmount(doc.totalVat)}</span>
    ),
  },
  {
    key: "posted",
    label: "Статус",
    sortable: true,
    render: (doc) => (
      <Badge variant={doc.posted ? "default" : "secondary"} className="text-[10px]">
        {doc.posted ? "Проведён" : "Черновик"}
      </Badge>
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

  const [items, setItems] = useState<GoodsReceiptResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.goodsReceipts.list({ limit: 100, offset: 0 })
      setItems(res.items ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка загрузки данных")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  const visibleIds = useMemo(() => items.map((d) => d.id), [items])

  const {
    selectedIds,
    isAllSelected,
    isIndeterminate,
    toggleItem,
    toggleAll,
  } = useListSelection(visibleIds)

  const { sortColumn, sortDirection, handleSort } = useUrlSort()

  return (
    <div className="flex h-full flex-col">
      <div className="border-b bg-card px-4 py-1">
        <Tabs defaultValue="receipts">
          <TabsList className="h-8">
            <TabsTrigger value="receipts" className="text-xs">
              Приходные накладные
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      <DataToolbar
        title="Приходные накладные"
        onCreateHref="/purchases/goods-receipts/new"
        extraButtons={
          <Button variant="outline" size="sm" onClick={fetchData}>
            Обновить
          </Button>
        }
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
              <Button variant="outline" size="sm" onClick={fetchData}>
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
              onRowDoubleClick={(doc) =>
                router.push(`/purchases/goods-receipts/${doc.id}`)
              }
              renderPrefix={(doc) =>
                doc.posted ? (
                  <CircleCheck className="h-4 w-4 text-success" />
                ) : (
                  <Circle className="h-4 w-4 text-muted-foreground" />
                )
              }
            />
          )}
        </div>

        <FilterSidebar
          filters={docFilters}
          showGroups={false}
          showDetails
          fieldsMeta={goodsReceiptFieldsMeta}
          onFilterConfigChange={(keys) => {
            console.log("Selected filter keys:", keys)
          }}
        />
      </div>
    </div>
  )
}
