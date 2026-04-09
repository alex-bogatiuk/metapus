"use client"

import * as React from "react"
import { format } from "date-fns"
import { ru } from "date-fns/locale"
import { Calendar as CalendarIcon, X } from "lucide-react"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import { Input } from "@/components/ui/input"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"

interface DateTimePickerProps {
  /** Currently selected date+time */
  value?: Date
  /** Called when user selects/changes date or time */
  onChange?: (date: Date | undefined) => void
  /** Placeholder text when no date selected */
  placeholder?: string
  /** Additional CSS classes for the trigger button */
  className?: string
  /** Disabled state */
  disabled?: boolean
}

/**
 * DateTimePicker — calendar + HH:mm time input.
 * Stores full Date object with time precision.
 */
export function DateTimePicker({
  value,
  onChange,
  placeholder = "Дата и время",
  className,
  disabled = false,
}: DateTimePickerProps) {
  const [open, setOpen] = React.useState(false)

  const hours = value ? String(value.getHours()).padStart(2, "0") : "00"
  const minutes = value ? String(value.getMinutes()).padStart(2, "0") : "00"

  const handleDateSelect = (date: Date | undefined) => {
    if (!date) {
      onChange?.(undefined)
      return
    }
    // Preserve existing time when changing date
    if (value) {
      date.setHours(value.getHours(), value.getMinutes(), 0, 0)
    } else {
      date.setHours(0, 0, 0, 0)
    }
    onChange?.(new Date(date))
  }

  const handleTimeChange = (type: "hours" | "minutes", raw: string) => {
    if (!value) return
    const num = parseInt(raw, 10)
    if (isNaN(num)) return
    const next = new Date(value)
    if (type === "hours") {
      next.setHours(Math.min(23, Math.max(0, num)))
    } else {
      next.setMinutes(Math.min(59, Math.max(0, num)))
    }
    onChange?.(next)
  }

  const handleClear = (e: React.MouseEvent) => {
    e.stopPropagation()
    onChange?.(undefined)
  }

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
          <CalendarIcon className="mr-2 h-4 w-4 shrink-0" />
          {value ? (
            <span className="truncate">
              {format(value, "dd.MM.yy HH:mm", { locale: ru })}
            </span>
          ) : (
            <span>{placeholder}</span>
          )}
          {value && (
            <div
              role="button"
              tabIndex={0}
              className="ml-auto flex items-center justify-center p-0.5 rounded-sm hover:bg-muted/50 cursor-pointer"
              onClick={handleClear}
              onPointerDown={(e) => {
                // Prevent Radix PopoverTrigger from opening the popover
                e.preventDefault()
                e.stopPropagation()
                onChange?.(undefined)
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.stopPropagation()
                  onChange?.(undefined)
                }
              }}
            >
              <X className="h-3.5 w-3.5 shrink-0 text-muted-foreground/50 hover:text-destructive" />
            </div>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align="start">
        <Calendar
          mode="single"
          selected={value}
          onSelect={handleDateSelect}
          locale={ru}
        />
        <div className="border-t px-3 py-2 flex items-center gap-2">
          <span className="text-xs text-muted-foreground shrink-0">Время:</span>
          <Input
            type="number"
            min={0}
            max={23}
            value={hours}
            onChange={(e) => handleTimeChange("hours", e.target.value)}
            className="h-8 w-14 text-center text-sm tabular-nums [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
            disabled={!value}
          />
          <span className="text-sm font-medium">:</span>
          <Input
            type="number"
            min={0}
            max={59}
            value={minutes}
            onChange={(e) => handleTimeChange("minutes", e.target.value)}
            className="h-8 w-14 text-center text-sm tabular-nums [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
            disabled={!value}
          />
        </div>
      </PopoverContent>
    </Popover>
  )
}
