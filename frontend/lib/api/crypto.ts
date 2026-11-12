import type { CursorListResponse, CursorListParams } from "@/types/common"
import { apiFetch, assertUuidPathParam, buildListQS, idempotencyHeaders } from "./core"

export const cryptoApi = {
    unidentifiedReceipts: {
        list: (state: import("@/types/unidentified-receipt").ReceiptJournalState, includeDust: boolean, offset = 0) =>
            apiFetch<import("@/types/generated/http-contracts").UnidentifiedReceiptListResponse>(
                `/crypto/unidentified-receipts?state=${state}&includeDust=${includeDust}&limit=50&offset=${offset}`,
            ),
        detail: (receiptId: string) =>
            apiFetch<import("@/types/generated/http-contracts").CustodyReceiptDetailResponse>(
                `/crypto/unidentified-receipts/${assertUuidPathParam(receiptId, "receiptId")}`,
            ),
        prepareCredit: (receiptId: string, merchantId: string, key: string) =>
            apiFetch<import("@/types/generated/http-contracts").MerchantCreditResponse>(
                `/crypto/unidentified-receipts/${assertUuidPathParam(receiptId, "receiptId")}/credits`,
                { method: "POST", headers: idempotencyHeaders(key), body: JSON.stringify({ merchantId: assertUuidPathParam(merchantId, "merchantId") }) },
            ),
        approveCredit: (creditId: string, version: number, key: string) =>
            apiFetch<import("@/types/generated/http-contracts").MerchantCreditResponse>(
                `/crypto/merchant-credits/${assertUuidPathParam(creditId, "creditId")}/approve`,
                { method: "POST", headers: idempotencyHeaders(key), body: JSON.stringify({ version }) },
            ),
        rejectCredit: (creditId: string, version: number, reason: string, key: string) =>
            apiFetch<import("@/types/generated/http-contracts").MerchantCreditResponse>(
                `/crypto/merchant-credits/${assertUuidPathParam(creditId, "creditId")}/reject`,
                { method: "POST", headers: idempotencyHeaders(key), body: JSON.stringify({ version, reason }) },
            ),
        preparePlatformFunding: (receiptId: string, command: import("@/types/generated/http-contracts").PreparePlatformFundingRequest, key: string) =>
            apiFetch<import("@/types/generated/http-contracts").PlatformFundingResponse>(
                `/crypto/unidentified-receipts/${assertUuidPathParam(receiptId, "receiptId")}/platform-fundings`,
                { method: "POST", headers: idempotencyHeaders(key), body: JSON.stringify(command) },
            ),
        approvePlatformFunding: (fundingId: string, version: number, key: string) =>
            apiFetch<import("@/types/generated/http-contracts").PlatformFundingResponse>(
                `/crypto/platform-fundings/${assertUuidPathParam(fundingId, "fundingId")}/approve`,
                { method: "POST", headers: idempotencyHeaders(key), body: JSON.stringify({ version }) },
            ),
        rejectPlatformFunding: (fundingId: string, version: number, reason: string, key: string) =>
            apiFetch<import("@/types/generated/http-contracts").PlatformFundingResponse>(
                `/crypto/platform-fundings/${assertUuidPathParam(fundingId, "fundingId")}/reject`,
                { method: "POST", headers: idempotencyHeaders(key), body: JSON.stringify({ version, reason }) },
            ),
    },
    financialAdjustments: {
        paymentSources: (paymentId: string) =>
            apiFetch<import("@/types/generated/http-contracts").FinancialAdjustmentPaymentSourcesResponse>(
                `/document/treasury-withdrawal/reconciliation/financial-adjustments/payment/${assertUuidPathParam(paymentId, "paymentId")}/sources`,
            ),
        list: (status = "", paymentId = "") => {
            const params = new URLSearchParams()
            if (status) params.set("status", status)
            if (paymentId) params.set("paymentId", assertUuidPathParam(paymentId, "paymentId"))
            const qs = params.size ? `?${params}` : ""
            return apiFetch<import("@/types/generated/http-contracts").FinancialAdjustmentListResponse>(
                `/document/treasury-withdrawal/reconciliation/financial-adjustments${qs}`,
            )
        },
        get: (id: string) =>
            apiFetch<import("@/types/generated/http-contracts").FinancialAdjustmentResponse>(
                `/document/treasury-withdrawal/reconciliation/financial-adjustments/${assertUuidPathParam(id, "adjustmentId")}`,
            ),
        propose: (data: import("@/types/generated/http-contracts").ProposeFinancialAdjustmentRequest) =>
            apiFetch<import("@/types/generated/http-contracts").FinancialAdjustmentResponse>(
                "/document/treasury-withdrawal/reconciliation/financial-adjustments",
                { method: "POST", headers: idempotencyHeaders(), body: JSON.stringify(data) },
            ),
        approve: (id: string, reason: string) =>
            apiFetch<import("@/types/generated/http-contracts").FinancialAdjustmentResponse>(
                `/document/treasury-withdrawal/reconciliation/financial-adjustments/${assertUuidPathParam(id, "adjustmentId")}/approve`,
                { method: "POST", headers: idempotencyHeaders(), body: JSON.stringify({ reason }) },
            ),
        reject: (id: string, reason: string) =>
            apiFetch<import("@/types/generated/http-contracts").FinancialAdjustmentResponse>(
                `/document/treasury-withdrawal/reconciliation/financial-adjustments/${assertUuidPathParam(id, "adjustmentId")}/reject`,
                { method: "POST", headers: idempotencyHeaders(), body: JSON.stringify({ reason }) },
            ),
    },
    cryptoSweeps: {
        get: (id: string) =>
            apiFetch<import("@/types/generated/http-contracts").CryptoSweepDetailResponse>(
                `/document/crypto-sweep/${assertUuidPathParam(id, "id")}`,
            ),
        movements: (id: string) =>
            apiFetch<import("@/types/generated/http-contracts").DocumentMovementsResponse>(
                `/document/crypto-sweep/${assertUuidPathParam(id, "id")}/movements`,
            ),
        retry: (id: string) =>
            apiFetch<import("@/types/generated/http-contracts").SuccessResponse>(
                `/document/crypto-sweep/${assertUuidPathParam(id, "id")}/retry`,
                { method: "POST", headers: idempotencyHeaders() },
            ),
    },
    withdrawalRequests: {
        list: (params?: import("@/types/withdrawal-request").AdminWithdrawalRequestFilter) => {
            const entries: [string, string][] = []
            if (params?.status) entries.push(["status", params.status])
            if (params?.limit) entries.push(["limit", String(params.limit)])
            const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
            return apiFetch<import("@/types/withdrawal-request").AdminWithdrawalRequestListResponse>(
                `/document/withdrawal-request${qs}`
            )
        },
        get: (id: string) =>
            apiFetch<import("@/types/withdrawal-request").AdminWithdrawalRequestDetail>(
                `/document/withdrawal-request/${assertUuidPathParam(id, "id")}`,
            ),
        approve: (id: string) =>
            apiFetch<import("@/types/withdrawal-request").AdminApproveWithdrawalResponse>(
                `/document/withdrawal-request/${assertUuidPathParam(id, "id")}/approve`,
                { method: "POST", headers: idempotencyHeaders() },
            ),
        reject: (id: string, reason: string) =>
            apiFetch<{ status: string }>(
                `/document/withdrawal-request/${assertUuidPathParam(id, "id")}/reject`,
                { method: "POST", headers: idempotencyHeaders(), body: JSON.stringify({ reason }) },
            ),
        signingJob: (id: string) =>
            apiFetch<{ status: string; attempts: number; lastError: string }>(
                `/document/withdrawal-request/${assertUuidPathParam(id, "id")}/signing-job`,
            ),
        retrySigning: (id: string) =>
            apiFetch<{ status: string }>(
                `/document/withdrawal-request/${assertUuidPathParam(id, "id")}/retry-signing`,
                { method: "POST", headers: idempotencyHeaders() },
            ),
    },

    treasuryWithdrawals: {
        _basePath: "/document/treasury-withdrawal",
        list: (params?: CursorListParams) =>
            apiFetch<CursorListResponse<import("@/types/treasury").TreasuryWithdrawal>>(
                `/document/treasury-withdrawal${buildListQS(params)}`,
            ),
        get: (id: string) =>
            apiFetch<import("@/types/treasury").TreasuryWithdrawal>(
                `/document/treasury-withdrawal/${assertUuidPathParam(id, "id")}`,
            ),
        create: (data: import("@/types/treasury").CreateTreasuryWithdrawalRequest, idempotencyKey?: string) =>
            apiFetch<import("@/types/treasury").TreasuryWithdrawal>("/document/treasury-withdrawal", {
                method: "POST",
                headers: idempotencyHeaders(idempotencyKey),
                body: JSON.stringify(data),
            }),
        update: (id: string, data: import("@/types/treasury").UpdateTreasuryWithdrawalRequest) =>
            apiFetch<import("@/types/treasury").TreasuryWithdrawal>(
                `/document/treasury-withdrawal/${assertUuidPathParam(id, "id")}`,
                { method: "PUT", body: JSON.stringify(data) },
            ),
        delete: (id: string) =>
            apiFetch<void>(
                `/document/treasury-withdrawal/${assertUuidPathParam(id, "id")}`,
                { method: "DELETE" },
            ),
        addresses: {
            list: () =>
                apiFetch<{ items: import("@/types/treasury").TreasuryAddress[] }>(
                    "/document/treasury-withdrawal/addresses",
                ),
            create: (data: import("@/types/treasury").CreateTreasuryAddressRequest, idempotencyKey?: string) =>
                apiFetch<{ id: string }>("/document/treasury-withdrawal/addresses", {
                    method: "POST",
                    headers: idempotencyHeaders(idempotencyKey),
                    body: JSON.stringify(data),
                }),
        },
        availability: (tokenId: string) =>
            apiFetch<import("@/types/treasury").TreasuryAvailability>(
                `/document/treasury-withdrawal/availability?tokenId=${encodeURIComponent(tokenId)}`,
            ),
        sourceWallets: (tokenId: string) =>
            apiFetch<{ items: import("@/types/treasury").TreasurySourceWallet[] }>(
                `/document/treasury-withdrawal/source-wallets?tokenId=${encodeURIComponent(tokenId)}`,
            ),
        approve: (id: string, reason?: string, idempotencyKey?: string) =>
            apiFetch<{ message: string }>(`/document/treasury-withdrawal/${assertUuidPathParam(id, "id")}/approve`, {
                method: "POST",
                headers: idempotencyHeaders(idempotencyKey),
                body: JSON.stringify({ reason: reason || "" }),
            }),
        reject: (id: string, reason?: string, idempotencyKey?: string) =>
            apiFetch<{ message: string }>(`/document/treasury-withdrawal/${assertUuidPathParam(id, "id")}/reject`, {
                method: "POST",
                headers: idempotencyHeaders(idempotencyKey),
                body: JSON.stringify({ reason: reason || "" }),
            }),
        signingJob: (id: string) =>
            apiFetch<{ status: string; attempts: number; lastError: string }>(
                `/document/treasury-withdrawal/${assertUuidPathParam(id, "id")}/signing-job`,
            ),
        retrySigning: (id: string) =>
            apiFetch<{ status: string }>(
                `/document/treasury-withdrawal/${assertUuidPathParam(id, "id")}/retry-signing`,
                { method: "POST", headers: idempotencyHeaders() },
            ),
        reconciliation: {
            outflows: (params?: { status?: string; limit?: number }) => {
                const entries: [string, string][] = []
                if (params?.status) entries.push(["status", params.status])
                if (params?.limit) entries.push(["limit", String(params.limit)])
                const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
                return apiFetch<{ items: import("@/types/treasury").ExternalOutflow[] }>(
                    `/document/treasury-withdrawal/reconciliation/outflows${qs}`,
                )
            },
            getOutflow: (id: string) =>
                apiFetch<import("@/types/treasury").ExternalOutflowDetail>(
                    `/document/treasury-withdrawal/reconciliation/outflows/${assertUuidPathParam(id, "outflowId")}`,
                ),
            classifyOutflow: (id: string, body: import("@/types/treasury").ClassifyExternalOutflowRequest) =>
                apiFetch<import("@/types/treasury").ExternalOutflowDetail>(
                    `/document/treasury-withdrawal/reconciliation/outflows/${assertUuidPathParam(id, "outflowId")}/classify`,
                    { method: "POST", body: JSON.stringify(body) },
                ),
            resolveOutflow: (id: string, reason: string) =>
                apiFetch<import("@/types/treasury").ExternalOutflowDetail>(
                    `/document/treasury-withdrawal/reconciliation/outflows/${assertUuidPathParam(id, "outflowId")}/resolve`,
                    { method: "POST", body: JSON.stringify({ reason }) },
                ),
            approveCustodyAdjustment: (id: string, reason: string) =>
                apiFetch<import("@/types/treasury").CryptoCustodyAdjustment>(
                    `/document/treasury-withdrawal/reconciliation/custody-adjustments/${assertUuidPathParam(id, "adjustmentId")}/approve`,
                    { method: "POST", body: JSON.stringify({ reason }) },
                ),
            rejectCustodyAdjustment: (id: string, reason: string) =>
                apiFetch<import("@/types/treasury").CryptoCustodyAdjustment>(
                    `/document/treasury-withdrawal/reconciliation/custody-adjustments/${assertUuidPathParam(id, "adjustmentId")}/reject`,
                    { method: "POST", body: JSON.stringify({ reason }) },
                ),
        },
    },

    walletPools: {
        list: () =>
            apiFetch<import("@/types/wallet-pool").WalletPoolListResponse>(
                "/crypto/wallet-pools"
            ),
        updatePolicy: (
            networkId: string,
            policy: import("@/types/wallet-pool").WalletPoolPolicyPatch
        ) =>
            apiFetch<import("@/types/wallet-pool").WalletPoolState>(
                `/crypto/wallet-pools/${assertUuidPathParam(networkId, "networkId")}/policy`,
                {
                    method: "PATCH",
                    body: JSON.stringify(policy),
                }
            ),
        reconcile: (networkId: string) =>
            apiFetch<import("@/types/wallet-pool").WalletPoolReconcileResponse>(
                `/crypto/wallet-pools/${assertUuidPathParam(networkId, "networkId")}/reconcile`,
                {
                    method: "POST",
                    headers: idempotencyHeaders(),
                }
            ),
        getMerchantOverride: (networkId: string, merchantId: string) =>
            apiFetch<import("@/types/wallet-pool").MerchantWalletAllocationLimits>(
                `/crypto/wallet-pools/${assertUuidPathParam(networkId, "networkId")}/merchant-overrides/${assertUuidPathParam(merchantId, "merchantId")}`
            ),
        updateMerchantOverride: (
            networkId: string,
            merchantId: string,
            limits: import("@/types/wallet-pool").MerchantWalletAllocationLimitsUpdate
        ) =>
            apiFetch<import("@/types/wallet-pool").MerchantWalletAllocationLimits>(
                `/crypto/wallet-pools/${assertUuidPathParam(networkId, "networkId")}/merchant-overrides/${assertUuidPathParam(merchantId, "merchantId")}`,
                {
                    method: "PUT",
                    body: JSON.stringify(limits),
                }
            ),
    },

    cryptoNetworks: {
		rpcConfiguration: (networkId: string) =>
			apiFetch<import("@/types/generated/http-contracts").CryptoRPCConfigurationResponse>(
				`/admin/crypto/networks/${assertUuidPathParam(networkId, "networkId")}/rpc-configuration`,
			),
		updateRPCConfiguration: (networkId: string, data: import("@/types/generated/http-contracts").UpdateCryptoRPCConfigurationRequest, key: string) =>
			apiFetch<import("@/types/generated/http-contracts").CryptoRPCConfigurationResponse>(
				`/admin/crypto/networks/${assertUuidPathParam(networkId, "networkId")}/rpc-configuration`,
				{ method: "PATCH", headers: idempotencyHeaders(key), body: JSON.stringify(data) },
			),
        shadowStatus: (networkId: string) =>
            apiFetch<import("@/types/generated/http-contracts").CryptoShadowControlResponse>(
                `/admin/crypto/networks/${assertUuidPathParam(networkId, "networkId")}/shadow`
            ),
        setShadowPaused: (
            networkId: string,
            command: import("@/types/generated/http-contracts").SetCryptoShadowControlRequest,
            idempotencyKey: string,
        ) =>
            apiFetch<import("@/types/generated/http-contracts").CryptoShadowControlResponse>(
                `/admin/crypto/networks/${assertUuidPathParam(networkId, "networkId")}/shadow`,
                { method: "POST", headers: idempotencyHeaders(idempotencyKey), body: JSON.stringify(command) },
            ),
        list: () =>
            apiFetch<import("@/types/crypto-network").CryptoNetworkListResponse>(
                "/admin/crypto/networks"
            ),
        setWatching: (
            networkId: string,
            command: import("@/types/generated/http-contracts").SetCryptoNetworkWatchingRequest,
            idempotencyKey: string,
        ) =>
            apiFetch<import("@/types/crypto-network").CryptoNetworkState>(
                `/admin/crypto/networks/${assertUuidPathParam(networkId, "networkId")}/watching`,
                { method: "POST", headers: idempotencyHeaders(idempotencyKey), body: JSON.stringify(command) },
            ),
        status: (networkId: string) =>
            apiFetch<import("@/types/crypto-network").CryptoNetworkState>(
                `/admin/crypto/networks/${assertUuidPathParam(networkId, "networkId")}/status`
            ),
        updatePolicy: (
            networkId: string,
            policy: import("@/types/crypto-network").CryptoNetworkPolicyPatch
        ) =>
            apiFetch<import("@/types/crypto-network").CryptoNetworkState>(
                `/admin/crypto/networks/${assertUuidPathParam(networkId, "networkId")}/policy`,
                {
                    method: "PATCH",
                    headers: idempotencyHeaders(),
                    body: JSON.stringify(policy),
                }
            ),
    },
}
