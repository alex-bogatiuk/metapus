import { test, expect } from "@playwright/test"
import { api } from "../../lib/api"
import { findHTTPContract } from "../../lib/http-contracts"
import {
  derivePortalNetworks,
  filterInvoiceTokensByNetwork,
  resolveInvoiceNetworkId,
  resolveInvoiceTokenId,
} from "../../configuration/crypto/lib/portal-invoice-state"
import { formatLocalYYYYMMDD } from "../../lib/filter-utils"
import { ids } from "../helpers"

const nativeFetch = globalThis.fetch
test.describe.configure({ mode: "serial" })
test.afterEach(() => { globalThis.fetch = nativeFetch })

test("portal route contracts distinguish HTTP method and scope while ignoring query parameters", () => {
  expect(findHTTPContract("portal", "POST", "/invoices?merchant_id=a")?.request).not.toBeNull()
  expect(findHTTPContract("portal", "GET", "/invoices/id?merchant_id=a")?.path).toBe("/invoices/:id")
  expect(findHTTPContract("api", "GET", "/invoices")).toBeUndefined()
  expect(findHTTPContract("portal", "GET", "/invoices/id/extra")).toBeUndefined()
})

test("portal validates bodies before sending and keeps human amount, false, zero and null intact", async () => {
  let calls = 0
  let body: RequestInit["body"]
  globalThis.fetch = async (_url, options) => {
    calls++; body = options?.body
    return Response.json({ defaultTtlMinutes: 0, singlePaymentPerInvoice: false })
  }
  const patch = { defaultTtlMinutes: null }
  await api.portal.settings.update(ids.merchant, patch)
  expect(body).toBe(JSON.stringify(patch))
  const bad = { tokenId: ids.token, amount: "9007199254740993.000000001", injected: true }
  await expect(api.portal.createInvoice(ids.merchant, bad)).rejects.toThrow("Unrecognized key")
  expect(calls).toBe(1)
  globalThis.fetch = async (_url, options) => {
    body = options?.body
    return Response.json({ object: "payment_link", id: ids.token, publicRef: "ref", payUrl: "https://example.test/ref" })
  }
  const request = { tokenId: ids.token, amount: "9007199254740993", reusable: false, maxUses: 0 }
  await api.portal.paymentLinks.create(ids.merchant, request)
  expect(body).toBe(JSON.stringify(request))
})

test("portal deposit-addresses validates request and response contracts with customerId", async () => {
  expect(findHTTPContract("portal", "GET", "/deposit-addresses?merchant_id=a")?.request).toBeNull()
  expect(findHTTPContract("portal", "POST", "/deposit-addresses?merchant_id=a")?.request).not.toBeNull()

  let sentBody: RequestInit["body"]
  globalThis.fetch = async (_url, options) => {
    sentBody = options?.body
    return Response.json({
      object: "deposit_address",
      id: ids.wallet,
      customerId: "cust_42",
      address: "T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb",
      networkId: ids.network,
      networkCode: "tron",
      networkName: "TRON",
      tokens: ["USDT", "TRX"],
      reused: false,
      createdAt: "2026-09-30T10:00:00Z",
    })
  }
  const created = await api.portal.depositAddresses.create(ids.merchant, {
    networkId: ids.network,
    customerId: "cust_42",
  })
  expect(sentBody).toBe(JSON.stringify({ networkId: ids.network, customerId: "cust_42" }))
  expect(created.customerId).toBe("cust_42")
})

test("invoice network and token selectors require explicit choice and scope by network", () => {
  const tokens = [
    { tokenId: "tok-tron-usdt", symbol: "USDT", networkId: "net-tron", network: "TRON" },
    { tokenId: "tok-tron-trx", symbol: "TRX", networkId: "net-tron", network: "TRON" },
    { tokenId: "tok-eth-usdt", symbol: "USDT", networkId: "net-eth", network: "Ethereum" },
  ]
  const networks = derivePortalNetworks(tokens)
  expect(networks).toEqual([
    { networkId: "net-tron", network: "TRON", symbols: ["USDT", "TRX"] },
    { networkId: "net-eth", network: "Ethereum", symbols: ["USDT"] },
  ])

  // Initially neither network nor token is auto-selected
  expect(resolveInvoiceNetworkId(networks, ids.merchant, null)).toBe("")
  expect(resolveInvoiceTokenId(tokens, ids.merchant, null)).toBe("")
  expect(filterInvoiceTokensByNetwork(tokens, "")).toEqual([])

  // Selecting a network unlocks its tokens without auto-picking a token
  const afterNetSelect = { merchantId: ids.merchant, networkId: "net-tron", tokenId: "" }
  expect(resolveInvoiceNetworkId(networks, ids.merchant, afterNetSelect)).toBe("net-tron")
  expect(resolveInvoiceTokenId(tokens, ids.merchant, afterNetSelect)).toBe("")
  expect(filterInvoiceTokensByNetwork(tokens, "net-tron").map((t) => t.tokenId)).toEqual([
    "tok-tron-usdt",
    "tok-tron-trx",
  ])

  // Token from another network is rejected
  expect(
    resolveInvoiceTokenId(tokens, ids.merchant, {
      merchantId: ids.merchant,
      networkId: "net-tron",
      tokenId: "tok-eth-usdt",
    })
  ).toBe("")

  // Valid token in selected network is resolved
  expect(
    resolveInvoiceTokenId(tokens, ids.merchant, {
      merchantId: ids.merchant,
      networkId: "net-tron",
      tokenId: "tok-tron-usdt",
    })
  ).toBe("tok-tron-usdt")
})

test("portal rejects numeric money and malformed list envelopes before returning to stores", async () => {
  globalThis.fetch = async () => Response.json({ totalInvoices: 0, paidInvoices: 0, pendingInvoices: 0, receiptTotals: [{tokenId: ids.wallet, networkId: ids.merchant, symbol: "ETH", decimalPlaces: 18, totalMinor: 42, received24hMinor: "0", change24hPct: "0.00"}] })
  await expect(api.portal.summary(ids.merchant)).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ items: [null], total: 1 })
  await expect(api.portal.invoices()).rejects.toThrow()
  await expect(api.merchantApiKeys.list(ids.merchant)).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ items: [], total: 0 })
  expect(await api.portal.invoices()).toEqual({ items: [], total: 0 })
})

test("webhook response retains unknown status separately from missing delivery and zero duration", async () => {
  for (const response of [{ eventId: "event", success: false },
    { eventId: "event", success: false, deliveryId: "delivery", statusCode: null, durationMs: 0 }]) {
    globalThis.fetch = async () => Response.json(response)
    expect(await api.portal.webhookEndpoints.test(ids.merchant, ids.wallet)).toEqual(response)
  }
  globalThis.fetch = async () => Response.json({ eventId: "event", success: "false" })
  await expect(api.portal.webhookEndpoints.test(ids.merchant, ids.wallet)).rejects.toThrow()
})

test("portal invoice date formatting preserves local calendar date without UTC offset shift", () => {
  const oct1 = new Date(2026, 9, 1, 0, 0, 0)
  expect(formatLocalYYYYMMDD(oct1)).toBe("2026-10-01")
  const oct31 = new Date(2026, 9, 31, 23, 59, 59)
  expect(formatLocalYYYYMMDD(oct31)).toBe("2026-10-31")
})

