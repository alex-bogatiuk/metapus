import { createTabStateKey, useTabStateStore } from "@/stores/useTabStateStore"
import { usePathname } from "next/navigation"
import { useEffect, type RefObject } from "react"

const SCROLL_KEY = createTabStateKey<number>("scrollTop")

/**
 * Restores and saves scroll position for list pages on tab switch (M2).
 *
 * When the user scrolls a list, switches to another tab, and comes back,
 * the scroll position is exactly where they left it.
 *
 * Usage:
 * ```tsx
 * const scrollRef = useRef<HTMLDivElement>(null)
 * useScrollRestore(scrollRef)
 * // Then: <ScrollArea ref={scrollRef} ... />
 * ```
 */
export function useScrollRestore(
  containerRef: RefObject<HTMLElement | null>,
): void {
  const pathname = usePathname()

  useEffect(() => {
    const el = containerRef.current
    if (!el) return

    // Restore saved scroll position on mount
    const saved = useTabStateStore.getState().get(pathname, SCROLL_KEY)
    let frame: number | undefined
    if (saved && saved > 0) {
      // Use requestAnimationFrame to ensure content has rendered
      frame = requestAnimationFrame(() => {
        // Radix ScrollArea uses a nested [data-radix-scroll-area-viewport]
        const viewport = el.querySelector<HTMLElement>("[data-radix-scroll-area-viewport]") ?? el
        viewport.scrollTop = saved
      })
    }

    // Save scroll position on unmount (tab switch)
    return () => {
      if (frame !== undefined) cancelAnimationFrame(frame)
      const viewport = el.querySelector<HTMLElement>("[data-radix-scroll-area-viewport]") ?? el
      const scrollTop = viewport.scrollTop
      useTabStateStore.getState().set(pathname, SCROLL_KEY, scrollTop)
    }

  }, [pathname, containerRef])
}
