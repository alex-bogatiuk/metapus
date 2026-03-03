"use client"

import { useEffect, useState, useMemo, useCallback, useRef } from "react"
import { useRouter } from "next/navigation"
import { CircleCheck, Circle, Loader2 } from "lucide-react"
import { DataToolbar } from "@/components/shared/data-toolbar"
import { FilterSidebar } from "@/components/shared/filter-sidebar"
import { DataTable, Column } from "@/components/shared/data-table"

import { Button } from "@/components/ui/button"
import { useListSelection } from "@/hooks/useListSelection"
import { useUrlSort } from "@/hooks/useUrlSort"
import { useEntityFiltersMeta } from "@/hooks/useEntityFiltersMeta"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { api } from "@/lib/api"
import { buildFilterItems, type FilterValues } from "@/lib/filter-utils"
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

// Default filters shown on page load (keys from fieldsMeta)
const defaultFilterKeys: string[] = []

// ── Document field metadata — fetched dynamically from backend ──────────
// The backend metadata registry (GET /api/v1/meta/GoodsReceipt/filters)
// is the single source of truth for the document structure.
// When a new field is added to the Go struct, only the backend label map
// needs updating — the frontend adapts automatically.

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

  // Fetch filter field metadata from backend (single source of truth)
  const { fieldsMeta } = useEntityFiltersMeta("GoodsReceipt")

  const [items, setItems] = useState<GoodsReceiptResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Track current filter values for API calls
  const filterValuesRef = useRef<FilterValues>({})

  const isPrefsLoaded = useUserPrefsStore((s) => s.isLoaded)
  const initialListFilters = useUserPrefsStore((s) => s.listFilters["GoodsReceipt"])
  const setListFilters = useUserPrefsStore((s) => s.setListFilters)

  const fetchData = useCallback(async (filterValues?: FilterValues) => {
    setLoading(true)
    setError(null)
    try {
      // Build advanced filter items from sidebar values
      const advancedFilters = filterValues
        ? buildFilterItems(filterValues, fieldsMeta, "date")
        : []

      const res = await api.goodsReceipts.list({
        limit: 100,
        offset: 0,
        filter: advancedFilters.length > 0 ? advancedFilters : undefined,
      })
      setItems(res.items ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка загрузки данных")
    } finally {
      setLoading(false)
    }
  }, [fieldsMeta])

  const initialized = useRef(false)

  useEffect(() => {
    if (isPrefsLoaded && !initialized.current) {
      initialized.current = true
      const initial = initialListFilters ?? {}
      filterValuesRef.current = initial
      fetchData(initial)
    }
    // Explicitly run only when `isPrefsLoaded` becomes true to avoid constant refetching
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isPrefsLoaded, fetchData])

  const handleFilterValuesChange = useCallback(
    (values: FilterValues) => {
      filterValuesRef.current = values
      setListFilters("GoodsReceipt", values)
      fetchData(values)
    },
    [fetchData, setListFilters]
  )

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

      <DataToolbar
        title="Приходные накладные"
        onCreateHref="/purchases/goods-receipts/new"
        extraButtons={
          <Button variant="outline" size="sm" onClick={() => fetchData(filterValuesRef.current)}>
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
              <Button variant="outline" size="sm" onClick={() => fetchData(filterValuesRef.current)}>
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

        {isPrefsLoaded && (
          <FilterSidebar
            key="goods-receipt-filters"
            showGroups={false}
            showDetails
            fieldsMeta={fieldsMeta}
            defaultSelectedKeys={defaultFilterKeys}
            periodField="date"
            onFilterValuesChange={handleFilterValuesChange}
            initialFilterValues={initialListFilters ?? {}}
          />
        )}
      </div>
    </div >
  )
}
