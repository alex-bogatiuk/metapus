"use client"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { DatePicker } from "@/components/ui/date-picker"
import { ReferenceField, type ReferenceOption } from "@/components/shared/reference-field"
import { X } from "lucide-react"
import { cn } from "@/lib/utils"
import { exactFilterEntryError, isExactFilterField } from "@/lib/filter-numbers"
import type { FilterFieldMeta } from "@/types/filter"
import { filterRangeSchema, filterReferenceSchema, type FilterValue } from "@/lib/schemas/filters"
import {
    formatLocalYYYYMMDD,
    isBetweenOperator,
    isListOperator,
    isNullaryOperator,
    type FilterEntry,
} from "@/lib/filter-utils"

export interface FilterValueControlProps {
    field: FilterFieldMeta
    entry: FilterEntry
    onChange: (entry: FilterEntry) => void
    className?: string
    autoFocus?: boolean
}

function setValue(entry: FilterEntry, value: FilterValue): FilterEntry {
    return { operator: entry.operator, value }
}

function BooleanValue({ entry, onChange, className }: {
    entry: FilterEntry
    onChange: (entry: FilterEntry) => void
    className?: string
}) {
    let stringValue = "null"
    if (entry.value === true || entry.value === "true") stringValue = "true"
    else if (entry.value === false || entry.value === "false") stringValue = "false"

    return (
        <ToggleGroup
            type="single"
            variant="outline"
            value={stringValue}
            onValueChange={(val) => {
                if (!val) val = "null"
                let nextValue: boolean | undefined
                if (val === "true") nextValue = true
                else if (val === "false") nextValue = false
                onChange({ operator: "eq", value: nextValue })
            }}
            className={cn("w-full gap-0 -space-x-px", className)}
        >
            <ToggleGroupItem value="null" className="h-7 text-[11px] px-2 flex-1 rounded-none rounded-l-md data-[state=on]:z-10">
                Не выбрано
            </ToggleGroupItem>
            <ToggleGroupItem value="true" className="h-7 text-[11px] px-2 flex-1 rounded-none data-[state=on]:z-10">
                Да
            </ToggleGroupItem>
            <ToggleGroupItem value="false" className="h-7 text-[11px] px-2 flex-1 rounded-none rounded-r-md data-[state=on]:z-10">
                Нет
            </ToggleGroupItem>
        </ToggleGroup>
    )
}

function StringValue({ field, entry, onChange, className, autoFocus }: {
    field: FilterFieldMeta
    entry: FilterEntry
    onChange: (entry: FilterEntry) => void
    className?: string
    autoFocus?: boolean
}) {
    return (
        <Input
            autoFocus={autoFocus}
            className={cn("h-7 text-xs", className)}
            placeholder={field.label}
            value={typeof entry.value === "string" ? entry.value : ""}
            onChange={(e) => onChange(setValue(entry, e.target.value || undefined))}
        />
    )
}

function NumberValue({ field, entry, onChange, className, autoFocus }: {
    field: FilterFieldMeta
    entry: FilterEntry
    onChange: (entry: FilterEntry) => void
    className?: string
    autoFocus?: boolean
}) {
    return (
        <Input
            autoFocus={autoFocus}
            className={cn("h-7 text-xs", className)}
            type={isExactFilterField(field) ? "text" : "number"}
            inputMode={field.fieldType === "money" ? "numeric" : "decimal"}
            aria-label={field.label}
            aria-invalid={!!exactFilterEntryError(field, entry)}
            placeholder={field.label}
            value={entry.value !== undefined && entry.value !== null ? String(entry.value) : ""}
            onChange={(e) => onChange(setValue(entry, e.target.value || undefined))}
        />
    )
}

function BetweenNumberValue({ field, entry, onChange, className }: {
    field: FilterFieldMeta
    entry: FilterEntry
    onChange: (entry: FilterEntry) => void
    className?: string
}) {
    const range = filterRangeSchema.safeParse(entry.value).data ?? {}
    return (
        <div className="flex items-center gap-1">
            <Input
                className={cn("h-7 text-xs flex-1", className)}
                type={isExactFilterField(field) ? "text" : "number"}
                inputMode={field.fieldType === "money" ? "numeric" : "decimal"}
                aria-label={`${field.label}: от`}
                aria-invalid={!!exactFilterEntryError(field, entry)}
                placeholder="от"
                value={range.from !== undefined && range.from !== null ? String(range.from) : ""}
                onChange={(e) => onChange(setValue(entry, { ...range, from: e.target.value || undefined }))}
            />
            <span className="text-[10px] text-muted-foreground shrink-0">—</span>
            <Input
                className={cn("h-7 text-xs flex-1", className)}
                type={isExactFilterField(field) ? "text" : "number"}
                inputMode={field.fieldType === "money" ? "numeric" : "decimal"}
                aria-label={`${field.label}: до`}
                aria-invalid={!!exactFilterEntryError(field, entry)}
                placeholder="до"
                value={range.to !== undefined && range.to !== null ? String(range.to) : ""}
                onChange={(e) => onChange(setValue(entry, { ...range, to: e.target.value || undefined }))}
            />
        </div>
    )
}

function DateValue({ entry, onChange, className }: {
    entry: FilterEntry
    onChange: (entry: FilterEntry) => void
    className?: string
}) {
    const dateStr = typeof entry.value === "string" ? entry.value : ""
    const dateObj = dateStr ? new Date(dateStr) : undefined
    return (
        <DatePicker
            value={dateObj}
            onChange={(d) => onChange(setValue(entry, d ? formatLocalYYYYMMDD(d) : undefined))}
            className={cn("h-7 text-xs", className)}
        />
    )
}

function BetweenDateValue({ entry, onChange, className }: {
    entry: FilterEntry
    onChange: (entry: FilterEntry) => void
    className?: string
}) {
    const range = filterRangeSchema.safeParse(entry.value).data ?? {}
    const fromDate = typeof range.from === "string" && range.from ? new Date(range.from) : undefined
    const toDate = typeof range.to === "string" && range.to ? new Date(range.to) : undefined
    return (
        <div className="flex items-center gap-1">
            <DatePicker
                value={fromDate}
                onChange={(d) => onChange(setValue(entry, { ...range, from: d ? formatLocalYYYYMMDD(d) : undefined }))}
                className={cn("h-7 text-xs flex-1", className)}
            />
            <span className="text-[10px] text-muted-foreground shrink-0">—</span>
            <DatePicker
                value={toDate}
                onChange={(d) => onChange(setValue(entry, { ...range, to: d ? formatLocalYYYYMMDD(d) : undefined }))}
                className={cn("h-7 text-xs flex-1", className)}
            />
        </div>
    )
}

function ReferenceSingle({ field, entry, onChange, className }: {
    field: FilterFieldMeta
    entry: FilterEntry
    onChange: (entry: FilterEntry) => void
    className?: string
}) {
    const ref = filterReferenceSchema.safeParse(entry.value).data
    return (
        <ReferenceField
            value={ref?.id ?? ""}
            displayName={ref?.name ?? ""}
            apiEndpoint={field.refEndpoint ?? ""}
            placeholder={field.label}
            compact
            className={className}
            onChange={(id, name) => onChange(setValue(entry, id ? { id, name } : undefined))}
        />
    )
}

function ReferenceList({ field, entry, onChange, className }: {
    field: FilterFieldMeta
    entry: FilterEntry
    onChange: (entry: FilterEntry) => void
    className?: string
}) {
    const items: ReferenceOption[] = filterReferenceSchema.array().safeParse(entry.value).data ?? []

    const addItem = (id: string, name: string) => {
        if (!id || items.some((i) => i.id === id)) return
        onChange(setValue(entry, [...items, { id, name }]))
    }

    const removeItem = (id: string) => {
        const next = items.filter((i) => i.id !== id)
        onChange(setValue(entry, next.length > 0 ? next : undefined))
    }

    return (
        <div className="flex flex-col gap-1">
            {items.map((item) => (
                <div key={item.id} className={cn("flex items-center gap-1 rounded border bg-muted/40 px-1.5 py-0.5", className)}>
                    <span className="text-[10px] truncate flex-1">{item.name || item.id.slice(0, 8)}</span>
                    <Button
                        variant="ghost"
                        size="icon"
                        className="h-3.5 w-3.5 shrink-0 text-muted-foreground/50 hover:text-destructive"
                        onClick={() => removeItem(item.id)}
                    >
                        <X className="h-2.5 w-2.5" />
                    </Button>
                </div>
            ))}
            <ReferenceField
                value=""
                apiEndpoint={field.refEndpoint ?? ""}
                placeholder="Добавить…"
                compact
                onChange={(id, name) => { addItem(id, name) }}
            />
        </div>
    )
}

function EnumValue({ field, entry, onChange, className }: {
    field: FilterFieldMeta
    entry: FilterEntry
    onChange: (entry: FilterEntry) => void
    className?: string
}) {
    if (!field.enumValues || field.enumValues.length === 0) {
        return <StringValue field={field} entry={entry} onChange={onChange} className={className} />
    }
    const valStr = typeof entry.value === "string" ? entry.value : "null"
    return (
        <Select
            value={valStr}
            onValueChange={(val) => onChange(setValue(entry, val === "null" ? undefined : val))}
        >
            <SelectTrigger aria-label={field.label} className={cn("h-7 text-xs", className)}>
                <SelectValue placeholder="Не выбрано" />
            </SelectTrigger>
            <SelectContent>
                <SelectItem value="null" className="text-xs text-muted-foreground">Не выбрано</SelectItem>
                {field.enumValues.map((ev) => (
                    <SelectItem key={ev.value} value={ev.value} className="text-xs">
                        {ev.label}
                    </SelectItem>
                ))}
            </SelectContent>
        </Select>
    )
}

export function FilterValueControl({
    field,
    entry,
    onChange,
    className,
    autoFocus,
}: FilterValueControlProps) {
    const op = entry.operator
    if (isNullaryOperator(op)) {
        return (
            <div className="text-[10px] italic text-muted-foreground/60 pl-0.5">
                {op === "null" ? "Поле не заполнено" : "Поле заполнено"}
            </div>
        )
    }

    const list = isListOperator(op)
    const between = isBetweenOperator(op)

    if (field.fieldType === "boolean") {
        return <BooleanValue entry={entry} onChange={onChange} className={className} />
    }
    if (field.fieldType === "string") {
        return <StringValue field={field} entry={entry} onChange={onChange} className={className} autoFocus={autoFocus} />
    }
    if (field.fieldType === "number" || field.fieldType === "money") {
        const error = exactFilterEntryError(field, entry)
        return <div className="grid gap-1">
            {between
                ? <BetweenNumberValue field={field} entry={entry} onChange={onChange} className={className} />
                : <NumberValue field={field} entry={entry} onChange={onChange} className={className} autoFocus={autoFocus} />}
            {field.fieldType === "money" && <p className="text-xs text-muted-foreground">В минимальных единицах валюты: например, 100 копеек = 1 рубль.</p>}
            {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
        </div>
    }
    if (field.fieldType === "date") {
        return between
            ? <BetweenDateValue entry={entry} onChange={onChange} className={className} />
            : <DateValue entry={entry} onChange={onChange} className={className} />
    }
    if (field.fieldType === "reference") {
        return list
            ? <ReferenceList field={field} entry={entry} onChange={onChange} className={className} />
            : <ReferenceSingle field={field} entry={entry} onChange={onChange} className={className} />
    }
    if (field.fieldType === "enum") {
        return list
            ? <StringValue field={field} entry={entry} onChange={onChange} className={className} autoFocus={autoFocus} />
            : <EnumValue field={field} entry={entry} onChange={onChange} className={className} />
    }
    return <StringValue field={field} entry={entry} onChange={onChange} className={className} autoFocus={autoFocus} />
}
