"use client"

import { useState, useMemo } from "react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogFooter,
    DialogDescription,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
    ChevronRight,
    ChevronLeft,
    ChevronsRight,
    ChevronsLeft,
    ChevronUp,
    ChevronDown,
    Search,
    FolderOpen,
    Minus,
    Plus,
    GripVertical,
} from "lucide-react"
import { cn } from "@/lib/utils"

// ── Types ──────────────────────────────────────────────────────────────

/** Type of field — determines icon and filter rendering */
export type FieldType =
    | "string"
    | "number"
    | "money"
    | "date"
    | "boolean"
    | "reference"
    | "enum"

/**
 * Metadata for a single filterable field of a document.
 * Mirrors the structure hierarchy: header fields at root,
 * tabular section fields nested inside groups.
 */
export interface FilterFieldMeta {
    /** Unique key, e.g. "header.counterparty" or "lines.nomenclature" */
    key: string
    /** Human-readable label */
    label: string
    /** Field data type */
    fieldType: FieldType
    /** Group label (e.g. "Товары", "Услуги") — acts like a folder */
    group?: string
    /** API endpoint path for reference fields, e.g. "/catalog/warehouses" */
    refEndpoint?: string
    /** Storage multiplier for scaled numeric types (e.g. 10000 for Quantity, 100 for Money). 0 = no scaling. */
    valueScale?: number
}

interface FilterConfigDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    /** Full list of fields available for filtering */
    availableFields: FilterFieldMeta[]
    /** Currently selected filter keys */
    selectedKeys: string[]
    /** Called when user confirms selection */
    onApply: (selectedKeys: string[]) => void
}

// ── Helpers ─────────────────────────────────────────────────────────────

const FIELD_TYPE_ICONS: Record<FieldType, string> = {
    string: "Abc",
    number: "123",
    money: "💰",
    date: "📅",
    boolean: "☑",
    reference: "🔗",
    enum: "▤",
}

function FieldTypeIcon({ type }: { type: FieldType }) {
    const label = FIELD_TYPE_ICONS[type]
    return (
        <span
            className={cn(
                "inline-flex h-4 w-4 items-center justify-center rounded-[3px] text-[9px] font-bold shrink-0",
                type === "string" && "bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300",
                (type === "number" || type === "money") && "bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300",
                type === "date" && "bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300",
                type === "boolean" && "bg-violet-100 text-violet-700 dark:bg-violet-900/40 dark:text-violet-300",
                type === "reference" && "bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300",
                type === "enum" && "bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300"
            )}
        >
            {label}
        </span>
    )
}

// ── Component ───────────────────────────────────────────────────────────

export function FilterConfigDialog({
    open,
    onOpenChange,
    availableFields,
    selectedKeys: initialSelectedKeys,
    onApply,
}: FilterConfigDialogProps) {
    const [selected, setSelected] = useState<string[]>(initialSelectedKeys)
    const [availableHighlight, setAvailableHighlight] = useState<string | null>(null)
    const [selectedHighlight, setSelectedHighlight] = useState<string | null>(null)
    const [searchQuery, setSearchQuery] = useState("")
    const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set())

    // Reset internal state when dialog opens
    const handleOpenChange = (isOpen: boolean) => {
        if (isOpen) {
            setSelected(initialSelectedKeys)
            setSearchQuery("")
            setAvailableHighlight(null)
            setSelectedHighlight(null)
            // Expand all groups by default
            const groups = new Set(availableFields.map((f) => f.group).filter(Boolean) as string[])
            setExpandedGroups(groups)
        }
        onOpenChange(isOpen)
    }

    // ⚡ Perf: O(1) lookup Set for selected keys — avoids O(N) .includes() in filter loop.
    const selectedSet = useMemo(() => new Set(selected), [selected])

    // Build grouped structure for available fields
    const groupedAvailable = useMemo(() => {
        const query = searchQuery.toLowerCase()
        const filtered = availableFields.filter((f) => {
            if (selectedSet.has(f.key)) return false
            if (query && !f.label.toLowerCase().includes(query)) return false
            return true
        })

        const groups = new Map<string, FilterFieldMeta[]>()
        const ungrouped: FilterFieldMeta[] = []

        for (const field of filtered) {
            if (field.group) {
                if (!groups.has(field.group)) groups.set(field.group, [])
                groups.get(field.group)!.push(field)
            } else {
                ungrouped.push(field)
            }
        }

        return { groups, ungrouped }
    }, [availableFields, selectedSet, searchQuery])

    // Selected fields in order
    const selectedFields = useMemo(() => {
        const fieldMap = new Map(availableFields.map((f) => [f.key, f]))
        return selected.map((key) => fieldMap.get(key)).filter(Boolean) as FilterFieldMeta[]
    }, [availableFields, selected])

    const toggleGroup = (group: string) => {
        setExpandedGroups((prev) => {
            const next = new Set(prev)
            if (next.has(group)) next.delete(group)
            else next.add(group)
            return next
        })
    }

    // ── Actions ─────

    const moveToSelected = () => {
        if (!availableHighlight || selectedSet.has(availableHighlight)) return
        setSelected((prev) => [...prev, availableHighlight])
        setAvailableHighlight(null)
    }

    const moveToAvailable = () => {
        if (!selectedHighlight) return
        setSelected((prev) => prev.filter((k) => k !== selectedHighlight))
        setSelectedHighlight(null)
    }

    const moveAllToSelected = () => {
        const allKeys = availableFields.map((f) => f.key)
        setSelected(allKeys)
        setAvailableHighlight(null)
    }

    const moveAllToAvailable = () => {
        setSelected([])
        setSelectedHighlight(null)
    }

    const moveSelectedUp = () => {
        if (!selectedHighlight) return
        setSelected((prev) => {
            const idx = prev.indexOf(selectedHighlight)
            if (idx <= 0) return prev
            const next = [...prev]
                ;[next[idx - 1], next[idx]] = [next[idx], next[idx - 1]]
            return next
        })
    }

    const moveSelectedDown = () => {
        if (!selectedHighlight) return
        setSelected((prev) => {
            const idx = prev.indexOf(selectedHighlight)
            if (idx === -1 || idx >= prev.length - 1) return prev
            const next = [...prev]
                ;[next[idx], next[idx + 1]] = [next[idx + 1], next[idx]]
            return next
        })
    }

    const handleApply = () => {
        onApply(selected)
        onOpenChange(false)
    }

    return (
        <Dialog open={open} onOpenChange={handleOpenChange}>
            <DialogContent className="max-w-[720px] gap-0 p-0">
                <DialogHeader className="px-5 pt-5 pb-3">
                    <DialogTitle className="text-base">Настройка фильтров</DialogTitle>
                    <DialogDescription className="text-xs text-muted-foreground">
                        Выберите поля для фильтрации из структуры документа
                    </DialogDescription>
                </DialogHeader>

                {/* Search bar */}
                <div className="px-5 pb-3">
                    <div className="relative">
                        <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                        <Input
                            value={searchQuery}
                            onChange={(e) => setSearchQuery(e.target.value)}
                            placeholder="Поиск (Ctrl+F)"
                            className="h-8 pl-8 text-xs"
                        />
                    </div>
                </div>

                {/* Two-column layout */}
                <div className="flex gap-0 border-y">
                    {/* Left: Available fields */}
                    <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 border-b bg-muted/40 px-3 py-1.5">
                            <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                                Доступные фильтры
                            </span>
                        </div>
                        <ScrollArea className="h-[340px]">
                            <div className="py-1">
                                {/* Ungrouped fields */}
                                {groupedAvailable.ungrouped.map((field) => (
                                    <button
                                        key={field.key}
                                        className={cn(
                                            "flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors hover:bg-accent/50",
                                            availableHighlight === field.key && "bg-primary/10 text-primary"
                                        )}
                                        onClick={() => setAvailableHighlight(field.key)}
                                        onDoubleClick={() => {
                                            setAvailableHighlight(field.key)
                                            if (!selectedSet.has(field.key)) {
                                                setSelected((prev) => [...prev, field.key])
                                            }
                                        }}
                                    >
                                        <Minus className="h-3 w-3 text-muted-foreground/50 shrink-0" />
                                        <FieldTypeIcon type={field.fieldType} />
                                        <span className="truncate">{field.label}</span>
                                    </button>
                                ))}

                                {/* Grouped fields */}
                                {Array.from(groupedAvailable.groups.entries()).map(([group, fields]) => (
                                    <div key={group}>
                                        <button
                                            className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs font-medium text-muted-foreground hover:bg-accent/30 transition-colors"
                                            onClick={() => toggleGroup(group)}
                                        >
                                            <FolderOpen className="h-3.5 w-3.5 text-amber-500 shrink-0" />
                                            {expandedGroups.has(group) ? (
                                                <Minus className="h-3 w-3 text-muted-foreground/60 shrink-0" />
                                            ) : (
                                                <Plus className="h-3 w-3 text-muted-foreground/60 shrink-0" />
                                            )}
                                            <span>{group}</span>
                                        </button>
                                        {expandedGroups.has(group) &&
                                            fields.map((field) => (
                                                <button
                                                    key={field.key}
                                                    className={cn(
                                                        "flex w-full items-center gap-2 pl-8 pr-3 py-1.5 text-left text-xs transition-colors hover:bg-accent/50",
                                                        availableHighlight === field.key && "bg-primary/10 text-primary"
                                                    )}
                                                    onClick={() => setAvailableHighlight(field.key)}
                                                    onDoubleClick={() => {
                                                        setAvailableHighlight(field.key)
                                                        if (!selectedSet.has(field.key)) {
                                                            setSelected((prev) => [...prev, field.key])
                                                        }
                                                    }}
                                                >
                                                    <Minus className="h-3 w-3 text-muted-foreground/50 shrink-0" />
                                                    <FieldTypeIcon type={field.fieldType} />
                                                    <span className="truncate">{field.label}</span>
                                                </button>
                                            ))}
                                    </div>
                                ))}

                                {groupedAvailable.ungrouped.length === 0 &&
                                    groupedAvailable.groups.size === 0 && (
                                        <div className="px-3 py-6 text-center text-xs text-muted-foreground/60">
                                            Все поля уже выбраны
                                        </div>
                                    )}
                            </div>
                        </ScrollArea>
                    </div>

                    {/* Center: Transfer buttons */}
                    <div className="flex flex-col items-center justify-center gap-1 border-x px-2 bg-muted/20">
                        <Button
                            variant="outline"
                            size="icon"
                            className="h-7 w-7"
                            onClick={moveToSelected}
                            disabled={!availableHighlight}
                            title="Добавить выбранное"
                        >
                            <ChevronRight className="h-4 w-4" />
                        </Button>
                        <Button
                            variant="outline"
                            size="icon"
                            className="h-7 w-7"
                            onClick={moveToAvailable}
                            disabled={!selectedHighlight}
                            title="Убрать выбранное"
                        >
                            <ChevronLeft className="h-4 w-4" />
                        </Button>
                        <div className="h-px w-5 bg-border my-1" />
                        <Button
                            variant="outline"
                            size="icon"
                            className="h-7 w-7"
                            onClick={moveAllToSelected}
                            title="Добавить все"
                        >
                            <ChevronsRight className="h-4 w-4" />
                        </Button>
                        <Button
                            variant="outline"
                            size="icon"
                            className="h-7 w-7"
                            onClick={moveAllToAvailable}
                            title="Убрать все"
                        >
                            <ChevronsLeft className="h-4 w-4" />
                        </Button>
                    </div>

                    {/* Right: Selected filters */}
                    <div className="flex-1 min-w-0">
                        <div className="flex items-center justify-between border-b bg-muted/40 px-3 py-1.5">
                            <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                                Выбранные фильтры
                            </span>
                            {/* Up/Down controls */}
                            <div className="flex items-center gap-0.5">
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-5 w-5"
                                    onClick={moveSelectedUp}
                                    disabled={!selectedHighlight}
                                >
                                    <ChevronUp className="h-3.5 w-3.5" />
                                </Button>
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-5 w-5"
                                    onClick={moveSelectedDown}
                                    disabled={!selectedHighlight}
                                >
                                    <ChevronDown className="h-3.5 w-3.5" />
                                </Button>
                            </div>
                        </div>
                        <ScrollArea className="h-[340px]">
                            <div className="py-1">
                                {selectedFields.map((field) => (
                                    <button
                                        key={field.key}
                                        className={cn(
                                            "flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors hover:bg-accent/50 group",
                                            selectedHighlight === field.key && "bg-primary/10 text-primary"
                                        )}
                                        onClick={() => setSelectedHighlight(field.key)}
                                        onDoubleClick={() => {
                                            setSelected((prev) => prev.filter((k) => k !== field.key))
                                            setSelectedHighlight(null)
                                        }}
                                    >
                                        <GripVertical className="h-3 w-3 text-muted-foreground/30 group-hover:text-muted-foreground/60 shrink-0" />
                                        <FieldTypeIcon type={field.fieldType} />
                                        <span className="truncate flex-1">{field.label}</span>
                                    </button>
                                ))}
                                {selectedFields.length === 0 && (
                                    <div className="px-3 py-6 text-center text-xs text-muted-foreground/60">
                                        Нет выбранных фильтров
                                    </div>
                                )}
                            </div>
                        </ScrollArea>
                    </div>
                </div>

                {/* Footer */}
                <DialogFooter className="px-5 py-3">
                    <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
                        Отмена
                    </Button>
                    <Button size="sm" onClick={handleApply}>
                        Завершить редактирование
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
