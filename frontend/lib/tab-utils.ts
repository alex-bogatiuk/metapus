import { getNavigation } from "@/lib/navigation"
import { useMetadataStore } from "@/stores/useMetadataStore"

/** Static section labels — navigation groups that are NOT entities */
const sectionLabels: Record<string, string> = {
  catalogs: "Справочники",
  documents: "Документы",
  finance: "Деньги",
  "exchange-rates": "Курсы валют",
  company: "Компания",
  settings: "Настройки",
  admin: "Управление",
  "security-profiles": "Профили безопасности",
  "find-references": "Найти ссылки",
  "marked-objects": "Удаление помеченных",
  "batch-modify": "Групповое изменение",
  "event-log": "Журнал событий",
  "automation-rules": "Правила автоматизации",
  crm: "CRM",
  help: "Помощь",
  related: "Связанные документы",
  movements: "Движения документа",
}

/** Static fallback for entity route segments when metadata has not loaded yet */
const staticEntityPluralLabels: Record<string, string> = {
  counterparties: "Контрагенты",
  counterparty: "Контрагенты",
  contracts: "Договоры",
  contract: "Договоры",
  currencies: "Валюты",
  currency: "Валюты",
  organizations: "Организации",
  organization: "Организации",
  "rate-sources": "Источники курсов",
  "rate-source": "Источники курсов",
}

/**
 * Adds a configuration's tab labels: `sections` for route segments that are
 * not entities, `entities` for entity route segments before metadata loads.
 */
export function registerRouteLabels(labels: { sections?: Record<string, string>; entities?: Record<string, string> }): void {
  Object.assign(sectionLabels, labels.sections)
  Object.assign(staticEntityPluralLabels, labels.entities)
}

/**
 * Resolve a URL segment to a human-readable label.
 * Priority: metadata store (entity routePrefix) → static entity labels → static section label → undefined.
 *
 * NOTE: calls useMetadataStore.getState() directly (not a hook).
 * Safe in event handlers and utility functions.
 * If metadata hasn't loaded yet, gracefully falls back to static labels.
 */
export function resolveSegmentLabel(segment: string): string | undefined {
  // 1. Try entity metadata (e.g. "currencies" → "Валюты")
  const entityLabel = entityLabelForSegment(segment)
  if (entityLabel) return entityLabel

  // 2. Static entity plural fallback (available synchronously before metadata fetch)
  const staticEntity = staticEntityPluralLabels[segment]
  if (staticEntity) return staticEntity

  // 3. Static section label
  return sectionLabels[segment]
}

function entityLabelForSegment(segment: string): string | undefined {
  const metaState = useMetadataStore.getState()
  const entity =
    metaState.getEntityByRoute(segment) ??
    (segment.endsWith("s") ? metaState.getEntityByRoute(segment.slice(0, -1)) : undefined)
  return entity?.presentation.plural
}

/**
 * The label the navigation gives a page: a system link, a palette entry or a
 * sidebar item with a URL, matched exactly. A sidebar entity item matches the
 * list route its key spells (integration_pool → /catalogs/integration-pools),
 * so the label is known before metadata loads.
 */
export function navigationTitleForUrl(pathname: string): string | undefined {
  const { sections, systemLinks, commands } = getNavigation()
  const link = systemLinks.find((l) => l.url === pathname)
  if (link) return link.title
  const command = commands.find((c) => c.url === pathname && c.section !== "create")
  if (command) return command.label
  const [, area, segment, ...rest] = pathname.split("/")
  const listRoute = (area === "catalogs" || area === "documents") && segment && rest.length === 0
  for (const section of sections) {
    for (const group of section.groups) {
      for (const item of group.items) {
        if (item.url ? item.url === pathname : listRoute && entityRouteSegment(item.entityKey) === segment) return item.fallback
      }
    }
  }
  return undefined
}

function entityRouteSegment(entityKey: string): string {
  const prefix = entityKey.replace(/_/g, "-")
  return prefix.endsWith("s") ? prefix : `${prefix}s`
}

/**
 * Resolve a pathname to a human-readable tab title.
 * Used when opening tabs automatically (RouteSync, open-by-URL).
 *
 * Titles are temporary — useTabTitle hook updates them once entity data loads.
 */
export function resolveTitleFromUrl(pathname: string): string {
  if (pathname === "/") return "Главное"
  const segments = pathname.split("/").filter(Boolean)
  const lastSegment = segments[segments.length - 1]

  // /…/new → "New (ParentLabel)" using entity metadata
  if (lastSegment === "new" && segments.length >= 2) {
    const parentSegment = segments[segments.length - 2]
    const entity = useMetadataStore.getState().getEntityByRoute(parentSegment)
    if (entity) return entity.presentation.new ?? `Новый (${entity.presentation.plural})`
    const sectionLabel = sectionLabels[parentSegment]
    if (sectionLabel) return `Новый (${sectionLabel})`
    return "Новый"
  }

  // A page the navigation names; entity metadata, once loaded, names it as the sidebar does
  if (!entityLabelForSegment(lastSegment)) {
    const navTitle = navigationTitleForUrl(pathname)
    if (navTitle) return navTitle
  }

  // Known segment → list page title
  const label = resolveSegmentLabel(lastSegment)
  if (label) {
    // For sub-pages like /documents/goods-receipts/{uuid}/movements,
    // include the parent entity type so the tab is identifiable before data loads.
    if ((lastSegment === "movements" || lastSegment === "related") && segments.length >= 3) {
      const entitySegment = segments[segments.length - 3]
      const entityLabel = resolveSegmentLabel(entitySegment)
      if (entityLabel) {
        const prefix = lastSegment === "movements" ? "Движения" : "Связанные"
        return `${prefix}: ${entityLabel}…`
      }
    }
    return label
  }

  // UUID ([id] page) — temporary title until useTabTitle updates it
  if (segments.length >= 2) {
    const parentSegment = segments[segments.length - 2]
    const parentPath = `/${segments.slice(0, -1).join("/")}`
    const parentLabel = entityLabelForSegment(parentSegment) ?? navigationTitleForUrl(parentPath) ?? resolveSegmentLabel(parentSegment)
    if (parentLabel) return `${parentLabel}…`
  }

  return lastSegment
}
