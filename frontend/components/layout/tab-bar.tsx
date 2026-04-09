"use client"

import * as React from "react"
import { ChevronLeft, ChevronRight } from "lucide-react"
import { useTabsStore, type Tab } from "@/stores/useTabsStore"
import { TabItem } from "./tab-item"

interface TabBarProps {
  onTabClick: (tab: Tab) => void
  onTabClose: (e: React.MouseEvent, tab: Tab) => void
  onCloseOthers: (keepId: string) => void
  onCloseRight: (id: string) => void
}

export function TabBar({ onTabClick, onTabClose, onCloseOthers, onCloseRight }: TabBarProps) {
  const tabs = useTabsStore((s) => s.tabs)
  const activeTabId = useTabsStore((s) => s.activeTabId)

  const tabBarRef = React.useRef<HTMLDivElement>(null)
  const [canScrollLeft, setCanScrollLeft] = React.useState(false)
  const [canScrollRight, setCanScrollRight] = React.useState(false)
  const [hasDirtyLeft, setHasDirtyLeft] = React.useState(false)
  const [hasDirtyRight, setHasDirtyRight] = React.useState(false)

  // Adaptive tab widths based on count
  const tabMaxWidth =
    tabs.length > 10
      ? "max-w-[10rem]"
      : tabs.length > 7
        ? "max-w-[12rem]"
        : "max-w-[14rem]"
  const tabMinWidth = tabs.length > 10 ? "min-w-[6rem]" : "min-w-[8rem]"

  // ── Scroll state detection ──
  const updateScrollState = React.useCallback(() => {
    const el = tabBarRef.current
    if (!el) return
    const left = el.scrollLeft > 0
    const right = el.scrollLeft + el.clientWidth < el.scrollWidth - 1
    setCanScrollLeft(left)
    setCanScrollRight(right)

    // Dirty-dot detection: check which dirty tabs are outside visible area
    const containerRect = el.getBoundingClientRect()
    let dirtyLeft = false
    let dirtyRight = false

    const dirtyEls = el.querySelectorAll<HTMLElement>("[data-dirty='true']")
    for (const dirtyEl of dirtyEls) {
      const rect = dirtyEl.getBoundingClientRect()
      if (rect.right < containerRect.left + 4) dirtyLeft = true
      if (rect.left > containerRect.right - 4) dirtyRight = true
    }
    setHasDirtyLeft(dirtyLeft)
    setHasDirtyRight(dirtyRight)
  }, [])

  // Debounced scroll handler via rAF
  const rafRef = React.useRef<number>(0)
  const handleScroll = React.useCallback(() => {
    cancelAnimationFrame(rafRef.current)
    rafRef.current = requestAnimationFrame(updateScrollState)
  }, [updateScrollState])

  // ResizeObserver + tabs.length change
  React.useEffect(() => {
    updateScrollState()
    const el = tabBarRef.current
    if (!el) return
    const ro = new ResizeObserver(updateScrollState)
    ro.observe(el)
    return () => {
      ro.disconnect()
      cancelAnimationFrame(rafRef.current)
    }
  }, [tabs.length, updateScrollState])

  // Auto-scroll to active tab
  React.useEffect(() => {
    const el = tabBarRef.current
    if (!el) return
    const activeEl = el.querySelector<HTMLElement>(
      `[data-tab-id="${CSS.escape(activeTabId)}"]`,
    )
    if (activeEl) {
      activeEl.scrollIntoView({ behavior: "smooth", block: "nearest", inline: "nearest" })
    }
    // Small delay for updateScrollState after scrollIntoView finishes
    const t = setTimeout(updateScrollState, 350)
    return () => clearTimeout(t)
  }, [activeTabId, updateScrollState])

  const scrollLeft = () =>
    tabBarRef.current?.scrollBy({ left: -200, behavior: "smooth" })
  const scrollRight = () =>
    tabBarRef.current?.scrollBy({ left: 200, behavior: "smooth" })

  return (
    <div className="flex flex-1 items-end min-w-0">
      {/* Left scroll indicator */}
      {canScrollLeft && (
        <button
          onClick={scrollLeft}
          className="relative flex h-[36px] w-6 shrink-0 items-center justify-center bg-gradient-to-r from-muted/40 to-transparent z-10 hover:from-muted/60 transition-colors"
          aria-label="Прокрутить влево"
        >
          <ChevronLeft className="h-3.5 w-3.5 text-muted-foreground" />
          {hasDirtyLeft && (
            <span className="absolute top-1 right-0 h-1.5 w-1.5 rounded-full bg-destructive" />
          )}
        </button>
      )}

      {/* Scrollable tab container */}
      <div
        ref={tabBarRef}
        className="flex flex-1 items-end gap-1 overflow-x-auto scrollbar-hide"
        onScroll={handleScroll}
      >
        {tabs.map((tab) => (
          <TabItem
            key={tab.id}
            tab={tab}
            isActive={tab.id === activeTabId}
            tabCount={tabs.length}
            tabMaxWidth={tabMaxWidth}
            tabMinWidth={tabMinWidth}
            onTabClick={onTabClick}
            onTabClose={onTabClose}
            onCloseOthers={onCloseOthers}
            onCloseRight={onCloseRight}
          />
        ))}
      </div>

      {/* Right scroll indicator */}
      {canScrollRight && (
        <button
          onClick={scrollRight}
          className="relative flex h-[36px] w-6 shrink-0 items-center justify-center bg-gradient-to-l from-muted/40 to-transparent z-10 hover:from-muted/60 transition-colors"
          aria-label="Прокрутить вправо"
        >
          <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
          {hasDirtyRight && (
            <span className="absolute top-1 left-0 h-1.5 w-1.5 rounded-full bg-destructive" />
          )}
        </button>
      )}
    </div>
  )
}
