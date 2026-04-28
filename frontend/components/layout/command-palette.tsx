// frontend/components/layout/command-palette.tsx
"use client"

/**
 * Command Palette (Ctrl+K) — global search and quick-action dialog.
 *
 * UX modeled after Linear/Raycast/VS Code Command Palette:
 * - **Zero-state** (empty query): contextual actions → favorites → recent items
 * - **Search-state** (typing): fuzzy-matched navigation, create shortcuts, reports
 *
 * Built on shadcn/ui <Command> (cmdk) + <Dialog>.
 * Mounted once in AppShell — available from any screen.
 */

import * as React from "react"
import { useRouter } from "next/navigation"
import { toast } from "sonner"
import {
  StarOff,
  Clock,
  Zap,
} from "lucide-react"

import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut,
} from "@/components/ui/command"
import { useCommandPaletteStore } from "@/stores/useCommandPaletteStore"
import { useFavoritesStore } from "@/stores/useFavoritesStore"
import { useRecentStore } from "@/stores/useRecentStore"
import { useTabsStore } from "@/stores/useTabsStore"
import { useShortcut } from "@/hooks/useShortcut"
import { getEntityIcon } from "@/lib/entity-icon"
import {
  getStaticNavItems,
  getDynamicNavItems,
  COMMAND_SECTION_LABELS,
  type CommandNavItem,
} from "@/lib/command-nav-items"

// ── Constants ───────────────────────────────────────────────────────────

const _maxRecentInPalette = 7
const _maxFavoritesInPalette = 7

// ── Component ───────────────────────────────────────────────────────────

export function CommandPalette() {
  const router = useRouter()
  const isOpen = useCommandPaletteStore((s) => s.isOpen)
  const setOpen = useCommandPaletteStore((s) => s.setOpen)

  const openTab = useTabsStore((s) => s.openTab)

  // We read version to trigger re-render when actions change
  useCommandPaletteStore((s) => s.version)

  const contextActions = useCommandPaletteStore.getState().getAllActions()
  const favoriteItems = useFavoritesStore((s) => s.items)
  const recentItems = useRecentStore((s) => s.items)

  // Build nav items (memoized since metadata rarely changes)
  const navItems = React.useMemo<CommandNavItem[]>(() => {
    return [...getStaticNavItems(), ...getDynamicNavItems()]
  }, [isOpen]) // eslint-disable-line react-hooks/exhaustive-deps
  // Recalculate when palette opens (metadata might have loaded since last open)

  // ── Navigation handler ──────────────────────────────────────────────

  const navigateTo = React.useCallback(
    (url: string, title: string) => {
      const result = openTab({ id: url.split("?")[0], title, url })
      if (result.warning) toast.warning(result.warning)
      router.push(url)
      setOpen(false)
    },
    [openTab, router, setOpen],
  )

  // ── Keyboard shortcut: Ctrl+K ───────────────────────────────────────

  const handleToggle = React.useCallback(() => {
    useCommandPaletteStore.getState().toggle()
  }, [])

  useShortcut(
    "general.command-palette",
    "mod+k",
    "Командная строка",
    "general",
    handleToggle,
  )

  // ── Determine what to show ────────────────────────────────────────────

  const hasContextActions = contextActions.length > 0
  const hasFavorites = favoriteItems.length > 0
  const hasRecent = recentItems.length > 0

  // Group nav items by section for search results
  const navBySection = React.useMemo(() => {
    const grouped = new Map<CommandNavItem["section"], CommandNavItem[]>()
    for (const item of navItems) {
      const existing = grouped.get(item.section) ?? []
      existing.push(item)
      grouped.set(item.section, existing)
    }
    return grouped
  }, [navItems])

  return (
    <CommandDialog open={isOpen} onOpenChange={setOpen}>
      <CommandInput placeholder="Поиск, навигация, команды..." />
      <CommandList className="max-h-[400px]">
        <CommandEmpty>
          <div className="flex flex-col items-center gap-2 py-4">
            <span className="text-sm text-muted-foreground">Ничего не найдено</span>
            <span className="text-xs text-muted-foreground/70">
              Попробуйте другой запрос
            </span>
          </div>
        </CommandEmpty>

        {/* ── Zero-state: Contextual Actions ─────────────────────── */}
        {hasContextActions && (
          <CommandGroup heading="Действия">
            {contextActions.map((action) => (
              <CommandItem
                key={action.id}
                value={`action:${action.id} ${action.label}`}
                onSelect={() => {
                  action.action()
                  setOpen(false)
                }}
              >
                {action.icon ? (
                  <action.icon className="mr-2 h-4 w-4 text-muted-foreground" />
                ) : (
                  <Zap className="mr-2 h-4 w-4 text-muted-foreground" />
                )}
                <span>{action.label}</span>
                {action.shortcut && (
                  <CommandShortcut>{action.shortcut.join("+")}</CommandShortcut>
                )}
              </CommandItem>
            ))}
          </CommandGroup>
        )}

        {/* ── Zero-state: Favorites ──────────────────────────────── */}
        {hasFavorites && (
          <>
            {hasContextActions && <CommandSeparator />}
            <CommandGroup heading="Избранное">
              {favoriteItems.slice(0, _maxFavoritesInPalette).map((fav) => {
                const IconComponent = getEntityIcon(fav.entityType)
                return (
                  <CommandItem
                    key={`fav:${fav.entityType}:${fav.entityId}`}
                    value={`fav:${fav.title} ${fav.entityType}`}
                    onSelect={() => navigateTo(fav.url, fav.title)}
                    className="group/fav"
                  >
                    <IconComponent className="mr-2 h-3.5 w-3.5 text-muted-foreground" />
                    <span className="truncate">{fav.title}</span>
                    <button
                      className="ml-auto flex h-5 w-5 shrink-0 items-center justify-center rounded-sm opacity-0 transition-opacity group-data-[selected=true]/fav:opacity-100 hover:bg-muted"
                      aria-label="Убрать из избранного"
                      onMouseDown={(e) => {
                        // Prevent cmdk from stealing focus / triggering onSelect
                        e.preventDefault()
                        e.stopPropagation()
                        useFavoritesStore.getState().removeFavorite(fav.entityType, fav.entityId)
                        toast.success("Убрано из избранного")
                      }}
                    >
                      <StarOff className="h-3 w-3 text-muted-foreground" />
                    </button>
                  </CommandItem>
                )
              })}
            </CommandGroup>
          </>
        )}

        {/* ── Zero-state: Recent Items ───────────────────────────── */}
        {hasRecent && (
          <>
            {(hasContextActions || hasFavorites) && <CommandSeparator />}
            <CommandGroup heading="Недавние">
              {recentItems.slice(0, _maxRecentInPalette).map((recent) => {
                const IconComponent = getEntityIcon(recent.entityType ?? "")
                return (
                  <CommandItem
                    key={`recent:${recent.url}`}
                    value={`recent:${recent.title} ${recent.entityType ?? ""}`}
                    onSelect={() => navigateTo(recent.url, recent.title)}
                  >
                    <Clock className="mr-2 h-3.5 w-3.5 text-muted-foreground" />
                    <IconComponent className="mr-1.5 h-3.5 w-3.5 text-muted-foreground" />
                    <span className="truncate">{recent.title}</span>
                    <span className="ml-auto text-[10px] text-muted-foreground/60">
                      {formatCompactTime(recent.visitedAt)}
                    </span>
                  </CommandItem>
                )
              })}
            </CommandGroup>
          </>
        )}

        {/* ── Search-state: Navigation items by section ───────────── */}
        <CommandSeparator />
        {(["navigate", "create", "report", "system"] as const).map((section) => {
          const items = navBySection.get(section)
          if (!items || items.length === 0) return null
          return (
            <CommandGroup key={section} heading={COMMAND_SECTION_LABELS[section]}>
              {items.map((item) => (
                <CommandItem
                  key={item.id}
                  value={`${item.id} ${item.label} ${item.keywords}`}
                  onSelect={() => navigateTo(item.url, item.label)}
                  keywords={[item.keywords]}
                >
                  <item.icon className="mr-2 h-4 w-4 text-muted-foreground" />
                  <span>{item.label}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          )
        })}
      </CommandList>
    </CommandDialog>
  )
}

// ── Helpers ─────────────────────────────────────────────────────────────

function formatCompactTime(iso: string): string {
  const now = Date.now()
  const then = new Date(iso).getTime()
  const diffMin = Math.floor((now - then) / 60_000)

  if (diffMin < 1) return "сейчас"
  if (diffMin < 60) return `${diffMin}м`

  const diffHours = Math.floor(diffMin / 60)
  if (diffHours < 24) return `${diffHours}ч`

  const diffDays = Math.floor(diffHours / 24)
  if (diffDays === 1) return "вчера"
  if (diffDays < 7) return `${diffDays}д`

  return new Date(iso).toLocaleDateString("ru-RU", {
    day: "numeric",
    month: "short",
  })
}
