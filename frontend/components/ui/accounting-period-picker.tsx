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
  startOfWeek,
  endOfWeek,
  startOfQuarter,
  endOfQuarter,
  startOfYear,
  endOfYear,
} from "date-fns"
import { CalendarIcon, XIcon, ChevronLeftIcon, ChevronRightIcon } from "lucide-react"

import { useUserPrefsStore } from "@/stores/useUserPrefsStore"

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
  /** Callback when the period is confirmed via "Select" button */
  onChange?: (value: DateRangeValue) => void
  /** Placeholder text shown on the trigger button when no dates are selected */
  placeholder?: string
  className?: string
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const MONTH_LABELS = [
  "Янв", "Фев", "Мар",
  "Апр", "Май", "Июн",
  "Июл", "Авг", "Сен",
  "Окт", "Ноя", "Дек",
]

import { applyDateMask, tryParse, fmtDateStr, parseSmartDate } from "@/lib/date-parser"

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
  dateFormat: string
}

function DateInputField({ value, onChange, onClear, placeholder, dateFormat }: DateInputFieldProps) {
  // Local state for the input field to allow partial typing
  const [localValue, setLocalValue] = React.useState(value)

  // Sync with external value when it changes (e.g., cleared by parent or picked from grid)
  React.useEffect(() => {
    setLocalValue(value)
  }, [value])

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    // Allow typing, applying the visual mask immediately based on dateFormat
    setLocalValue(applyDateMask(e.target.value, dateFormat))
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
    const smartParsed = parseSmartDate(localValue, dateFormat)
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
        placeholder={placeholder ?? dateFormat.toLowerCase()}
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

  // React hydration safety for persisted Zustand store
  const dateFormat = useUserPrefsStore((s) => s.interface.dateFormat) ?? "dd.MM.yyyy"

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
      setDraftFromText(fmtDateStr(value?.from, dateFormat))
      setDraftToText(fmtDateStr(value?.to, dateFormat))
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
  const draftFrom = tryParse(draftFromText, dateFormat)
  const draftTo = tryParse(draftToText, dateFormat)

  // Handlers
  const handleMonthClick = (year: number, monthIndex: number) => {
    const clickedStart = startOfMonth(new Date(year, monthIndex, 1))
    const clickedEnd = endOfMonth(new Date(year, monthIndex, 1))

    if (rangeStep === "start") {
      // First click: set "from", clear "to", move to step "end"
      setDraftFromText(fmtDateStr(clickedStart, dateFormat))
      setDraftToText("")
      setRangeStep("end")
    } else {
      // Second click: set "to"
      // If the clicked month is before the current "from", swap them
      if (draftFrom && isBefore(clickedStart, draftFrom)) {
        setDraftToText(fmtDateStr(endOfMonth(draftFrom), dateFormat))
        setDraftFromText(fmtDateStr(clickedStart, dateFormat))
      } else if (draftFrom && isSameDay(clickedStart, startOfMonth(draftFrom))) {
        // Same month clicked again — select just that one month
        setDraftToText(fmtDateStr(clickedEnd, dateFormat))
      } else {
        setDraftToText(fmtDateStr(clickedEnd, dateFormat))
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

  const handlePresetClick = (preset: string) => {
    const now = new Date()
    let from: Date | undefined
    let to: Date | undefined

    switch (preset) {
      case "today":
        from = now
        to = now
        break
      case "thisWeek":
        from = startOfWeek(now, { weekStartsOn: 1 })
        to = endOfWeek(now, { weekStartsOn: 1 })
        break
      case "thisMonth":
        from = startOfMonth(now)
        to = endOfMonth(now)
        break
      case "lastMonth": {
        const d = new Date(now.getFullYear(), now.getMonth() - 1, 1)
        from = startOfMonth(d)
        to = endOfMonth(d)
        break
      }
      case "thisQuarter":
        from = startOfQuarter(now)
        to = endOfQuarter(now)
        break
      case "thisYear":
        from = startOfYear(now)
        to = endOfYear(now)
        break
    }

    if (from && to) {
      onChange?.({ from, to })
      setOpen(false)
    }
  }

  // Build trigger label
  const triggerLabel = React.useMemo(() => {
    if (value?.from && value?.to) {
      return `${fmtDateStr(value.from, dateFormat)} – ${fmtDateStr(value.to, dateFormat)}`
    }
    if (value?.from) return `с ${fmtDateStr(value.from, dateFormat)}`
    if (value?.to) return `по ${fmtDateStr(value.to, dateFormat)}`
    return placeholder ?? "Выберите период"
  }, [value, placeholder, dateFormat])

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
              dateFormat={dateFormat}
            />
            <span className="text-muted-foreground text-sm select-none">–</span>
            <DateInputField
              value={draftToText}
              onChange={(v) => { setDraftToText(v); setRangeStep("start") }}
              onClear={() => { setDraftToText(""); setRangeStep("start") }}
              dateFormat={dateFormat}
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

          {/* ── Quick Presets ── */}
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="secondary" size="sm" onClick={() => handlePresetClick("today")}>Сегодня</Button>
            <Button type="button" variant="secondary" size="sm" onClick={() => handlePresetClick("thisWeek")}>Эта неделя</Button>
            <Button type="button" variant="secondary" size="sm" onClick={() => handlePresetClick("thisMonth")}>Этот месяц</Button>
            <Button type="button" variant="secondary" size="sm" onClick={() => handlePresetClick("lastMonth")}>Прошлый месяц</Button>
            <Button type="button" variant="secondary" size="sm" onClick={() => handlePresetClick("thisQuarter")}>Этот квартал</Button>
            <Button type="button" variant="secondary" size="sm" onClick={() => handlePresetClick("thisYear")}>Этот год</Button>
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
                Отменить
              </Button>
            </div>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}

export { AccountingPeriodPicker }
