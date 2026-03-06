"use client"

import { Badge } from "@/components/ui/badge"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import type { Column } from "@/components/shared/data-table"
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
  return (
    <CatalogListPage
      config={{
        title: "Организации",
        createHref: "/catalogs/organizations/new",
        editHref: (item) => `/catalogs/organizations/${item.id}`,
        columns,
        fetcher: api.organizations.list,
        emptyMessage: "Нет организаций. Создайте первую.",
      }}
    />
  )
}
