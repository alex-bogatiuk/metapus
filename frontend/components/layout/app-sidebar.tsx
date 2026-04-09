// frontend/components/layout/app-sidebar.tsx
"use client"

import { useMemo, useState } from "react"
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
  ScrollText,
  TrendingUp,
  User,
  ChevronRight,
  Bell,
  MoreHorizontal,
  LogOut,
  Puzzle,
} from "lucide-react"
import type { LucideIcon } from "lucide-react"

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
import { useMetadataStore } from "@/stores/useMetadataStore"
import { buildEntityUrlByRoute } from "@/lib/entity-url"
import { api } from "@/lib/api"
import { UserPreferencesDialog } from "@/components/shared/user-preferences-dialog"
import {
  SectionPanel,
  type NavSection,
  type ResolvedNavSection,
} from "@/components/layout/section-panel"

// ── Navigation structure ────────────────────────────────────────────────

/** A flat navigation item (Главное, Журнал событий, etc.) */
interface NavItem {
  title: string
  url: string
  icon: LucideIcon
}

/**
 * Grouped sections — clicking a section button opens a Sheet panel
 * with items organized by group (Документы, Справочники, Отчёты).
 */
const navSections: NavSection[] = [
  {
    title: "Закупки",
    icon: ShoppingCart,
    groups: [
      {
        label: "Документы",
        items: [
          { entityKey: "goods_receipt", fallback: "Поступления товаров" },
        ],
      },
      {
        label: "Справочники",
        items: [
          { entityKey: "counterparty", fallback: "Контрагенты" },
          { entityKey: "contract", fallback: "Договоры" },
        ],
      },
    ],
  },
  {
    title: "Продажи",
    icon: TrendingUp,
    groups: [
      {
        label: "Документы",
        items: [
          { entityKey: "goods_issue", fallback: "Реализации товаров" },
        ],
      },
      {
        label: "Справочники",
        items: [
          { entityKey: "counterparty", fallback: "Контрагенты" },
          { entityKey: "contract", fallback: "Договоры" },
        ],
      },
    ],
  },
  {
    title: "Склад",
    icon: Warehouse,
    groups: [
      {
        label: "Справочники",
        items: [
          { entityKey: "warehouse", fallback: "Склады" },
          { entityKey: "nomenclature", fallback: "Номенклатура" },
          { entityKey: "unit", fallback: "Единицы измерения" },
        ],
      },
    ],
  },
  {
    title: "Деньги",
    icon: Wallet,
    groups: [
      {
        label: "Справочники",
        items: [
          { entityKey: "currency", fallback: "Валюты" },
        ],
      },
    ],
  },
  {
    title: "Справочники",
    icon: Package,
    groups: [
      {
        label: "Справочники",
        items: [
          { entityKey: "nomenclature", fallback: "Номенклатура" },
          { entityKey: "counterparty", fallback: "Контрагенты" },
          { entityKey: "warehouse", fallback: "Склады" },
          { entityKey: "organization", fallback: "Организации" },
          { entityKey: "currency", fallback: "Валюты" },
          { entityKey: "unit", fallback: "Единицы измерения" },
          { entityKey: "vat_rate", fallback: "Ставки НДС" },
          { entityKey: "contract", fallback: "Договоры" },
        ],
      },
    ],
  },
]

/** Flat top-level items that create tabs directly. */
const navFlat: NavItem[] = [
  { title: "Главное", url: "/", icon: LayoutDashboard },
]

/** Bottom-section flat items */
const navBottom: NavItem[] = [
  { title: "Журнал событий", url: "/settings/event-log", icon: ScrollText },
  { title: "Настройки", url: "/settings", icon: Settings },
  { title: "Помощь", url: "/help", icon: HelpCircle },
]

/** Demo notification count; replace with real data source */
const NOTIFICATION_COUNT = 3

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const pathname = usePathname()
  const router = useRouter()
  const { openTab } = useTabsStore()
  const user = useAuthStore((s) => s.user)
  const logout = useAuthStore((s) => s.logout)
  const getEntity = useMetadataStore((s) => s.getEntity)
  const metaLoaded = useMetadataStore((s) => s.loaded)
  const [prefsOpen, setPrefsOpen] = useState(false)
  const [activeSection, setActiveSection] = useState<ResolvedNavSection | null>(null)

  // ── Resolve section groups from metadata ───────────────────────────
  const resolvedSections = useMemo(() => {
    return navSections
      .filter((s) => s.groups.some((g) => g.items.length > 0))
      .map((section): ResolvedNavSection => ({
        title: section.title,
        icon: section.icon,
        groups: section.groups.map((group) => {
          const resolvedItems = group.items.map((item) => {
            const entity = getEntity(item.entityKey)
            const title = entity?.presentation.plural ?? item.fallback
            const routePrefix = entity?.routePrefix ?? item.entityKey
            const entityType = entity?.type ?? "catalog"
            const url = buildEntityUrlByRoute(routePrefix, entityType)
            return { title, url, description: item.description }
          })
          return { label: group.label, items: resolvedItems }
        }),
      }))
  }, [getEntity, metaLoaded]) // eslint-disable-line react-hooks/exhaustive-deps

  // ── Dynamic extension items from metadata ─────────────────────────────
  // Entities not covered by hardcoded navSections appear here automatically.
  const extensionItems = useMemo(() => {
    if (!metaLoaded) return []

    // Collect all entity keys already in navSections
    const coveredKeys = new Set<string>()
    for (const section of navSections) {
      for (const group of section.groups) {
        for (const item of group.items) {
          coveredKeys.add(item.entityKey)
        }
      }
    }

    const { entities } = useMetadataStore.getState()
    return entities
      .filter((e) => {
        return !coveredKeys.has(e.key) && (e.type === "catalog" || e.type === "document")
      })
      .map((e) => {
        const prefix = e.routePrefix ?? e.key
        const title = e.presentation?.plural ?? e.name
        const url = buildEntityUrlByRoute(prefix, e.type)
        return { title, url }
      })
  }, [metaLoaded])

  const isActive = (url: string) =>
    url === "/" ? pathname === "/" : pathname.startsWith(url)

  /** Checks if any item URL in a resolved section matches the current path. */
  const isSectionActive = (section: ResolvedNavSection) =>
    section.groups.some((g) => g.items.some((item) => pathname.startsWith(item.url)))

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
                <a href="/" className="group-data-[state=collapsed]:justify-center group-data-[state=collapsed]:!gap-0">
                  <div className="flex aspect-square size-8 items-center justify-center overflow-hidden rounded-lg transition-transform duration-200 group-data-[state=collapsed]:scale-75">
                    <LogoIcon size={32} />
                  </div>
                  <div className="grid flex-1 text-left text-sm leading-tight group-data-[state=collapsed]:hidden">
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
          {/* ── Top flat items (Главное) ── */}
          <SidebarGroup>
            <SidebarGroupLabel>Навигация</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {navFlat.map((item) => (
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

                {/* ── Section buttons (open companion panel) ── */}
                {resolvedSections.map((section) => (
                  <SidebarMenuItem key={section.title}>
                    <SidebarMenuButton
                      tooltip={section.title}
                      isActive={isSectionActive(section) || activeSection?.title === section.title}
                      onClick={() =>
                        setActiveSection((prev) =>
                          prev?.title === section.title ? null : section
                        )
                      }
                    >
                      <section.icon />
                      <span>{section.title}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>

          {/* ── Extensions (auto-discovered from metadata) ── */}
          {extensionItems.length > 0 && (
            <SidebarGroup>
              <SidebarGroupLabel>Расширения</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  <Collapsible
                    asChild
                    defaultOpen={extensionItems.some(c => pathname.startsWith(c.url))}
                    className="group/collapsible"
                  >
                    <SidebarMenuItem>
                      <CollapsibleTrigger asChild>
                        <SidebarMenuButton tooltip="Расширения">
                          <Puzzle />
                          <span>Расширения</span>
                          <ChevronRight className="ml-auto h-4 w-4 transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />
                        </SidebarMenuButton>
                      </CollapsibleTrigger>
                      <CollapsibleContent>
                        <SidebarMenuSub>
                          {extensionItems.map((child) => (
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
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          )}

          {/* ── System section (Журнал событий, Настройки, Помощь) ── */}
          <SidebarGroup>
            <SidebarGroupLabel>Система</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {navBottom.map((item) => (
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

      {/* ── Section Panel (companion, non-modal) ── */}
      <SectionPanel
        section={activeSection}
        open={!!activeSection}
        onClose={() => setActiveSection(null)}
        onItemClick={handleNavClick}
        currentPath={pathname}
      />

      <UserPreferencesDialog open={prefsOpen} onOpenChange={setPrefsOpen} />
    </>
  )
}
