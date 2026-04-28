// frontend/components/layout/recent-popover.tsx
"use client"

import * as React from "react"
import { useRouter } from "next/navigation"
import { Clock, MoreHorizontal, Trash2, Search, XCircle } from "lucide-react"
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
import { useRecentStore, type RecentItem } from "@/stores/useRecentStore"
import { useTabsStore } from "@/stores/useTabsStore"
import { useShortcut } from "@/hooks/useShortcut"
import { getEntityIcon } from "@/lib/entity-icon"

const _searchThreshold = 8

/**
 * RecentPopover — header trigger (🕐) + popover dropdown with recent documents list.
 *
 * Positioned between FavoritesPopover (⭐) and NotificationBell (🔔) in the header.
 * Each item shows an entity-type icon + truncated title + relative time.
 */
export function RecentPopover() {
  const router = useRouter()
  const openTab = useTabsStore((s) => s.openTab)

  const items = useRecentStore((s) => s.items)
  const removeRecent = useRecentStore((s) => s.removeRecent)
  const clearAll = useRecentStore((s) => s.clearAll)

  const [open, setOpen] = React.useState(false)
  const [search, setSearch] = React.useState("")

  const hasItems = items.length > 0
  const showSearch = items.length >= _searchThreshold

  // Filter items by search query
  const filteredItems = React.useMemo(() => {
    if (!search.trim()) return items
    const q = search.toLowerCase().trim()
    return items.filter((item) => item.title.toLowerCase().includes(q))
  }, [items, search])

  // Navigate to recent item
  const handleNavigate = React.useCallback(
    (item: RecentItem) => {
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

  // Remove from recent
  const handleRemove = React.useCallback(
    (e: React.MouseEvent, item: RecentItem) => {
      e.stopPropagation()
      removeRecent(item.url)
    },
    [removeRecent],
  )

  // Clear all recent items
  const handleClearAll = React.useCallback(() => {
    clearAll()
    toast.success("История очищена")
  }, [clearAll])

  // Reset search when popover closes
  React.useEffect(() => {
    if (!open) setSearch("")
  }, [open])

  // Keyboard shortcut: Ctrl+Shift+R
  useShortcut(
    "nav.recent",
    "mod+shift+r",
    "Недавние",
    "navigation",
    () => setOpen((prev) => !prev),
  )

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <Button
              id="recent-trigger"
              variant="ghost"
              size="icon"
              className={cn(
                "relative h-8 w-8",
                open && "bg-accent text-accent-foreground",
              )}
              aria-label={`Недавние${hasItems ? ` (${items.length})` : ""}`}
            >
              <Clock className="h-4 w-4" />
            </Button>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent side="bottom">
          Недавние (Ctrl+Shift+R)
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
            <Clock className="h-3.5 w-3.5" />
            <span className="text-sm font-medium">Недавние</span>
          </div>
          <div className="flex items-center gap-1">
            {hasItems && (
              <span className="text-xs text-muted-foreground tabular-nums">
                {items.length}
              </span>
            )}
            {hasItems && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    className="h-5 w-5 flex items-center justify-center rounded-sm hover:bg-muted transition-colors"
                    onClick={handleClearAll}
                    aria-label="Очистить историю"
                  >
                    <XCircle className="h-3.5 w-3.5 text-muted-foreground" />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom">Очистить все</TooltipContent>
              </Tooltip>
            )}
          </div>
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
            {filteredItems.length === 0 ? (
              <EmptyState hasSearch={!!search.trim()} itemsExist={hasItems} />
            ) : (
              filteredItems.map((item) => (
                <RecentRow
                  key={item.url}
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

// ── RecentRow ───────────────────────────────────────────────────────────

interface RecentRowProps {
  item: RecentItem
  onNavigate: (item: RecentItem) => void
  onRemove: (e: React.MouseEvent, item: RecentItem) => void
}

function RecentRow({ item, onNavigate, onRemove }: RecentRowProps) {
  const icon = getEntityIcon(item.entityType ?? "")
  const timeAgo = useRelativeTime(item.visitedAt)

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
      <div className="flex-1 min-w-0">
        <span className="block truncate text-sm" title={item.title}>
          {item.title}
        </span>
        <span className="block text-[11px] text-muted-foreground/70">
          {timeAgo}
        </span>
      </div>
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
            Убрать из недавних
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

// ── Relative time formatting ────────────────────────────────────────────

function useRelativeTime(iso: string): string {
  // Re-render every minute for live updates
  const [, setTick] = React.useState(0)
  React.useEffect(() => {
    const interval = setInterval(() => setTick((n) => n + 1), 60_000)
    return () => clearInterval(interval)
  }, [])

  return formatRelativeTime(iso)
}

function formatRelativeTime(iso: string): string {
  const now = Date.now()
  const then = new Date(iso).getTime()
  const diffMs = now - then
  const diffMin = Math.floor(diffMs / 60_000)

  if (diffMin < 1) return "только что"
  if (diffMin < 60) return `${diffMin} мин. назад`

  const diffHours = Math.floor(diffMin / 60)
  if (diffHours < 24) return `${diffHours} ч. назад`

  const diffDays = Math.floor(diffHours / 24)
  if (diffDays === 1) return "вчера"
  if (diffDays < 7) return `${diffDays} дн. назад`

  // Older than a week — show date
  return new Date(iso).toLocaleDateString("ru-RU", {
    day: "numeric",
    month: "short",
  })
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
        <Clock className="h-5 w-5 text-muted-foreground" />
      </div>
      <div className="space-y-0.5">
        {hasSearch ? (
          <p className="text-xs text-muted-foreground">Ничего не найдено</p>
        ) : itemsExist ? (
          <p className="text-xs text-muted-foreground">Список пуст</p>
        ) : (
          <>
            <p className="text-xs font-medium text-muted-foreground">
              Нет недавних документов
            </p>
            <p className="text-[11px] text-muted-foreground/70">
              Закрытые вкладки появятся здесь
            </p>
          </>
        )}
      </div>
    </div>
  )
}
