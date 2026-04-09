"use client"

import { Badge } from "@/components/ui/badge"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import type { Column } from "@/components/shared/data-table"
import { api } from "@/lib/api"
import type { VATRateResponse } from "@/types/catalog"

// ── Columns ─────────────────────────────────────────────────────────────

const ALL_COLUMNS: Column<VATRateResponse>[] = [
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
    key: "rate",
    label: "Ставка",
    sortable: true,
    render: (item) => (
      <span className="font-semibold text-foreground">{item.rate}%</span>
    ),
  },
  {
    key: "isTaxExempt",
    label: "Без НДС",
    sortable: true,
    render: (item) => (
      item.isTaxExempt
        ? <Badge variant="secondary" className="text-[10px]">Да</Badge>
        : <span className="text-xs text-muted-foreground">—</span>
    ),
  },
]

const DEFAULT_VISIBLE_KEYS = ["code", "name", "rate", "isTaxExempt"]

// ── Page ────────────────────────────────────────────────────────────────

export default function VATRatesListPage() {
  return (
    <CatalogListPage
      config={{
        title: "Ставки НДС",
        entityKey: "vat_rate",
        createHref: "/catalogs/vat-rates/new",
        editHref: (item) => `/catalogs/vat-rates/${item.id}`,
        columns: ALL_COLUMNS,
        allColumns: ALL_COLUMNS,
        defaultVisibleKeys: DEFAULT_VISIBLE_KEYS,
        fetcher: api.vatRates.list,
        emptyMessage: "Нет ставок НДС. Создайте первую.",
      }}
    />
  )
}
