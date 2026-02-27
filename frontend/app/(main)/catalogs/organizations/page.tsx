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
import type { OrganizationResponse } from "@/types/catalog"

const columns: Column<OrganizationResponse>[] = [
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
    key: "fullName",
    label: "Полное наименование",
    sortable: false,
    render: (item) => (
      <span className="text-xs text-muted-foreground truncate max-w-[250px] block">
        {item.fullName || "—"}
      </span>
    ),
  },
  {
    key: "inn",
    label: "ИНН",
    sortable: false,
    render: (item) => (
      <span className="font-mono text-xs text-muted-foreground">{item.inn || "—"}</span>
    ),
  },
  {
    key: "isDefault",
    label: "Основная",
    sortable: true,
    render: (item) => (
      item.isDefault
        ? <Badge variant="default" className="text-[10px]">Да</Badge>
        : <span className="text-xs text-muted-foreground">—</span>
    ),
  },
]

export default function OrganizationsListPage() {
  const router = useRouter()
  const [items, setItems] = useState<OrganizationResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.organizations.list({ limit: 200, offset: 0 })
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
        title="Организации"
        onCreateHref="/catalogs/organizations/new"
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
              Нет организаций. Создайте первую.
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
              onRowDoubleClick={(item) => router.push(`/catalogs/organizations/${item.id}`)}
            />
          )}
        </div>
      </div>
    </div>
  )
}
