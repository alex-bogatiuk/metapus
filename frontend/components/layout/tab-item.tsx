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

function tabClassName({
  isActive,
  isDragging,
}: {
  isActive: boolean
  isDragging?: boolean
}) {
  return cn(
    "group relative flex h-7 min-w-[7.5rem] max-w-[12.5rem] flex-[1_1_0] items-center gap-1.5 rounded-md pl-2 pr-1.5 text-[13px] select-none focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/25",
    isActive
      ? "z-10 bg-accent font-medium text-foreground"
      : "text-muted-foreground hover:bg-muted hover:text-foreground",
    isDragging && "z-50 bg-popover shadow-floating ring-1 ring-border",
  )
}

function TabTitle({ tab }: { tab: Tab }) {
  // Metadata may arrive after the tab opens; re-render so entity tabs pick up their icon.
  useMetadataStore((s) => s.loaded)
  return (
    <>
    {React.createElement(getRouteIcon(tab.url), { "aria-hidden": true, className: "size-3.5 shrink-0 opacity-80" })}
    <span className="flex-1 truncate text-left">
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

  const handleCopyUrl = React.useCallback(() => {
    const fullUrl = `${window.location.origin}${tab.url}`
    navigator.clipboard.writeText(fullUrl).then(() => {
      toast.success("Ссылка скопирована в буфер обмена")
    })
  }, [tab.url])

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
                onClick={() => onTabClick(tab)}
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
                <TabTitle tab={tab} />

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
