import { expect, test } from "@playwright/test"
import { credentialProfile as getProfile, emptyCredentialKeys as getEmptyKeys, expiryInputValue, expiryRequestValue, nextQuotaPeriod, normalizeCredentialKey, prepareCredentialKeys as prepareKeys, type CredentialKey } from "../../lib/integrations/account-settings"
import type { IntegrationProviderResponse } from "../../types/generated/http-contracts"

import definitions from "../fixtures/integration-providers.json"

const credentialProfile = (provider: string) => getProfile(provider, definitions.providers)
const emptyCredentialKeys = (provider: string) => getEmptyKeys(provider, definitions.providers)
const prepareCredentialKeys = (provider: string, keys: readonly CredentialKey[]) => prepareKeys(provider, keys, definitions.providers)

const normalizeInfuraKey = (raw: string) => normalizeCredentialKey(raw, "sepolia", credentialProfile("infura"))
const normalizeGetBlockToken = (raw: string, role: string) => normalizeCredentialKey(raw, role, credentialProfile("getblock"))

test("a new provider uses credential metadata without adding frontend branches", () => {
  const provider: IntegrationProviderResponse = {
    id: "future-provider", label: "Future", description: "", kinds: ["rpc"], canCreate: true, quotaPolicy: "monthly",
    credentials: {
      fields: [{ name: "execution", label: "API token", description: "" }], replaceLabel: "Replace", description: "", inputFormat: "",
      keyFormat: "url_safe", requireFields: true, singleKey: true,
      urlImports: [{ host: "rpc.future.test", pathPrefix: "/execution" }],
    },
  }
  expect(prepareKeys(provider.id, [{ name: "execution", value: "https://rpc.future.test/execution/new-token" }], [provider]))
    .toEqual({ keys: [{ name: "execution", value: "new-token" }], errors: {} })
  expect(prepareKeys(provider.id, [{ name: "execution", value: "https://foreign.test/execution/new-token" }], [provider]).errors).toHaveProperty("key-value-0")
  expect(prepareKeys(provider.id, [{ name: "wrong-slot", value: "new-token" }], [provider]).errors).toHaveProperty("credentials")
})

test("Infura extracts only a Sepolia key and rejects other origins and URL parameters", () => {
  const key = "0123456789abcdef0123456789abcdef"
  expect(emptyCredentialKeys("infura")).toEqual([{ name: "sepolia", value: "" }])
  expect(normalizeInfuraKey(key)).toEqual({ ok: true, value: key, fromUrl: false })
  expect(normalizeInfuraKey(`https://sepolia.infura.io/v3/${key}`)).toEqual({ ok: true, value: key, fromUrl: true })
  expect(prepareCredentialKeys("infura", [{ name: "sepolia", value: `https://sepolia.infura.io/v3/${key}` }])).toEqual({ keys: [{ name: "sepolia", value: key }], errors: {} })
  for (const raw of ["", "not-a-key", `https://mainnet.infura.io/v3/${key}`, `https://sepolia.infura.io.evil.test/v3/${key}`, `http://sepolia.infura.io/v3/${key}`, `https://sepolia.infura.io/v3/${key}?`, `https://user@sepolia.infura.io/v3/${key}`, `https://sepolia.infura.io/v3/${key}/extra`]) expect(normalizeInfuraKey(raw).ok).toBe(false)
  expect(prepareCredentialKeys("infura", emptyCredentialKeys("infura")).errors).toHaveProperty("key-value-0")
})

test("GetBlock provides fixed role slots and preserves generic provider slots", () => {
  expect(emptyCredentialKeys("getblock")).toEqual([{ name: "fullnode", value: "" }, { name: "solidity", value: "" }])
  expect(credentialProfile("getblock")?.fields.map(field => field.label)).toEqual(["Токен FullNode REST", "Токен Solidity REST"])
  expect(emptyCredentialKeys("chainstack")).toEqual([{ name: "default", value: "" }])
})

test("GetBlock extracts only tokens from supported REST URLs and rejects ambiguous addresses", () => {
  for (const [input, role] of [
    [" demo-token ", "fullnode"],
    ["https://shared.eu-central-1.getblock.io/demo-token/", "fullnode"],
    ["https://shared.eu-central-1.getblock.io/demo-token/wallet/getnowblock", "fullnode"],
    ["https://shared.eu-central-1.getblock.io/demo-token/walletsolidity/getnowblock", "solidity"],
  ]) expect(normalizeGetBlockToken(input, role)).toMatchObject({ ok: true, value: "demo-token" })
  for (const input of [
    "", "two tokens", "http://shared.eu-central-1.getblock.io/token/",
    "https://shared.eu-central-1.getblock.io.attacker.test/token/",
    "https://shared.us-east-1.getblock.io/token/", "https://go.getblock.io/token/",
    "https://user:password@shared.eu-central-1.getblock.io/token/",
    "https://shared.eu-central-1.getblock.io/token/?api=token",
    "https://shared.eu-central-1.getblock.io/token/#fragment",
    "https://shared.eu-central-1.getblock.io/token/?",
    "https://shared.eu-\ncentral-1.getblock.io/token/",
    "https://shared.eu-central-1.getblock.io/token/unexpected",
    "https://shared.eu-central-1.getblock.io/token/walletsolidity/getnowblock",
  ]) expect(normalizeGetBlockToken(input, "fullnode").ok).toBe(false)
})

test("credential validation requires both GetBlock roles and normalizes only GetBlock values", () => {
  expect(prepareCredentialKeys("getblock", [{ name: "fullnode", value: "token" }]).errors).toHaveProperty("credentials")
  expect(prepareCredentialKeys("getblock", emptyCredentialKeys("getblock")).errors).toHaveProperty("key-value-1")
  const values = [{ name: "fullnode", value: "https://shared.eu-central-1.getblock.io/full-token/" }, { name: "solidity", value: "solid-token" }]
  expect(prepareCredentialKeys("getblock", values)).toEqual({ keys: [{ name: "fullnode", value: "full-token" }, { name: "solidity", value: "solid-token" }], errors: {} })
  expect(prepareCredentialKeys("alchemy", [{ name: "default", value: "opaque/value" }]).keys[0].value).toBe("opaque/value")
  expect(prepareCredentialKeys("chainstack", [{ name: "nile", value: "https://host/token" }]).errors).toHaveProperty("key-value-0")
})

for (const [now, day, zone, expected] of [
  ["2026-10-19T10:00:00Z", 1, "UTC", "1 ноября 2026, 00:00"],
  ["2026-10-05T10:00:00Z", 15, "UTC", "15 октября 2026, 00:00"],
  ["2026-02-27T10:00:00Z", 31, "UTC", "28 февраля 2026, 00:00"],
  ["2026-02-28T00:00:00Z", 31, "UTC", "31 марта 2026, 00:00"],
  ["2024-02-28T23:59:59Z", 31, "UTC", "29 февраля 2024, 00:00"],
  ["2026-10-31T22:00:00Z", 1, "Europe/Moscow", "1 декабря 2026, 00:00"],
  ["2026-03-20T10:00:00Z", 1, "America/New_York", "1 апреля 2026, 00:00"],
  ["2026-12-31T12:00:00Z", 1, "UTC", "1 января 2027, 00:00"],
] as const) test(`quota preview matches local calendar: ${now}, ${day}, ${zone}`, () => {
  expect(nextQuotaPeriod(day, zone, new Date(now))).toBe(expected)
})

test("invalid quota settings do not display a plausible date", () => {
  for (const day of [0, 32, 1.5, NaN]) expect(nextQuotaPeriod(day, "UTC")).toBeNull()
  expect(nextQuotaPeriod(1, "Not/A_Zone")).toBeNull()
})

test("Getblock.net uses one separate bearer credential", () => {
  expect(emptyCredentialKeys("getblock_net")).toEqual([{ name: "default", value: "" }])
  expect(prepareCredentialKeys("getblock_net", [{ name: "default", value: "opaque.bearer-token" }]).errors).toEqual({})
  for (const value of ["https://getblock.net/token", "Bearer token", "token\tvalue", "token\u0000value"]) {
    expect(prepareCredentialKeys("getblock_net", [{ name: "default", value }]).errors).toHaveProperty("key-value-0")
  }
  expect(prepareCredentialKeys("getblock_net", [{ name: "fullnode", value: "rpc-key" }]).errors).toHaveProperty("credentials")
  expect(prepareCredentialKeys("getblock_net", [{ name: "default", value: "one" }, { name: "extra", value: "two" }]).errors).toHaveProperty("credentials")
})


test("dRPC accepts one Sepolia token or its supported URL through metadata", () => {
  expect(emptyCredentialKeys("drpc")).toEqual([{ name: "sepolia", value: "" }])
  expect(prepareCredentialKeys("drpc", [{ name: "sepolia", value: "https://lb.drpc.live/sepolia/test-token_123" }]))
    .toEqual({ keys: [{ name: "sepolia", value: "test-token_123" }], errors: {} })
  for (const value of ["https://foreign.test/sepolia/key", "https://lb.drpc.live/sepolia/key?x=1", "https://lb.drpc.live/mainnet/key", "key/extra", "two keys"]) {
    expect(prepareCredentialKeys("drpc", [{ name: "sepolia", value }]).errors).toHaveProperty("key-value-0")
  }
  expect(prepareCredentialKeys("drpc", [{ name: "default", value: "key" }]).errors).toHaveProperty("credentials")
})

test("manual expiry preserves a UTC instant across offsets and DST dates", () => {
  for (const iso of ["2026-11-15T00:00:00+05:00", "2026-03-08T01:30:00-05:00", "2026-11-01T01:30:00-04:00", "2026-11-01T01:30:00-05:00"]) {
    expect(expiryRequestValue(expiryInputValue(iso))).toBe(new Date(iso).toISOString())
  }
  expect(expiryRequestValue("2026-11-14T19:00")).toBe("2026-11-14T19:00:00.000Z")
  for (const value of ["", "2026-02-30T00:00", "2026-13-01T00:00", "2026-01-01T24:00", "2026-01-01T00:60", "2026-01-01", "2026-01-01T00:00Z"]) expect(expiryRequestValue(value)).toBeNull()
  expect(expiryInputValue(null)).toBe("")
  expect(expiryInputValue("bad")).toBe("")
})
