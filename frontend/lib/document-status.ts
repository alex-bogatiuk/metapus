/**
 * The status model of a document (статусная модель): which statuses the form
 * offers. The server checks every change, the permission of a transition
 * included; the form only leaves out statuses no transition reaches.
 */

import type * as Contract from "@/types/generated/http-contracts"
import type { MetadataField } from "@/lib/schemas/metadata"

type StatusModel = Contract.MetadataStatusModelDef

/**
 * The statuses a document in stored (undefined for a new document, which
 * starts in the initial status) may be written in: its own and those a
 * transition from it reaches, in the model's order. Without transitions
 * every status is reachable.
 */
export function reachableStatuses(model: StatusModel, stored: string | undefined): string[] {
    const from = stored || model.initial
    const transitions = model.transitions ?? []
    return model.values
        .map((value) => value.value)
        .filter((value) => transitions.length === 0 || value === from
            || transitions.some((t) => t.to === value && (!t.from?.length || t.from.includes(from))))
}

/** field with only the reachable statuses as choices when it is the status field. */
export function withReachableStatuses(field: MetadataField, model: StatusModel | undefined, stored: string | undefined): MetadataField {
    if (!model || field.name !== model.field || !field.enumValues) return field
    const reachable = new Set(reachableStatuses(model, stored))
    return { ...field, enumValues: field.enumValues.filter((value) => reachable.has(value.value)) }
}
