"use client"

import * as React from "react"
import { startOfWeek, startOfMonth, endOfMonth, addDays } from "date-fns"
import { ru } from "date-fns/locale"
import { Calendar as CalendarIcon, XIcon } from "lucide-react"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Calendar } from "@/components/ui/calendar"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { applyDateMask, parseSmartDate, tryParse, fmtDateStr } from "@/lib/date-parser"

export type DateShortcutContext = "past" | "future" | "any"

export interface DateShortcut {
  label: string
  date: () => Date
}

export interface DatePickerProps {
  /** Currently selected date */
  value?: Date | null
  /** Called when user selects a date */
  onChange?: (date: Date | undefined) => void
  /** Placeholder text when no date selected */
  placeholder?: string
  /** Additional CSS classes for the wrapper */
  className?: string
  /** Disabled state */
  disabled?: boolean
  /** Show contextual date shortcuts in the popover */
  shortcuts?: DateShortcutContext | DateShortcut[]
}

const PAST_SHORTCUTS: DateShortcut[] = [
  { label: "Сегодня", date: () => new Date() },
  { label: "Вчера", date: () => addDays(new Date(), -1) },
  { label: "Начало недели", date: () => startOfWeek(new Date(), { weekStartsOn: 1 }) },
  { label: "Начало месяца", date: () => startOfMonth(new Date()) },
]

const FUTURE_SHORTCUTS: DateShortcut[] = [
  { label: "Сегодня", date: () => new Date() },
  { label: "Завтра", date: () => addDays(new Date(), 1) },
  { label: "+7 дней", date: () => addDays(new Date(), 7) },
  { label: "+30 дней", date: () => addDays(new Date(), 30) },
  { label: "Конец месяца", date: () => endOfMonth(new Date()) },
]

const ANY_SHORTCUTS: DateShortcut[] = [
  { label: "Сегодня", date: () => new Date() },
  { label: "Начало месяца", date: () => startOfMonth(new Date()) },
  { label: "Конец месяца", date: () => endOfMonth(new Date()) },
]

/**
 * DatePicker — smart keyboard-first date input for Metapus ERP.
 */
export function DatePicker({
  value,
  onChange,
  placeholder,
  className,
  disabled = false,
  shortcuts,
}: DatePickerProps) {
  const [open, setOpen] = React.useState(false)
  
  // React hydration safety for persisted Zustand store: fallback to dd.MM.yyyy until loaded
  const dateFormat = useUserPrefsStore((s) => s.interface.dateFormat) ?? "dd.MM.yyyy"

  // Local state for the input field to allow partial typing
  const [localValue, setLocalValue] = React.useState("")

  // Sync with external value when it changes
  React.useEffect(() => {
    // If external value is null or undefined, empty the field
    setLocalValue(value ? fmtDateStr(value, dateFormat) : "")
  }, [value, dateFormat])

  const activeShortcuts = React.useMemo(() => {
    if (!shortcuts) return null
    if (Array.isArray(shortcuts)) return shortcuts
    if (shortcuts === "past") return PAST_SHORTCUTS
    if (shortcuts === "future") return FUTURE_SHORTCUTS
    return ANY_SHORTCUTS
  }, [shortcuts])

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setLocalValue(applyDateMask(e.target.value, dateFormat))
  }

  const handleCommit = () => {
    const rawDigits = localValue.replace(/\D/g, "")

    // If empty, clear the value
    if (!rawDigits) {
      setLocalValue("")
      onChange?.(undefined)
      return
    }

    // Try to smart parse
    const smartParsed = parseSmartDate(localValue, dateFormat)
    if (smartParsed) {
      setLocalValue(smartParsed)
      const parsedDate = tryParse(smartParsed, dateFormat)
      if (parsedDate) onChange?.(parsedDate)
    } else {
      // Invalid date -> revert to last known good value
      setLocalValue(value ? fmtDateStr(value, dateFormat) : "")
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault()
      handleCommit()
    }
  }

  const handleClear = () => {
    setLocalValue("")
    onChange?.(undefined)
  }

  return (
    <div className={cn("relative flex items-center w-full", className)}>
      <Input
        value={localValue}
        onChange={handleChange}
        onBlur={handleCommit}
        onKeyDown={handleKeyDown}
        placeholder={placeholder ?? dateFormat.toLowerCase()}
        className="w-full pr-14 font-mono text-sm"
        maxLength={10}
        disabled={disabled}
      />

      <div className="absolute right-1 flex items-center">
        {localValue && !disabled && (
          <button
            type="button"
            onClick={handleClear}
            className="flex items-center justify-center size-6 text-muted-foreground hover:text-foreground transition-colors mr-1"
            aria-label="Очистить дату"
          >
            <XIcon className="size-3" />
          </button>
        )}

        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <button
              type="button"
              disabled={disabled}
              className="flex items-center justify-center size-6 text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
              aria-label="Открыть календарь"
            >
              <CalendarIcon className="size-4" />
            </button>
          </PopoverTrigger>
          <PopoverContent className="w-auto p-0" align="end">
            <div className={cn("flex", activeShortcuts ? "flex-row" : "flex-col")}>
              {activeShortcuts && (
                <div className="flex flex-col gap-1 border-r border-border p-3 w-[140px]">
                  {activeShortcuts.map((sc, i) => (
                    <Button
                      key={i}
                      type="button"
                      variant="ghost"
                      className="justify-start px-2 font-normal text-sm w-full h-8"
                      onClick={() => {
                        onChange?.(sc.date())
                        setOpen(false)
                      }}
                    >
                      {sc.label}
                    </Button>
                  ))}
                </div>
              )}
              <Calendar
                mode="single"
                selected={value || undefined}
                onSelect={(date) => {
                  onChange?.(date)
                  setOpen(false)
                }}
                locale={ru}
                initialFocus
              />
            </div>
          </PopoverContent>
        </Popover>
      </div>
    </div>
  )
}
