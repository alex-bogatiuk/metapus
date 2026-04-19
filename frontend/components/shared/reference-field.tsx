"use client"

import { useState, useEffect, useCallback, useRef, useMemo } from "react"
import { useRouter } from "next/navigation"
import { ChevronsUpDown, X, Loader2, ExternalLink } from "lucide-react"
import { ReferencePickerDialog } from "./reference-picker-dialog"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { apiFetch } from "@/lib/api"
import { useCompactMode } from "@/hooks/useCompactMode"
import { resolveReferenceUrl } from "@/lib/reference-utils"
import { resolveTitleFromUrl } from "@/lib/tab-utils"
import { useTabsStore } from "@/stores/useTabsStore"
import type { CursorListResponse } from "@/types/common"

/**
 * Option item for the reference field dropdown.
 * Matches the {id, name} shape returned by catalog list endpoints.
 */
export interface ReferenceOption {
  id: string
  name: string
  code?: string
}

interface ReferenceFieldProps {
  /** Currently selected ID */
  value: string
  /** Display name for the currently selected value (from resolved refs) */
  displayName?: string
  /** Called when user selects an item */
  onChange: (id: string, display: string) => void
  /** API endpoint path to search (e.g. "/catalog/counterparties") */
  apiEndpoint: string
  /** Placeholder text */
  placeholder?: string
  /** Additional CSS classes for the trigger button */
  className?: string
  /** Compact mode for table cells */
  compact?: boolean
  /** Disabled state */
  disabled?: boolean
  /** Error message to display and style the field with a red border */
  error?: string
}

// ── Module-level cache: endpoint:id → name ─────────────────────────────
// Shared across all ReferenceField instances on the page.
// Avoids duplicate fetches when the same reference appears in multiple rows.
const _refNameCache = new Map<string, string>()

function cacheKey(endpoint: string, id: string): string {
  return `${endpoint}:${id}`
}

// ── Recent selections: last 5 per apiEndpoint (like 1C) ────────────────
// Stored in localStorage to persist across sessions.
const RECENT_STORAGE_PREFIX = "metapus-ref-recent:"
const RECENT_MAX = 5

function getRecentSelections(apiEndpoint: string): ReferenceOption[] {
  if (typeof window === "undefined") return []
  try {
    const raw = localStorage.getItem(RECENT_STORAGE_PREFIX + apiEndpoint)
    return raw ? JSON.parse(raw) as ReferenceOption[] : []
  } catch {
    return []
  }
}

function pushRecentSelection(apiEndpoint: string, item: ReferenceOption): void {
  if (typeof window === "undefined" || !item.id) return
  try {
    const prev = getRecentSelections(apiEndpoint)
    // Remove duplicate, prepend new, cap at RECENT_MAX
    const next = [item, ...prev.filter((r) => r.id !== item.id)].slice(0, RECENT_MAX)
    localStorage.setItem(RECENT_STORAGE_PREFIX + apiEndpoint, JSON.stringify(next))
  } catch {
    // ignore quota errors
  }
}

/**
 * ReferenceField — combobox with search for selecting catalog references.
 *
 * Self-resolving: the component internally remembers selected names and
 * auto-resolves unknown IDs from the API, so parent components never need
 * to manage display names manually.
 *
 * Analogous to:
 * - 1С: "Поле ввода" with type restriction to a catalog
 * - ERPNext: Link field with search
 * - SAP Fiori: ValueHelp dialog
 *
 * Features:
 * - Type-ahead search against the catalog API
 * - Displays name (not UUID) for selected value
 * - Self-resolving: remembers name after selection + auto-fetches on mount
 * - Clear button
 * - Keyboard navigable (Enter to open, type to search)
 */
export function ReferenceField({
  value,
  displayName,
  onChange,
  apiEndpoint,
  placeholder = "Выберите…",
  className,
  compact = false,
  disabled = false,
  error,
}: ReferenceFieldProps) {
  const [open, setOpen] = useState(false)
  const [pickerOpen, setPickerOpen] = useState(false)
  const [search, setSearch] = useState("")
  const [options, setOptions] = useState<ReferenceOption[]>([])
  const [loading, setLoading] = useState(false)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // ── Reference navigation ──────────────────────────────────────────────
  const router = useRouter()
  const openTab = useTabsStore((s) => s.openTab)

  const referenceUrl = useMemo(
    () => resolveReferenceUrl(apiEndpoint, value),
    [apiEndpoint, value],
  )

  const handleOpenReference = useCallback(
    (e: React.MouseEvent | React.KeyboardEvent) => {
      e.stopPropagation()
      e.preventDefault()
      if (!referenceUrl) return

      if ("ctrlKey" in e && (e.ctrlKey || e.metaKey)) {
        window.open(referenceUrl, "_blank")
      } else {
        openTab({
          id: referenceUrl,
          title: resolveTitleFromUrl(referenceUrl),
          url: referenceUrl,
        })
        router.push(referenceUrl)
      }
    },
    [referenceUrl, openTab, router],
  )

  // ── Self-resolution: internal name tracking ──────────────────────────
  // Stores the display name for the currently selected value.
  // Priority: displayName prop > internalName > auto-resolve > truncated UUID.
  const [internalName, setInternalName] = useState<string>(() => {
    if (value) {
      const cached = _refNameCache.get(cacheKey(apiEndpoint, value))
      if (cached) return cached
    }
    return ""
  })
  const [resolving, setResolving] = useState(false)

  // Auto-resolve: when value is present but no name is known, fetch from API
  useEffect(() => {
    if (!value || displayName || internalName) return

    const key = cacheKey(apiEndpoint, value)
    const cached = _refNameCache.get(key)
    if (cached) {
      setInternalName(cached)
      return
    }

    let cancelled = false
    setResolving(true)

    apiFetch<{ name?: string }>(`${apiEndpoint}/${value}`)
      .then((data) => {
        if (cancelled) return
        if (data?.name) {
          _refNameCache.set(key, data.name)
          setInternalName(data.name)
        }
      })
      .catch((err) => {
        if (err?.status === 404) {
          onChange("", "")
          const recent = getRecentSelections(apiEndpoint)
          const cleaned = recent.filter((r) => r.id !== value)
          localStorage.setItem(RECENT_STORAGE_PREFIX + apiEndpoint, JSON.stringify(cleaned))
        }
      })
      .finally(() => { if (!cancelled) setResolving(false) })

    return () => { cancelled = true }
  }, [value, apiEndpoint]) // eslint-disable-line react-hooks/exhaustive-deps

  // Self-heal localStorage recent cache when a fresh displayName arrives for the same value
  // (e.g. after save, server returns updated names that differ from what's cached)
  useEffect(() => {
    if (!value || !displayName) return
    // Update module-level cache
    _refNameCache.set(cacheKey(apiEndpoint, value), displayName)
    // Update localStorage recent entries if the name changed
    const recent = getRecentSelections(apiEndpoint)
    const existing = recent.find((r) => r.id === value)
    if (existing && existing.name !== displayName) {
      const updated = recent.map((r) => r.id === value ? { ...r, name: displayName } : r)
      localStorage.setItem(RECENT_STORAGE_PREFIX + apiEndpoint, JSON.stringify(updated))
    }
  }, [value, displayName, apiEndpoint])

  // If an external error is provided (e.g. backend FK violation) for a specific value,
  // it indicates the value was likely deleted or invalid on the backend.
  // Remove it from recents so it doesn't get suggested again.
  useEffect(() => {
    if (error && value) {
      const recent = getRecentSelections(apiEndpoint)
      if (recent.some((r) => r.id === value)) {
        const cleaned = recent.filter((r) => r.id !== value)
        localStorage.setItem(RECENT_STORAGE_PREFIX + apiEndpoint, JSON.stringify(cleaned))
      }
    }
  }, [error, value, apiEndpoint])

  // Reset internal name when value changes to a different ID
  const prevValueRef = useRef(value)
  useEffect(() => {
    if (prevValueRef.current !== value) {
      prevValueRef.current = value
      if (!value) {
        setInternalName("")
      } else {
        // Check cache for new value
        const cached = _refNameCache.get(cacheKey(apiEndpoint, value))
        setInternalName(cached ?? "")
      }
    }
  }, [value, apiEndpoint])

  // Display text: displayName prop > internalName > resolving spinner > truncated ID
  const resolvedName = displayName || internalName
  const displayText = resolvedName || (resolving ? "" : (value ? `${value.slice(0, 8)}…` : ""))

  // Fetch options from the catalog API with search
  const fetchOptions = useCallback(
    async (query: string) => {
      // When search is empty — show recent selections if available,
      // otherwise fetch first 5 items as initial suggestions (like 1C)
      if (!query.trim()) {
        const recent = getRecentSelections(apiEndpoint)
        if (recent.length > 0) {
          // Recent history exists — show it directly, no API call
          setOptions([])
          setLoading(false)
          return
        }
        // No history yet — fetch first 5 items as initial suggestions
        setLoading(true)
        try {
          const qs = new URLSearchParams({ limit: "5" }).toString()
          const data = await apiFetch<CursorListResponse<ReferenceOption>>(`${apiEndpoint}?${qs}`)
          setOptions(data.items ?? [])
        } catch {
          setOptions([])
        } finally {
          setLoading(false)
        }
        return
      }
      setLoading(true)
      try {
        const params: Record<string, string> = { limit: "20" }
        params.search = query.trim()
        const qs = new URLSearchParams(params).toString()
        const data = await apiFetch<CursorListResponse<ReferenceOption>>(`${apiEndpoint}?${qs}`)
        setOptions(data.items ?? [])
      } catch {
        setOptions([])
      } finally {
        setLoading(false)
      }
    },
    [apiEndpoint]
  )

  // ⚡ Perf: single merged effect replaces two separate effects that both fired on open,
  // causing duplicate API calls. Now: immediate fetch on open, debounced on search changes.
  const wasOpenRef = useRef(false)

  useEffect(() => {
    if (!open) {
      wasOpenRef.current = false
      return
    }

    // Just opened — fetch immediately without debounce delay
    if (!wasOpenRef.current) {
      wasOpenRef.current = true
      fetchOptions(search)
      return
    }

    // Search text changed while already open — debounce
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      fetchOptions(search)
    }, 200)
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [search, open, fetchOptions])

  const handleSelect = (option: ReferenceOption) => {
    // Store in internal state + module cache so name is never lost
    setInternalName(option.name)
    _refNameCache.set(cacheKey(apiEndpoint, option.id), option.name)
    // Save to recent selections history (like 1C)
    pushRecentSelection(apiEndpoint, option)
    onChange(option.id, option.name)
    setOpen(false)
    setSearch("")
  }

  const handleClear = (e: React.MouseEvent) => {
    e.stopPropagation()
    setInternalName("")
    onChange("", "")
  }

  const globalCompact = useCompactMode()
  // compact prop = table cell context; globalCompact = user preference.
  // Table cell: h-7 normal → h-6 compact. Form field: h-9 normal → h-7 compact.
  const triggerHeight = compact
    ? (globalCompact ? "h-6" : "h-7")
    : (globalCompact ? "h-7" : "h-9")
  const textSize = compact ? "text-xs" : "text-sm"

  return (
    <>
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className={cn(
            "w-full justify-between truncate font-normal",
            triggerHeight,
            textSize,
            !value && "text-muted-foreground",
            error && "border-destructive focus-visible:ring-destructive",
            className,
          )}
        >
          <span className="truncate flex-1 text-left">
            {resolving && !resolvedName ? (
              <span className="inline-flex items-center gap-1 text-muted-foreground">
                <Loader2 className="h-3 w-3 animate-spin" />
              </span>
            ) : (
              displayText || placeholder
            )}
          </span>
          <span className="flex items-center gap-0.5 shrink-0 ml-1">
            {value && !disabled && referenceUrl && (
              <div
                role="button"
                tabIndex={0}
                className={cn(
                  "flex items-center justify-center p-1 rounded-sm hover:bg-muted/50 cursor-pointer transition-opacity",
                  compact && "opacity-0 group-hover:opacity-100",
                )}
                onClick={handleOpenReference}
                onPointerDown={(e) => e.stopPropagation()}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    handleOpenReference(e)
                  }
                }}
                title="Открыть в новой вкладке"
              >
                <ExternalLink className="h-3 w-3 text-muted-foreground/60 hover:text-primary" />
              </div>
            )}
            {value && !disabled && (
              <div
                role="button"
                tabIndex={0}
                className="flex items-center justify-center p-1 -mr-1 rounded-sm hover:bg-muted/50 cursor-pointer"
                onClick={handleClear}
                onPointerDown={(e) => e.stopPropagation()}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.stopPropagation()
                    onChange("", "")
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
            ) : !search.trim() ? (
              /* No search text — show recent selections or initial suggestions */
              (() => {
                const recent = getRecentSelections(apiEndpoint)
                if (recent.length > 0) {
                  return (
                    <CommandGroup heading="Недавние">
                      {recent.map((opt) => (
                        <CommandItem
                          key={opt.id}
                          value={opt.id}
                          onSelect={() => handleSelect(opt)}
                          className="text-xs cursor-pointer"
                        >
                          <div className="flex items-center gap-2 w-full min-w-0">
                            {opt.code && (
                              <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                                {opt.code}
                              </span>
                            )}
                            <span className="truncate">{opt.name}</span>
                          </div>
                        </CommandItem>
                      ))}
                    </CommandGroup>
                  )
                }
                // No recent history — show initial suggestions from API
                if (options.length > 0) {
                  return (
                    <CommandGroup>
                      {options.map((opt) => (
                        <CommandItem
                          key={opt.id}
                          value={opt.id}
                          onSelect={() => handleSelect(opt)}
                          className="text-xs cursor-pointer"
                        >
                          <div className="flex items-center gap-2 w-full min-w-0">
                            {opt.code && (
                              <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                                {opt.code}
                              </span>
                            )}
                            <span className="truncate">{opt.name}</span>
                          </div>
                        </CommandItem>
                      ))}
                    </CommandGroup>
                  )
                }
                return (
                  <div className="py-4 text-center text-xs text-muted-foreground">
                    Нет данных
                  </div>
                )
              })()
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
                    onSelect={() => handleSelect(opt)}
                    className="text-xs cursor-pointer"
                  >
                    <div className="flex items-center gap-2 w-full min-w-0">
                      {opt.code && (
                        <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                          {opt.code}
                        </span>
                      )}
                      <span className="truncate">{opt.name}</span>
                    </div>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {/* "Show All" link — opens full picker dialog */}
            <div className="border-t px-2 py-1.5">
              <button
                type="button"
                className="w-full text-xs text-primary hover:underline text-left cursor-pointer"
                onPointerDown={(e) => e.preventDefault()}
                onClick={() => { setPickerOpen(true); setOpen(false) }}
              >
                Показать все…
              </button>
            </div>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
    {error && (
      <div className="text-[0.8rem] font-medium text-destructive mt-1">
        {error}
      </div>
    )}

    {/* Picker dialog — metadata-driven full list with search, sort, pagination */}
    <ReferencePickerDialog
      open={pickerOpen}
      onOpenChange={setPickerOpen}
      apiEndpoint={apiEndpoint}
      onSelect={(id, name) => {
        handleSelect({ id, name })
        setPickerOpen(false)
      }}
    />
    </>
  )
}
