"use client"

import * as React from "react"
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core"
import { restrictToHorizontalAxis } from "@dnd-kit/modifiers"
import {
  SortableContext,
  horizontalListSortingStrategy,
  sortableKeyboardCoordinates,
} from "@dnd-kit/sortable"
import { ChevronLeft, ChevronRight } from "lucide-react"
import { useTabsStore, type Tab } from "@/stores/useTabsStore"
import { TabItem } from "./tab-item"

interface TabBarProps {
  onTabClick: (tab: Tab) => void
  onTabClose: (e: React.SyntheticEvent, tab: Tab) => void
  onCloseOthers: (keepId: string) => void
  onCloseRight: (id: string) => void
}

export function TabBar({ onTabClick, onTabClose, onCloseOthers, onCloseRight }: TabBarProps) {
  const tabs = useTabsStore((s) => s.tabs)
  const activeTabId = useTabsStore((s) => s.activeTabId)
  const reorderTab = useTabsStore((s) => s.reorderTab)
  const closingTabIds = useTabsStore((s) => s.closingTabIds)
  const activeTabsCount = React.useMemo(
    () => tabs.filter((t) => !closingTabIds.includes(t.id)).length,
    [tabs, closingTabIds],
  )

  const tabBarRef = React.useRef<HTMLDivElement>(null)
  const [canScrollLeft, setCanScrollLeft] = React.useState(false)
  const [canScrollRight, setCanScrollRight] = React.useState(false)
  const [hasDirtyLeft, setHasDirtyLeft] = React.useState(false)
  const [hasDirtyRight, setHasDirtyRight] = React.useState(false)
  const sortableTabIds = React.useMemo(() => tabs.map((tab) => tab.id), [tabs])
  const hasOverflow = canScrollLeft || canScrollRight

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  )

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

  const scrollTabIntoView = React.useCallback((tabId: string, behavior: ScrollBehavior = "smooth") => {
    const el = tabBarRef.current
    if (!el) return

    const tabEl = el.querySelector<HTMLElement>(
      `[data-tab-id="${CSS.escape(tabId)}"]`,
    )
    tabEl?.scrollIntoView({ behavior, block: "nearest", inline: "nearest" })
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
    scrollTabIntoView(activeTabId)
    // Correct once after scroll controls have occupied their final width.
    const t = setTimeout(() => {
      scrollTabIntoView(activeTabId, "auto")
      updateScrollState()
    }, 350)
    return () => clearTimeout(t)
  }, [activeTabId, scrollTabIntoView, updateScrollState])



  const handleDragEnd = React.useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event

      if (!over || active.id === over.id) return

      const didReorder = reorderTab(String(active.id), String(over.id))
      if (!didReorder) return

      requestAnimationFrame(() => {
        scrollTabIntoView(String(active.id), "auto")
        updateScrollState()
      })
    },
    [reorderTab, scrollTabIntoView, updateScrollState],
  )

  const scrollToHiddenTab = React.useCallback((direction: "left" | "right") => {
    const el = tabBarRef.current
    if (!el) return

    const containerRect = el.getBoundingClientRect()
    const tabEls = Array.from(
      el.querySelectorAll<HTMLElement>("[data-tab-id]"),
    )
    let target: HTMLElement | undefined

    if (direction === "right") {
      target = tabEls.find(
        (tabEl) => tabEl.getBoundingClientRect().right > containerRect.right + 1,
      )
    } else {
      for (let index = tabEls.length - 1; index >= 0; index -= 1) {
        if (tabEls[index].getBoundingClientRect().left < containerRect.left - 1) {
          target = tabEls[index]
          break
        }
      }
    }

    target?.scrollIntoView({
      behavior: "smooth",
      block: "nearest",
      inline: direction === "left" ? "start" : "end",
    })
  }, [])

  return (
    <div className="flex flex-1 items-center min-w-0">
      {/* Left scroll indicator */}
      {hasOverflow && (
        <button
          disabled={!canScrollLeft}
          onClick={() => scrollToHiddenTab("left")}
          className="relative z-10 flex h-7 w-6 shrink-0 items-center justify-center rounded-md transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-30"
          aria-label="Прокрутить влево"
        >
          <ChevronLeft className="h-3.5 w-3.5 text-muted-foreground" />
          {hasDirtyLeft && (
            <span className="absolute top-1 right-0 h-1.5 w-1.5 rounded-full bg-destructive" />
          )}
        </button>
      )}

      {/* Scrollable tab container */}
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        modifiers={[restrictToHorizontalAxis]}
        autoScroll
        onDragEnd={handleDragEnd}
      >
        <SortableContext
          items={sortableTabIds}
          strategy={horizontalListSortingStrategy}
        >
          <div
            ref={tabBarRef}
            className="flex flex-1 items-center gap-0.5 overflow-x-auto scrollbar-hide"
            onScroll={handleScroll}
          >
            {tabs.map((tab) => (
              <TabItem
                key={tab.id}
                tab={tab}
                isActive={tab.id === activeTabId}
                tabCount={activeTabsCount}
                onTabClick={onTabClick}
                onTabClose={onTabClose}
                onCloseOthers={onCloseOthers}
                onCloseRight={onCloseRight}
              />
            ))}
          </div>
        </SortableContext>
      </DndContext>

      {/* Right scroll indicator */}
      {hasOverflow && (
        <button
          disabled={!canScrollRight}
          onClick={() => scrollToHiddenTab("right")}
          className="relative z-10 flex h-7 w-6 shrink-0 items-center justify-center rounded-md transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-30"
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
