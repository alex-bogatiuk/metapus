"use client"

import { Badge } from "@/components/ui/badge"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import type { Column } from "@/components/shared/data-table"
import { api } from "@/lib/api"
import type { WarehouseResponse } from "@/types/catalog"
import { WAREHOUSE_TYPE_LABELS } from "@/types/catalog"

// ── Columns ─────────────────────────────────────────────────────────────

const ALL_COLUMNS: Column<WarehouseResponse>[] = [
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
  {
    key: "description",
    label: "Описание",
    sortable: false,
    render: (item) => (
      <span className="text-xs text-muted-foreground truncate max-w-[200px] block">
        {item.description || "—"}
      </span>
    ),
  },
  {
    key: "allowNegativeStock",
    label: "Отрицательные остатки",
    sortable: false,
    width: 160,
    render: (item) => (
      <Badge variant={item.allowNegativeStock ? "destructive" : "secondary"} className="text-[10px]">
        {item.allowNegativeStock ? "Да" : "Нет"}
      </Badge>
    ),
  },
  {
    key: "isDefault",
    label: "По умолчанию",
    sortable: true,
    width: 120,
    render: (item) => (
      item.isDefault
        ? <Badge variant="default" className="text-[10px]">Да</Badge>
        : <span className="text-xs text-muted-foreground">—</span>
    ),
  },
]

const DEFAULT_VISIBLE_KEYS = ["code", "name", "type", "isActive", "address"]

// ── Page ────────────────────────────────────────────────────────────────

export default function WarehousesListPage() {
  return (
    <CatalogListPage
      config={{
        title: "Склады",
        entityKey: "warehouse",
        createHref: "/catalogs/warehouses/new",
        editHref: (item) => `/catalogs/warehouses/${item.id}`,
        columns: ALL_COLUMNS,
        allColumns: ALL_COLUMNS,
        defaultVisibleKeys: DEFAULT_VISIBLE_KEYS,
        fetcher: api.warehouses.list,
        emptyMessage: "Нет складов. Создайте первый.",
      }}
    />
  )
}
