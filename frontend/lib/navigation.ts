// frontend/lib/navigation.ts
/**
 * Navigation registry: the sidebar sections, the system links and the command
 * palette's static entries. The platform declares its own navigation here; a
 * configuration adds its parts with registerNavigation at start-up.
 */

import type { LucideIcon } from "lucide-react"
import {
  Activity,
  BarChart3,
  BookOpen,
  FileText,
  HelpCircle,
  Plus,
  ScrollText,
  Settings,
  ShieldCheck,
} from "lucide-react"

// ── Types ────────────────────────────────────────────────────────────────

export interface NavSectionItem {
  /** Stable entity key matching backend metadata.Key, e.g. "counterparty" */
  entityKey: string
  fallback: string
  description?: string
  /** Explicit URL overrides entityKey resolution */
  url?: string
  /** Permission the user needs to see the item */
  permission?: string
}

export interface NavSectionGroup {
  label: string
  items: NavSectionItem[]
}

export interface NavSection {
  title: string
  icon: LucideIcon
  groups: NavSectionGroup[]
}

/** A flat sidebar link (Настройки, Журнал событий, …). */
export interface NavLink {
  title: string
  url: string
  icon: LucideIcon
  permission?: string
}

export interface CommandNavItem {
  /** Unique id for cmdk */
  id: string
  /** Display label */
  label: string
  /** URL to navigate to */
  url: string
  /** Lucide icon */
  icon: LucideIcon
  /** Search keywords (lowercase, space-separated) for fuzzy matching */
  keywords: string
  /** Section group label in the palette */
  section: "navigate" | "create" | "system" | "report"
  /** Permission the user needs to see the item */
  permission?: string
}

/** A palette entry whose icon defaults to the route icon of its URL. */
export type CommandNavEntry = Omit<CommandNavItem, "icon"> & { icon?: LucideIcon }

/**
 * Groups added to a sidebar section. A group whose label already exists
 * extends that group: its items go first with `prepend`, last otherwise. A new
 * group goes after the group labelled `after`, or at the end.
 */
export interface NavSectionContribution {
  section: string
  /** Icon of the section when the contribution creates it */
  icon?: LucideIcon
  groups: (NavSectionGroup & { after?: string; prepend?: boolean })[]
}

export interface NavigationContribution {
  sections?: NavSectionContribution[]
  /** System links; one with `before` goes in front of the link with that URL. */
  systemLinks?: (NavLink & { before?: string })[]
  commands?: CommandNavEntry[]
}

export interface Navigation {
  sections: NavSection[]
  systemLinks: NavLink[]
  commands: CommandNavEntry[]
}

// ── Platform navigation ──────────────────────────────────────────────────

const platformNavigation: Navigation = {
  sections: [
    { title: "Документы", icon: FileText, groups: [] },
    {
      title: "Справочники",
      icon: BookOpen,
      groups: [
        {
          label: "Бизнес и контрагенты",
          items: [
            { entityKey: "counterparty", fallback: "Контрагенты" },
            { entityKey: "contract", fallback: "Договоры" },
            { entityKey: "organization", fallback: "Организации" },
          ],
        },
        {
          label: "Интеграции",
          items: [
            { entityKey: "integration_account", fallback: "Аккаунты интеграций" },
            { entityKey: "integration_pool", fallback: "Пулы аккаунтов" },
          ],
        },
        {
          label: "Финансы и валюты",
          items: [
            { entityKey: "currency", fallback: "Валюты" },
            { entityKey: "exchange-rate", fallback: "Курсы валют", url: "/finance/exchange-rates", permission: "register:exchange_rate:read" },
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
  ],
  systemLinks: [
    { title: "Настройки", url: "/settings", icon: Settings },
    { title: "Управление", url: "/admin", icon: ShieldCheck },
    { title: "Журнал событий", url: "/admin/event-log", icon: ScrollText },
    { title: "Задачи воркера", url: "/admin/worker-jobs", icon: Activity },
    { title: "Помощь", url: "/help", icon: HelpCircle },
  ],
  commands: [
    // Navigate
    { id: "nav:exchange-rates", label: "Курсы валют", url: "/finance/exchange-rates", keywords: "курсы валют CoinGecko exchange rates", section: "navigate", permission: "register:exchange_rate:read" },
    { id: "nav:home", label: "Главное", url: "/", keywords: "главное дашборд dashboard home", section: "navigate" },
    { id: "nav:counterparties", label: "Контрагенты", url: "/catalogs/counterparties", keywords: "контрагенты поставщики покупатели counterparty", section: "navigate" },
    { id: "nav:organizations", label: "Организации", url: "/catalogs/organizations", keywords: "организации фирмы organization", section: "navigate" },
    { id: "nav:currencies", label: "Валюты", url: "/catalogs/currencies", keywords: "валюты currency", section: "navigate" },
    { id: "nav:contracts", label: "Договоры", url: "/catalogs/contracts", keywords: "договоры contract", section: "navigate" },
    { id: "nav:rate-sources", label: "Источники курсов", url: "/catalogs/rate-sources", keywords: "источники курсов rate sources coingecko binance", section: "navigate" },

    // Create (quick actions)
    { id: "create:counterparty", label: "Создать контрагента", url: "/catalogs/counterparties/new", icon: Plus, keywords: "создать новый контрагент", section: "create" },

    // Reports
    { id: "report:doc-journal", label: "Журнал документов", url: "/reports/document-journal", keywords: "журнал документов document journal", section: "report" },

    // System
    { id: "sys:settings", label: "Настройки", url: "/settings", keywords: "настройки settings preferences", section: "system" },
    { id: "sys:admin", label: "Управление", url: "/admin", keywords: "управление администрирование admin", section: "system" },
    { id: "sys:automation-rules", label: "Правила автоматизации", url: "/admin/automation-rules", keywords: "автоматизация правила события уведомления расписание automation rules", section: "system" },
    { id: "sys:event-log", label: "Журнал событий", url: "/admin/event-log", keywords: "журнал событий лог event log", section: "system" },
    { id: "sys:worker-jobs", label: "Задачи воркера", url: "/admin/worker-jobs", keywords: "задачи воркер worker jobs фоновые background", section: "system" },
    { id: "sys:help", label: "Помощь", url: "/help", keywords: "помощь help", section: "system" },
  ],
}

// ── Registry ─────────────────────────────────────────────────────────────

const contributions: NavigationContribution[] = []
let cached: Navigation | null = null

/** Adds a configuration's navigation. Call at start-up, before the UI renders. */
export function registerNavigation(contribution: NavigationContribution): void {
  contributions.push(contribution)
  cached = null
}

function mergeSection(sections: NavSection[], contribution: NavSectionContribution): void {
  let section = sections.find((s) => s.title === contribution.section)
  if (!section) {
    section = { title: contribution.section, icon: contribution.icon ?? FileText, groups: [] }
    sections.push(section)
  }
  for (const { after, prepend, ...group } of contribution.groups) {
    const existing = section.groups.find((g) => g.label === group.label)
    if (existing) {
      existing.items = prepend ? [...group.items, ...existing.items] : [...existing.items, ...group.items]
      continue
    }
    const anchor = after === undefined ? -1 : section.groups.findIndex((g) => g.label === after)
    const copy = { label: group.label, items: [...group.items] }
    if (anchor < 0) section.groups.push(copy)
    else section.groups.splice(anchor + 1, 0, copy)
  }
}

/** The platform navigation with every registered contribution applied. */
export function getNavigation(): Navigation {
  if (cached) return cached
  const sections = platformNavigation.sections.map((s) => ({ ...s, groups: s.groups.map((g) => ({ ...g, items: [...g.items] })) }))
  const systemLinks = [...platformNavigation.systemLinks]
  const commands = [...platformNavigation.commands]
  for (const contribution of contributions) {
    for (const section of contribution.sections ?? []) mergeSection(sections, section)
    for (const { before, ...link } of contribution.systemLinks ?? []) {
      const anchor = before === undefined ? -1 : systemLinks.findIndex((l) => l.url === before)
      if (anchor < 0) systemLinks.push(link)
      else systemLinks.splice(anchor, 0, link)
    }
    commands.push(...(contribution.commands ?? []))
  }
  cached = { sections, systemLinks, commands }
  return cached
}
