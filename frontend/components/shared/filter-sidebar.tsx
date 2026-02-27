"use client"

import { useState, useEffect, useMemo, useCallback, useRef } from "react"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { Filter, FolderOpen, FileText, PanelRightClose, PanelRightOpen, X, RotateCcw } from "lucide-react"
import { cn } from "@/lib/utils"
import { AccountingPeriodPicker, type DateRangeValue } from "@/components/ui/accounting-period-picker"
import { DatePicker } from "@/components/ui/date-picker"
import { FilterConfigDialog, type FilterFieldMeta } from "@/components/shared/filter-config-dialog"
import { ReferenceField, type ReferenceOption } from "@/components/shared/reference-field"
import {
  type FilterValues,
  type FilterEntry,
  PERIOD_FILTER_KEY,
  getOperatorsForType,
  getDefaultOperator,
  isNullaryOperator,
  isListOperator,
} from "@/lib/filter-utils"
import type { ComparisonOperator } from "@/types/common"

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
}: FilterSidebarProps) {
  const [isCollapsed, setIsCollapsed] = useState(true)
  const [filterDialogOpen, setFilterDialogOpen] = useState(false)
  const [selectedFilterKeys, setSelectedFilterKeys] = useState<string[]>(
    () => defaultSelectedKeys ?? []
  )

  // ── Filter values state (FilterEntry per key) ───────────────────────
  const [filterValues, setFilterValues] = useState<FilterValues>({})
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Emit filter changes with debounce (300ms)
  const emitFilterChange = useCallback(
    (values: FilterValues) => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
      debounceRef.current = setTimeout(() => {
        onFilterValuesChange?.(values)
      }, 300)
    },
    [onFilterValuesChange]
  )

  // Clean up debounce on unmount
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [])

  const updateFilterEntry = useCallback(
    (key: string, entry: FilterEntry) => {
      setFilterValues((prev) => {
        const next = { ...prev, [key]: entry }
        emitFilterChange(next)
        return next
      })
    },
    [emitFilterChange]
  )

  const updateFilterOperator = useCallback(
    (key: string, fieldType: FilterFieldMeta["fieldType"], newOp: ComparisonOperator) => {
      setFilterValues((prev) => {
        const current = prev[key]
        const oldOp = current?.operator
        // Reset value when switching between incompatible operator groups
        let value: unknown = current?.value
        if (
          isNullaryOperator(newOp) ||
          (isListOperator(newOp) !== isListOperator(oldOp ?? getDefaultOperator(fieldType))) ||
          (isNullaryOperator(oldOp ?? getDefaultOperator(fieldType)))
        ) {
          value = isListOperator(newOp) ? [] : undefined
        }
        const next = { ...prev, [key]: { operator: newOp, value } }
        emitFilterChange(next)
        return next
      })
    },
    [emitFilterChange]
  )

  const resetAllFilters = useCallback(() => {
    setFilterValues({})
    onFilterValuesChange?.({})
  }, [onFilterValuesChange])

  // ── Derive visible filters from selectedFilterKeys ────────────────────

  const visibleFilters: FilterFieldMeta[] = useMemo(() => {
    const metaMap = new Map(fieldsMeta.map((m) => [m.key, m]))
    return selectedFilterKeys
      .map((key) => metaMap.get(key))
      .filter(Boolean) as FilterFieldMeta[]
  }, [selectedFilterKeys, fieldsMeta])

  // ── Remove a single filter from the visible list ────────────────────

  const removeFilter = (key: string) => {
    setSelectedFilterKeys((prev) => prev.filter((k) => k !== key))
    onFilterConfigChange?.(selectedFilterKeys.filter((k) => k !== key))
    setFilterValues((prev) => {
      const next = { ...prev }
      delete next[key]
      emitFilterChange(next)
      return next
    })
  }

  // Persist collapse state in localStorage
  useEffect(() => {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored !== null) {
      setIsCollapsed(stored === "true")
    }
  }, [])

  const toggleCollapse = () => {
    const next = !isCollapsed
    setIsCollapsed(next)
    localStorage.setItem(STORAGE_KEY, String(next))
  }

  // Count active (non-empty) filter entries
  const activeCount = useMemo(() => {
    return Object.values(filterValues).filter((entry) => {
      if (!entry) return false
      if (isNullaryOperator(entry.operator)) return true
      const v = entry.value
      if (v === undefined || v === null || v === "") return false
      if (Array.isArray(v)) return v.length > 0
      // Period range { from?, to? }
      if (typeof v === "object" && v !== null && ("from" in v || "to" in v)) {
        const r = v as { from?: string; to?: string }
        return !!(r.from || r.to)
      }
      // Reference { id, name }
      if (typeof v === "object" && v !== null && "id" in v) {
        return !!(v as { id: string }).id
      }
      return true
    }).length
  }, [filterValues])

  // ── Get current entry for a filter key ───────────────────────────

  function getEntry(meta: FilterFieldMeta): FilterEntry {
    return filterValues[meta.key] ?? {
      operator: getDefaultOperator(meta.fieldType),
      value: meta.fieldType === "boolean" ? true : undefined,
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

  // ── Value renderers per field type ───────────────────────────────

  function renderBooleanValue(meta: FilterFieldMeta, entry: FilterEntry) {
    const checked = entry.value === true || entry.value === "true"
    return (
      <div className="flex items-center gap-2">
        <Switch
          checked={checked}
          onCheckedChange={(val) => updateFilterEntry(meta.key, { operator: "eq", value: val })}
        />
        <span className="text-xs text-muted-foreground">
          {checked ? "Да" : "Нет"}
        </span>
      </div>
    )
  }

  function renderStringValue(meta: FilterFieldMeta, entry: FilterEntry) {
    return (
      <Input
        className="h-7 text-xs"
        placeholder={meta.label}
        value={(entry.value as string) ?? ""}
        onChange={(e) => updateFilterEntry(meta.key, { operator: entry.operator, value: e.target.value || undefined })}
      />
    )
  }

  function renderNumberValue(meta: FilterFieldMeta, entry: FilterEntry) {
    return (
      <Input
        className="h-7 text-xs"
        type="number"
        placeholder={meta.label}
        value={entry.value !== undefined && entry.value !== null ? String(entry.value) : ""}
        onChange={(e) => {
          const val = e.target.value ? Number(e.target.value) : undefined
          updateFilterEntry(meta.key, { operator: entry.operator, value: val })
        }}
      />
    )
  }

  function renderDateValue(meta: FilterFieldMeta, entry: FilterEntry) {
    const dateStr = typeof entry.value === "string" ? entry.value : ""
    const dateObj = dateStr ? new Date(dateStr) : undefined
    return (
      <DatePicker
        value={dateObj}
        onChange={(d) => {
          const val = d ? d.toISOString() : undefined
          updateFilterEntry(meta.key, { operator: entry.operator, value: val })
        }}
        placeholder={meta.label}
        className="h-7 text-xs"
      />
    )
  }

  /**
   * Built-in period picker (like 1C). Always visible in document lists,
   * non-removable. Stored under PERIOD_FILTER_KEY in filterValues.
   */
  function renderPeriodSection() {
    if (!periodField) return null
    const entry = filterValues[PERIOD_FILTER_KEY]
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
                  from: val.from?.toISOString(),
                  to: val.to?.toISOString(),
                },
              })
            }
          }}
          placeholder="Выбрать период"
          className="h-8 w-full text-xs"
        />
      </div>
    )
  }

  function renderReferenceSingle(meta: FilterFieldMeta, entry: FilterEntry) {
    // Value is stored as { id, name } to preserve human-readable display
    const ref = entry.value as { id: string; name: string } | undefined
    const selectedId = ref?.id ?? ""
    const selectedName = ref?.name ?? ""
    return (
      <ReferenceField
        value={selectedId}
        displayName={selectedName}
        apiEndpoint={meta.refEndpoint ?? ""}
        placeholder={meta.label}
        compact
        onChange={(id, name) =>
          updateFilterEntry(meta.key, {
            operator: entry.operator,
            value: id ? { id, name } : undefined,
          })
        }
      />
    )
  }

  function renderReferenceList(meta: FilterFieldMeta, entry: FilterEntry) {
    const items: ReferenceOption[] = Array.isArray(entry.value) ? (entry.value as ReferenceOption[]) : []

    const addItem = (id: string, name: string) => {
      if (!id || items.some((i) => i.id === id)) return
      const next = [...items, { id, name }]
      updateFilterEntry(meta.key, { operator: entry.operator, value: next })
    }

    const removeItem = (id: string) => {
      const next = items.filter((i) => i.id !== id)
      updateFilterEntry(meta.key, { operator: entry.operator, value: next.length > 0 ? next : undefined })
    }

    return (
      <div className="flex flex-col gap-1">
        {items.map((item) => (
          <div key={item.id} className="flex items-center gap-1 rounded border bg-muted/40 px-1.5 py-0.5">
            <span className="text-[10px] truncate flex-1">{item.name || item.id.slice(0, 8)}</span>
            <Button
              variant="ghost"
              size="icon"
              className="h-3.5 w-3.5 shrink-0 text-muted-foreground/50 hover:text-destructive"
              onClick={() => removeItem(item.id)}
            >
              <X className="h-2.5 w-2.5" />
            </Button>
          </div>
        ))}
        <ReferenceField
          value=""
          apiEndpoint={meta.refEndpoint ?? ""}
          placeholder="Добавить…"
          compact
          onChange={(id, name) => { addItem(id, name) }}
        />
      </div>
    )
  }

  // ── Render a single filter widget ─────────────────────────────────

  function renderFilter(meta: FilterFieldMeta) {
    const entry = getEntry(meta)
    const op = entry.operator
    const nullary = isNullaryOperator(op)
    const list = isListOperator(op)

    return (
      <div key={meta.key} className="flex flex-col gap-1">
        {renderFilterHeader(meta, entry)}

        {/* Nullary operators — no value input */}
        {nullary && (
          <div className="text-[10px] italic text-muted-foreground/60 pl-0.5">
            {op === "null" ? "Поле не заполнено" : "Поле заполнено"}
          </div>
        )}

        {/* Boolean */}
        {!nullary && meta.fieldType === "boolean" && renderBooleanValue(meta, entry)}

        {/* String */}
        {!nullary && meta.fieldType === "string" && renderStringValue(meta, entry)}

        {/* Number */}
        {!nullary && meta.fieldType === "number" && renderNumberValue(meta, entry)}

        {/* Date */}
        {!nullary && meta.fieldType === "date" && renderDateValue(meta, entry)}

        {/* Reference — single select */}
        {!nullary && !list && meta.fieldType === "reference" && renderReferenceSingle(meta, entry)}

        {/* Reference — multi-select (in/nin) */}
        {!nullary && list && meta.fieldType === "reference" && renderReferenceList(meta, entry)}

        {/* Enum — treat like reference for now */}
        {!nullary && !list && meta.fieldType === "enum" && renderStringValue(meta, entry)}
      </div>
    )
  }

  return (
    <div
      className={cn(
        "flex flex-col shrink-0 border-l bg-card transition-all duration-300 ease-in-out overflow-hidden",
        isCollapsed ? "w-9" : "w-72"
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
      <div
        className={cn(
          "flex-1 overflow-auto transition-opacity duration-200",
          isCollapsed ? "opacity-0 pointer-events-none" : "opacity-100"
        )}
      >
        <Tabs defaultValue="filters">
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
            <div className="flex flex-col gap-3">
              {/* Built-in period filter (document lists only) */}
              {renderPeriodSection()}

              {visibleFilters.map((m) => renderFilter(m))}

              {visibleFilters.length === 0 && !periodField && (
                <div className="rounded-md border border-dashed p-4 text-center text-xs text-muted-foreground">
                  Фильтры не настроены
                </div>
              )}

              <div className="flex gap-2 mt-2">
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

                {activeCount > 0 && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-xs text-muted-foreground"
                    onClick={resetAllFilters}
                    title="Сбросить все фильтры"
                  >
                    <RotateCcw className="h-3 w-3" />
                  </Button>
                )}
              </div>

              {fieldsMeta.length > 0 && (
                <FilterConfigDialog
                  open={filterDialogOpen}
                  onOpenChange={setFilterDialogOpen}
                  availableFields={fieldsMeta}
                  selectedKeys={selectedFilterKeys}
                  onApply={(keys) => {
                    setSelectedFilterKeys(keys)
                    onFilterConfigChange?.(keys)
                  }}
                />
              )}
            </div>
          </TabsContent>

          {showDetails && (
            <TabsContent value="details" className="p-3">
              <p className="text-sm text-muted-foreground">
                Выберите элемент для просмотра деталей
              </p>
            </TabsContent>
          )}
        </Tabs>
      </div>

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
    </div>
  )
}
