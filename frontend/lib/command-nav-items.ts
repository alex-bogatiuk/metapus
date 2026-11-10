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

import { buildEntityUrlByRoute } from "@/lib/entity-url"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type { EntityMeta } from "@/types/metadata"
import type { LucideIcon } from "lucide-react"
import {
  Activity,
  AlertTriangle,
  ArrowUpRight,
  Bitcoin,
  Building2,
  Coins,
  CreditCard,
  FileText,
  HelpCircle,
  LayoutDashboard,
  List,
  Network,
  Plus,
  Receipt,
  RefreshCw,
  ScrollText,
  Settings,
  ShieldCheck,
  Store,
  TrendingUp,
  Users,
  Wallet,
} from "lucide-react"

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
  { id: "nav:exchange-rates", label: "Курсы валют", url: "/finance/exchange-rates", icon: Wallet, keywords: "курсы валют CoinGecko exchange rates", section: "navigate" },
  // Navigate
  { id: "nav:home",          label: "Главное",                url: "/",                          icon: LayoutDashboard, keywords: "главное дашборд dashboard home",           section: "navigate" },
  { id: "nav:counterparties", label: "Контрагенты",           url: "/catalogs/counterparties",   icon: Users,           keywords: "контрагенты поставщики покупатели counterparty",  section: "navigate" },
  { id: "nav:organizations", label: "Организации",            url: "/catalogs/organizations",    icon: Building2,       keywords: "организации фирмы organization",                  section: "navigate" },
  { id: "nav:currencies",    label: "Валюты",                 url: "/catalogs/currencies",       icon: Wallet,          keywords: "валюты currency",                                 section: "navigate" },
  { id: "nav:contracts",     label: "Договоры",               url: "/catalogs/contracts",        icon: FileText,        keywords: "договоры contract",                               section: "navigate" },

  // Crypto processing
  { id: "nav:crypto-invoices",     label: "Крипто-инвойсы",        url: "/documents/crypto-invoice",      icon: Receipt,       keywords: "крипто инвойс invoice платёж payment",                section: "navigate" },
  { id: "nav:crypto-payments",     label: "Крипто-платежи",        url: "/documents/crypto-payment",      icon: CreditCard,    keywords: "крипто платёж payment blockchain",                    section: "navigate" },
  { id: "nav:unidentified-receipts", label: "Невыясненные поступления", url: "/crypto/unidentified-receipts", icon: AlertTriangle, keywords: "крипто невыясненные поступления зачисление мерчанту dust", section: "navigate" },
  { id: "nav:crypto-withdrawals",  label: "Крипто-выводы",         url: "/documents/crypto-withdrawal",   icon: ArrowUpRight,  keywords: "крипто вывод withdrawal",                             section: "navigate" },
  { id: "nav:withdrawal-requests", label: "Заявки на вывод",       url: "/documents/withdrawal-requests", icon: FileText,      keywords: "крипто заявки вывод withdrawal requests approve одобрение", section: "navigate" },
  { id: "nav:treasury-withdrawals", label: "Казначейские выводы",  url: "/documents/treasury-withdrawals", icon: Bitcoin,       keywords: "крипто казначейские выводы treasury withdrawal reconciliation", section: "navigate" },
  { id: "nav:crypto-sweeps",       label: "Крипто-свипы",          url: "/documents/crypto-sweep",        icon: RefreshCw,     keywords: "крипто свип sweep консолидация",                      section: "navigate" },
  { id: "nav:crypto-resource-rentals", label: "Аренды TRON-ресурсов", url: "/documents/crypto-resource-rental", icon: RefreshCw, keywords: "аренды tron ресурсов energy bandwidth", section: "navigate" },
  { id: "nav:crypto-network-operations", label: "Сетевые криптооперации", url: "/documents/crypto-network-operation", icon: Network, keywords: "сетевые криптооперации blockchain транзакции", section: "navigate" },
  { id: "nav:crypto-provider-balance-operations", label: "Баланс провайдеров", url: "/documents/crypto-provider-balance-operation", icon: Activity, keywords: "баланс провайдеров провайдеры корректировка", section: "navigate" },
  { id: "nav:blockchain-networks", label: "Блокчейн-сети",         url: "/catalogs/blockchain-networks", icon: Bitcoin,        keywords: "блокчейн сети blockchain network tron ethereum",      section: "navigate" },
  { id: "nav:tokens",              label: "Токены",                url: "/catalogs/tokens",              icon: Coins,         keywords: "токены token usdt usdc crypto",                       section: "navigate" },
  { id: "nav:merchants",           label: "Мерчанты",              url: "/catalogs/merchants",           icon: Store,         keywords: "мерчанты merchant магазин",                           section: "navigate" },
  { id: "nav:wallets",             label: "Кошельки",              url: "/catalogs/wallets",             icon: Wallet,        keywords: "кошельки wallet адрес address",                       section: "navigate" },
  { id: "nav:crypto-providers",    label: "Крипто-провайдеры",     url: "/catalogs/crypto-provider-accounts", icon: Wallet,   keywords: "крипто провайдеры аккаунты provider accounts itrx",  section: "navigate" },
  { id: "nav:rate-sources",        label: "Источники курсов",       url: "/catalogs/rate-sources",        icon: TrendingUp,    keywords: "источники курсов rate sources coingecko binance",     section: "navigate" },

  // Create (quick actions)
  { id: "create:counterparty",   label: "Создать контрагента",    url: "/catalogs/counterparties/new",  icon: Plus,  keywords: "создать новый контрагент",              section: "create" },
  { id: "create:crypto-invoice",  label: "Создать крипто-инвойс", url: "/documents/crypto-invoice/new", icon: Plus,  keywords: "создать новый крипто инвойс invoice",   section: "create" },
  { id: "create:crypto-withdrawal", label: "Создать крипто-вывод", url: "/documents/crypto-withdrawal/new", icon: Plus, keywords: "создать новый крипто вывод withdrawal", section: "create" },
  { id: "create:merchant",       label: "Создать мерчанта",       url: "/catalogs/merchants/new",       icon: Plus,  keywords: "создать новый мерчант merchant",        section: "create" },
  { id: "create:wallet",         label: "Создать кошелек",        url: "/catalogs/wallets/new",         icon: Plus,  keywords: "создать новый кошелек wallet pool",     section: "create" },

  // Reports
  { id: "report:doc-journal",    label: "Журнал документов",     url: "/reports/document-journal", icon: ScrollText, keywords: "журнал документов document journal",                   section: "report" },

  // System
  { id: "sys:settings",     label: "Настройки",              url: "/settings",              icon: Settings,    keywords: "настройки settings preferences",        section: "system" },
  { id: "sys:admin",        label: "Управление",             url: "/admin",                 icon: ShieldCheck, keywords: "управление администрирование admin",    section: "system" },
  { id: "sys:automation-rules", label: "Правила автоматизации", url: "/admin/automation-rules", icon: List, keywords: "автоматизация правила события уведомления расписание automation rules", section: "system" },
  { id: "sys:event-log",    label: "Журнал событий",         url: "/admin/event-log",       icon: ScrollText,  keywords: "журнал событий лог event log",           section: "system" },
  { id: "sys:worker-jobs",  label: "Задачи воркера",         url: "/admin/worker-jobs",     icon: Activity,    keywords: "задачи воркер worker jobs фоновые background", section: "system" },
  { id: "sys:crypto-networks", label: "Криптосети", url: "/admin/crypto-networks", icon: Network, keywords: "криптовалютные крипто сети настройки runtime rpc signer itrx circuits policy", section: "system" },
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
export function getDynamicNavItems(entities: EntityMeta[] = useMetadataStore.getState().entities): CommandNavItem[] {
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
      icon: FileText, // fallback for extension entities
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
