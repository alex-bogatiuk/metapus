"use client"

import { Badge } from "@/components/ui/badge"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import type { Column } from "@/components/shared/data-table"
import { api } from "@/lib/api"
import type { NomenclatureResponse } from "@/types/catalog"
import { NOMENCLATURE_TYPE_LABELS } from "@/types/catalog"

// ── Columns ─────────────────────────────────────────────────────────────

const ALL_COLUMNS: Column<NomenclatureResponse>[] = [
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
    key: "weight",
    label: "Вес",
    sortable: false,
    width: 80,
    render: (item) => (
      <span className="font-mono text-xs text-muted-foreground">
        {item.weight || "—"}
      </span>
    ),
  },
  {
    key: "volume",
    label: "Объём",
    sortable: false,
    width: 80,
    render: (item) => (
      <span className="font-mono text-xs text-muted-foreground">
        {item.volume || "—"}
      </span>
    ),
  },
]

const DEFAULT_VISIBLE_KEYS = ["code", "name", "article", "type", "barcode"]

// ── Page ────────────────────────────────────────────────────────────────

export default function NomenclatureListPage() {
  return (
    <CatalogListPage
      config={{
        title: "Номенклатура",
        entityKey: "nomenclature",
        createHref: "/catalogs/nomenclatures/new",
        editHref: (item) => `/catalogs/nomenclatures/${item.id}`,
        columns: ALL_COLUMNS,
        allColumns: ALL_COLUMNS,
        defaultVisibleKeys: DEFAULT_VISIBLE_KEYS,
        fetcher: api.nomenclature.list,
        limit: 100,
        emptyMessage: "Нет данных. Создайте первый элемент номенклатуры.",
        defaultFilterKeys: ["type"],
      }}
    />
  )
}
