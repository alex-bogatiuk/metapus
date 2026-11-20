"use client"

import * as React from "react"
import { useSortable } from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { X } from "lucide-react"
import { cn } from "@/lib/utils"
import { toast } from "sonner"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuGroup,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { useTabsStore, type Tab } from "@/stores/useTabsStore"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { getRouteIcon } from "@/lib/entity-icon"
import { copyText } from "@/lib/clipboard"

interface TabItemProps {
  tab: Tab
  isActive: boolean
  tabCount: number
  onTabClick: (tab: Tab) => void
  onTabClose: (e: React.SyntheticEvent, tab: Tab) => void
  onCloseOthers: (keepId: string) => void
  onCloseRight: (id: string) => void
}

/** Color and border transitions for hover/active tab styling. */
const TAB_COLOR_TRANSITION =
  "background-color 150ms ease, color 150ms ease, border-color 150ms ease"

/**
 * Browser-style divider on each tab's left edge, centred in the 2px strip gap.
 * Like Chrome, it hides wherever a filled tab already marks the boundary:
 * the first tab, the active or hovered tab, and the tab right after them.
 */
const TAB_SEPARATOR =
  "before:pointer-events-none before:absolute before:left-[-1.5px] before:top-1/2 before:h-4 before:w-px before:-translate-y-1/2 before:bg-border before:content-[''] before:transition-opacity before:duration-150 " +
  "first:before:opacity-0 hover:before:opacity-0 aria-[current=page]:before:opacity-0 " +
  "[:hover+&]:before:opacity-0 [[aria-current=page]+&]:before:opacity-0"

/** Clipped titles dissolve into the tab instead of ending in an ellipsis. */
const TAB_TITLE_FADE =
  "data-[overflow=true]:[-webkit-mask-image:linear-gradient(to_right,black_calc(100%_-_1.25rem),transparent)] data-[overflow=true]:[mask-image:linear-gradient(to_right,black_calc(100%_-_1.25rem),transparent)]"

/** Tracks whether the element's text is wider than its box, so only clipped titles fade. */
function useOverflowFlag<E extends HTMLElement>(text: string) {
  const ref = React.useRef<E>(null)
  const [overflow, setOverflow] = React.useState(false)
  React.useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const measure = () => setOverflow(el.scrollWidth > el.clientWidth + 1)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    // Web fonts can land after the first measure and change the text width.
    let alive = true
    void document.fonts?.ready.then(() => { if (alive) measure() })
    return () => {
      alive = false
      observer.disconnect()
    }
  }, [text])
  return [ref, overflow] as const
}

function tabClassName({
  isActive,
  isDragging,
}: {
  isActive: boolean
  isDragging?: boolean
}) {
  return cn(
    "group relative flex h-7 min-w-[7.5rem] max-w-[12.5rem] flex-[1_1_0] items-center gap-1.5 rounded-md pl-2 pr-1.5 text-[13px] select-none focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/25",
    TAB_SEPARATOR,
    isActive
      ? "z-10 bg-accent font-medium text-foreground"
      : "text-muted-foreground hover:bg-muted hover:text-foreground",
    isDragging && "z-50 bg-popover shadow-floating ring-1 ring-border before:opacity-0",
  )
}

function TabTitle({ tab, isActive }: { tab: Tab; isActive: boolean }) {
  // Metadata may arrive after the tab opens; re-render so entity tabs pick up their icon.
  useMetadataStore((s) => s.loaded)
  // Active tabs use a heavier weight, which widens the text without resizing the box.
  const [titleRef, overflow] = useOverflowFlag<HTMLSpanElement>(`${isActive ? 1 : 0}${tab.isDirty ? "● " : ""}${tab.title}`)
  return (
    <>
    {React.createElement(getRouteIcon(tab.url), { "aria-hidden": true, className: "size-3.5 shrink-0 opacity-80" })}
    <span
      ref={titleRef}
      data-overflow={overflow ? "true" : undefined}
      className={cn("min-w-0 flex-1 overflow-hidden whitespace-nowrap text-left", TAB_TITLE_FADE)}
    >
      {tab.isDirty && (
        <span className="mr-1 text-destructive">●</span>
      )}
      {tab.title}
    </span>
    </>
  )
}

export const TabItem = React.memo(function TabItem({
  tab,
  isActive,
  tabCount,
  onTabClick,
  onTabClose,
  onCloseOthers,
  onCloseRight,
}: TabItemProps) {
  const [tooltipOpen, setTooltipOpen] = React.useState(false)
  const hoverTimer = React.useRef<ReturnType<typeof setTimeout>>(undefined)
  const isClosing = useTabsStore((s) => s.closingTabIds.includes(tab.id))

  React.useEffect(() => {
    if (isClosing) {
      setTooltipOpen(false)
      clearTimeout(hoverTimer.current)
    }
  }, [isClosing])

  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id: tab.id,
    disabled: isClosing,
    transition: {
      duration: 200,
      easing: "cubic-bezier(0.2, 0, 0, 1)",
    },
  })

  const style = React.useMemo<React.CSSProperties>(
    () => {
      if (isClosing) {
        return {
          maxWidth: 0,
          minWidth: 0,
          paddingLeft: 0,
          paddingRight: 0,
          marginRight: "-0.25rem",
          borderWidth: 0,
          opacity: 0,
          transform: "scale(0.92)",
          transition: "all 180ms cubic-bezier(0.32, 0.72, 0, 1)",
          pointerEvents: "none",
          overflow: "hidden",
        }
      }
      return {
        // Translate-only: strips scaleX/scaleY that deform text and cause subpixel blur
        transform: CSS.Translate.toString(transform),
        // Only apply transform transition when dnd-kit specifies it (displaced neighbors).
        // During active drag or upon drop reordering, transition is undefined to avoid double displacement overshoot ("замах").
        transition: transition
          ? `${transition}, ${TAB_COLOR_TRANSITION}`
          : TAB_COLOR_TRANSITION,
        willChange: isDragging ? 'transform' : undefined,
      }
    },
    [isClosing, transform, transition, isDragging],
  )

  const handlePointerEnter = React.useCallback(() => {
    hoverTimer.current = setTimeout(() => setTooltipOpen(true), 400)
  }, [])

  const handlePointerLeave = React.useCallback(() => {
    clearTimeout(hoverTimer.current)
    setTooltipOpen(false)
  }, [])

  // Close tooltip when context menu opens (right-click)
  const handleContextMenuOpen = React.useCallback((open: boolean) => {
    if (open) {
      clearTimeout(hoverTimer.current)
      setTooltipOpen(false)
    }
  }, [])

  // Double click on the open tab and the context menu both copy the tab's full link.
  const handleCopyUrl = React.useCallback(() => {
    void copyText(`${window.location.origin}${tab.url}`).then(copied => {
      if (copied) toast.success("Ссылка скопирована в буфер обмена")
      else toast.error("Не удалось скопировать ссылку")
    })
  }, [tab.url])

  // Whether the tab was already open when the current click sequence began: a double click
  // copies the link only then, so double-clicking a background tab just opens it.
  const openAtFirstClick = React.useRef(false)
  const handleClick = React.useCallback(
    (e: React.MouseEvent) => {
      if (e.detail <= 1) openAtFirstClick.current = isActive
      onTabClick(tab)
    },
    [isActive, onTabClick, tab],
  )
  const handleDoubleClick = React.useCallback(() => {
    if (openAtFirstClick.current) handleCopyUrl()
  }, [handleCopyUrl])

  const handleCloseKeyDown = React.useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault()
        onTabClose(e, tab)
      }
    },
    [onTabClose, tab],
  )

  return (
    <TooltipProvider delayDuration={0}>
      <Tooltip open={tooltipOpen} onOpenChange={setTooltipOpen}>
        <ContextMenu onOpenChange={handleContextMenuOpen}>
          <ContextMenuTrigger asChild>
            <TooltipTrigger asChild>
              <button
                ref={setNodeRef}
                type="button"
                data-tab-id={tab.id}
                data-dirty={tab.isDirty ? "true" : undefined}
                style={style}
                aria-label={`Вкладка: ${tab.title}`}
                aria-current={isActive ? "page" : undefined}
                onClick={handleClick}
                onDoubleClick={handleDoubleClick}
                onAuxClick={(e) => {
                  if (e.button === 1) onTabClose(e, tab)
                }}
                onPointerEnter={handlePointerEnter}
                onPointerLeave={handlePointerLeave}
                className={tabClassName({
                  isActive,
                  isDragging,
                })}
                {...attributes}
                {...listeners}
              >
                {/* Title + dirty indicator */}
                <TabTitle tab={tab} isActive={isActive} />

                {/* Close button — show if not the only tab */}
                {tabCount > 1 && (
                  <span
                    role="button"
                    tabIndex={0}
                    onClick={(e) => onTabClose(e, tab)}
                    onKeyDown={handleCloseKeyDown}
                    onPointerDown={(e) => e.stopPropagation()}
                    className={cn(
                      "flex size-[18px] shrink-0 items-center justify-center rounded-[5px] transition-colors",
                      "hover:bg-foreground/10 hover:text-foreground",
                      isActive
                        ? "text-muted-foreground"
                        : "opacity-0 group-hover:opacity-100 text-muted-foreground/60",
                    )}
                  >
                    <X className="size-3" />
                  </span>
                )}
              </button>
            </TooltipTrigger>
          </ContextMenuTrigger>
          <ContextMenuContent className="w-52">
            <ContextMenuGroup>
              <ContextMenuItem onClick={handleCopyUrl}>
                Копировать ссылку
              </ContextMenuItem>
            </ContextMenuGroup>
            <ContextMenuSeparator />
            <ContextMenuGroup>
              <ContextMenuItem
                onClick={() => onCloseOthers(tab.id)}
                disabled={tabCount <= 1}
              >
                Закрыть другие вкладки
              </ContextMenuItem>
              <ContextMenuItem
                onClick={() => onCloseRight(tab.id)}
              >
                Закрыть вкладки справа
              </ContextMenuItem>
            </ContextMenuGroup>
            <ContextMenuSeparator />
            <ContextMenuGroup>
              <ContextMenuItem
                onClick={(e) => onTabClose(e, tab)}
                disabled={tabCount <= 1}
                className="text-destructive focus:text-destructive"
              >
                Закрыть
              </ContextMenuItem>
            </ContextMenuGroup>
          </ContextMenuContent>
        </ContextMenu>
        <TooltipContent side="bottom" className="max-w-xs">
          {tab.title}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
})
