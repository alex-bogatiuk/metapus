import { test, expect } from "@playwright/test"
import { reachableStatuses, withReachableStatuses } from "../../lib/document-status"

const model = {
  field: "status", initial: "new",
  values: [{ value: "new", label: "Новая" }, { value: "work", label: "В работе" }, { value: "closed", label: "Закрыта" }],
  transitions: [
    { from: ["new"], to: "work" },
    { to: "closed" },
    { from: ["closed"], to: "work", permission: "document:ticket:reopen" },
  ],
}

test("a document is offered its own status and those a transition reaches", () => {
  expect(reachableStatuses(model, "new")).toEqual(["new", "work", "closed"])
  expect(reachableStatuses(model, "work"), "no transition leads back to new").toEqual(["work", "closed"])
  expect(reachableStatuses(model, "closed"), "a transition with a permission is offered; the server checks it").toEqual(["work", "closed"])
  expect(reachableStatuses(model, undefined), "a new document starts in the initial status").toEqual(["new", "work", "closed"])
  expect(reachableStatuses({ ...model, transitions: undefined }, "closed"), "without transitions every status is open").toEqual(["new", "work", "closed"])
})

test("only the status field's choices are narrowed", () => {
  const status = { name: "status", type: "enum" as const, enumValues: model.values }
  expect(withReachableStatuses(status, model, "work").enumValues?.map((v) => v.value)).toEqual(["work", "closed"])
  const other = { name: "kind", type: "enum" as const, enumValues: model.values }
  expect(withReachableStatuses(other, model, "work")).toBe(other)
  expect(withReachableStatuses(status, undefined, "work")).toBe(status)
})
