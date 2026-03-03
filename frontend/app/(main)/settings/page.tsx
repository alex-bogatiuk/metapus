"use client"

import { useState } from "react"
import {
  Building2,
  Calculator,
  Users,
  Palette,
  ChevronRight,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { ScrollArea } from "@/components/ui/scroll-area"
import { OrganizationSection } from "@/components/settings/organization-section"
import { AccountingSection } from "@/components/settings/accounting-section"
import { UsersRolesSection } from "@/components/settings/users-roles-section"

type SettingsSection = "organization" | "accounting" | "users"

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
    id: "users",
    title: "Пользователи и роли",
    description: "Управление доступом и правами",
    icon: Users,
  },
]

const sectionComponents: Record<SettingsSection, React.ComponentType> = {
  organization: OrganizationSection,
  accounting: AccountingSection,
  users: UsersRolesSection,
}

export default function SettingsPage() {
  const [activeSection, setActiveSection] =
    useState<SettingsSection>("organization")

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
        <div className="mx-auto max-w-3xl px-6 py-6">
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
