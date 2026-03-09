"use client"

import * as React from "react"
import {
  format,
  parse,
  startOfMonth,
  endOfMonth,
  isValid,
  isSameDay,
  isBefore,
  isAfter,
} from "date-fns"
import { CalendarIcon, XIcon, ChevronLeftIcon, ChevronRightIcon } from "lucide-react"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface DateRangeValue {
  from?: Date
  to?: Date
}

export interface AccountingPeriodPickerProps {
  /** Current selected period */
  value?: DateRangeValue
  /** Callback when the period is confirmed via "Выбрать" */
  onChange?: (value: DateRangeValue) => void
  /** Placeholder text shown on the trigger button when no dates are selected */
  placeholder?: string
  className?: string
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const DATE_FORMAT = "dd.MM.yyyy"

const MONTH_LABELS = [
  "Янв", "Фев", "Мар",
  "Апр", "Май", "Июн",
  "Июл", "Авг", "Сен",
  "Окт", "Ноя", "Дек",
]

/** Apply a dd.MM.yyyy mask as the user types digits */
function applyDateMask(raw: string): string {
  const digits = raw.replace(/\D/g, "").slice(0, 8)
  let result = ""
  for (let i = 0; i < digits.length; i++) {
    if (i === 2 || i === 4) result += "."
    result += digits[i]
  }
  return result
}

/** Try to parse a dd.MM.yyyy string into a valid Date */
function tryParse(text: string): Date | undefined {
  if (text.length !== 10) return undefined
  const d = parse(text, DATE_FORMAT, new Date())
  return isValid(d) ? d : undefined
}

/** Format a Date to dd.MM.yyyy, or return empty string */
function fmt(date: Date | undefined): string {
  if (!date) return ""
  return format(date, DATE_FORMAT)
}

/** 
 * Smart parsing for 1C-style date entry.
 * Takes a raw input string (can be partial digits like "0503" or "5")
 * and attempts to expand it to a full dd.MM.yyyy string based on current date.
 */
function parseSmartDate(raw: string): string | null {
  const digits = raw.replace(/\D/g, "")
  if (!digits) return null

  const now = new Date()
  const currentMonth = format(now, "MM")
  const currentYearStr = format(now, "yyyy")

  let dStr = "", mStr = "", yStr = ""

  if (digits.length <= 2) {
    // Only day provided (e.g. "5" -> 05.currentMonth.currentYear)
    dStr = digits.padStart(2, "0")
    mStr = currentMonth
    yStr = currentYearStr
  } else if (digits.length <= 4) {
    // Day and month provided (e.g. "0503" -> 05.03.currentYear or "53" -> 05.03.currentYear)
    dStr = digits.slice(0, 2).padStart(2, "0")
    if (digits.length === 3) {
      // e.g., "503" -> 05.03.currentYear
      dStr = `0${digits[0]}`
      mStr = digits.slice(1, 3).padStart(2, "0")
    } else {
      mStr = digits.slice(2, 4).padStart(2, "0")
    }
    yStr = currentYearStr
  } else if (digits.length <= 6) {
    // Day, month, and short year (e.g. "050325" -> 05.03.2025)
    dStr = digits.slice(0, 2).padStart(2, "0")
    // handle 5 digits like 05.03.2 -> not standard, let's just pad
    if (digits.length === 5) {
      mStr = digits.slice(2, 4).padStart(2, "0")
      yStr = `200${digits[4]}`
    } else {
      mStr = digits.slice(2, 4).padStart(2, "0")
      yStr = `20${digits.slice(4, 6)}` // Assume 20xx for 2-digit years
    }
  } else {
    // Full date provided (7-8 digits)
    dStr = digits.slice(0, 2).padStart(2, "0")
    if (digits.length === 7) {
      mStr = digits.slice(2, 4).padStart(2, "0")
      // if they typed 0503202 instead of 2026, let's just use what they typed
      yStr = digits.slice(4, 8).padEnd(4, "0")
    } else {
      mStr = digits.slice(2, 4).padStart(2, "0")
      yStr = digits.slice(4, 8)
    }
  }

  const formatted = `${dStr}.${mStr}.${yStr}`

  // Verify it's an actually valid calendar date by parsing it back
  const parsedDate = parse(formatted, DATE_FORMAT, new Date())
  if (!isValid(parsedDate)) {
    return null
  }

  return formatted
}

/** Convert year + monthIndex to a comparable number (e.g. 2026*12 + 1 = 24313) */
function monthKey(year: number, monthIndex: number): number {
  return year * 12 + monthIndex
}

/** Get the month key from a Date */
function monthKeyFromDate(d: Date): number {
  return d.getFullYear() * 12 + d.getMonth()
}

type MonthRole = "start" | "end" | "middle" | "single" | null

/** Determine the role of a month cell in the current range selection */
function getMonthRole(
  year: number,
  monthIndex: number,
  from: Date | undefined,
  to: Date | undefined,
): MonthRole {
  if (!from) return null
  const cellKey = monthKey(year, monthIndex)
  const fromKey = monthKeyFromDate(from)

  if (!to) {
    // Only "from" is set (first click done, waiting for second)
    if (cellKey === fromKey) return "single"
    return null
  }

  const toKey = monthKeyFromDate(to)

  if (fromKey === toKey && cellKey === fromKey) return "single"
  if (cellKey === fromKey) return "start"
  if (cellKey === toKey) return "end"
  if (cellKey > fromKey && cellKey < toKey) return "middle"
  return null
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

interface DateInputFieldProps {
  value: string
  onChange: (v: string) => void
  onClear: () => void
  placeholder?: string
}

function DateInputField({ value, onChange, onClear, placeholder }: DateInputFieldProps) {
  // Local state for the input field to allow partial typing
  const [localValue, setLocalValue] = React.useState(value)

  // Sync with external value when it changes (e.g., cleared by parent or picked from grid)
  React.useEffect(() => {
    setLocalValue(value)
  }, [value])

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    // Allow typing, applying the visual dot mask immediately
    setLocalValue(applyDateMask(e.target.value))
  }

  const handleCommit = () => {
    const rawDigits = localValue.replace(/\D/g, "")

    // If empty, just clear
    if (!rawDigits) {
      setLocalValue("")
      onChange("")
      return
    }

    // Try to smart parse
    const smartParsed = parseSmartDate(localValue)
    if (smartParsed) {
      setLocalValue(smartParsed)
      onChange(smartParsed)
    } else {
      // Invalid date -> revert to last known good value
      setLocalValue(value)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault()
      handleCommit()
    }
  }

  return (
    <div className="relative flex items-center">
      <CalendarIcon className="absolute left-2.5 size-4 text-muted-foreground pointer-events-none" />
      <Input
        value={localValue}
        onChange={handleChange}
        onBlur={handleCommit}
        onKeyDown={handleKeyDown}
        placeholder={placeholder ?? "дд.мм.гггг"}
        className="pl-8 pr-8 w-[160px] font-mono text-sm"
        maxLength={10}
      />
      {localValue && (
        <button
          type="button"
          onClick={() => {
            setLocalValue("")
            onClear()
          }}
          className="absolute right-2 text-muted-foreground hover:text-foreground transition-colors"
          aria-label="Очистить"
        >
          <XIcon className="size-4" />
        </button>
      )}
    </div>
  )
}

interface YearColumnProps {
  year: number
  draftFrom: Date | undefined
  draftTo: Date | undefined
  onMonthClick: (year: number, monthIndex: number) => void
  onPrevYear?: () => void
  onNextYear?: () => void
  isFirst?: boolean
  isLast?: boolean
}

function YearColumn({
  year,
  draftFrom,
  draftTo,
  onMonthClick,
  onPrevYear,
  onNextYear,
  isFirst,
  isLast,
}: YearColumnProps) {
  return (
    <div className="flex flex-col items-center gap-2">
      {/* Year header with navigation */}
      <div className="flex items-center justify-center w-full relative h-7">
        {isFirst && (
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-7 absolute left-0"
            onClick={onPrevYear}
          >
            <ChevronLeftIcon className="size-4" />
          </Button>
        )}
        <span className="text-sm font-semibold select-none">
          {year}
        </span>
        {isLast && (
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-7 absolute right-0"
            onClick={onNextYear}
          >
            <ChevronRightIcon className="size-4" />
          </Button>
        )}
      </div>

      {/* 4×3 month grid */}
      <div className="grid grid-cols-3 gap-0">
        {MONTH_LABELS.map((label, idx) => {
          const role = getMonthRole(year, idx, draftFrom, draftTo)
          const isEdge = role === "start" || role === "end" || role === "single"
          const isMiddle = role === "middle"

          return (
            <div
              key={idx}
              className={cn(
                "relative flex items-center justify-center",
                // Range background band behind the cell
                isMiddle && "bg-accent",
                role === "start" && "bg-gradient-to-l from-accent to-transparent",
                role === "end" && "bg-gradient-to-r from-accent to-transparent",
              )}
            >
              <button
                type="button"
                onClick={() => onMonthClick(year, idx)}
                className={cn(
                  "relative z-10 rounded-md px-2.5 py-1.5 text-sm font-normal transition-colors select-none w-full",
                  "hover:bg-accent hover:text-accent-foreground",
                  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                  isEdge
                    ? "bg-primary text-primary-foreground hover:bg-primary/90"
                    : isMiddle
                      ? "bg-accent text-accent-foreground rounded-none"
                      : "text-foreground"
                )}
              >
                {label}
              </button>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

function AccountingPeriodPicker({
  value,
  onChange,
  placeholder,
  className,
}: AccountingPeriodPickerProps) {
  const [open, setOpen] = React.useState(false)

  // Draft string values for the inputs (only committed on "Выбрать")
  const [draftFromText, setDraftFromText] = React.useState("")
  const [draftToText, setDraftToText] = React.useState("")

  // Track range selection step: "start" = next click picks from, "end" = next click picks to
  const [rangeStep, setRangeStep] = React.useState<"start" | "end">("start")

  // Central year for the 3-column grid
  const currentYear = new Date().getFullYear()
  const [baseYear, setBaseYear] = React.useState(currentYear)

  // Sync draft from value when popover opens
  React.useEffect(() => {
    if (open) {
      setDraftFromText(fmt(value?.from))
      setDraftToText(fmt(value?.to))
      // If we already have a complete range, next click starts fresh
      setRangeStep(value?.from && value?.to ? "start" : value?.from ? "end" : "start")
      // Reset baseYear to current year (or year of selected "from" date)
      if (value?.from) {
        setBaseYear(value.from.getFullYear())
      } else {
        setBaseYear(currentYear)
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  // Derived parsed dates from draft text
  const draftFrom = tryParse(draftFromText)
  const draftTo = tryParse(draftToText)

  // Handlers
  const handleMonthClick = (year: number, monthIndex: number) => {
    const clickedStart = startOfMonth(new Date(year, monthIndex, 1))
    const clickedEnd = endOfMonth(new Date(year, monthIndex, 1))

    if (rangeStep === "start") {
      // First click: set "from", clear "to", move to step "end"
      setDraftFromText(fmt(clickedStart))
      setDraftToText("")
      setRangeStep("end")
    } else {
      // Second click: set "to"
      // If the clicked month is before the current "from", swap them
      if (draftFrom && isBefore(clickedStart, draftFrom)) {
        setDraftToText(fmt(endOfMonth(draftFrom)))
        setDraftFromText(fmt(clickedStart))
      } else if (draftFrom && isSameDay(clickedStart, startOfMonth(draftFrom))) {
        // Same month clicked again — select just that one month
        setDraftToText(fmt(clickedEnd))
      } else {
        setDraftToText(fmt(clickedEnd))
      }
      setRangeStep("start")
    }
  }

  const handleClearPeriod = () => {
    setDraftFromText("")
    setDraftToText("")
    setRangeStep("start")
  }

  const handleConfirm = () => {
    onChange?.({ from: draftFrom, to: draftTo })
    setOpen(false)
  }

  const handleCancel = () => {
    setOpen(false)
  }

  // Build trigger label
  const triggerLabel = React.useMemo(() => {
    if (value?.from && value?.to) {
      return `${fmt(value.from)} – ${fmt(value.to)}`
    }
    if (value?.from) return `с ${fmt(value.from)}`
    if (value?.to) return `по ${fmt(value.to)}`
    return placeholder ?? "Выберите период"
  }, [value, placeholder])

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          className={cn(
            "justify-start text-left font-normal",
            !value?.from && !value?.to && "text-muted-foreground",
            className
          )}
        >
          <CalendarIcon className="mr-2 size-4" />
          {triggerLabel}
        </Button>
      </PopoverTrigger>

      <PopoverContent
        className="w-auto p-0"
        align="start"
        sideOffset={4}
      >
        <div className="flex flex-col gap-4 p-4">
          {/* ── Top block: manual inputs + clear ── */}
          <div className="flex items-center gap-2 flex-wrap">
            <DateInputField
              value={draftFromText}
              onChange={(v) => { setDraftFromText(v); setRangeStep("start") }}
              onClear={() => { setDraftFromText(""); setRangeStep("start") }}
            />
            <span className="text-muted-foreground text-sm select-none">–</span>
            <DateInputField
              value={draftToText}
              onChange={(v) => { setDraftToText(v); setRangeStep("start") }}
              onClear={() => { setDraftToText(""); setRangeStep("start") }}
            />
            <Button
              type="button"
              variant="link"
              size="sm"
              className="text-muted-foreground px-1"
              onClick={handleClearPeriod}
            >
              Очистить период
            </Button>
          </div>

          {/* ── Middle block: 3-column month grid ── */}
          <div className="border border-border rounded-md p-3">
            <div className="grid grid-cols-3 gap-4">
              {[baseYear - 1, baseYear, baseYear + 1].map((year, idx) => (
                <YearColumn
                  key={year}
                  year={year}
                  draftFrom={draftFrom}
                  draftTo={draftTo}
                  onMonthClick={handleMonthClick}
                  onPrevYear={() => setBaseYear((y) => y - 1)}
                  onNextYear={() => setBaseYear((y) => y + 1)}
                  isFirst={idx === 0}
                  isLast={idx === 2}
                />
              ))}
            </div>
          </div>

          {/* ── Bottom block: actions ── */}
          <div className="flex items-center justify-end">
            <div className="flex items-center gap-2">
              <Button type="button" size="sm" onClick={handleConfirm}>
                Выбрать
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={handleCancel}
              >
                Отмена
              </Button>
            </div>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}

export { AccountingPeriodPicker }
