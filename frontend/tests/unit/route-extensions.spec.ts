// An add-on's route table merges its fields of a base catalog into the
// base's routes (extends): «Автобизнес» adds the brand of an item.
import { expect, test } from "@playwright/test"
import { httpRoutes as tradeRoutes } from "../../configuration/trade/lib/generated/http-contract-routes"
import { httpRoutes as automotiveRoutes } from "../../configuration/automotive/lib/generated/http-contract-routes"
import { findHTTPContract, projectFormRequest, registerHTTPRoutes, validateHTTPBody } from "../../lib/http-contracts"
import type { HTTPRouteContract } from "../../lib/http-contract-types"

test.describe.configure({ mode: "serial" })

test("an extension route needs its base route", () => {
  const orphan: HTTPRouteContract[] = [{ scope: "api", method: "POST", path: "/catalog/no-such-catalog", request: null, requestFields: null, response: null, optionalRequest: false, extends: true }]
  expect(() => registerHTTPRoutes(orphan)).toThrow("extends a route that is not registered")
})

test("the add-on's fields join the base's create and update requests", () => {
  registerHTTPRoutes(tradeRoutes)
  registerHTTPRoutes(automotiveRoutes)
  const draft = { code: "IT-1", name: "Фильтр", article: "W 712/75", manufacturer: "", unit: "шт", brand: "MANN", normalizedArticle: "W71275", attributes: {} }
  const created = projectFormRequest("POST", "/catalog/items", draft)
  expect(created.brand).toBe("MANN")
  expect(created.article).toBe("W 712/75")
  expect(created).not.toHaveProperty("normalizedArticle")
  expect(projectFormRequest("PUT", "/catalog/items/0196", { ...draft, version: 3 }).brand).toBe("MANN")

  const contract = findHTTPContract("api", "POST", "/catalog/items")!
  expect(() => validateHTTPBody(contract, JSON.stringify({ name: "Фильтр", brand: 7 }))).toThrow()
  expect(() => validateHTTPBody(contract, JSON.stringify({ name: "Фильтр", injected: true }))).toThrow()
  expect(() => validateHTTPBody(contract, JSON.stringify({ name: "Фильтр", brand: "MANN" }))).not.toThrow()
  expect(contract.extends).toBeUndefined()
})

test("the response carries the add-on's fields", () => {
  const read = findHTTPContract("api", "GET", "/catalog/items/0196")!
  const item = { id: "0196", version: 1, deletionMark: false, code: "IT-1", name: "Фильтр", isFolder: false, attributes: {}, article: "W 712/75", manufacturer: "", unit: "шт" }
  expect(() => read.response!.parse({ ...item, brand: "MANN", normalizedArticle: "W71275" })).not.toThrow()
  expect(() => read.response!.parse(item)).toThrow()
})
