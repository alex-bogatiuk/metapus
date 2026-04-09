"use client"

import * as React from "react"
import { X } from "lucide-react"
import { cn } from "@/lib/utils"
import { toast } from "sonner"
import {
  ContextMenu,
  ContextMenuContent,
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
import type { Tab } from "@/stores/useTabsStore"

interface TabItemProps {
  tab: Tab
  isActive: boolean
  tabCount: number
  tabMaxWidth: string
  tabMinWidth: string
  onTabClick: (tab: Tab) => void
  onTabClose: (e: React.MouseEvent, tab: Tab) => void
  onCloseOthers: (keepId: string) => void
  onCloseRight: (id: string) => void
}

export const TabItem = React.memo(function TabItem({
  tab,
  isActive,
  tabCount,
  tabMaxWidth,
  tabMinWidth,
  onTabClick,
  onTabClose,
  onCloseOthers,
  onCloseRight,
}: TabItemProps) {
  const [tooltipOpen, setTooltipOpen] = React.useState(false)
  const hoverTimer = React.useRef<ReturnType<typeof setTimeout>>(undefined)

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

  return (
    <TooltipProvider delayDuration={0}>
      <Tooltip open={tooltipOpen} onOpenChange={setTooltipOpen}>
        <ContextMenu onOpenChange={handleContextMenuOpen}>
          <ContextMenuTrigger asChild>
            <TooltipTrigger asChild>
              <button
                data-tab-id={tab.id}
                data-dirty={tab.isDirty ? "true" : undefined}
                onClick={() => onTabClick(tab)}
                onAuxClick={(e) => {
                  if (e.button === 1) onTabClose(e, tab)
                }}
                onPointerEnter={handlePointerEnter}
                onPointerLeave={handlePointerLeave}
                className={cn(
                  "group flex h-[36px] shrink-0 items-center gap-2 rounded-t-md border-x border-t-2 px-3 text-xs font-medium transition-colors",
                  tabMinWidth,
                  tabMaxWidth,
                  isActive
                    ? "relative z-10 -mb-px bg-background text-foreground border-border border-t-primary"
                    : "relative bg-muted/30 text-muted-foreground border-border border-t-border hover:bg-muted hover:text-foreground",
                )}
              >
                {/* Title + dirty indicator */}
                <span className="flex-1 truncate text-left">
                  {tab.isDirty && (
                    <span className="mr-1 text-destructive">●</span>
                  )}
                  {tab.title}
                </span>

                {/* Close button — show if not the only tab */}
                {tabCount > 1 && (
                  <span
                    role="button"
                    tabIndex={0}
                    onClick={(e) => onTabClose(e, tab)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        onTabClose(e as unknown as React.MouseEvent, tab)
                      }
                    }}
                    className={cn(
                      "flex h-4 w-4 shrink-0 items-center justify-center rounded-sm transition-colors",
                      "hover:bg-muted hover:text-foreground",
                      isActive
                        ? "text-muted-foreground"
                        : "opacity-0 group-hover:opacity-100 text-muted-foreground/60",
                    )}
                  >
                    <X className="h-3 w-3" />
                  </span>
                )}
              </button>
            </TooltipTrigger>
          </ContextMenuTrigger>
          <ContextMenuContent className="w-52">
            <ContextMenuItem onClick={handleCopyUrl}>
              Копировать ссылку
            </ContextMenuItem>
            <ContextMenuSeparator />
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
            <ContextMenuSeparator />
            <ContextMenuItem
              onClick={(e) => onTabClose(e as unknown as React.MouseEvent, tab)}
              disabled={tabCount <= 1}
              className="text-destructive focus:text-destructive"
            >
              Закрыть
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
        <TooltipContent side="bottom" className="max-w-xs">
          {tab.title}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
})
