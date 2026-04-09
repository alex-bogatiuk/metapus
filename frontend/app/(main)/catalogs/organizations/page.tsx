"use client"

import { Badge } from "@/components/ui/badge"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import type { Column } from "@/components/shared/data-table"
import { api } from "@/lib/api"
import type { OrganizationResponse } from "@/types/catalog"

// ── Columns ─────────────────────────────────────────────────────────────

const ALL_COLUMNS: Column<OrganizationResponse>[] = [
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
    key: "kpp",
    label: "КПП",
    sortable: false,
    width: 120,
    render: (item) => (
      <span className="font-mono text-xs text-muted-foreground">{item.kpp || "—"}</span>
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

const DEFAULT_VISIBLE_KEYS = ["code", "name", "fullName", "inn", "isDefault"]

// ── Page ────────────────────────────────────────────────────────────────

export default function OrganizationsListPage() {
  return (
    <CatalogListPage
      config={{
        title: "Организации",
        entityKey: "organization",
        createHref: "/catalogs/organizations/new",
        editHref: (item) => `/catalogs/organizations/${item.id}`,
        columns: ALL_COLUMNS,
        allColumns: ALL_COLUMNS,
        defaultVisibleKeys: DEFAULT_VISIBLE_KEYS,
        fetcher: api.organizations.list,
        emptyMessage: "Нет организаций. Создайте первую.",
      }}
    />
  )
}
