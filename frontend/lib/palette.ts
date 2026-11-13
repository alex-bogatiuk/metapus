/**
 * The shared palette people pick colours from for data: networks today, tags or statuses later.
 * Keys match types.PaletteColor on the server; each key is drawn by a `--palette-*` token
 * tuned separately for the light and the dark theme, so a stored key never fixes a colour code.
 */
export const paletteKeys = [
  "red", "orange", "amber", "yellow", "lime", "green", "teal", "sky", "blue", "indigo", "violet", "pink", "gray",
] as const

export type PaletteKey = (typeof paletteKeys)[number]

const known: ReadonlySet<string> = new Set(paletteKeys)
export const isPaletteKey = (value: string | null | undefined): value is PaletteKey => typeof value === "string" && known.has(value)

/** Light swatches carry dark marks on top of them; the rest carry white ones. */
const lightKeys: ReadonlySet<PaletteKey> = new Set(["amber", "yellow", "lime"])
export const paletteInk = (key: PaletteKey) => lightKeys.has(key) ? "hsl(240 8% 8%)" : "#fff"

/** CSS colour of a key, optionally see-through: `paletteColor("blue", 0.5)`. */
export function paletteColor(key: PaletteKey, alpha = 1): string {
  return alpha >= 1 ? `hsl(var(--palette-${key}))` : `hsl(var(--palette-${key}) / ${alpha})`
}

/** Keys an automatic choice may land on; gray stays for an explicit "neutral". */
const autoKeys = paletteKeys.filter(key => key !== "gray")

/** A stable colour for something nobody coloured yet: the same seed always gets the same key. */
export function autoPaletteKey(seed: string): PaletteKey {
  let hash = 2166136261
  for (let i = 0; i < seed.length; i++) hash = Math.imul(hash ^ seed.charCodeAt(i), 16777619)
  return autoKeys[(hash >>> 0) % autoKeys.length]
}

/** The chosen key when there is one, otherwise the automatic key for `seed`. */
export const resolvePaletteKey = (value: string | null | undefined, seed: string): PaletteKey =>
  isPaletteKey(value) ? value : autoPaletteKey(seed)
