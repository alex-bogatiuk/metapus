import type { IntegrationProviderResponse, IntegrationCredentialProfileResponse } from "@/types/generated/http-contracts"

export interface CredentialKey {
  name: string
  value: string
}

export function credentialProfile(provider: string, providers: readonly IntegrationProviderResponse[]): IntegrationCredentialProfileResponse | undefined {
  const profile = providers.find(item => item.id === provider)?.credentials
  return profile?.fields.length ? profile : undefined
}

export function emptyCredentialKeys(provider: string, providers: readonly IntegrationProviderResponse[]): CredentialKey[] {
  return (credentialProfile(provider, providers)?.fields ?? [{ name: "default" }]).map(field => ({ name: field.name, value: "" }))
}

type TokenResult = { ok: true; value: string; fromUrl: boolean } | { ok: false; error: string }

function validCredentialKey(value: string, format?: string): boolean {
  if (!value.trim() || value.length > 4096 || /[\r\n]/.test(value)) return false
  switch (format) {
    case "hex32": return /^[A-Fa-f0-9]{32}$/.test(value)
    case "url_safe": return /^[A-Za-z0-9_-]+$/.test(value)
    case "bearer": return !/[\s\u0000-\u001f\u007f]/.test(value) && !value.includes("://")
    case undefined: case "": return true
    default: return false
  }
}

export function normalizeCredentialKey(raw: string, role: string, profile?: IntegrationCredentialProfileResponse): TokenResult {
  const imports = profile?.urlImports ?? []
  const value = imports.length ? raw.trim() : raw
  const invalid: TokenResult = { ok: false, error: "Введите ключ допустимого формата или ссылку поддерживаемого подключения без дополнительных параметров." }
  if (validCredentialKey(value, profile?.keyFormat) && !value.includes("://")) return { ok: true, value, fromUrl: false }
  if (/\s/.test(value)) return invalid
  try {
    const url = new URL(value)
    if (url.protocol !== "https:" || (url.port && url.port !== "443") || url.username || url.password || /[?#]/.test(value)) return invalid
    for (const rule of imports) {
      if (url.hostname !== rule.host || !url.pathname.startsWith(`${rule.pathPrefix}/`)) continue
      const rest = url.pathname.slice(rule.pathPrefix.length + 1)
      const parts = rest.split("/").filter(Boolean)
      const key = parts[0]
      if (!key || !validCredentialKey(key, profile?.keyFormat)) continue
      if (rule.rolePaths) {
        if (parts[1] && parts[1] !== rule.rolePaths[role]) continue
      } else if (rest !== key && rest !== `${key}/`) continue
      return { ok: true, value: key, fromUrl: true }
    }
  } catch { /* Invalid input is reported without repeating a credential URL. */ }
  return invalid
}

/** Whether the profile lets an account leave this key out. */
export function optionalCredentialField(profile: IntegrationCredentialProfileResponse | undefined, name: string): boolean {
  return !!profile?.fields.find(field => field.name === name)?.optional
}

export function prepareCredentialKeys(provider: string, input: readonly CredentialKey[], providers: readonly IntegrationProviderResponse[]) {
  const profile = providers.find(item => item.id === provider)?.credentials
  const errors: Record<string, string> = {}
  const names = new Set<string>()
  // An optional key left empty is not sent; the indexes keep pointing at the inputs.
  const sent = input.map((key, index) => ({ key, index })).filter(({ key }) => key.value.trim() || !optionalCredentialField(profile, key.name))
  const keys = sent.map(({ key, index }) => {
    if (!key.name || key.name.length > 64 || /\s/.test(key.name)) errors[`key-name-${index}`] = "Укажите имя до 64 символов без пробелов."
    else if (names.has(key.name)) errors[`key-name-${index}`] = "Это имя уже используется. Укажите другое."
    names.add(key.name)
    const result = normalizeCredentialKey(key.value, key.name, profile)
    if (!result.ok) errors[`key-value-${index}`] = result.error
    return { name: key.name, value: result.ok ? result.value : key.value }
  })
  if (!sent.length && profile?.fields.length && profile.fields.every(field => field.optional)) {
    errors.credentials = "Укажите хотя бы один ключ."
  } else if (!sent.length || sent.length > 32 || (profile?.singleKey && sent.length !== 1) || profile?.fields.some(field => !field.optional && !names.has(field.name))) {
    errors.credentials = "Укажите полный набор ключей, предусмотренный профилем провайдера."
  }
  return { keys, errors }
}

export function validQuotaTimezone(timezone: string): boolean {
  try { new Intl.DateTimeFormat("en", { timeZone: timezone }).format(); return !!timezone }
  catch { return false }
}

export function quotaTimezones(current: string): string[] {
  const zones = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : ["Europe/Moscow", "Europe/London", "America/New_York", "Asia/Dubai"]
  return [...new Set(["UTC", ...(validQuotaTimezone(current) ? [current] : []), ...zones])]
}

/** Display a local midnight, without converting it through the browser timezone. */
export function nextQuotaPeriod(day: number, timezone: string, now = new Date()): string | null {
  if (!Number.isInteger(day) || day < 1 || day > 31 || !validQuotaTimezone(timezone)) return null
  const parts = Object.fromEntries(new Intl.DateTimeFormat("en", { timeZone: timezone, year: "numeric", month: "numeric", day: "numeric" }).formatToParts(now).map(part => [part.type, part.value]))
  let year = Number(parts.year), month = Number(parts.month)
  const anchor = Math.min(day, new Date(Date.UTC(year, month, 0)).getUTCDate())
  if (anchor <= Number(parts.day)) { month++; if (month > 12) { month = 1; year++ } }
  const nextDay = Math.min(day, new Date(Date.UTC(year, month, 0)).getUTCDate())
  const date = new Intl.DateTimeFormat("ru-RU", { day: "numeric", month: "long", year: "numeric", timeZone: "UTC" }).format(new Date(Date.UTC(year, month - 1, nextDay))).replace(/\s?г\.$/, "")
  return `${date}, 00:00`
}

/** The expiry editor uses an explicit UTC timezone, independent of browser DST. */
export function expiryInputValue(iso: string | null | undefined): string {
  if (!iso) return ""
  const date = new Date(iso)
  return Number.isFinite(date.getTime()) ? date.toISOString().slice(0, 19) : ""
}

export function expiryRequestValue(input: string): string | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(input)) return null
  const date = new Date(`${input}Z`)
  if (!Number.isFinite(date.getTime())) return null
  const expected = input.length === 16 ? `${input}:00` : input
  return date.toISOString().slice(0, 19) === expected ? date.toISOString() : null
}
