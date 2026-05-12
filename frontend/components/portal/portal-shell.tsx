"use client"

import { usePathname } from "next/navigation"
import Link from "next/link"
import {
  LayoutDashboard,
  Receipt,
  LogOut,
} from "lucide-react"

import {
  SidebarProvider,
  Sidebar,
  SidebarContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarFooter,
  SidebarInset,
} from "@/components/ui/sidebar"
import { MerchantSwitcher } from "./merchant-switcher"
import { useAuthStore } from "@/stores/useAuthStore"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import { LogoIcon } from "@/components/icons/logo"

const navItems = [
  { title: "Дашборд", href: "/portal", icon: LayoutDashboard },
  { title: "Инвойсы", href: "/portal/invoices", icon: Receipt },
]

export function PortalShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const user = useAuthStore((s) => s.user)
  const logout = useAuthStore((s) => s.logout)

  const initials = user
    ? (user.firstName?.[0] ?? "").toUpperCase() + (user.lastName?.[0] ?? "").toUpperCase()
    : "?"

  return (
    <SidebarProvider>
      <Sidebar variant="sidebar" collapsible="icon">
        <SidebarHeader className="p-4">
          <div className="flex items-center gap-2">
            <LogoIcon size={28} />
            <span className="text-sm font-semibold group-data-[collapsible=icon]:hidden">
              Metapus Portal
            </span>
          </div>
        </SidebarHeader>

        <SidebarContent className="px-2">
          <SidebarMenu>
            {navItems.map((item) => {
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
        </SidebarContent>

        <SidebarFooter className="p-2">
          <div className="flex items-center gap-2 px-2 py-1 group-data-[collapsible=icon]:justify-center">
            <Avatar className="size-7">
              <AvatarFallback className="text-xs bg-primary/10 text-primary">
                {initials}
              </AvatarFallback>
            </Avatar>
            <div className="flex-1 min-w-0 group-data-[collapsible=icon]:hidden">
              <p className="text-xs font-medium truncate">
                {user?.fullName || user?.email}
              </p>
              <p className="text-[10px] text-muted-foreground truncate">
                {user?.email}
              </p>
            </div>
          </div>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton
                tooltip="Выйти"
                onClick={() => logout()}
                className="text-muted-foreground hover:text-destructive"
              >
                <LogOut className="size-4" />
                <span>Выйти</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarFooter>
      </Sidebar>

      <SidebarInset>
        <header className="flex h-14 shrink-0 items-center gap-4 border-b px-4">
          <MerchantSwitcher />
        </header>
        <div className="flex flex-1 flex-col min-h-0 overflow-auto p-4 md:p-6">
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}
