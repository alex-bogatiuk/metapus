import { test, expect } from "@playwright/test"
import { registerFromMetadata } from "../../lib/entity-registry-defaults"
import { entityRegistry } from "../../lib/entity-registry"
import { api } from "../../lib/api"
import { transitionScope } from "../../lib/auth-scope"
import { entityMetadataSchema, entitySummarySchema, filterFieldMetadataSchema } from "../../lib/schemas/metadata"
import { parseEntityValues } from "../../lib/schemas/entity-values"
import { useMetadataStore } from "../../stores/useMetadataStore"

const metadata = { key: "extension", name: "Extension", type: "catalog", presentation: { singular: "Карточка", plural: "Карточки" },
  fields: [{ name: "name", type: "string" }, { name: "amount", type: "money" }, { name: "active", type: "boolean" }],
  tableParts: [{ name: "lines", columns: [{ name: "amount", type: "money" }] }] }

test.describe.configure({ mode: "serial" })
const nativeFetch = globalThis.fetch
test.afterEach(() => { globalThis.fetch = nativeFetch })

test("metadata and recursive filter contracts reject malformed types, actions and capabilities", () => {
  expect(entityMetadataSchema.parse(metadata).fields).toHaveLength(3)
  expect(entityMetadataSchema.safeParse({ ...metadata, fields: [{ name: "amount", type: "unsupported" }] }).success).toBe(false)
  expect(entitySummarySchema.safeParse({ ...metadata, actions: [{ key: "run", label: "Run", method: "POST", path: "/:id/run", scope: "unsafe" }] }).success).toBe(false)
  expect(entitySummarySchema.safeParse({ ...metadata, capabilities: { read: "true" } }).success).toBe(false)
  expect(filterFieldMetadataSchema.safeParse({ key: "currency", label: "Currency", fieldType: "reference", refFields: [{ key: "amount", label: "Amount", fieldType: "money", valueScale: "100" }] }).success).toBe(false)
})

test("invalid metadata never reaches caches and subsequent requests can retry", async () => {
  transitionScope({ tenantId: "metadata-test", principalId: "user", sessionId: "session", userVersion: "1", policyVersion: "1" })
  let calls = 0
  globalThis.fetch = async () => { calls++; return Response.json({ ...metadata, fields: [{ name: "amount", type: {} }] }) }
  await expect(api.meta.getEntity("Extension")).rejects.toThrow()
  globalThis.fetch = async () => { calls++; return Response.json(metadata) }
  await expect(api.meta.getEntity("Extension")).resolves.toMatchObject(metadata)
  await api.meta.getEntity("Extension")
  expect(calls).toBe(2)
  globalThis.fetch = async () => Response.json([{ ...metadata, type: "not-an-entity" }])
  await useMetadataStore.getState().fetch()
  expect(useMetadataStore.getState().loaded).toBe(false)
  expect(useMetadataStore.getState().entities).toEqual([])
})

test("AutoForm validates monetary headers and lines before publishing or sending", () => {
  const meta = entityMetadataSchema.parse(metadata)
  const valid = { id: "card", version: 1, name: "Name", amount: "9007199254740993", active: false,
    lines: [{ amount: "-9223372036854775808" }], attributes: { amount: "1" } }
  expect(parseEntityValues(meta, valid)).toEqual(valid)
  // Attributes are a free JSON object: no metadata types its keys, but its shape is checked.
  expect(parseEntityValues(meta, { ...valid, attributes: { amount: 123 } }).attributes).toEqual({ amount: 123 })
  for (const value of [{ ...valid, amount: 123 }, { ...valid, active: "false" }, { ...valid, name: {} },
    { ...valid, lines: [null] }, { ...valid, lines: [{ amount: 123 }] }, { ...valid, attributes: ["amount"] },
    { ...valid, lines: [{ amount: "9223372036854775808" }] }]) {
    expect(() => parseEntityValues(meta, value)).toThrow()
    expect(() => parseEntityValues(meta, value, "request")).toThrow()
  }
  expect(() => parseEntityValues(meta, { ...valid, version: "1" })).toThrow()
  expect(parseEntityValues(meta, { id: "card", version: 1, amount: null })).toEqual({ id: "card", version: 1, amount: null })
})

test("CryptoResourceRental normalizes resource amounts to integer and accepts numeric values", () => {
  const rentalMeta = entityMetadataSchema.parse({
    key: "crypto_resource_rental",
    name: "CryptoResourceRental",
    type: "document",
    presentation: { singular: "Аренда", plural: "Аренды" },
    fields: [
      { name: "requestedAmount", label: "Запрошено", type: "money" },
      { name: "delegatedAmount", label: "Делегировано", type: "money" },
    ],
  })
  expect(rentalMeta.fields.find(f => f.name === "requestedAmount")?.type).toBe("integer")
  const valid = { id: "rr-1", version: 1, requestedAmount: 65000, delegatedAmount: 65000 }
  expect(parseEntityValues(rentalMeta, valid)).toEqual(valid)
})


test("discovered entities keep their canonical key without a manual UI overlay", async () => {
  transitionScope({ tenantId: "discovery-test", principalId: "user", sessionId: "discovery", userVersion: "1", policyVersion: "1" })
  const entity = entitySummarySchema.parse({ key: "vehicle", name: "Vehicle", type: "catalog", routePrefix: "vehicles",
    presentation: { singular: "Автомобиль", plural: "Автомобили" } })
  useMetadataStore.setState({ entities: [entity], loaded: true })
  registerFromMetadata()
  const registration = entityRegistry.getByRoute("vehicles")
  expect(registration?.entityKey).toBe("vehicle")
  let requested = ""
  globalThis.fetch = async (input) => { requested = String(input); return Response.json([]) }
  await api.meta.getFilters(registration!.entityKey!)
  expect(requested).toContain("/meta/vehicle/filters")
})
