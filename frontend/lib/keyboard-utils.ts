// lib/keyboard-utils.ts
//
// Pure utilities for keyboard shortcut handling.
// Cross-platform: "mod" alias → Ctrl on Windows/Linux, Cmd (⌘) on macOS.
// Cyrillic-safe: uses e.code for letter keys so shortcuts work regardless of keyboard layout.

/** Detect macOS platform */
export function isMac(): boolean {
  if (typeof navigator === "undefined") return false
  // navigator.platform is deprecated but widely supported;
  // fall back to userAgentData when available.
  const ua = (navigator as unknown as { userAgentData?: { platform?: string } }).userAgentData
  if (ua?.platform) return /mac/i.test(ua.platform)
  return /mac/i.test(navigator.platform)
}

// ── Combo parsing ───────────────────────────────────────────────────────

export interface ParsedCombo {
  /** Lowercase key name, e.g. "s", "enter", "f9", "arrowleft" */
  key: string
  ctrl: boolean
  meta: boolean
  alt: boolean
  shift: boolean
}

/**
 * Parse a human-friendly combo string into a structured descriptor.
 *
 * Supported modifiers: mod (platform-adaptive), ctrl, meta, alt, shift.
 * Examples: "mod+s", "alt+w", "f9", "mod+shift+enter", "delete"
 */
export function parseCombo(combo: string): ParsedCombo {
  const parts = combo.toLowerCase().split("+").map((p) => p.trim())
  const mac = isMac()

  let ctrl = false
  let meta = false
  let alt = false
  let shift = false
  let key = ""

  for (const part of parts) {
    switch (part) {
      case "mod":
        if (mac) meta = true
        else ctrl = true
        break
      case "ctrl":
        ctrl = true
        break
      case "meta":
      case "cmd":
        meta = true
        break
      case "alt":
        alt = true
        break
      case "shift":
        shift = true
        break
      default:
        key = part
    }
  }

  return { key, ctrl, meta, alt, shift }
}

// ── Key code mapping ────────────────────────────────────────────────────

/**
 * Map from lowercase key name → KeyboardEvent.code.
 *
 * Covers letters, digits, and common punctuation.
 * Using e.code makes shortcuts layout-independent:
 *   - Letters: Ctrl+S works in Russian layout (physical KeyS, e.key = "ы")
 *   - Punctuation: Ctrl+/ works in Russian layout (physical Slash, e.key = ".")
 */
const PUNCTUATION_TO_CODE: Record<string, string> = {
  "/": "Slash",
  "\\": "Backslash",
  "[": "BracketLeft",
  "]": "BracketRight",
  ";": "Semicolon",
  "'": "Quote",
  ",": "Comma",
  ".": "Period",
  "-": "Minus",
  "=": "Equal",
  "`": "Backquote",
}

function keyToCode(key: string): string | undefined {
  if (/^[a-z]$/.test(key)) return `Key${key.toUpperCase()}`
  if (/^[0-9]$/.test(key)) return `Digit${key}`
  return PUNCTUATION_TO_CODE[key]
}

/**
 * Check if a KeyboardEvent matches a ParsedCombo.
 *
 * For letter keys we compare against e.code (layout-independent, works with Cyrillic).
 * For special keys (Enter, F9, Delete, Escape, arrows) we compare e.key.
 */
export function matchEvent(e: KeyboardEvent, combo: ParsedCombo): boolean {
  // Modifier check
  if (combo.ctrl !== e.ctrlKey) return false
  if (combo.meta !== e.metaKey) return false
  if (combo.alt !== e.altKey) return false
  if (combo.shift !== e.shiftKey) return false

  // Key check — use code for letters/digits, key for everything else
  const expectedCode = keyToCode(combo.key)
  if (expectedCode) {
    return e.code === expectedCode
  }

  // Special keys: compare e.key (lowercased)
  return e.key.toLowerCase() === combo.key
}

// ── Display formatting ──────────────────────────────────────────────────

const MOD_SYMBOLS_MAC: Record<string, string> = {
  ctrl: "⌃",
  meta: "⌘",
  alt: "⌥",
  shift: "⇧",
}

const MOD_SYMBOLS_WIN: Record<string, string> = {
  ctrl: "Ctrl",
  meta: "Win",
  alt: "Alt",
  shift: "Shift",
}

const KEY_LABELS: Record<string, string> = {
  arrowleft: "←",
  arrowright: "→",
  arrowup: "↑",
  arrowdown: "↓",
  enter: "Enter",
  escape: "Esc",
  delete: "Del",
  backspace: "⌫",
  " ": "Space",
  "/": "/",
}

/**
 * Format a combo string for display.
 *
 * On Mac: "mod+s" → "⌘S", "alt+arrowleft" → "⌥←"
 * On Win: "mod+s" → "Ctrl+S", "alt+arrowleft" → "Alt+←"
 */
export function formatCombo(combo: string): string {
  const mac = isMac()
  const symbols = mac ? MOD_SYMBOLS_MAC : MOD_SYMBOLS_WIN
  const parts = combo.toLowerCase().split("+").map((p) => p.trim())
  const modifiers: string[] = []
  let key = ""

  for (const part of parts) {
    switch (part) {
      case "mod":
        modifiers.push(mac ? symbols.meta : symbols.ctrl)
        break
      case "ctrl":
        modifiers.push(symbols.ctrl)
        break
      case "meta":
      case "cmd":
        modifiers.push(symbols.meta)
        break
      case "alt":
        modifiers.push(symbols.alt)
        break
      case "shift":
        modifiers.push(symbols.shift)
        break
      default:
        key = part
    }
  }

  // Resolve key label
  // KEY_LABELS covers arrows, enter, esc, etc.; everything else (F1–F12, letters, digits) uppercased
  const keyLabel = KEY_LABELS[key] ?? key.toUpperCase()

  if (mac) {
    // Mac style: ⌘⇧S (no separators)
    return [...modifiers, keyLabel].join("")
  }
  // Windows style: Ctrl+Shift+S
  return [...modifiers, keyLabel].join("+")
}
