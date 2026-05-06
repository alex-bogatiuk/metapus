"use client"

import React, { useMemo, useCallback, useRef, useState } from "react"
import { useRouter } from "next/navigation"
import { Plus, Copy, Pencil, Trash2, CircleCheckBig, CircleOff } from "lucide-react"
import { Button } from "@/components/ui/button"
import { DataToolbar } from "@/components/shared/data-toolbar"
import { DataTable, type Column } from "@/components/shared/data-table"
import { ColumnChooserPopover } from "@/components/shared/column-chooser-popover"
import { FilterSidebar } from "@/components/shared/filter-sidebar"
import { ListContent } from "@/components/shared/list-content"
import { SelectAllBanner } from "@/components/shared/select-all-banner"
import { ScrollArea } from "@/components/ui/scroll-area"
import { ScrollSentinel } from "@/components/shared/scroll-sentinel"
import {
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
} from "@/components/ui/context-menu"
import { useEntityListPage } from "@/hooks/useEntityListPage"
import { useDocumentBatchActions } from "@/hooks/useDocumentBatchActions"
import { useListViews } from "@/hooks/useListViews"
import { useColumnResize } from "@/hooks/useColumnResize"
import { useVisibleColumns } from "@/hooks/useVisibleColumns"
import { useListExport, type ExportColumn } from "@/hooks/useListExport"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { useShortcut } from "@/hooks/useShortcut"
import { useScrollRestore } from "@/hooks/useScrollRestore"
import type { CursorListParams, CursorListResponse, AdvancedFilterItem, BatchActionResponse, BatchActionType, BatchActionByFilterRequest } from "@/types/common"
import type { ListViewConfig } from "@/types/list-view"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { apiFetch, buildListQS } from "@/lib/api"

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
  /** API base path for export (e.g. "/catalog/nomenclatures"). If omitted, export is disabled. */
  basePath?: string
  /** Optional row prefix renderer (e.g., posted/deletionMark icons for documents). */
  renderPrefix?: (item: T) => React.ReactNode
  /** Optional row className (e.g., opacity+strikethrough for deletion-marked documents). */
  rowClassName?: (item: T) => string | undefined
  /** Optional context menu renderer — enables right-click menus on rows (custom override). */
  renderContextMenu?: (item: T, targets: T[]) => React.ReactNode
  /** Optional row click handler — enables row focus highlighting (custom override). */
  onRowClick?: (item: T) => void
  /** Optional copy button handler — enables "Copy" toolbar button (custom override). */
  onCopyClick?: (() => void) | null
  /**
   * Document mode: enables batch actions, context menu, keyboard shortcuts,
   * select-all banner, and row focus — matching the goods-receipts reference.
   * When set, `renderContextMenu`, `onRowClick`, `onCopyClick` are auto-generated.
   */
  documentMode?: {
    /** API base path for batch operations (e.g. "/document/crypto-invoice"). */
    basePath: string
  }
}

// ── Component ───────────────────────────────────────────────────────────

/** Minimal document shape for batch actions (documents always have these fields). */
interface DocumentLike {
  id: string
  posted: boolean
  deletionMark: boolean
}

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
    setFocusedId,
    searchQuery,
    setSearchQuery,
    currentFilters,
    totalCount,
    selectAllByFilter,
    excludedIds,
    activateSelectAll,
    clearSelection,
    replaceItems,
  } = useEntityListPage<T>({
    entityKey: config.entityKey,
    api: { list: config.fetcher },
    periodField: config.periodField,
    limit: config.limit ?? 100,
  })

  // ── Document mode: batch actions + context menu ──────────────────────
  const isDocumentMode = !!config.documentMode
  const docBasePath = config.documentMode?.basePath ?? ""

  // Build a batch-capable API adapter from basePath (generic — works for any document entity)
  const batchApi = useMemo(() => {
    if (!isDocumentMode) return null
    return {
      get: (id: string) => apiFetch<DocumentLike>(`${docBasePath}/${id}`),
      list: (params?: { limit?: number; filter?: AdvancedFilterItem[]; includeDeleted?: boolean }) =>
        apiFetch<{ items: DocumentLike[] }>(`${docBasePath}${buildListQS(params)}`),
      batchAction: (ids: string[], action: BatchActionType) =>
        apiFetch<BatchActionResponse>(`${docBasePath}/batch-action`, {
          method: "POST",
          body: JSON.stringify({ ids, action }),
        }),
      batchActionByFilter: (req: BatchActionByFilterRequest) =>
        apiFetch<BatchActionResponse>(`${docBasePath}/batch-action-by-filter`, {
          method: "POST",
          body: JSON.stringify(req),
        }),
      _basePath: docBasePath,
    }
  }, [isDocumentMode, docBasePath])

  // Cast items to DocumentLike for batch actions (safe: backend guarantees posted/deletionMark for documents)
  const docItems = items as unknown as DocumentLike[]

  const _noopResponse: BatchActionResponse = { results: [], total: 0, success: 0, failed: 0 }
  const batchActions = useDocumentBatchActions<DocumentLike>({
    api: batchApi ?? { get: async () => ({} as DocumentLike), batchAction: async () => _noopResponse, batchActionByFilter: async () => _noopResponse, _basePath: "" },
    replaceItems: replaceItems as unknown as (updated: DocumentLike[]) => void,
    refresh,
    items: docItems,
    selectedIds,
    focusedId,
    selectAllByFilter,
    excludedIds,
    currentFilters,
    showDeleted,
    clearSelection,
  })

  // ── Document mode: row click handler ───────────────────────────────
  const handleDocRowClick = useCallback((item: T) => {
    setFocusedId(item.id)
  }, [setFocusedId])

  // ── Document mode: copy handler ───────────────────────────────────
  const handleDocCopy = useCallback(() => {
    const targetId = focusedId ?? (selectedIds.length === 1 ? selectedIds[0] : null)
    if (targetId && config.editHref) {
      // Derive the "new" page from editHref pattern
      const editUrl = config.editHref({ id: targetId } as T)
      const listBase = editUrl.substring(0, editUrl.lastIndexOf("/"))
      router.push(`${listBase}/new?copyFrom=${targetId}`)
    }
  }, [focusedId, selectedIds, config, router])

  // ── Document mode: deletion mark handler ──────────────────────────
  const handleDocDeleteMark = useCallback(() => {
    if (!isDocumentMode) return
    const targets = selectedIds.length > 0
      ? docItems.filter((d) => selectedIds.includes(d.id))
      : docItems.filter((d) => d.id === focusedId)
    if (targets.length === 0) return
    const shouldMark = targets.some((d) => !d.deletionMark)
    batchActions.handleToggleDeletionMarkBatch(targets, shouldMark)
  }, [isDocumentMode, focusedId, docItems, selectedIds, batchActions])

  // ── Document mode: keyboard shortcuts ──────────────────────────────
  useShortcut("list.copy", "f9", "Копировать", "list", isDocumentMode ? handleDocCopy : () => {})
  useShortcut("list.delete", "delete", "Пометить на удаление", "list", isDocumentMode ? handleDocDeleteMark : () => {})

  // ── Document mode: context menu ───────────────────────────────────
  const renderDocContextMenu = useCallback((item: T, targets: T[]) => {
    if (!isDocumentMode) return null
    const docTargets = targets as unknown as DocumentLike[]
    const isBatch = selectAllByFilter || targets.length > 1
    const { postableCount, unpostableCount, markableCount, unmarkeableCount } = batchActions.getBatchMenuCounts(docTargets)
    const virtualTotal = totalCount - excludedIds.length
    const fmtCount = (n: number) =>
      selectAllByFilter
        ? ` (${virtualTotal.toLocaleString("ru-RU")})`
        : isBatch ? ` (${n})` : ""

    return (
      <>
        <ContextMenuItem onClick={() => router.push(config.createHref)}>
          <Plus className="mr-2 h-4 w-4" />
          Создать
          <ContextMenuShortcut>Ins</ContextMenuShortcut>
        </ContextMenuItem>
        {!isBatch && (
          <ContextMenuItem onClick={() => {
            const editUrl = config.editHref(item)
            const listBase = editUrl.substring(0, editUrl.lastIndexOf("/"))
            router.push(`${listBase}/new?copyFrom=${item.id}`)
          }}>
            <Copy className="mr-2 h-4 w-4" />
            Скопировать
            <ContextMenuShortcut>F9</ContextMenuShortcut>
          </ContextMenuItem>
        )}
        {!isBatch && (
          <ContextMenuItem onClick={() => router.push(config.editHref(item))}>
            <Pencil className="mr-2 h-4 w-4" />
            Изменить
            <ContextMenuShortcut>F2</ContextMenuShortcut>
          </ContextMenuItem>
        )}
        <ContextMenuSeparator />
        {markableCount > 0 && (
          <ContextMenuItem onClick={() => batchActions.handleToggleDeletionMarkBatch(docTargets.filter((d) => !d.deletionMark), true)}>
            <Trash2 className="mr-2 h-4 w-4" />
            Пометить на удаление{fmtCount(markableCount)}
            {!isBatch && <ContextMenuShortcut>Del</ContextMenuShortcut>}
          </ContextMenuItem>
        )}
        {unmarkeableCount > 0 && (
          <ContextMenuItem onClick={() => batchActions.handleToggleDeletionMarkBatch(docTargets.filter((d) => d.deletionMark), false)}>
            <Trash2 className="mr-2 h-4 w-4" />
            Снять пометку удаления{fmtCount(unmarkeableCount)}
            {!isBatch && <ContextMenuShortcut>Del</ContextMenuShortcut>}
          </ContextMenuItem>
        )}
        <ContextMenuSeparator />
        {postableCount > 0 && (
          <ContextMenuItem onClick={() => batchActions.handlePostBatch(docTargets)}>
            <CircleCheckBig className="mr-2 h-4 w-4" />
            Провести{fmtCount(postableCount)}
          </ContextMenuItem>
        )}
        {unpostableCount > 0 && (
          <ContextMenuItem onClick={() => batchActions.handleUnpostBatch(docTargets)}>
            <CircleOff className="mr-2 h-4 w-4" />
            Отменить проведение{fmtCount(unpostableCount)}
          </ContextMenuItem>
        )}
      </>
    )
  }, [isDocumentMode, batchActions, selectAllByFilter, totalCount, excludedIds, config, router])

  // ── Resolve effective handlers (document mode auto-generated vs custom override) ──
  const effectiveOnRowClick = config.onRowClick ?? (isDocumentMode ? handleDocRowClick : undefined)
  const effectiveRenderContextMenu = config.renderContextMenu ?? (isDocumentMode ? renderDocContextMenu : undefined)
  const effectiveCopyClick = config.onCopyClick !== undefined ? config.onCopyClick : (isDocumentMode && (focusedId || selectedIds.length === 1) ? handleDocCopy : null)
  const effectiveSelectAll = isDocumentMode ? { totalCount, selectAllByFilter, excludedIds, activateSelectAll, clearSelection } : undefined

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

  // ── List Export ────────────────────────────────────────────────────────
  const { exportToExcel, exporting } = useListExport({
    basePath: config.basePath ?? "",
  })

  const handleExport = useCallback(() => {
    const cols: ExportColumn[] = effectiveColumns.map((c) => ({
      key: c.key,
      label: c.label,
    }))
    exportToExcel({
      columns: cols,
      filters: currentFilters,
      orderBy: sortColumn ? `${sortDirection === "desc" ? "-" : ""}${sortColumn}` : undefined,
      includeDeleted: showDeleted,
      search: searchQuery,
    })
  }, [effectiveColumns, exportToExcel, sortColumn, sortDirection, showDeleted, searchQuery, currentFilters])

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

  // ── Ctrl+F → focus search input (M5) ──────────────────────────────
  const searchInputRef = useRef<HTMLInputElement | null>(null)
  useShortcut("list.search", "ctrl+f", "Поиск", "list", () => {
    searchInputRef.current?.focus()
    searchInputRef.current?.select()
  })

  // ── Ctrl+Shift+E → export to Excel ────────────────────────────────
  useShortcut("list.export", "ctrl+shift+e", "Экспорт в Excel", "list", () => {
    if (config.basePath && !exporting) handleExport()
  })

  // ── Scroll restoration on tab switch (M2) ─────────────────────────
  useScrollRestore(scrollContainerRef)

  // ── Saved Views ───────────────────────────────────────────────────
  const getCurrentConfig = useCallback((): ListViewConfig => ({
    filters: initialFilterValues,
    columns: hasColumnChooser ? colChooser.visibleKeys : effectiveColumns.map((c) => c.key),
    sortColumn: sortColumn ?? null,
    sortDir: sortDirection,
  }), [initialFilterValues, hasColumnChooser, colChooser.visibleKeys, effectiveColumns, sortColumn, sortDirection])

  const [viewResetKey, setViewResetKey] = useState(0)

  const handleApplyViewConfig = useCallback((viewConfig: ListViewConfig) => {
    // Apply filters
    handleFilterValuesChange(viewConfig.filters ?? {})
    // Apply columns (if column chooser enabled)
    if (hasColumnChooser && viewConfig.columns?.length > 0) {
      colChooser.reorderColumns(viewConfig.columns)
    }
    // Apply sort
    if (viewConfig.sortColumn) {
      handleSort(viewConfig.sortColumn)
    }
    // Force FilterSidebar remount to pick up new initialFilterValues from store.
    setViewResetKey((k) => k + 1)
  }, [handleFilterValuesChange, hasColumnChooser, colChooser, handleSort])

  const listViewsHook = useListViews({
    entityType: config.entityKey,
    onApplyConfig: handleApplyViewConfig,
    getCurrentConfig,
  })

  return (
    <div className="flex h-full flex-col">
      <DataToolbar
        title={title}
        onCreateHref={config.createHref}
        onCopyClick={effectiveCopyClick}
        searchValue={searchQuery}
        onSearchChange={setSearchQuery}
        searchInputRef={(el) => { searchInputRef.current = el }}
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
        onExportClick={config.basePath ? handleExport : undefined}
        exporting={exporting}
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
            {effectiveSelectAll && (
              <SelectAllBanner
                selectedCount={selectedIds.length}
                totalCount={effectiveSelectAll.totalCount}
                selectAllByFilter={effectiveSelectAll.selectAllByFilter}
                excludedCount={effectiveSelectAll.excludedIds.length}
                onSelectAll={effectiveSelectAll.activateSelectAll}
                onClearAll={effectiveSelectAll.clearSelection}
              />
            )}
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
              onRowClick={effectiveOnRowClick}
              onRowDoubleClick={(item) => router.push(config.editHref(item))}
              colWidths={colWidths}
              onResizeStart={onResizeStart}
              isResizing={isResizing}
              renderPrefix={config.renderPrefix}
              rowClassName={config.rowClassName}
              renderContextMenu={effectiveRenderContextMenu}
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
            key={`${config.entityKey}-filters-${viewResetKey}`}
            showGroups={false}
            showDetails={false}
            fieldsMeta={fieldsMeta}
            defaultSelectedKeys={config.defaultFilterKeys}
            periodField={config.periodField}
            onFilterValuesChange={handleFilterValuesChange}
            initialFilterValues={initialFilterValues}
            savedViews={listViewsHook.views}
            activeViewId={listViewsHook.activeViewId}
            onSelectView={listViewsHook.selectView}
            onSaveView={listViewsHook.saveCurrentAsView}
            onOverwriteView={listViewsHook.overwriteView}
            onRenameView={listViewsHook.renameView}
            onDeleteView={listViewsHook.deleteView}
            onSetDefaultView={listViewsHook.setDefault}
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
