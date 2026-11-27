"use client"

import { useState, useEffect, useMemo, useCallback, useRef } from "react"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Filter, FolderOpen, FileText, PanelRightClose, PanelRightOpen, X, RotateCcw, Search, Bookmark, Save, MoreHorizontal, Check, Pencil, Trash2, RefreshCw, Star } from "lucide-react"
import { cn } from "@/lib/utils"
import { ScrollArea } from "@/components/ui/scroll-area"
import { AccountingPeriodPicker, type DateRangeValue } from "@/components/ui/accounting-period-picker"
import { FilterConfigDialog } from "@/components/shared/filter-config-dialog"
import { SaveViewDialog } from "@/components/shared/save-view-dialog"
import { FilterValueControl } from "@/components/shared/filter-value-control"
import { ColumnFilterDialog } from "@/components/shared/column-filter-dialog"
import {
  type FilterValues,
  type FilterEntry,
  PERIOD_FILTER_KEY,
  getOperatorsForType,
  getDefaultOperator,
  isNullaryOperator,
  valueAfterOperatorChange,
  indexFilterFields,
  formatLocalYYYYMMDD,
} from "@/lib/filter-utils"
import type { ComparisonOperator } from "@/types/common"
import type { FilterFieldMeta, ColumnFilterCell } from "@/types/filter"
import type { ListView, ViewVisibility } from "@/types/list-view"
import { useShortcut } from "@/hooks/useShortcut"
import { toast } from "sonner"
import { useAuthStore } from "@/stores/useAuthStore"
import { canEditSavedView } from "@/lib/saved-view-policy"
import { exactFilterEntryError } from "@/lib/filter-numbers"
import { isImeComposing } from "@/lib/keyboard-utils"

interface FilterSidebarProps {
  showGroups?: boolean
  showDetails?: boolean
  /** Full list of document fields available for filtering */
  fieldsMeta?: FilterFieldMeta[]
  /** Default selected filter keys (used when filters prop is not provided) */
  defaultSelectedKeys?: string[]
  /**
   * DB column name that the built-in period filter targets (e.g. "date").
   * When set, a permanent, non-removable AccountingPeriodPicker is rendered
   * at the top of the filter list (like the standard period in 1C).
   * Absent in catalogs — only used for document lists.
   */
  periodField?: string
  /** Callback when filter configuration changes (which filters are selected) */
  onFilterConfigChange?: (selectedKeys: string[]) => void
  /** Callback when filter VALUES change — use this to refetch data */
  onFilterValuesChange?: (values: FilterValues) => void
  /** Initial values to restore saved filter settings */
  initialFilterValues?: FilterValues
  /** Content to render inside the "Details" tab (e.g. document preview). */
  detailsContent?: React.ReactNode
  /** Called when the user switches between sidebar tabs ("groups" | "filters" | "details"). */
  onActiveTabChange?: (tab: string) => void

  // ── Saved Views ─────────────────────────────────────────────────────
  /** Available saved views for this entity. */
  savedViews?: ListView[]
  /** Currently active view ID. */
  activeViewId?: string | null
  /** Called when the user selects a saved view (or null to clear). */
  onSelectView?: (id: string | null) => void
  /** Called when the user wants to save current filters as a new view. */
  onSaveView?: (name: string, visibility: ViewVisibility) => Promise<void>
  /** Called when the user wants to overwrite a view with current filters. */
  onOverwriteView?: (id: string) => Promise<void>
  /** Called when the user renames a view. */
  onRenameView?: (id: string, name: string) => Promise<void>
  /** Called when the user deletes a view. */
  onDeleteView?: (id: string) => Promise<void>
  /** Called when the user sets a view as default. */
  onSetDefaultView?: (id: string) => Promise<void>
  /**
   * Current list cell for Alt+F. Pass the prop (including null) on list pages
   * to enable column-context filters. Omit it on reports.
   */
  columnFilterContext?: ColumnFilterCell | null
}

const STORAGE_KEY = "metapus-filter-sidebar-collapsed"

export function FilterSidebar({
  showGroups = true,
  showDetails = true,
  fieldsMeta = [],
  defaultSelectedKeys,
  periodField,
  onFilterConfigChange,
  onFilterValuesChange,
  initialFilterValues,
  detailsContent,
  onActiveTabChange,
  savedViews = [],
  activeViewId,
  onSelectView,
  onSaveView,
  onOverwriteView,
  onRenameView,
  onDeleteView,
  onSetDefaultView,
  columnFilterContext,
}: FilterSidebarProps) {
  const user = useAuthStore(state => state.user)
  const canEditActiveView = canEditSavedView(savedViews.find(view => view.id === activeViewId), user)
  const [isCollapsed, setIsCollapsed] = useState(() => {
    if (typeof window === "undefined") return true
    const stored = window.localStorage.getItem(STORAGE_KEY)
    return stored !== null ? stored === "true" : true
  })
  const [filterDialogOpen, setFilterDialogOpen] = useState(false)
  const [saveViewDialogOpen, setSaveViewDialogOpen] = useState(false)
  const [renamingViewId, setRenamingViewId] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState("")
  const [deletingViewId, setDeletingViewId] = useState<string | null>(null)

  // ── Filter values state (FilterEntry per key) ───────────────────────
  const [filterValues, setFilterValues] = useState<FilterValues>(() => initialFilterValues ?? {})

  const [selectedFilterKeys, setSelectedFilterKeys] = useState<string[]>(
    () => {
      if (initialFilterValues && Object.keys(initialFilterValues).length > 0) {
        return Object.keys(initialFilterValues).filter(k => k !== PERIOD_FILTER_KEY)
      }
      return defaultSelectedKeys ?? []
    }
  )

  const [hasPendingChanges, setHasPendingChanges] = useState(false)
  const filterValuesRef = useRef<FilterValues>(filterValues)
  useEffect(() => { filterValuesRef.current = filterValues }, [filterValues])

  const selectedFilterKeysRef = useRef(selectedFilterKeys)
  useEffect(() => { selectedFilterKeysRef.current = selectedFilterKeys }, [selectedFilterKeys])

  const upsertFilter = useCallback((key: string, entry: FilterEntry) => {
    const nextKeys = selectedFilterKeysRef.current.includes(key)
      ? selectedFilterKeysRef.current
      : [...selectedFilterKeysRef.current, key]
    setSelectedFilterKeys(nextKeys)
    onFilterConfigChange?.(nextKeys)
    const next = { ...filterValuesRef.current, [key]: entry }
    filterValuesRef.current = next
    setFilterValues(next)
    setHasPendingChanges(false)
    setIsCollapsed(false)
    if (typeof window !== "undefined") {
      localStorage.setItem(STORAGE_KEY, "false")
    }
    onFilterValuesChange?.(next)
  }, [onFilterConfigChange, onFilterValuesChange])

  const columnFilterEnabled = columnFilterContext !== undefined
  const [columnFilterOpen, setColumnFilterOpen] = useState(false)

  const openColumnFilter = useCallback(() => {
    if (!columnFilterEnabled) return
    if (!columnFilterContext) {
      toast("Кликните ячейку, чтобы выбрать колонку")
      return
    }
    setColumnFilterOpen(true)
  }, [columnFilterEnabled, columnFilterContext])

  useShortcut(
    "list.column-filter",
    "alt+f",
    "Отбор по колонке",
    "list",
    openColumnFilter,
    { enabled: columnFilterEnabled },
  )

  // Apply current filter values to the list (manual trigger via 🔍 button)
  const applyFilters = useCallback(() => {
    const fields = indexFilterFields(fieldsMeta)
    for (const [key, entry] of Object.entries(filterValuesRef.current)) {
      const field = fields.get(key)
      const error = field && exactFilterEntryError(field, entry)
      if (error) {
        toast.error(`${field.label}: ${error}`)
        return
      }
    }
    onFilterValuesChange?.(filterValuesRef.current)
    setHasPendingChanges(false)
  }, [fieldsMeta, onFilterValuesChange])

  const updateFilterEntry = useCallback(
    (key: string, entry: FilterEntry) => {
      setFilterValues((prev) => ({ ...prev, [key]: entry }))
      setHasPendingChanges(true)
    },
    []
  )

  const updateFilterOperator = useCallback(
    (key: string, fieldType: FilterFieldMeta["fieldType"], newOp: ComparisonOperator) => {
      setFilterValues((prev) => {
        const current = prev[key]
        const oldOp = current?.operator ?? getDefaultOperator(fieldType)
        const value = valueAfterOperatorChange(oldOp, newOp, current?.value)
        return { ...prev, [key]: { operator: newOp, value } }
      })
      setHasPendingChanges(true)
    },
    []
  )

  const resetAllFilters = useCallback(() => {
    setFilterValues({})
    onFilterValuesChange?.({})
    setHasPendingChanges(false)
  }, [onFilterValuesChange])

    // ── Flatten fieldsMeta to include nested refFields ──────────────────
    // This allows lookup of dotted keys like "counterpartyId.inn" by composing
    // the parent key + child key, and prepending the parent label for context.

    const flatFieldsMetaMap = useMemo(
      () => indexFilterFields(fieldsMeta),
      [fieldsMeta],
    )

    // ── Derive visible filters from selectedFilterKeys ────────────────────

    const visibleFilters: FilterFieldMeta[] = useMemo(() => {
        return selectedFilterKeys
            .map((key) => flatFieldsMetaMap.get(key))
            .filter(Boolean) as FilterFieldMeta[]
    }, [selectedFilterKeys, flatFieldsMetaMap])

  // ── Remove a single filter from the visible list ────────────────────

  const removeFilter = (key: string) => {
    setSelectedFilterKeys((prev) => prev.filter((k) => k !== key))
    onFilterConfigChange?.(selectedFilterKeys.filter((k) => k !== key))
    const next = { ...filterValues }
    delete next[key]
    setFilterValues(next)
    onFilterValuesChange?.(next)
    setHasPendingChanges(false)
  }

  const toggleCollapse = () => {
    const next = !isCollapsed
    setIsCollapsed(next)
    localStorage.setItem(STORAGE_KEY, String(next))
  }

  // Check if a filter entry has a non-empty value
  function isEntryActive(entry: FilterEntry | undefined): boolean {
    if (!entry) return false
    if (isNullaryOperator(entry.operator)) return true
    const v = entry.value
    if (v === undefined || v === null || v === "") return false
    if (Array.isArray(v)) return v.length > 0
    if (typeof v === "object" && v !== null && ("from" in v || "to" in v)) {
      const r = v as { from?: string; to?: string }
      return !!(r.from || r.to)
    }
    if (typeof v === "object" && v !== null && "id" in v) {
      return !!(v as { id: string }).id
    }
    return true
  }

  // Count active (non-empty) filter entries
  const activeCount = useMemo(() => {
    return Object.values(filterValues).filter(isEntryActive).length
  }, [filterValues])

  // ── Get current entry for a filter key ───────────────────────────

  function getEntry(meta: FilterFieldMeta): FilterEntry {
    return filterValues[meta.key] ?? {
      operator: getDefaultOperator(meta.fieldType),
      value: undefined,
    }
  }

  // ── Operator selector (shared across all types) ─────────────────────

  function renderOperatorSelect(meta: FilterFieldMeta, entry: FilterEntry) {
    const operators = getOperatorsForType(meta.fieldType)
    if (operators.length <= 1) return null
    return (
      <Select
        value={entry.operator}
        onValueChange={(val) => updateFilterOperator(meta.key, meta.fieldType, val as ComparisonOperator)}
      >
        <SelectTrigger className="h-6 text-[10px] px-2 w-auto min-w-0 shrink">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {operators.map((op) => (
            <SelectItem key={op.value} value={op.value} className="text-xs">
              {op.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    )
  }

  // ── Filter header (label + operator + remove) ──────────────────────

  function renderFilterHeader(meta: FilterFieldMeta, entry: FilterEntry) {
    return (
      <div className="flex items-center gap-1 mb-1">
        <Label className="text-xs text-muted-foreground truncate flex-1 min-w-0">
          {meta.label}
        </Label>
        {renderOperatorSelect(meta, entry)}
        <Button
          variant="ghost"
          size="icon"
          className="h-4 w-4 shrink-0 text-muted-foreground/50 hover:text-destructive"
          onClick={() => removeFilter(meta.key)}
          title="Убрать фильтр"
        >
          <X className="h-3 w-3" />
        </Button>
      </div>
    )
  }

  // ── Period + filter widgets ───────────────────────────────────────

  /**
   * Built-in period picker (like 1C). Always visible in document lists,
   * non-removable. Stored under PERIOD_FILTER_KEY in filterValues.
   */
  function renderPeriodSection() {
    if (!periodField) return null
    const entry = filterValues[PERIOD_FILTER_KEY]
    const active = isEntryActive(entry)
    const range = entry?.value as { from?: string; to?: string } | undefined
    return (
      <div className="flex flex-col gap-1">
        <Label className="text-xs text-muted-foreground">Период</Label>
        <AccountingPeriodPicker
            value={
              range
                ? {
                  from: range.from ? new Date(range.from) : undefined,
                  to: range.to ? new Date(range.to) : undefined,
                }
                : undefined
            }
            onChange={(val: DateRangeValue | undefined) => {
              if (!val || (!val.from && !val.to)) {
                updateFilterEntry(PERIOD_FILTER_KEY, { operator: "gte", value: undefined })
              } else {
                updateFilterEntry(PERIOD_FILTER_KEY, {
                  operator: "gte",
                  value: {
                    from: val.from ? formatLocalYYYYMMDD(val.from) : undefined,
                    to: val.to ? formatLocalYYYYMMDD(val.to) : undefined,
                  },
                })
              }
            }}
            placeholder="Выбрать период"
            className={cn("h-8 w-full text-xs", active && "border-l-2 border-l-primary")}
          />
      </div>
    )
  }

  function renderFilter(meta: FilterFieldMeta) {
    const entry = getEntry(meta)
    const active = isEntryActive(filterValues[meta.key])
    const accentCls = active ? "border-l-2 border-l-primary" : undefined
    return (
      <div key={meta.key} className="flex flex-col gap-1">
        {renderFilterHeader(meta, entry)}
        <FilterValueControl
          field={meta}
          entry={entry}
          className={accentCls}
          onChange={(next) => updateFilterEntry(meta.key, next)}
        />
      </div>
    )
  }

  return (
    <div
      className={cn(
        "flex flex-col shrink-0 border-l bg-card transition-all duration-300 ease-in-out overflow-hidden",
        isCollapsed ? "w-9 max-w-9" : "w-72 max-w-72"
      )}
    >
      {/* Top action button — visible only when collapsed */}
      <div
        className={cn(
          "flex items-center justify-center border-b shrink-0 bg-muted/20 transition-all duration-300 relative",
          !isCollapsed ? "h-0 opacity-0 pointer-events-none border-b-0" : "h-11 opacity-100"
        )}
      >
        <TooltipProvider delayDuration={300}>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8 text-muted-foreground hover:text-foreground hover:bg-transparent"
                onClick={toggleCollapse}
              >
                <Filter className="h-4 w-4" />
                {activeCount > 0 && (
                  <span className="absolute -top-0.5 -right-0.5 flex h-4 w-4 items-center justify-center rounded-full bg-primary text-[9px] font-bold text-primary-foreground">
                    {activeCount}
                  </span>
                )}
              </Button>
            </TooltipTrigger>
            <TooltipContent side="left">Развернуть панель</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>

      {/* Content — hidden when collapsed */}
      <ScrollArea
        className={cn(
          "flex-1 transition-opacity duration-200 [&_[data-radix-scroll-area-viewport]>div]:!block [&_[data-radix-scroll-area-viewport]>div]:!min-w-0",
          isCollapsed ? "opacity-0 pointer-events-none" : "opacity-100"
        )}
      >
        <Tabs defaultValue="filters" onValueChange={onActiveTabChange}>
          <TabsList className="mx-2 mt-2 w-auto">
            {showGroups && (
              <TabsTrigger value="groups" className="gap-1.5 text-xs">
                <FolderOpen className="h-3.5 w-3.5" />
                Группы
              </TabsTrigger>
            )}
            <TabsTrigger value="filters" className="gap-1.5 text-xs">
              <Filter className="h-3.5 w-3.5" />
              Фильтры
              {activeCount > 0 && (
                <span className="ml-1 flex h-4 w-4 items-center justify-center rounded-full bg-primary text-[9px] font-bold text-primary-foreground">
                  {activeCount}
                </span>
              )}
            </TabsTrigger>
            {showDetails && (
              <TabsTrigger value="details" className="gap-1.5 text-xs">
                <FileText className="h-3.5 w-3.5" />
                Детали
              </TabsTrigger>
            )}
          </TabsList>

          {showGroups && (
            <TabsContent value="groups" className="p-3">
              <div className="rounded-md border p-3 text-center text-sm text-muted-foreground">
                Все элементы
              </div>
            </TabsContent>
          )}

          <TabsContent value="filters" className="p-3">
            <div
              className="flex flex-col gap-3"
              onKeyDown={(e) => {
                if (e.key === "Enter" && !isImeComposing(e)) { e.preventDefault(); applyFilters() }
              }}
            >
              {/* ── Saved Views dropdown ──────────────────────────────── */}
              {(savedViews.length > 0 || onSaveView) && (
                <div className="flex flex-col gap-1">
                  <Label className="text-xs text-muted-foreground">Сохранённые виды</Label>
                  <div className="flex items-center gap-1">
                    <Select
                      value={activeViewId ?? "__none__"}
                      onValueChange={(val) => {
                        onSelectView?.(val === "__none__" ? null : val)
                      }}
                    >
                      <SelectTrigger className="h-7 text-xs flex-1">
                        <SelectValue placeholder="Выбрать вид" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__none__" className="text-xs text-muted-foreground">
                          Без вида
                        </SelectItem>
                        {savedViews.map((view) => (
                          <SelectItem key={view.id} value={view.id} className="text-xs">
                            <span className="flex items-center gap-1.5">
                              {view.isDefault && <Star className="h-3 w-3 text-primary shrink-0 fill-primary" />}
                              {view.name}
                              {view.visibility === "shared" && (
                                <span className="text-[9px] text-muted-foreground">(общий)</span>
                              )}
                            </span>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>

                    {/* Save / Actions menu */}
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="outline" size="icon" className="h-7 w-7 shrink-0">
                          <Bookmark className="h-3.5 w-3.5" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        {onSaveView && (
                          <DropdownMenuItem onClick={() => setSaveViewDialogOpen(true)}>
                            <Save className="mr-2 h-3.5 w-3.5" />
                            Сохранить как новый вид
                          </DropdownMenuItem>
                        )}
                        {activeViewId && canEditActiveView && onOverwriteView && (
                          <DropdownMenuItem onClick={() => onOverwriteView(activeViewId)}>
                            <RefreshCw className="mr-2 h-3.5 w-3.5" />
                            Обновить текущий вид
                          </DropdownMenuItem>
                        )}
                        {activeViewId && canEditActiveView && (
                          <>
                            <DropdownMenuSeparator />
                            {onRenameView && (
                              <DropdownMenuItem onClick={() => {
                                const view = savedViews.find((v) => v.id === activeViewId)
                                if (view) {
                                  setRenamingViewId(view.id)
                                  setRenameValue(view.name)
                                }
                              }}>
                                <Pencil className="mr-2 h-3.5 w-3.5" />
                                Переименовать
                              </DropdownMenuItem>
                            )}
                            {onSetDefaultView && (
                              <DropdownMenuItem onClick={() => onSetDefaultView(activeViewId)}>
                                <Star className="mr-2 h-3.5 w-3.5" />
                                Сделать по умолчанию
                              </DropdownMenuItem>
                            )}
                            {onDeleteView && (
                              <>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem
                                  className="text-destructive focus:text-destructive"
                                  onClick={() => setDeletingViewId(activeViewId)}
                                >
                                  <Trash2 className="mr-2 h-3.5 w-3.5" />
                                  Удалить
                                </DropdownMenuItem>
                              </>
                            )}
                          </>
                        )}
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>

                  {/* Inline rename */}
                  {renamingViewId && onRenameView && (
                    <div
                      className="flex items-center gap-1"
                      onKeyDown={(e) => e.stopPropagation()}
                    >
                      <Input
                        className="h-7 text-xs flex-1"
                        value={renameValue}
                        onChange={(e) => setRenameValue(e.target.value)}
                        onKeyDown={(e) => {
                          if (isImeComposing(e)) return
                          if (e.key === "Enter" && renameValue.trim()) {
                            e.preventDefault()
                            onRenameView(renamingViewId, renameValue.trim())
                            setRenamingViewId(null)
                          }
                          if (e.key === "Escape") setRenamingViewId(null)
                        }}
                        autoFocus
                      />
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7 shrink-0"
                        onClick={() => {
                          if (renameValue.trim()) {
                            onRenameView(renamingViewId, renameValue.trim())
                          }
                          setRenamingViewId(null)
                        }}
                      >
                        <Check className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7 shrink-0 text-muted-foreground"
                        onClick={() => setRenamingViewId(null)}
                      >
                        <X className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  )}
                </div>
              )}

              {/* Delete view confirmation */}
              {deletingViewId && onDeleteView && (
                <AlertDialog open onOpenChange={(open) => { if (!open) setDeletingViewId(null) }}>
                  <AlertDialogContent className="sm:max-w-[360px]">
                    <AlertDialogHeader>
                      <AlertDialogTitle className="text-sm">
                        Удалить вид «{savedViews.find((v) => v.id === deletingViewId)?.name}»?
                      </AlertDialogTitle>
                      <AlertDialogDescription className="text-xs">
                        Сохранённый набор фильтров и настроек будет удалён без возможности восстановления.
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel className="h-8 text-xs">
                        Отмена
                      </AlertDialogCancel>
                      <AlertDialogAction
                        className="h-8 text-xs bg-destructive text-destructive-foreground hover:bg-destructive/90"
                        onClick={() => {
                          onDeleteView(deletingViewId)
                          setDeletingViewId(null)
                        }}
                      >
                        Удалить
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              )}

              {/* Save view dialog */}
              {onSaveView && (
                <SaveViewDialog
                  open={saveViewDialogOpen}
                  onOpenChange={setSaveViewDialogOpen}
                  onSave={onSaveView}
                />
              )}

              {/* Toolbar: configure + apply + reset */}
              <div className="flex items-center gap-1.5">
                <Button
                  variant="outline"
                  size="sm"
                  className="flex-1 text-xs"
                  onClick={() => setFilterDialogOpen(true)}
                >
                  <Filter className="mr-1.5 h-3 w-3" />
                  {visibleFilters.length > 0
                    ? "Настроить фильтры"
                    : "Добавить фильтр"}
                </Button>

                <TooltipProvider delayDuration={300}>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant={hasPendingChanges ? "default" : "outline"}
                        size="icon"
                        className="h-8 w-8 shrink-0"
                        aria-label="Применить фильтры"
                        onClick={applyFilters}
                      >
                        <Search className="h-3.5 w-3.5" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent side="bottom">Применить фильтры (Enter)</TooltipContent>
                  </Tooltip>
                </TooltipProvider>

                {activeCount > 0 && (
                  <TooltipProvider delayDuration={300}>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 shrink-0 text-muted-foreground"
                          onClick={resetAllFilters}
                        >
                          <RotateCcw className="h-3.5 w-3.5" />
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent side="bottom">Сбросить все</TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                )}
              </div>

              {/* Built-in period filter (document lists only) */}
              {renderPeriodSection()}

              {visibleFilters.map((m) => renderFilter(m))}

              {visibleFilters.length === 0 && !periodField && (
                <div className="rounded-md border border-dashed p-4 text-center text-xs text-muted-foreground">
                  Фильтры не настроены
                </div>
              )}

              {fieldsMeta.length > 0 && (
                <FilterConfigDialog
                  open={filterDialogOpen}
                  onOpenChange={setFilterDialogOpen}
                  availableFields={fieldsMeta}
                  selectedKeys={selectedFilterKeys}
                  onApply={(keys) => {
                    setSelectedFilterKeys(keys)
                    onFilterConfigChange?.(keys)
                    const next = { ...filterValues }
                    keys.forEach((k) => {
                      if (!next[k]) {
                        const meta = flatFieldsMetaMap.get(k)
                        next[k] = {
                          operator: getDefaultOperator(meta?.fieldType || "string"),
                          value: undefined,
                        }
                      }
                    })
                    Object.keys(next).forEach((k) => {
                      if (k !== PERIOD_FILTER_KEY && !keys.includes(k)) {
                        delete next[k]
                      }
                    })
                    setFilterValues(next)
                    onFilterValuesChange?.(next)
                    setHasPendingChanges(false)
                  }}
                />
              )}
            </div>
          </TabsContent>

          {showDetails && (
            <TabsContent value="details" className="p-3">
              {detailsContent ?? (
                <p className="text-sm text-muted-foreground">
                  Выберите элемент для просмотра деталей
                </p>
              )}
            </TabsContent>
          )}
        </Tabs>
      </ScrollArea>

      {/* Toggle button — moved to bottom */}
      <div
        className={cn(
          "flex items-center border-t h-9 mt-auto",
          isCollapsed ? "justify-center" : "justify-end px-2"
        )}
      >
        <TooltipProvider delayDuration={300}>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 shrink-0"
                onClick={toggleCollapse}
              >
                {isCollapsed ? (
                  <PanelRightOpen className="h-4 w-4" />
                ) : (
                  <PanelRightClose className="h-4 w-4" />
                )}
              </Button>
            </TooltipTrigger>
            <TooltipContent side="left">
              {isCollapsed ? "Показать панель" : "Скрыть панель"}
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>

      {columnFilterEnabled && (
        <ColumnFilterDialog
          key={`${columnFilterContext?.field?.key ?? "none"}:${columnFilterOpen ? "open" : "closed"}`}
          open={columnFilterOpen}
          onOpenChange={setColumnFilterOpen}
          columnLabel={columnFilterContext?.label ?? ""}
          field={columnFilterContext?.field ?? null}
          initialValue={columnFilterContext?.value}
          onApply={(entry) => {
            const key = columnFilterContext?.field?.key
            if (key) upsertFilter(key, entry)
          }}
        />
      )}
    </div>
  )
}
