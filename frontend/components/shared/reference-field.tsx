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
import { api } from "@/lib/api"
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

/**
 * ReferenceField — combobox with search for selecting catalog references.
 *
 * Analogous to:
 * - 1С: "Поле ввода" with type restriction to a catalog
 * - ERPNext: Link field with search
 * - SAP Fiori: ValueHelp dialog
 *
 * Features:
 * - Type-ahead search against the catalog API
 * - Displays name (not UUID) for selected value
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

  // Display text: resolved name, or fallback to truncated ID
  const displayText = displayName || (value ? `${value.slice(0, 8)}…` : "")

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
        const res = await fetch(
          `${process.env.NEXT_PUBLIC_API_URL ?? "/api/v1"}${apiEndpoint}?${qs}`,
          {
            headers: buildAuthHeaders(),
          }
        )
        if (!res.ok) throw new Error("Failed to fetch")
        const data: ListResponse<ReferenceOption> = await res.json()
        setOptions(data.items ?? [])
      } catch {
        setOptions([])
      } finally {
        setLoading(false)
      }
    },
    [apiEndpoint]
  )

  // Debounced search
  useEffect(() => {
    if (!open) return
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      fetchOptions(search)
    }, 200)
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [search, open, fetchOptions])

  // Load initial options when opening
  useEffect(() => {
    if (open) {
      fetchOptions("")
    }
  }, [open, fetchOptions])

  const handleSelect = (option: ReferenceOption) => {
    onChange(option.id, option.name)
    setOpen(false)
    setSearch("")
  }

  const handleClear = (e: React.MouseEvent) => {
    e.stopPropagation()
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
            {displayText || placeholder}
          </span>
          <span className="flex items-center gap-0.5 shrink-0 ml-1">
            {value && !disabled && (
              <X
                className="h-3 w-3 text-muted-foreground/60 hover:text-destructive cursor-pointer"
                onClick={handleClear}
              />
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

// ── Auth header helper (reuses store tokens) ──────────────────────────

function buildAuthHeaders(): Record<string, string> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  }

  const tenantId = process.env.NEXT_PUBLIC_TENANT_ID ?? ""
  if (tenantId) {
    headers["X-Tenant-ID"] = tenantId
  }

  // Access auth store for token
  try {
    const { useAuthStore } = require("@/stores/useAuthStore")
    const tokens = useAuthStore.getState().tokens
    if (tokens?.accessToken) {
      headers["Authorization"] = `${tokens.tokenType || "Bearer"} ${tokens.accessToken}`
    }
  } catch {
    // Auth store not available, skip
  }

  return headers
}
