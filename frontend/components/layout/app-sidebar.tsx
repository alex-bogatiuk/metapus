// frontend/components/layout/app-sidebar.tsx
"use client"

import type { LucideIcon } from "lucide-react"
import {
  Activity,
  BarChart3,
  Bell,
  BookOpen,
  ChevronRight,
  FileText,
  HelpCircle,
  LayoutDashboard,
  LogOut,
  MoreHorizontal,
  Network,
  Puzzle,
  ScrollText,
  Settings,
  ShieldCheck,
  User,
} from "lucide-react"
import { usePathname, useRouter } from "next/navigation"
import { useMemo, useState } from "react"

import { LogoIcon } from "@/components/icons/logo"
import {
  SectionPanel,
  type NavSection,
  type ResolvedNavSection,
} from "@/components/layout/section-panel"
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
} from "@/components/ui/sidebar"
import { usePermissions } from "@/hooks/use-permissions"
import { api } from "@/lib/api"
import { buildEntityUrlByRoute } from "@/lib/entity-url"
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

/**
 * Grouped sections — clicking a section button opens a Sheet panel
 * with items organized by group (Документы, Справочники, Отчёты).
 */
const navSections: NavSection[] = [
  {
    title: "Документы",
    icon: FileText,
    groups: [
      {
        label: "Приём платежей",
        items: [
          { entityKey: "crypto_invoice", fallback: "Крипто-инвойсы" },
          { entityKey: "crypto_payment", fallback: "Крипто-платежи" },
        ],
      },
      {
        label: "Поступления на кошельки",
        items: [
          { entityKey: "crypto_custody_receipt", fallback: "Поступления на кошельки" },
          { entityKey: "unidentified_receipts", fallback: "Невыясненные поступления", url: "/crypto/unidentified-receipts" },
          { entityKey: "crypto_merchant_credit", fallback: "Ручные зачисления мерчантам" },
          { entityKey: "crypto_platform_funding", fallback: "Пополнения платформы" },
          { entityKey: "wallet_fundings", fallback: "Пополнения кошельков", url: "/crypto/wallet-fundings" },
        ],
      },
      {
        label: "Выплаты и казна",
        items: [
          { entityKey: "withdrawal_request", fallback: "Заявки на вывод" },
          { entityKey: "crypto_withdrawal", fallback: "Крипто-выводы" },
          { entityKey: "treasury_withdrawal", fallback: "Казначейские выводы" },
          { entityKey: "crypto_sweep", fallback: "Крипто-свипы" },
        ],
      },
      {
        label: "Технические операции",
        items: [
          { entityKey: "crypto_resource_rental", fallback: "Аренды TRON-ресурсов" },
          { entityKey: "crypto_network_operation", fallback: "Сетевые криптооперации" },
          { entityKey: "crypto_provider_balance_operation", fallback: "Баланс провайдеров" },
        ],
      },
      {
        label: "Финансовый контроль",
        items: [
          { entityKey: "crypto_adjustment", fallback: "Криптокорректировки", url: "/documents/crypto-adjustments" },
        ],
      },
    ],
  },
  {
    title: "Справочники",
    icon: BookOpen,
    groups: [
      {
        label: "Бизнес и контрагенты",
        items: [
          { entityKey: "merchant", fallback: "Мерчанты" },
          { entityKey: "counterparty", fallback: "Контрагенты" },
          { entityKey: "contract", fallback: "Договоры" },
          { entityKey: "organization", fallback: "Организации" },
        ],
      },
      {
        label: "Блокчейн и сеть",
        items: [
          { entityKey: "blockchain_network", fallback: "Блокчейн-сети" },
          { entityKey: "token", fallback: "Токены" },
          { entityKey: "wallet", fallback: "Кошельки" },
          { entityKey: "crypto_provider_account", fallback: "Крипто-провайдеры" },
        ],
      },
      { label: "Интеграции", items: [{ entityKey: "integration_account", fallback: "Аккаунты интеграций" }, { entityKey: "integration_pool", fallback: "Пулы аккаунтов" }, { entityKey: "crypto_rpc_endpoint", fallback: "Подключения RPC" }] },
      {
        label: "Финансы и валюты",
        items: [
          { entityKey: "currency", fallback: "Валюты" },
          { entityKey: "exchange-rate", fallback: "Курсы валют", url: "/finance/exchange-rates" },
          { entityKey: "rate_source", fallback: "Источники курсов" },
        ],
      },
    ],
  },
  {
    title: "Отчёты",
    icon: BarChart3,
    groups: [
      {
        label: "Реестры и аналитика",
        items: [
          { entityKey: "report-document-journal", fallback: "Журнал документов", url: "/reports/document-journal" },
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
  { title: "Настройки", url: "/settings", icon: Settings },
  { title: "Управление", url: "/admin", icon: ShieldCheck },
  { title: "Журнал событий", url: "/admin/event-log", icon: ScrollText },
  { title: "Задачи воркера", url: "/admin/worker-jobs", icon: Activity },
  { title: "Криптосети", url: "/admin/crypto-networks", icon: Network },
  { title: "Помощь", url: "/help", icon: HelpCircle },
]



export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const pathname = usePathname()
  const router = useRouter()
  const openTab = useTabsStore((s) => s.openTab)
  const permissions = usePermissions()
  const canReadRates = permissions.has("register:exchange_rate:read")
  const canReadAdjustments = permissions.has("crypto_reconciliation:read")
  const user = useAuthStore((s) => s.user)
  const logout = useAuthStore((s) => s.logout)
  const getEntity = useMetadataStore((s) => s.getEntity)
  const entities = useMetadataStore((s) => s.entities)
  const metaLoaded = useMetadataStore((s) => s.loaded)
  const [prefsOpen, setPrefsOpen] = useState(false)
  const [activeSection, setActiveSection] = useState<ResolvedNavSection | null>(null)
  const unreadCount = useNotificationStore((s) => s.unreadCount)
  const setPanelOpen = useNotificationStore((s) => s.setPanelOpen)

  // ── Resolve section groups from metadata ───────────────────────────
  const resolvedSections = useMemo(() => {
    return navSections
      .filter((s) => s.groups.some((g) => g.items.length > 0))
      .map((section): ResolvedNavSection => ({
        title: section.title,
        icon: section.icon,
        groups: section.groups.map((group) => {
          const resolvedItems = group.items.filter(item =>
            (item.entityKey !== "exchange-rate" || canReadRates) &&
            (item.entityKey !== "crypto_adjustment" || canReadAdjustments)
          ).flatMap((item) => {
            if (item.url) {
              return [{ title: item.fallback, url: item.url, description: item.description }]
            }
            const entity = entities.find((entity) => entity.key === item.entityKey)
            const title = entity?.presentation.plural ?? item.fallback
            const routePrefix = entity?.routePrefix ?? item.entityKey
            const entityType = entity?.type ?? "catalog"
            if (entityType === "register") return []
            const url = buildEntityUrlByRoute(routePrefix, entityType)
            return [{ title, url, description: item.description }]
          })
          return { label: group.label, items: resolvedItems }
        }),
      }))
  }, [entities, canReadRates, canReadAdjustments])

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
      .flatMap((e) => {
        if (e.type === "register") return []
        const prefix = e.routePrefix ?? e.key
        const title = e.presentation?.plural ?? e.name
        const url = buildEntityUrlByRoute(prefix, e.type)
        return [{ title, url }]
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
        open={!!activeSection}
        onClose={() => setActiveSection(null)}
        onItemClick={handleNavClick}
        currentPath={pathname}
      />

      <UserPreferencesDialog open={prefsOpen} onOpenChange={setPrefsOpen} />
    </>
  )
}
