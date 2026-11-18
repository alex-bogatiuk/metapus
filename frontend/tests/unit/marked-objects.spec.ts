import { test, expect } from "@playwright/test"
import {
    markedKey,
    parseMarkedKey,
    plural,
    referenceAction,
    referenceSourceName,
    statusExplanation,
} from "@/lib/marked-objects"
import type { IncomingReference, MarkedObject } from "@/types/common"

const label = (name: string) => ({ Contract: "Договор", Counterparty: "Контрагент" })[name] ?? name

const contractRef: IncomingReference = {
    sourceEntityName: "Contract",
    sourceEntityType: "catalog",
    sourceId: "01a1cb5f-4f75-71ac-8463-d1105828ea41",
    sourceField: "counterpartyId",
    presentation: "Договор эквайринга (CTR-002)",
    sourceMarked: false,
    sourcePosted: false,
    count: 1,
}

function marked(overrides: Partial<MarkedObject>): MarkedObject {
    return {
        entityName: "Counterparty",
        entityType: "catalog",
        entityId: "01a1cb5f-4f60-70a1-8464-89582dd1ff36",
        presentation: "ИП Алексеев О. И. (CP-GEN-299)",
        status: "ready",
        refCount: 0,
        liveRefCount: 0,
        references: [],
        referencesTruncated: false,
        ...overrides,
    }
}

test("selection keys round-trip", () => {
    const ref = { entityName: "Counterparty", entityId: "01a1cb5f-4f60-70a1-8464-89582dd1ff36" }
    expect(parseMarkedKey(markedKey(ref))).toEqual(ref)
})

test("blocked object names what uses it", () => {
    const item = marked({ status: "blocked", reason: "liveReference", blockedBy: contractRef, refCount: 3, liveRefCount: 3, references: [contractRef] })
    expect(statusExplanation(item, label)).toBe("Используется: Договор эквайринга (CTR-002) и ещё 2")
})

test("object deleted with its referencing objects says how many", () => {
    const item = marked({ status: "withGroup", refCount: 1, references: [{ ...contractRef, sourceMarked: true, sourceStatus: "ready" }] })
    expect(statusExplanation(item, label)).toBe("Ссылаются только помеченные объекты: 1")
    expect(statusExplanation(marked({}), label)).toBeNull()
})

test("system table references are named by table and column", () => {
    const system: IncomingReference = { sourceEntityType: "system", sourceField: "sys_rates.currency_id", presentation: "", sourceMarked: false, sourcePosted: false, count: 4 }
    expect(referenceSourceName(system, label)).toBe("Системная таблица sys_rates.currency_id")
    expect(referenceAction(system, false, true)).toEqual({ kind: "none", hint: "Эти записи нельзя пометить на удаление" })
})

test("reference actions follow the source's state", () => {
    expect(referenceAction(contractRef, false, true)).toEqual({ kind: "mark" })
    expect(referenceAction(contractRef, false, false).kind).toBe("none")
    expect(referenceAction({ ...contractRef, sourcePosted: true }, false, true)).toEqual({
        kind: "none",
        hint: "Документ проведён. Сначала отмените проведение",
    })
    expect(referenceAction({ ...contractRef, sourceMarked: true }, false, true)).toEqual({ kind: "select" })
    expect(referenceAction({ ...contractRef, sourceMarked: true }, true, true)).toEqual({ kind: "none" })
})

test("russian plural forms", () => {
    const forms = (n: number) => plural(n, "объект", "объекта", "объектов")
    expect([1, 2, 5, 11, 12, 21, 22, 25, 111].map(forms)).toEqual([
        "объект", "объекта", "объектов", "объектов", "объектов", "объект", "объекта", "объектов", "объектов",
    ])
})
