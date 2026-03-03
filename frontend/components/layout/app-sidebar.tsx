"use client"

import { useState } from "react"
import { usePathname, useRouter } from "next/navigation"
import {
  LayoutDashboard,
  ShoppingCart,
  Package,
  Warehouse,
  Wallet,
  Building2,
  Settings,
  HelpCircle,
  Users,
  TrendingUp,
  User,
  ChevronRight,
  Bell,
  MoreHorizontal,
  LogOut,
} from "lucide-react"

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@radix-ui/react-collapsible"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarRail,
} from "@/components/ui/sidebar"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Badge } from "@/components/ui/badge"
import { LogoIcon } from "@/components/icons/logo"
import { useTabsStore } from "@/stores/useTabsStore"
import { useAuthStore } from "@/stores/useAuthStore"
import { api } from "@/lib/api"
import { UserPreferencesDialog } from "@/components/shared/user-preferences-dialog"

/** breadcrumb label resolver — used to derive tab titles from URL segments */
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

const navMain = [
  { title: "Главное", url: "/", icon: LayoutDashboard },
  { title: "CRM", url: "/crm", icon: Users },
  { title: "Продажи", url: "/sales", icon: TrendingUp },
  { title: "Закупки", url: "/purchases", icon: ShoppingCart },
  { title: "Склад", url: "/warehouse", icon: Warehouse },
  { title: "Деньги", url: "/finance", icon: Wallet },
  { title: "Компания", url: "/company", icon: Building2 },
]

const navSecondary = [
  {
    title: "Справочники",
    url: "/catalogs/nomenclature",
    icon: Package,
    children: [
      { title: "Номенклатура", url: "/catalogs/nomenclature" },
      { title: "Контрагенты", url: "/catalogs/counterparties" },
      { title: "Склады", url: "/catalogs/warehouses" },
      { title: "Организации", url: "/catalogs/organizations" },
    ],
  },
  { title: "Настройки", url: "/settings", icon: Settings },
  { title: "Помощь", url: "/help", icon: HelpCircle },
]

/** Demo notification count; replace with real data source */
const NOTIFICATION_COUNT = 3

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const pathname = usePathname()
  const router = useRouter()
  const { openTab, activeTabId, tabs } = useTabsStore()
  const user = useAuthStore((s) => s.user)
  const logout = useAuthStore((s) => s.logout)
  const [prefsOpen, setPrefsOpen] = useState(false)

  const isActive = (url: string) =>
    url === "/" ? pathname === "/" : pathname.startsWith(url)

  /** Opens a tab (singleton) and navigates. */
  const handleNavClick = (
    e: React.MouseEvent,
    item: { title: string; url: string }
  ) => {
    e.preventDefault()

    openTab({
      id: item.url,
      title: item.title,
      url: item.url,
    })

    router.push(item.url)
  }

  const handleLogout = async () => {
    try {
      await api.auth.logout()
    } catch {
      // ignore — we still want to clear local state
    }
    logout()
    router.replace("/login")
  }

  return (
    <>
      <Sidebar collapsible="icon" {...props}>
        <SidebarHeader>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton
                size="lg"
                asChild
                onClick={(e: React.MouseEvent) =>
                  handleNavClick(e, { title: "Главное", url: "/" })
                }
              >
                {/* eslint-disable-next-line @next/next/no-html-link-for-pages */}
                <a href="/">
                  <div className="flex aspect-square size-8 items-center justify-center overflow-hidden rounded-lg">
                    <LogoIcon size={32} />
                  </div>
                  <div className="grid flex-1 text-left text-sm leading-tight">
                    <span className="truncate font-semibold">Metapus</span>
                    <span className="truncate text-xs text-muted-foreground">
                      ERP Platform
                    </span>
                  </div>
                </a>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarHeader>

        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupLabel>Навигация</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {navMain.map((item) => (
                  <SidebarMenuItem key={item.url}>
                    <SidebarMenuButton
                      asChild
                      isActive={isActive(item.url)}
                      tooltip={item.title}
                    >
                      <a
                        href={item.url}
                        onClick={(e) => handleNavClick(e, item)}
                      >
                        <item.icon />
                        <span>{item.title}</span>
                      </a>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          <SidebarGroup>
            <SidebarGroupLabel>Система</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {navSecondary.map((item) =>
                  item.children ? (
                    <Collapsible key={item.url} asChild defaultOpen={pathname.startsWith("/catalogs")} className="group/collapsible">
                      <SidebarMenuItem>
                        <CollapsibleTrigger asChild>
                          <SidebarMenuButton tooltip={item.title}>
                            <item.icon />
                            <span>{item.title}</span>
                            <ChevronRight className="ml-auto h-4 w-4 transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />
                          </SidebarMenuButton>
                        </CollapsibleTrigger>
                        <CollapsibleContent>
                          <SidebarMenuSub>
                            {item.children.map((child) => (
                              <SidebarMenuSubItem key={child.url}>
                                <SidebarMenuSubButton
                                  asChild
                                  isActive={pathname.startsWith(child.url)}
                                >
                                  <a
                                    href={child.url}
                                    onClick={(e) => handleNavClick(e, child)}
                                  >
                                    <span>{child.title}</span>
                                  </a>
                                </SidebarMenuSubButton>
                              </SidebarMenuSubItem>
                            ))}
                          </SidebarMenuSub>
                        </CollapsibleContent>
                      </SidebarMenuItem>
                    </Collapsible>
                  ) : (
                    <SidebarMenuItem key={item.url}>
                      <SidebarMenuButton
                        asChild
                        isActive={isActive(item.url)}
                        tooltip={item.title}
                      >
                        <a
                          href={item.url}
                          onClick={(e) => handleNavClick(e, item)}
                        >
                          <item.icon />
                          <span>{item.title}</span>
                        </a>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  )
                )}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>

        {/* ── User Profile Footer ── */}
        <SidebarFooter>
          <SidebarMenu>
            <SidebarMenuItem>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <SidebarMenuButton
                    size="lg"
                    className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
                    tooltip="Пользователь"
                  >
                    <div className="flex aspect-square size-8 items-center justify-center rounded-full bg-muted border border-border">
                      <User className="h-4 w-4 text-foreground" />
                    </div>
                    <div className="grid flex-1 text-left text-sm leading-tight">
                      <span className="truncate font-semibold text-sm">
                        {user?.fullName || "Пользователь"}
                      </span>
                      <span className="truncate text-xs text-muted-foreground">
                        {user?.email || ""}
                      </span>
                    </div>
                    <MoreHorizontal className="ml-auto h-4 w-4 text-muted-foreground" />
                  </SidebarMenuButton>
                </DropdownMenuTrigger>
                <DropdownMenuContent
                  className="w-56"
                  side="top"
                  align="end"
                  sideOffset={4}
                >
                  <DropdownMenuItem className="gap-2">
                    <Bell className="h-4 w-4" />
                    <span className="flex-1">Уведомления</span>
                    {NOTIFICATION_COUNT > 0 && (
                      <Badge
                        variant="destructive"
                        className="ml-auto h-5 min-w-[1.25rem] justify-center px-1 text-[10px]"
                      >
                        {NOTIFICATION_COUNT}
                      </Badge>
                    )}
                  </DropdownMenuItem>
                  <DropdownMenuItem className="gap-2" onSelect={() => setPrefsOpen(true)}>
                    <Settings className="h-4 w-4" />
                    Настройки профиля
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem className="gap-2 text-destructive focus:text-destructive" onClick={handleLogout}>
                    <LogOut className="h-4 w-4" />
                    Выйти
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarFooter>

        <SidebarRail />
      </Sidebar>
      <UserPreferencesDialog open={prefsOpen} onOpenChange={setPrefsOpen} />
    </>
  )
}
