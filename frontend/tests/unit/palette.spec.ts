import { readFileSync } from "node:fs"
import { join } from "node:path"
import { test, expect } from "@playwright/test"
import { autoPaletteKey, isPaletteKey, paletteColor, paletteKeys, resolvePaletteKey } from "../../lib/palette"

const root = join(__dirname, "..", "..")

test("the palette is the server's palette, and both themes draw every key", () => {
  const goSource = readFileSync(join(root, "..", "internal", "core", "types", "palette.go"), "utf8")
  const serverKeys = [...goSource.matchAll(/^\s*\{Palette\w+, "[^"]+"\},$/gm)].map(m => m[0])
  const serverValues = [...goSource.matchAll(/^\s*Palette(\w+)\s+PaletteColor = "(\w+)"$/gm)].map(m => m[2])
  expect(serverValues).toEqual(paletteKeys)
  expect(serverKeys).toHaveLength(paletteKeys.length)

  const css = readFileSync(join(root, "app", "globals.css"), "utf8")
  for (const key of paletteKeys) expect(css.match(new RegExp(`--palette-${key}:`, "g")), key).toHaveLength(2)
})

test("an uncoloured item gets a stable automatic colour, never gray", () => {
  expect(isPaletteKey("blue")).toBe(true)
  expect(isPaletteKey("Blue")).toBe(false)
  expect(isPaletteKey("")).toBe(false)
  const seeds = Array.from({ length: 200 }, (_, i) => `network-${i}`)
  for (const seed of seeds) expect(autoPaletteKey(seed)).toBe(autoPaletteKey(seed))
  expect(seeds.map(autoPaletteKey)).not.toContain("gray")
  expect(new Set(seeds.map(autoPaletteKey)).size).toBeGreaterThan(8)
  expect(resolvePaletteKey("red", "x")).toBe("red")
  expect(resolvePaletteKey("", "x")).toBe(autoPaletteKey("x"))
  expect(paletteColor("blue")).toBe("hsl(var(--palette-blue))")
  expect(paletteColor("blue", 0.5)).toBe("hsl(var(--palette-blue) / 0.5)")
})
