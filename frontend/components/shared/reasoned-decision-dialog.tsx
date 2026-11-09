"use client"

import { useId } from "react"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Textarea } from "@/components/ui/textarea"
import { Button } from "@/components/ui/button"
import type { useReasonedDecision } from "@/hooks/useReasonedDecision"

interface ReasonedDecisionDialogProps { controller: ReturnType<typeof useReasonedDecision> }

export function ReasonedDecisionDialog({ controller }: ReasonedDecisionDialogProps) {
    const reasonId = useId()
    const { draft, busy, pending, error, close, changeReason, submit } = controller
    const reasonBytes = new TextEncoder().encode(draft?.reason.trim() ?? "").length
    const maxBytes = draft?.action.input?.reasonMaxBytes ?? 2000
    return (
        <Dialog open={draft !== null} onOpenChange={(open) => { if (!open) close() }}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>{draft?.action.label} документ</DialogTitle>
                    <DialogDescription>Проверьте основание и укажите причину решения. Действие будет записано в аудит.</DialogDescription>
                </DialogHeader>
                <form className="flex flex-col gap-4" onSubmit={(event) => { event.preventDefault(); void submit() }}>
                    <dl className="grid grid-cols-2 gap-2">
                        {draft?.summary.map((field) => <div key={field.label} className="col-span-2 grid grid-cols-2 gap-2">
                            <dt>{field.label}</dt><dd className="break-all">{field.value}</dd>
                        </div>)}
                    </dl>
                    {draft?.availability.decisionMode === "admin_self" && <Alert>
                        <AlertTitle>Решение по собственному документу</AlertTitle>
                        <AlertDescription>Вы принимаете решение как системный администратор, без независимого оператора. Исключение будет записано в аудит.</AlertDescription>
                    </Alert>}
                    <FieldGroup>
                        <Field data-invalid={reasonBytes > maxBytes || undefined} data-disabled={busy || pending || undefined}>
                            <FieldLabel htmlFor={reasonId}>Причина решения</FieldLabel>
                            <Textarea id={reasonId} value={draft?.reason ?? ""} required disabled={busy || pending}
                                aria-invalid={reasonBytes > maxBytes} onChange={(event) => changeReason(event.target.value)} />
                            <FieldDescription>{reasonBytes} / {maxBytes} байт UTF-8</FieldDescription>
                        </Field>
                    </FieldGroup>
                    {error && <Alert variant="destructive"><AlertTitle>Запрос не завершён</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}
                    <DialogFooter>
                        <Button type="button" variant="outline" disabled={busy || pending} onClick={close}>Отмена</Button>
                        <Button type="submit" variant={draft?.action.destructive ? "destructive" : "default"}
                            disabled={busy || (!pending && (reasonBytes === 0 || reasonBytes > maxBytes))}>
                            {busy ? "Ожидание подтверждения…" : pending ? "Повторить тот же запрос" : "Подтвердить решение"}
                        </Button>
                    </DialogFooter>
                </form>
            </DialogContent>
        </Dialog>
    )
}
