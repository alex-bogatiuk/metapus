import { format, parse, isValid } from "date-fns"

/** Apply a date mask dynamically based on user format */
export function applyDateMask(raw: string, dateFormat: string): string {
  const digits = raw.replace(/\D/g, "").slice(0, 8)
  let result = ""
  
  if (dateFormat === "yyyy-MM-dd") {
    // mask: ####-##-##
    for (let i = 0; i < digits.length; i++) {
      if (i === 4 || i === 6) result += "-"
      result += digits[i]
    }
  } else if (dateFormat === "MM/dd/yyyy") {
    // mask: ##/##/####
    for (let i = 0; i < digits.length; i++) {
      if (i === 2 || i === 4) result += "/"
      result += digits[i]
    }
  } else {
    // mask: ##.##.####
    for (let i = 0; i < digits.length; i++) {
      if (i === 2 || i === 4) result += "."
      result += digits[i]
    }
  }
  return result
}

/** Try to parse a string into a valid Date */
export function tryParse(text: string, dateFormat: string): Date | undefined {
  if (text.length !== 10) return undefined
  const d = parse(text, dateFormat, new Date())
  return isValid(d) ? d : undefined
}

/** Format a Date to the user's format, or return empty string */
export function fmtDateStr(date: Date | undefined | null, dateFormat: string): string {
  if (!date) return ""
  return format(date, dateFormat)
}

/** 
 * Smart parsing for date entry.
 * Adapts to the user's date format left-to-right.
 */
export function parseSmartDate(raw: string, dateFormat: string): string | null {
  const digits = raw.replace(/\D/g, "")
  if (!digits) return null

  const now = new Date()
  let d = format(now, "dd")
  let m = format(now, "MM")
  let y = format(now, "yyyy")

  const isYMD = dateFormat === "yyyy-MM-dd"
  const isMDY = dateFormat === "MM/dd/yyyy"

  if (isYMD) {
    if (digits.length <= 2) {
      y = `20${digits}`
    } else if (digits.length <= 4) {
      y = digits.padEnd(4, "0")
    } else if (digits.length <= 6) {
      if (digits.startsWith("20")) {
        y = digits.slice(0, 4)
        m = digits.slice(4, 6).padStart(2, "0")
      } else {
        y = `20${digits.slice(0, 2)}`
        m = digits.slice(2, 4).padStart(2, "0")
        d = digits.slice(4, 6).padStart(2, "0")
      }
    } else {
      y = digits.slice(0, 4)
      m = digits.slice(4, 6).padStart(2, "0")
      d = digits.slice(6, 8).padStart(2, "0")
    }
  } else {
    const first = digits.slice(0, 2).padStart(2, "0")
    const second = digits.length > 2 ? digits.slice(2, 4).padStart(2, "0") : (isMDY ? d : m)
    const thirdStr = digits.slice(4, 8)
    const third = thirdStr ? (thirdStr.length === 2 ? `20${thirdStr}` : thirdStr.padEnd(4, "0")) : y

    if (isMDY) {
      m = first
      if (digits.length > 2) d = second
      if (digits.length > 4) y = third
    } else {
      d = first
      if (digits.length > 2) m = second
      if (digits.length > 4) y = third
    }
  }

  let formatted = ""
  if (isYMD) formatted = `${y}-${m}-${d}`
  else if (isMDY) formatted = `${m}/${d}/${y}`
  else formatted = `${d}.${m}.${y}`

  const parsedDate = parse(formatted, dateFormat, new Date())
  if (!isValid(parsedDate)) return null
  
  if (format(parsedDate, "dd") !== d || format(parsedDate, "MM") !== m || format(parsedDate, "yyyy") !== y) {
      return null
  }

  return formatted
}
