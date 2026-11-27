"use client"

/**
 * SequenceStateAlert warns on the form of a document that closes a period
 * (закрытие месяца) when the sequence of documents it restores is broken:
 * a posted closing is outdated by documents changed before its period's
 * end, or an earlier period is to be closed again before this one.
 */

import { useEffect, useState } from "react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { apiFetch } from "@/lib/api"
import type * as Contract from "@/types/generated/http-contracts"

function day(iso: string): string {
    return new Date(iso).toLocaleDateString("ru-RU", { timeZone: "UTC" })
}

export function SequenceStateAlert({ documentPath }: { documentPath: string }) {
    const [state, setState] = useState<Contract.SequenceStateResponse | null>(null)

    useEffect(() => {
        let cancelled = false
        apiFetch<Contract.SequenceStateResponse>(`${documentPath}/sequence`)
            .then((answer) => { if (!cancelled) setState(answer) })
            .catch(() => { /* the form works without the warning */ })
        return () => { cancelled = true }
    }, [documentPath])

    if (!state?.boundary) return null
    if (state.earlierBroken) {
        return (
            <Alert variant="destructive">
                <AlertTitle>Сначала закройте предыдущий период</AlertTitle>
                <AlertDescription>
                    Документы изменены задним числом с {day(state.boundary)}. Перепроведите закрытие периода с этой даты, затем следующие по порядку.
                </AlertDescription>
            </Alert>
        )
    }
    if (state.outdated) {
        return (
            <Alert variant="destructive">
                <AlertTitle>Закрытие устарело</AlertTitle>
                <AlertDescription>
                    Документы периода изменены после закрытия, с {day(state.boundary)}. Перепроведите этот документ и закрытия следующих периодов.
                </AlertDescription>
            </Alert>
        )
    }
    return null
}
