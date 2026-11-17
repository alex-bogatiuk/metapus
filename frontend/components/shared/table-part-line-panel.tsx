"use client"

import React from "react"
import { ChevronDown, ChevronUp } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import type { MetadataField } from "@/lib/schemas/metadata"

export interface TablePartLinePanelProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    /** The part's title, e.g. "Товары". */
    title: string
    /** Every field of a line, the grid's hidden columns included. */
    columns: readonly MetadataField[]
    line: number
    lineCount: number
    onNavigate: (line: number) => void
    /** Renders a field of the shown line: its editor, or its value as text when it is not editable. */
    renderField: (column: MetadataField) => React.ReactNode
}

/**
 * A line's details (ERPNext's row form, a Fiori item page): every field of
 * one line in a side panel, for parts with more columns than the grid shows.
 * Alt+↑/↓ and the arrows in its header go to the line before or after;
 * changes go to the line as they are made, so there is nothing to apply.
 */
export function TablePartLinePanel({ open, onOpenChange, title, columns, line, lineCount, onNavigate, renderField }: TablePartLinePanelProps) {
    const go = (step: -1 | 1) => {
        const next = line + step
        if (next >= 0 && next < lineCount) onNavigate(next)
    }
    return (
        <Sheet open={open} onOpenChange={onOpenChange}>
            <SheetContent side="right" className="flex w-[min(440px,100vw)] flex-col gap-0 p-0 sm:max-w-none"
                onKeyDown={(event) => {
                    if (event.altKey && (event.key === "ArrowUp" || event.key === "ArrowDown")) {
                        event.preventDefault()
                        go(event.key === "ArrowUp" ? -1 : 1)
                    }
                }}>
                <SheetHeader className="border-b px-5 py-4 pr-12 text-left">
                    <div className="flex items-center justify-between gap-3">
                        <div className="min-w-0">
                            <SheetTitle className="truncate text-base">{title}: строка {line + 1}</SheetTitle>
                            <SheetDescription className="text-xs">из {lineCount} · Alt+↑/↓ — соседние строки</SheetDescription>
                        </div>
                        <div className="flex shrink-0 gap-1">
                            <Button type="button" variant="outline" size="icon" className="h-7 w-7" aria-label="Предыдущая строка"
                                disabled={line <= 0} onClick={() => go(-1)}><ChevronUp className="h-4 w-4" /></Button>
                            <Button type="button" variant="outline" size="icon" className="h-7 w-7" aria-label="Следующая строка"
                                disabled={line >= lineCount - 1} onClick={() => go(1)}><ChevronDown className="h-4 w-4" /></Button>
                        </div>
                    </div>
                </SheetHeader>
                <div key={line} className="min-h-0 flex-1 space-y-4 overflow-auto px-5 py-4 animate-in fade-in-0 duration-150 motion-reduce:animate-none">
                    {columns.map((column) => (
                        <div key={column.name} className="grid gap-1.5" data-line-field={column.name}>
                            <span className="text-xs text-muted-foreground">
                                {column.label || column.name}
                                {column.required && !column.readOnly ? <span className="text-destructive"> *</span> : null}
                            </span>
                            {renderField(column)}
                        </div>
                    ))}
                </div>
            </SheetContent>
        </Sheet>
    )
}
