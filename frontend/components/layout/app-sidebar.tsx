// frontend/components/layout/app-sidebar.tsx
"use client"

import type { LucideIcon } from "lucide-react"
import {
  Bell,
  ChevronRight,
  LayoutDashboard,
  LogOut,
  MoreHorizontal,
  Puzzle,
  Settings,
  User,
} from "lucide-react"
import { usePathname, useRouter } from "next/navigation"
import { useMemo, useState } from "react"

import { LogoIcon } from "@/components/icons/logo"
import { getEntityIcon } from "@/lib/entity-icon"
import {
  MobileSectionDrill,
  SectionPanel,
  type ResolvedNavSection,
} from "@/components/layout/section-panel"
import { getNavigation } from "@/lib/navigation"
import { UserPreferencesDialog } from "@/components/shared/user-preferences-dialog"
import { Badge } from "@/components/ui/badge"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
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
  useSidebar,
} from "@/components/ui/sidebar"
import { usePermissions } from "@/hooks/use-permissions"
import { api } from "@/lib/api"
import { buildEntityUrlByRoute } from "@/lib/entity-url"
import { allowed, entityReadPermission } from "@/lib/navigation-access"
import { useAuthStore } from "@/stores/useAuthStore"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { useNotificationStore } from "@/stores/useNotificationStore"
import { useTabsStore } from "@/stores/useTabsStore"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@radix-ui/react-collapsible"


// ── Navigation structure ────────────────────────────────────────────────

/** A flat navigation item (Главное, Журнал событий, etc.) */
interface NavItem {
  title: string
  url: string
  icon: LucideIcon
}

/** Flat top-level items that create tabs directly. */
const navFlat: NavItem[] = [
  { title: "Главное", url: "/", icon: LayoutDashboard },
]

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const pathname = usePathname()
  const router = useRouter()
  const openTab = useTabsStore((s) => s.openTab)
  const permissions = usePermissions()
  const navigation = getNavigation()
  const navBottom = navigation.systemLinks.filter((item) => !item.permission || permissions.has(item.permission))
  const user = useAuthStore((s) => s.user)
  const logout = useAuthStore((s) => s.logout)
  const getEntity = useMetadataStore((s) => s.getEntity)
  const entities = useMetadataStore((s) => s.entities)
  const metaLoaded = useMetadataStore((s) => s.loaded)
  const [prefsOpen, setPrefsOpen] = useState(false)
  const [activeSection, setActiveSection] = useState<ResolvedNavSection | null>(null)
  const { isMobile, setOpenMobile } = useSidebar()
  const unreadCount = useNotificationStore((s) => s.unreadCount)
  const setPanelOpen = useNotificationStore((s) => s.setPanelOpen)

  // ── Resolve section groups from metadata ───────────────────────────
  // An entity item without an explicit permission needs the entity's read
  // permission; groups and sections left without items are not shown.
  const resolvedSections = useMemo(() => {
    return navigation.sections
      .map((section): ResolvedNavSection => ({
        title: section.title,
        icon: section.icon,
        groups: section.groups
          .map((group) => {
            const resolvedItems = group.items.flatMap((item) => {
              if (item.url) {
                if (!allowed(item.permission, permissions.has)) return []
                return [{ title: item.fallback, url: item.url, description: item.description, icon: getEntityIcon(item.entityKey) }]
              }
              const entity = entities.find((entity) => entity.key === item.entityKey)
              if (!allowed(item.permission ?? entityReadPermission(entity), permissions.has)) return []
              const title = entity?.presentation.plural ?? item.fallback
              const routePrefix = entity?.routePrefix ?? item.entityKey
              const entityType = entity?.type ?? "catalog"
              if (entityType === "register") return []
              const url = buildEntityUrlByRoute(routePrefix, entityType)
              return [{ title, url, description: item.description, icon: getEntityIcon(item.entityKey) }]
            })
            return { label: group.label, items: resolvedItems }
          })
          .filter((group) => group.items.length > 0),
      }))
      .filter((section) => section.groups.length > 0)
  }, [navigation, entities, permissions])

  // ── Dynamic extension items from metadata ─────────────────────────────
  // Entities not covered by the registered navigation appear here
  // automatically, for users with their read permission.
  const extensionItems = useMemo(() => {
    if (!metaLoaded) return []

    // Collect all entity keys already in the registered navigation
    const coveredKeys = new Set<string>()
    for (const section of navigation.sections) {
      for (const group of section.groups) {
        for (const item of group.items) {
          coveredKeys.add(item.entityKey)
        }
      }
    }

    return entities
      .filter((e) => !coveredKeys.has(e.key) && allowed(entityReadPermission(e), permissions.has))
      .flatMap((e) => {
        if (e.type !== "catalog" && e.type !== "document") return []
        const prefix = e.routePrefix ?? e.key
        const title = e.presentation?.plural ?? e.name
        const url = buildEntityUrlByRoute(prefix, e.type)
        return [{ title, url }]
      })
  }, [navigation, metaLoaded, entities, permissions])

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
    // On phones the sidebar is a drawer over the page: close it once the user picked where to go.
    if (isMobile) setOpenMobile(false)
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
      <Sidebar collapsible="icon" variant="inset" {...props}>
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

        <MobileSectionDrill currentPath={pathname} onItemClick={handleNavClick}>{(openSection) => (
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
                        isMobile
                          ? openSection(section)
                          : setActiveSection((prev) =>
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
        )}</MobileSectionDrill>

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
                  <DropdownMenuItem className="gap-2" onSelect={() => setPanelOpen(true)}>
                    <Bell className="h-4 w-4" />
                    <span className="flex-1">Уведомления</span>
                    {unreadCount > 0 && (
                      <Badge
                        variant="destructive"
                        className="ml-auto h-5 min-w-[1.25rem] justify-center px-1 text-[10px]"
                      >
                        {unreadCount > 99 ? "99+" : unreadCount}
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
        open={!!activeSection && !isMobile}
        onClose={() => setActiveSection(null)}
        onItemClick={handleNavClick}
        currentPath={pathname}
      />

      <UserPreferencesDialog open={prefsOpen} onOpenChange={setPrefsOpen} />
    </>
  )
}
