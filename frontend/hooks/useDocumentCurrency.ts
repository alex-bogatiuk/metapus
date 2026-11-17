"use client"

import { useEffect, useState } from "react"
import { apiFetch } from "@/lib/api"
import { getSessionGeneration } from "@/lib/auth-scope"
import { withReferenceListParams } from "@/lib/reference-utils"
import type { JsonObject } from "@/lib/json-value"
import type { MetadataField } from "@/lib/schemas/metadata"
import type { CursorListResponse } from "@/types/common"
import { useCurrencyScale } from "@/hooks/useCurrencyScale"

/** The currency a form's money is shown and entered in. */
export interface DocumentCurrency {
    /** Places of the currency's minor unit (2 for RUB, 0 for JPY). */
    scale: number
    /** ISO code shown beside amounts, e.g. "RUB". */
    code: string
}

/** The system's base currency, read once per session (cat_currencies.is_base is unique). */
let baseCurrency: { generation: number; id: string | null } | null = null
let baseRequest: Promise<string | null> | null = null

function loadBaseCurrency(): Promise<string | null> {
    const generation = getSessionGeneration()
    if (baseCurrency?.generation === generation) return Promise.resolve(baseCurrency.id)
    if (!baseRequest) {
        baseRequest = apiFetch<CursorListResponse<{ id: string }>>(withReferenceListParams("/catalog/currencies", {
            limit: 1, filter: [{ field: "is_base", operator: "eq", value: true }],
        }))
            .then((result) => result.items?.[0]?.id ?? null)
            .catch(() => null)
            .then((id) => {
                baseCurrency = { generation, id }
                baseRequest = null
                return id
            })
    }
    return baseRequest
}

function useBaseCurrencyId(enabled: boolean): string | undefined {
    const cached = baseCurrency?.generation === getSessionGeneration() ? baseCurrency.id : undefined
    const [loaded, setLoaded] = useState<string | null | undefined>(cached)
    useEffect(() => {
        if (!enabled || loaded !== undefined) return
        let cancelled = false
        void loadBaseCurrency().then((id) => { if (!cancelled) setLoaded(id) })
        return () => { cancelled = true }
    }, [enabled, loaded])
    return enabled ? loaded ?? undefined : undefined
}

/**
 * The currency of a document's money (1C "валюта документа"): the currency
 * its header names (currencyId, else the first reference to a currency),
 * else, when it names none, the system's base currency. Money is stored in
 * minor units, so this only decides how amounts read and are typed. Null
 * while it loads or when no currency can be read: forms then keep minor units.
 */
export function useDocumentCurrency(fields: readonly MetadataField[] | undefined, values: JsonObject): DocumentCurrency | null {
    const isCurrency = (field: MetadataField) => field.type === "reference" && field.referenceType === "currency"
    const field = fields?.find((f) => f.name === "currencyId" && isCurrency(f)) ?? fields?.find(isCurrency)
    const own = field ? values[field.name] : undefined
    const ownId = typeof own === "string" && own ? own : undefined
    const baseId = useBaseCurrencyId(!ownId)
    const { decimalPlaces, symbol } = useCurrencyScale(ownId ?? baseId)
    return decimalPlaces === null ? null : { scale: decimalPlaces, code: symbol }
}
