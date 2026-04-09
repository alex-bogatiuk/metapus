"use client"

import { Badge } from "@/components/ui/badge"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import type { Column } from "@/components/shared/data-table"
import { api } from "@/lib/api"
import type { ContractResponse } from "@/types/catalog"
import { CONTRACT_TYPE_LABELS } from "@/types/catalog"

// ── Columns ─────────────────────────────────────────────────────────────

const ALL_COLUMNS: Column<ContractResponse>[] = [
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
        {CONTRACT_TYPE_LABELS[item.type] ?? item.type}
      </Badge>
    ),
  },
  {
    key: "validFrom",
    label: "Действует с",
    sortable: true,
    render: (item) => (
      <span className="text-xs text-muted-foreground">
        {item.validFrom ? new Date(item.validFrom).toLocaleDateString() : "—"}
      </span>
    ),
  },
  {
    key: "validTo",
    label: "Действует по",
    sortable: true,
    render: (item) => (
      <span className="text-xs text-muted-foreground">
        {item.validTo ? new Date(item.validTo).toLocaleDateString() : "—"}
      </span>
    ),
  },
  {
    key: "paymentTermDays",
    label: "Срок оплаты (дн.)",
    sortable: true,
    render: (item) => (
      <span className="text-xs">{item.paymentTermDays}</span>
    ),
  },
]

const DEFAULT_VISIBLE_KEYS = ["code", "name", "type", "validFrom", "validTo"]

// ── Page ────────────────────────────────────────────────────────────────

export default function ContractsListPage() {
  return (
    <CatalogListPage
      config={{
        title: "Договоры",
        entityKey: "contract",
        createHref: "/catalogs/contracts/new",
        editHref: (item) => `/catalogs/contracts/${item.id}`,
        columns: ALL_COLUMNS,
        allColumns: ALL_COLUMNS,
        defaultVisibleKeys: DEFAULT_VISIBLE_KEYS,
        fetcher: api.contracts.list,
        emptyMessage: "Нет договоров. Создайте первый.",
      }}
    />
  )
}
