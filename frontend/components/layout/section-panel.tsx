// frontend/components/layout/section-panel.tsx
"use client"

import { useEffect, useRef, useCallback, useState, type ReactNode } from "react"
import type { LucideIcon } from "lucide-react"
import { ChevronLeft, X } from "lucide-react"
import { ScrollArea } from "@/components/ui/scroll-area"
import { SidebarContent, useSidebar } from "@/components/ui/sidebar"
import { cn } from "@/lib/utils"

// ── Types ────────────────────────────────────────────────────────────────

export type { NavSection, NavSectionGroup, NavSectionItem } from "@/lib/navigation"

/** Resolved version with titles/urls computed from metadata */
export interface ResolvedSectionItem {
  title: string
  url: string
  description?: string
  icon?: LucideIcon
}

export interface ResolvedSectionGroup {
  label: string
  items: ResolvedSectionItem[]
}

export interface ResolvedNavSection {
  title: string
  icon: LucideIcon
  groups: ResolvedSectionGroup[]
}

// ── Shared list ──────────────────────────────────────────────────────────

type ItemClick = (e: React.MouseEvent, item: { title: string; url: string }) => void

/**
 * Groups of a section as a one-column menu: icon, title, optional description.
 * Used by the desktop companion panel and by the drill-down inside the mobile drawer,
 * so a section reads the same everywhere. `touch` gives rows a 44px target.
 */
function SectionGroups({ groups, currentPath, onItemClick, touch = false }: {
  groups: ResolvedSectionGroup[]
  currentPath: string
  onItemClick: ItemClick
  touch?: boolean
}) {
  const visible = groups.filter((g) => g.items.length > 0)
  if (visible.length === 0) {
    return <p className="py-8 text-center text-sm text-muted-foreground">В этом разделе пока нет доступных пунктов</p>
  }
  return (
    <div className="flex flex-col gap-4">
      {visible.map((group) => (
        <div key={group.label}>
          <h3 className="px-2 pb-1 text-xs font-medium text-muted-foreground">{group.label}</h3>
          <ul className="flex flex-col gap-px">
            {group.items.map((item) => {
              const isActive = currentPath.startsWith(item.url)
              return (
                <li key={item.url}>
                  <a
                    href={item.url}
                    onClick={(e) => onItemClick(e, item)}
                    aria-current={isActive ? "page" : undefined}
                    className={cn(
                      "group flex w-full items-center gap-3 rounded-md px-2 text-left text-sm transition-colors",
                      "focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/25",
                      touch ? "min-h-11 py-1.5" : "min-h-9 py-1",
                      isActive
                        ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground"
                        : "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                    )}
                  >
                    {item.icon && (
                      <span
                        aria-hidden
                        className={cn(
                          "flex size-7 shrink-0 items-center justify-center rounded-md border border-border bg-card shadow-control [&_svg]:size-4",
                          isActive ? "text-foreground" : "text-muted-foreground group-hover:text-foreground",
                        )}
                      >
                        <item.icon />
                      </span>
                    )}
                    <span className="min-w-0 flex-1">
                      <span className={cn("block", touch ? "leading-snug" : "truncate")}>{item.title}</span>
                      {item.description && (
                        <span className="block truncate text-xs font-normal text-muted-foreground">{item.description}</span>
                      )}
                    </span>
                  </a>
                </li>
              )
            })}
          </ul>
        </div>
      ))}
    </div>
  )
}

// ── Desktop: companion panel ─────────────────────────────────────────────

interface SectionPanelProps {
  section: ResolvedNavSection | null
  open: boolean
  onClose: () => void
  onItemClick: ItemClick
  currentPath: string
}

export function SectionPanel({
  section,
  open,
  onClose,
  onItemClick,
  currentPath,
}: SectionPanelProps) {
  const panelRef = useRef<HTMLDivElement>(null)
  const { state: sidebarState } = useSidebar()
  const panelLeft = sidebarState === "collapsed"
    ? "var(--sidebar-width-icon, 3rem)"
    : "var(--sidebar-width, 14rem)"

  // Close on Escape
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose()
    },
    [onClose],
  )

  useEffect(() => {
    if (open) {
      document.addEventListener("keydown", handleKeyDown)
      return () => document.removeEventListener("keydown", handleKeyDown)
    }
  }, [open, handleKeyDown])

  const handleItemClick = (e: React.MouseEvent, item: ResolvedSectionItem) => {
    onItemClick(e, item)
    onClose()
  }

  const Icon = section?.icon

  return (
    <>
      {/* Backdrop over the page only, never over the sidebar. On narrow windows the panel
          covers most of the page, so the page is dimmed to show it is not interactive. */}
      {open && (
        <div
          className="fixed inset-0 z-40 bg-scrim/25 xl:bg-transparent"
          style={{ left: panelLeft }}
          onClick={onClose}
          aria-hidden
        />
      )}

      {/* Companion panel — slides out from the sidebar edge and never runs past the window. */}
      <div
        ref={panelRef}
        className={cn(
          "fixed top-0 bottom-0 z-40",
          "bg-sidebar border-r border-border shadow-floating",
          "flex flex-col",
          "transition-[transform,visibility] duration-200 ease-out",
          open
            ? "translate-x-0 visible"
            : "-translate-x-full invisible pointer-events-none",
        )}
        style={{ left: panelLeft, width: `min(22rem, calc(100vw - ${panelLeft}))` }}
        role="dialog"
        aria-label={section?.title ?? "Панель навигации"}
      >
        {section && (
          <>
            <div className="flex items-center gap-3 border-b border-border px-4 pb-3 pt-4">
              {Icon && (
                <div className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-border bg-card text-foreground shadow-control">
                  <Icon className="size-4" />
                </div>
              )}
              <h2 className="flex-1 truncate text-base font-semibold text-foreground">
                {section.title}
              </h2>
              <button
                type="button"
                onClick={onClose}
                className="flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/25"
                aria-label="Закрыть"
              >
                <X className="size-4" />
              </button>
            </div>

            <ScrollArea className="flex-1">
              <div className="px-2 py-3">
                <SectionGroups groups={section.groups} currentPath={currentPath} onItemClick={handleItemClick} />
              </div>
            </ScrollArea>
          </>
        )}
      </div>
    </>
  )
}

// ── Mobile: drill-down inside the sidebar drawer ─────────────────────────

/**
 * On phones the sidebar is a drawer, so a section opens inside it as its own screen
 * with a way back, instead of a panel beside a sidebar that is not there.
 * The state lives in this component: the drawer unmounts its content when it closes,
 * so the next time it opens on the main menu.
 */
export function MobileSectionDrill({ children, currentPath, onItemClick }: {
  children: (openSection: (section: ResolvedNavSection) => void) => ReactNode
  currentPath: string
  onItemClick: ItemClick
}) {
  const [section, setSection] = useState<ResolvedNavSection | null>(null)
  if (!section) return <>{children(setSection)}</>
  const Icon = section.icon
  return (
    <SidebarContent className="gap-0">
      <div className="flex items-center gap-1 px-2 pt-1">
        <button
          type="button"
          onClick={() => setSection(null)}
          className="-ml-1 flex min-h-11 items-center gap-1 rounded-md px-2 text-sm text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/25"
        >
          <ChevronLeft className="size-4" />
          Меню
        </button>
      </div>
      <div className="flex items-center gap-2.5 px-4 pb-3 pt-1">
        <span aria-hidden className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-border bg-card text-foreground shadow-control [&_svg]:size-4"><Icon /></span>
        <h2 className="truncate text-base font-semibold text-foreground">{section.title}</h2>
      </div>
      <div className="px-2 pb-4">
        <SectionGroups groups={section.groups} currentPath={currentPath} onItemClick={onItemClick} touch />
      </div>
    </SidebarContent>
  )
}
