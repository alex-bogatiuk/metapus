"use client"

import Link from "next/link"
import { Plus, Search, SlidersHorizontal, MoreHorizontal, Copy } from "lucide-react"
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
  extraButtons?: React.ReactNode
  /** Custom items rendered at the top of the "Еще" dropdown. */
  menuItems?: DataToolbarMenuItem[]
}

export function DataToolbar({
  title,
  onCreateHref,
  onCopyClick,
  showSearch = true,
  extraButtons,
  menuItems,
}: DataToolbarProps) {
  return (
    <div className="flex items-center justify-between border-b bg-card px-4 py-2">
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
            <DropdownMenuItem>Экспорт в Excel</DropdownMenuItem>
            <DropdownMenuItem>Печать списка</DropdownMenuItem>
            <DropdownMenuItem>Настройка списка</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {showSearch && (
        <div className="relative w-60">
          <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input placeholder="Поиск (Ctrl+F)" className="h-8 pl-8 text-sm" />
        </div>
      )}
    </div>
  )
}
