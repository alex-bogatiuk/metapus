"use client"

import { useMemo, useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { FilterValueControl } from "@/components/shared/filter-value-control"
import type { FilterFieldMeta } from "@/types/filter"
import type { ComparisonOperator } from "@/types/common"
import { isColumnFilterValueReady } from "@/lib/list-column-filter"
import {
    getDefaultOperator,
    getOperatorsForType,
    valueAfterOperatorChange,
    type FilterEntry,
} from "@/lib/filter-utils"

export interface ColumnFilterDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    columnLabel: string
    field: FilterFieldMeta | null
    initialValue: import("@/lib/schemas/filters").FilterValue
    onApply: (entry: FilterEntry) => void
}

export function ColumnFilterDialog({
    open,
    onOpenChange,
    columnLabel,
    field,
    initialValue,
    onApply,
}: ColumnFilterDialogProps) {
    const [entry, setEntry] = useState<FilterEntry>(() => ({
        operator: field ? getDefaultOperator(field.fieldType) : "contains",
        value: initialValue,
    }))

    const operators = field ? getOperatorsForType(field.fieldType) : []
    const showOperatorSelect = operators.length > 1

    const canApply = useMemo(
        () => !!field && isColumnFilterValueReady(field, entry.value, entry.operator),
        [field, entry.value, entry.operator],
    )

    const handleOperatorChange = (nextOp: ComparisonOperator) => {
        setEntry((current) => ({
            operator: nextOp,
            value: valueAfterOperatorChange(current.operator, nextOp, current.value),
        }))
    }

    const handleSubmit = (e: FormEvent) => {
        e.preventDefault()
        if (!field || !canApply) return
        onApply(entry)
        onOpenChange(false)
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-sm gap-4 p-5 sm:rounded-lg">
                {field ? (
                    <form onSubmit={handleSubmit} className="grid gap-4">
                        <DialogHeader>
                            <DialogTitle>Отбор: {field.label}</DialogTitle>
                            <DialogDescription>
                                Условие будет добавлено в фильтры списка.
                            </DialogDescription>
                        </DialogHeader>
                        {showOperatorSelect && (
                            <div className="grid gap-1.5">
                                <Label htmlFor="column-filter-operator" className="text-xs text-muted-foreground">
                                    Сравнение
                                </Label>
                                <Select
                                    value={entry.operator}
                                    onValueChange={(val) => handleOperatorChange(val as ComparisonOperator)}
                                >
                                    <SelectTrigger id="column-filter-operator" className="h-8 text-xs">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                        {operators.map((op) => (
                                            <SelectItem key={op.value} value={op.value} className="text-xs">
                                                {op.label}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </div>
                        )}
                        <FilterValueControl
                            field={field}
                            entry={entry}
                            onChange={setEntry}
                            autoFocus={!showOperatorSelect}
                        />
                        <DialogFooter>
                            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                                Отмена
                            </Button>
                            <Button type="submit" disabled={!canApply}>
                                Добавить отбор
                            </Button>
                        </DialogFooter>
                    </form>
                ) : (
                    <>
                        <DialogHeader>
                            <DialogTitle>{columnLabel || "Колонка"}</DialogTitle>
                            <DialogDescription>
                                По этой колонке нельзя отобрать. Выберите ячейку другой колонки.
                            </DialogDescription>
                        </DialogHeader>
                        <DialogFooter>
                            <Button type="button" onClick={() => onOpenChange(false)}>
                                Закрыть
                            </Button>
                        </DialogFooter>
                    </>
                )}
            </DialogContent>
        </Dialog>
    )
}
