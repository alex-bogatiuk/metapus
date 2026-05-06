// frontend/lib/command-nav-items.ts
/**
 * Static navigation index for Command Palette search.
 *
 * Combines sidebar navigation sections, flat items, and system items
 * into a flat searchable array. Also provides a function to build
 * a dynamic index from entity metadata for entities not in the static list.
 *
 * Each item has keywords for fuzzy matching (e.g. "Поступления товаров"
 * matches "пост", "товар", "поступления").
 */

import type { LucideIcon } from "lucide-react"
import {
  LayoutDashboard,
  ShoppingCart,
  TrendingUp,
  Warehouse,
  Package,
  Users,
  Building2,
  Wallet,
  Settings,
  ShieldCheck,
  ScrollText,
  HelpCircle,
  ClipboardCheck,
  ClipboardMinus,
  FileText,
  Plus,
  BarChart3,
  Bitcoin,
  Coins,
  Store,
  Receipt,
  CreditCard,
  ArrowUpRight,
  RefreshCw,
} from "lucide-react"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { buildEntityUrlByRoute } from "@/lib/entity-url"

// ── Types ───────────────────────────────────────────────────────────────

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
}

// ── Static items ────────────────────────────────────────────────────────

const _staticItems: CommandNavItem[] = [
  // Navigate
  { id: "nav:home",          label: "Главное",                url: "/",                          icon: LayoutDashboard, keywords: "главное дашборд dashboard home",           section: "navigate" },
  { id: "nav:purchases",     label: "Поступления товаров",    url: "/documents/goods-receipts",  icon: ClipboardCheck,  keywords: "поступления товаров закупки приход receipt purchase", section: "navigate" },
  { id: "nav:sales",         label: "Реализации товаров",     url: "/documents/goods-issues",    icon: ClipboardMinus,  keywords: "реализации товаров продажи расход issue sale",     section: "navigate" },
  { id: "nav:counterparties", label: "Контрагенты",           url: "/catalogs/counterparties",   icon: Users,           keywords: "контрагенты поставщики покупатели counterparty",  section: "navigate" },
  { id: "nav:nomenclature",  label: "Номенклатура",           url: "/catalogs/nomenclatures",    icon: Package,         keywords: "номенклатура товары продукция nomenclature",      section: "navigate" },
  { id: "nav:warehouses",    label: "Склады",                 url: "/catalogs/warehouses",       icon: Warehouse,       keywords: "склады warehouse",                                section: "navigate" },
  { id: "nav:organizations", label: "Организации",            url: "/catalogs/organizations",    icon: Building2,       keywords: "организации фирмы organization",                  section: "navigate" },
  { id: "nav:currencies",    label: "Валюты",                 url: "/catalogs/currencies",       icon: Wallet,          keywords: "валюты currency",                                 section: "navigate" },
  { id: "nav:units",         label: "Единицы измерения",      url: "/catalogs/units",            icon: Package,         keywords: "единицы измерения unit",                          section: "navigate" },
  { id: "nav:vat-rates",     label: "Ставки НДС",             url: "/catalogs/vat-rates",        icon: Package,         keywords: "ндс ставки vat rate",                             section: "navigate" },
  { id: "nav:contracts",     label: "Договоры",               url: "/catalogs/contracts",        icon: FileText,        keywords: "договоры contract",                               section: "navigate" },

  // Crypto processing
  { id: "nav:crypto-invoices",     label: "Крипто-инвойсы",        url: "/documents/crypto-invoice",      icon: Receipt,       keywords: "крипто инвойс invoice платёж payment",                section: "navigate" },
  { id: "nav:crypto-payments",     label: "Крипто-платежи",        url: "/documents/crypto-payment",      icon: CreditCard,    keywords: "крипто платёж payment blockchain",                    section: "navigate" },
  { id: "nav:crypto-withdrawals",  label: "Крипто-выводы",         url: "/documents/crypto-withdrawal",   icon: ArrowUpRight,  keywords: "крипто вывод withdrawal",                             section: "navigate" },
  { id: "nav:crypto-sweeps",       label: "Крипто-свипы",          url: "/documents/crypto-sweep",        icon: RefreshCw,     keywords: "крипто свип sweep консолидация",                      section: "navigate" },
  { id: "nav:blockchain-networks", label: "Блокчейн-сети",         url: "/catalogs/blockchain-networks", icon: Bitcoin,        keywords: "блокчейн сети blockchain network tron ethereum",      section: "navigate" },
  { id: "nav:tokens",              label: "Токены",                url: "/catalogs/tokens",              icon: Coins,         keywords: "токены token usdt usdc crypto",                       section: "navigate" },
  { id: "nav:merchants",           label: "Мерчанты",              url: "/catalogs/merchants",           icon: Store,         keywords: "мерчанты merchant магазин",                           section: "navigate" },
  { id: "nav:wallets",             label: "Кошельки",              url: "/catalogs/wallets",             icon: Wallet,        keywords: "кошельки wallet адрес address",                       section: "navigate" },

  // Create (quick actions)
  { id: "create:goods-receipt",  label: "Создать поступление",    url: "/documents/goods-receipts/new", icon: Plus,  keywords: "создать новый поступление receipt",   section: "create" },
  { id: "create:goods-issue",    label: "Создать реализацию",     url: "/documents/goods-issues/new",   icon: Plus,  keywords: "создать новый реализация issue sale",  section: "create" },
  { id: "create:counterparty",   label: "Создать контрагента",    url: "/catalogs/counterparties/new",  icon: Plus,  keywords: "создать новый контрагент",              section: "create" },
  { id: "create:nomenclature",   label: "Создать номенклатуру",   url: "/catalogs/nomenclatures/new",   icon: Plus,  keywords: "создать новый номенклатура товар",      section: "create" },
  { id: "create:crypto-invoice",  label: "Создать крипто-инвойс", url: "/documents/crypto-invoice/new", icon: Plus,  keywords: "создать новый крипто инвойс invoice",   section: "create" },
  { id: "create:crypto-withdrawal", label: "Создать крипто-вывод", url: "/documents/crypto-withdrawal/new", icon: Plus, keywords: "создать новый крипто вывод withdrawal", section: "create" },

  // Reports
  { id: "report:stock-balance",  label: "Остатки товаров",       url: "/reports/stock-balance",    icon: BarChart3, keywords: "остатки товаров отчёт баланс stock balance report",    section: "report" },
  { id: "report:stock-turnover", label: "Оборотная ведомость",   url: "/reports/stock-turnover",   icon: BarChart3, keywords: "оборотная ведомость отчёт turnover report",            section: "report" },
  { id: "report:doc-journal",    label: "Журнал документов",     url: "/reports/document-journal", icon: ScrollText, keywords: "журнал документов document journal",                   section: "report" },

  // System
  { id: "sys:settings",     label: "Настройки",              url: "/settings",              icon: Settings,    keywords: "настройки settings preferences",        section: "system" },
  { id: "sys:admin",        label: "Администрирование",      url: "/admin",                 icon: ShieldCheck, keywords: "администрирование admin",               section: "system" },
  { id: "sys:event-log",    label: "Журнал событий",         url: "/admin/event-log",       icon: ScrollText,  keywords: "журнал событий лог event log",           section: "system" },
  { id: "sys:help",         label: "Помощь",                 url: "/help",                  icon: HelpCircle,  keywords: "помощь help",                           section: "system" },
]

/**
 * Get a copy of the static navigation items.
 * Always returns a fresh copy to satisfy the defensive-copy invariant.
 */
export function getStaticNavItems(): CommandNavItem[] {
  return [..._staticItems]
}

/**
 * Build dynamic navigation items from metadata store for entities
 * not covered by the static list.
 */
export function getDynamicNavItems(): CommandNavItem[] {
  const { entities } = useMetadataStore.getState()
  if (entities.length === 0) return []

  // Collect URLs already in static list
  const coveredUrls = new Set(_staticItems.map((i) => i.url))
  const result: CommandNavItem[] = []

  for (const entity of entities) {
    const routePrefix = entity.routePrefix ?? entity.key
    const type = entity.type as "catalog" | "document"
    const url = buildEntityUrlByRoute(routePrefix, type)

    if (coveredUrls.has(url)) continue

    const label = entity.presentation?.plural ?? entity.name
    result.push({
      id: `nav:${entity.key}`,
      label,
      url,
      icon: Package, // fallback for extension entities
      keywords: `${label.toLowerCase()} ${entity.key.replace(/_/g, " ")}`,
      section: "navigate",
    })
  }

  return result
}

// ── Section display labels ──────────────────────────────────────────────

export const COMMAND_SECTION_LABELS: Record<CommandNavItem["section"], string> = {
  navigate: "Перейти",
  create: "Создать",
  report: "Отчёты",
  system: "Система",
}
