"use client"

import { useCompactMode } from "@/hooks/useCompactMode"

import Link from "next/link"
import { Plus, Search, MoreHorizontal, Copy, FileSpreadsheet, Loader2 } from "lucide-react"
import { cn } from "@/lib/utils"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

export interface DataToolbarMenuItem {
  label: string
  /** When defined, renders as a checkbox toggle item. */
  checked?: boolean
  onClick?: () => void
}

interface DataToolbarProps {
  title: string
  onCreateHref?: string
  onCopyClick?: (() => void) | null
  showSearch?: boolean
  /** Current search text (controlled). */
  searchValue?: string
  /** Called on every keystroke in the search input. */
  onSearchChange?: (value: string) => void
  /** Ref callback for the search <input> — allows parent to focus it (e.g. Ctrl+F). */
  searchInputRef?: React.RefCallback<HTMLInputElement>
  extraButtons?: React.ReactNode
  /** Custom items rendered at the top of the "More" dropdown. */
  menuItems?: DataToolbarMenuItem[]
  /** Called when user clicks "Настройка списка" in the More menu. */
  onColumnChooserClick?: () => void
  /** Called when user clicks "Экспорт в Excel". */
  onExportClick?: () => void
  /** True while export is in progress. */
  exporting?: boolean
}

export function DataToolbar({
  title,
  onCreateHref,
  onCopyClick,
  showSearch = true,
  searchValue,
  onSearchChange,
  searchInputRef,
  extraButtons,
  menuItems,
  onColumnChooserClick,
  onExportClick,
  exporting,
}: DataToolbarProps) {
  const compact = useCompactMode()
  return (
    <div className={cn("flex items-center justify-between border-b bg-card px-4", compact ? "py-1" : "py-2")}>
      <div className="flex items-center gap-2">
        {onCreateHref && (
          <Button size="sm" asChild>
            <Link href={onCreateHref}>
              <Plus className="mr-1.5 h-3.5 w-3.5" />
              Создать
            </Link>
          </Button>
        )}
        {onCopyClick !== undefined && (
          <TooltipProvider delayDuration={300}>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="icon"
                  className="h-8 w-8"
                  disabled={!onCopyClick}
                  onClick={onCopyClick ?? undefined}
                >
                  <Copy className="h-3.5 w-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Копировать (F9)</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
        {extraButtons}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm">
              Еще
              <MoreHorizontal className="ml-1.5 h-3.5 w-3.5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            {menuItems?.map((item, i) =>
              item.checked !== undefined ? (
                <DropdownMenuItem
                  key={i}
                  onSelect={(e) => {
                    e.preventDefault()
                    item.onClick?.()
                  }}
                >
                  <Checkbox
                    checked={item.checked}
                    className="mr-2 pointer-events-none"
                    tabIndex={-1}
                  />
                  {item.label}
                </DropdownMenuItem>
              ) : (
                <DropdownMenuItem key={i} onClick={item.onClick}>
                  {item.label}
                </DropdownMenuItem>
              )
            )}
            {menuItems && menuItems.length > 0 && <DropdownMenuSeparator />}
            <DropdownMenuItem onClick={onExportClick} disabled={exporting || !onExportClick}>
              {exporting ? (
                <><Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />Экспорт...</>
              ) : (
                <><FileSpreadsheet className="mr-2 h-3.5 w-3.5" />Экспорт в Excel</>
              )}
            </DropdownMenuItem>
            <DropdownMenuItem>Печать списка</DropdownMenuItem>
            <DropdownMenuItem onClick={onColumnChooserClick}>Настройка списка</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {showSearch && (
        <div className="relative w-60">
          <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            ref={searchInputRef}
            placeholder="Поиск (Ctrl+F)"
            className="h-8 pl-8 text-sm"
            value={searchValue ?? ""}
            onChange={(e) => onSearchChange?.(e.target.value)}
          />
        </div>
      )}
    </div>
  )
}
