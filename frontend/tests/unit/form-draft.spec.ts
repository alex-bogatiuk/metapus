import { test, expect } from "@playwright/test"
import { beginRead, editForm, emptyForm, receiveRead, receiveSave, saveTicket } from "../../lib/form-draft"

function loaded() {
  const [state, ticket] = beginRead(emptyForm())
  return receiveRead(state, { id: "A", name: "Server", version: 1, details: { city: "Moscow" } }, ticket)
}
test("remount/background GET keeps dirty input and reports a changed server version", () => {
  const draft = editForm(loaded(), "name", "Draft")
  const [waiting, ticket] = beginRead(draft)
  const response = receiveRead(waiting, { id: "A", name: "Other author", version: 2 }, ticket)
  expect(response.draft.name).toBe("Draft")
  expect(response.baseVersion).toBe(1)
  expect(response.serverSnapshot?.version).toBe(2)
  expect(response.conflict).toBe(true)
  expect(response.dirty).toBe(true)
})
test("clean GET does not overwrite input typed and reverted while the request was running", () => {
  const [waiting, ticket] = beginRead(loaded())
  const changed = editForm(editForm(waiting, "name", "Typed"), "name", "Server")
  expect(changed.dirty).toBe(false)
  const response = receiveRead(changed, { name: "New server", version: 2 }, ticket)
  expect(response.draft.name).toBe("Server")
  expect(response.conflict).toBe(true)
})
test("explicit reload replaces a dirty draft only if no later input occurred", () => {
  const [waiting, ticket] = beginRead(editForm(loaded(), "name", "Draft"))
  const replacement = { name: "Reloaded", version: 3 }
  expect(receiveRead(waiting, replacement, ticket, true).dirty).toBe(false)
  const response = receiveRead(editForm(waiting, "name", "New input"), replacement, ticket, true)
  expect(response.draft.name).toBe("New input")
  expect(response.dirty).toBe(true)
})
test("save acknowledges sent fields and preserves only subsequent edits with the new version", () => {
  const draft = editForm(editForm(loaded(), "name", "Sent"), "password", "one-time-secret")
  const ticket = saveTicket(draft)
  const duringSave = editForm(draft, "details", { city: "Kazan" })
  const response = receiveSave(duringSave, { id: "A", name: "Sent", version: 2, details: { city: "Moscow" } }, ticket)
  expect(response.draft).toEqual({ id: "A", name: "Sent", version: 2, details: { city: "Kazan" } })
  expect(response.baseVersion).toBe(2)
  expect(response.dirty).toBe(true)
  expect(response.conflict).toBe(false)
  const saved = receiveSave(response, response.draft, saveTicket(response))
  expect(saved.dirty).toBe(false)
})
test("old GET cannot revert a save, and a later GET wins over an earlier response", () => {
  const [waiting, oldRead] = beginRead(loaded())
  const saved = receiveSave(waiting, { name: "Saved", version: 2 }, saveTicket(waiting))
  expect(receiveRead(saved, { name: "Old", version: 1 }, oldRead)).toBe(saved)
  const [first, t1] = beginRead(saved)
  const [second, t2] = beginRead(first)
  const newest = receiveRead(second, { name: "Newest", version: 3 }, t2)
  expect(receiveRead(newest, { name: "Old", version: 2 }, t1)).toBe(newest)
})
test("late save preserves knowledge of an even newer server version", () => {
  const initial = editForm(loaded(), "name", "Submitted")
  const ticket = saveTicket(initial)
  const [waiting, read] = beginRead(initial)
  const newer = receiveRead(waiting, { name: "Newer author", version: 3 }, read)
  const saved = receiveSave(newer, { name: "Submitted", version: 2 }, ticket)
  expect(saved.baseVersion).toBe(2)
  expect(saved.serverSnapshot?.version).toBe(3)
  expect(saved.conflict).toBe(true)
})
