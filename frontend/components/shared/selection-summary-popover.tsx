"use client"

import React, { useEffect, useState } from "react"
import { Copy, X } from "lucide-react"
import { toast } from "sonner"
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/popover"
import { formatSummaryNumber, summaryCopyText, type SelectionSummary } from "@/lib/selection-summary"

export interface SelectionSummaryPopoverProps {
    summary: SelectionSummary
    /** Column the totals are for. */
    label: string
    /** The current cell the popover points at. */
    children: React.ReactElement
    onDismiss: () => void
}

/**
 * Copies text: the Clipboard API, else a hidden textarea and execCommand for
 * browsers or embedded views that deny clipboard permission.
 */
async function copyText(text: string): Promise<boolean> {
    try {
        await navigator.clipboard.writeText(text)
        return true
    } catch {
        const area = document.createElement("textarea")
        area.value = text
        area.setAttribute("readonly", "")
        area.style.position = "fixed"
        area.style.opacity = "0"
        document.body.appendChild(area)
        area.select()
        try {
            return document.execCommand("copy")
        } catch {
            return false
        } finally {
            area.remove()
        }
    }
}

/** True while Shift, Cmd or Ctrl is held: the user is extending a selection. */
function useSelectingModifier(): boolean {
    const [held, setHeld] = useState(false)
    useEffect(() => {
        // Keys report the change itself; pointer moves carry the modifier
        // state too, which covers a key pressed while the window was inactive.
        const update = (event: KeyboardEvent | PointerEvent) => setHeld(event.shiftKey || event.metaKey || event.ctrlKey)
        const release = () => setHeld(false)
        window.addEventListener("keydown", update)
        window.addEventListener("keyup", update)
        window.addEventListener("pointermove", update)
        window.addEventListener("blur", release)
        return () => {
            window.removeEventListener("keydown", update)
            window.removeEventListener("keyup", update)
            window.removeEventListener("pointermove", update)
            window.removeEventListener("blur", release)
        }
    }, [])
    return held
}

function CopyValue({ title, value, unit }: { title: string; value: string; unit: string }) {
    const text = formatSummaryNumber(value)
    return (
        <button
            type="button"
            className="group flex w-full items-center justify-between gap-3 rounded px-1.5 py-1 text-left hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
            title="Скопировать"
            onClick={async () => {
                const copied = summaryCopyText(value)
                if (await copyText(copied)) toast.success(`Скопировано: ${copied}`)
                else toast.error("Не удалось скопировать")
            }}
        >
            <span className="text-muted-foreground">{title}</span>
            <span className="flex items-center gap-1.5 font-mono tabular-nums">
                {text}{unit ? <span className="text-muted-foreground">{unit}</span> : null}
                <Copy className="h-3 w-3 opacity-0 group-hover:opacity-60 group-focus-visible:opacity-60" aria-hidden />
            </span>
        </button>
    )
}

/**
 * Totals of the current numeric column over the selected rows, pointing at
 * the current cell (1C style). A click on a total copies it with a decimal
 * comma and no grouping, ready for a calculator. Focus stays in the table.
 */
export function SelectionSummaryPopover({ summary, label, children, onDismiss }: SelectionSummaryPopoverProps) {
    // While a selection key is held the popover is closed, so clicks reach the
    // rows under it and a range can be extended past it; it reopens with the
    // new totals when the key is released.
    const selecting = useSelectingModifier()
    return (
        <Popover open={!selecting}>
            <PopoverAnchor asChild>{children}</PopoverAnchor>
            <PopoverContent
                side="bottom"
                align="end"
                className="w-auto min-w-56 p-2 text-xs"
                onOpenAutoFocus={(event) => event.preventDefault()}
                onCloseAutoFocus={(event) => event.preventDefault()}
                onEscapeKeyDown={onDismiss}
                onInteractOutside={(event) => event.preventDefault()}
                data-selection-summary
            >
                <div className="mb-1 flex items-center justify-between gap-3 px-1.5">
                    <span className="font-medium">{label}</span>
                    <button type="button" className="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground" aria-label="Скрыть итоги" onClick={onDismiss}>
                        <X className="h-3 w-3" />
                    </button>
                </div>
                {summary.groups.map((group) => (
                    <div key={group.unit} className="border-t pt-1 first-of-type:border-t-0 first-of-type:pt-0">
                        <CopyValue title="Сумма" value={group.sum} unit={group.unit} />
                        <CopyValue title="Среднее" value={group.average} unit={group.unit} />
                        {summary.groups.length > 1 && (
                            <p className="px-1.5 pb-1 text-muted-foreground">Строк: {group.count}</p>
                        )}
                    </div>
                ))}
                <p className="mt-1 border-t px-1.5 pt-1 text-muted-foreground">
                    Выделено строк: {summary.rows}
                    {summary.groups.reduce((total, group) => total + group.count, 0) < summary.rows ? `, с числом: ${summary.groups.reduce((total, group) => total + group.count, 0)}` : ""}
                </p>
            </PopoverContent>
        </Popover>
    )
}
