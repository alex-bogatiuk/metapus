"use client"

import { useEffect } from "react"
import { usePathname } from "next/navigation"
import { SidebarProvider, SidebarInset } from "@/components/ui/sidebar"
import { AppSidebar } from "./app-sidebar"
import { SiteHeader } from "./site-header"
import { useTabsStore } from "@/stores/useTabsStore"

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
  const { tabs, openTab, setActiveTab } = useTabsStore()

  useEffect(() => {
    const matchingTab = tabs.find((t) => t.url === pathname)
    if (matchingTab) {
      setActiveTab(matchingTab.id)
    } else {
      // Auto-open a tab for this URL (e.g. browser back/forward)
      openTab({
        id: pathname,
        title: resolveTitleFromUrl(pathname),
        url: pathname,
      })
    }
  }, [pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  return null
}

export function AppShell({ children }: { children: React.ReactNode }) {
  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <RouteSync />
        <SiteHeader />
        <div className="flex flex-1 flex-col min-h-0 overflow-hidden">
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}
