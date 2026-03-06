"use client"

import React from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { DataToolbar } from "@/components/shared/data-toolbar"
import { DataTable, type Column } from "@/components/shared/data-table"
import { FilterSidebar } from "@/components/shared/filter-sidebar"
import { ListContent } from "@/components/shared/list-content"
import { useListPage } from "@/hooks/useListPage"
import type { ListParams, ListResponse } from "@/types/common"
import type { FilterFieldMeta } from "@/components/shared/filter-config-dialog"

// ── Config ──────────────────────────────────────────────────────────────

export interface CatalogListPageConfig<T extends { id: string }> {
  /** Page title shown in toolbar. */
  title: string
  /** Route for creating a new entity. */
  createHref: string
  /** Route for editing an entity. */
  editHref: (item: T) => string
  /** Column definitions for DataTable. */
  columns: Column<T>[]
  /** API fetcher function (e.g. api.warehouses.list). */
  fetcher: (params?: ListParams) => Promise<ListResponse<T>>
  /** Empty state message. */
  emptyMessage?: string
  /** Max items per fetch. Default 200. */
  limit?: number
  /** Optional filter field metadata for FilterSidebar. */
  filterFieldsMeta?: FilterFieldMeta[]
  /** Default selected filter keys. */
  defaultFilterKeys?: string[]
  /** Whether to show filter groups tab. Default true. */
  showFilterGroups?: boolean
  /** Whether to show filter details tab. Default true. */
  showFilterDetails?: boolean
}

// ── Component ───────────────────────────────────────────────────────────

export function CatalogListPage<T extends { id: string }>({
  config,
}: {
  config: CatalogListPageConfig<T>
}) {
  const router = useRouter()
  const {
    items,
    loading,
    error,
    refresh,
    selection,
    sortColumn,
    sortDirection,
    handleSort,
  } = useListPage({ fetcher: config.fetcher, limit: config.limit })

  const { selectedIds, isAllSelected, isIndeterminate, toggleItem, toggleAll } = selection

  return (
    <div className="flex h-full flex-col">
      <DataToolbar
        title={config.title}
        onCreateHref={config.createHref}
        extraButtons={
          <Button variant="outline" size="sm" onClick={refresh}>
            Обновить
          </Button>
        }
      />

      <div className="flex flex-1 overflow-hidden">
        <div className="flex-1 overflow-auto">
          <ListContent
            loading={loading}
            error={error}
            isEmpty={items.length === 0}
            onRetry={refresh}
            emptyMessage={config.emptyMessage ?? "Нет данных."}
          >
            <DataTable
              data={items}
              columns={config.columns}
              selectedIds={selectedIds}
              isAllSelected={isAllSelected}
              isIndeterminate={isIndeterminate}
              onToggleAll={toggleAll}
              onToggleItem={toggleItem}
              sortColumn={sortColumn}
              sortDirection={sortDirection}
              onSort={handleSort}
              onRowDoubleClick={(item) => router.push(config.editHref(item))}
            />
          </ListContent>
        </div>

        {config.filterFieldsMeta && (
          <FilterSidebar
            fieldsMeta={config.filterFieldsMeta}
            defaultSelectedKeys={config.defaultFilterKeys}
            showGroups={config.showFilterGroups ?? true}
            showDetails={config.showFilterDetails ?? true}
          />
        )}
      </div>
    </div>
  )
}
