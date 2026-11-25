import { expect, test } from "@playwright/test"
import { rolesForFamily, suggestedRole } from "../../configuration/crypto/components/integrations/rpc-endpoint-fields"

test("a network offers only the purposes of its family", () => {
  expect(rolesForFamily("evm")).toEqual(["rpc"])
  expect(rolesForFamily("tron")).toEqual(["fullnode", "solidity", "indexer"])
  expect(rolesForFamily("ton")).toEqual([])
})

test("a single purpose is filled in and a foreign one is cleared", () => {
  expect(suggestedRole(["rpc"], "")).toBe("rpc")
  expect(suggestedRole(["rpc"], "fullnode")).toBe("rpc")
  expect(suggestedRole(["rpc"], "rpc")).toBeUndefined()
  expect(suggestedRole(["fullnode", "solidity"], "rpc")).toBe("")
  expect(suggestedRole(["fullnode", "solidity"], "")).toBeUndefined()
  expect(suggestedRole(["fullnode", "solidity"], "solidity")).toBeUndefined()
  expect(suggestedRole([], "rpc")).toBeUndefined()
})
