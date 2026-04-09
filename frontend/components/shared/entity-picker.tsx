"use client"

import { useState, useEffect, useCallback, useRef, useMemo } from "react"
import { ChevronsUpDown, X, Loader2 } from "lucide-react"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { apiFetch } from "@/lib/api"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type { EntityMeta } from "@/types/metadata"
import type { CursorListResponse } from "@/types/common"

/**
 * EntityPicker — two-step reference picker for any metadata entity.
 *
 * Step 1: User selects entity type (e.g. "Поступление товаров", "Контрагент")
 * Step 2: ReferenceField-like combobox searches for a specific record
 *
 * Human-readable display after selection:
 *   "Поступление товаров: GR-SEED-01775"
 *   "Контрагент: ООО Ромашка"
 *
 * Analogous to 1С's "Форма подбора объекта" — the platform auto-resolves
 * entity representations without the developer needing to think about it.
 */

interface EntityPickerProps {
  /** Currently selected entity type key (e.g. "goods_receipt") */
  entityType: string
  /** Currently selected entity ID */
  entityId: string
  /** Human-readable display name (auto-resolved; pass for initial state) */
  displayName?: string
  /** Callback when user selects/clears */
  onChange: (entityType: string, entityId: string, displayName: string) => void
  /** Placeholder for collapsed state */
  placeholder?: string
  /** Additional CSS classes */
  className?: string
}

/** Generic list item — works for both catalogs ({id, name}) and documents ({id, number}) */
interface EntityListItem {
  id: string
  name?: string
  number?: string
  code?: string
}

/**
 * Resolves the API search endpoint for a given entity metadata entry.
 * - catalog → /catalog/{routePrefix}
 * - document → /document/{routePrefix}
 */
function resolveApiEndpoint(entity: EntityMeta): string | null {
  if (!entity.routePrefix) return null
  if (entity.type === "catalog") return `/catalog/${entity.routePrefix}`
  if (entity.type === "document") return `/document/${entity.routePrefix}`
  return null
}

/** Returns display name for an entity list item: name (catalogs) or number (documents). */
function getItemDisplayName(item: EntityListItem): string {
  return item.name || item.number || item.id.slice(0, 8) + "…"
}

/** Builds full human-readable representation: "Тип: Название". */
function buildFullDisplay(entityLabel: string, itemName: string): string {
  return `${entityLabel}: ${itemName}`
}

// Module-level cache: "entityType:entityId" → display name
const _entityDisplayCache = new Map<string, string>()

export function EntityPicker({
  entityType,
  entityId,
  displayName,
  onChange,
  placeholder = "Выберите объект…",
  className,
}: EntityPickerProps) {
  const entities = useMetadataStore((s) => s.entities)
  const getEntity = useMetadataStore((s) => s.getEntity)
  const getLabel = useMetadataStore((s) => s.getLabel)

  // ── Step 2: record picker state ──────────────────────────────────────
  const [recordOpen, setRecordOpen] = useState(false)
  const [search, setSearch] = useState("")
  const [options, setOptions] = useState<EntityListItem[]>([])
  const [loading, setLoading] = useState(false)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // ── Self-resolution of existing entityId ─────────────────────────────
  const [internalName, setInternalName] = useState<string>(() => {
    if (entityType && entityId) {
      const cached = _entityDisplayCache.get(`${entityType}:${entityId}`)
      if (cached) return cached
    }
    return ""
  })
  const [resolving, setResolving] = useState(false)

  // Compute API endpoint from selected entity type
  const selectedEntity = entityType ? getEntity(entityType) : undefined
  const apiEndpoint = useMemo(
    () => (selectedEntity ? resolveApiEndpoint(selectedEntity) : null),
    [selectedEntity],
  )
  const entityLabel = entityType ? getLabel(entityType, "singular") : ""

  // Auto-resolve: when entityId is set but no display name known
  useEffect(() => {
    if (!entityType || !entityId || !apiEndpoint) return
    if (displayName || internalName) return

    const cacheKey = `${entityType}:${entityId}`
    const cached = _entityDisplayCache.get(cacheKey)
    if (cached) {
      setInternalName(cached)
      return
    }

    let cancelled = false
    setResolving(true)

    apiFetch<EntityListItem>(`${apiEndpoint}/${entityId}`)
      .then((data) => {
        if (cancelled) return
        const itemName = getItemDisplayName(data)
        _entityDisplayCache.set(cacheKey, itemName)
        setInternalName(itemName)
      })
      .catch(() => { /* fallback to truncated UUID */ })
      .finally(() => { if (!cancelled) setResolving(false) })

    return () => { cancelled = true }
  }, [entityType, entityId, apiEndpoint]) // eslint-disable-line react-hooks/exhaustive-deps

  // Reset when entityType changes
  const prevTypeRef = useRef(entityType)
  useEffect(() => {
    if (prevTypeRef.current !== entityType) {
      prevTypeRef.current = entityType
      setInternalName("")
    }
  }, [entityType])

  // Reset when entityId cleared
  const prevIdRef = useRef(entityId)
  useEffect(() => {
    if (prevIdRef.current !== entityId) {
      prevIdRef.current = entityId
      if (!entityId) {
        setInternalName("")
      } else {
        const cached = _entityDisplayCache.get(`${entityType}:${entityId}`)
        setInternalName(cached ?? "")
      }
    }
  }, [entityId, entityType])

  // Display text
  const resolvedItemName = displayName || internalName
  const fullDisplay = resolvedItemName && entityLabel
    ? buildFullDisplay(entityLabel, resolvedItemName)
    : ""

  // ── Fetch records from entity API ────────────────────────────────────
  const fetchOptions = useCallback(
    async (query: string) => {
      if (!apiEndpoint) return
      setLoading(true)
      try {
        const params: Record<string, string> = { limit: "20" }
        if (query.trim()) params.search = query.trim()
        const qs = new URLSearchParams(params).toString()
        const data = await apiFetch<CursorListResponse<EntityListItem>>(`${apiEndpoint}?${qs}`)
        setOptions(data.items ?? [])
      } catch {
        setOptions([])
      } finally {
        setLoading(false)
      }
    },
    [apiEndpoint],
  )

  // Fetch on open + debounced search
  const wasOpenRef = useRef(false)
  useEffect(() => {
    if (!recordOpen) {
      wasOpenRef.current = false
      return
    }
    if (!wasOpenRef.current) {
      wasOpenRef.current = true
      fetchOptions(search)
      return
    }
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => fetchOptions(search), 200)
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current) }
  }, [search, recordOpen, fetchOptions])

  // Handle entity type change (step 1)
  const handleTypeChange = (newType: string) => {
    // Clear record when type changes
    onChange(newType, "", "")
    setInternalName("")
    setOptions([])
  }

  // Handle record select (step 2)
  const handleSelectRecord = (item: EntityListItem) => {
    const itemName = getItemDisplayName(item)
    const cacheKey = `${entityType}:${item.id}`
    _entityDisplayCache.set(cacheKey, itemName)
    setInternalName(itemName)
    onChange(entityType, item.id, itemName)
    setRecordOpen(false)
    setSearch("")
  }

  // Clear everything
  const handleClear = (e: React.MouseEvent) => {
    e.stopPropagation()
    setInternalName("")
    onChange("", "", "")
  }

  // Clear only record (keep type)
  const handleClearRecord = (e: React.MouseEvent) => {
    e.stopPropagation()
    setInternalName("")
    onChange(entityType, "", "")
  }

  // ── Collapsed view: show "Тип: Представление" ────────────────────────
  if (entityType && entityId && fullDisplay) {
    return (
      <div className={cn("flex items-center gap-1", className)}>
        <div className="flex-1 flex items-center gap-1.5 px-3 h-9 rounded-md border bg-background text-sm min-w-0">
          <span className="truncate">{fullDisplay}</span>
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="h-9 w-9 shrink-0"
          onClick={handleClear}
          title="Очистить"
        >
          <X className="h-3.5 w-3.5 text-muted-foreground hover:text-destructive" />
        </Button>
      </div>
    )
  }

  // ── Expanded view: two-step picker ───────────────────────────────────
  return (
    <div className={cn("flex items-center gap-2", className)}>
      {/* Step 1: Entity type selector */}
      <Select value={entityType} onValueChange={handleTypeChange}>
        <SelectTrigger className="w-[200px] h-9 text-sm shrink-0">
          <SelectValue placeholder="Все объекты" />
        </SelectTrigger>
        <SelectContent>
          {entities.map((e) => (
            <SelectItem key={e.key} value={e.key} className="text-xs">
              {e.presentation.singular}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {/* Step 2: Record picker (enabled only when type selected) */}
      <Popover open={recordOpen} onOpenChange={setRecordOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="outline"
            role="combobox"
            aria-expanded={recordOpen}
            disabled={!entityType}
            className={cn(
              "flex-1 justify-between font-normal h-9 text-sm min-w-[180px]",
              !entityId && "text-muted-foreground",
            )}
          >
            <span className="truncate flex-1 text-left">
              {resolving && !resolvedItemName ? (
                <span className="inline-flex items-center gap-1 text-muted-foreground">
                  <Loader2 className="h-3 w-3 animate-spin" />
                </span>
              ) : (
                resolvedItemName || (entityType ? "Выберите запись…" : "Сначала выберите тип")
              )}
            </span>
            <span className="flex items-center gap-0.5 shrink-0 ml-1">
              {entityId && (
                <div
                  role="button"
                  tabIndex={0}
                  className="flex items-center justify-center p-1 -mr-1 rounded-sm hover:bg-muted/50 cursor-pointer"
                  onClick={handleClearRecord}
                  onPointerDown={(e) => e.stopPropagation()}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.stopPropagation()
                      onChange(entityType, "", "")
                    }
                  }}
                >
                  <X className="h-3.5 w-3.5 text-muted-foreground/60 hover:text-destructive" />
                </div>
              )}
              <ChevronsUpDown className="h-3 w-3 text-muted-foreground/60" />
            </span>
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[--radix-popover-trigger-width] p-0" align="start">
          <Command shouldFilter={false}>
            <CommandInput
              placeholder="Поиск…"
              value={search}
              onValueChange={setSearch}
              className="h-8 text-xs"
            />
            <CommandList>
              {loading ? (
                <div className="flex items-center justify-center py-4 text-xs text-muted-foreground">
                  <Loader2 className="mr-2 h-3 w-3 animate-spin" />
                  Загрузка…
                </div>
              ) : options.length === 0 ? (
                <CommandEmpty className="py-4 text-center text-xs text-muted-foreground">
                  Ничего не найдено
                </CommandEmpty>
              ) : (
                <CommandGroup>
                  {options.map((opt) => (
                    <CommandItem
                      key={opt.id}
                      value={opt.id}
                      onSelect={() => handleSelectRecord(opt)}
                      className="text-xs cursor-pointer"
                    >
                      <div className="flex items-center gap-2 w-full min-w-0">
                        {opt.code && (
                          <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                            {opt.code}
                          </span>
                        )}
                        <span className="truncate">{getItemDisplayName(opt)}</span>
                      </div>
                    </CommandItem>
                  ))}
                </CommandGroup>
              )}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </div>
  )
}
