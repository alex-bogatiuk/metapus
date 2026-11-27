"use client"

import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { useShortcut } from "@/hooks/useShortcut"
import { resolveTitleFromUrl } from "@/lib/tab-utils"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { useTabsStore } from "@/stores/useTabsStore"
import dynamic from "next/dynamic"
import { usePathname, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { AppSidebar } from "./app-sidebar"
import { ImpersonationBanner } from "./impersonation-banner"
import { MFAEnrollmentBanner } from "./mfa-enrollment-banner"
import { ShortcutManager } from "./shortcut-manager"
import { SiteHeader } from "./site-header"

// Closed until summoned: they load beside the shell instead of delaying its first paint.
const CommandPalette = dynamic(() => import("./command-palette").then((m) => m.CommandPalette), { ssr: false })
const ShortcutHelpDialog = dynamic(() => import("./shortcut-help-dialog").then((m) => m.ShortcutHelpDialog), { ssr: false })

/**
 * RouteSync — ensures that if the user navigates via browser back/forward
 * or directly via URL, the tabs store stays in sync.
 */
function RouteSync() {
  const pathname = usePathname()
  const searchParams = useSearchParams()
  // AppShell restores the persisted tabs in its own effect, which React runs
  // after this child's. Opening a tab before that would persist the default
  // set over the saved tabs, so wait for the restore.
  const hydrated = useTabsStore((s) => s._hasHydrated)
  const metadataLoaded = useMetadataStore((s) => s.loaded)
  useEffect(() => {
    if (!hydrated) return
    const { tabs, openTab, setActiveTab, updateTabUrl } = useTabsStore.getState()
    const search = searchParams.toString()
    const fullUrl = search ? `${pathname}?${search}` : pathname

    const matchingTab = tabs.find((t) => t.id === pathname)
    if (matchingTab) {
      setActiveTab(matchingTab.id)
      // Keep tab URL in sync (e.g. ?copyFrom= added)
      if (matchingTab.url !== fullUrl) {
        updateTabUrl(matchingTab.id, fullUrl)
      }
    } else {
      // Auto-open a tab for this URL (e.g. browser back/forward)
      const result = openTab({
        id: pathname,
        title: resolveTitleFromUrl(pathname),
        url: fullUrl,
        provisional: true,
      })
      if (result.warning) toast.warning(result.warning)
    }
  }, [pathname, searchParams, hydrated])

  // A title read from the URL before metadata loaded can miss the entity's
  // name; resolve it again once metadata is in. This also heals tabs saved
  // with such a guess before tabs recorded it.
  useEffect(() => {
    if (hydrated && metadataLoaded) useTabsStore.getState().refreshProvisionalTitles(resolveTitleFromUrl)
  }, [hydrated, metadataLoaded])

  return null
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const fetchMeta = useMetadataStore((s) => s.fetch)
  useEffect(() => { fetchMeta() }, [fetchMeta])

  // Rehydrate persisted tabs (must happen client-side to avoid hydration mismatch)
  useEffect(() => { useTabsStore.persist.rehydrate() }, [])

  // ── Keyboard shortcuts: Help Dialog (Ctrl+/) ──────────────────────────
  const [helpOpen, setHelpOpen] = useState(false)
  const openHelp = useCallback(() => setHelpOpen(true), [])
  useShortcut("general.help", "mod+/", "Горячие клавиши", "general", openHelp)

  return (
    <SidebarProvider>
      {/* First in tab order: keyboard users skip the sidebar and the tab bar. */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:left-3 focus:top-3 focus:z-[100] focus:rounded-md focus:bg-background focus:px-3 focus:py-2 focus:text-sm focus:font-medium focus:shadow-md focus:outline-none focus:ring-2 focus:ring-ring"
      >
        Перейти к содержимому
      </a>
      <ShortcutManager />
      <ShortcutHelpDialog open={helpOpen} onOpenChange={setHelpOpen} />
      <CommandPalette />
      <AppSidebar />
      <SidebarInset>
        <ImpersonationBanner />
        <MFAEnrollmentBanner />
        <RouteSync />
        <SiteHeader />
        <div id="main-content" tabIndex={-1} className="flex flex-1 flex-col min-h-0 overflow-hidden bg-workspace-surface outline-none">
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}
