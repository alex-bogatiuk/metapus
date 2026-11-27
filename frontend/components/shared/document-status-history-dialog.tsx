"use client"

/**
 * DocumentStatusHistoryDialog lists a document's status changes (история
 * статусов), oldest first: when, from which status to which, and who.
 */

import { useEffect, useState } from "react"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { apiFetch, ApiError } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import type * as Contract from "@/types/generated/http-contracts"

type Change = Contract.DocumentStatusChange

interface DocumentStatusHistoryDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    /** The document's API path, e.g. "/document/customer-order/<id>". */
    documentPath: string
}

export function DocumentStatusHistoryDialog({ open, onOpenChange, documentPath }: DocumentStatusHistoryDialogProps) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-lg">
                <DialogHeader>
                    <DialogTitle>История статусов</DialogTitle>
                    <DialogDescription>Каждая запись документа в новом статусе.</DialogDescription>
                </DialogHeader>
                {open && <StatusHistory documentPath={documentPath} />}
            </DialogContent>
        </Dialog>
    )
}

/** The history, read each time the dialog opens. */
function StatusHistory({ documentPath }: { documentPath: string }) {
    const [items, setItems] = useState<Change[] | null>(null)
    const [error, setError] = useState<string | null>(null)

    useEffect(() => {
        let cancelled = false
        apiFetch<Contract.DocumentStatusHistoryResponse>(`${documentPath}/status-history`)
            .then((answer) => { if (!cancelled) setItems(answer.items) })
            .catch((err: Error) => {
                if (cancelled) return
                setError(err instanceof ApiError ? err.parsedBody?.message ?? `Ошибка ${err.status}` : "Не удалось загрузить историю")
            })
        return () => { cancelled = true }
    }, [documentPath])

    if (error) return <p className="text-sm text-destructive">{error}</p>
    if (items === null) return <p className="text-sm text-muted-foreground">Загрузка…</p>
    if (items.length === 0) return <p className="text-sm text-muted-foreground">Статус не менялся.</p>
    return (
        <ScrollArea className="max-h-80">
            <ol className="space-y-2 text-sm">
                {items.map((change, index) => (
                    <li key={index} className="grid grid-cols-[9rem_minmax(0,1fr)] gap-x-3">
                        <span className="tabular-nums text-muted-foreground">{formatDateTime(change.changedAt)}</span>
                        <span>
                            {change.previousStatusLabel ? `${change.previousStatusLabel} → ` : ""}
                            <span className="font-medium">{change.statusLabel}</span>
                            <span className="block truncate text-xs text-muted-foreground">{change.changedByName || "Система"}</span>
                        </span>
                    </li>
                ))}
            </ol>
        </ScrollArea>
    )
}
