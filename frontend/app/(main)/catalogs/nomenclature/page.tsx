"use client"

import { useEffect, useState, useMemo, useCallback } from "react"
import { useRouter } from "next/navigation"
import { Eye, Loader2 } from "lucide-react"
import { DataToolbar } from "@/components/shared/data-toolbar"
import { FilterSidebar } from "@/components/shared/filter-sidebar"
import { DataTable, Column } from "@/components/shared/data-table"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { useListSelection } from "@/hooks/useListSelection"
import { useUrlSort } from "@/hooks/useUrlSort"
import { api } from "@/lib/api"
import type { NomenclatureResponse } from "@/types/catalog"
import { NOMENCLATURE_TYPE_LABELS } from "@/types/catalog"

// ── Filters ─────────────────────────────────────────────────────────────

const nomenclatureFilters = [
  {
    key: "type",
    label: "Тип",
    type: "select" as const,
    options: [
      { value: "all", label: "Все" },
      { value: "goods", label: "Товар" },
      { value: "service", label: "Услуга" },
      { value: "work", label: "Работа" },
      { value: "material", label: "Материал" },
      { value: "semi", label: "Полуфабрикат" },
      { value: "product", label: "Продукция" },
    ],
    defaultValue: "all",
  },
]

// ── Columns ─────────────────────────────────────────────────────────────

const columns: Column<NomenclatureResponse>[] = [
  {
    key: "code",
    label: "Код",
    sortable: true,
    render: (item) => (
      <span className="font-mono text-xs text-muted-foreground">{item.code}</span>
    ),
  },
  {
    key: "name",
    label: "Наименование",
    sortable: true,
    render: (item) => (
      <span className="font-medium text-foreground">{item.name}</span>
    ),
  },
  {
    key: "article",
    label: "Артикул",
    sortable: true,
    render: (item) => (
      <span className="font-mono text-xs text-muted-foreground">
        {item.article || "—"}
      </span>
    ),
  },
  {
    key: "type",
    label: "Тип",
    sortable: true,
    render: (item) => (
      <Badge variant="secondary" className="text-[10px]">
        {NOMENCLATURE_TYPE_LABELS[item.type] ?? item.type}
      </Badge>
    ),
  },
  {
    key: "barcode",
    label: "Штрихкод",
    sortable: false,
    render: (item) => (
      <span className="font-mono text-xs text-muted-foreground">
        {item.barcode || "—"}
      </span>
    ),
  },
]

// ── Page ────────────────────────────────────────────────────────────────

export default function NomenclatureListPage() {
  const router = useRouter()

  const [items, setItems] = useState<NomenclatureResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.nomenclature.list({ limit: 100, offset: 0 })
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

  const visibleIds = useMemo(() => items.map((i) => i.id), [items])

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
        title="Номенклатура"
        onCreateHref="/catalogs/nomenclature/new"
        extraButtons={
          <Button variant="outline" size="sm" onClick={fetchData}>
            <Eye className="mr-1.5 h-3.5 w-3.5" />
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
              Нет данных. Создайте первый элемент номенклатуры.
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
              onRowDoubleClick={(item) =>
                router.push(`/catalogs/nomenclature/${item.id}`)
              }
            />
          )}
        </div>

        <FilterSidebar
          filters={nomenclatureFilters}
          showGroups
          showDetails
        />
      </div>
    </div>
  )
}
