"use client"

import { useEffect, useRef, type RefObject } from "react"

// Skip only the initial cached query. A changed loader represents a changed
// query and must reload even when this tab was restored from cache.
export function useCachedLoad(
  load: () => Promise<void>,
  hasCache: boolean,
  scrollRef?: RefObject<HTMLElement | null>,
) {
  const cachedLoader = useRef(hasCache ? load : null)
  useEffect(() => {
    if (cachedLoader.current === load) return
    cachedLoader.current = null
    if (scrollRef?.current) scrollRef.current.scrollTop = 0
    void load()
  }, [load, scrollRef])
}
