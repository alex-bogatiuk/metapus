"use client"

import { PermissionGate } from "@/components/auth/permission-gate"
import { SYSTEM_ADMIN } from "@/lib/system-permissions"
import { useEffect, useMemo } from "react"
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
  Plug,
  Pencil,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { ScrollArea } from "@/components/ui/scroll-area"
import { UsersRolesSection } from "@/components/settings/users-roles-section"
import { SecurityProfilesSection } from "@/components/settings/security-profiles-section"
import { AccessMatrix } from "@/components/settings/access-matrix"
import { SystemInfoSection } from "@/components/settings/system-info-section"
import { TenantOverviewSection } from "@/components/settings/tenant-overview-section"
import { IntegrationsSection } from "@/components/settings/integrations-section"
import { useSettingsStore } from "@/stores/useSettingsStore"
import { getAdminSections, type AdminSectionDef } from "@/lib/admin-registry"
import { createTabStateKey } from "@/stores/useTabStateStore"

const tabStateKeys = {
  "activeSection": createTabStateKey<AdminSection>("frontend/app/(main)/admin/page.tsx:activeSection"),
}


// Platform section ids, or the id of a section a configuration registered.
type AdminSection = "users" | "security" | "matrix" | "integrations" | "system" | "tenants" | (string & {})

type SectionItem = Omit<AdminSectionDef, "component">

interface ToolItem {
  title: string
  description: string
  icon: React.ElementType
  href: string
}

const platformSections: SectionItem[] = [
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
    description: "Мониторинг тенантов и версий схем",
    icon: Cloud,
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

const platformComponents: Record<string, React.ComponentType> = {
  users: UsersRolesSection,
  security: SecurityProfilesSection,
  matrix: AccessMatrix,
  integrations: IntegrationsSection,
  system: SystemInfoSection,
  tenants: TenantOverviewSection,
}

// Read at render: configuration sections register at start-up, and a page
// module may evaluate before the layout that runs the registration.
function adminSections(): { sections: SectionItem[]; sectionComponents: Record<string, React.ComponentType> } {
  const registered = getAdminSections()
  return {
    sections: [...platformSections, ...registered],
    sectionComponents: { ...platformComponents, ...Object.fromEntries(registered.map((s) => [s.id, s.component])) },
  }
}

function AdminPageContent() {
  const router = useRouter()
  const { sections, sectionComponents } = useMemo(() => adminSections(), [])
  const [activeSection, setActiveSection] =
    useTabState(tabStateKeys["activeSection"], "users")

  const fetchSettings = useSettingsStore((s) => s.fetchSettings)

  useEffect(() => {
    fetchSettings()
  }, [fetchSettings])

  const activeMeta = sections.find((s) => s.id === activeSection) ?? sections[0]
  const ActiveComponent = sectionComponents[activeMeta.id]

  useEffect(() => {
    if (!(activeSection in sectionComponents)) {
      setActiveSection("users")
    }
  }, [activeSection, sectionComponents, setActiveSection])

  return (
    <div className="flex h-full flex-col md:flex-row">
      {/* Left sidebar — section list */}
      <div className="hidden w-64 shrink-0 border-r bg-muted/30 md:block overflow-y-auto scrollbar-hide">
        <div className="px-4 py-4">
          <h1 className="text-sm font-semibold text-foreground">Управление</h1>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Администрирование системы
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
                    "flex items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 focus-visible:ring-offset-1",
                    isActive
                      ? "border border-border/65 bg-workspace-surface text-foreground shadow-control"
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
                  className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm text-muted-foreground hover:bg-background/60 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 focus-visible:ring-offset-1"
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
          className="w-full rounded-md border bg-workspace-surface px-3 py-2 text-sm"
        >
          {sections.map((s) => (
            <option key={s.id} value={s.id}>
              {s.title}
            </option>
          ))}
        </select>
      </div>

      {/* Right content — active section */}
      <ScrollArea data-material="workspace" className="min-h-0 w-full flex-1">
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

export default function AdminPage() {
  return (
    <PermissionGate permission={SYSTEM_ADMIN} subject="администрированию системы">
      <AdminPageContent />
    </PermissionGate>
  )
}
