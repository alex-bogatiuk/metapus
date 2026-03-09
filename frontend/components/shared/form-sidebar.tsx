"use client"

import { Info, Paperclip, PanelRightOpen, PanelRightClose, Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { fmtDate } from "@/lib/format"

interface FormSidebarMeta {
  updatedAt?: string
  updatedByUser?: { name: string } | null
  createdAt?: string
  createdByUser?: { name: string } | null
}

interface FormSidebarProps {
  collapsed: boolean
  onToggle: () => void
  meta?: FormSidebarMeta
  children?: React.ReactNode
}

export function FormSidebar({ collapsed, onToggle, meta, children }: FormSidebarProps) {
  return (
    <div
      className={cn(
        "flex flex-col shrink-0 border-l border-border bg-card/30 transition-all duration-300 ease-in-out overflow-hidden",
        collapsed ? "w-9" : "w-72"
      )}
    >
      {/* Collapsed indicator */}
      <div
        className={cn(
          "flex items-center justify-center border-b shrink-0 bg-muted/20 transition-all duration-300",
          !collapsed ? "h-0 opacity-0 pointer-events-none border-b-0" : "h-11 opacity-100"
        )}
      >
        <TooltipProvider delayDuration={300}>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-foreground hover:bg-transparent" onClick={onToggle}>
                <Info className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="left">Развернуть панель</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>

      {/* Content */}
      <div className={cn("flex-1 overflow-y-auto transition-opacity duration-200", collapsed ? "opacity-0 pointer-events-none" : "opacity-100")}>
        <div className="p-4 space-y-6">
          {/* Files section */}
          <div>
            <div className="flex items-center justify-between text-muted-foreground mb-3">
              <div className="flex items-center gap-2 text-sm font-medium">
                <Paperclip className="h-4 w-4" />
                Файлы
              </div>
              <Button variant="ghost" size="icon" className="h-6 w-6"><Plus className="h-4 w-4" /></Button>
            </div>
            <div className="text-xs text-muted-foreground/60 text-center py-4">Нет прикрепленных файлов</div>
          </div>

          {/* Extra children (page-specific content) */}
          {children}
        </div>

        {/* Metadata (Изменено / Создано) */}
        <div className="p-4 border-t border-border/50 text-xs text-muted-foreground space-y-2">
          <div>
            <span className="block text-muted-foreground/70 mb-0.5">Изменено:</span>
            <span className="text-foreground/80">
              {meta?.updatedAt
                ? `${fmtDate(meta.updatedAt)}${meta.updatedByUser?.name ? ` — ${meta.updatedByUser.name}` : ""}`
                : "—"}
            </span>
          </div>
          <div>
            <span className="block text-muted-foreground/70 mb-0.5">Создано:</span>
            <span className="text-foreground/80">
              {meta?.createdAt
                ? `${fmtDate(meta.createdAt)}${meta.createdByUser?.name ? ` — ${meta.createdByUser.name}` : ""}`
                : "—"}
            </span>
          </div>
        </div>
      </div>

      {/* Toggle button */}
      <div className={cn("flex items-center border-t h-9 mt-auto shrink-0", collapsed ? "justify-center" : "justify-end px-2")}>
        <TooltipProvider delayDuration={300}>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" onClick={onToggle}>
                {collapsed ? <PanelRightOpen className="h-4 w-4" /> : <PanelRightClose className="h-4 w-4" />}
              </Button>
            </TooltipTrigger>
            <TooltipContent side="left">{collapsed ? "Показать панель" : "Скрыть панель"}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>
    </div>
  )
}
