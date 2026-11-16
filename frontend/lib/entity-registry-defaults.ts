import type { JsonObject } from "@/lib/json-value"
/**
 * Entity Registry — Auto-discovery from backend metadata.
 *
 * This is the frontend counterpart of backend content.RegisterDefaults().
 * Instead of hardcoding entity registrations, it auto-discovers entities
 * from the /meta/entities endpoint and registers them in the UIRegistry.
 *
 * Custom columns are declared per entity for rich list views.
 * Custom components are registered separately per entity when needed.
 *
 * Usage:
 *   await registerFromMetadata()  // called once on app init
 *   entityRegistry.registerCatalog({ entityName: "Vehicle", ... })  // extension overlay
 */

import React from "react"
import { onScopeReset } from "@/lib/auth-scope"
import { entityRegistry } from "./entity-registry"
import type { AutoListColumn, EntityUIRegistration } from "./entity-registry"
import { cellBoolBadge, cellCode, cellDate, cellMono, cellMuted, cellName, cellTruncated } from "./entity-cells"
import { useMetadataStore } from "@/stores/useMetadataStore"

// ── Column Overlay Configs ──────────────────────────────────────────────
// Presentation overlay merged onto metadata list columns (render, width, default visible).
// Enum columns use `enumEntity` for dynamic label resolution in AutoList.

type ColumnOverlayConfig = Pick<EntityUIRegistration,
    "listColumns" | "defaultVisibleKeys" | "defaultFilterKeys" | "entityKey" | "formComponent" | "listComponent" | "detailContent" | "renderField">

/** EntityOverlay is a configuration's presentation for one metadata entity. */
export type EntityOverlay = ColumnOverlayConfig

const configurationOverlays: Record<string, EntityOverlay> = {}

/**
 * Registers a configuration's overlays by entity name: list columns, default
 * columns and filters, an optional form or list component, panels under the
 * metadata form and custom field editors. Call it from
 * the composition root (app/) before the first registerFromMetadata().
 */
export function registerEntityOverlays(overlays: Record<string, EntityOverlay>): void {
    Object.assign(configurationOverlays, overlays)
}

const columnOverlayConfigs: Record<string, ColumnOverlayConfig> = {
    IntegrationAccount: {
        entityKey: "integration_account",
        defaultVisibleKeys: ["code", "name", "integrationKind", "provider", "isActive"],
        defaultFilterKeys: ["integrationKind", "provider", "isActive"],
    },
    IntegrationPool: {
        entityKey: "integration_pool",
        defaultVisibleKeys: ["code", "name", "integrationKind", "provider", "isActive"],
        defaultFilterKeys: ["integrationKind", "provider", "isActive"],
    },
    Counterparty: {
        entityKey: "counterparty",
        defaultVisibleKeys: ["code", "name", "type", "legalForm", "inn", "phone"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, width: 100, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, width: 280, render: cellName },
            { key: "type", label: "Тип", sortable: true, width: 120, enumEntity: "Counterparty" },
            { key: "legalForm", label: "Правовая форма", sortable: true, width: 140, enumEntity: "Counterparty" },
            { key: "inn", label: "ИНН", sortable: false, width: 130, render: cellMono("inn") },
            { key: "phone", label: "Телефон", sortable: false, width: 140, render: cellMuted("phone") },
            { key: "email", label: "Email", sortable: false, width: 180, render: cellMuted("email") },
            { key: "contactPerson", label: "Контактное лицо", sortable: false, width: 180, render: cellMuted("contactPerson") },
            { key: "fullName", label: "Полное наименование", sortable: false, width: 250, render: cellTruncated("fullName", 250) },
        ],
    },
    Currency: {
        entityKey: "currency",
        defaultVisibleKeys: ["code", "name", "isoCode", "symbol", "isBase"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, render: cellName },
            { key: "isoCode", label: "ISO Код", sortable: true, render: (item) =>
                React.createElement("span", { className: "font-semibold text-foreground" }, String(item.isoCode ?? "") || "—") },
            { key: "isoNumericCode", label: "Цифровой код", sortable: true, render: cellMuted("isoNumericCode") },
            { key: "symbol", label: "Символ", sortable: true, render: (item) =>
                React.createElement("span", { className: "font-semibold" }, String(item.symbol ?? "") || "—") },
            { key: "isBase", label: "Базовая", sortable: true, render: cellBoolBadge("isBase") },
            { key: "country", label: "Страна", sortable: true, render: cellMuted("country") },
        ],
    },
    Organization: {
        entityKey: "organization",
        defaultVisibleKeys: ["code", "name", "fullName", "inn", "isDefault"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, render: cellName },
            { key: "fullName", label: "Полное наименование", sortable: false, render: cellTruncated("fullName", 250) },
            { key: "inn", label: "ИНН", sortable: false, render: cellMono("inn") },
            { key: "kpp", label: "КПП", sortable: false, width: 120, render: cellMono("kpp") },
            { key: "isDefault", label: "Основная", sortable: true, render: cellBoolBadge("isDefault") },
        ],
    },
    Contract: {
        entityKey: "contract",
        defaultVisibleKeys: ["code", "name", "type", "validFrom", "validTo"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, render: cellName },
            { key: "type", label: "Тип", sortable: true, enumEntity: "Contract" },
            { key: "validFrom", label: "Действует с", sortable: true, render: cellDate("validFrom") },
            { key: "validTo", label: "Действует по", sortable: true, render: cellDate("validTo") },
            { key: "paymentTermDays", label: "Срок оплаты (дн.)", sortable: true, render: cellMuted("paymentTermDays") },
        ],
    },

}

// ── Auto-Discovery from Backend Metadata ────────────────────────────────

const IntegrationList = React.lazy(() => import("@/components/integrations/integration-list"))
const IntegrationAccountForm = React.lazy(() => import("@/components/integrations/account-form"))
const IntegrationPoolForm = React.lazy(() => import("@/components/integrations/pool-form"))
const RateSourceForm = React.lazy(() => import("@/components/exchange-rates/rate-source-form"))

let registeredCount = 0
const discovered: EntityUIRegistration[] = []
onScopeReset(() => {
    discovered.forEach((reg) => entityRegistry.unregister(reg))
    discovered.length = 0
    registeredCount = 0
})

/**
 * Registers all entities from backend metadata into the UIRegistry.
 * Must be called AFTER useMetadataStore.fetch() completes.
 *
 * Entities already registered (e.g. by extensions) will NOT be overwritten.
 * Column overlays from columnOverlayConfigs are merged automatically.
 *
 * Idempotent: safe to call multiple times. Only re-processes if the
 * metadata store has new entities that weren't registered yet.
 */
export function registerFromMetadata(): void {
    const { entities } = useMetadataStore.getState()
    // Skip if metadata store hasn't loaded yet or nothing new
    if (entities.length === 0 || entities.length === registeredCount) return
    registeredCount = entities.length

    for (const entity of entities) {
        const type = entity.type as "catalog" | "document"
        const routePrefix = entity.routePrefix ?? entity.key

        // Skip if already registered by extension or custom code
        if (type === "catalog" && entityRegistry.getCatalog(entity.name)) continue
        if (type === "document" && entityRegistry.getDocument(entity.name)) continue

        const overlay = configurationOverlays[entity.name] ?? columnOverlayConfigs[entity.name]

        const reg = {
            entityType: type,
            entityName: entity.name,
            routePrefix,
            entityKey: entity.key,
            formComponent: overlay?.formComponent
                ?? (entity.name === "IntegrationAccount" ? IntegrationAccountForm
                    : entity.name === "IntegrationPool" ? IntegrationPoolForm
                    : entity.name === "RateSource" ? RateSourceForm
                    : undefined),
            listComponent: overlay?.listComponent ?? (entity.name === "IntegrationAccount" || entity.name === "IntegrationPool" ? IntegrationList : undefined),
            listColumns: overlay?.listColumns,
            defaultVisibleKeys: overlay?.defaultVisibleKeys,
            defaultFilterKeys: overlay?.defaultFilterKeys,
            detailContent: overlay?.detailContent,
            renderField: overlay?.renderField,
        }

        if (type === "catalog") {
            entityRegistry.registerCatalog(reg)
        } else {
            entityRegistry.registerDocument(reg)
        }
        discovered.push(reg)
    }
}
