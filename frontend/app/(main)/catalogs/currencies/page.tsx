"use client"

import { Badge } from "@/components/ui/badge"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import type { Column } from "@/components/shared/data-table"
import { api } from "@/lib/api"
import type { CurrencyResponse } from "@/types/catalog"

// ── Columns ─────────────────────────────────────────────────────────────

const ALL_COLUMNS: Column<CurrencyResponse>[] = [
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
    key: "isoCode",
    label: "ISO Код",
    sortable: true,
    render: (item) => (
      <span className="font-semibold text-foreground">{item.isoCode || "—"}</span>
    ),
  },
  {
    key: "isoNumericCode",
    label: "Цифровой код",
    sortable: true,
    render: (item) => (
      <span className="text-xs text-muted-foreground">{item.isoNumericCode || "—"}</span>
    ),
  },
  {
    key: "symbol",
    label: "Символ",
    sortable: true,
    render: (item) => (
      <span className="font-semibold">{item.symbol || "—"}</span>
    ),
  },
  {
    key: "isBase",
    label: "Базовая",
    sortable: true,
    render: (item) => (
      item.isBase
        ? <Badge variant="default" className="text-[10px]">Да</Badge>
        : <span className="text-xs text-muted-foreground">—</span>
    ),
  },
  {
    key: "country",
    label: "Страна",
    sortable: true,
    render: (item) => (
      <span className="text-xs text-muted-foreground">{item.country || "—"}</span>
    ),
  },
]

const DEFAULT_VISIBLE_KEYS = ["code", "name", "isoCode", "symbol", "isBase"]

// ── Page ────────────────────────────────────────────────────────────────

export default function CurrenciesListPage() {
  return (
    <CatalogListPage
      config={{
        title: "Валюты",
        entityKey: "currency",
        createHref: "/catalogs/currencies/new",
        editHref: (item) => `/catalogs/currencies/${item.id}`,
        columns: ALL_COLUMNS,
        allColumns: ALL_COLUMNS,
        defaultVisibleKeys: DEFAULT_VISIBLE_KEYS,
        fetcher: api.currencies.list,
        emptyMessage: "Нет валют. Создайте первую.",
      }}
    />
  )
}
