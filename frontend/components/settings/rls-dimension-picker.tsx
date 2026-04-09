"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { X, Search, Loader2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { api } from "@/lib/api"

// ── Dimension → catalog API mapping ──────────────────────────────────

interface CatalogItem {
  id: string
  name: string
  code?: string
}

type DimensionKey = "organization" | "warehouse" | "counterparty"

const DIMENSION_FETCHERS: Record<
  DimensionKey,
  (search: string) => Promise<CatalogItem[]>
> = {
  organization: async (search) => {
    const res = await api.organizations.list({ search: search || undefined, limit: 20 })
    return (res.items ?? []).map((o) => ({ id: o.id, name: o.name, code: o.code }))
  },
  warehouse: async (search) => {
    const res = await api.warehouses.list({ search: search || undefined, limit: 20 })
    return (res.items ?? []).map((w) => ({ id: w.id, name: w.name, code: w.code }))
  },
  counterparty: async (search) => {
    const res = await api.counterparties.list({ search: search || undefined, limit: 20 })
    return (res.items ?? []).map((c) => ({ id: c.id, name: c.name, code: c.code }))
  },
}

// ── Props ────────────────────────────────────────────────────────────

interface RlsDimensionPickerProps {
  dimensionKey: string
  selectedIds: string[]
  onChange: (ids: string[]) => void
}

// ── Component ────────────────────────────────────────────────────────

export function RlsDimensionPicker({
  dimensionKey,
  selectedIds,
  onChange,
}: RlsDimensionPickerProps) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState("")
  const [options, setOptions] = useState<CatalogItem[]>([])
  const [loading, setLoading] = useState(false)
  const [resolvedNames, setResolvedNames] = useState<Record<string, string>>({})
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(null)

  const fetcher = DIMENSION_FETCHERS[dimensionKey as DimensionKey]

  // Resolve names for already-selected IDs on mount
  useEffect(() => {
    if (!fetcher || selectedIds.length === 0) return

    const unresolvedIds = selectedIds.filter((id) => !resolvedNames[id])
    if (unresolvedIds.length === 0) return

    // Fetch with empty search to get all, then filter
    fetcher("").then((items) => {
      const nameMap: Record<string, string> = {}
      for (const item of items) {
        if (unresolvedIds.includes(item.id)) {
          nameMap[item.id] = item.name
        }
      }
      setResolvedNames((prev) => ({ ...prev, ...nameMap }))
    }).catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Debounced search
  const handleSearch = useCallback(
    (value: string) => {
      setSearch(value)
      if (!fetcher) return

      if (debounceRef.current) clearTimeout(debounceRef.current)
      debounceRef.current = setTimeout(async () => {
        setLoading(true)
        try {
          const items = await fetcher(value)
          setOptions(items)
          // Update resolved names
          const nameMap: Record<string, string> = {}
          for (const item of items) {
            nameMap[item.id] = item.name
          }
          setResolvedNames((prev) => ({ ...prev, ...nameMap }))
        } catch {
          setOptions([])
        } finally {
          setLoading(false)
        }
      }, 300)
    },
    [fetcher]
  )

  // Load initial options when popover opens
  useEffect(() => {
    if (open && options.length === 0) {
      handleSearch("")
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const handleSelect = (itemId: string) => {
    if (selectedIds.includes(itemId)) {
      onChange(selectedIds.filter((id) => id !== itemId))
    } else {
      onChange([...selectedIds, itemId])
    }
  }

  const handleRemove = (itemId: string) => {
    onChange(selectedIds.filter((id) => id !== itemId))
  }

  if (!fetcher) {
    // Fallback for unknown dimensions — show raw input
    return (
      <input
        value={selectedIds.join(", ")}
        onChange={(e) =>
          onChange(
            e.target.value
              .split(",")
              .map((s) => s.trim())
              .filter(Boolean)
          )
        }
        placeholder="UUID через запятую"
        className="h-8 w-full rounded-md border bg-background px-3 text-xs font-mono"
      />
    )
  }

  return (
    <div className="space-y-2">
      {/* Selected badges */}
      {selectedIds.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {selectedIds.map((itemId) => (
            <Badge
              key={itemId}
              variant="secondary"
              className="h-6 gap-1 text-xs font-normal"
            >
              {resolvedNames[itemId] || itemId.slice(0, 8) + "…"}
              <button
                type="button"
                className="ml-0.5 rounded-full hover:bg-muted-foreground/20"
                onClick={() => handleRemove(itemId)}
              >
                <X className="h-3 w-3" />
              </button>
            </Badge>
          ))}
        </div>
      )}

      {/* Search popover */}
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="outline"
            size="sm"
            className="h-8 w-full justify-start text-xs text-muted-foreground font-normal"
          >
            <Search className="mr-2 h-3 w-3" />
            Найти и добавить...
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[320px] p-0" align="start">
          <Command shouldFilter={false}>
            <CommandInput
              placeholder="Поиск по названию..."
              value={search}
              onValueChange={handleSearch}
              className="h-9 text-xs"
            />
            <CommandList>
              {loading ? (
                <div className="flex items-center justify-center py-4">
                  <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                </div>
              ) : (
                <>
                  <CommandEmpty className="py-3 text-center text-xs text-muted-foreground">
                    Ничего не найдено
                  </CommandEmpty>
                  <CommandGroup>
                    {options.map((item) => {
                      const isSelected = selectedIds.includes(item.id)
                      return (
                        <CommandItem
                          key={item.id}
                          value={item.id}
                          onSelect={() => handleSelect(item.id)}
                          className="text-xs"
                        >
                          <div className="flex items-center gap-2 flex-1">
                            <div
                              className={`h-3.5 w-3.5 rounded border flex items-center justify-center shrink-0 ${
                                isSelected
                                  ? "bg-primary border-primary text-primary-foreground"
                                  : "border-muted-foreground/30"
                              }`}
                            >
                              {isSelected && (
                                <svg
                                  className="h-2.5 w-2.5"
                                  fill="none"
                                  viewBox="0 0 24 24"
                                  stroke="currentColor"
                                  strokeWidth={3}
                                >
                                  <path
                                    strokeLinecap="round"
                                    strokeLinejoin="round"
                                    d="M5 13l4 4L19 7"
                                  />
                                </svg>
                              )}
                            </div>
                            <span className="truncate">{item.name}</span>
                            {item.code && (
                              <span className="ml-auto text-[10px] text-muted-foreground font-mono">
                                {item.code}
                              </span>
                            )}
                          </div>
                        </CommandItem>
                      )
                    })}
                  </CommandGroup>
                </>
              )}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </div>
  )
}
