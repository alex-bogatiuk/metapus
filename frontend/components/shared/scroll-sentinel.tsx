"use client"

import { useEffect, useRef } from "react"
import React from "react"
import { Loader2 } from "lucide-react"

interface ScrollSentinelProps {
  /** Called when the sentinel becomes visible in the viewport. */
  onIntersect: () => void
  /** Whether data is currently loading (shows spinner). */
  loading?: boolean
  /** Whether there is more data to load. Hidden when false. */
  enabled?: boolean
  /** Root margin for IntersectionObserver. Default "200px" (prefetch). */
  rootMargin?: string
  /** Scroll container to use as IntersectionObserver root.
   *  Pass the ref of the nearest overflow-auto/scroll parent.
   *  Without this the observer uses the viewport which is clipped by overflow containers. */
  scrollContainer?: React.RefObject<HTMLElement | null>
}

/**
 * Invisible sentinel element that triggers `onIntersect` when scrolled into view.
 * Used for infinite scroll — place at the bottom (or top) of a scrollable list.
 */
export function ScrollSentinel({
  onIntersect,
  loading = false,
  enabled = true,
  rootMargin = "200px",
  scrollContainer,
}: ScrollSentinelProps) {
  const ref = useRef<HTMLDivElement>(null)
  const loadingRef = useRef(loading)
  const onIntersectRef = useRef(onIntersect)

  useEffect(() => {
    loadingRef.current = loading
  }, [loading])

  useEffect(() => {
    onIntersectRef.current = onIntersect
  }, [onIntersect])

  useEffect(() => {
    if (!enabled || !ref.current) return

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting && !loadingRef.current) {
          onIntersectRef.current()
        }
      },
      { root: scrollContainer?.current ?? null, rootMargin },
    )

    observer.observe(ref.current)
    return () => observer.disconnect()
  }, [enabled, rootMargin, scrollContainer])

  if (!enabled) return null

  return (
    <div ref={ref} className="flex items-center justify-center py-4">
      {loading && <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />}
    </div>
  )
}
