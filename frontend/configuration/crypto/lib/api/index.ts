/**
 * API client of the crypto-processing configuration: its catalogs, treasury
 * and receipt operations, merchant portal and merchant administration. The
 * platform client (@/lib/api) carries only platform namespaces.
 *
 *   import { cryptoApi } from "@/configuration/crypto/lib/api"
 *   const tokens = await cryptoApi.tokens.list()
 */
import { cryptoCatalogsApi } from "./catalogs"
import { cryptoOperationsApi } from "./crypto"
import { dashboardApi } from "./dashboard"
import { portalApi, merchantApiKeysApi, merchantUsersApi, feeScheduleApi, merchantFeeScheduleApi } from "./portal"
import { rpcKeysApi } from "./rpc-keys"

export const cryptoApi = {
    ...cryptoCatalogsApi,
    ...cryptoOperationsApi,
    dashboard: dashboardApi,
    portal: portalApi,
    merchantApiKeys: merchantApiKeysApi,
    merchantUsers: merchantUsersApi,
    feeSchedule: feeScheduleApi,
    merchantFeeSchedule: merchantFeeScheduleApi,
    rpcKeys: rpcKeysApi,
} as const
