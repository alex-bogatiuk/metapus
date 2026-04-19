"use client"

import * as React from "react"
import { useState, useCallback, useMemo, useEffect } from "react"
import { format } from "date-fns"
import { ru } from "date-fns/locale"
import { Calendar as CalendarIcon, X, Clock, CalendarDays } from "lucide-react"

import { cn } from "@/lib/utils"
import {
  WEEKDAYS,
  MONTHS,
  getScheduleDescription,
  configToCron,
  cronToConfig,
  getNextRunDates,
  type ScheduleConfig,
  type DailyMode,
} from "@/lib/cron-config"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { Badge } from "@/components/ui/badge"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

interface ScheduleConfiguratorProps {
  value?: string
  onChange?: (cronExpression: string) => void
  open?: boolean
  onOpenChange?: (open: boolean) => void
}

// Компонент выбора времени HH:MM:SS
function TimeInput({
  value,
  onChange,
  label,
}: {
  value: { hour: number; minute: number; second: number }
  onChange: (val: { hour: number; minute: number; second: number }) => void
  label?: string
}) {
  return (
    <div className="flex items-center gap-1">
      {label && <span className="text-sm text-muted-foreground mr-2">{label}</span>}
      <Input
        type="number"
        min={0}
        max={23}
        value={value.hour.toString().padStart(2, "0")}
        onChange={(e) => onChange({ ...value, hour: Math.min(23, Math.max(0, parseInt(e.target.value) || 0)) })}
        className="w-14 text-center font-mono"
      />
      <span className="text-muted-foreground">:</span>
      <Input
        type="number"
        min={0}
        max={59}
        value={value.minute.toString().padStart(2, "0")}
        onChange={(e) => onChange({ ...value, minute: Math.min(59, Math.max(0, parseInt(e.target.value) || 0)) })}
        className="w-14 text-center font-mono"
      />
      <span className="text-muted-foreground">:</span>
      <Input
        type="number"
        min={0}
        max={59}
        value={value.second.toString().padStart(2, "0")}
        onChange={(e) => onChange({ ...value, second: Math.min(59, Math.max(0, parseInt(e.target.value) || 0)) })}
        className="w-14 text-center font-mono"
      />
    </div>
  )
}

// Компонент выбора даты с кнопкой очистки
function DatePickerField({
  value,
  onChange,
  placeholder,
}: {
  value: Date | null
  onChange: (date: Date | null) => void
  placeholder: string
}) {
  return (
    <div className="flex items-center gap-1">
      <Popover>
        <PopoverTrigger asChild>
          <Button
            variant="outline"
            className={cn(
              "w-[180px] justify-start text-left font-normal",
              !value && "text-muted-foreground"
            )}
          >
            <CalendarIcon className="mr-2 h-4 w-4" />
            {value ? format(value, "dd.MM.yyyy", { locale: ru }) : placeholder}
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-auto p-0" align="start">
          <Calendar
            mode="single"
            selected={value ?? undefined}
            onSelect={(date) => onChange(date ?? null)}
            locale={ru}
            initialFocus
          />
        </PopoverContent>
      </Popover>
      {value && (
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8"
          onClick={() => onChange(null)}
        >
          <X className="h-4 w-4" />
        </Button>
      )}
    </div>
  )
}

export function ScheduleConfigurator({
  value = "0 0 9 * * *",
  onChange,
  open,
  onOpenChange,
}: ScheduleConfiguratorProps) {
  const [config, setConfig] = useState<ScheduleConfig>(() => {
    return cronToConfig(value)
  })

  // Синхронизация при изменении value извне
  useEffect(() => {
    setConfig(cronToConfig(value))
  }, [value])

  const [activeTab, setActiveTab] = useState("general")

  const scheduleDescription = useMemo(() => {
    return getScheduleDescription(config)
  }, [config])

  const cronExpression = useMemo(() => {
    return configToCron(config)
  }, [config])

  const nextRuns = useMemo(() => {
    return getNextRunDates(config, 5)
  }, [config])

  // Валидация временного диапазона
  const isInvalidTimeRange = useMemo(() => {
    if (config.dailyMode !== "interval") return false
    const startMinutes = config.intervalStartTime.hour * 60 + config.intervalStartTime.minute
    const endMinutes = config.intervalEndTime.hour * 60 + config.intervalEndTime.minute
    return startMinutes >= endMinutes
  }, [config.dailyMode, config.intervalStartTime, config.intervalEndTime])

  const updateConfig = useCallback((updates: Partial<ScheduleConfig>) => {
    setConfig((prev) => ({ ...prev, ...updates }))
  }, [])

  const handleSave = useCallback(() => {
    onChange?.(cronExpression)
    onOpenChange?.(false)
  }, [cronExpression, onChange, onOpenChange])

  const toggleWeekday = useCallback((day: number) => {
    setConfig((prev) => {
      const newWeekdays = prev.weekdays.includes(day)
        ? prev.weekdays.filter((d) => d !== day)
        : [...prev.weekdays, day].sort()
      return { ...prev, weekdays: newWeekdays, weeklyEnabled: newWeekdays.length > 0 }
    })
  }, [])

  const toggleMonthDay = useCallback((day: number) => {
    setConfig((prev) => {
      const newDays = prev.monthDays.includes(day)
        ? prev.monthDays.filter((d) => d !== day)
        : [...prev.monthDays, day].sort((a, b) => a - b)
      return { ...prev, monthDays: newDays, monthlyEnabled: newDays.length > 0 || prev.months.length > 0 }
    })
  }, [])

  const toggleMonth = useCallback((month: number) => {
    setConfig((prev) => {
      const newMonths = prev.months.includes(month)
        ? prev.months.filter((m) => m !== month)
        : [...prev.months, month].sort((a, b) => a - b)
      return { ...prev, months: newMonths, monthlyEnabled: prev.monthDays.length > 0 || newMonths.length > 0 }
    })
  }, [])

  const dialogContent = (
    <div className="flex flex-col h-full">
      {/* Табы */}
      <Tabs value={activeTab} onValueChange={setActiveTab} className="flex-1">
        <TabsList className="grid w-full grid-cols-4 mb-4">
          <TabsTrigger value="general" className="text-sm">Общие</TabsTrigger>
          <TabsTrigger value="daily" className="text-sm">Дневное</TabsTrigger>
          <TabsTrigger value="weekly" className="text-sm">Недельное</TabsTrigger>
          <TabsTrigger value="monthly" className="text-sm">Месячное</TabsTrigger>
        </TabsList>

        {/* Вкладка "Общие" */}
        <TabsContent value="general" className="space-y-4 mt-0">
          <div className="grid gap-4">
            {/* Дата начала */}
            <div className="grid grid-cols-[140px_1fr] items-center gap-4">
              <Label className="text-sm">Дата начала:</Label>
              <DatePickerField
                value={config.startDate}
                onChange={(date) => updateConfig({ startDate: date })}
                placeholder=". ."
              />
            </div>

            {/* Дата окончания */}
            <div className="grid grid-cols-[140px_1fr] items-center gap-4">
              <Label className="text-sm">Дата окончания:</Label>
              <DatePickerField
                value={config.endDate}
                onChange={(date) => updateConfig({ endDate: date })}
                placeholder=". ."
              />
            </div>

            {/* Повторять каждые N дней */}
            <div className="grid grid-cols-[140px_1fr] items-center gap-4">
              <Label className="text-sm">Повторять каждые:</Label>
              <div className="flex items-center gap-2">
                <Input
                  type="number"
                  min={1}
                  max={365}
                  value={config.repeatEveryDays}
                  onChange={(e) => updateConfig({ repeatEveryDays: Math.max(1, parseInt(e.target.value) || 1) })}
                  className="w-20"
                />
                <span className="text-sm text-muted-foreground">(дн.)</span>
              </div>
            </div>
          </div>
        </TabsContent>

        {/* Вкладка "Дневное" */}
        <TabsContent value="daily" className="space-y-4 mt-0">
          <RadioGroup
            value={config.dailyMode}
            onValueChange={(v) => updateConfig({ dailyMode: v as DailyMode })}
            className="space-y-4"
          >
            {/* Один раз в день */}
            <div className="flex items-start gap-3 p-3 rounded-lg border bg-card">
              <RadioGroupItem value="once" id="once" className="mt-1" />
              <div className="flex-1 space-y-3">
                <Label htmlFor="once" className="text-sm font-medium cursor-pointer">
                  Один раз в день
                </Label>
                {config.dailyMode === "once" && (
                  <div className="flex items-center gap-2">
                    <Clock className="h-4 w-4 text-muted-foreground" />
                    <TimeInput
                      value={config.dailyTime}
                      onChange={(val) => updateConfig({ dailyTime: val })}
                    />
                  </div>
                )}
              </div>
            </div>

            {/* Несколько раз в день (интервал) */}
            <div className="flex items-start gap-3 p-3 rounded-lg border bg-card">
              <RadioGroupItem value="interval" id="interval" className="mt-1" />
              <div className="flex-1 space-y-3">
                <Label htmlFor="interval" className="text-sm font-medium cursor-pointer">
                  Повторять в течение дня
                </Label>
                {config.dailyMode === "interval" && (
                  <div className="space-y-3">
                    <div className="flex items-center gap-2">
                      <span className="text-sm text-muted-foreground w-8">с</span>
                      <TimeInput
                        value={config.intervalStartTime}
                        onChange={(val) => updateConfig({ intervalStartTime: val })}
                      />
                    </div>
                    <div className="flex items-center gap-2">
                      <span className="text-sm text-muted-foreground w-8">по</span>
                      <TimeInput
                        value={config.intervalEndTime}
                        onChange={(val) => updateConfig({ intervalEndTime: val })}
                      />
                    </div>
                    {isInvalidTimeRange && (
                      <p className="text-sm text-destructive">
                        Время окончания должно быть позже времени начала
                      </p>
                    )}
                    <div className="flex items-center gap-2">
                      <span className="text-sm text-muted-foreground">каждые</span>
                      <Input
                        type="number"
                        min={1}
                        max={1440}
                        value={config.intervalMinutes}
                        onChange={(e) => updateConfig({ intervalMinutes: Math.max(1, parseInt(e.target.value) || 1) })}
                        className="w-20"
                      />
                      <span className="text-sm text-muted-foreground">мин.</span>
                    </div>
                  </div>
                )}
              </div>
            </div>
          </RadioGroup>
        </TabsContent>

        {/* Вкладка "Недельное" */}
        <TabsContent value="weekly" className="space-y-4 mt-0">
          <div className="space-y-3">
            <Label className="text-sm text-muted-foreground">
              Выберите дни недели для запуска:
            </Label>
            <div className="flex flex-wrap gap-2">
              {WEEKDAYS.map((day) => {
                const isSelected = config.weekdays.includes(day.value)
                return (
                  <button
                    key={day.value}
                    type="button"
                    onClick={() => toggleWeekday(day.value)}
                    aria-label={`${day.fullLabel}, ${isSelected ? "выбран" : "не выбран"}`}
                    aria-pressed={isSelected}
                    className={cn(
                      "flex items-center justify-center h-10 px-4 rounded-md text-sm font-medium transition-all border",
                      isSelected
                        ? "bg-primary text-primary-foreground border-primary"
                        : "bg-card text-card-foreground border-input hover:bg-accent hover:text-accent-foreground"
                    )}
                  >
                    {day.fullLabel}
                  </button>
                )
              })}
            </div>
            {config.weekdays.length === 0 && (
              <p className="text-sm text-muted-foreground italic">
                Не выбрано (будет запускаться каждый день)
              </p>
            )}
          </div>
        </TabsContent>

        {/* Вкладка "Месячное" */}
        <TabsContent value="monthly" className="space-y-6 mt-0">
          {/* Дни месяца */}
          <div className="space-y-3">
            <Label className="text-sm text-muted-foreground">
              Числа месяца:
            </Label>
            <div className="grid grid-cols-7 gap-1.5">
              {Array.from({ length: 31 }, (_, i) => i + 1).map((day) => {
                const isSelected = config.monthDays.includes(day)
                return (
                  <button
                    key={day}
                    type="button"
                    onClick={() => toggleMonthDay(day)}
                    aria-label={`${day} число, ${isSelected ? "выбрано" : "не выбрано"}`}
                    aria-pressed={isSelected}
                    className={cn(
                      "h-8 w-8 rounded text-xs font-medium transition-all border",
                      isSelected
                        ? "bg-primary text-primary-foreground border-primary"
                        : "bg-card text-card-foreground border-input hover:bg-accent"
                    )}
                  >
                    {day}
                  </button>
                )
              })}
            </div>
          </div>

          {/* Месяцы */}
          <div className="space-y-3">
            <Label className="text-sm text-muted-foreground">
              Месяцы (оставьте пустым для всех):
            </Label>
            <div className="grid grid-cols-4 gap-2">
              {MONTHS.map((month) => {
                const isSelected = config.months.includes(month.value)
                return (
                  <button
                    key={month.value}
                    type="button"
                    onClick={() => toggleMonth(month.value)}
                    aria-label={`${month.label}, ${isSelected ? "выбран" : "не выбран"}`}
                    aria-pressed={isSelected}
                    className={cn(
                      "h-9 px-2 rounded-md text-xs font-medium transition-all border",
                      isSelected
                        ? "bg-primary text-primary-foreground border-primary"
                        : "bg-card text-card-foreground border-input hover:bg-accent"
                    )}
                  >
                    {month.label}
                  </button>
                )
              })}
            </div>
          </div>
        </TabsContent>
      </Tabs>

      {/* Описание расписания (внизу, как в 1С) */}
      <div className="mt-4 pt-4 border-t space-y-3">
        <div className="flex items-start gap-2 text-sm">
          <CalendarDays className="h-4 w-4 text-primary mt-0.5 shrink-0" />
          <span className="text-foreground">{scheduleDescription}</span>
        </div>
        
        {nextRuns.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            <span className="text-xs text-muted-foreground">Ближайшие:</span>
            {nextRuns.slice(0, 3).map((date, i) => (
              <Badge key={i} variant="secondary" className="text-xs font-normal">
                {format(date, "d MMM HH:mm", { locale: ru })}
              </Badge>
            ))}
          </div>
        )}
      </div>
    </div>
  )

  // Если используется как диалог
  if (open !== undefined && onOpenChange) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <CalendarIcon className="h-5 w-5" />
              Расписание
            </DialogTitle>
          </DialogHeader>
          {dialogContent}
          <DialogFooter>
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              Отмена
            </Button>
            <Button onClick={handleSave} disabled={isInvalidTimeRange}>
              OK
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    )
  }

  // Если используется как встроенный компонент
  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="flex items-center gap-2 mb-4 pb-3 border-b">
        <CalendarIcon className="h-5 w-5" />
        <h3 className="font-semibold">Расписание</h3>
      </div>
      {dialogContent}
    </div>
  )
}

// Компактная кнопка для вызова диалога конфигуратора расписания
export function ScheduleButton({
  value = "0 0 9 * * *",
  onChange,
  className,
}: {
  value?: string
  onChange?: (cron: string) => void
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const config = useMemo(() => cronToConfig(value), [value])
  const description = useMemo(() => getScheduleDescription(config), [config])

  return (
    <>
      <Button
        variant="outline"
        className={cn("justify-start text-left h-auto py-2", className)}
        onClick={() => setOpen(true)}
      >
        <CalendarIcon className="mr-2 h-4 w-4 shrink-0" />
        <span className="truncate">{description}</span>
      </Button>
      <ScheduleConfigurator
        value={value}
        onChange={onChange}
        open={open}
        onOpenChange={setOpen}
      />
    </>
  )
}
