import { expect, test } from "@playwright/test"
import { accountsWithoutKey, suggestedKeyName } from "../../components/integrations/rpc-key-name-field"

const accounts = [
  { id: "a", name: "A", hasCredentials: true, keys: ["nile", "sepolia"] },
  { id: "b", name: "B", hasCredentials: true, keys: ["nile"] },
  { id: "c", name: "C", hasCredentials: false, keys: [] },
]

test("a fixed key name always wins over the stored value", () => {
  expect(suggestedKeyName({ required: "sepolia", names: [], accounts }, "default")).toBe("sepolia")
  expect(suggestedKeyName({ required: "sepolia", names: [], accounts }, "sepolia")).toBeUndefined()
})

test("a single common key is preselected, otherwise the operator chooses", () => {
  expect(suggestedKeyName({ required: "", names: ["nile"], accounts }, "default")).toBe("nile")
  expect(suggestedKeyName({ required: "", names: ["nile"], accounts }, "nile")).toBeUndefined()
  expect(suggestedKeyName({ required: "", names: ["nile", "sepolia"], accounts }, "default")).toBeUndefined()
  expect(suggestedKeyName({ required: "", names: [], accounts }, "default")).toBeUndefined()
})

test("only keyed accounts are reported as missing a key", () => {
  expect(accountsWithoutKey({ required: "", names: ["nile"], accounts }, "sepolia")).toEqual(["B"])
  expect(accountsWithoutKey({ required: "", names: ["nile"], accounts }, "nile")).toEqual([])
})
