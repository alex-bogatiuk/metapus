"use client"

import { Badge } from "@/components/ui/badge"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import type { Column } from "@/components/shared/data-table"
import { api } from "@/lib/api"
import type { UnitResponse } from "@/types/catalog"
import { useEnumFormatter } from "@/hooks/useEntityFiltersMeta"
import { useMemo } from "react"

// ── Columns ─────────────────────────────────────────────────────────────

const ALL_COLUMNS = (formatEnum: (k: string, v: string) => string): Column<UnitResponse>[] => [
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
        {formatEnum("type", item.type)}
      </Badge>
    ),
  },
  {
    key: "symbol",
    label: "Символ",
    sortable: true,
    render: (item) => (
      <span className="text-foreground font-semibold">{item.symbol}</span>
    ),
  },
  {
    key: "internationalCode",
    label: "Код ОКЕИ",
    sortable: true,
    render: (item) => (
      <span className="text-xs text-muted-foreground">{item.internationalCode || "—"}</span>
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
    key: "conversionFactor",
    label: "Коэффициент",
    sortable: true,
    render: (item) => (
      <span className="font-mono text-xs">{item.conversionFactor}</span>
    ),
  },
]

const DEFAULT_VISIBLE_KEYS = ["code", "name", "type", "symbol", "isBase"]

// ── Page ────────────────────────────────────────────────────────────────

export default function UnitsListPage() {
  const formatEnum = useEnumFormatter("Unit")
  const columns = useMemo(() => ALL_COLUMNS(formatEnum), [formatEnum])

  return (
    <CatalogListPage
      config={{
        title: "Единицы измерения",
        entityKey: "unit",
        createHref: "/catalogs/units/new",
        editHref: (item) => `/catalogs/units/${item.id}`,
        columns: columns,
        allColumns: columns,
        defaultVisibleKeys: DEFAULT_VISIBLE_KEYS,
        fetcher: api.units.list,
        emptyMessage: "Нет единиц измерения. Создайте первую.",
      }}
    />
  )
}
