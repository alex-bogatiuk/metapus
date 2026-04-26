// lib/cron-config.ts
// Schedule configuration: UI-friendly ScheduleConfig ↔ 6-field CRON conversion.
// Adapted from example/ for production use in automation rules.

// Дни недели
export const WEEKDAYS = [
  { value: 1, label: "Пн", fullLabel: "Понедельник" },
  { value: 2, label: "Вт", fullLabel: "Вторник" },
  { value: 3, label: "Ср", fullLabel: "Среда" },
  { value: 4, label: "Чт", fullLabel: "Четверг" },
  { value: 5, label: "Пт", fullLabel: "Пятница" },
  { value: 6, label: "Сб", fullLabel: "Суббота" },
  { value: 0, label: "Вс", fullLabel: "Воскресенье" },
] as const

// Месяцы
export const MONTHS = [
  { value: 1, label: "Январь" },
  { value: 2, label: "Февраль" },
  { value: 3, label: "Март" },
  { value: 4, label: "Апрель" },
  { value: 5, label: "Май" },
  { value: 6, label: "Июнь" },
  { value: 7, label: "Июль" },
  { value: 8, label: "Август" },
  { value: 9, label: "Сентябрь" },
  { value: 10, label: "Октябрь" },
  { value: 11, label: "Ноябрь" },
  { value: 12, label: "Декабрь" },
] as const

// Режим дневного расписания
export type DailyMode = "once" | "interval"

// Полная конфигурация расписания в стиле 1С
export interface ScheduleConfig {
  // Общие настройки
  startDate: Date | null
  endDate: Date | null
  repeatEveryDays: number // Повторять каждые N дней (0 = каждый день)
  
  // Дневное расписание
  dailyMode: DailyMode
  dailyTime: { hour: number; minute: number; second: number }
  // Для интервального режима
  intervalStartTime: { hour: number; minute: number; second: number }
  intervalEndTime: { hour: number; minute: number; second: number }
  intervalMinutes: number // Интервал повтора в минутах
  
  // Недельное расписание
  weeklyEnabled: boolean
  weekdays: number[] // 0-6, где 0 = Вс
  
  // Месячное расписание
  monthlyEnabled: boolean
  monthDays: number[] // 1-31
  months: number[] // 1-12
}

// Дефолтная конфигурация
export function getDefaultScheduleConfig(): ScheduleConfig {
  return {
    startDate: null,
    endDate: null,
    repeatEveryDays: 1,
    
    dailyMode: "once",
    dailyTime: { hour: 9, minute: 0, second: 0 },
    intervalStartTime: { hour: 9, minute: 0, second: 0 },
    intervalEndTime: { hour: 18, minute: 0, second: 0 },
    intervalMinutes: 60,
    
    weeklyEnabled: false,
    weekdays: [],
    
    monthlyEnabled: false,
    monthDays: [],
    months: [],
  }
}

// Генерация человекочитаемого описания расписания
export function getScheduleDescription(config: ScheduleConfig): string {
  const parts: string[] = []
  
  // Базовое описание повторения
  if (config.repeatEveryDays === 1) {
    parts.push("каждый день")
  } else if (config.repeatEveryDays > 1) {
    parts.push(`каждые ${config.repeatEveryDays} дн.`)
  }
  
  // Недельное расписание
  if (config.weeklyEnabled && config.weekdays.length > 0) {
    const dayNames = config.weekdays
      .map(d => WEEKDAYS.find(w => w.value === d)?.label)
      .filter(Boolean)
      .join(", ")
    parts.length = 0 // Очищаем, т.к. недельное расписание приоритетнее
    parts.push(`по ${dayNames}`)
  }
  
  // Месячное расписание
  if (config.monthlyEnabled && config.monthDays.length > 0) {
    const days = config.monthDays.join(", ")
    parts.length = 0
    parts.push(`${days}-го числа`)
    
    if (config.months.length > 0 && config.months.length < 12) {
      const monthNames = config.months
        .map(m => MONTHS.find(mo => mo.value === m)?.label.toLowerCase())
        .filter(Boolean)
        .join(", ")
      parts.push(`(${monthNames})`)
    } else {
      parts.push("каждого месяца")
    }
  }
  
  // Время запуска
  if (config.dailyMode === "once") {
    const { hour, minute, second } = config.dailyTime
    const timeStr = `${hour.toString().padStart(2, "0")}:${minute.toString().padStart(2, "0")}:${second.toString().padStart(2, "0")}`
    parts.push(`в ${timeStr}`)
  } else {
    const startStr = `${config.intervalStartTime.hour.toString().padStart(2, "0")}:${config.intervalStartTime.minute.toString().padStart(2, "0")}`
    const endStr = `${config.intervalEndTime.hour.toString().padStart(2, "0")}:${config.intervalEndTime.minute.toString().padStart(2, "0")}`
    parts.push(`с ${startStr} по ${endStr}`)
    
    if (config.intervalMinutes >= 60) {
      const hours = Math.floor(config.intervalMinutes / 60)
      parts.push(`каждые ${hours} ч.`)
    } else {
      parts.push(`каждые ${config.intervalMinutes} мин.`)
    }
  }
  
  return parts.join("; ")
}

// Конвертация конфигурации в CRON выражение (6-field: sec min hr dom mon dow)
export function configToCron(config: ScheduleConfig): string {
  let minute = config.dailyTime.minute.toString()
  let hour = config.dailyTime.hour.toString()
  let dayOfMonth = "*"
  let month = "*"
  let dayOfWeek = "*"
  
  // Месячное расписание
  if (config.monthlyEnabled && config.monthDays.length > 0) {
    dayOfMonth = config.monthDays.join(",")
    if (config.months.length > 0 && config.months.length < 12) {
      month = config.months.join(",")
    }
  }
  
  // Недельное расписание
  if (config.weeklyEnabled && config.weekdays.length > 0) {
    dayOfWeek = config.weekdays.join(",")
  }
  
  // Интервальный режим - используем */N для минут/часов
  if (config.dailyMode === "interval") {
    if (config.intervalMinutes >= 60) {
      minute = "0"
      const hours = Math.floor(config.intervalMinutes / 60)
      hour = `${config.intervalStartTime.hour}-${config.intervalEndTime.hour}/${hours}`
    } else {
      minute = `*/${config.intervalMinutes}`
      hour = `${config.intervalStartTime.hour}-${config.intervalEndTime.hour}`
    }
  }
  
  return `0 ${minute} ${hour} ${dayOfMonth} ${month} ${dayOfWeek}`
}

// Парсинг CRON в конфигурацию
export function cronToConfig(cron: string): ScheduleConfig {
  const config = getDefaultScheduleConfig()
  const parts = cron.split(" ")
  
  if (parts.length !== 6) return config
  
  const [, minute, hour, dayOfMonth, month, dayOfWeek] = parts
  
  // Парсим интервальный режим для часов (например: "9-18/2")
  if (hour.includes("/")) {
    config.dailyMode = "interval"
    const [range, step] = hour.split("/")
    if (range.includes("-")) {
      const [start, end] = range.split("-").map(n => parseInt(n))
      config.intervalStartTime.hour = isNaN(start) ? 9 : start
      config.intervalEndTime.hour = isNaN(end) ? 18 : end
    }
    const stepNum = parseInt(step)
    if (!isNaN(stepNum)) {
      config.intervalMinutes = stepNum * 60
    }
  } else if (hour.includes("-")) {
    // Диапазон без шага (например: "9-18")
    config.dailyMode = "interval"
    const [start, end] = hour.split("-").map(n => parseInt(n))
    config.intervalStartTime.hour = isNaN(start) ? 9 : start
    config.intervalEndTime.hour = isNaN(end) ? 18 : end
  } else {
    // Простое значение часа
    const hourNum = parseInt(hour)
    if (!isNaN(hourNum)) {
      config.dailyTime.hour = hourNum
    }
  }
  
  // Парсим интервальный режим для минут (например: "*/30")
  if (minute.startsWith("*/")) {
    config.dailyMode = "interval"
    const intervalNum = parseInt(minute.slice(2))
    if (!isNaN(intervalNum)) {
      config.intervalMinutes = intervalNum
    }
  } else {
    const minuteNum = parseInt(minute)
    if (!isNaN(minuteNum)) {
      config.dailyTime.minute = minuteNum
      // Также устанавливаем для интервального режима, если он активен
      if (config.dailyMode === "interval") {
        config.intervalStartTime.minute = minuteNum
      }
    }
  }
  
  // Парсим дни месяца
  if (dayOfMonth !== "*" && dayOfMonth !== "?") {
    config.monthlyEnabled = true
    config.monthDays = dayOfMonth.split(",").map(d => parseInt(d)).filter(n => !isNaN(n))
  }
  
  // Парсим месяцы
  if (month !== "*" && month !== "?") {
    config.months = month.split(",").map(m => parseInt(m)).filter(n => !isNaN(n))
  }
  
  // Парсим дни недели
  if (dayOfWeek !== "*" && dayOfWeek !== "?") {
    config.weeklyEnabled = true
    config.weekdays = dayOfWeek.split(",").map(d => parseInt(d)).filter(n => !isNaN(n))
  }
  
  return config
}

// Расчёт следующих дат запуска
export function getNextRunDates(config: ScheduleConfig, count: number = 5): Date[] {
  const dates: Date[] = []
  const now = new Date()
  let currentDay = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 0, 0, 0)
  
  // Проверяем дату начала
  if (config.startDate && config.startDate > currentDay) {
    currentDay = new Date(config.startDate)
  }
  
  for (let dayOffset = 0; dayOffset < 365 && dates.length < count; dayOffset++) {
    const candidate = new Date(currentDay)
    candidate.setDate(candidate.getDate() + dayOffset)
    
    // Проверяем дату окончания
    if (config.endDate && candidate > config.endDate) {
      break
    }
    
    // Проверяем повтор каждые N дней
    if (config.repeatEveryDays > 1 && config.startDate) {
      const daysDiff = Math.floor((candidate.getTime() - config.startDate.getTime()) / (24 * 60 * 60 * 1000))
      if (daysDiff % config.repeatEveryDays !== 0) continue
    }
    
    // Проверяем недельное расписание
    if (config.weeklyEnabled && config.weekdays.length > 0) {
      if (!config.weekdays.includes(candidate.getDay())) continue
    }
    
    // Проверяем месячное расписание
    if (config.monthlyEnabled) {
      if (config.monthDays.length > 0 && !config.monthDays.includes(candidate.getDate())) continue
      if (config.months.length > 0 && !config.months.includes(candidate.getMonth() + 1)) continue
    }
    
    if (config.dailyMode === "interval") {
      // Interval mode: generate multiple runs within the time window
      const startH = config.intervalStartTime.hour
      const startM = config.intervalStartTime.minute
      const endH = config.intervalEndTime.hour
      const endM = config.intervalEndTime.minute
      const interval = config.intervalMinutes
      
      const startTotalMin = startH * 60 + startM
      const endTotalMin = endH * 60 + endM
      
      for (let min = startTotalMin; min <= endTotalMin && dates.length < count; min += interval) {
        const runDate = new Date(candidate)
        runDate.setHours(Math.floor(min / 60), min % 60, 0)
        
        if (runDate > now) {
          dates.push(runDate)
        }
      }
    } else {
      // Single run mode
      candidate.setHours(config.dailyTime.hour, config.dailyTime.minute, config.dailyTime.second)
      
      if (candidate > now) {
        dates.push(candidate)
      }
    }
  }
  
  return dates
}
