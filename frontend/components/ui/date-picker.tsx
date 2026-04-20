"use client"

import * as React from "react"
import { format, startOfWeek, startOfMonth, endOfMonth, addDays } from "date-fns"
import { ru } from "date-fns/locale"
import { Calendar as CalendarIcon } from "lucide-react"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"

export type DateShortcutContext = "past" | "future" | "any"

export interface DateShortcut {
  label: string
  date: () => Date
}

interface DatePickerProps {
  /** Currently selected date */
  value?: Date
  /** Called when user selects a date */
  onChange?: (date: Date | undefined) => void
  /** Placeholder text when no date selected */
  placeholder?: string
  /** Additional CSS classes for the trigger button */
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
 * DatePicker — shadcn/ui date picker built on Popover + Calendar.
 *
 * Follows the official shadcn/ui Date Picker pattern:
 * https://ui.shadcn.com/docs/components/radix/date-picker
 */
export function DatePicker({
  value,
  onChange,
  placeholder = "Выберите дату",
  className,
  disabled = false,
  shortcuts,
}: DatePickerProps) {
  const [open, setOpen] = React.useState(false)
  
  // React hydration safety for persisted Zustand store: fallback to dd.MM.yyyy until loaded
  const dateFormat = useUserPrefsStore((s) => s.interface.dateFormat) ?? "dd.MM.yyyy"

  const activeShortcuts = React.useMemo(() => {
    if (!shortcuts) return null
    if (Array.isArray(shortcuts)) return shortcuts
    if (shortcuts === "past") return PAST_SHORTCUTS
    if (shortcuts === "future") return FUTURE_SHORTCUTS
    return ANY_SHORTCUTS
  }, [shortcuts])

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          disabled={disabled}
          data-empty={!value}
          className={cn(
            "data-[empty=true]:text-muted-foreground w-full justify-start text-left font-normal",
            className
          )}
        >
          <CalendarIcon className="mr-2 h-4 w-4" />
          {value ? format(value, dateFormat, { locale: ru }) : <span>{placeholder}</span>}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align="start">
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
            selected={value}
            onSelect={(date) => {
              onChange?.(date)
              setOpen(false)
            }}
            locale={ru}
          />
        </div>
      </PopoverContent>
    </Popover>
  )
}
