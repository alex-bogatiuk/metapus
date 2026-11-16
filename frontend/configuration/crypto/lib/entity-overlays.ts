/**
 * Entity overlays of the crypto-processing configuration: list columns of its
 * catalogs and documents, their forms and read-only cards.
 */
import React from "react"
import type { JsonObject } from "@/lib/json-value"
import { entityRegistry } from "@/lib/entity-registry"
import { registerEntityOverlays, type EntityOverlay } from "@/lib/entity-registry-defaults"
import { cellAmount, cellBoolBadge, cellCode, cellDate, cellHashTruncated, cellMono, cellMuted, cellName, cellRef, cellStatusBadge, cellTruncated } from "@/lib/entity-cells"
import { formatRawMinor } from "@/lib/money"
import { copyToClipboard } from "@/lib/format"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { COPY_VALUE_ATTR } from "@/components/shared/data-table"
import { AMLRiskBadge } from "@/configuration/crypto/components/crypto/aml-risk"

const CryptoProviderAccountForm = React.lazy(() => import("@/configuration/crypto/components/catalogs/crypto-provider-account-form"))
const CryptoSweepCard = React.lazy(() => import("@/configuration/crypto/components/documents/crypto-sweep-card"))
const CustodyReceiptCard = React.lazy(() => import("@/configuration/crypto/components/documents/crypto-custody-receipt-card"))

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

const overlays: Record<string, EntityOverlay> = {
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
        formComponent: CryptoProviderAccountForm,
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
        formComponent: CryptoSweepCard,
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

let registered = false

/** Registers the configuration's entity overlays once. */
export function registerCryptoEntities(): void {
    if (registered) return
    registered = true
    registerEntityOverlays(overlays)
    // A custody receipt is a system-owned fact without a metadata-driven list or
    // form; payments and the receipt journal link to its read-only card.
    entityRegistry.registerDocument({
        entityType: "document",
        entityName: "CryptoCustodyReceipt",
        routePrefix: "crypto-custody-receipt",
        entityKey: "crypto_custody_receipt",
        formComponent: CustodyReceiptCard,
    })
}
