/**
 * Pure helpers of the "Удаление помеченных объектов" processing: selection
 * keys and the wording of planned statuses. The plan itself is computed by
 * the server (POST /system/marked-objects/plan); nothing here re-derives it.
 */

import type {
    BlockReason,
    DeletionStatus,
    IncomingReference,
    MarkedObject,
    MarkedObjectRef,
} from "@/types/common"

/** Stable selection key of an object. */
export function markedKey(ref: MarkedObjectRef): string {
    return `${ref.entityName}:${ref.entityId}`
}

/** Inverse of markedKey; entity names never contain a colon. */
export function parseMarkedKey(key: string): MarkedObjectRef {
    const at = key.indexOf(":")
    return { entityName: key.slice(0, at), entityId: key.slice(at + 1) }
}

export type StatusTone = "success" | "info" | "destructive" | "muted"

export const deletionStatusView: Record<DeletionStatus, { label: string; tone: StatusTone }> = {
    ready: { label: "Удалится", tone: "success" },
    withGroup: { label: "Удалится вместе со ссылками", tone: "info" },
    blocked: { label: "Не удалится", tone: "destructive" },
    excluded: { label: "Не выбран", tone: "muted" },
}

/** Display name of a referencing object or system table. */
export function referenceSourceName(ref: IncomingReference, entityLabel: (entityName: string) => string): string {
    if (ref.sourceEntityType === "system") {
        return `Системная таблица ${ref.sourceField}`
    }
    const label = ref.sourceEntityName ? entityLabel(ref.sourceEntityName) : ""
    if (ref.presentation) return ref.presentation
    if (!ref.sourceId) return label || "Объект"
    return `${label || "Объект"} ${ref.sourceId.slice(0, 8)}…`
}

const blockReasonPrefix: Record<BlockReason, string> = {
    liveReference: "Используется:",
    notSelected: "На него ссылается невыбранный объект:",
    chain: "На него ссылается объект, который не удалится:",
    cycle: "Взаимная ссылка с объектом другого типа:",
}

/** One-line explanation shown under a marked object. */
export function statusExplanation(item: MarkedObject, entityLabel: (entityName: string) => string): string | null {
    if (item.status === "blocked" && item.reason) {
        const source = item.blockedBy ? referenceSourceName(item.blockedBy, entityLabel) : ""
        const prefix = blockReasonPrefix[item.reason]
        if (!source) return prefix.replace(/:$/, "")
        const rest = item.reason === "liveReference" ? item.liveRefCount - Math.max(item.blockedBy?.count ?? 1, 1) : 0
        const others = rest > 0 ? ` и ещё ${rest}` : ""
        return `${prefix} ${source}${others}`
    }
    if (item.status === "withGroup") {
        const together = item.references.filter((ref) => ref.sourceMarked).length
        return `Ссылаются только помеченные объекты: ${together}`
    }
    return null
}

/** What the processing can offer for one incoming reference. */
export type ReferenceAction =
    | { kind: "mark" }
    | { kind: "select" }
    | { kind: "none"; hint?: string }

export function referenceAction(
    ref: IncomingReference,
    selected: boolean,
    canSetDeletionMark: boolean,
): ReferenceAction {
    if (ref.sourceEntityType === "system" || ref.sourceEntityType === "register" || !ref.sourceId) {
        return { kind: "none", hint: "Эти записи нельзя пометить на удаление" }
    }
    if (ref.sourceMarked) {
        return selected ? { kind: "none" } : { kind: "select" }
    }
    if (ref.sourcePosted) {
        return { kind: "none", hint: "Документ проведён. Сначала отмените проведение" }
    }
    if (!canSetDeletionMark) {
        return { kind: "none", hint: "Этот объект нельзя пометить на удаление" }
    }
    return { kind: "mark" }
}

/** Russian plural form for n: one (1, 21), few (2–4), many (5–20, 0). */
export function plural(n: number, one: string, few: string, many: string): string {
    const mod10 = n % 10
    const mod100 = n % 100
    if (mod10 === 1 && mod100 !== 11) return one
    if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return few
    return many
}
