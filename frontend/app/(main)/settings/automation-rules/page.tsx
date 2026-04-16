"use client"

import { Badge } from "@/components/ui/badge"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import type { Column } from "@/components/shared/data-table"
import { api } from "@/lib/api"
import type { AutomationRule } from "@/types/automation"

// ── Columns ─────────────────────────────────────────────────────────────

const ALL_COLUMNS: Column<AutomationRule>[] = [
  {
    key: "name",
    label: "Наименование",
    sortable: true,
    render: (item) => (
      <span className="font-medium text-foreground">{item.name}</span>
    ),
  },
  {
    key: "eventType",
    label: "Событие",
    sortable: true,
    render: (item) => (
      <span className="text-xs text-muted-foreground">{item.eventType}</span>
    ),
  },
  {
    key: "actionType",
    label: "Действие",
    sortable: true,
    render: (item) => (
      <span className="text-xs text-muted-foreground">{item.actionType}</span>
    ),
  },
  {
    key: "isActive",
    label: "Статус",
    sortable: true,
    render: (item) => (
      <Badge variant={item.isActive ? "default" : "secondary"} className="text-[10px]">
        {item.isActive ? "Активно" : "Отключено"}
      </Badge>
    ),
  },
  {
    key: "createdAt",
    label: "Создано",
    sortable: true,
    render: (item) => (
      <span className="text-xs text-muted-foreground">
        {item.createdAt ? new Date(item.createdAt).toLocaleString() : "—"}
      </span>
    ),
  },
]

const DEFAULT_VISIBLE_KEYS = ["name", "eventType", "actionType", "isActive", "createdAt"]

// ── Page ────────────────────────────────────────────────────────────────

export default function AutomationRulesListPage() {
  return (
    <CatalogListPage
      config={{
        title: "Правила автоматизации",
        entityKey: "automation_rule",
        createHref: "/settings/automation-rules/new",
        editHref: (item) => `/settings/automation-rules/${item.id}`,
        columns: ALL_COLUMNS,
        allColumns: ALL_COLUMNS,
        defaultVisibleKeys: DEFAULT_VISIBLE_KEYS,
        fetcher: api.automationRules.list,
        emptyMessage: "Нет правил автоматизации. Создайте первое правило.",
      }}
    />
  )
}
