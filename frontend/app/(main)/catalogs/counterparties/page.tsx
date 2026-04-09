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

const ALL_COLUMNS: Column<CounterpartyResponse>[] = [
  {
    key: "code",
    label: "Код",
    sortable: true,
    width: 100,
    render: (item) => (
      <span className="font-mono text-xs text-muted-foreground">{item.code}</span>
    ),
  },
  {
    key: "name",
    label: "Наименование",
    sortable: true,
    width: 280,
    render: (item) => (
      <span className="font-medium text-foreground">{item.name}</span>
    ),
  },
  {
    key: "type",
    label: "Тип",
    sortable: true,
    width: 120,
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
    width: 140,
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
    width: 130,
    render: (item) => (
      <span className="font-mono text-xs text-muted-foreground">{item.inn || "—"}</span>
    ),
  },
  {
    key: "phone",
    label: "Телефон",
    sortable: false,
    width: 140,
    render: (item) => (
      <span className="text-xs text-muted-foreground">{item.phone || "—"}</span>
    ),
  },
  {
    key: "email",
    label: "Email",
    sortable: false,
    width: 180,
    render: (item) => (
      <span className="text-xs text-muted-foreground">{item.email || "—"}</span>
    ),
  },
  {
    key: "contactPerson",
    label: "Контактное лицо",
    sortable: false,
    width: 180,
    render: (item) => (
      <span className="text-xs text-muted-foreground">{item.contactPerson || "—"}</span>
    ),
  },
  {
    key: "fullName",
    label: "Полное наименование",
    sortable: false,
    width: 250,
    render: (item) => (
      <span className="text-xs text-muted-foreground truncate max-w-[250px] block">
        {item.fullName || "—"}
      </span>
    ),
  },
]

const DEFAULT_VISIBLE_KEYS = ["code", "name", "type", "legalForm", "inn", "phone"]

// ── Page ────────────────────────────────────────────────────────────────

export default function CounterpartiesListPage() {
  return (
    <CatalogListPage
      config={{
        title: "Контрагенты",
        entityKey: "counterparty",
        createHref: "/catalogs/counterparties/new",
        editHref: (item) => `/catalogs/counterparties/${item.id}`,
        columns: ALL_COLUMNS,
        allColumns: ALL_COLUMNS,
        defaultVisibleKeys: DEFAULT_VISIBLE_KEYS,
        fetcher: api.counterparties.list,
        emptyMessage: "Нет контрагентов. Создайте первого.",
      }}
    />
  )
}
