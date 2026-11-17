"use client"

import React, { useMemo, useCallback, useRef, useState, useEffect } from "react"
import { useRouter } from "next/navigation"
import { Plus, Copy, Pencil, Trash2, CircleCheckBig, CircleOff, PanelBottomClose, PanelBottomOpen, FileText } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
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
import { useColumnFilterContext } from "@/hooks/useColumnFilterContext"
import type { CursorListParams, CursorListResponse, AdvancedFilterItem, BatchActionResponse, BatchActionType, BatchActionByFilterRequest } from "@/types/common"
import type { ListViewConfig } from "@/types/list-view"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { apiFetch, buildListQS } from "@/lib/api"
import type { DocumentCapabilities } from "@/types/metadata"
import { toast } from "sonner"

// ── Config ──────────────────────────────────────────────────────────────

export interface CatalogListPageConfig<T extends { id: string }> {
  rowsState: import("@/stores/useTabStateStore").TabStateKey<T[]>
  /** Page title shown in toolbar. If entityKey is provided, resolved from metadata store. */
  title: string
  /** Entity key for metadata-driven label resolution and filter metadata (e.g. "token"). */
  entityKey: string
  /** Route for creating a new entity. */
  createHref?: string
  /** Route for editing an entity. */
  editHref: (item: T) => string
  /** Column definitions for DataTable. */
  columns: Column<T>[]
  /** API fetcher function (e.g. api.counterparties.list). */
  fetcher: (params?: CursorListParams) => Promise<CursorListResponse<T>>
  /** Empty state message. */
  emptyMessage?: string
  /** Max items per fetch. Default 100. */
  limit?: number
  /** Default selected filter keys. */
  defaultFilterKeys?: string[]
  additionalFilterFields?: readonly import("@/components/shared/filter-config-dialog").FilterFieldMeta[]
  /** Period field key for filter sidebar (omit for catalogs without date filter). */
  periodField?: string
  /** Full column registry (all possible columns). When provided, enables Column Chooser. */
  allColumns?: Column<T>[]
  /** Default visible column keys. Required when allColumns is provided. */
  defaultVisibleKeys?: string[]
  /** API base path for export (e.g. "/catalog/tokens"). If omitted, export is disabled. */
  basePath?: string
  /** Optional row prefix renderer (e.g., posted/deletionMark icons for documents). */
  renderPrefix?: (item: T) => React.ReactNode
  /** Optional row className (e.g., opacity+strikethrough for deletion-marked documents). */
  rowClassName?: (item: T) => string | undefined
  /** Optional context menu renderer — enables right-click menus on rows (custom override). */
  renderContextMenu?: (item: T, targets: T[]) => React.ReactNode
  /**
   * Extra row-click side effect (preview panel, detail fetch, …).
   * Current-row focus is always applied first; this runs after it.
   * Do not use this prop to "enable" highlighting — that is a list primitive.
   */
  onRowClick?: (item: T) => void
  /** Optional details area for the focused row. Business actions remain in the caller. */
  renderSelectionPanel?: (item: T | null, refresh: () => void) => React.ReactNode
  /** Optional copy button handler — enables "Copy" toolbar button (custom override). */
  onCopyClick?: (() => void) | null
  /**
   * Document mode: enables batch actions, document context menu, keyboard
   * shortcuts (copy / deletion mark / post), and select-all banner.
   * Current-row focus is independent of this flag — it is a list primitive.
   * When set, `renderContextMenu` and `onCopyClick` are auto-generated
   * unless a custom override is provided.
   */
  documentMode?: {
    /** API base path for batch operations (e.g. "/document/goods-receipt"). */
    basePath: string
    /** Metadata-driven generic document capabilities. Missing means legacy full document surface. */
    capabilities?: DocumentCapabilities
  }
}

// ── Component ───────────────────────────────────────────────────────────

/** Minimal document shape for batch actions (documents always have these fields). */
interface DocumentLike {
  id: string
  posted: boolean
  deletionMark: boolean
}

function isDocumentRow<T extends { id: string }>(row: T): row is T & DocumentLike {
  return "posted" in row && typeof row.posted === "boolean" && "deletionMark" in row && typeof row.deletionMark === "boolean"
}

const SELECTION_PANEL_STORAGE_KEY = "metapus-selection-panel-collapsed"

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
    refreshing,
    loadingMore,
    error,
    refresh,
    hasMore,
    loadMore,
    selectedIds,
    isAllSelected,
    isIndeterminate,
    toggleItem,
    selectRows,
    toggleAll,
    sortColumn,
    sortDirection,
    handleSort,
    fieldsMeta,
    fieldsMetaLoading,
    isPrefsLoaded,
    initialFilterValues,
    handleFilterValuesChange,
    showDeleted,
    toggleShowDeleted,
    focusedId,
    handleRowClick: focusRow,
    focusedColumnKey,
    handleCellClick: focusCell,
    searchQuery,
    setSearchQuery,
    currentFilters,
    totalCount,
    selectAllByFilter,
    excludedIds,
    activateSelectAll,
    clearSelection,
    replaceItems,
    revealTarget,
  } = useEntityListPage<T>({
    rowsState: config.rowsState,
    entityKey: config.entityKey,
    api: { list: config.fetcher },
    periodField: config.periodField,
    limit: config.limit ?? 100,
    additionalFilterFields: config.additionalFilterFields,
  })

  // ── Document mode: batch actions + context menu ──────────────────────
  const isDocumentMode = !!config.documentMode
  const docBasePath = config.documentMode?.basePath ?? ""
  const documentCapabilities = config.documentMode?.capabilities
  const hasDocCapability = useCallback(
    (key: keyof DocumentCapabilities) => documentCapabilities?.[key] !== false,
    [documentCapabilities],
  )
  const canDocCreate = !!config.createHref
  const canDocCopy = isDocumentMode && hasDocCapability("copy") && !!config.createHref
  const canDocSetDeletionMark = isDocumentMode && hasDocCapability("setDeletionMark")
  const canDocPost = isDocumentMode && hasDocCapability("post")
  const canDocUnpost = isDocumentMode && hasDocCapability("unpost")
  const canDocBatchAction = isDocumentMode && hasDocCapability("batchAction")
  const canDocBatchActionByFilter = isDocumentMode && hasDocCapability("batchActionByFilter")
  const canExportList = !isDocumentMode || hasDocCapability("exportList")

  // Build a batch-capable API adapter from basePath (generic — works for any document entity)
  const fetchList = config.fetcher
  const batchApi = useMemo(() => {
    if (!isDocumentMode) return null
    return {
      list: (params?: { limit?: number; filter?: AdvancedFilterItem[]; includeDeleted?: boolean }) =>
        fetchList(params).then(response => ({ items: response.items.filter(isDocumentRow) })),
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
  }, [isDocumentMode, docBasePath, fetchList])

  const docItems = items.filter(isDocumentRow)

  const _noopResponse: BatchActionResponse = { results: [], total: 0, success: 0, failed: 0 }
  const batchActions = useDocumentBatchActions<T & DocumentLike>({
    api: batchApi ?? { batchAction: async () => _noopResponse, batchActionByFilter: async () => _noopResponse, _basePath: "" },
    replaceItems,
    refresh,
    items: docItems,
    selectedIds,
    focusedId,
    selectAllByFilter,
    excludedIds,
    currentFilters,
    showDeleted,
    clearSelection,
    capabilities: documentCapabilities,
  })

  // Current row is a list primitive (catalogs and documents). Extra
  // onRowClick from config is composed after focus, not a replacement —
  // otherwise a custom handler can silently drop highlighting.
  const extraOnRowClick = config.onRowClick
  const onRowClick = useCallback(
    (item: T) => {
      focusRow(item)
      extraOnRowClick?.(item)
    },
    [focusRow, extraOnRowClick],
  )

  // ── Universal Copy handler (both document & catalog) ─────────────
  const canCopy = isDocumentMode
    ? canDocCopy
    : !!config.createHref && !!config.editHref

  const handleCopy = useCallback(() => {
    if (!canCopy) return
    const targetId = focusedId ?? (selectedIds.length === 1 ? selectedIds[0] : null)
    if (targetId && config.editHref) {
      // Derive the "new" page from editHref pattern
      const editUrl = config.editHref({ id: targetId } as T)
      const listBase = editUrl.substring(0, editUrl.lastIndexOf("/"))
      router.push(`${listBase}/new?copyFrom=${targetId}`)
    }
  }, [canCopy, focusedId, selectedIds, config, router])

  const copyFromItem = useCallback((targetItem: T) => {
    const editUrl = config.editHref(targetItem)
    const listBase = editUrl.substring(0, editUrl.lastIndexOf("/"))
    router.push(`${listBase}/new?copyFrom=${targetItem.id}`)
  }, [config, router])

  // ── Document mode: deletion mark handler ──────────────────────────
  const handleDocDeleteMark = useCallback(() => {
    if (!isDocumentMode || !canDocSetDeletionMark || !canDocBatchAction) return
    const targets = selectedIds.length > 0
      ? docItems.filter((d) => selectedIds.includes(d.id))
      : docItems.filter((d) => d.id === focusedId)
    if (targets.length === 0) return
    const shouldMark = targets.some((d) => !d.deletionMark)
    batchActions.handleToggleDeletionMarkBatch(targets, shouldMark)
  }, [isDocumentMode, canDocSetDeletionMark, canDocBatchAction, focusedId, docItems, selectedIds, batchActions])

  // ── Catalog mode: deletion mark handler ────────────────────────────
  const handleToggleCatalogDeletionMark = useCallback(async (targets: T[], mark: boolean) => {
    if (!config.basePath || targets.length === 0) return
    try {
      await Promise.all(
        targets.map((t) =>
          apiFetch<void>(`${config.basePath}/${t.id}/deletion-mark`, {
            method: "POST",
            body: JSON.stringify({ marked: mark }),
          })
        )
      )
      const targetMap = new Map(targets.map((t) => [t.id, { ...t, deletionMark: mark }]))
      if (!showDeleted && mark) {
        refresh()
      } else {
        replaceItems(
          items.map((item) => (targetMap.has(item.id) ? (targetMap.get(item.id) as T) : item))
        )
      }
      toast.success(
        mark
          ? targets.length === 1
            ? "Помечено на удаление"
            : `Помечено на удаление: ${targets.length}`
          : targets.length === 1
          ? "Пометка удаления снята"
          : `Пометка удаления снята: ${targets.length}`
      )
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Не удалось изменить пометку удаления")
    }
  }, [config.basePath, showDeleted, refresh, replaceItems, items])

  const handleCatalogDeleteMarkShortcut = useCallback(() => {
    if (isDocumentMode || !config.basePath) return
    const targets = selectedIds.length > 0
      ? items.filter((d) => selectedIds.includes(d.id))
      : items.filter((d) => d.id === focusedId)
    if (targets.length === 0) return
    const shouldMark = targets.some((d) => !(d as { deletionMark?: boolean }).deletionMark)
    handleToggleCatalogDeletionMark(targets, shouldMark)
  }, [isDocumentMode, config.basePath, selectedIds, items, focusedId, handleToggleCatalogDeletionMark])

  // ── Keyboard shortcuts ─────────────────────────────────────────────
  useShortcut("list.copy", "f9", "Создать копию", "list", canCopy ? handleCopy : () => {})
  useShortcut(
    "list.delete",
    "delete",
    "Пометить на удаление",
    "list",
    isDocumentMode
      ? (canDocSetDeletionMark && canDocBatchAction ? handleDocDeleteMark : () => {})
      : (config.basePath ? handleCatalogDeleteMarkShortcut : () => {})
  )
  useShortcut(
    "list.create",
    "insert",
    "Создать",
    "list",
    config.createHref ? () => router.push(config.createHref!) : () => {}
  )

  // Handled by DataTable while focus is in the list; listed here for the help dialog.
  useShortcut("list.nav-rows", "arrowdown", "Перейти по строкам (также ↑, PgUp, PgDn, Home, End)", "list", () => {}, { passive: true })
  useShortcut("list.nav-select", "shift+arrowdown", "Выделить строки (также Shift+↑)", "list", () => {}, { passive: true })
  useShortcut("list.nav-columns", "arrowright", "Перейти по колонкам (также ←)", "list", () => {}, { passive: true })
  useShortcut("list.open", "enter", "Открыть", "list", () => {}, { passive: true })

  // ── Document mode: context menu ───────────────────────────────────
  const renderDocContextMenu = useCallback((item: T, targets: T[]) => {
    if (!isDocumentMode) return null
    const docTargets = targets.filter(isDocumentRow)
    const isBatch = selectAllByFilter || targets.length > 1
    const { postableCount, unpostableCount, markableCount, unmarkeableCount } = batchActions.getBatchMenuCounts(docTargets)
    const virtualTotal = totalCount - excludedIds.length
    const fmtCount = (n: number) =>
      selectAllByFilter
        ? ` (${virtualTotal.toLocaleString("ru-RU")})`
        : isBatch ? ` (${n})` : ""

    return (
      <>
        {canDocCreate && (
          <ContextMenuItem onClick={() => config.createHref && router.push(config.createHref)}>
            <Plus className="mr-2 h-4 w-4" />
            Создать
            <ContextMenuShortcut>Ins</ContextMenuShortcut>
          </ContextMenuItem>
        )}
        {!isBatch && canCopy && (
          <ContextMenuItem onClick={() => copyFromItem(item)}>
            <Copy className="mr-2 h-4 w-4" />
            Создать копию
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
        {(canDocSetDeletionMark || canDocPost || canDocUnpost) && <ContextMenuSeparator />}
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
        {(canDocPost || canDocUnpost) && <ContextMenuSeparator />}
        {postableCount > 0 && canDocBatchAction && (
          <ContextMenuItem onClick={() => batchActions.handlePostBatch(docTargets)}>
            <CircleCheckBig className="mr-2 h-4 w-4" />
            Провести{fmtCount(postableCount)}
          </ContextMenuItem>
        )}
        {unpostableCount > 0 && canDocBatchAction && (
          <ContextMenuItem onClick={() => batchActions.handleUnpostBatch(docTargets)}>
            <CircleOff className="mr-2 h-4 w-4" />
            Отменить проведение{fmtCount(unpostableCount)}
          </ContextMenuItem>
        )}
      </>
    )
  }, [isDocumentMode, canDocCreate, canCopy, copyFromItem, canDocSetDeletionMark, canDocPost, canDocUnpost, canDocBatchAction, batchActions, selectAllByFilter, totalCount, excludedIds, config, router])

  // ── Catalog mode: context menu ────────────────────────────────────
  const renderCatalogContextMenu = useCallback((item: T, targets: T[]) => {
    if (isDocumentMode) return null
    const isBatch = targets.length > 1
    const markableTargets = targets.filter((d) => !(d as { deletionMark?: boolean }).deletionMark)
    const unmarkableTargets = targets.filter((d) => (d as { deletionMark?: boolean }).deletionMark)
    const canDelete = !!config.basePath
    const canCreate = !!config.createHref
    const canCopyItem = !isBatch && canCopy

    return (
      <>
        {canCreate && (
          <ContextMenuItem onClick={() => config.createHref && router.push(config.createHref)}>
            <Plus className="mr-2 h-4 w-4" />
            Создать
            <ContextMenuShortcut>Ins</ContextMenuShortcut>
          </ContextMenuItem>
        )}
        {canCopyItem && (
          <ContextMenuItem onClick={() => copyFromItem(item)}>
            <Copy className="mr-2 h-4 w-4" />
            Создать копию
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
        {canDelete && (markableTargets.length > 0 || unmarkableTargets.length > 0) && <ContextMenuSeparator />}
        {markableTargets.length > 0 && (
          <ContextMenuItem onClick={() => handleToggleCatalogDeletionMark(markableTargets, true)}>
            <Trash2 className="mr-2 h-4 w-4" />
            Пометить на удаление{isBatch ? ` (${markableTargets.length})` : ""}
            {!isBatch && <ContextMenuShortcut>Del</ContextMenuShortcut>}
          </ContextMenuItem>
        )}
        {unmarkableTargets.length > 0 && (
          <ContextMenuItem onClick={() => handleToggleCatalogDeletionMark(unmarkableTargets, false)}>
            <Trash2 className="mr-2 h-4 w-4" />
            Снять пометку удаления{isBatch ? ` (${unmarkableTargets.length})` : ""}
            {!isBatch && <ContextMenuShortcut>Del</ContextMenuShortcut>}
          </ContextMenuItem>
        )}
      </>
    )
  }, [isDocumentMode, config, router, canCopy, copyFromItem, handleToggleCatalogDeletionMark])

  // ── Resolve effective handlers (document mode auto-generated vs catalog mode vs custom override) ──
  const effectiveRenderContextMenu = config.renderContextMenu ?? (isDocumentMode ? renderDocContextMenu : renderCatalogContextMenu)
  const effectiveCopyClick = config.onCopyClick !== undefined
    ? config.onCopyClick
    : canCopy
      ? (focusedId || selectedIds.length === 1 ? handleCopy : null)
      : undefined
  const effectiveSelectAll = isDocumentMode && canDocBatchActionByFilter
    ? { totalCount, selectAllByFilter, excludedIds, activateSelectAll, clearSelection }
    : undefined

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
    basePath: canExportList ? config.basePath ?? "" : "",
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

  const columnFilterContext = useColumnFilterContext({
    focusedColumnKey,
    focusedId,
    items,
    columns: effectiveColumns,
    fieldsMeta,
  })

  // ── Ctrl+Shift+E → export to Excel ────────────────────────────────
  useShortcut("list.export", "ctrl+shift+e", "Экспорт в Excel", "list", () => {
    if (config.basePath && canExportList && !exporting) handleExport()
  })

  // ── Scroll restoration on tab switch (M2) ─────────────────────────
  useScrollRestore(scrollContainerRef)

  useEffect(() => {
    if (!revealTarget) return
    const frame = requestAnimationFrame(() => {
      const row = scrollContainerRef.current?.querySelector<HTMLElement>(
        `[data-row-id="${CSS.escape(revealTarget.id)}"]`,
      )
      row?.scrollIntoView({ block: "center", inline: "nearest" })
      row?.focus({ preventScroll: true })
    })
    return () => cancelAnimationFrame(frame)
  }, [revealTarget])

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
  const shouldRenderFilterSidebar = isPrefsLoaded && (fieldsMeta.length > 0 || fieldsMetaLoading)

  const [isSelectionPanelCollapsed, setIsSelectionPanelCollapsed] = useState(() => {
    if (typeof window === "undefined") return false
    const stored = window.localStorage.getItem(SELECTION_PANEL_STORAGE_KEY)
    return stored !== null ? stored === "true" : false
  })

  const toggleSelectionPanel = useCallback(() => {
    setIsSelectionPanelCollapsed((prev) => {
      const next = !prev
      window.localStorage.setItem(SELECTION_PANEL_STORAGE_KEY, String(next))
      return next
    })
  }, [])

  const focusedItem = useMemo(
    () => items.find((item) => item.id === focusedId) ?? null,
    [items, focusedId],
  )

  const focusedItemTitle = useMemo(() => {
    if (!focusedItem) return null
    const candidate = focusedItem as { number?: string; name?: string }
    return candidate.number ?? candidate.name ?? null
  }, [focusedItem])

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
        onExportClick={config.basePath && canExportList ? handleExport : undefined}
        exporting={exporting}
      />

      <div className="flex flex-1 overflow-hidden">
        <div className="flex flex-1 flex-col min-w-0 overflow-hidden">
          <ScrollArea className="flex-1" viewportRef={scrollContainerRef}>
            <ListContent
              initialLoading={loading}
              refreshing={refreshing}
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
                focusedColumnKey={focusedColumnKey}
                onRowClick={onRowClick}
                onCellClick={focusCell}
                onSelectRows={selectRows}
                onRowDoubleClick={(item) => router.push(config.editHref(item))}
                colWidths={colWidths}
                onResizeStart={onResizeStart}
                isResizing={isResizing}
                renderPrefix={config.renderPrefix}
                rowClassName={config.rowClassName}
                renderContextMenu={effectiveRenderContextMenu}
              />
              {/* Below the rows and pinned to the bottom, so selecting rows does not move them. */}
              {effectiveSelectAll && (
                <div className="sticky bottom-0 z-20 bg-background shadow-[0_-1px_0_hsl(var(--border))] empty:hidden">
                  <SelectAllBanner
                    selectedCount={selectedIds.length}
                    totalCount={effectiveSelectAll.totalCount}
                    selectAllByFilter={effectiveSelectAll.selectAllByFilter}
                    excludedCount={effectiveSelectAll.excludedIds.length}
                    onSelectAll={effectiveSelectAll.activateSelectAll}
                    onClearAll={effectiveSelectAll.clearSelection}
                  />
                </div>
              )}
            </ListContent>
            <ScrollSentinel
              onIntersect={loadMore}
              loading={loadingMore}
              enabled={hasMore}
              rootMargin="0px 0px 2000px 0px"
              scrollContainer={scrollContainerRef}
            />
          </ScrollArea>

          {config.renderSelectionPanel && (
            <div
              className={cn(
                "shrink-0 border-t bg-card",
                isSelectionPanelCollapsed && "h-9"
              )}
            >
              <div
                className={cn(
                  "flex cursor-pointer items-center justify-between bg-muted/20 px-3 text-xs text-muted-foreground hover:bg-muted/30 transition-colors select-none",
                  isSelectionPanelCollapsed ? "h-full" : "h-9 border-b"
                )}
                onClick={toggleSelectionPanel}
              >
                <span className="flex items-center gap-1.5 font-medium text-foreground/80">
                  <FileText className="h-4 w-4 text-muted-foreground" />
                  {focusedItemTitle ? `Детали: ${focusedItemTitle}` : "Детали выбранного документа"}
                </span>
                <TooltipProvider delayDuration={300}>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7 text-muted-foreground hover:text-foreground shrink-0"
                        onClick={(e) => {
                          e.stopPropagation()
                          toggleSelectionPanel()
                        }}
                      >
                        {isSelectionPanelCollapsed ? (
                          <PanelBottomOpen className="h-4 w-4" />
                        ) : (
                          <PanelBottomClose className="h-4 w-4" />
                        )}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent side="top">
                      {isSelectionPanelCollapsed ? "Развернуть панель деталей" : "Свернуть панель деталей"}
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </div>

              {!isSelectionPanelCollapsed && (
                <div className="max-h-72 overflow-y-auto">
                  {config.renderSelectionPanel(focusedItem, refresh)}
                </div>
              )}
            </div>
          )}
        </div>

        {shouldRenderFilterSidebar && (
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
            columnFilterContext={columnFilterContext}
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
