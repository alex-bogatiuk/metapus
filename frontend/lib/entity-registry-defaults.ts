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
import { getStatusColorClass } from "./status-colors"
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
    const Cell = (item: Record<string, unknown>) =>
        React.createElement("span", { className: "text-xs text-muted-foreground" }, String(item[key] ?? "") || "—")
    Cell.displayName = `CellMuted(${key})`
    return Cell
}

/**
 * Renders a resolved reference field. Backend returns nested RefDisplay objects:
 *   { merchantId: "uuid", merchant: { id: "uuid", name: "Test Merchant" } }
 * This renderer accesses item[refKey].name.
 */
function cellRef(refKey: string) {
    const Cell = (item: Record<string, unknown>) => {
        const ref = item[refKey] as { name?: string } | null | undefined
        return React.createElement("span", { className: "text-xs text-muted-foreground" }, ref?.name || "—")
    }
    Cell.displayName = `CellRef(${refKey})`
    return Cell
}

function cellMono(key: string) {
    const Cell = (item: Record<string, unknown>) =>
        React.createElement("span", { className: "font-mono text-xs text-muted-foreground" }, String(item[key] ?? "") || "—")
    Cell.displayName = `CellMono(${key})`
    return Cell
}

function cellBoolBadge(key: string, yesLabel = "Да") {
    const Cell = (item: Record<string, unknown>) => {
        const value = item[key]
        if (value) {
            return React.createElement("span", {
                className: "inline-flex items-center rounded-md border px-2 py-0.5 text-[10px] font-semibold bg-primary text-primary-foreground",
            }, yesLabel)
        }
        return React.createElement("span", { className: "text-xs text-muted-foreground" }, "—")
    }
    Cell.displayName = `CellBoolBadge(${key})`
    return Cell
}

function cellDate(key: string) {
    const Cell = (item: Record<string, unknown>) => {
        const value = item[key]
        if (!value) return React.createElement("span", { className: "text-xs text-muted-foreground" }, "—")
        try {
            return React.createElement("span", { className: "text-xs text-muted-foreground" },
                new Date(String(value)).toLocaleDateString())
        } catch {
            return React.createElement("span", { className: "text-xs text-muted-foreground" }, String(value))
        }
    }
    Cell.displayName = `CellDate(${key})`
    return Cell
}

function cellTruncated(key: string, maxW = 200) {
    const Cell = (item: Record<string, unknown>) =>
        React.createElement("span", {
            className: `text-xs text-muted-foreground truncate block`,
            style: { maxWidth: maxW },
        }, String(item[key] ?? "") || "—")
    Cell.displayName = `CellTruncated(${key})`
    return Cell
}

// Crypto-specific cell renderers

function cellHashTruncated(key: string) {
    const Cell = (item: Record<string, unknown>) => {
        const hash = String(item[key] ?? "")
        if (!hash || hash === "—") return React.createElement("span", { className: "text-xs text-muted-foreground" }, "—")
        const truncated = hash.length > 16 ? `${hash.slice(0, 8)}…${hash.slice(-6)}` : hash
        return React.createElement("span", {
            className: "font-mono text-xs text-muted-foreground cursor-help",
            title: hash,
        }, truncated)
    }
    Cell.displayName = `CellHash(${key})`
    return Cell
}

function cellStatusBadge(key: string) {
    const Cell = (item: Record<string, unknown>) => {
        const status = String(item[key] ?? "")
        if (!status) return React.createElement("span", { className: "text-xs text-muted-foreground" }, "—")
        const color = getStatusColorClass(status)
        return React.createElement("span", {
            className: `inline-flex items-center rounded-md border px-2 py-0.5 text-[10px] font-semibold ${color}`,
        }, status)
    }
    Cell.displayName = `CellStatus(${key})`
    return Cell
}

function cellAmount(key: string) {
    const Cell = (item: Record<string, unknown>) => {
        const value = String(item[key] ?? "0")
        return React.createElement("span", {
            className: "font-mono text-xs text-right block tabular-nums text-foreground",
        }, value)
    }
    Cell.displayName = `CellAmount(${key})`
    return Cell
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

    // ── Crypto Processing Entities ──────────────────────────────────────

    BlockchainNetwork: {
        entityKey: "blockchain_network",
        defaultVisibleKeys: ["code", "name", "chainId", "isTestnet", "isActive"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, width: 140, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, width: 200, render: cellName },
            { key: "chainId", label: "Chain ID", sortable: true, width: 100, render: cellMono("chainId") },
            { key: "isTestnet", label: "Testnet", sortable: true, width: 100, render: cellBoolBadge("isTestnet", "Test") },
            { key: "isActive", label: "Активна", sortable: true, width: 100, render: cellBoolBadge("isActive") },
            { key: "confirmationsNeeded", label: "Подтверждения", sortable: true, width: 130, render: cellMono("confirmationsNeeded") },
            { key: "blockExplorerUrl", label: "Explorer", sortable: false, width: 200, render: cellTruncated("blockExplorerUrl", 200) },
        ],
    },
    Token: {
        entityKey: "token",
        defaultVisibleKeys: ["code", "name", "symbol", "contractAddress", "isActive"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, width: 100, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, width: 180, render: cellName },
            { key: "symbol", label: "Символ", sortable: true, width: 80, render: (item) =>
                React.createElement("span", { className: "font-semibold text-foreground" }, String(item.symbol ?? "")) },
            { key: "network", label: "Сеть", sortable: false, width: 140, render: cellRef("network") },
            { key: "contractAddress", label: "Контракт", sortable: false, width: 160, render: cellHashTruncated("contractAddress") },
            { key: "decimalPlaces", label: "Decimals", sortable: true, width: 90, render: cellMono("decimalPlaces") },
            { key: "isActive", label: "Активен", sortable: true, width: 100, render: cellBoolBadge("isActive") },
        ],
    },
    Merchant: {
        entityKey: "merchant",
        defaultVisibleKeys: ["code", "name", "kybStatusName", "webhookUrl"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, width: 120, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, width: 220, render: cellName },
            { key: "kybStatusName", label: "KYB", sortable: true, width: 120, render: cellStatusBadge("kybStatusName") },
            { key: "webhookUrl", label: "Webhook", sortable: false, width: 200, render: cellTruncated("webhookUrl", 200) },
            { key: "feePercent", label: "Комиссия %", sortable: true, width: 110, render: cellMono("feePercent") },
        ],
    },
    Wallet: {
        entityKey: "wallet",
        defaultVisibleKeys: ["address", "network", "statusName", "tierName"],
        listColumns: [
            { key: "address", label: "Адрес", sortable: false, width: 180, render: cellHashTruncated("address") },
            { key: "network", label: "Сеть", sortable: false, width: 130, render: cellRef("network") },
            { key: "statusName", label: "Статус", sortable: true, width: 120, render: cellStatusBadge("statusName") },
            { key: "tierName", label: "Tier", sortable: true, width: 100, render: cellMuted("tierName") },
            { key: "merchant", label: "Мерчант", sortable: false, width: 150, render: cellRef("merchant") },
        ],
    },
    CryptoInvoice: {
        entityKey: "crypto_invoice",
        defaultVisibleKeys: ["number", "date", "statusName", "merchant", "expectedAmount", "receivedAmount"],
        defaultFilterKeys: ["status"],
        listColumns: [
            { key: "number", label: "Номер", sortable: true, width: 130, render: cellMono("number") },
            { key: "date", label: "Дата", sortable: true, width: 110, render: cellDate("date") },
            { key: "statusName", label: "Статус", sortable: true, width: 130, render: cellStatusBadge("statusName") },
            { key: "merchant", label: "Мерчант", sortable: false, width: 150, render: cellRef("merchant") },
            { key: "token", label: "Токен", sortable: false, width: 80, render: cellRef("token") },
            { key: "expectedAmount", label: "Ожидается", sortable: true, width: 130, render: cellAmount("expectedAmount") },
            { key: "receivedAmount", label: "Получено", sortable: true, width: 130, render: cellAmount("receivedAmount") },
            { key: "posted", label: "Проведён", sortable: true, width: 100, render: cellBoolBadge("posted", "✓") },
        ],
    },
    CryptoPayment: {
        entityKey: "crypto_payment",
        defaultVisibleKeys: ["number", "date", "statusName", "amount", "txHash", "confirmations"],
        defaultFilterKeys: ["status"],
        listColumns: [
            { key: "number", label: "Номер", sortable: true, width: 120, render: cellMono("number") },
            { key: "date", label: "Дата", sortable: true, width: 110, render: cellDate("date") },
            { key: "statusName", label: "Статус", sortable: true, width: 130, render: cellStatusBadge("statusName") },
            { key: "merchant", label: "Мерчант", sortable: false, width: 140, render: cellRef("merchant") },
            { key: "amount", label: "Сумма", sortable: true, width: 120, render: cellAmount("amount") },
            { key: "txHash", label: "TX Hash", sortable: false, width: 160, render: cellHashTruncated("txHash") },
            { key: "confirmations", label: "Confs", sortable: true, width: 80, render: cellMono("confirmations") },
            { key: "posted", label: "Проведён", sortable: true, width: 100, render: cellBoolBadge("posted", "✓") },
        ],
    },
    CryptoWithdrawal: {
        entityKey: "crypto_withdrawal",
        defaultVisibleKeys: ["number", "date", "statusName", "merchant", "amount", "destAddress"],
        defaultFilterKeys: ["status"],
        listColumns: [
            { key: "number", label: "Номер", sortable: true, width: 120, render: cellMono("number") },
            { key: "date", label: "Дата", sortable: true, width: 110, render: cellDate("date") },
            { key: "statusName", label: "Статус", sortable: true, width: 130, render: cellStatusBadge("statusName") },
            { key: "merchant", label: "Мерчант", sortable: false, width: 140, render: cellRef("merchant") },
            { key: "amount", label: "Сумма", sortable: true, width: 120, render: cellAmount("amount") },
            { key: "destAddress", label: "Адрес", sortable: false, width: 160, render: cellHashTruncated("destAddress") },
            { key: "txHash", label: "TX Hash", sortable: false, width: 140, render: cellHashTruncated("txHash") },
            { key: "posted", label: "Проведён", sortable: true, width: 100, render: cellBoolBadge("posted", "✓") },
        ],
    },
    CryptoSweep: {
        entityKey: "crypto_sweep",
        defaultVisibleKeys: ["number", "date", "statusName", "totalAmount", "totalFee", "lineCount"],
        defaultFilterKeys: ["status"],
        listColumns: [
            { key: "number", label: "Номер", sortable: true, width: 120, render: cellMono("number") },
            { key: "date", label: "Дата", sortable: true, width: 110, render: cellDate("date") },
            { key: "statusName", label: "Статус", sortable: true, width: 130, render: cellStatusBadge("statusName") },
            { key: "token", label: "Токен", sortable: false, width: 80, render: cellRef("token") },
            { key: "totalAmount", label: "Сумма", sortable: true, width: 120, render: cellAmount("totalAmount") },
            { key: "totalFee", label: "Комиссия", sortable: true, width: 110, render: cellAmount("totalFee") },
            { key: "lineCount", label: "Кошельков", sortable: true, width: 100, render: cellMono("lineCount") },
            { key: "posted", label: "Проведён", sortable: true, width: 100, render: cellBoolBadge("posted", "✓") },
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
