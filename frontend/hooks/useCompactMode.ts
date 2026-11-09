/**
 * useCompactMode — reads the compactMode flag from user preferences.
 *
 * Components use this hook when they need JS-level branching (e.g. changing
 * className strings via cn()). For purely CSS-driven changes, prefer
 * the `[data-compact]` attribute selector on <html>.
 */

import { useUserPrefsStore } from "@/stores/useUserPrefsStore"

export function useCompactMode(): boolean {
    return useUserPrefsStore((s) => s.interface.compactMode ?? false)
}
