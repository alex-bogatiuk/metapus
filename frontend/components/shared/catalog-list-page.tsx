"use client"

import React, { useMemo, useCallback, useRef, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { DataToolbar } from "@/components/shared/data-toolbar"
import { DataTable, type Column } from "@/components/shared/data-table"
import { ColumnChooserPopover } from "@/components/shared/column-chooser-popover"
import { FilterSidebar } from "@/components/shared/filter-sidebar"
import { ListContent } from "@/components/shared/list-content"
import { ScrollArea } from "@/components/ui/scroll-area"
import { ScrollSentinel } from "@/components/shared/scroll-sentinel"
import { useEntityListPage } from "@/hooks/useEntityListPage"
import { useColumnResize } from "@/hooks/useColumnResize"
import { useVisibleColumns } from "@/hooks/useVisibleColumns"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import type { CursorListParams, CursorListResponse } from "@/types/common"
import { useMetadataStore } from "@/stores/useMetadataStore"

// ── Config ──────────────────────────────────────────────────────────────

export interface CatalogListPageConfig<T extends { id: string }> {
  /** Page title shown in toolbar. If entityKey is provided, resolved from metadata store. */
  title: string
  /** Entity key for metadata-driven label resolution and filter metadata (e.g. "nomenclature"). */
  entityKey: string
  /** Route for creating a new entity. */
  createHref: string
  /** Route for editing an entity. */
  editHref: (item: T) => string
  /** Column definitions for DataTable. */
  columns: Column<T>[]
  /** API fetcher function (e.g. api.warehouses.list). */
  fetcher: (params?: CursorListParams) => Promise<CursorListResponse<T>>
  /** Empty state message. */
  emptyMessage?: string
  /** Max items per fetch. Default 100. */
  limit?: number
  /** Default selected filter keys. */
  defaultFilterKeys?: string[]
  /** Period field key for filter sidebar (omit for catalogs without date filter). */
  periodField?: string
  /** Full column registry (all possible columns). When provided, enables Column Chooser. */
  allColumns?: Column<T>[]
  /** Default visible column keys. Required when allColumns is provided. */
  defaultVisibleKeys?: string[]
}

// ── Component ───────────────────────────────────────────────────────────

export function CatalogListPage<T extends { id: string }>({
  config,
}: {
  config: CatalogListPageConfig<T>
}) {
  const router = useRouter()
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  const {
    items,
    loading,
    loadingMore,
    error,
    refresh,
    hasMore,
    loadMore,
    selectedIds,
    isAllSelected,
    isIndeterminate,
    toggleItem,
    toggleAll,
    sortColumn,
    sortDirection,
    handleSort,
    fieldsMeta,
    isPrefsLoaded,
    initialFilterValues,
    handleFilterValuesChange,
    showDeleted,
    toggleShowDeleted,
    focusedId,
  } = useEntityListPage<T>({
    entityKey: config.entityKey,
    api: { list: config.fetcher },
    periodField: config.periodField,
    limit: config.limit ?? 100,
  })

  const metaLabel = useMetadataStore((s) => s.getLabel(config.entityKey, "plural"))
  const title = metaLabel || config.title

  // ── Column Chooser (opt-in) ───────────────────────────────────────────
  const hasColumnChooser = !!config.allColumns && !!config.defaultVisibleKeys
  const colChooser = useVisibleColumns({
    entityKey: config.entityKey,
    allColumns: config.allColumns ?? config.columns,
    defaultVisibleKeys: config.defaultVisibleKeys ?? config.columns.map((c) => c.key),
  })
  const effectiveColumns = hasColumnChooser ? colChooser.visibleColumns : config.columns
  const [columnChooserOpen, setColumnChooserOpen] = useState(false)

  // ── Column resize with persistence ────────────────────────────────────
  const storedWidths = useUserPrefsStore((s) => s.getListColumnWidths(config.entityKey))
  const setListColumnWidths = useUserPrefsStore((s) => s.setListColumnWidths)

  const resizeDefs = useMemo(
    () => effectiveColumns.map((col) => ({ key: col.key, width: col.width, minWidth: col.minWidth })),
    [effectiveColumns]
  )

  const handleWidthsChange = useCallback(
    (widths: Record<string, number>) => setListColumnWidths(config.entityKey, widths),
    [config.entityKey, setListColumnWidths]
  )

  const { colWidths, onResizeStart, isResizing } = useColumnResize({
    columns: resizeDefs,
    storedWidths,
    onWidthsChange: handleWidthsChange,
  })

  return (
    <div className="flex h-full flex-col">
      <DataToolbar
        title={title}
        onCreateHref={config.createHref}
        extraButtons={
          <Button variant="outline" size="sm" onClick={refresh}>
            Обновить
          </Button>
        }
        menuItems={[
          {
            label: "Помеченные на удаление",
            checked: showDeleted,
            onClick: toggleShowDeleted,
          },
        ]}
        onColumnChooserClick={hasColumnChooser ? () => setColumnChooserOpen(true) : undefined}
      />

      <div className="flex flex-1 overflow-hidden">
        <ScrollArea className="flex-1" viewportRef={scrollContainerRef}>
          <ListContent
            loading={loading}
            error={error}
            isEmpty={items.length === 0}
            onRetry={refresh}
            emptyMessage={config.emptyMessage ?? "Список пуст."}
          >
            <DataTable
              data={items}
              columns={effectiveColumns}
              selectedIds={selectedIds}
              isAllSelected={isAllSelected}
              isIndeterminate={isIndeterminate}
              onToggleAll={toggleAll}
              onToggleItem={toggleItem}
              sortColumn={sortColumn}
              sortDirection={sortDirection}
              onSort={handleSort}
              focusedId={focusedId}
              onRowDoubleClick={(item) => router.push(config.editHref(item))}
              colWidths={colWidths}
              onResizeStart={onResizeStart}
              isResizing={isResizing}
            />
          </ListContent>
          <ScrollSentinel
            onIntersect={loadMore}
            loading={loadingMore}
            enabled={hasMore}
            rootMargin="0px 0px 2000px 0px"
            scrollContainer={scrollContainerRef}
          />
        </ScrollArea>

        {isPrefsLoaded && fieldsMeta.length > 0 && (
          <FilterSidebar
            key={`${config.entityKey}-filters`}
            showGroups={false}
            showDetails={false}
            fieldsMeta={fieldsMeta}
            defaultSelectedKeys={config.defaultFilterKeys}
            periodField={config.periodField}
            onFilterValuesChange={handleFilterValuesChange}
            initialFilterValues={initialFilterValues}
          />
        )}
      </div>

      {/* Column Chooser */}
      {hasColumnChooser && (
        <ColumnChooserPopover
          allColumns={colChooser.orderedAllColumns}
          visibleKeys={colChooser.visibleKeys}
          onToggle={colChooser.toggleColumn}
          onReorder={colChooser.reorderColumns}
          onReset={colChooser.resetColumns}
          open={columnChooserOpen}
          onOpenChange={setColumnChooserOpen}
        />
      )}
    </div>
  )
}
