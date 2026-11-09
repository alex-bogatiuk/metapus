import type { JsonObject, JsonValue } from "@/lib/json-value"

export interface FormDraft {
  serverSnapshot: JsonObject | null
  draft: JsonObject
  baseVersion: number | null
  dirty: boolean
  conflict: boolean
  revision: number
  readId: number
  writeEpoch: number
}
export interface ReadTicket { revision: number; readId: number; writeEpoch: number }
export interface SaveTicket { revision: number; draft: JsonObject }

function version(data: JsonObject): number | null {
  return typeof data.version === "number" && Number.isSafeInteger(data.version) ? data.version : null
}
function equal(a: JsonValue | undefined, b: JsonValue | undefined): boolean {
  if (a === b) return true
  if (a === null || b === null || typeof a !== "object" || typeof b !== "object") return false
  if (Array.isArray(a) || Array.isArray(b)) return Array.isArray(a) && Array.isArray(b) && a.length === b.length && a.every((v, i) => equal(v, b[i]))
  const keys = Object.keys(a)
  return keys.length === Object.keys(b).length && keys.every(key => Object.hasOwn(b, key) && equal(a[key], b[key]))
}
export function emptyForm(): FormDraft {
  return { serverSnapshot: null, draft: {}, baseVersion: null, dirty: false, conflict: false, revision: 0, readId: 0, writeEpoch: 0 }
}
export function editForm(state: FormDraft, name: string, value: JsonValue): FormDraft {
  const draft = { ...state.draft, [name]: value }
  return { ...state, draft, revision: state.revision + 1, dirty: !equal(draft, state.serverSnapshot ?? {}) }
}
export function beginRead(state: FormDraft): [FormDraft, ReadTicket] {
  const next = { ...state, readId: state.readId + 1 }
  return [next, { revision: next.revision, readId: next.readId, writeEpoch: next.writeEpoch }]
}
export function receiveRead(state: FormDraft, data: JsonObject, ticket: ReadTicket, discard = false): FormDraft {
  if (ticket.readId !== state.readId || ticket.writeEpoch !== state.writeEpoch) return state
  // Even confirmed reload cannot discard input made AFTER the reload started.
  const replace = state.revision === ticket.revision && (discard || !state.dirty)
  if (replace) return { ...state, serverSnapshot: data, draft: data, baseVersion: version(data), dirty: false, conflict: false }
  return { ...state, serverSnapshot: data, conflict: state.baseVersion !== version(data) }
}
export function saveTicket(state: FormDraft): SaveTicket { return { revision: state.revision, draft: state.draft } }
export function receiveSave(state: FormDraft, data: JsonObject, ticket: SaveTicket): FormDraft {
  const draft = { ...data }
  if (state.revision !== ticket.revision) {
    for (const key of new Set([...Object.keys(state.draft), ...Object.keys(ticket.draft)])) {
      if (key === "version" || equal(state.draft[key], ticket.draft[key])) continue
      if (Object.hasOwn(state.draft, key)) draft[key] = state.draft[key]
      else delete draft[key]
    }
  }
  const serverSnapshot = state.serverSnapshot && (version(state.serverSnapshot) ?? 0) > (version(data) ?? 0) ? state.serverSnapshot : data
  return { ...state, serverSnapshot, draft, baseVersion: version(data), dirty: !equal(draft, data),
    conflict: version(serverSnapshot) !== version(data), readId: state.readId + 1, writeEpoch: state.writeEpoch + 1 }
}
