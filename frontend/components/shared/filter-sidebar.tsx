"use client"

import { useState, useEffect, useMemo } from "react"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { Filter, FolderOpen, FileText, PanelRightClose, PanelRightOpen, X } from "lucide-react"
import { cn } from "@/lib/utils"
import { AccountingPeriodPicker, type DateRangeValue } from "@/components/ui/accounting-period-picker"
import { FilterConfigDialog, type FilterFieldMeta } from "@/components/shared/filter-config-dialog"

interface FilterConfig {
  key: string
  label: string
  type: "select" | "toggle" | "text" | "range" | "date-range"
  options?: { value: string; label: string }[]
  defaultValue?: string
}

interface FilterSidebarProps {
  filters?: FilterConfig[]
  showGroups?: boolean
  showDetails?: boolean
  /** Full list of document fields available for filtering */
  fieldsMeta?: FilterFieldMeta[]
  /** Callback when filter configuration changes */
  onFilterConfigChange?: (selectedKeys: string[]) => void
}

const STORAGE_KEY = "metapus-filter-sidebar-collapsed"

// ── Helper: convert FieldType → FilterConfig.type ───────────────────────

function fieldTypeToFilterType(
  fieldType: FilterFieldMeta["fieldType"]
): FilterConfig["type"] {
  switch (fieldType) {
    case "reference":
    case "enum":
      return "select"
    case "date":
      return "date-range"
    case "boolean":
      return "toggle"
    case "number":
      return "range"
    case "string":
    default:
      return "text"
  }
}

/** Build a FilterConfig from a FilterFieldMeta entry */
function metaToFilterConfig(meta: FilterFieldMeta): FilterConfig {
  const type = fieldTypeToFilterType(meta.fieldType)
  const cfg: FilterConfig = {
    key: meta.key,
    label: meta.label,
    type,
  }
  // For select-type filters, provide a default "Все" option
  if (type === "select") {
    cfg.options = [{ value: "all", label: "Все" }]
    cfg.defaultValue = "all"
  }
  return cfg
}

export function FilterSidebar({
  filters = [],
  showGroups = true,
  showDetails = true,
  fieldsMeta = [],
  onFilterConfigChange,
}: FilterSidebarProps) {
  const [activeFilters, setActiveFilters] = useState<Record<string, boolean>>({})
  const [isCollapsed, setIsCollapsed] = useState(true)
  const [dateRanges, setDateRanges] = useState<Record<string, DateRangeValue | undefined>>({})
  const [filterDialogOpen, setFilterDialogOpen] = useState(false)
  const [selectedFilterKeys, setSelectedFilterKeys] = useState<string[]>(
    () => filters.map((f) => f.key)
  )

  // ── Derive visible filters from selectedFilterKeys ────────────────────
  //
  // Priority: if a key matches a manually defined FilterConfig from `filters`
  // prop, use that (it has richer options). Otherwise, auto-generate from
  // fieldsMeta. This way the static filters from page.tsx serve as overrides.

  const visibleFilters: FilterConfig[] = useMemo(() => {
    const staticMap = new Map(filters.map((f) => [f.key, f]))
    const metaMap = new Map(fieldsMeta.map((m) => [m.key, m]))

    return selectedFilterKeys
      .map((key) => {
        // Prefer the static config if it exists (has proper options etc.)
        if (staticMap.has(key)) return staticMap.get(key)!
        // Otherwise, auto-generate from metadata
        const meta = metaMap.get(key)
        if (meta) return metaToFilterConfig(meta)
        return null
      })
      .filter(Boolean) as FilterConfig[]
  }, [selectedFilterKeys, filters, fieldsMeta])

  // ── Remove a single filter from the visible list ──────────────────────

  const removeFilter = (key: string) => {
    setSelectedFilterKeys((prev) => prev.filter((k) => k !== key))
    onFilterConfigChange?.(selectedFilterKeys.filter((k) => k !== key))
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

  // ── Render a single filter widget ─────────────────────────────────────

  function renderFilter(f: FilterConfig) {
    return (
      <div key={f.key} className="flex flex-col gap-1.5">
        {f.type === "toggle" && (
          <div className="flex items-center justify-between">
            <Switch
              checked={activeFilters[f.key] ?? false}
              onCheckedChange={(v) =>
                setActiveFilters((s) => ({ ...s, [f.key]: v }))
              }
            />
            <Label className="text-xs">{f.label}</Label>
            {f.options && (
              <Select defaultValue={f.defaultValue}>
                <SelectTrigger className="h-7 w-24 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {f.options.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>
        )}
        {f.type === "select" && (
          <div>
            <div className="flex items-center justify-between mb-1">
              <Label className="text-xs text-muted-foreground">
                {f.label}
              </Label>
              <Button
                variant="ghost"
                size="icon"
                className="h-4 w-4 text-muted-foreground/50 hover:text-destructive"
                onClick={() => removeFilter(f.key)}
                title="Убрать фильтр"
              >
                <X className="h-3 w-3" />
              </Button>
            </div>
            <Select defaultValue={f.defaultValue}>
              <SelectTrigger className="h-8 text-xs">
                <SelectValue placeholder={f.label} />
              </SelectTrigger>
              <SelectContent>
                {f.options?.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
        {f.type === "text" && (
          <div>
            <div className="flex items-center justify-between mb-1">
              <Label className="text-xs text-muted-foreground">
                {f.label}
              </Label>
              <Button
                variant="ghost"
                size="icon"
                className="h-4 w-4 text-muted-foreground/50 hover:text-destructive"
                onClick={() => removeFilter(f.key)}
                title="Убрать фильтр"
              >
                <X className="h-3 w-3" />
              </Button>
            </div>
            <Input
              className="h-8 text-xs"
              placeholder={f.label}
            />
          </div>
        )}
        {f.type === "range" && (
          <div>
            <div className="flex items-center justify-between mb-1">
              <Label className="text-xs text-muted-foreground">
                {f.label}
              </Label>
              <Button
                variant="ghost"
                size="icon"
                className="h-4 w-4 text-muted-foreground/50 hover:text-destructive"
                onClick={() => removeFilter(f.key)}
                title="Убрать фильтр"
              >
                <X className="h-3 w-3" />
              </Button>
            </div>
            <div className="flex items-center gap-2">
              <Input className="h-7 text-xs" placeholder="От" />
              <span className="text-xs text-muted-foreground">–</span>
              <Input className="h-7 text-xs" placeholder="до" />
            </div>
          </div>
        )}
        {f.type === "date-range" && (
          <div>
            <div className="flex items-center justify-between mb-1">
              <Label className="text-xs text-muted-foreground">
                {f.label}
              </Label>
              <Button
                variant="ghost"
                size="icon"
                className="h-4 w-4 text-muted-foreground/50 hover:text-destructive"
                onClick={() => removeFilter(f.key)}
                title="Убрать фильтр"
              >
                <X className="h-3 w-3" />
              </Button>
            </div>
            <AccountingPeriodPicker
              value={dateRanges[f.key]}
              onChange={(range) =>
                setDateRanges((s) => ({ ...s, [f.key]: range }))
              }
              placeholder="Выбрать период"
              className="h-8 w-full text-xs"
            />
          </div>
        )}
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
          "flex items-center justify-center border-b shrink-0 bg-muted/20 transition-all duration-300",
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
              {visibleFilters.map((f) => renderFilter(f))}

              {visibleFilters.length === 0 && (
                <div className="rounded-md border border-dashed p-4 text-center text-xs text-muted-foreground">
                  Фильтры не настроены
                </div>
              )}

              <Button
                variant="outline"
                size="sm"
                className="mt-2 text-xs"
                onClick={() => setFilterDialogOpen(true)}
              >
                <Filter className="mr-1.5 h-3 w-3" />
                {visibleFilters.length > 0
                  ? "Настроить фильтры"
                  : "Добавить фильтр"}
              </Button>

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
