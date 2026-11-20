"use client"

import { useEffect } from "react"
import { ChevronRight } from "lucide-react"
import type { LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useSettingsStore } from "@/stores/useSettingsStore"
import type { SettingsSection } from "@/stores/useSettingsStore"
import { useTabState } from "@/hooks/useTabState"
import { PermissionGate } from "@/components/auth/permission-gate"
import { SYSTEM_ADMIN } from "@/lib/system-permissions"
import { getSettingsPages, settingsSections } from "@/lib/settings-registry"
import type { SettingSectionDef } from "@/lib/settings-registry"
import { SettingsSectionRenderer } from "@/components/settings/settings-section-renderer"
import { createTabStateKey } from "@/stores/useTabStateStore"

const tabStateKeys = {
  "activeSection": createTabStateKey<ActiveSection>("frontend/app/(main)/settings/page.tsx:activeSection"),
}

// The system settings. The user's own preferences (theme, formats) open from
// the profile menu in the sidebar (UserPreferencesDialog).

// A sys_settings JSONB section, or the id of a page a configuration registers.
type ActiveSection = SettingsSection | (string & {})

const DEFAULT_SECTION = "performance"

const generalSections = settingsSections.filter((s) => s.category === "general")
const moduleSections = settingsSections.filter((s) => s.category === "module")

/** Registered settings pages, grouped by their sidebar group in registration order. */
function settingsPageGroups() {
  const groups = new Map<string, ReturnType<typeof getSettingsPages>>()
  for (const page of getSettingsPages()) groups.set(page.group, [...(groups.get(page.group) ?? []), page])
  return [...groups]
}

function SettingsPageContent() {
  const [activeSection, setActiveSection] =
    useTabState(tabStateKeys["activeSection"], DEFAULT_SECTION)

  const fetchSettings = useSettingsStore((s) => s.fetchSettings)

  useEffect(() => {
    fetchSettings()
  }, [fetchSettings])

  const activeDef = settingsSections.find((s) => s.id === activeSection) as SettingSectionDef | undefined
  const pageGroups = settingsPageGroups()
  const activePage = getSettingsPages().find((p) => p.id === activeSection)
  // A remembered section that no longer exists (the former profile among
  // them) opens the first one.
  const sectionExists = !!activeDef || !!activePage
  useEffect(() => {
    if (!sectionExists) setActiveSection(DEFAULT_SECTION)
  }, [sectionExists, setActiveSection])

  return (
    <div className="flex h-full flex-col md:flex-row">
      {/* Left sidebar — section navigation */}
      <div className="hidden w-64 shrink-0 border-r bg-muted/30 pb-4 md:block overflow-y-auto scrollbar-hide">
        <div className="px-4 py-4">
          <h1 className="text-sm font-semibold text-foreground">Настройки</h1>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Параметры системы
          </p>
        </div>

        {/* General sections */}
        <nav className="flex flex-col gap-0.5 px-2">
          {generalSections.map((section) => (
            <SidebarItem
              key={section.id}
              section={section}
              isActive={section.id === activeSection}
              onClick={() => setActiveSection(section.id)}
            />
          ))}
        </nav>

        {/* Module sections */}
        {moduleSections.length > 0 && (
          <div className="mt-4 border-t pt-4 px-2">
            <div className="px-3 mb-2">
              <span className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
                Модули
              </span>
            </div>
            <nav className="flex flex-col gap-0.5">
              {moduleSections.map((section) => (
                <SidebarItem
                  key={section.id}
                  section={section}
                  isActive={section.id === activeSection}
                  onClick={() => setActiveSection(section.id)}
                />
              ))}
            </nav>
          </div>
        )}

        {/* Settings pages registered by configurations */}
        {pageGroups.map(([group, pages]) => (
          <div key={group} className="mt-4 border-t pt-4 px-2">
            <div className="px-3 mb-2">
              <span className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
                {group}
              </span>
            </div>
            <nav className="flex flex-col gap-0.5">
              {pages.map((page) => (
                <SidebarItem
                  key={page.id}
                  section={{ icon: page.icon, title: page.title, description: page.summary }}
                  isActive={page.id === activeSection}
                  onClick={() => setActiveSection(page.id)}
                />
              ))}
            </nav>
          </div>
        ))}
      </div>

      {/* Mobile section selector */}
      <div className="block w-full border-b bg-muted/30 px-4 py-2 md:hidden">
        <select
          value={sectionExists ? activeSection : DEFAULT_SECTION}
          onChange={(e) => setActiveSection(e.target.value as ActiveSection)}
          className="w-full rounded-md border bg-workspace-surface px-3 py-2 text-sm"
        >
          <optgroup label="Общие">
            {generalSections.map((s) => (
              <option key={s.id} value={s.id}>{s.title}</option>
            ))}
          </optgroup>
          {moduleSections.length > 0 && (
            <optgroup label="Модули">
              {moduleSections.map((s) => (
                <option key={s.id} value={s.id}>{s.title}</option>
              ))}
            </optgroup>
          )}
          {pageGroups.map(([group, pages]) => (
            <optgroup key={group} label={group}>
              {pages.map((page) => (
                <option key={page.id} value={page.id}>{page.title}</option>
              ))}
            </optgroup>
          ))}
        </select>
      </div>

      {/* Right content area */}
      <ScrollArea data-material="workspace" className="min-h-0 w-full flex-1">
        <div className="mx-auto max-w-4xl px-6 py-6">
          {activePage ? (
            <>
              <div className="mb-6">
                <h2 className="text-lg font-semibold text-foreground">{activePage.title}</h2>
                <p className="mt-0.5 text-sm text-muted-foreground">{activePage.description}</p>
              </div>
              <activePage.component />
            </>
          ) : activeDef ? (
            <>
              <div className="mb-6">
                <h2 className="text-lg font-semibold text-foreground">
                  {activeDef.title}
                </h2>
                <p className="mt-0.5 text-sm text-muted-foreground">
                  {activeDef.description}
                </p>
              </div>
              <SettingsSectionRenderer section={activeDef} />
            </>
          ) : null}
        </div>
      </ScrollArea>
    </div>
  )
}

export default function SettingsPage() {
  return (
    <PermissionGate permission={SYSTEM_ADMIN} subject="системным настройкам">
      <SettingsPageContent />
    </PermissionGate>
  )
}

// ── Sidebar item ────────────────────────────────────────────────────────

interface SidebarItemProps {
  section: { icon: LucideIcon; title: string; description: string }
  isActive: boolean
  onClick: () => void
}

function SidebarItem({ section, isActive, onClick }: SidebarItemProps) {
  const Icon = section.icon
  return (
    <button
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 focus-visible:ring-offset-1",
        isActive
          ? "border border-border/65 bg-workspace-surface text-foreground shadow-control"
          : "text-muted-foreground hover:bg-background/60 hover:text-foreground"
      )}
    >
      <Icon
        className={cn(
          "h-4 w-4 shrink-0",
          isActive ? "text-primary" : "text-muted-foreground"
        )}
      />
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium">{section.title}</div>
        <div className="truncate text-[11px] text-muted-foreground">
          {section.description}
        </div>
      </div>
      {isActive && (
        <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      )}
    </button>
  )
}
