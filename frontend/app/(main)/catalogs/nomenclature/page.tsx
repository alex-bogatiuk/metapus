"use client"

import { Badge } from "@/components/ui/badge"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import type { Column } from "@/components/shared/data-table"
import { api } from "@/lib/api"
import type { NomenclatureResponse } from "@/types/catalog"
import { NOMENCLATURE_TYPE_LABELS } from "@/types/catalog"
import type { FilterFieldMeta } from "@/components/shared/filter-config-dialog"

// ── Filters ─────────────────────────────────────────────────────────────

const nomenclatureFieldsMeta: FilterFieldMeta[] = [
  { key: "type", label: "Тип", fieldType: "enum" },
  { key: "code", label: "Код", fieldType: "string" },
  { key: "name", label: "Наименование", fieldType: "string" },
]

// ── Columns ─────────────────────────────────────────────────────────────

const columns: Column<NomenclatureResponse>[] = [
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
    key: "article",
    label: "Артикул",
    sortable: true,
    render: (item) => (
      <span className="font-mono text-xs text-muted-foreground">
        {item.article || "—"}
      </span>
    ),
  },
  {
    key: "type",
    label: "Тип",
    sortable: true,
    render: (item) => (
      <Badge variant="secondary" className="text-[10px]">
        {NOMENCLATURE_TYPE_LABELS[item.type] ?? item.type}
      </Badge>
    ),
  },
  {
    key: "barcode",
    label: "Штрихкод",
    sortable: false,
    render: (item) => (
      <span className="font-mono text-xs text-muted-foreground">
        {item.barcode || "—"}
      </span>
    ),
  },
]

// ── Page ────────────────────────────────────────────────────────────────

export default function NomenclatureListPage() {
  return (
    <CatalogListPage
      config={{
        title: "Номенклатура",
        createHref: "/catalogs/nomenclature/new",
        editHref: (item) => `/catalogs/nomenclature/${item.id}`,
        columns,
        fetcher: api.nomenclature.list,
        limit: 100,
        emptyMessage: "Нет данных. Создайте первый элемент номенклатуры.",
        filterFieldsMeta: nomenclatureFieldsMeta,
        defaultFilterKeys: ["type"],
      }}
    />
  )
}
