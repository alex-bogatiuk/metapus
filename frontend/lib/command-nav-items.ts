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
import { getRouteIcon } from "@/lib/entity-icon"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type { EntityMeta } from "@/types/metadata"
import type { LucideIcon } from "lucide-react"
import { Plus } from "lucide-react"

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
  { id: "nav:exchange-rates", label: "Курсы валют", url: "/finance/exchange-rates", icon: getRouteIcon("/finance/exchange-rates"), keywords: "курсы валют CoinGecko exchange rates", section: "navigate" },
  // Navigate
  { id: "nav:home",          label: "Главное",                url: "/",                          icon: getRouteIcon("/"), keywords: "главное дашборд dashboard home",           section: "navigate" },
  { id: "nav:counterparties", label: "Контрагенты",           url: "/catalogs/counterparties",   icon: getRouteIcon("/catalogs/counterparties"),           keywords: "контрагенты поставщики покупатели counterparty",  section: "navigate" },
  { id: "nav:organizations", label: "Организации",            url: "/catalogs/organizations",    icon: getRouteIcon("/catalogs/organizations"),       keywords: "организации фирмы organization",                  section: "navigate" },
  { id: "nav:currencies",    label: "Валюты",                 url: "/catalogs/currencies",       icon: getRouteIcon("/catalogs/currencies"),          keywords: "валюты currency",                                 section: "navigate" },
  { id: "nav:contracts",     label: "Договоры",               url: "/catalogs/contracts",        icon: getRouteIcon("/catalogs/contracts"),        keywords: "договоры contract",                               section: "navigate" },

  // Crypto processing
  { id: "nav:crypto-invoices",     label: "Крипто-инвойсы",        url: "/documents/crypto-invoice",      icon: getRouteIcon("/documents/crypto-invoice"),       keywords: "крипто инвойс invoice платёж payment",                section: "navigate" },
  { id: "nav:crypto-payments",     label: "Крипто-платежи",        url: "/documents/crypto-payment",      icon: getRouteIcon("/documents/crypto-payment"),    keywords: "крипто платёж payment blockchain",                    section: "navigate" },
  { id: "nav:unidentified-receipts", label: "Невыясненные поступления", url: "/crypto/unidentified-receipts", icon: getRouteIcon("/crypto/unidentified-receipts"), keywords: "крипто невыясненные поступления зачисление мерчанту dust", section: "navigate" },
  { id: "nav:custody-receipts",    label: "Поступления на кошельки", url: "/documents/crypto-custody-receipt", icon: getRouteIcon("/documents/crypto-custody-receipt"), keywords: "крипто поступления receipt custody кошелёк", section: "navigate" },
  { id: "nav:merchant-credits",    label: "Ручные зачисления мерчантам", url: "/documents/crypto-merchant-credit", icon: getRouteIcon("/documents/crypto-merchant-credit"), keywords: "крипто ручное зачисление мерчанту credit", section: "navigate" },
  { id: "nav:platform-fundings",   label: "Пополнения платформы",  url: "/documents/crypto-platform-funding", icon: getRouteIcon("/documents/crypto-platform-funding"), keywords: "крипто пополнение платформы funding невыясненные", section: "navigate" },
  { id: "nav:wallet-fundings",     label: "Пополнения кошельков",  url: "/crypto/wallet-fundings",         icon: getRouteIcon("/crypto/wallet-fundings"), keywords: "крипто пополнение кошелька funding hot fee", section: "navigate" },
  { id: "nav:crypto-withdrawals",  label: "Крипто-выводы",         url: "/documents/crypto-withdrawal",   icon: getRouteIcon("/documents/crypto-withdrawal"),  keywords: "крипто вывод withdrawal",                             section: "navigate" },
  { id: "nav:withdrawal-requests", label: "Заявки на вывод",       url: "/documents/withdrawal-requests", icon: getRouteIcon("/documents/withdrawal-requests"),      keywords: "крипто заявки вывод withdrawal requests approve одобрение", section: "navigate" },
  { id: "nav:treasury-withdrawals", label: "Казначейские выводы",  url: "/documents/treasury-withdrawals", icon: getRouteIcon("/documents/treasury-withdrawals"),       keywords: "крипто казначейские выводы treasury withdrawal reconciliation", section: "navigate" },
  { id: "nav:crypto-sweeps",       label: "Крипто-свипы",          url: "/documents/crypto-sweep",        icon: getRouteIcon("/documents/crypto-sweep"),     keywords: "крипто свип sweep консолидация",                      section: "navigate" },
  { id: "nav:crypto-resource-rentals", label: "Аренды TRON-ресурсов", url: "/documents/crypto-resource-rental", icon: getRouteIcon("/documents/crypto-resource-rental"), keywords: "аренды tron ресурсов energy bandwidth", section: "navigate" },
  { id: "nav:crypto-network-operations", label: "Сетевые криптооперации", url: "/documents/crypto-network-operation", icon: getRouteIcon("/documents/crypto-network-operation"), keywords: "сетевые криптооперации blockchain транзакции", section: "navigate" },
  { id: "nav:crypto-provider-balance-operations", label: "Баланс провайдеров", url: "/documents/crypto-provider-balance-operation", icon: getRouteIcon("/documents/crypto-provider-balance-operation"), keywords: "баланс провайдеров провайдеры корректировка", section: "navigate" },
  { id: "nav:blockchain-networks", label: "Блокчейн-сети",         url: "/catalogs/blockchain-networks", icon: getRouteIcon("/catalogs/blockchain-networks"),        keywords: "блокчейн сети blockchain network tron ethereum",      section: "navigate" },
  { id: "nav:tokens",              label: "Токены",                url: "/catalogs/tokens",              icon: getRouteIcon("/catalogs/tokens"),         keywords: "токены token usdt usdc crypto",                       section: "navigate" },
  { id: "nav:merchants",           label: "Мерчанты",              url: "/catalogs/merchants",           icon: getRouteIcon("/catalogs/merchants"),         keywords: "мерчанты merchant магазин",                           section: "navigate" },
  { id: "nav:wallets",             label: "Кошельки",              url: "/catalogs/wallets",             icon: getRouteIcon("/catalogs/wallets"),        keywords: "кошельки wallet адрес address",                       section: "navigate" },
  { id: "nav:crypto-providers",    label: "Крипто-провайдеры",     url: "/catalogs/crypto-provider-accounts", icon: getRouteIcon("/catalogs/crypto-provider-accounts"),   keywords: "крипто провайдеры аккаунты provider accounts itrx",  section: "navigate" },
  { id: "nav:rate-sources",        label: "Источники курсов",       url: "/catalogs/rate-sources",        icon: getRouteIcon("/catalogs/rate-sources"),    keywords: "источники курсов rate sources coingecko binance",     section: "navigate" },

  // Create (quick actions)
  { id: "create:counterparty",   label: "Создать контрагента",    url: "/catalogs/counterparties/new",  icon: Plus,  keywords: "создать новый контрагент",              section: "create" },
  { id: "create:crypto-invoice",  label: "Создать крипто-инвойс", url: "/documents/crypto-invoice/new", icon: Plus,  keywords: "создать новый крипто инвойс invoice",   section: "create" },
  { id: "create:crypto-withdrawal", label: "Создать крипто-вывод", url: "/documents/crypto-withdrawal/new", icon: Plus, keywords: "создать новый крипто вывод withdrawal", section: "create" },
  { id: "create:merchant",       label: "Создать мерчанта",       url: "/catalogs/merchants/new",       icon: Plus,  keywords: "создать новый мерчант merchant",        section: "create" },
  { id: "create:wallet",         label: "Создать кошелек",        url: "/catalogs/wallets/new",         icon: Plus,  keywords: "создать новый кошелек wallet pool",     section: "create" },

  // Reports
  { id: "report:doc-journal",    label: "Журнал документов",     url: "/reports/document-journal", icon: getRouteIcon("/reports/document-journal"), keywords: "журнал документов document journal",                   section: "report" },

  // System
  { id: "sys:settings",     label: "Настройки",              url: "/settings",              icon: getRouteIcon("/settings"),    keywords: "настройки settings preferences",        section: "system" },
  { id: "sys:admin",        label: "Управление",             url: "/admin",                 icon: getRouteIcon("/admin"), keywords: "управление администрирование admin",    section: "system" },
  { id: "sys:automation-rules", label: "Правила автоматизации", url: "/admin/automation-rules", icon: getRouteIcon("/admin/automation-rules"), keywords: "автоматизация правила события уведомления расписание automation rules", section: "system" },
  { id: "sys:event-log",    label: "Журнал событий",         url: "/admin/event-log",       icon: getRouteIcon("/admin/event-log"),  keywords: "журнал событий лог event log",           section: "system" },
  { id: "sys:worker-jobs",  label: "Задачи воркера",         url: "/admin/worker-jobs",     icon: getRouteIcon("/admin/worker-jobs"),    keywords: "задачи воркер worker jobs фоновые background", section: "system" },
  { id: "sys:crypto-networks", label: "Криптосети", url: "/admin/crypto-networks", icon: getRouteIcon("/admin/crypto-networks"), keywords: "криптовалютные крипто сети настройки runtime rpc signer itrx circuits policy", section: "system" },
  { id: "sys:help",         label: "Помощь",                 url: "/help",                  icon: getRouteIcon("/help"),  keywords: "помощь help",                           section: "system" },
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
      icon: getRouteIcon(url), // known entities share their icon; others fall back to a section icon
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
