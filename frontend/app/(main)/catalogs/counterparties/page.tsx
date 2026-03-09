"use client"

import { Badge } from "@/components/ui/badge"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import type { Column } from "@/components/shared/data-table"
import { api } from "@/lib/api"
import type { CounterpartyResponse } from "@/types/catalog"
import {
  COUNTERPARTY_TYPE_LABELS as TYPE_LABELS,
  LEGAL_FORM_LABELS as FORM_LABELS,
} from "@/types/catalog"

// ── Columns ─────────────────────────────────────────────────────────────

const columns: Column<CounterpartyResponse>[] = [
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
        {TYPE_LABELS[item.type] ?? item.type}
      </Badge>
    ),
  },
  {
    key: "legalForm",
    label: "Правовая форма",
    sortable: true,
    render: (item) => (
      <span className="text-muted-foreground text-xs">
        {FORM_LABELS[item.legalForm] ?? item.legalForm}
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
    key: "phone",
    label: "Телефон",
    sortable: false,
    render: (item) => (
      <span className="text-xs text-muted-foreground">{item.phone || "—"}</span>
    ),
  },
]

// ── Page ────────────────────────────────────────────────────────────────

export default function CounterpartiesListPage() {
  return (
    <CatalogListPage
      config={{
        title: "Контрагенты",
        createHref: "/catalogs/counterparties/new",
        editHref: (item) => `/catalogs/counterparties/${item.id}`,
        columns,
        fetcher: api.counterparties.list,
        emptyMessage: "Нет контрагентов. Создайте первого.",
      }}
    />
  )
}
