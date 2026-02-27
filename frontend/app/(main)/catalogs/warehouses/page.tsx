"use client"

import { useEffect, useState, useMemo, useCallback } from "react"
import { useRouter } from "next/navigation"
import { Loader2 } from "lucide-react"
import { DataToolbar } from "@/components/shared/data-toolbar"
import { DataTable, Column } from "@/components/shared/data-table"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useListSelection } from "@/hooks/useListSelection"
import { useUrlSort } from "@/hooks/useUrlSort"
import { api } from "@/lib/api"
import type { WarehouseResponse } from "@/types/catalog"
import { WAREHOUSE_TYPE_LABELS } from "@/types/catalog"

const columns: Column<WarehouseResponse>[] = [
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
    key: "type",
    label: "Тип",
    sortable: true,
    render: (item) => (
      <Badge variant="outline" className="text-[10px]">
        {WAREHOUSE_TYPE_LABELS[item.type] ?? item.type}
      </Badge>
    ),
  },
  {
    key: "isActive",
    label: "Активен",
    sortable: true,
    render: (item) => (
      <Badge variant={item.isActive ? "default" : "secondary"} className="text-[10px]">
        {item.isActive ? "Да" : "Нет"}
      </Badge>
    ),
  },
  {
    key: "address",
    label: "Адрес",
    sortable: false,
    render: (item) => (
      <span className="text-xs text-muted-foreground truncate max-w-[200px] block">
        {item.address || "—"}
      </span>
    ),
  },
]

export default function WarehousesListPage() {
  const router = useRouter()
  const [items, setItems] = useState<WarehouseResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.warehouses.list({ limit: 200, offset: 0 })
      setItems(res.items ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : "Ошибка загрузки")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const visibleIds = useMemo(() => items.map((d) => d.id), [items])
  const { selectedIds, isAllSelected, isIndeterminate, toggleItem, toggleAll } = useListSelection(visibleIds)
  const { sortColumn, sortDirection, handleSort } = useUrlSort()

  return (
    <div className="flex h-full flex-col">
      <DataToolbar
        title="Склады"
        onCreateHref="/catalogs/warehouses/new"
        extraButtons={
          <Button variant="outline" size="sm" onClick={fetchData}>Обновить</Button>
        }
      />
      <div className="flex flex-1 overflow-hidden">
        <div className="flex-1 overflow-auto">
          {loading ? (
            <div className="flex items-center justify-center py-20 text-muted-foreground">
              <Loader2 className="mr-2 h-5 w-5 animate-spin" />Загрузка…
            </div>
          ) : error ? (
            <div className="flex flex-col items-center justify-center gap-2 py-20 text-destructive">
              <p>{error}</p>
              <Button variant="outline" size="sm" onClick={fetchData}>Повторить</Button>
            </div>
          ) : items.length === 0 ? (
            <div className="flex items-center justify-center py-20 text-muted-foreground">
              Нет складов. Создайте первый.
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
              onRowDoubleClick={(item) => router.push(`/catalogs/warehouses/${item.id}`)}
            />
          )}
        </div>
      </div>
    </div>
  )
}
