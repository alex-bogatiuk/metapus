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

/**
 * UserPicker — combobox for selecting a user from /auth/users.
 *
 * Analogous to 1С's "Поле ввода" restricted to the "Пользователи" catalog.
 * Displays human-readable representation: "FullName (email)".
 *
 * Usage:
 *   <UserPicker
 *     value={userId}
 *     displayName={userDisplayName}
 *     onChange={(id, display) => { setUserId(id); setUserName(display); }}
 *   />
 */

interface UserPickerProps {
  /** Currently selected user ID */
  value: string
  /** Human-readable display name (e.g. "Admin Admin (admin@metapus.io)") */
  displayName?: string
  /** Callback when user selects an item */
  onChange: (userId: string, displayName: string) => void
  /** Placeholder */
  placeholder?: string
  /** Additional CSS classes */
  className?: string
}

interface UserOption {
  id: string
  fullName: string
  email: string
}

function formatUserDisplay(user: UserOption): string {
  return user.fullName
    ? `${user.fullName} (${user.email})`
    : user.email
}

// Module-level cache: userId → display name
const _userNameCache = new Map<string, string>()

export function UserPicker({
  value,
  displayName,
  onChange,
  placeholder = "Выберите пользователя…",
  className,
}: UserPickerProps) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState("")
  const [options, setOptions] = useState<UserOption[]>([])
  const [loading, setLoading] = useState(false)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Self-resolution: resolve userId → display name
  const [internalName, setInternalName] = useState<string>(() => {
    if (value) {
      const cached = _userNameCache.get(value)
      if (cached) return cached
    }
    return ""
  })
  const [resolving, setResolving] = useState(false)

  // Auto-resolve unknown userId
  useEffect(() => {
    if (!value || displayName || internalName) return

    const cached = _userNameCache.get(value)
    if (cached) {
      setInternalName(cached)
      return
    }

    let cancelled = false
    setResolving(true)

    api.users.get(value)
      .then((user) => {
        if (cancelled) return
        const display = user.fullName
          ? `${user.fullName} (${user.email})`
          : user.email
        _userNameCache.set(value, display)
        setInternalName(display)
      })
      .catch(() => { /* fallback to truncated UUID */ })
      .finally(() => { if (!cancelled) setResolving(false) })

    return () => { cancelled = true }
  }, [value]) // eslint-disable-line react-hooks/exhaustive-deps

  // Reset internal name when value changes
  const prevValueRef = useRef(value)
  useEffect(() => {
    if (prevValueRef.current !== value) {
      prevValueRef.current = value
      if (!value) {
        setInternalName("")
      } else {
        const cached = _userNameCache.get(value)
        setInternalName(cached ?? "")
      }
    }
  }, [value])

  const resolvedName = displayName || internalName
  const displayText = resolvedName || (resolving ? "" : (value ? `${value.slice(0, 8)}…` : ""))

  const fetchOptions = useCallback(async (query: string) => {
    setLoading(true)
    try {
      const result = await api.users.list(query || undefined)
      setOptions(
        (result.items ?? []).map((u) => ({
          id: u.id,
          fullName: u.fullName,
          email: u.email,
        }))
      )
    } catch {
      setOptions([])
    } finally {
      setLoading(false)
    }
  }, [])

  // Fetch on open + debounced search
  const wasOpenRef = useRef(false)
  useEffect(() => {
    if (!open) {
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
  }, [search, open, fetchOptions])

  const handleSelect = (opt: UserOption) => {
    const display = formatUserDisplay(opt)
    setInternalName(display)
    _userNameCache.set(opt.id, display)
    onChange(opt.id, display)
    setOpen(false)
    setSearch("")
  }

  const handleClear = (e: React.MouseEvent) => {
    e.stopPropagation()
    setInternalName("")
    onChange("", "")
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className={cn(
            "w-full justify-between font-normal h-9 text-sm",
            !value && "text-muted-foreground",
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
            {value && (
              <div
                role="button"
                tabIndex={0}
                className="flex items-center justify-center p-1 -mr-1 rounded-sm hover:bg-muted/50 cursor-pointer"
                onClick={handleClear}
                onPointerDown={(e) => e.stopPropagation()}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
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
                {options.map((opt) => (
                  <CommandItem
                    key={opt.id}
                    value={opt.id}
                    onSelect={() => handleSelect(opt)}
                    className="text-xs cursor-pointer"
                  >
                    <div className="flex flex-col gap-0.5 w-full min-w-0">
                      <span className="truncate font-medium">
                        {opt.fullName || opt.email}
                      </span>
                      {opt.fullName && (
                        <span className="truncate text-[10px] text-muted-foreground">
                          {opt.email}
                        </span>
                      )}
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
