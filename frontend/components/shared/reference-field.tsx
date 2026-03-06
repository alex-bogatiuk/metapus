"use client"

import { useState, useEffect, useCallback, useRef } from "react"
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
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { apiFetch } from "@/lib/api"
import type { ListResponse } from "@/types/common"

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
}

// ── Module-level cache: endpoint:id → name ─────────────────────────────
// Shared across all ReferenceField instances on the page.
// Avoids duplicate fetches when the same reference appears in multiple rows.
const _refNameCache = new Map<string, string>()

function cacheKey(endpoint: string, id: string): string {
  return `${endpoint}:${id}`
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
}: ReferenceFieldProps) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState("")
  const [options, setOptions] = useState<ReferenceOption[]>([])
  const [loading, setLoading] = useState(false)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

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
      .catch(() => { /* ignore — fallback to truncated UUID */ })
      .finally(() => { if (!cancelled) setResolving(false) })

    return () => { cancelled = true }
  }, [value, apiEndpoint]) // eslint-disable-line react-hooks/exhaustive-deps

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
      setLoading(true)
      try {
        const params: Record<string, string> = { limit: "20" }
        if (query.trim()) {
          params.search = query.trim()
        }
        const qs = new URLSearchParams(params).toString()
        const data = await apiFetch<ListResponse<ReferenceOption>>(`${apiEndpoint}?${qs}`)
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
    onChange(option.id, option.name)
    setOpen(false)
    setSearch("")
  }

  const handleClear = (e: React.MouseEvent) => {
    e.stopPropagation()
    setInternalName("")
    onChange("", "")
  }

  const triggerHeight = compact ? "h-7" : "h-9"
  const textSize = compact ? "text-xs" : "text-sm"

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className={cn(
            "w-full justify-between font-normal",
            triggerHeight,
            textSize,
            !value && "text-muted-foreground",
            className
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
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
