// frontend/components/layout/favorites-popover.tsx
"use client"

import * as React from "react"
import { useRouter } from "next/navigation"
import { Star, MoreHorizontal, Trash2, Search } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"
import { useFavoritesStore } from "@/stores/useFavoritesStore"
import { useTabsStore } from "@/stores/useTabsStore"
import { useShortcut } from "@/hooks/useShortcut"
import { getEntityIcon } from "@/lib/entity-icon"
import type { FavoriteItem } from "@/types/user-prefs"

const _searchThreshold = 8

/**
 * FavoritesPopover — header trigger (⭐) + popover dropdown with favorites list.
 *
 * Replaces the old SidebarFavorites. Positioned next to NotificationBell in the header.
 * Each item shows an entity-type icon + truncated title + more menu.
 */
export function FavoritesPopover() {
  const router = useRouter()
  const openTab = useTabsStore((s) => s.openTab)

  const items = useFavoritesStore((s) => s.items)
  const isLoaded = useFavoritesStore((s) => s.isLoaded)
  const removeFavorite = useFavoritesStore((s) => s.removeFavorite)

  const [open, setOpen] = React.useState(false)
  const [search, setSearch] = React.useState("")

  const hasFavorites = isLoaded && items.length > 0
  const showSearch = items.length >= _searchThreshold

  // Filter items by search query
  const filteredItems = React.useMemo(() => {
    if (!search.trim()) return items
    const q = search.toLowerCase().trim()
    return items.filter((item) => item.title.toLowerCase().includes(q))
  }, [items, search])

  // Navigate to favorite item
  const handleNavigate = React.useCallback(
    (item: FavoriteItem) => {
      const result = openTab({
        id: item.url,
        title: item.title,
        url: item.url,
      })
      if (result.warning) toast.warning(result.warning)
      router.push(item.url)
      setOpen(false)
    },
    [openTab, router],
  )

  // Remove from favorites
  const handleRemove = React.useCallback(
    (e: React.MouseEvent, item: FavoriteItem) => {
      e.stopPropagation()
      removeFavorite(item.entityType, item.entityId)
      toast.success("Убрано из избранного")
    },
    [removeFavorite],
  )

  // Reset search when popover closes
  React.useEffect(() => {
    if (!open) setSearch("")
  }, [open])

  // Keyboard shortcut: Ctrl+Shift+F
  useShortcut(
    "nav.favorites",
    "mod+shift+f",
    "Избранное",
    "navigation",
    () => setOpen((prev) => !prev),
  )

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <Button
              id="favorites-trigger"
              variant="ghost"
              size="icon"
              className={cn(
                "relative h-8 w-8",
                open && "bg-accent text-accent-foreground",
              )}
              aria-label={`Избранное${hasFavorites ? ` (${items.length})` : ""}`}
            >
              <Star className="h-4 w-4" />
            </Button>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent side="bottom">
          Избранное (Ctrl+Shift+F)
        </TooltipContent>
      </Tooltip>

      <PopoverContent
        className="w-80 p-0"
        align="end"
        sideOffset={8}
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b px-3 py-2">
          <div className="flex items-center gap-1.5">
            <Star className="h-3.5 w-3.5" />
            <span className="text-sm font-medium">Избранное</span>
          </div>
          {hasFavorites && (
            <span className="text-xs text-muted-foreground tabular-nums">
              {items.length}
            </span>
          )}
        </div>

        {/* Search (shown for 8+ items) */}
        {showSearch && (
          <div className="border-b px-3 py-2">
            <div className="relative">
              <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                placeholder="Поиск..."
                className="h-7 pl-7 text-xs"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                autoFocus
              />
            </div>
          </div>
        )}

        {/* Items list */}
        <ScrollArea className={cn(items.length > 10 ? "h-80" : "")}>
          <div className="py-1">
            {!isLoaded ? (
              <div className="px-3 py-8 text-center text-xs text-muted-foreground">
                Загрузка…
              </div>
            ) : filteredItems.length === 0 ? (
              <EmptyState hasSearch={!!search.trim()} itemsExist={items.length > 0} />
            ) : (
              filteredItems.map((item) => (
                <FavoriteRow
                  key={`${item.entityType}::${item.entityId}`}
                  item={item}
                  onNavigate={handleNavigate}
                  onRemove={handleRemove}
                />
              ))
            )}
          </div>
        </ScrollArea>
      </PopoverContent>
    </Popover>
  )
}

// ── FavoriteRow ─────────────────────────────────────────────────────────

interface FavoriteRowProps {
  item: FavoriteItem
  onNavigate: (item: FavoriteItem) => void
  onRemove: (e: React.MouseEvent, item: FavoriteItem) => void
}

function FavoriteRow({ item, onNavigate, onRemove }: FavoriteRowProps) {
  const icon = getEntityIcon(item.entityType)

  return (
    <div
      className="group flex items-center gap-2 px-2 py-1 mx-1 rounded-md cursor-pointer hover:bg-accent transition-colors"
      onClick={() => onNavigate(item)}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault()
          onNavigate(item)
        }
      }}
    >
      {React.createElement(icon, { className: "h-3.5 w-3.5 shrink-0 text-muted-foreground" })}
      <span className="flex-1 truncate text-sm" title={item.title}>
        {item.title}
      </span>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            className="h-5 w-5 shrink-0 flex items-center justify-center rounded-sm opacity-0 group-hover:opacity-100 hover:bg-muted transition-opacity"
            onClick={(e) => e.stopPropagation()}
            aria-label="Действия"
          >
            <MoreHorizontal className="h-3.5 w-3.5 text-muted-foreground" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48">
          <DropdownMenuItem
            className="gap-2 text-destructive focus:text-destructive"
            onClick={(e) => onRemove(e, item)}
          >
            <Trash2 className="h-4 w-4" />
            Убрать из избранного
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

// ── EmptyState ──────────────────────────────────────────────────────────

function EmptyState({
  hasSearch,
  itemsExist,
}: {
  hasSearch: boolean
  itemsExist: boolean
}) {
  return (
    <div className="flex flex-col items-center gap-2 px-4 py-8 text-center">
      <div className="flex h-10 w-10 items-center justify-center rounded-full bg-muted">
        <Star className="h-5 w-5 text-muted-foreground" />
      </div>
      <div className="space-y-0.5">
        {hasSearch ? (
          <p className="text-xs text-muted-foreground">Ничего не найдено</p>
        ) : itemsExist ? (
          <p className="text-xs text-muted-foreground">Список пуст</p>
        ) : (
          <>
            <p className="text-xs font-medium text-muted-foreground">
              Нет избранных
            </p>
            <p className="text-[11px] text-muted-foreground/70">
              Нажмите ⭐ на любой странице
            </p>
          </>
        )}
      </div>
    </div>
  )
}
