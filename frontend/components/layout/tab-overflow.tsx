"use client"

import * as React from "react"
import { ChevronDown } from "lucide-react"
import { cn } from "@/lib/utils"
import { useTabsStore, type Tab } from "@/stores/useTabsStore"
import { Badge } from "@/components/ui/badge"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"

interface TabOverflowProps {
  onTabClick: (tab: Tab) => void
  onCloseAll: () => void
}

export function TabOverflow({ onTabClick, onCloseAll }: TabOverflowProps) {
  const tabs = useTabsStore((s) => s.tabs)
  const activeTabId = useTabsStore((s) => s.activeTabId)
  const [open, setOpen] = React.useState(false)

  const showBadge = tabs.length > 5

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          className="flex h-[36px] w-8 shrink-0 items-center justify-center rounded-t-md text-muted-foreground hover:text-foreground hover:bg-muted/50 transition-colors gap-0.5"
          aria-label="Все вкладки"
        >
          <ChevronDown className="h-3.5 w-3.5" />
          {showBadge && (
            <Badge
              variant="secondary"
              className="h-4 min-w-[1rem] justify-center px-1 text-[10px] leading-none font-medium"
            >
              {tabs.length}
            </Badge>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-64 p-1" align="end">
        <ScrollArea className="max-h-[300px]">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => {
                onTabClick(tab)
                setOpen(false)
              }}
              className={cn(
                "flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-xs transition-colors text-left",
                tab.id === activeTabId
                  ? "bg-accent text-accent-foreground font-medium"
                  : "hover:bg-muted text-foreground",
              )}
            >
              {tab.isDirty && (
                <span className="shrink-0 text-destructive text-[10px]">●</span>
              )}
              <span className="truncate">{tab.title}</span>
            </button>
          ))}
        </ScrollArea>
        {tabs.length > 1 && (
          <>
            <div className="my-1 h-px bg-border" />
            <button
              onClick={() => {
                onCloseAll()
                setOpen(false)
              }}
              className="flex w-full items-center rounded-sm px-2 py-1.5 text-xs text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
            >
              Закрыть все
            </button>
          </>
        )}
      </PopoverContent>
    </Popover>
  )
}
