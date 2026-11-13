import type { Page, Route } from "@playwright/test"
import type { AuthUserResponse } from "../types/auth"

export const ids = {
  user: "10000000-0000-4000-8000-000000000001",
  merchant: "10000000-0000-4000-8000-000000000002",
  token: "10000000-0000-4000-8000-000000000003",
  network: "10000000-0000-4000-8000-000000000004",
  wallet: "10000000-0000-4000-8000-000000000005",
  address: "10000000-0000-4000-8000-000000000006",
  outflow: "10000000-0000-4000-8000-000000000007",
  adjustment: "10000000-0000-4000-8000-000000000008",
}

export const erpUser: AuthUserResponse = {
  id: ids.user,
  email: "reconciliation@example.test",
  fullName: "Reconciliation Checker",
  isActive: true,
  isAdmin: false,
  emailVerified: true,
  roles: [{ id: "10000000-0000-4000-8000-000000000009", code: "reconciliation", name: "Reconciliation", isSystem: false }],
  merchantIds: [],
  mfaEnabled: true,
  mfaPending: false,
  mfaRequired: false,
  createdAt: "2026-01-01T00:00:00Z",
}

export const portalOwner: AuthUserResponse = {
  ...erpUser,
  email: "owner@example.test",
  fullName: "Merchant Owner",
  roles: [],
  merchantIds: [ids.merchant],
}

export const portalRoles = {
  owner: 1,
  manager: 2,
  viewer: 3,
} as const

export async function installSession(page: Page, user: AuthUserResponse = erpUser) {
  await page.addInitScript(({ persistedUser }) => {
    const cacheScope = { tenantId: "e2e-tenant", principalId: persistedUser.id, sessionId: "10000000-0000-4000-8000-000000000010", userVersion: "1", policyVersion: "1" }
    localStorage.setItem("metapus-cache-scope", JSON.stringify(Object.values(cacheScope)))
    localStorage.setItem("metapus-auth", JSON.stringify({
      state: {
        tokens: { accessToken: "e2e-access-token", expiresAt: "2099-01-01T00:00:00Z", tokenType: "Bearer", cacheScope },
        cacheScope,
        user: persistedUser,
        isAuthenticated: true,
        originalSession: null,
        isImpersonating: false,
      },
      version: 0,
    }))
  }, { persistedUser: user })
}

export async function installPortalSession(page: Page) {
  await installSession(page, portalOwner)
  await page.addInitScript(({ merchantId }) => {
    localStorage.setItem("metapus-portal", JSON.stringify({
      state: { activeMerchantId: merchantId },
      version: 0,
    }))
  }, { merchantId: ids.merchant })
}

export function json<T>(route: Route, body: T, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  })
}

export function commonAPIResponse(pathname: string, user: AuthUserResponse = erpUser) {
  if (pathname === "/api/v1/auth/me") return user
  if (pathname === "/api/v1/me/preferences") {
    return { userId: user.id, interface: {}, listFilters: {}, listColumns: {}, dashboardLayout: null, favorites: [], updatedAt: "2026-09-12T00:00:00Z" }
  }
  if (pathname === "/api/v1/meta/entities") return []
  // Every authenticated page asks for these in the background. A body that fails their schema
  // makes the stores log console.error, which the Next.js dev overlay counts as issues.
  // An empty ticket is valid and keeps the client from opening a socket to a server that is not there.
  if (pathname === "/api/v1/auth/ws-ticket") return { ticket: "" }
  if (pathname === "/api/v1/system/notifications") return { items: [], unreadCount: 0 }
  return undefined
}

// Complete generic Token response; fake partial records hide wire-contract regressions.
export const tokenRecord = {
 id: ids.token, code: "TOKEN", name: "Token", networkId: ids.network, contractAddress: "",
 symbol: "TKN", decimalPlaces: 9, tokenStandard: "native", isActive: true,
 sweepThreshold: "0", sweepMaxAgeHours: 24, sweepRetainAmount: "0", isFolder: false,
 deletionMark: false, version: 1,
} satisfies import("../types/generated/http-contracts").CryptoTokenResponse
export const tokenRequiredFields = [
 { name: "networkId", type: "string" }, { name: "symbol", type: "string" }, { name: "tokenStandard", type: "string" },
]

export function unvaluedPortalBalance(assets: { tokenId: string; symbol: string }[]): import("../types/generated/http-contracts").PortalBalanceResponse {
  const bucket = { amount: null, coverage: "none", unvaluedCount: assets.length }
  return {
    baseCurrency: { id: ids.network, code: "USD", name: "US Dollar", decimalPlaces: 2 },
    source: null, evaluatedAt: "2026-09-20T12:00:00Z", freshUntil: null,
    valuation: { total: bucket, pending: bucket, available: bucket },
    byToken: assets.map(asset => ({
      tokenId: asset.tokenId, tokenSymbol: asset.symbol, currencyCode: asset.symbol, decimalPlaces: 6,
      rawAmount: "100000000", humanAmount: "100", pendingRaw: "0", pendingHuman: "0", availableRaw: "100000000", availableHuman: "100",
      valuation: { status: "unavailable", reason: "no_rate", selectedQuote: null, lastKnownQuote: null, freshUntil: null, total: null, pending: null, available: null },
    })),
  }
}
