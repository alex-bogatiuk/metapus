// frontend/components/layout/command-palette.tsx
"use client"

/**
 * Command Palette (Ctrl+K) — global search and quick-action dialog.
 *
 * UX modeled after Linear/Raycast/VS Code Command Palette:
 * - **Zero-state** (empty query): contextual actions → favorites → recent items
 * - **Search-state** (typing): fuzzy-matched navigation, create shortcuts, reports
 * - **Calc-state** (math expression): inline calculator with copy-to-clipboard
 * - **Data-search** (">prefix"): global data search across all entities
 * - **Preview** (ArrowRight on search result): side panel with entity details
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
  Calculator,
  Copy,
  Equal,
  Loader2,
  Eye,
  ChevronRight,
} from "lucide-react"

import type { SearchResultItem } from "@/types/search"

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
import { EntityPreviewCard } from "@/components/shared/entity-preview-card"
import { useCommandPaletteStore } from "@/stores/useCommandPaletteStore"
import { useFavoritesStore } from "@/stores/useFavoritesStore"
import { useRecentStore } from "@/stores/useRecentStore"
import { useTabsStore } from "@/stores/useTabsStore"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { api } from "@/lib/api"
import { useShortcut } from "@/hooks/useShortcut"
import { getEntityIcon } from "@/lib/entity-icon"
import {
  getStaticNavItems,
  getDynamicNavItems,
  COMMAND_SECTION_LABELS,
  type CommandNavItem,
} from "@/lib/command-nav-items"
import {
  isMathExpression,
  evaluateExpression,
  type CalcResult,
} from "@/lib/calc-engine"
import { parseEntityTypeFromUrl } from "@/lib/entity-url"
import { cn } from "@/lib/utils"

// ── Constants ───────────────────────────────────────────────────────────

const _maxRecentInPalette = 7
const _maxFavoritesInPalette = 7
const _searchDebounceMs = 300
const _minSearchLength = 2

/** Minimum viewport width for preview panel (desktop-only). */
const _minPreviewWidth = 1024

/** Prefix character that activates global data search mode. */
const _dataSearchPrefix = ">"

/** UUID v4/v7 pattern — guards preview to entity detail pages only. */
const _uuidRe = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

// ── Preview target type ─────────────────────────────────────────────────

interface PreviewTarget {
  entityType: string
  entityKey: string
  entityId: string
}

// ── Collapsible Command Group ─────────────────────────────────────────

/**
 * A cmdk-compatible collapsible group. When collapsed, children
 * (CommandItems) are not rendered → cmdk skips them in keyboard nav.
 * Expanded by default.
 */
function CollapsibleCommandGroup({
  heading,
  children,
  defaultOpen = true,
}: {
  heading: string
  children: React.ReactNode
  defaultOpen?: boolean
}) {
  const [open, setOpen] = React.useState(defaultOpen)

  return (
    <div className="overflow-hidden p-1 text-foreground" cmdk-group="">
      <button
        type="button"
        onClick={() => setOpen((prev) => !prev)}
        className="flex w-full items-center gap-1 px-2 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors select-none"
        // Prevent cmdk from treating this as an item selection
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault()
            e.stopPropagation()
            setOpen((prev) => !prev)
          }
        }}
        tabIndex={-1}
      >
        <ChevronRight
          className={cn(
            "h-3 w-3 shrink-0 transition-transform duration-150",
            open && "rotate-90",
          )}
        />
        {heading}
      </button>
      {open && (
        <div cmdk-group-items="">
          {children}
        </div>
      )}
    </div>
  )
}

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

  // ── Search input state (controlled) ─────────────────────────────────

  const [search, setSearch] = React.useState("")

  // ── Preview panel state ─────────────────────────────────────────────

  const [previewTarget, setPreviewTarget] = React.useState<PreviewTarget | null>(null)

  // Desktop gate: only show preview on wide screens
  const [isDesktop, setIsDesktop] = React.useState(false)

  React.useEffect(() => {
    const check = () => setIsDesktop(window.innerWidth >= _minPreviewWidth)
    check()
    window.addEventListener("resize", check)
    return () => window.removeEventListener("resize", check)
  }, [])

  // Reset search and preview when palette opens/closes
  React.useEffect(() => {
    if (!isOpen) {
      setSearch("")
      setPreviewTarget(null)
    }
  }, [isOpen])

  // Close preview when search changes
  React.useEffect(() => {
    setPreviewTarget(null)
  }, [search])

  // ── Global data search (debounced, activated by ">" prefix) ─────────

  const [searchResults, setSearchResults] = React.useState<SearchResultItem[]>([])
  const [isSearching, setIsSearching] = React.useState(false)
  const searchAbortRef = React.useRef<AbortController | null>(null)

  // Data search mode: user typed ">" prefix (e.g. ">ООО" or ">GR-SEED")
  const isDataSearchMode = search.startsWith(_dataSearchPrefix)
  const dataSearchQuery = isDataSearchMode ? search.slice(_dataSearchPrefix.length).trim() : ""

  React.useEffect(() => {
    // Only trigger global data search when ">" prefix is present
    if (!isDataSearchMode || dataSearchQuery.length < _minSearchLength) {
      setSearchResults([])
      setIsSearching(false)
      return
    }

    setIsSearching(true)

    const timer = setTimeout(() => {
      // Cancel previous in-flight request
      searchAbortRef.current?.abort()
      const controller = new AbortController()
      searchAbortRef.current = controller

      api.search
        .query(dataSearchQuery)
        .then((res) => {
          if (!controller.signal.aborted) {
            setSearchResults(res.results ?? [])
            setIsSearching(false)
          }
        })
        .catch(() => {
          if (!controller.signal.aborted) {
            setSearchResults([])
            setIsSearching(false)
          }
        })
    }, _searchDebounceMs)

    return () => {
      clearTimeout(timer)
      searchAbortRef.current?.abort()
    }
  }, [isDataSearchMode, dataSearchQuery])

  // Group search results by entityName for display
  const groupedResults = React.useMemo(() => {
    const groups = new Map<string, SearchResultItem[]>()
    for (const item of searchResults) {
      const existing = groups.get(item.entityName) ?? []
      existing.push(item)
      groups.set(item.entityName, existing)
    }
    return groups
  }, [searchResults])

  // ── Calculator ──────────────────────────────────────────────────────

  const calcResult = React.useMemo<CalcResult | null>(() => {
    if (!isMathExpression(search)) return null
    return evaluateExpression(search)
  }, [search])

  const handleCopyResult = React.useCallback(() => {
    if (!calcResult) return
    // Copy raw numeric value (not formatted) for paste into other fields
    navigator.clipboard.writeText(String(calcResult.value))
    toast.success("Скопировано в буфер обмена", {
      description: calcResult.formatted,
    })
    setOpen(false)
  }, [calcResult, setOpen])

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

  // ── Preview activation via ArrowRight ─────────────────────────────────
  // cmdk manages highlighted state internally via data-selected attribute.
  // We read it from the DOM instead of tracking React state, because cmdk
  // keyboard navigation does NOT fire onFocus/onMouseEnter on items.

  const searchResultsRef = React.useRef<SearchResultItem[]>(searchResults)
  searchResultsRef.current = searchResults

  /**
   * Resolve PreviewTarget from the currently selected cmdk item.
   * Handles three value prefixes:
   *   - "search:<id> ..."  → lookup in searchResults
   *   - "fav:<title> ..."  → lookup in favoriteItems by entityId
   *   - "recent:<title>..." → parse entityKey from URL, extract UUID
   */
  const resolvePreviewFromCmdk = React.useCallback(
    (dataValue: string): PreviewTarget | null => {
      const getMetadata = useMetadataStore.getState().getEntity

      // 1) Global data search results
      if (dataValue.startsWith("search:")) {
        const match = searchResultsRef.current.find((item) =>
          dataValue.includes(item.entityId),
        )
        if (!match) return null
        return {
          entityType: match.entityType,
          entityKey: match.entityKey,
          entityId: match.entityId,
        }
      }

      // 2) Favorites — FavoriteItem has entityType (= entity key) + entityId
      if (dataValue.startsWith("fav:")) {
        const fav = favoriteItems.find((f) =>
          dataValue.includes(f.entityId),
        )
        if (!fav) return null
        const meta = getMetadata(fav.entityType)
        if (!meta) return null
        return {
          entityType: meta.type,
          entityKey: fav.entityType,
          entityId: fav.entityId,
        }
      }

      // 3) Recent — RecentItem has url + entityType (entity key, optional)
      if (dataValue.startsWith("recent:")) {
        const recent = recentItems.find((r) => {
          const segments = r.url.split("/")
          const uuid = segments[segments.length - 1]
          // Only match detail pages (valid UUID), skip list pages
          return uuid && _uuidRe.test(uuid) && dataValue.includes(uuid)
        })
        if (!recent) return null
        const entityKey =
          recent.entityType ?? parseEntityTypeFromUrl(recent.url)
        if (!entityKey) return null
        const meta = getMetadata(entityKey)
        if (!meta) return null
        const segments = recent.url.split("/")
        const entityId = segments[segments.length - 1]
        if (!entityId || !_uuidRe.test(entityId)) return null
        return {
          entityType: meta.type,
          entityKey,
          entityId,
        }
      }

      return null
    },
    [favoriteItems, recentItems],
  )

  // Ref avoids adding previewTarget to handleKeyDown deps (keeps callback stable).
  const previewOpenRef = React.useRef(false)
  previewOpenRef.current = previewTarget !== null

  const handleKeyDown = React.useCallback(
    (e: React.KeyboardEvent) => {
      // Open preview for the current item
      if (e.key === "ArrowRight" && isDesktop) {
        const selectedEl = document.querySelector<HTMLElement>(
          '[cmdk-item][data-selected="true"]',
        )
        if (!selectedEl) return

        const value = selectedEl.getAttribute("data-value") ?? ""
        const target = resolvePreviewFromCmdk(value)
        if (!target) return

        e.preventDefault()
        setPreviewTarget(target)
      }

      // Auto-update preview on ↑/↓ when preview panel is already open.
      // cmdk processes arrow keys in the same tick but DOM updates after,
      // so we use rAF to read the new data-selected element.
      if (
        (e.key === "ArrowUp" || e.key === "ArrowDown") &&
        previewOpenRef.current
      ) {
        requestAnimationFrame(() => {
          const selectedEl = document.querySelector<HTMLElement>(
            '[cmdk-item][data-selected="true"]',
          )
          if (!selectedEl) return
          const value = selectedEl.getAttribute("data-value") ?? ""
          const target = resolvePreviewFromCmdk(value)
          if (target) setPreviewTarget(target)
        })
      }
    },
    [isDesktop, resolvePreviewFromCmdk],
  )

  // ── Escape hierarchy: close preview first, then dialog ────────────────
  // Uses Radix onEscapeKeyDown — fires before Dialog closes, so we can
  // call preventDefault() to keep the dialog open.

  const handleEscapeKeyDown = React.useCallback(
    (e: KeyboardEvent) => {
      if (previewTarget) {
        e.preventDefault()
        setPreviewTarget(null)
      }
    },
    [previewTarget],
  )

  // ── Determine what to show ────────────────────────────────────────────

  const hasContextActions = contextActions.length > 0
  const hasFavorites = favoriteItems.length > 0
  const hasRecent = recentItems.length > 0
  const showPreview = previewTarget !== null && isDesktop

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
    <CommandDialog
      open={isOpen}
      onOpenChange={setOpen}
      onEscapeKeyDown={handleEscapeKeyDown}
      dialogClassName={cn(
        "transition-all duration-200",
        showPreview && "sm:max-w-[820px]",
      )}
    >
      <div className={cn("flex", showPreview && "divide-x")}>
        {/* ── Left column: Command list ──────────────────────── */}
        <div className={cn("flex-1 min-w-0", showPreview && "max-w-[500px]")} onKeyDown={handleKeyDown}>
          <CommandInput
            placeholder="Навигация, = калькулятор, > поиск по данным..."
            value={search}
            onValueChange={setSearch}
          />
          <CommandList className="max-h-[400px]">
            {!calcResult && !isSearching && searchResults.length === 0 && (
              <CommandEmpty>
                <div className="flex flex-col items-center gap-2 py-4">
                  <span className="text-sm text-muted-foreground">Ничего не найдено</span>
                  <span className="text-xs text-muted-foreground/70">
                    Попробуйте другой запрос
                  </span>
                </div>
              </CommandEmpty>
            )}

            {/* ── Calculator result ─────────────────────────────────────── */}
            {calcResult && (
              <CommandGroup heading="Калькулятор" forceMount>
                <CommandItem
                  forceMount
                  value={`calc:${search}`}
                  onSelect={handleCopyResult}
                  className="flex items-center gap-3"
                >
                  <Calculator className="h-4 w-4 text-muted-foreground" />
                  <span className="text-sm text-muted-foreground">{search}</span>
                  <Equal className="h-3 w-3 text-muted-foreground/60" />
                  <span className="text-lg font-semibold tabular-nums">
                    {calcResult.formatted}
                  </span>
                  <span className="ml-auto flex items-center gap-1 text-[10px] text-muted-foreground/60">
                    <Copy className="h-3 w-3" />
                    Enter
                  </span>
                </CommandItem>
              </CommandGroup>
            )}

            {/* ── Zero-state: Contextual Actions (hidden in data search mode) */}
            {hasContextActions && !isDataSearchMode && (
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

            {/* ── Zero-state: Favorites (hidden in data search mode) ── */}
            {hasFavorites && !isDataSearchMode && (
              <>
                {hasContextActions && <CommandSeparator />}
                <CollapsibleCommandGroup heading="Избранное">
                  {favoriteItems.slice(0, _maxFavoritesInPalette).map((fav) => {
                    const IconComponent = getEntityIcon(fav.entityType)
                    return (
                      <CommandItem
                        key={`fav:${fav.entityType}:${fav.entityId}`}
                        value={`fav:${fav.entityId} ${fav.title} ${fav.entityType}`}
                        onSelect={() => navigateTo(fav.url, fav.title)}
                        className="group/fav"
                      >
                        <IconComponent className="mr-2 h-3.5 w-3.5 text-muted-foreground" />
                        <span className="truncate">{fav.title}</span>
                        {/* Preview hint */}
                        {isDesktop && (
                          <span className="ml-auto flex items-center gap-1 text-[10px] shrink-0 transition-opacity opacity-0 group-data-[selected=true]/fav:opacity-60 text-muted-foreground">
                            <Eye className="h-3 w-3" />
                            →
                          </span>
                        )}
                        <button
                          className="flex h-5 w-5 shrink-0 items-center justify-center rounded-sm opacity-0 transition-opacity group-data-[selected=true]/fav:opacity-100 hover:bg-muted"
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
                </CollapsibleCommandGroup>
              </>
            )}

            {/* ── Zero-state: Recent Items (hidden in data search mode) */}
            {hasRecent && !isDataSearchMode && (
              <>
                {(hasContextActions || hasFavorites) && <CommandSeparator />}
                <CollapsibleCommandGroup heading="Недавние">
                  {recentItems.slice(0, _maxRecentInPalette).map((recent) => {
                    const IconComponent = getEntityIcon(recent.entityType ?? "")
                    // Extract UUID from URL for cmdk value (needed for preview resolution)
                    const urlSegments = recent.url.split("/")
                    const entityUuid = urlSegments[urlSegments.length - 1]
                    return (
                      <CommandItem
                        key={`recent:${recent.url}`}
                        value={`recent:${entityUuid} ${recent.title} ${recent.entityType ?? ""}`}
                        onSelect={() => navigateTo(recent.url, recent.title)}
                        className="group/recent"
                      >
                        <Clock className="mr-2 h-3.5 w-3.5 text-muted-foreground" />
                        <IconComponent className="mr-1.5 h-3.5 w-3.5 text-muted-foreground" />
                        <span className="truncate">{recent.title}</span>
                        {/* Preview hint — only for entity detail pages (valid UUID) */}
                        {isDesktop && _uuidRe.test(entityUuid) && (
                          <span className="ml-auto flex items-center gap-1 text-[10px] shrink-0 transition-opacity opacity-0 group-data-[selected=true]/recent:opacity-60 text-muted-foreground">
                            <Eye className="h-3 w-3" />
                            →
                          </span>
                        )}
                        <span className={cn(
                          "text-[10px] text-muted-foreground/60",
                          isDesktop ? "" : "ml-auto",
                        )}>
                          {formatCompactTime(recent.visitedAt)}
                        </span>
                      </CommandItem>
                    )
                  })}
                </CollapsibleCommandGroup>
              </>
            )}

            {/* ── Global Data Search results ─────────────────────────── */}
            {searchResults.length > 0 && (
              <>
                <CommandSeparator />
                {Array.from(groupedResults.entries()).map(([entityName, items]) => {
                  const entityMeta = useMetadataStore.getState().getEntityByName(entityName)
                  const heading = entityMeta?.presentation.plural ?? entityName
                  return (
                    <CommandGroup key={`search:${entityName}`} heading={heading} forceMount>
                      {items.map((item) => {
                        const IconComponent = getEntityIcon(item.entityKey)
                        return (
                          <CommandItem
                            key={`search:${item.entityId}`}
                            forceMount
                            value={`search:${item.entityId} ${item.title} ${item.subtitle}`}
                            onSelect={() => navigateTo(item.url, item.title)}
                            className="group/search"
                          >
                            <IconComponent className="mr-2 h-3.5 w-3.5 text-muted-foreground" />
                            <span className="truncate">{item.title}</span>
                            {item.subtitle && (
                              <span className="ml-1 text-xs text-muted-foreground">
                                {item.subtitle}
                              </span>
                            )}
                            {/* Preview hint — visible on desktop when item is selected by cmdk */}
                            {isDesktop && (
                              <span className="ml-auto flex items-center gap-1 text-[10px] shrink-0 transition-opacity opacity-0 group-data-[selected=true]/search:opacity-60 text-muted-foreground">
                                <Eye className="h-3 w-3" />
                                →
                              </span>
                            )}
                          </CommandItem>
                        )
                      })}
                    </CommandGroup>
                  )
                })}
              </>
            )}

            {/* ── Search loading indicator ────────────────────────────── */}
            {isSearching && isDataSearchMode && dataSearchQuery.length >= _minSearchLength && (
              <div className="flex items-center justify-center gap-2 py-4 text-sm text-muted-foreground" cmdk-loading="">
                <Loader2 className="h-4 w-4 animate-spin" />
                <span>Поиск…</span>
              </div>
            )}

            {/* ── Search-state: Navigation items by section (hidden in data search mode) */}
            {!isDataSearchMode && <CommandSeparator />}
            {!isDataSearchMode && (["navigate", "create", "report", "system"] as const).map((section) => {
              const items = navBySection.get(section)
              if (!items || items.length === 0) return null
              return (
                <CollapsibleCommandGroup key={section} heading={COMMAND_SECTION_LABELS[section]}>
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
                </CollapsibleCommandGroup>
              )
            })}
          </CommandList>
        </div>

        {/* ── Right column: Preview panel ────────────────────── */}
        {showPreview && previewTarget && (
          <div className="w-[320px] shrink-0 border-l bg-muted/20">
            <EntityPreviewCard
              entityType={previewTarget.entityType}
              entityKey={previewTarget.entityKey}
              entityId={previewTarget.entityId}
              onNavigate={navigateTo}
            />
          </div>
        )}
      </div>
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
