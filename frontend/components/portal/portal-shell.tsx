"use client"

import { useEffect, useState } from "react"
import { usePathname, useRouter } from "next/navigation"
import Link from "next/link"
import {
  LayoutDashboard,
  Receipt,
  Link2,
  KeyRound,
  Settings,
  LogOut,
  ChevronsUpDown,
  Check,
  User,
  MoreHorizontal,
  Palette,
} from "lucide-react"

import {
  SidebarProvider,
  Sidebar,
  SidebarContent,
  SidebarHeader,
  SidebarFooter,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarInset,
  SidebarRail,
  SidebarTrigger,
  useSidebar,
} from "@/components/ui/sidebar"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Separator } from "@/components/ui/separator"
import { useAuthStore } from "@/stores/useAuthStore"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { usePortalStore } from "@/stores/usePortalStore"
import { LogoIcon } from "@/components/icons/logo"
import { UserPreferencesDialog } from "@/components/shared/user-preferences-dialog"
import { api } from "@/lib/api"

// ── Navigation structure ────────────────────────────────────────────────

interface NavItem {
  title: string
  href: string
  icon: React.ComponentType<{ className?: string }>
}

const mainItems: NavItem[] = [
  { title: "Дашборд", href: "/portal", icon: LayoutDashboard },
  { title: "Инвойсы", href: "/portal/invoices", icon: Receipt },
  { title: "Платёжные ссылки", href: "/portal/payment-links", icon: Link2 },
]

const developerItems: NavItem[] = [
  { title: "API-ключи", href: "/portal/developers/keys", icon: KeyRound },
]

const settingsItems: NavItem[] = [
  { title: "Настройки", href: "/portal/settings", icon: Settings },
]

// ── MerchantSwitcher (sidebar-07 TeamSwitcher pattern) ───────────────────

function MerchantSwitcherSidebar() {
  const { activeMerchantId, merchants, setActiveMerchant } = usePortalStore()
  const { isMobile } = useSidebar()
  const activeMerchant = merchants.find((m) => m.id === activeMerchantId)

  if (!activeMerchant) return null

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size="lg"
              className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
            >
              <div className="flex aspect-square size-8 shrink-0 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                <LogoIcon size={32} />
              </div>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-semibold">{activeMerchant.name}</span>
                <span className="truncate text-xs text-muted-foreground">{activeMerchant.code}</span>
              </div>
              <ChevronsUpDown className="ml-auto" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="w-[--radix-dropdown-menu-trigger-width] min-w-56 rounded-lg"
            align="start"
            side={isMobile ? "bottom" : "right"}
            sideOffset={4}
          >
            <DropdownMenuLabel className="text-xs text-muted-foreground">
              Мерчанты
            </DropdownMenuLabel>
            {merchants.map((m) => (
              <DropdownMenuItem
                key={m.id}
                onClick={() => setActiveMerchant(m.id)}
                className="gap-2 p-2"
              >
                <div className="flex size-6 items-center justify-center rounded-md border">
                  {activeMerchantId === m.id ? (
                    <Check className="size-3.5 shrink-0" />
                  ) : (
                    <LogoIcon size={14} />
                  )}
                </div>
                {m.name}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  )
}

// ── NavSection ──────────────────────────────────────────────────────────

function NavSection({ items, label }: { items: NavItem[]; label?: string }) {
  const pathname = usePathname()

  return (
    <SidebarGroup>
      {label && <SidebarGroupLabel>{label}</SidebarGroupLabel>}
      <SidebarMenu>
        {items.map((item) => {
          const isActive =
            item.href === "/portal"
              ? pathname === "/portal"
              : pathname.startsWith(item.href)
          return (
            <SidebarMenuItem key={item.href}>
              <SidebarMenuButton
                asChild
                isActive={isActive}
                tooltip={item.title}
              >
                <Link href={item.href}>
                  <item.icon className="size-4" />
                  <span>{item.title}</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          )
        })}
      </SidebarMenu>
    </SidebarGroup>
  )
}

// ── NavUser (sidebar-07 pattern — DropdownMenu on user block) ───────────

function NavUser() {
  const user = useAuthStore((s) => s.user)
  const logout = useAuthStore((s) => s.logout)
  const router = useRouter()
  const [prefsOpen, setPrefsOpen] = useState(false)

  const initials = user
    ? (user.firstName?.[0] ?? "").toUpperCase() + (user.lastName?.[0] ?? "").toUpperCase()
    : "?"

  const handleLogout = async () => {
    try {
      await api.auth.logout()
    } catch {
      // ignore — still clear local state
    }
    logout()
    router.replace("/login")
  }

  return (
    <>
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
              <DropdownMenuLabel className="p-0 font-normal">
                <div className="flex items-center gap-2 px-1 py-1.5 text-left text-sm">
                  <div className="flex aspect-square size-8 items-center justify-center rounded-full bg-muted border border-border">
                    <span className="text-xs font-medium">{initials}</span>
                  </div>
                  <div className="grid flex-1 text-left text-sm leading-tight">
                    <span className="truncate font-medium">{user?.fullName || "Пользователь"}</span>
                    <span className="truncate text-xs text-muted-foreground">{user?.email}</span>
                  </div>
                </div>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem className="gap-2" onSelect={() => setPrefsOpen(true)}>
                <Palette className="h-4 w-4" />
                Настройки интерфейса
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                className="gap-2 text-destructive focus:text-destructive"
                onClick={handleLogout}
              >
                <LogOut className="h-4 w-4" />
                Выйти
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </SidebarMenuItem>
      </SidebarMenu>

      <UserPreferencesDialog open={prefsOpen} onOpenChange={setPrefsOpen} />
    </>
  )
}

// ── PortalShell ─────────────────────────────────────────────────────────

export function PortalShell({ children }: { children: React.ReactNode }) {
  // Load entity metadata for ReferenceField/ReferencePickerDialog
  const fetchMeta = useMetadataStore((s) => s.fetch)
  useEffect(() => { fetchMeta() }, [fetchMeta])

  return (
    <SidebarProvider>
      <Sidebar collapsible="icon">
        <SidebarHeader>
          <MerchantSwitcherSidebar />
        </SidebarHeader>

        <SidebarContent>
          <NavSection items={mainItems} />
          <NavSection items={developerItems} label="Разработчикам" />
          <NavSection items={settingsItems} />
        </SidebarContent>

        <SidebarFooter>
          <NavUser />
        </SidebarFooter>

        <SidebarRail />
      </Sidebar>

      <SidebarInset>
        <header className="flex h-14 shrink-0 items-center gap-2 border-b px-4 transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-12">
          <SidebarTrigger className="-ml-1 h-7 w-7" />
          <Separator orientation="vertical" className="mr-2 data-[orientation=vertical]:h-4" />
          <span className="text-sm font-medium text-muted-foreground">Merchant Portal</span>
        </header>
        <div className="flex flex-1 flex-col min-h-0 overflow-auto p-4 md:p-6">
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}
