"use client"

import { useEffect } from "react"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"

/**
 * ThemeProvider — applies the `dark` class to <html> based on user preference.
 *
 * Supports three modes:
 * - "light"  → removes `dark` class
 * - "dark"   → adds `dark` class
 * - "system" → follows OS preference via matchMedia
 *
 * Must be rendered inside the <body> (client component).
 * Works together with the inline theme script in layout.tsx to avoid FOUC.
 */
export function ThemeProvider({ children }: { children: React.ReactNode }) {
    const theme = useUserPrefsStore((s) => s.interface.theme)
    const accentColor = useUserPrefsStore((s) => s.interface.accentColor)

    useEffect(() => {
        const root = document.documentElement
        const resolved = theme ?? "light"

        function apply(mode: "light" | "dark") {
            if (mode === "dark") {
                root.classList.add("dark")
            } else {
                root.classList.remove("dark")
            }
        }

        if (resolved === "system") {
            const mq = window.matchMedia("(prefers-color-scheme: dark)")
            apply(mq.matches ? "dark" : "light")

            const handler = (e: MediaQueryListEvent) => apply(e.matches ? "dark" : "light")
            mq.addEventListener("change", handler)
            return () => mq.removeEventListener("change", handler)
        }

        apply(resolved)
    }, [theme])

    useEffect(() => {
        const root = document.documentElement
        if (!accentColor || accentColor === "yellow") {
            root.removeAttribute("data-accent")
        } else {
            root.setAttribute("data-accent", accentColor)
        }
    }, [accentColor])

    // Compact mode — data-compact attribute for CSS density adjustments
    const compactMode = useUserPrefsStore((s) => s.interface.compactMode)

    useEffect(() => {
        document.documentElement.toggleAttribute("data-compact", !!compactMode)
    }, [compactMode])

    return <>{children}</>
}
