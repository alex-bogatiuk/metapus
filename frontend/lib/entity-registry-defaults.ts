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
import { getStatusColorClass } from "./status-colors"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { formatRawMinor } from "@/lib/money"
import { copyToClipboard } from "@/lib/format"
import { toast } from "sonner"
import { AMLRiskBadge } from "@/components/crypto/aml-risk"
import { Button } from "@/components/ui/button"
import { COPY_VALUE_ATTR } from "@/components/shared/data-table"

// ── Shared cell renderers ───────────────────────────────────────────────
// Reusable render functions to avoid duplication across entity column sets.
// These are pure functions — no hooks, no state.

function cellCode(item: JsonObject) {
    return React.createElement("span", { className: "font-mono text-xs text-muted-foreground" }, String(item.code ?? ""))
}

function cellName(item: JsonObject) {
    return React.createElement("span", { className: "font-medium text-foreground" }, String(item.name ?? ""))
}

function cellMuted(key: string) {
    const Cell = (item: JsonObject) =>
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
    const Cell = (item: JsonObject) => {
        const ref = item[refKey] as { name?: string } | null | undefined
        return React.createElement("span", { className: "text-xs text-muted-foreground" }, ref?.name || "—")
    }
    Cell.displayName = `CellRef(${refKey})`
    return Cell
}

function cellMono(key: string) {
    const Cell = (item: JsonObject) =>
        React.createElement("span", { className: "font-mono text-xs text-muted-foreground" }, String(item[key] ?? "") || "—")
    Cell.displayName = `CellMono(${key})`
    return Cell
}

function cellBoolBadge(key: string, yesLabel = "Да") {
    const Cell = (item: JsonObject) => {
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
    const Cell = (item: JsonObject) => {
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
    const Cell = (item: JsonObject) =>
        React.createElement("span", {
            className: `text-xs text-muted-foreground truncate block`,
            style: { maxWidth: maxW },
        }, String(item[key] ?? "") || "—")
    Cell.displayName = `CellTruncated(${key})`
    return Cell
}

// Crypto-specific cell renderers

function cellHashTruncated(key: string) {
    const Cell = (item: JsonObject) => {
        const hash = String(item[key] ?? "")
        if (!hash || hash === "—") return React.createElement("span", { className: "text-xs text-muted-foreground" }, "—")
        const truncated = hash.length > 16 ? `${hash.slice(0, 8)}…${hash.slice(-6)}` : hash
        return React.createElement("span", {
            className: "font-mono text-xs text-muted-foreground cursor-help",
            title: hash,
            [COPY_VALUE_ATTR]: hash,
        }, truncated)
    }
    Cell.displayName = `CellHash(${key})`
    return Cell
}

function cellStatusBadge(key: string) {
    const Cell = (item: JsonObject) => {
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
    const Cell = (item: JsonObject) => {
        const value = String(item[key] ?? "0")
        return React.createElement("span", {
            className: "font-mono text-xs text-right block tabular-nums text-foreground",
        }, value)
    }
    Cell.displayName = `CellAmount(${key})`
    return Cell
}

function withdrawalAmount(item: JsonObject) {
    const value = item.amount
    const scale = item.decimalPlaces
    const symbol = item.symbol
    if (typeof value !== "string" || typeof scale !== "number" || typeof symbol !== "string") return "—"
    return React.createElement("span", { className: "block text-right font-mono text-xs tabular-nums" }, `${formatRawMinor(value, scale)} ${symbol}`)
}

function withdrawalAddress(item: JsonObject) {
    const address = item.destAddress
    if (typeof address !== "string" || !address) return "—"
    const short = address.length > 16 ? `${address.slice(0, 8)}…${address.slice(-6)}` : address
    return React.createElement(Button, {
        variant: "ghost", size: "sm", title: address,
        className: "font-mono text-xs",
        onClick: (event: React.MouseEvent) => {
            event.stopPropagation()
            copyToClipboard(address)
            toast.success("Адрес скопирован")
        },
    }, React.createElement("span", { [COPY_VALUE_ATTR]: address }, short))
}

// ── Column Overlay Configs ──────────────────────────────────────────────
// Presentation overlay merged onto metadata list columns (render, width, default visible).
// Enum columns use `enumEntity` for dynamic label resolution in AutoList.

type ColumnOverlayConfig = Pick<EntityUIRegistration,
    "listColumns" | "defaultVisibleKeys" | "defaultFilterKeys" | "entityKey">

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

    // ── Crypto Processing Entities ──────────────────────────────────────

    BlockchainNetwork: {
        entityKey: "blockchain_network",
        defaultVisibleKeys: ["code", "name", "chainFamily", "networkIdentifier", "isActive"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, width: 140, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, width: 200, render: cellName },
            { key: "chainFamily", label: "Семейство", sortable: true, width: 120, render: cellMono("chainFamily") },
            { key: "networkIdentifier", label: "Идентификатор сети", sortable: true, width: 160, render: cellMono("networkIdentifier") },
            { key: "isActive", label: "Активна", sortable: true, width: 100, render: cellBoolBadge("isActive") },
            { key: "confirmationsNeeded", label: "Подтверждения", sortable: true, width: 130, render: cellMono("confirmationsNeeded") },
            { key: "explorerUrl", label: "Explorer URL", sortable: false, width: 240, render: cellTruncated("explorerUrl", 240) },
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
        defaultVisibleKeys: ["address", "network", "statusName", "tierName", "custodyMerchant", "riskClass"],
        listColumns: [
            { key: "address", label: "Адрес", sortable: false, width: 180, render: cellHashTruncated("address") },
            { key: "network", label: "Сеть", sortable: false, width: 130, render: cellRef("network") },
            { key: "statusName", label: "Статус", sortable: true, width: 120, render: cellStatusBadge("statusName") },
            { key: "tierName", label: "Tier", sortable: true, width: 100, render: cellMuted("tierName") },
            { key: "custodyMerchant", label: "Владелец средств", sortable: false, width: 170, render: cellRef("custodyMerchant") },
            { key: "riskClass", label: "AML-риск", sortable: false, width: 180, render: item => React.createElement(AMLRiskBadge, { riskClass: String(item.riskClass ?? "unknown"), enabled: item.riskEnabled === true, empty: item.riskHasFunds === false, expiresAt: typeof item.riskExpiresAt === "string" ? item.riskExpiresAt : undefined }) },
            { key: "merchant", label: "Мерчант назначения", sortable: false, width: 170, render: cellRef("merchant") },
        ],
    },
    CryptoProviderAccount: {
        entityKey: "crypto_provider_account",
        defaultVisibleKeys: ["code", "name", "provider", "network", "hasCredentials", "isActive"],
        listColumns: [
            { key: "code", label: "Код", sortable: true, width: 120, render: cellCode },
            { key: "name", label: "Наименование", sortable: true, width: 220, render: cellName },
            { key: "provider", label: "Провайдер", sortable: true, width: 120, render: (item) =>
                React.createElement("span", { className: "font-semibold text-foreground uppercase text-xs" }, String(item.provider ?? "")) },
            { key: "network", label: "Сеть", sortable: false, width: 150, render: cellRef("network") },
            { key: "externalAccountRef", label: "Внешний ID", sortable: false, width: 140, render: cellMono("externalAccountRef") },
            { key: "hasCredentials", label: "Ключи API", sortable: true, width: 150, render: (item) => {
                if (item.hasCredentials) {
                    return React.createElement("span", {
                        className: "inline-flex items-center rounded-md border border-emerald-500/20 bg-emerald-500/10 px-2 py-0.5 text-[10px] font-semibold text-emerald-600 dark:text-emerald-400 font-mono",
                    }, String(item.maskedApiKey ?? "Заданы"))
                }
                return React.createElement("span", {
                    className: "inline-flex items-center rounded-md border border-destructive/20 bg-destructive/10 px-2 py-0.5 text-[10px] font-semibold text-destructive",
                }, "Не настроены")
            }},
            { key: "isActive", label: "Активен", sortable: true, width: 90, render: cellBoolBadge("isActive") },
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
        defaultVisibleKeys: ["number", "date", "statusName", "riskClass", "amount", "txHash", "confirmations"],
        defaultFilterKeys: ["status"],
        listColumns: [
            { key: "riskClass", label: "AML-риск", sortable: false, width: 180, render: item => {
                const a = item.aml
                return React.createElement(AMLRiskBadge, { riskClass: String(item.riskClass ?? "unknown"), enabled: typeof a === "object" && a !== null && !Array.isArray(a) && a.enabled === true, expiresAt: typeof a === "object" && a !== null && !Array.isArray(a) && typeof a.expiresAt === "string" ? a.expiresAt : undefined })
            } },
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
    WithdrawalRequest: {
        entityKey: "withdrawal_request",
        defaultVisibleKeys: ["number", "status", "merchantName", "amount", "network", "destAddress"],
        defaultFilterKeys: ["status"],
        listColumns: [
            { key: "number", label: "Номер", sortable: true, width: 120, render: cellMono("number") },
            { key: "status", label: "Статус", sortable: true, width: 140, enumEntity: "WithdrawalRequest" },
            { key: "merchantName", label: "Мерчант", sortable: false, width: 160, render: cellMuted("merchantName") },
            { key: "symbol", label: "Токен", sortable: false, width: 90, render: cellMuted("symbol") },
            { key: "amount", label: "Сумма", sortable: true, width: 150, align: "right", render: withdrawalAmount },
            { key: "network", label: "Сеть", sortable: false, width: 120, render: cellMuted("network") },
            { key: "destAddress", label: "Адрес", sortable: false, width: 170, render: withdrawalAddress },
            { key: "createdAt", label: "Создана", sortable: true, width: 120, render: cellDate("createdAt") },
        ],
    },
    TreasuryWithdrawal: {
        entityKey: "treasury_withdrawal",
        defaultVisibleKeys: ["number", "date", "status", "token", "sourceWallet", "amount", "destAddress"],
        defaultFilterKeys: ["status"],
        listColumns: [
            { key: "number", label: "Номер", sortable: true, width: 120, render: cellMono("number") },
            { key: "date", label: "Дата", sortable: true, width: 110, render: cellDate("date") },
            { key: "status", label: "Статус", sortable: true, width: 140, render: cellStatusBadge("status") },
            { key: "token", label: "Токен", sortable: false, width: 90, render: cellRef("token") },
            { key: "sourceWallet", label: "Кошелёк", sortable: false, width: 150, render: cellRef("sourceWallet") },
            { key: "amount", label: "Сумма", sortable: true, width: 120, render: cellAmount("amount") },
            { key: "networkFee", label: "Комиссия", sortable: true, width: 110, render: cellAmount("networkFee") },
            { key: "destAddress", label: "Адрес", sortable: false, width: 170, render: cellHashTruncated("destAddress") },
            { key: "txHash", label: "TX Hash", sortable: false, width: 150, render: cellHashTruncated("txHash") },
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
    CryptoResourceRental: {
        entityKey: "crypto_resource_rental",
        defaultVisibleKeys: ["number", "status", "provider", "receiveWalletId", "delegatedAmount", "reservedAmount", "consumedAmount", "netCostAmount", "expiresAt"],
        defaultFilterKeys: ["status", "provider"],
        listColumns: [
            { key: "number", label: "Номер", sortable: true, width: 120, render: cellMono("number") },
            { key: "status", label: "Статус", sortable: true, width: 130, render: cellStatusBadge("status") },
            { key: "provider", label: "Провайдер", sortable: true, width: 90, render: cellMuted("provider") },
            { key: "receiveWalletId", label: "Кошелёк", sortable: false, width: 150, render: cellHashTruncated("receiveWalletId") },
            { key: "delegatedAmount", label: "Energy", sortable: true, width: 100, render: cellMono("delegatedAmount") },
            { key: "reservedAmount", label: "Резерв", sortable: true, width: 100, render: cellMono("reservedAmount") },
            { key: "consumedAmount", label: "Расход", sortable: true, width: 100, render: cellMono("consumedAmount") },
            { key: "netCostAmount", label: "Стоимость", sortable: true, width: 110, render: cellAmount("netCostAmount") },
            { key: "expiresAt", label: "Окончание", sortable: true, width: 140, render: cellDate("expiresAt") },
        ],
    },
    CryptoNetworkOperation: {
        entityKey: "crypto_network_operation",
        defaultVisibleKeys: ["number", "date", "operationType", "status", "targetAddress", "costAmount", "txHash", "posted"],
        defaultFilterKeys: ["operationType", "status", "networkId"],
        listColumns: [
            { key: "number", label: "Номер", sortable: true, width: 120, render: cellMono("number") },
            { key: "date", label: "Дата", sortable: true, width: 130, render: cellDate("date") },
            { key: "operationType", label: "Операция", sortable: true, width: 150, render: cellMuted("operationType") },
            { key: "status", label: "Статус", sortable: true, width: 120, render: cellStatusBadge("status") },
            { key: "targetAddress", label: "Адрес", sortable: false, width: 170, render: cellHashTruncated("targetAddress") },
            { key: "costAmount", label: "Затраты", sortable: true, width: 110, render: cellAmount("costAmount") },
            { key: "txHash", label: "TX Hash", sortable: false, width: 170, render: cellHashTruncated("txHash") },
            { key: "posted", label: "Проведён", sortable: true, width: 100, render: cellBoolBadge("posted", "✓") },
        ],
    },
    CryptoProviderBalanceOperation: {
        entityKey: "crypto_provider_balance_operation",
        defaultVisibleKeys: ["number", "date", "status", "operationType", "recordType", "amount", "externalRef"],
        defaultFilterKeys: ["status", "operationType"],
        listColumns: [
            { key: "number", label: "Номер", sortable: true, width: 120, render: cellMono("number") },
            { key: "date", label: "Дата", sortable: true, width: 110, render: cellDate("date") },
            { key: "status", label: "Статус", sortable: true, width: 130, render: cellStatusBadge("status") },
            { key: "operationType", label: "Операция", sortable: true, width: 120, render: cellMuted("operationType") },
            { key: "recordType", label: "Направление", sortable: true, width: 110, render: cellMuted("recordType") },
            { key: "amount", label: "Сумма", sortable: true, width: 120, render: cellAmount("amount") },
            { key: "externalRef", label: "Основание", sortable: false, width: 170, render: cellMono("externalRef") },
        ],
    },
}

// ── Auto-Discovery from Backend Metadata ────────────────────────────────

const IntegrationList = React.lazy(() => import("@/components/integrations/integration-list"))
const IntegrationAccountForm = React.lazy(() => import("@/components/integrations/account-form"))
const IntegrationPoolForm = React.lazy(() => import("@/components/integrations/pool-form"))
const RateSourceForm = React.lazy(() => import("@/components/exchange-rates/rate-source-form"))
const CryptoProviderAccountForm = React.lazy(() => import("@/components/catalogs/crypto-provider-account-form"))
const CryptoSweepCard = React.lazy(() => import("@/components/documents/crypto-sweep-card"))
const CustodyReceiptCard = React.lazy(() => import("@/components/documents/crypto-custody-receipt-card"))

// A custody receipt is a system-owned fact without a metadata-driven list or
// form; payments and the receipt journal link to its read-only card.
entityRegistry.registerDocument({
    entityType: "document",
    entityName: "CryptoCustodyReceipt",
    routePrefix: "crypto-custody-receipt",
    entityKey: "crypto_custody_receipt",
    formComponent: CustodyReceiptCard,
})
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

        const overlay = columnOverlayConfigs[entity.name]

        const reg = {
            entityType: type,
            entityName: entity.name,
            routePrefix,
            entityKey: entity.key,
            formComponent:
                entity.name === "IntegrationAccount" ? IntegrationAccountForm
 : entity.name === "IntegrationPool" ? IntegrationPoolForm
 : entity.name === "RateSource"
                    ? RateSourceForm
                    : entity.name === "CryptoProviderAccount"
                        ? CryptoProviderAccountForm
                        : entity.name === "CryptoSweep"
                            ? CryptoSweepCard
                        : undefined,
            listComponent: entity.name === "IntegrationAccount" || entity.name === "IntegrationPool" ? IntegrationList : undefined,
            listColumns: overlay?.listColumns,
            defaultVisibleKeys: overlay?.defaultVisibleKeys,
            defaultFilterKeys: overlay?.defaultFilterKeys,
        }

        if (type === "catalog") {
            entityRegistry.registerCatalog(reg)
        } else {
            entityRegistry.registerDocument(reg)
        }
        discovered.push(reg)
    }
}
