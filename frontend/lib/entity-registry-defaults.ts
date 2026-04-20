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
import { entityRegistry } from "./entity-registry"
import type { AutoListColumn, EntityUIRegistration } from "./entity-registry"
import { useMetadataStore } from "@/stores/useMetadataStore"

// ── Shared cell renderers ───────────────────────────────────────────────
// Reusable render functions to avoid duplication across entity column sets.
// These are pure functions — no hooks, no state.

function cellCode(item: Record<string, unknown>) {
    return React.createElement("span", { className: "font-mono text-xs text-muted-foreground" }, String(item.code ?? ""))
}

function cellName(item: Record<string, unknown>) {
    return React.createElement("span", { className: "font-medium text-foreground" }, String(item.name ?? ""))
}

function cellMuted(key: string) {
    return (item: Record<string, unknown>) =>
        React.createElement("span", { className: "text-xs text-muted-foreground" }, String(item[key] ?? "") || "—")
}

function cellMono(key: string) {
    return (item: Record<string, unknown>) =>
        React.createElement("span", { className: "font-mono text-xs text-muted-foreground" }, String(item[key] ?? "") || "—")
}

function cellBoolBadge(key: string, yesLabel = "Да") {
    return (item: Record<string, unknown>) => {
        const value = item[key]
        if (value) {
            return React.createElement("span", {
                className: "inline-flex items-center rounded-md border px-2 py-0.5 text-[10px] font-semibold bg-primary text-primary-foreground",
            }, yesLabel)
        }
        return React.createElement("span", { className: "text-xs text-muted-foreground" }, "—")
    }
}

function cellDate(key: string) {
    return (item: Record<string, unknown>) => {
        const value = item[key]
        if (!value) return React.createElement("span", { className: "text-xs text-muted-foreground" }, "—")
        try {
            return React.createElement("span", { className: "text-xs text-muted-foreground" },
                new Date(String(value)).toLocaleDateString())
        } catch {
            return React.createElement("span", { className: "text-xs text-muted-foreground" }, String(value))
        }
    }
}

function cellTruncated(key: string, maxW = 200) {
    return (item: Record<string, unknown>) =>
        React.createElement("span", {
            className: `text-xs text-muted-foreground truncate block`,
            style: { maxWidth: maxW },
        }, String(item[key] ?? "") || "—")
}

// ── Column Overlay Configs ──────────────────────────────────────────────
// These define rich column sets for all built-in catalog entities.
// Enum columns use `enumEntity` for dynamic label resolution in AutoList.

type ColumnOverlayConfig = Pick<EntityUIRegistration,
    "listColumns" | "defaultVisibleKeys" | "defaultFilterKeys" | "entityKey">

const columnOverlayConfigs: Record<string, ColumnOverlayConfig> = {
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
    Nomenclature: {
        entityKey: "nomenclature",
        defaultVisibleKeys: ["code", "name", "article", "type", "barcode"],
        defaultFilterKeys: ["type"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, render: cellName },
            { key: "article", label: "Артикул", sortable: true, render: cellMono("article") },
            { key: "type", label: "Тип", sortable: true, enumEntity: "Nomenclature" },
            { key: "barcode", label: "Штрихкод", sortable: false, render: cellMono("barcode") },
            { key: "description", label: "Описание", sortable: false, render: cellTruncated("description", 200) },
            { key: "weight", label: "Вес", sortable: false, width: 80, render: cellMono("weight") },
            { key: "volume", label: "Объём", sortable: false, width: 80, render: cellMono("volume") },
        ],
    },
    Warehouse: {
        entityKey: "warehouse",
        defaultVisibleKeys: ["code", "name", "type", "isActive", "address"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, render: cellName },
            { key: "type", label: "Тип", sortable: true, enumEntity: "Warehouse" },
            { key: "isActive", label: "Активен", sortable: true, render: (item) => {
                const active = item.isActive
                return React.createElement("span", {
                    className: `inline-flex items-center rounded-md border px-2 py-0.5 text-[10px] font-semibold ${active ? "bg-primary text-primary-foreground" : "bg-secondary text-secondary-foreground"}`,
                }, active ? "Да" : "Нет")
            }},
            { key: "address", label: "Адрес", sortable: false, render: cellTruncated("address", 200) },
            { key: "description", label: "Описание", sortable: false, render: cellTruncated("description", 200) },
            { key: "allowNegativeStock", label: "Отрицательные остатки", sortable: false, width: 160, render: (item) => {
                const allow = item.allowNegativeStock
                return React.createElement("span", {
                    className: `inline-flex items-center rounded-md border px-2 py-0.5 text-[10px] font-semibold ${allow ? "bg-destructive text-destructive-foreground" : "bg-secondary text-secondary-foreground"}`,
                }, allow ? "Да" : "Нет")
            }},
            { key: "isDefault", label: "По умолчанию", sortable: true, width: 120, render: cellBoolBadge("isDefault") },
        ],
    },
    Unit: {
        entityKey: "unit",
        defaultVisibleKeys: ["code", "name", "type", "symbol", "isBase"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, render: cellName },
            { key: "type", label: "Тип", sortable: true, enumEntity: "Unit" },
            { key: "symbol", label: "Символ", sortable: true, render: (item) =>
                React.createElement("span", { className: "text-foreground font-semibold" }, String(item.symbol ?? "")) },
            { key: "internationalCode", label: "Код ОКЕИ", sortable: true, render: cellMuted("internationalCode") },
            { key: "isBase", label: "Базовая", sortable: true, render: cellBoolBadge("isBase") },
            { key: "conversionFactor", label: "Коэффициент", sortable: true, render: cellMono("conversionFactor") },
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
    VATRate: {
        entityKey: "vat_rate",
        defaultVisibleKeys: ["code", "name", "rate", "isTaxExempt"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, render: cellName },
            { key: "rate", label: "Ставка", sortable: true, render: (item) =>
                React.createElement("span", { className: "font-semibold text-foreground" }, `${item.rate}%`) },
            { key: "isTaxExempt", label: "Без НДС", sortable: true, render: (item) => {
                if (item.isTaxExempt) {
                    return React.createElement("span", {
                        className: "inline-flex items-center rounded-md border px-2 py-0.5 text-[10px] font-semibold bg-secondary text-secondary-foreground",
                    }, "Да")
                }
                return React.createElement("span", { className: "text-xs text-muted-foreground" }, "—")
            }},
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

let registeredCount = 0

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

        const overlay = columnOverlayConfigs[entity.name]

        const reg = {
            entityType: type,
            entityName: entity.name,
            routePrefix,
            entityKey: overlay?.entityKey,
            listColumns: overlay?.listColumns,
            defaultVisibleKeys: overlay?.defaultVisibleKeys,
            defaultFilterKeys: overlay?.defaultFilterKeys,
        }

        if (type === "catalog") {
            entityRegistry.registerCatalog(reg)
        } else {
            entityRegistry.registerDocument(reg)
        }
    }
}
