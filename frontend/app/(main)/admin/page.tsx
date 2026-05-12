"use client"

import { useEffect } from "react"
import { useRouter } from "next/navigation"
import { useTabState } from "@/hooks/useTabState"
import {
  Users,
  ShieldCheck,
  LayoutGrid,
  ChevronRight,
  Search,
  Trash2,
  Info,
  Cloud,
  Package,
  Plug,
  Pencil,
  Store,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { ScrollArea } from "@/components/ui/scroll-area"
import { UsersRolesSection } from "@/components/settings/users-roles-section"
import { SecurityProfilesSection } from "@/components/settings/security-profiles-section"
import { AccessMatrix } from "@/components/settings/access-matrix"
import { SystemInfoSection } from "@/components/settings/system-info-section"
import { ControlPlaneSection } from "@/components/settings/control-plane-section"
import { UpdatePageSection } from "@/components/settings/update-page-section"
import { IntegrationsSection } from "@/components/settings/integrations-section"
import { useSettingsStore } from "@/stores/useSettingsStore"
import { MerchantsSection } from "@/components/settings/merchants-section"

type AdminSection = "users" | "security" | "matrix" | "integrations" | "system" | "tenants" | "update" | "merchants"

interface SectionItem {
  id: AdminSection
  title: string
  description: string
  icon: React.ElementType
}

interface ToolItem {
  title: string
  description: string
  icon: React.ElementType
  href: string
}

const sections: SectionItem[] = [
  {
    id: "users",
    title: "Пользователи и роли",
    description: "Управление доступом и правами",
    icon: Users,
  },
  {
    id: "security",
    title: "Профили безопасности",
    description: "Доступ к данным, скрытие полей, условия",
    icon: ShieldCheck,
  },
  {
    id: "matrix",
    title: "Матрица доступа",
    description: "Сводная таблица RBAC-разрешений по ролям",
    icon: LayoutGrid,
  },
  {
    id: "integrations",
    title: "Интеграции и автоматизации",
    description: "Каналы доставки, правила автоматизации",
    icon: Plug,
  },
  {
    id: "system",
    title: "О системе",
    description: "Версия, сборка, диагностика",
    icon: Info,
  },
  {
    id: "tenants",
    title: "Тенанты",
    description: "Управление тенантами и группами версий",
    icon: Cloud,
  },
  {
    id: "update",
    title: "Обновление",
    description: "Обновление системы из Docker-образа",
    icon: Package,
  },
  {
    id: "merchants",
    title: "Мерчанты",
    description: "Пользователи и доступ к мерчантам",
    icon: Store,
  },
]

const tools: ToolItem[] = [
  {
    title: "Найти ссылки",
    description: "Входящие ссылки на объект",
    icon: Search,
    href: "/admin/find-references",
  },
  {
    title: "Удаление помеченных",
    description: "Физическое удаление объектов с пометкой",
    icon: Trash2,
    href: "/admin/marked-objects",
  },
  {
    title: "Групповое изменение",
    description: "Массовое изменение реквизитов объектов",
    icon: Pencil,
    href: "/admin/batch-modify",
  },
]

const sectionComponents: Record<AdminSection, React.ComponentType> = {
  users: UsersRolesSection,
  security: SecurityProfilesSection,
  matrix: AccessMatrix,
  integrations: IntegrationsSection,
  system: SystemInfoSection,
  tenants: ControlPlaneSection,
  update: UpdatePageSection,
  merchants: MerchantsSection,
}

export default function AdminPage() {
  const router = useRouter()
  const [activeSection, setActiveSection] =
    useTabState<AdminSection>("activeSection", "users")

  const fetchSettings = useSettingsStore((s) => s.fetchSettings)

  useEffect(() => {
    fetchSettings()
  }, [fetchSettings])

  const ActiveComponent = sectionComponents[activeSection]
  const activeMeta = sections.find((s) => s.id === activeSection)!

  return (
    <div className="flex h-full">
      {/* Left sidebar — section list */}
      <div className="hidden w-64 shrink-0 border-r bg-muted/30 md:block overflow-y-auto scrollbar-hide">
        <div className="px-4 py-4">
          <h1 className="text-sm font-semibold text-foreground">Администрирование</h1>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Управление системой
          </p>
        </div>
          <nav className="flex flex-col gap-0.5 px-2">
            {sections.map((section) => {
              const Icon = section.icon
              const isActive = section.id === activeSection
              return (
                <button
                  key={section.id}
                  onClick={() => setActiveSection(section.id)}
                  className={cn(
                    "flex items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors",
                    isActive
                      ? "bg-background text-foreground shadow-sm border border-border"
                      : "text-muted-foreground hover:bg-background/60 hover:text-foreground"
                  )}
                >
                  <Icon
                    className={cn(
                      "h-4 w-4 shrink-0",
                      isActive ? "text-primary" : "text-muted-foreground"
                    )}
                  />
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium">{section.title}</div>
                    <div className="truncate text-[11px] text-muted-foreground">
                      {section.description}
                    </div>
                  </div>
                  {isActive && (
                    <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  )}
                </button>
              )
            })}
          </nav>

          {/* Tools section */}
          <div className="mt-4 border-t pt-4 px-2 pb-4">
            <div className="px-3 mb-2">
              <span className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
                Инструменты
              </span>
            </div>
            {tools.map((tool) => {
              const ToolIcon = tool.icon
              return (
                <button
                  key={tool.href}
                  onClick={() => router.push(tool.href)}
                  className="flex items-center gap-3 rounded-md px-3 py-2 text-left text-sm text-muted-foreground hover:bg-background/60 hover:text-foreground transition-colors w-full"
                >
                  <ToolIcon className="h-4 w-4 shrink-0" />
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium">{tool.title}</div>
                    <div className="truncate text-[11px] text-muted-foreground">
                      {tool.description}
                    </div>
                  </div>
                </button>
              )
            })}
          </div>
      </div>

      {/* Mobile section selector (visible on small screens) */}
      <div className="block w-full border-b bg-muted/30 px-4 py-2 md:hidden">
        <select
          value={activeSection}
          onChange={(e) => setActiveSection(e.target.value as AdminSection)}
          className="w-full rounded-md border bg-background px-3 py-2 text-sm"
        >
          {sections.map((s) => (
            <option key={s.id} value={s.id}>
              {s.title}
            </option>
          ))}
        </select>
      </div>

      {/* Right content — active section */}
      <ScrollArea className="flex-1">
        <div className={cn("mx-auto px-6 py-6", activeSection === "matrix" ? "max-w-6xl" : "max-w-4xl")}>
          <div className="mb-6">
            <h2 className="text-lg font-semibold text-foreground">
              {activeMeta.title}
            </h2>
            <p className="mt-0.5 text-sm text-muted-foreground">
              {activeMeta.description}
            </p>
          </div>
          <ActiveComponent />
        </div>
      </ScrollArea>
    </div>
  )
}
