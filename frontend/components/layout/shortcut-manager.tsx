"use client"

// components/layout/shortcut-manager.tsx
//
// Single global keydown listener. Replaces N scattered addEventListener calls.
// Renders as an invisible component, mounted once in AppShell.

import { useEffect } from "react"
import { useShortcutStore } from "@/stores/useShortcutStore"
import { parseCombo, matchEvent } from "@/lib/keyboard-utils"

/**
 * Determine if the shortcut should be suppressed because focus is inside
 * a text-editable element.
 *
 * Rules:
 * - Single-key shortcuts (F9, Delete, Enter) are suppressed in inputs.
 * - Modifier combos (Ctrl+S, Alt+W) always fire — they are intentional.
 */
function shouldSuppressInInput(
  target: EventTarget | null,
  hasModifier: boolean,
): boolean {
  if (hasModifier) return false
  if (!target || !(target instanceof HTMLElement)) return false

  const tag = target.tagName
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true
  if (target.isContentEditable) return true

  return false
}

export function ShortcutManager() {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Read all entries from the store (fast: Map.values())
      const entries = Array.from(useShortcutStore.getState().entries.values())
      if (entries.length === 0) return

      // Find matching entries
      let bestMatch: { entry: (typeof entries)[0]; priority: number } | null = null

      for (const entry of entries) {
        if (entry.passive) continue

        const combo = parseCombo(entry.keys)
        if (!matchEvent(e, combo)) continue

        const priority = entry.priority
        if (!bestMatch || priority > bestMatch.priority) {
          bestMatch = { entry, priority }
        }
      }

      if (!bestMatch) return

      // Check input suppression
      const combo = parseCombo(bestMatch.entry.keys)
      const hasModifier = combo.ctrl || combo.meta || combo.alt || combo.shift
      if (shouldSuppressInInput(e.target, hasModifier)) return

      e.preventDefault()
      bestMatch.entry.action()
    }

    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [])

  return null
}
