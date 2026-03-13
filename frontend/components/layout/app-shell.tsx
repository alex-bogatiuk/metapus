"use client"

import { useEffect } from "react"
import { usePathname, useSearchParams } from "next/navigation"
import { SidebarProvider, SidebarInset } from "@/components/ui/sidebar"
import { AppSidebar } from "./app-sidebar"
import { SiteHeader } from "./site-header"
import { useTabsStore } from "@/stores/useTabsStore"
import { ImpersonationBanner } from "./impersonation-banner"

/** breadcrumb label resolver to derive tab titles from URL segments */
const breadcrumbMap: Record<string, string> = {
  catalogs: "Справочники",
  nomenclature: "Номенклатура",
  counterparties: "Контрагенты",
  warehouses: "Склады",
  organizations: "Организации",
  purchases: "Закупки",
  "goods-receipts": "Поступления товаров",
  new: "Новый",
  sales: "Продажи",
  warehouse: "Склад",
  finance: "Деньги",
  company: "Компания",
  settings: "Настройки",
  "security-profiles": "Профили безопасности",
  crm: "CRM",
  help: "Помощь",
}

function resolveTitleFromUrl(pathname: string): string {
  if (pathname === "/") return "Главное"
  const segments = pathname.split("/").filter(Boolean)
  const lastSegment = segments[segments.length - 1]

  // /…/new → "Новый (ParentLabel)"
  if (lastSegment === "new" && segments.length >= 2) {
    const parentSegment = segments[segments.length - 2]
    const parentLabel = breadcrumbMap[parentSegment]
    if (parentLabel) return `Новый (${parentLabel})`
    return "Новый"
  }

  // Known segment → list page title
  if (breadcrumbMap[lastSegment]) return breadcrumbMap[lastSegment]

  // UUID ([id] page) — temporary title until useTabTitle updates it
  if (segments.length >= 2) {
    const parentSegment = segments[segments.length - 2]
    const parentLabel = breadcrumbMap[parentSegment]
    if (parentLabel) return `${parentLabel}…`
  }

  return lastSegment
}

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
      openTab({
        id: pathname,
        title: resolveTitleFromUrl(pathname),
        url: fullUrl,
      })
    }
  }, [pathname, searchParams]) // eslint-disable-line react-hooks/exhaustive-deps

  return null
}

export function AppShell({ children }: { children: React.ReactNode }) {
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
