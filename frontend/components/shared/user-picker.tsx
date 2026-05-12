"use client"

/**
 * UserPicker — combobox for selecting platform users.
 *
 * Searches against GET /auth/users?search=... which returns UserResponse[].
 * Displays fullName + email as subtitle. Returns user.id on selection.
 *
 * Analogous to ReferenceField, but specialized for the auth users endpoint
 * which returns {id, email, fullName} instead of {id, name}.
 */

import { useState, useEffect, useCallback, useRef } from "react"
import { apiFetch } from "@/lib/api"
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
import { ChevronsUpDown, X, Loader2, User } from "lucide-react"
import { cn } from "@/lib/utils"

interface PlatformUser {
  id: string
  email: string
  fullName: string
  isActive: boolean
}

interface UserListResponse {
  items: PlatformUser[]
  total: number
}

interface UserPickerProps {
  /** Currently selected user ID */
  value: string
  /** Display name for the currently selected value (controlled from outside) */
  displayName?: string
  /** Called when user selects an item — passes the user ID and display label */
  onChange: (id: string, display: string) => void
  placeholder?: string
  disabled?: boolean
  className?: string
}

// Module-level cache: id → display label
const _cache = new Map<string, string>()

export function UserPicker({
  value,
  displayName: displayNameProp,
  onChange,
  placeholder = "Поиск по имени или email…",
  disabled = false,
  className,
}: UserPickerProps) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState("")
  const [options, setOptions] = useState<PlatformUser[]>([])
  const [loading, setLoading] = useState(false)
  const [displayLabel, setDisplayLabel] = useState<string>(() => displayNameProp ?? _cache.get(value) ?? "")
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Sync displayLabel when controlled displayName prop changes
  useEffect(() => {
    if (displayNameProp) setDisplayLabel(displayNameProp)
  }, [displayNameProp])

  // Auto-resolve display label when a value is set but label unknown
  useEffect(() => {
    if (!value || displayLabel) return
    const cached = _cache.get(value)
    if (cached) { setDisplayLabel(cached); return }

    apiFetch<PlatformUser>(`/auth/users/${value}`)
      .then((u) => {
        const label = u.fullName || u.email
        _cache.set(value, label)
        setDisplayLabel(label)
      })
      .catch(() => {/* user may have been deleted */})
  }, [value]) // eslint-disable-line react-hooks/exhaustive-deps

  // Reset display label when value cleared externally
  const prevValueRef = useRef(value)
  useEffect(() => {
    if (prevValueRef.current !== value) {
      prevValueRef.current = value
      if (!value) setDisplayLabel("")
      else {
        const cached = _cache.get(value)
        if (cached) setDisplayLabel(cached)
      }
    }
  }, [value])

  const fetchUsers = useCallback(async (query: string) => {
    setLoading(true)
    try {
      const qs = query.trim()
        ? new URLSearchParams({ search: query.trim(), limit: "20" }).toString()
        : new URLSearchParams({ limit: "10" }).toString()
      const data = await apiFetch<UserListResponse>(`/auth/users?${qs}`)
      setOptions(data.items ?? [])
    } catch {
      setOptions([])
    } finally {
      setLoading(false)
    }
  }, [])

  const wasOpenRef = useRef(false)
  useEffect(() => {
    if (!open) { wasOpenRef.current = false; return }
    if (!wasOpenRef.current) {
      wasOpenRef.current = true
      fetchUsers(search)
      return
    }
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => fetchUsers(search), 200)
    return () => { if (debounceRef.current) clearTimeout(debounceRef.current) }
  }, [search, open, fetchUsers])

  const handleSelect = (user: PlatformUser) => {
    const label = user.fullName || user.email
    _cache.set(user.id, label)
    setDisplayLabel(label)
    onChange(user.id, label)
    setOpen(false)
    setSearch("")
  }

  const handleClear = (e: React.MouseEvent) => {
    e.stopPropagation()
    setDisplayLabel("")
    onChange("", "")
  }

  const shownLabel = displayLabel || (value ? `${value.slice(0, 8)}…` : "")

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className={cn(
            "w-full h-9 justify-between font-normal overflow-hidden text-sm",
            !value && "text-muted-foreground",
            className,
          )}
        >
          <span className="flex items-center gap-2 truncate flex-1 min-w-0">
            {value && <User className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
            <span className="truncate">{shownLabel || placeholder}</span>
          </span>
          <span className="flex items-center gap-0.5 shrink-0 ml-1">
            {value && !disabled && (
              <div
                role="button"
                tabIndex={-1}
                className="flex items-center justify-center p-1 -mr-1 rounded-sm hover:bg-muted/50 cursor-pointer"
                onClick={handleClear}
                onPointerDown={(e) => e.stopPropagation()}
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
            placeholder="Поиск по имени или email…"
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
                Пользователи не найдены
              </CommandEmpty>
            ) : (
              <CommandGroup>
                {options.map((user) => (
                  <CommandItem
                    key={user.id}
                    value={user.id}
                    onSelect={() => handleSelect(user)}
                    className="text-xs cursor-pointer"
                  >
                    <div className="flex flex-col min-w-0 flex-1">
                      <span className="font-medium truncate">
                        {user.fullName || user.email}
                      </span>
                      {user.fullName && (
                        <span className="text-[10px] text-muted-foreground truncate">
                          {user.email}
                        </span>
                      )}
                    </div>
                    {!user.isActive && (
                      <span className="ml-2 shrink-0 text-[10px] text-muted-foreground">
                        неактивен
                      </span>
                    )}
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
