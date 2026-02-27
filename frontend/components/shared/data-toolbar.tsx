"use client"

import Link from "next/link"
import { Plus, RefreshCw, Search, SlidersHorizontal, MoreHorizontal } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

interface DataToolbarProps {
  title: string
  onCreateHref?: string
  showSearch?: boolean
  extraButtons?: React.ReactNode
}

export function DataToolbar({
  title,
  onCreateHref,
  showSearch = true,
  extraButtons,
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
        <Button variant="outline" size="sm">
          <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          Обновить
        </Button>
        {extraButtons}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm">
              Еще
              <MoreHorizontal className="ml-1.5 h-3.5 w-3.5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
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
