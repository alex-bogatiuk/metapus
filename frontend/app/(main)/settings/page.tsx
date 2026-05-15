"use client"

import { useEffect } from "react"
import { ChevronRight, User, Coins } from "lucide-react"
import { cn } from "@/lib/utils"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useSettingsStore } from "@/stores/useSettingsStore"
import type { SettingsSection } from "@/stores/useSettingsStore"
import { useTabState } from "@/hooks/useTabState"
import { settingsSections } from "@/lib/settings-registry"
import type { SettingSectionDef } from "@/lib/settings-registry"
import { SettingsSectionRenderer } from "@/components/settings/settings-section-renderer"
import { PreferencesContent } from "@/components/settings/preferences-content"
import { FeeScheduleTable } from "@/components/catalogs/fee-schedule-table"

// Special pseudo-sections (not stored in sys_settings JSONB)
const PROFILE_ID = "__profile__" as const
const FEE_SCHEDULE_ID = "__fee_schedule__" as const
type ActiveSection = SettingsSection | typeof PROFILE_ID | typeof FEE_SCHEDULE_ID

const generalSections = settingsSections.filter((s) => s.category === "general")
const moduleSections = settingsSections.filter((s) => s.category === "module")

export default function SettingsPage() {
  const [activeSection, setActiveSection] =
    useTabState<ActiveSection>("activeSection", "numbering")

  const fetchSettings = useSettingsStore((s) => s.fetchSettings)

  useEffect(() => {
    fetchSettings()
  }, [fetchSettings])

  const activeDef = settingsSections.find((s) => s.id === activeSection) as SettingSectionDef | undefined

  return (
    <div className="flex h-full">
      {/* Left sidebar — section navigation */}
      <div className="hidden w-64 shrink-0 border-r bg-muted/30 md:block overflow-y-auto scrollbar-hide">
        <div className="px-4 py-4">
          <h1 className="text-sm font-semibold text-foreground">Настройки</h1>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Параметры системы
          </p>
        </div>

        {/* General sections */}
        <nav className="flex flex-col gap-0.5 px-2">
          {generalSections.map((section) => (
            <SidebarItem
              key={section.id}
              section={section}
              isActive={section.id === activeSection}
              onClick={() => setActiveSection(section.id)}
            />
          ))}
        </nav>

        {/* Module sections */}
        {moduleSections.length > 0 && (
          <div className="mt-4 border-t pt-4 px-2">
            <div className="px-3 mb-2">
              <span className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
                Модули
              </span>
            </div>
            <nav className="flex flex-col gap-0.5">
              {moduleSections.map((section) => (
                <SidebarItem
                  key={section.id}
                  section={section}
                  isActive={section.id === activeSection}
                  onClick={() => setActiveSection(section.id)}
                />
              ))}
            </nav>
          </div>
        )}

        {/* Crypto processing section */}
        <div className="mt-4 border-t pt-4 px-2">
          <div className="px-3 mb-2">
            <span className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
              Криптопроцессинг
            </span>
          </div>
          <nav className="flex flex-col gap-0.5">
            <button
              onClick={() => setActiveSection(FEE_SCHEDULE_ID)}
              className={cn(
                "flex items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors w-full",
                activeSection === FEE_SCHEDULE_ID
                  ? "bg-background text-foreground shadow-sm border border-border"
                  : "text-muted-foreground hover:bg-background/60 hover:text-foreground"
              )}
            >
              <Coins
                className={cn(
                  "h-4 w-4 shrink-0",
                  activeSection === FEE_SCHEDULE_ID ? "text-primary" : "text-muted-foreground"
                )}
              />
              <div className="min-w-0 flex-1">
                <div className="truncate font-medium">Тарифы комиссий</div>
                <div className="truncate text-[11px] text-muted-foreground">
                  Глобальные ставки по умолчанию
                </div>
              </div>
              {activeSection === FEE_SCHEDULE_ID && (
                <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              )}
            </button>
          </nav>
        </div>

        {/* Profile section */}
        <div className="mt-4 border-t pt-4 px-2 pb-4">
          <div className="px-3 mb-2">
            <span className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
              Персональные
            </span>
          </div>
          <button
            onClick={() => setActiveSection(PROFILE_ID)}
            className={cn(
              "flex items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors w-full",
              activeSection === PROFILE_ID
                ? "bg-background text-foreground shadow-sm border border-border"
                : "text-muted-foreground hover:bg-background/60 hover:text-foreground"
            )}
          >
            <User
              className={cn(
                "h-4 w-4 shrink-0",
                activeSection === PROFILE_ID ? "text-primary" : "text-muted-foreground"
              )}
            />
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">Профиль</div>
              <div className="truncate text-[11px] text-muted-foreground">
                Тема, формат, язык
              </div>
            </div>
            {activeSection === PROFILE_ID && (
              <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            )}
          </button>
        </div>
      </div>

      {/* Mobile section selector */}
      <div className="block w-full border-b bg-muted/30 px-4 py-2 md:hidden">
        <select
          value={activeSection}
          onChange={(e) => setActiveSection(e.target.value as ActiveSection)}
          className="w-full rounded-md border bg-background px-3 py-2 text-sm"
        >
          <optgroup label="Общие">
            {generalSections.map((s) => (
              <option key={s.id} value={s.id}>{s.title}</option>
            ))}
          </optgroup>
          <optgroup label="Модули">
            {moduleSections.map((s) => (
              <option key={s.id} value={s.id}>{s.title}</option>
            ))}
          </optgroup>
          <optgroup label="Криптопроцессинг">
            <option value={FEE_SCHEDULE_ID}>Тарифы комиссий</option>
          </optgroup>
          <optgroup label="Персональные">
            <option value={PROFILE_ID}>Профиль</option>
          </optgroup>
        </select>
      </div>

      {/* Right content area */}
      <ScrollArea className="flex-1">
        <div className="mx-auto max-w-4xl px-6 py-6">
          {activeSection === FEE_SCHEDULE_ID ? (
            <>
              <div className="mb-6">
                <h2 className="text-lg font-semibold text-foreground">Тарифы комиссий</h2>
                <p className="mt-0.5 text-sm text-muted-foreground">
                  Глобальные ставки по умолчанию. Применяются ко всем мерчантам,
                  если у мерчанта нет индивидуальных настроек.
                </p>
              </div>
              <FeeScheduleTable merchantId={null} />
            </>
          ) : activeSection === PROFILE_ID ? (
            <>
              <div className="mb-6">
                <h2 className="text-lg font-semibold text-foreground">Профиль</h2>
                <p className="mt-0.5 text-sm text-muted-foreground">
                  Тема интерфейса, формат дат и чисел
                </p>
              </div>
              <PreferencesContent />
            </>
          ) : activeDef ? (
            <>
              <div className="mb-6">
                <h2 className="text-lg font-semibold text-foreground">
                  {activeDef.title}
                </h2>
                <p className="mt-0.5 text-sm text-muted-foreground">
                  {activeDef.description}
                </p>
              </div>
              <SettingsSectionRenderer section={activeDef} />
            </>
          ) : null}
        </div>
      </ScrollArea>
    </div>
  )
}

// ── Sidebar item ────────────────────────────────────────────────────────

interface SidebarItemProps {
  section: SettingSectionDef
  isActive: boolean
  onClick: () => void
}

function SidebarItem({ section, isActive, onClick }: SidebarItemProps) {
  const Icon = section.icon
  return (
    <button
      onClick={onClick}
      className={cn(
        "flex items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors w-full",
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
}
