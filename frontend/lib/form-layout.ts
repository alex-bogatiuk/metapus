import type { JsonValue } from "@/lib/json-value"
import type { MetadataField } from "@/lib/schemas/metadata"

/**
 * Layout of a generic entity form, derived from metadata by fixed rules
 * (after 1C managed forms, SAP Fiori object pages and Odoo form views):
 *
 *  - header: the identity row (document number and date, catalog code and name);
 *  - groups: the fields the user fills. Explicit meta:"group:<title>" groups
 *    keep their order; the remaining fields form "Основное", split by kind into
 *    "Основное" (references), "Суммы" (amounts next to their currency or token;
 *    in catalogs only a unit named like the amount),
 *    "Даты", "Реквизиты" and "Признаки" (booleans) once there are more than
 *    AUTO_GROUP_LIMIT of them. Required fields lead their group, booleans close it;
 *  - wide: long texts (comment, description), full width after the groups;
 *  - extra: fields the form cannot write (read-only in metadata or absent from
 *    the form's request contract): server state, shown apart and read-only;
 *  - parts: table parts inline under the form when there is one and the form is
 *    short, otherwise on tabs together with "Дополнительно".
 *
 * Required writable fields are never placed in extra or on a tab.
 */
export interface FormFieldGroup {
    key: string
    title?: string
    fields: MetadataField[]
}

export interface FormLayout {
    header: MetadataField[]
    groups: FormFieldGroup[]
    wide: MetadataField[]
    extra: MetadataField[]
    partsMode: "inline" | "tabs"
}

export interface FormLayoutInput {
    kind: "catalog" | "document" | "register"
    fields: readonly MetadataField[]
    partCount: number
    /** Fields the form's write request accepts; null when no contract is known. */
    accepted: ReadonlySet<string> | null
}

/** Above this many automatically placed fields, they are split by kind. */
export const AUTO_GROUP_LIMIT = 8
/** A single table part goes on a tab once the form has more fields than this. */
export const INLINE_PART_FIELD_LIMIT = 12

const IDENTITY: Record<FormLayoutInput["kind"], readonly string[]> = {
    document: ["number", "date"],
    catalog: ["code", "name"],
    register: [],
}

const WIDE_NAMES = new Set(["comment", "description", "notes", "note"])
const AMOUNT_NAME = /(amount|sum|total|price|fee|cost)/i
const NUMERIC = new Set(["integer", "number", "decimal", "money"])
const MONEY_UNIT_REFS = new Set(["currency", "token"])

/** A server-owned value as text: dates in Russian locale, enum labels, Да/Нет, "—" when empty. */
export function displayValue(field: MetadataField, value: JsonValue | undefined): string {
    if (value === undefined || value === null || value === "") return "—"
    if (field.type === "boolean") return value ? "Да" : "Нет"
    if ((field.type === "date" || field.type === "datetime") && typeof value === "string") {
        const date = new Date(value)
        if (Number.isNaN(date.getTime())) return value
        return field.type === "date" ? date.toLocaleDateString("ru-RU") : date.toLocaleString("ru-RU")
    }
    const option = field.enumValues?.find((item) => item.value === value)
    if (option) return option.label
    return typeof value === "object" ? JSON.stringify(value) : String(value)
}

export function isWideField(field: MetadataField): boolean {
    return field.type === "text" || (field.type === "string" && WIDE_NAMES.has(field.name))
}

/** Whether the form can write the field: not read-only and accepted by its request. */
export function isWritableField(field: MetadataField, accepted: ReadonlySet<string> | null): boolean {
    if (field.readOnly) return false
    return accepted === null || accepted.has(field.name)
}

function isAmount(field: MetadataField): boolean {
    return field.type === "money" || (NUMERIC.has(field.type) && AMOUNT_NAME.test(field.name))
}

function isMoneyUnit(field: MetadataField): boolean {
    return field.type === "reference" && !!field.referenceType && MONEY_UNIT_REFS.has(field.referenceType)
}

/**
 * Applies an ordering to the fields while each amount stays followed by its
 * paired unit (the unit moves with its amount).
 */
function keepPairs(fields: MetadataField[], order: (fields: MetadataField[]) => MetadataField[]): MetadataField[] {
    const unitOf = new Map<MetadataField, MetadataField>()
    fields.forEach((field, index) => {
        if (isAmount(field) && fields[index + 1] && isMoneyUnit(fields[index + 1])) unitOf.set(field, fields[index + 1])
    })
    const units = new Set(unitOf.values())
    return order(fields.filter((field) => !units.has(field))).flatMap((field) => unitOf.has(field) ? [field, unitOf.get(field)!] : [field])
}

/** Required fields first, booleans last; otherwise the model's order is kept. */
function orderWithinGroup(fields: MetadataField[]): MetadataField[] {
    const rank = (field: MetadataField) => (field.type === "boolean" ? 2 : field.required ? 0 : 1)
    return fields.map((field, index) => ({ field, index }))
        .sort((a, b) => rank(a.field) - rank(b.field) || a.index - b.index)
        .map(({ field }) => field)
}

/**
 * Places each amount's currency or token right after it: the unit named like
 * the amount (feeAmount → feeTokenId), or — when anyUnit is set, as in
 * documents whose amounts are operations in the document's currency or token —
 * the first unpaired unit. In a catalog an amount is usually a setting and a
 * currency there describes the item itself, so only named pairs are made.
 */
export function pairAmountsWithUnits(fields: MetadataField[], anyUnit = true): MetadataField[] {
    const units = fields.filter(isMoneyUnit)
    const paired = new Map<MetadataField, MetadataField>()
    const used = new Set<MetadataField>()
    for (const amount of fields.filter(isAmount)) {
        const base = amount.name.replace(/(amount|sum|total|price)$/i, "")
        const unit = (base && units.find((u) => !used.has(u) && u.name.startsWith(base)))
            || (anyUnit ? units.find((u) => !used.has(u)) : undefined)
        if (unit) {
            paired.set(amount, unit)
            used.add(unit)
        }
    }
    const out: MetadataField[] = []
    for (const field of fields) {
        if (used.has(field)) continue
        out.push(field)
        const unit = paired.get(field)
        if (unit) out.push(unit)
    }
    return out
}

/**
 * In a two-column grid an amount and its unit must share a row: when the pair
 * would start in the right column, the next single field is moved before it.
 */
export function alignPairsToRows(fields: MetadataField[]): MetadataField[] {
    const out = [...fields]
    const startsPair = (index: number) => isAmount(out[index]) && !!out[index + 1] && isMoneyUnit(out[index + 1])
    for (let index = 0; index < out.length; index++) {
        if (!startsPair(index)) continue
        if (index % 2 === 1) {
            const single = out.findIndex((field, i) => i > index + 1 && !startsPair(i) && !(i > 0 && startsPair(i - 1)))
            if (single !== -1) {
                const [moved] = out.splice(single, 1)
                out.splice(index, 0, moved)
                index += 1
            }
        }
        index += 1
    }
    return out
}

type Bucket = "main" | "amounts" | "dates" | "details" | "flags"
const BUCKET_TITLES: Record<Bucket, string> = {
    main: "Основное", amounts: "Суммы", dates: "Даты", details: "Реквизиты", flags: "Признаки",
}

function bucketOf(field: MetadataField, unitsOfAmounts: ReadonlySet<MetadataField>): Bucket {
    if (isAmount(field) || unitsOfAmounts.has(field)) return "amounts"
    if (field.type === "boolean") return "flags"
    if (field.type === "date" || field.type === "datetime") return "dates"
    if (field.type === "reference" || field.type === "typed_ref") return "main"
    return "details"
}

function autoGroups(fields: MetadataField[], anyUnit: boolean): FormFieldGroup[] {
    if (fields.length === 0) return []
    const paired = pairAmountsWithUnits(fields, anyUnit)
    if (fields.length <= AUTO_GROUP_LIMIT) {
        return [{ key: "main", fields: alignPairsToRows(keepPairs(paired, orderWithinGroup)) }]
    }
    const units = new Set<MetadataField>()
    paired.forEach((field, index) => {
        if (isAmount(field) && paired[index + 1] && isMoneyUnit(paired[index + 1])) units.add(paired[index + 1])
    })
    const buckets = new Map<Bucket, MetadataField[]>()
    for (const field of paired) {
        const bucket = bucketOf(field, units)
        buckets.set(bucket, [...(buckets.get(bucket) ?? []), field])
    }
    // A bucket of one field is not worth a heading: it joins "Основное".
    const order: Bucket[] = ["main", "amounts", "dates", "details", "flags"]
    for (const bucket of order.slice(1)) {
        const list = buckets.get(bucket)
        if (list && list.length < 2) {
            buckets.set("main", [...(buckets.get("main") ?? []), ...list])
            buckets.delete(bucket)
        }
    }
    return order.filter((bucket) => buckets.get(bucket)?.length)
        .map((bucket) => ({
            key: bucket,
            title: BUCKET_TITLES[bucket],
            // An amount keeps its unit beside it; required-first ordering is
            // applied to the other kinds only.
            fields: bucket === "amounts" ? alignPairsToRows(buckets.get(bucket)!) : orderWithinGroup(buckets.get(bucket)!),
        }))
}

export function planFormLayout({ kind, fields, partCount, accepted }: FormLayoutInput): FormLayout {
    const identity = IDENTITY[kind]
    const header = identity.map((name) => fields.find((field) => field.name === name)).filter((field): field is MetadataField => !!field)
    const rest = fields.filter((field) => !header.includes(field))

    const extra: MetadataField[] = []
    const wide: MetadataField[] = []
    const explicit = new Map<string, MetadataField[]>()
    const automatic: MetadataField[] = []
    for (const field of rest) {
        // An explicit group is the author's override and wins over every rule.
        if (field.group) {
            explicit.set(field.group, [...(explicit.get(field.group) ?? []), field])
        } else if (!isWritableField(field, accepted) && !field.required) {
            extra.push(field)
        } else if (isWideField(field)) {
            wide.push(field)
        } else {
            automatic.push(field)
        }
    }

    const groups = [
        ...autoGroups(automatic, kind === "document"),
        ...[...explicit.entries()].map(([title, list]) => ({ key: `group:${title}`, title, fields: orderWithinGroup(list) })),
    ]
    // A lone automatic group needs no heading when nothing else is titled.
    if (groups.length === 1 && groups[0].key === "main") groups[0] = { key: "main", fields: groups[0].fields }
    else if (groups[0]?.key === "main" && !groups[0].title) groups[0] = { ...groups[0], title: BUCKET_TITLES.main }

    const visibleFieldCount = header.length + groups.reduce((total, group) => total + group.fields.length, 0) + wide.length
    const partsMode = partCount >= 2 || (partCount === 1 && visibleFieldCount > INLINE_PART_FIELD_LIMIT) ? "tabs" : "inline"
    return { header, groups, wide, extra, partsMode }
}
