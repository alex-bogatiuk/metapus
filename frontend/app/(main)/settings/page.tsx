"use client"

import { useEffect } from "react"
import { useRouter } from "next/navigation"
import { useTabState } from "@/hooks/useTabState"
import {
  Building2,
  Calculator,
  Users,
  ShieldCheck,
  LayoutGrid,
  ChevronRight,
  Search,
  Trash2,
  Info,
  Cloud,
  Package,
  Gauge,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { ScrollArea } from "@/components/ui/scroll-area"
import { OrganizationSection } from "@/components/settings/organization-section"
import { AccountingSection } from "@/components/settings/accounting-section"
import { UsersRolesSection } from "@/components/settings/users-roles-section"
import { SecurityProfilesSection } from "@/components/settings/security-profiles-section"
import { AccessMatrix } from "@/components/settings/access-matrix"
import { SystemInfoSection } from "@/components/settings/system-info-section"
import { ControlPlaneSection } from "@/components/settings/control-plane-section"
import { UpdatePageSection } from "@/components/settings/update-page-section"
import { PerformanceSection } from "@/components/settings/performance-section"
import { useSettingsStore } from "@/stores/useSettingsStore"

type SettingsSection = "organization" | "accounting" | "performance" | "users" | "security" | "matrix" | "system" | "tenants" | "update"

interface SectionItem {
  id: SettingsSection
  title: string
  description: string
  icon: React.ElementType
}

const sections: SectionItem[] = [
  {
    id: "organization",
    title: "Организация",
    description: "Реквизиты, адрес, контакты компании",
    icon: Building2,
  },
  {
    id: "accounting",
    title: "Учёт и параметры",
    description: "Валюта, налоги, учётная политика",
    icon: Calculator,
  },
  {
    id: "performance",
    title: "Производительность",
    description: "Параллелизм и лимиты обработки",
    icon: Gauge,
  },
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
]

const sectionComponents: Record<SettingsSection, React.ComponentType> = {
  organization: OrganizationSection,
  accounting: AccountingSection,
  performance: PerformanceSection,
  users: UsersRolesSection,
  security: SecurityProfilesSection,
  matrix: AccessMatrix,
  system: SystemInfoSection,
  tenants: ControlPlaneSection,
  update: UpdatePageSection,
}

export default function SettingsPage() {
  const router = useRouter()
  const [activeSection, setActiveSection] =
    useTabState<SettingsSection>("activeSection", "organization")

  const fetchSettings = useSettingsStore((s) => s.fetchSettings)

  useEffect(() => {
    fetchSettings()
  }, [fetchSettings])

  const ActiveComponent = sectionComponents[activeSection]
  const activeMeta = sections.find((s) => s.id === activeSection)!

  return (
    <div className="flex h-full">
      {/* Left sidebar — section list */}
      <div className="hidden w-64 shrink-0 border-r bg-muted/30 md:block">
        <div className="px-4 py-4">
          <h1 className="text-sm font-semibold text-foreground">Настройки</h1>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Параметры системы
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

        {/* Processing tools section */}
        <div className="mt-4 border-t pt-4 px-2">
          <div className="px-3 mb-2">
            <span className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
              Обработки
            </span>
          </div>
          <button
            onClick={() => router.push("/settings/find-references")}
            className="flex items-center gap-3 rounded-md px-3 py-2 text-left text-sm text-muted-foreground hover:bg-background/60 hover:text-foreground transition-colors w-full"
          >
            <Search className="h-4 w-4 shrink-0" />
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">Найти ссылки</div>
              <div className="truncate text-[11px] text-muted-foreground">
                Поиск ссылок на объект
              </div>
            </div>
          </button>
          <button
            onClick={() => router.push("/settings/marked-objects")}
            className="flex items-center gap-3 rounded-md px-3 py-2 text-left text-sm text-muted-foreground hover:bg-background/60 hover:text-foreground transition-colors w-full"
          >
            <Trash2 className="h-4 w-4 shrink-0" />
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">Удаление помеченных</div>
              <div className="truncate text-[11px] text-muted-foreground">
                Безопасное удаление объектов
              </div>
            </div>
          </button>
        </div>
      </div>

      {/* Mobile section selector (visible on small screens) */}
      <div className="block w-full border-b bg-muted/30 px-4 py-2 md:hidden">
        <select
          value={activeSection}
          onChange={(e) => setActiveSection(e.target.value as SettingsSection)}
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
        <div className={cn("mx-auto px-6 py-6", activeSection === "matrix" ? "max-w-6xl" : "max-w-3xl")}>
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
