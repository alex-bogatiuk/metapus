"use client"

import { useRef, useState } from "react"
import { toast } from "sonner"
import { apiFetch, ApiError, idempotencyHeaders, shouldRetainMutationKey } from "@/lib/api"
import { useTabState } from "@/hooks/useTabState"
import { useTabDirty } from "@/hooks/useTabDirty"
import { useAuthStore } from "@/stores/useAuthStore"
import type { ActionAvailability, EntityActionDef, ReasonedDecisionRequest, ReasonedDecisionResult } from "@/types/metadata"
import { createTabStateKey } from "@/stores/useTabStateStore"

const tabStateKeys = {
  "reasonedDecisionDraft": createTabStateKey<DecisionDraft | null>("frontend/hooks/useReasonedDecision.ts:reasonedDecisionDraft"),
  "reasonedDecisionCommand": createTabStateKey<PendingDecision | null>("frontend/hooks/useReasonedDecision.ts:reasonedDecisionCommand"),
}


export interface DecisionSummaryField { label: string; value: string }
interface DecisionDraft {
    action: EntityActionDef
    path: string
    version: number
    availability: ActionAvailability
    summary: DecisionSummaryField[]
    reason: string
}
interface PendingDecision { path: string; body: ReasonedDecisionRequest; key: string; actorId: string }

export function decisionUnavailableReason(state?: ActionAvailability): string {
    switch (state?.reasonCode) {
        case "PROVIDER_OPENING_SELF_DECISION_FORBIDDEN": return "Решение по вашему документу должен принять другой оператор."
        case "PROVIDER_OPENING_PERMISSION_REQUIRED": return "Нет права на это действие."
        case "PROVIDER_OPENING_INVALID_MAKER": return "Не указан автор документа. Принять решение нельзя."
        case "PROVIDER_OPENING_NOT_PENDING": return "Решение по документу уже принято."
        case "READ_ONLY_SCOPE": return "В текущем режиме доступен только просмотр."
        default: return "Доступность действия не подтверждена. Обновите карточку."
    }
}

export function useReasonedDecision(refetch: () => Promise<void>) {
    const [draft, setDraft] = useTabState(tabStateKeys["reasonedDecisionDraft"], null)
    const [pending, setPending] = useTabState(tabStateKeys["reasonedDecisionCommand"], null)
    const [error, setError] = useState("")
    const [busy, setBusy] = useState(false)
    const inFlight = useRef(false)
    const { markDirty, markClean } = useTabDirty()

    function open(input: Omit<DecisionDraft, "reason">) {
        if (inFlight.current || pending || !input.availability.allowed) return
        setError("")
        setDraft({ ...input, reason: "" })
    }
    function close() {
        if (inFlight.current || pending) return
        setDraft(null)
        setError("")
        markClean()
    }
    function changeReason(reason: string) {
        if (!draft || inFlight.current || pending) return
        setDraft({ ...draft, reason })
        markDirty()
    }
    async function submit() {
        if (!draft || inFlight.current) return
        const reason = draft.reason.trim()
        const maxBytes = draft.action.input?.reasonMaxBytes ?? 2000
        if (!pending && (!reason || new TextEncoder().encode(reason).length > maxBytes)) {
            setError(`Укажите причину: не более ${maxBytes} байт UTF-8.`)
            return
        }
        const actorId = useAuthStore.getState().user?.id
        if (!actorId || (pending && pending.actorId !== actorId)) {
            setError("Для продолжения войдите под пользователем, отправившим запрос.")
            return
        }
        inFlight.current = true
        setBusy(true)
        setError("")
        const command = pending ?? { path: draft.path, body: { version: draft.version, reason }, key: idempotencyHeaders()["Idempotency-Key"], actorId }
        setPending(command)
        markDirty()
        let completed = false
        try {
            await apiFetch<ReasonedDecisionResult>(command.path, { method: "POST", headers: idempotencyHeaders(command.key), body: JSON.stringify(command.body) })
            completed = true
            setPending(null)
            setDraft(null)
            markClean()
            toast.success("Решение принято")
        } catch (err) {
            const message = err instanceof ApiError ? err.parsedBody?.message ?? err.message : "Не удалось получить ответ сервера."
            if (shouldRetainMutationKey(err)) {
                setError(`${message} Результат неизвестен. Повторите тот же запрос.`)
            } else {
                setPending(null)
                setError(message)
                if (err instanceof ApiError && err.parsedBody?.code !== "STEP_UP_REQUIRED" && err.parsedBody?.code !== "MFA_ENROLLMENT_REQUIRED") {
                    setDraft(null)
                    markClean()
                    completed = true
                    toast.error(message)
                }
            }
        } finally {
            // A successful POST stays terminal even when the following GET fails.
            if (completed) {
                try { await refetch() } catch { toast.error("Обновите карточку, чтобы увидеть актуальное состояние.") }
            }
            inFlight.current = false
            setBusy(false)
        }
    }
    return { draft, busy, pending: pending !== null, error, open, close, changeReason, submit }
}
