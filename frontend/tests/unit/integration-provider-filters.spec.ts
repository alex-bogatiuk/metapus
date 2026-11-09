import { expect, test } from "@playwright/test"
import { integrationListParams } from "../../lib/integrations/provider-filters"
import definitions from "../fixtures/integration-providers.json"
import { buildFilterItems } from "../../lib/filter-utils"

test("the metadata filter pipeline uses the same purpose predicate as the list fetcher", () => {
  const filter = buildFilterItems({ integrationKind: { operator: "eq", value: "exchange_rates" } }, [
    { key: "integrationKind", label: "Назначение", fieldType: "enum" },
  ])
  expect(filter[0].field).toBe("integration_kind")
  expect(integrationListParams({ filter }, definitions.providers)?.filter)
    .toEqual([{ field: "provider", operator: "in", value: ["coingecko_demo"] }])
})

test("purpose predicates are expanded before pagination, preserving other filters and cursor", () => {
  const input = { after: "cursor", limit: 25, orderBy: "name", filter: [
    { field: "integrationKind", operator: "eq" as const, value: "exchange_rates" },
    { field: "isActive", operator: "eq" as const, value: true },
  ] }
  expect(integrationListParams(input, definitions.providers)).toEqual({ ...input, filter: [
    { field: "provider", operator: "in", value: ["coingecko_demo"] },
    input.filter[1],
  ] })
  expect(input.filter[0].field).toBe("integrationKind")
})

test("a profile supporting several purposes stays one account and matches either purpose", () => {
  const provider = { ...definitions.providers[0], kinds: ["rpc", "exchange_rates"] }
  for (const kind of provider.kinds) {
    expect(integrationListParams({ filter: [{ field: "integrationKind", operator: "eq", value: kind }] }, [provider])?.filter)
      .toEqual([{ field: "provider", operator: "in", value: [provider.id] }])
  }
  expect(integrationListParams({ filter: [{ field: "integrationKind", operator: "neq", value: "rpc" }] }, [provider])?.filter)
    .toEqual([{ field: "provider", operator: "in", value: [] }])
})

test("unknown purposes produce an empty match and unsupported comparisons are rejected", () => {
  expect(integrationListParams({ filter: [{ field: "integrationKind", operator: "eq", value: "missing" }] }, definitions.providers)?.filter)
    .toEqual([{ field: "provider", operator: "in", value: [] }])
  expect(() => integrationListParams({ filter: [{ field: "integrationKind", operator: "contains", value: "rpc" }] }, definitions.providers)).toThrow()
})
