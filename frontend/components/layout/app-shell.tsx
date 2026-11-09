"use client"

import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { useShortcut } from "@/hooks/useShortcut"
import { resolveTitleFromUrl } from "@/lib/tab-utils"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { useTabsStore } from "@/stores/useTabsStore"
import { usePathname, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { AppSidebar } from "./app-sidebar"
import { CommandPalette } from "./command-palette"
import { ImpersonationBanner } from "./impersonation-banner"
import { MFAEnrollmentBanner } from "./mfa-enrollment-banner"
import { ShortcutHelpDialog } from "./shortcut-help-dialog"
import { ShortcutManager } from "./shortcut-manager"
import { SiteHeader } from "./site-header"

/**
 * RouteSync — ensures that if the user navigates via browser back/forward
 * or directly via URL, the tabs store stays in sync.
 */
function RouteSync() {
  const pathname = usePathname()
  const searchParams = useSearchParams()
  useEffect(() => {
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
      // Heal persisted tab if title was a raw UUID
      const uuidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
      if (uuidRegex.test(matchingTab.title)) {
        const resolved = resolveTitleFromUrl(pathname)
        if (resolved && !uuidRegex.test(resolved)) {
          useTabsStore.getState().updateTabTitle(matchingTab.id, resolved)
        }
      }
    } else {
      // Auto-open a tab for this URL (e.g. browser back/forward)
      const result = openTab({
        id: pathname,
        title: resolveTitleFromUrl(pathname),
        url: fullUrl,
      })
      if (result.warning) toast.warning(result.warning)
    }
  }, [pathname, searchParams])

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
      <ShortcutManager />
      <ShortcutHelpDialog open={helpOpen} onOpenChange={setHelpOpen} />
      <CommandPalette />
      <AppSidebar />
      <SidebarInset>
        <ImpersonationBanner />
        <MFAEnrollmentBanner />
        <RouteSync />
        <SiteHeader />
        <div className="flex flex-1 flex-col min-h-0 overflow-hidden bg-workspace-surface">
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}
