"use client"

import { useEffect } from "react"
import { usePathname, useSearchParams } from "next/navigation"
import { SidebarProvider, SidebarInset } from "@/components/ui/sidebar"
import { AppSidebar } from "./app-sidebar"
import { SiteHeader } from "./site-header"
import { useTabsStore } from "@/stores/useTabsStore"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { ImpersonationBanner } from "./impersonation-banner"
import { resolveTitleFromUrl } from "@/lib/tab-utils"
import { toast } from "sonner"

/**
 * RouteSync — ensures that if the user navigates via browser back/forward
 * or directly via URL, the tabs store stays in sync.
 */
function RouteSync() {
  const pathname = usePathname()
  const searchParams = useSearchParams()
  const { tabs, openTab, setActiveTab, updateTabUrl } = useTabsStore()

  useEffect(() => {
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
      })
      if (result.warning) toast.warning(result.warning)
    }
  }, [pathname, searchParams]) // eslint-disable-line react-hooks/exhaustive-deps

  return null
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const fetchMeta = useMetadataStore((s) => s.fetch)
  useEffect(() => { fetchMeta() }, [fetchMeta])

  // Rehydrate persisted tabs (must happen client-side to avoid hydration mismatch)
  useEffect(() => { useTabsStore.persist.rehydrate() }, [])

  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <ImpersonationBanner />
        <RouteSync />
        <SiteHeader />
        <div className="flex flex-1 flex-col min-h-0 overflow-hidden">
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}
