import { useCallback } from "react"
import { usePathname } from "next/navigation"
import { useTabState } from "@/hooks/useTabState"
import { useTabStateStore, createTabStateKey } from "@/stores/useTabStateStore"
import { useTabsStore } from "@/stores/useTabsStore"
import { assertCurrentSession, getSessionGeneration } from "@/lib/auth-scope"
import { beginRead, editForm, emptyForm, receiveRead, receiveSave, saveTicket, type FormDraft, type ReadTicket, type SaveTicket } from "@/lib/form-draft"
import type { JsonObject, JsonValue } from "@/lib/json-value"

export const AUTO_FORM_STATE_KEY = createTabStateKey<FormDraft>("autoFormState")
export function useAutoFormDraft() {
  const pathname = usePathname()
  const generation = getSessionGeneration()
  const [state, setState] = useTabState<FormDraft>(AUTO_FORM_STATE_KEY, emptyForm())
  const current = useCallback(() => {
    assertCurrentSession(generation)
    return useTabStateStore.getState().get(pathname, AUTO_FORM_STATE_KEY) ?? emptyForm()
  }, [pathname, generation])
  const apply = useCallback((next: FormDraft) => {
    assertCurrentSession(generation)
    setState(next)
    useTabsStore.getState().setTabDirty(pathname, next.dirty)
    return next
  }, [generation, pathname, setState])
  const edit = useCallback((name: string, value: JsonValue) => apply(editForm(current(), name, value)), [apply, current])
  const startRead = useCallback(() => {
    const [next, ticket] = beginRead(current())
    setState(next)
    return ticket
  }, [current, setState])
  const finishRead = useCallback((data: JsonObject, ticket: ReadTicket, discard = false) => apply(receiveRead(current(), data, ticket, discard)), [apply, current])
  const startSave = useCallback(() => saveTicket(current()), [current])
  const finishSave = useCallback((data: JsonObject, ticket: SaveTicket) => apply(receiveSave(current(), data, ticket)), [apply, current])
  return { state, edit, startRead, finishRead, startSave, finishSave, current }
}
