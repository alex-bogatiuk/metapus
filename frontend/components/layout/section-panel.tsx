// frontend/components/layout/section-panel.tsx
"use client"

import { useEffect, useRef, useCallback } from "react"
import type { LucideIcon } from "lucide-react"
import { X } from "lucide-react"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useSidebar } from "@/components/ui/sidebar"
import { cn } from "@/lib/utils"

// ── Types ────────────────────────────────────────────────────────────────

export interface NavSectionItem {
  /** Stable entity key matching backend metadata.Key, e.g. "goods_receipt", "counterparty" */
  entityKey: string
  fallback: string
  description?: string
  /** Explicit URL overrides entityKey resolution */
  url?: string
}

export interface NavSectionGroup {
  label: string
  items: NavSectionItem[]
}

export interface NavSection {
  title: string
  icon: LucideIcon
  groups: NavSectionGroup[]
}

/** Resolved version with titles/urls computed from metadata */
export interface ResolvedSectionItem {
  title: string
  url: string
  description?: string
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

// ── Component ────────────────────────────────────────────────────────────

interface SectionPanelProps {
  section: ResolvedNavSection | null
  open: boolean
  onClose: () => void
  onItemClick: (e: React.MouseEvent, item: { title: string; url: string }) => void
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
    : "var(--sidebar-width, 12rem)"

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

  const handleCardClick = (
    e: React.MouseEvent,
    item: ResolvedSectionItem,
  ) => {
    onItemClick(e, item)
    onClose()
  }

  // Filter out empty groups
  const visibleGroups = section?.groups.filter((g) => g.items.length > 0) ?? []

  const Icon = section?.icon

  return (
    <>
      {/* Transparent backdrop — covers ONLY main content, NOT the sidebar */}
      {open && (
        <div
          className="fixed inset-0 z-40"
        style={{ left: panelLeft }}
          onClick={onClose}
          aria-hidden
        />
      )}

      {/* Companion panel — slides out from sidebar edge */}
      <div
        ref={panelRef}
        className={cn(
          "fixed top-0 bottom-0 z-40",
          "w-[340px] sm:w-[400px]",
          "bg-sidebar border-r border-border shadow-xl",
          "flex flex-col",
          "transition-[transform,visibility] duration-200 ease-out",
          open
            ? "translate-x-0 visible"
            : "-translate-x-full invisible pointer-events-none",
        )}
        style={{ left: panelLeft }}
        role="dialog"
        aria-label={section?.title ?? "Панель навигации"}
      >
        {section && (
          <>
            {/* Header */}
            <div className="flex items-center gap-3 px-5 pt-5 pb-3 border-b border-border">
              {Icon && (
                <div className="flex items-center justify-center size-9 rounded-lg bg-primary/10 text-primary shrink-0">
                  <Icon className="size-5" />
                </div>
              )}
              <h2 className="text-lg font-semibold text-foreground flex-1 truncate">
                {section.title}
              </h2>
              <button
                type="button"
                onClick={onClose}
                className="flex items-center justify-center size-7 rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
                aria-label="Закрыть"
              >
                <X className="size-4" />
              </button>
            </div>

            {/* Scrollable content */}
            <ScrollArea className="flex-1">
              <div className="px-5 py-4 space-y-5">
                {visibleGroups.map((group) => (
                  <div key={group.label}>
                    {/* Group label */}
                    <h3 className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-2.5">
                      {group.label}
                    </h3>

                    {/* Cards grid */}
                    <div className="grid grid-cols-2 gap-2">
                      {group.items.map((item) => {
                        const isActive = currentPath.startsWith(item.url)
                        return (
                          <button
                            key={item.url}
                            type="button"
                            onClick={(e) => handleCardClick(e, item)}
                            className={cn(
                              "group relative flex flex-col items-start gap-1 rounded-lg border p-3 text-left text-sm",
                              "transition-all duration-150",
                              "hover:bg-accent hover:border-accent-foreground/20 hover:shadow-sm",
                              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                              isActive
                                ? "bg-accent border-primary/30 shadow-sm"
                                : "bg-card/50 border-border",
                            )}
                          >
                            <span
                              className={cn(
                                "font-medium leading-tight",
                                isActive ? "text-primary" : "text-foreground",
                              )}
                            >
                              {item.title}
                            </span>
                            {item.description && (
                              <span className="text-xs text-muted-foreground leading-snug line-clamp-2">
                                {item.description}
                              </span>
                            )}
                          </button>
                        )
                      })}
                    </div>
                  </div>
                ))}

                {visibleGroups.length === 0 && (
                  <p className="text-sm text-muted-foreground text-center py-8">
                    В этом разделе пока нет доступных пунктов
                  </p>
                )}
              </div>
            </ScrollArea>
          </>
        )}
      </div>
    </>
  )
}
