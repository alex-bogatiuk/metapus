import type {
    MerchantAPIKeyListItem,
    MerchantAPIKeyResponse,
    CreateMerchantAPIKeyRequest,
    MerchantAPIKeyListResponse,
    MerchantUserItem,
    MerchantUserListResponse,
    AddMerchantUserRequest,
    UpdateMerchantUserRoleRequest,
} from "@/configuration/crypto/types/merchant-api"
import { apiFetch, createScopedClient } from "@/lib/api/core"

// The merchant portal is the configuration's own backend surface, /portal/v1.
const { fetch: portalFetch, fetchBlob: portalFetchBlob } = createScopedClient("portal")

export const portalApi = {
    merchants: () =>
        portalFetch<{ items: import("@/configuration/crypto/types/portal-api").PortalMerchantItem[] }>("/merchants"),

    summary: (merchantId?: string) => {
        const qs = merchantId ? `?merchant_id=${encodeURIComponent(merchantId)}` : ""
        return portalFetch<import("@/configuration/crypto/types/portal-api").PortalSummaryResponse>(`/dashboard/summary${qs}`)
    },

    currencies: (merchantId?: string) => {
        const qs = merchantId ? `?merchant_id=${encodeURIComponent(merchantId)}` : ""
        return portalFetch<{ items: import("@/configuration/crypto/types/portal-api").PortalCurrencyItem[] }>(`/dashboard/currencies${qs}`)
    },

    chart: (period: "7d" | "30d" | "90d" = "30d", merchantId?: string) => {
        const params: string[] = [`period=${period}`]
        if (merchantId) params.push(`merchant_id=${encodeURIComponent(merchantId)}`)
        return portalFetch<{ items: import("@/configuration/crypto/types/portal-api").PortalChartPoint[] }>(`/dashboard/chart?${params.join("&")}`)
    },

    funnel: (merchantId?: string) => {
        const qs = merchantId ? `?merchant_id=${encodeURIComponent(merchantId)}` : ""
        return portalFetch<import("@/configuration/crypto/types/portal-api").PortalFunnelResponse>(`/dashboard/funnel${qs}`)
    },

    balance: (merchantId?: string) => {
        const params: string[] = []
        if (merchantId) params.push(`merchant_id=${encodeURIComponent(merchantId)}`)
        const qs = params.length > 0 ? `?${params.join("&")}` : ""
        return portalFetch<import("@/configuration/crypto/types/portal-api").PortalBalanceResponse>(`/dashboard/balance${qs}`)
    },

    balanceDetailed: (merchantId?: string) => {
        const params: string[] = []
        if (merchantId) params.push(`merchant_id=${encodeURIComponent(merchantId)}`)
        const qs = params.length > 0 ? `?${params.join("&")}` : ""
        return portalFetch<import("@/configuration/crypto/types/portal-api").PortalDetailedBalanceResponse>(`/balances${qs}`)
    },

    riskBalance: (merchantId: string) =>
        portalFetch<import("@/types/generated/http-contracts").PortalRiskBalanceResponse>(
            `/balances/risk?${new URLSearchParams({ merchant_id: merchantId })}`
        ),

    invoices: (params?: import("@/configuration/crypto/types/portal-api").InvoiceFilterParams) => {
        const entries: [string, string][] = []
        if (params?.merchantId) entries.push(["merchant_id", params.merchantId])
        if (params?.status) entries.push(["status", params.status])
        if (params?.search) entries.push(["search", params.search])
        if (params?.token) entries.push(["token", params.token])
        if (params?.dateFrom) entries.push(["dateFrom", params.dateFrom])
        if (params?.dateTo) entries.push(["dateTo", params.dateTo])
        if (params?.sort) entries.push(["sort", params.sort])
        if (params?.order) entries.push(["order", params.order])
        if (params?.limit) entries.push(["limit", String(params.limit)])
        if (params?.offset) entries.push(["offset", String(params.offset)])
        const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
        return portalFetch<import("@/configuration/crypto/types/portal-api").PortalInvoiceListResponse>(`/invoices${qs}`)
    },

    exportInvoicesCsv: async (params?: import("@/configuration/crypto/types/portal-api").InvoiceFilterParams) => {
        const entries: [string, string][] = []
        if (params?.merchantId) entries.push(["merchant_id", params.merchantId])
        if (params?.status) entries.push(["status", params.status])
        if (params?.search) entries.push(["search", params.search])
        if (params?.token) entries.push(["token", params.token])
        if (params?.dateFrom) entries.push(["dateFrom", params.dateFrom])
        if (params?.dateTo) entries.push(["dateTo", params.dateTo])
        const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""

        const { blob, filename } = await portalFetchBlob(`/invoices/export${qs}`)

        // Trigger browser download via <a> element
        const url = URL.createObjectURL(blob)
        const a = document.createElement("a")
        a.href = url
        a.download = filename
        document.body.appendChild(a)
        a.click()

        // Cleanup
        setTimeout(() => {
            document.body.removeChild(a)
            URL.revokeObjectURL(url)
        }, 100)
    },

    apiKeys: {
        list: (merchantId: string) =>
            portalFetch<{ items: MerchantAPIKeyListItem[]; total: number }>(`/api-keys?merchant_id=${encodeURIComponent(merchantId)}`),
        create: (merchantId: string, body: CreateMerchantAPIKeyRequest) =>
            portalFetch<MerchantAPIKeyResponse>(`/api-keys?merchant_id=${encodeURIComponent(merchantId)}`, {
                method: "POST",
                body: JSON.stringify(body),
            }),
        revoke: (merchantId: string, keyId: string) =>
            portalFetch<void>(`/api-keys/${keyId}?merchant_id=${encodeURIComponent(merchantId)}`, {
                method: "DELETE",
            }),
    },

    paymentLinks: {
        list: (params?: { merchantId?: string; limit?: number; offset?: number }) => {
            const entries: [string, string][] = []
            if (params?.merchantId) entries.push(["merchant_id", params.merchantId])
            if (params?.limit) entries.push(["limit", String(params.limit)])
            if (params?.offset) entries.push(["offset", String(params.offset)])
            const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
            return portalFetch<import("@/configuration/crypto/types/portal-api").PortalPaymentLinkListResponse>(`/payment-links${qs}`)
        },
        create: (merchantId: string, body: import("@/configuration/crypto/types/portal-api").CreatePaymentLinkRequest) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").PortalPaymentLinkCreateResponse>(`/payment-links?merchant_id=${encodeURIComponent(merchantId)}`, {
                method: "POST",
                body: JSON.stringify(body),
            }),
    },

    settings: {
        get: (merchantId: string) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").PortalSettingsResponse>(`/settings?merchant_id=${encodeURIComponent(merchantId)}`),
        update: (merchantId: string, body: import("@/configuration/crypto/types/portal-api").UpdatePortalSettingsRequest) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").PortalSettingsResponse>(`/settings?merchant_id=${encodeURIComponent(merchantId)}`, {
                method: "PATCH",
                body: JSON.stringify(body),
            }),
        fees: (merchantId: string) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").PortalFeeScheduleResponse>(
                `/settings/fees?merchant_id=${encodeURIComponent(merchantId)}`,
            ),
    },

    createInvoice: (merchantId: string, body: import("@/configuration/crypto/types/portal-api").PortalCreateInvoiceRequest) =>
        portalFetch<import("@/configuration/crypto/types/portal-api").PortalCreateInvoiceResponse>(
            `/invoices?merchant_id=${encodeURIComponent(merchantId)}`,
            { method: "POST", body: JSON.stringify(body) },
        ),

    invoiceDetail: (invoiceId: string, merchantId?: string) => {
        const qs = merchantId ? `?merchant_id=${encodeURIComponent(merchantId)}` : ""
        return portalFetch<import("@/configuration/crypto/types/portal-api").PortalInvoiceDetailResponse>(`/invoices/${invoiceId}${qs}`)
    },

    refundablePayment: (merchantId: string, paymentId: string) =>
        portalFetch<import("@/configuration/crypto/types/portal-api").PortalRefundablePaymentResponse>(
            `/payments/${encodeURIComponent(paymentId)}/refund?merchant_id=${encodeURIComponent(merchantId)}`,
        ),

    withdrawals: (params?: { merchantId?: string; status?: string; sort?: string; order?: "asc" | "desc"; limit?: number; offset?: number }) => {
        const entries: [string, string][] = []
        if (params?.merchantId) entries.push(["merchant_id", params.merchantId])
        if (params?.status) entries.push(["status", params.status])
        if (params?.sort) entries.push(["sort", params.sort])
        if (params?.order) entries.push(["order", params.order])
        if (params?.limit) entries.push(["limit", String(params.limit)])
        if (params?.offset) entries.push(["offset", String(params.offset)])
        const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
        return portalFetch<import("@/configuration/crypto/types/portal-api").PortalWithdrawalListResponse>(`/withdrawals${qs}`)
    },

    withdrawalAddresses: (merchantId: string) =>
        portalFetch<import("@/configuration/crypto/types/portal-api").PortalWithdrawalAddressListResponse>(
            `/withdrawal-addresses?merchant_id=${encodeURIComponent(merchantId)}`,
        ),

    addWithdrawalAddress: (merchantId: string, body: import("@/configuration/crypto/types/portal-api").PortalAddWithdrawalAddressRequest) =>
        portalFetch<{ id: string }>(
            `/withdrawal-addresses?merchant_id=${encodeURIComponent(merchantId)}`,
            { method: "POST", body: JSON.stringify(body) },
        ),

    removeWithdrawalAddress: (merchantId: string, addressId: string) =>
        portalFetch<void>(
            `/withdrawal-addresses/${addressId}?merchant_id=${encodeURIComponent(merchantId)}`,
            { method: "DELETE" },
        ),

    withdrawalRequests: (merchantId?: string) => {
        const qs = merchantId ? `?merchant_id=${encodeURIComponent(merchantId)}` : ""
        return portalFetch<import("@/configuration/crypto/types/portal-api").PortalWithdrawalRequestListResponse>(`/withdrawal-requests${qs}`)
    },

    createWithdrawalRequest: (merchantId: string, body: import("@/configuration/crypto/types/portal-api").PortalCreateWithdrawalRequest) =>
        portalFetch<{ id: string; number: string; status: string }>(
            `/withdrawal-requests?merchant_id=${encodeURIComponent(merchantId)}`,
            { method: "POST", body: JSON.stringify(body) },
        ),

    rejectWithdrawalRequest: (merchantId: string, id: string, reason?: string) =>
        portalFetch<{ status: string }>(
            `/withdrawal-requests/${id}/reject?merchant_id=${encodeURIComponent(merchantId)}`,
            { method: "POST", body: JSON.stringify({ reason: reason ?? "" }) },
        ),

    webhookEndpoints: {
        list: (merchantId: string) =>
            portalFetch<{ items: import("@/configuration/crypto/types/portal-api").PortalWebhookEndpoint[]; total: number }>(
                `/webhook-endpoints?merchant_id=${encodeURIComponent(merchantId)}`,
            ),
        create: (merchantId: string, body: { name: string; url: string; enabledEvents: string[]; isDefault: boolean }) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").PortalWebhookEndpoint>(
                `/webhook-endpoints?merchant_id=${encodeURIComponent(merchantId)}`,
                { method: "POST", body: JSON.stringify(body) },
            ),
        update: (merchantId: string, endpointId: string, body: { version: number } & Partial<{ name: string; url: string; enabledEvents: string[]; isDefault: boolean; isActive: boolean }>) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").PortalWebhookEndpoint>(
                `/webhook-endpoints/${encodeURIComponent(endpointId)}?merchant_id=${encodeURIComponent(merchantId)}`,
                { method: "PATCH", body: JSON.stringify(body) },
            ),
        remove: (merchantId: string, endpointId: string) =>
            portalFetch<void>(
                `/webhook-endpoints/${encodeURIComponent(endpointId)}?merchant_id=${encodeURIComponent(merchantId)}`,
                { method: "DELETE" },
            ),
        rotateSecret: (merchantId: string, endpointId: string) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").PortalWebhookEndpoint>(
                `/webhook-endpoints/${encodeURIComponent(endpointId)}/rotate-secret?merchant_id=${encodeURIComponent(merchantId)}`,
                { method: "POST" },
            ),
        test: (merchantId: string, endpointId: string) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").PortalWebhookTestResponse>(
                `/webhook-endpoints/${encodeURIComponent(endpointId)}/test?merchant_id=${encodeURIComponent(merchantId)}`,
                { method: "POST" },
            ),
    },

    merchantEvents: (merchantId: string, params?: { limit?: number; offset?: number }) => {
        const entries: [string, string][] = [["merchant_id", merchantId]]
        if (params?.limit) entries.push(["limit", String(params.limit)])
        if (params?.offset) entries.push(["offset", String(params.offset)])
        return portalFetch<{ items: import("@/configuration/crypto/types/portal-api").PortalMerchantEvent[]; total: number }>(
            `/events?${new URLSearchParams(entries).toString()}`,
        )
    },

    eventDeliveries: (merchantId: string, eventId: string) =>
        portalFetch<{ items: import("@/configuration/crypto/types/portal-api").PortalWebhookDeliveryItem[]; total: number }>(
            `/events/${encodeURIComponent(eventId)}/deliveries?merchant_id=${encodeURIComponent(merchantId)}`,
        ),

    redeliverEvent: (merchantId: string, eventId: string, endpointId: string) =>
        portalFetch<{ status: string }>(
            `/events/${encodeURIComponent(eventId)}/redeliver?merchant_id=${encodeURIComponent(merchantId)}`,
            { method: "POST", body: JSON.stringify({ endpointId }) },
        ),

    networkPolicies: {
        get: (merchantId: string, surface: import("@/configuration/crypto/types/portal-api").MerchantNetworkSurface) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").MerchantNetworkPolicyResponse>(
                `/security/network-policies/${surface}?merchant_id=${encodeURIComponent(merchantId)}`,
            ),
        setMode: (merchantId: string, surface: import("@/configuration/crypto/types/portal-api").MerchantNetworkSurface, mode: import("@/configuration/crypto/types/portal-api").MerchantNetworkPolicyMode, reason: string) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").MerchantNetworkPolicy>(
                `/security/network-policies/${surface}?merchant_id=${encodeURIComponent(merchantId)}`,
                { method: "PUT", body: JSON.stringify({ mode, reason }) },
            ),
        addNetwork: (merchantId: string, surface: import("@/configuration/crypto/types/portal-api").MerchantNetworkSurface, cidr: string, label: string, reason: string) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").MerchantNetworkPolicy>(
                `/security/network-policies/${surface}/allowed-networks?merchant_id=${encodeURIComponent(merchantId)}`,
                { method: "POST", body: JSON.stringify({ cidr, label, reason }) },
            ),
        revokeNetwork: (merchantId: string, surface: import("@/configuration/crypto/types/portal-api").MerchantNetworkSurface, networkId: string, reason: string) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").MerchantNetworkPolicy>(
                `/security/network-policies/${surface}/allowed-networks/${encodeURIComponent(networkId)}?merchant_id=${encodeURIComponent(merchantId)}`,
                { method: "DELETE", body: JSON.stringify({ reason }) },
            ),
        getForKey: (merchantId: string, keyId: string) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").MerchantNetworkPolicyResponse>(
                `/api-keys/${encodeURIComponent(keyId)}/network-policy?merchant_id=${encodeURIComponent(merchantId)}`,
            ),
        setModeForKey: (merchantId: string, keyId: string, mode: import("@/configuration/crypto/types/portal-api").MerchantNetworkPolicyMode, reason: string) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").MerchantNetworkPolicy>(
                `/api-keys/${encodeURIComponent(keyId)}/network-policy?merchant_id=${encodeURIComponent(merchantId)}`,
                { method: "PUT", body: JSON.stringify({ mode, reason }) },
            ),
        addNetworkForKey: (merchantId: string, keyId: string, cidr: string, label: string, reason: string) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").MerchantNetworkPolicy>(
                `/api-keys/${encodeURIComponent(keyId)}/allowed-networks?merchant_id=${encodeURIComponent(merchantId)}`,
                { method: "POST", body: JSON.stringify({ cidr, label, reason }) },
            ),
        revokeNetworkForKey: (merchantId: string, keyId: string, networkId: string, reason: string) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").MerchantNetworkPolicy>(
                `/api-keys/${encodeURIComponent(keyId)}/allowed-networks/${encodeURIComponent(networkId)}?merchant_id=${encodeURIComponent(merchantId)}`,
                { method: "DELETE", body: JSON.stringify({ reason }) },
            ),
    },

    depositAddresses: {
        rotate: (merchantId: string, walletId: string, body: import("@/configuration/crypto/types/portal-api").PortalRotateDepositAddressRequest) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").PortalRotateDepositAddressResponse>(
                `/deposit-addresses/${encodeURIComponent(walletId)}/rotate?${new URLSearchParams({ merchant_id: merchantId })}`,
                { method: "POST", body: JSON.stringify(body) },
            ),
        list: (params?: import("@/configuration/crypto/types/portal-api").DepositAddressFilterParams) => {
            const entries: [string, string][] = []
            if (params?.merchantId) entries.push(["merchant_id", params.merchantId])
            if (params?.networkId) entries.push(["network_id", params.networkId])
            if (params?.search) entries.push(["search", params.search])
            if (params?.limit !== undefined) entries.push(["limit", String(params.limit)])
            if (params?.offset !== undefined) entries.push(["offset", String(params.offset)])
            const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
            return portalFetch<import("@/configuration/crypto/types/portal-api").PortalDepositAddressListResponse>(`/deposit-addresses${qs}`)
        },
        create: (merchantId: string, body: import("@/configuration/crypto/types/portal-api").PortalCreateDepositAddressRequest) =>
            portalFetch<import("@/configuration/crypto/types/portal-api").PortalCreateDepositAddressResponse>(
                `/deposit-addresses?merchant_id=${encodeURIComponent(merchantId)}`,
                { method: "POST", body: JSON.stringify(body) },
            ),
    },
}

export const merchantApiKeysApi = {
    list: (merchantId: string): Promise<MerchantAPIKeyListItem[]> =>
        apiFetch<MerchantAPIKeyListResponse>(`/merchant-admin/merchants/${merchantId}/api-keys`)
            .then((r) => r.items),

    create: (merchantId: string, body: CreateMerchantAPIKeyRequest): Promise<MerchantAPIKeyResponse> =>
        apiFetch<MerchantAPIKeyResponse>(`/merchant-admin/merchants/${merchantId}/api-keys`, {
            method: "POST",
            body: JSON.stringify(body),
        }),

    revoke: (merchantId: string, keyId: string): Promise<void> =>
        apiFetch<void>(`/merchant-admin/merchants/${merchantId}/api-keys/${keyId}`, {
            method: "DELETE",
        }),
}

export const merchantUsersApi = {
    list: (merchantId: string): Promise<MerchantUserItem[]> =>
        apiFetch<MerchantUserListResponse>(`/merchant-admin/merchants/${merchantId}/users`)
            .then((r) => r.items),

    add: (merchantId: string, body: AddMerchantUserRequest): Promise<void> =>
        apiFetch<void>(`/merchant-admin/merchants/${merchantId}/users`, {
            method: "POST",
            body: JSON.stringify(body),
        }),

    remove: (merchantId: string, userId: string): Promise<void> =>
        apiFetch<void>(`/merchant-admin/merchants/${merchantId}/users/${userId}`, {
            method: "DELETE",
        }),

    updateRole: (merchantId: string, userId: string, body: UpdateMerchantUserRoleRequest): Promise<void> =>
        apiFetch<void>(`/merchant-admin/merchants/${merchantId}/users/${userId}/role`, {
            method: "PATCH",
            body: JSON.stringify(body),
        }),
}

export const feeScheduleApi = {
    list: (): Promise<import("@/configuration/crypto/types/fee-schedule").FeeScheduleListResponse> =>
        apiFetch<import("@/configuration/crypto/types/fee-schedule").FeeScheduleListResponse>("/system/fee-schedule"),

    upsert: (body: import("@/configuration/crypto/types/fee-schedule").FeeScheduleUpsertRequest): Promise<void> =>
        apiFetch<void>("/system/fee-schedule", {
            method: "PUT",
            body: JSON.stringify(body),
        }),

    delete: (body: import("@/configuration/crypto/types/fee-schedule").FeeScheduleDeleteRequest): Promise<void> =>
        apiFetch<void>("/system/fee-schedule", {
            method: "DELETE",
            body: JSON.stringify(body),
        }),
}

export const merchantFeeScheduleApi = {
    list: (merchantId: string): Promise<import("@/configuration/crypto/types/fee-schedule").FeeScheduleListResponse> =>
        apiFetch<import("@/configuration/crypto/types/fee-schedule").FeeScheduleListResponse>(
            `/merchant-admin/merchants/${merchantId}/fee-schedule`
        ),

    upsert: (merchantId: string, body: import("@/configuration/crypto/types/fee-schedule").FeeScheduleUpsertRequest): Promise<void> =>
        apiFetch<void>(`/merchant-admin/merchants/${merchantId}/fee-schedule`, {
            method: "PUT",
            body: JSON.stringify(body),
        }),

    delete: (merchantId: string, body: import("@/configuration/crypto/types/fee-schedule").FeeScheduleDeleteRequest): Promise<void> =>
        apiFetch<void>(`/merchant-admin/merchants/${merchantId}/fee-schedule`, {
            method: "DELETE",
            body: JSON.stringify(body),
        }),
}
