"use client"

import { useState, useMemo, useCallback, useEffect } from "react"
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
    Check,
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
    /** Referenced entity name (e.g. "Counterparty") for deep filtering */
    refEntityName?: string
    /** Nested filterable fields of the referenced entity */
    refFields?: FilterFieldMeta[]
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

// ── Tree node type ─────────────────────────────────────────────────────

/**
 * A node in the flattened rendering tree.
 * Three kinds of nodes:
 *
 * 1. **Pure group** (isGroupOnly=true): table part header like "Товары".
 *    Not selectable — only expands/collapses children. Uses folder icon.
 *
 * 2. **Expandable field** (hasChildren=true, isGroupOnly=false): a reference
 *    field like "Организация" that is BOTH selectable (click/dblclick adds
 *    it to filters) AND expandable (+/- toggles its refFields children).
 *    Single unified row — no duplication.
 *
 * 3. **Leaf field** (hasChildren=false, isGroupOnly=false): a regular scalar
 *    field like "ИНН" — only selectable.
 */
interface TreeNode extends FilterFieldMeta {
    /** Visual indentation depth: 0=header, 1=inside group, 2=deep nested */
    depth: number
    /** This node has expandable children (refFields or table part columns) */
    hasChildren?: boolean
    /** Toggle key used to expand/collapse this node's children */
    toggleKey?: string
    /** This node is a pure group header (table part) — not individually selectable */
    isGroupOnly?: boolean
    /**
     * Array of parent toggle keys that must ALL be expanded for this node
     * to be visible. E.g. a field inside "Товары → Номенклатура" needs
     * ["tp:Товары", "ref:items.nomenclatureId"] to both be expanded.
     */
    parentToggleKeys: string[]
}

/**
 * Build a flat tree-order array from availableFields.
 *
 * Produced structure example:
 *   [+] Организация          (depth=0, selectable + expandable, toggleKey="ref:organizationId")
 *       - Код                (depth=1, leaf, parents=["ref:organizationId"])
 *       - ИНН                (depth=1, leaf, parents=["ref:organizationId"])
 *   — № документа поставщика (depth=0, leaf)
 *   [▶] Товары               (depth=0, groupOnly, toggleKey="tp:Товары")
 *       — Номер строки       (depth=1, leaf, parents=["tp:Товары"])
 *       [+] Номенклатура     (depth=1, selectable+expandable, parents=["tp:Товары"], toggleKey="ref:items.nomenclatureId")
 *           - Код             (depth=2, leaf, parents=["tp:Товары", "ref:items.nomenclatureId"])
 */
function buildTree(fields: FilterFieldMeta[]): TreeNode[] {
    const result: TreeNode[] = []

    // Split into ungrouped (header) and grouped (table parts)
    const ungrouped: FilterFieldMeta[] = []
    const groups = new Map<string, FilterFieldMeta[]>()

    for (const f of fields) {
        if (f.group) {
            if (!groups.has(f.group)) groups.set(f.group, [])
            groups.get(f.group)!.push(f)
        } else {
            ungrouped.push(f)
        }
    }

    // ── Header fields ──
    for (const f of ungrouped) {
        const hasRefChildren = !!(f.refFields && f.refFields.length > 0)
        const toggleKey = hasRefChildren ? `ref:${f.key}` : undefined

        // The field itself — selectable. If it has refFields, also expandable.
        result.push({
            ...f,
            depth: 0,
            hasChildren: hasRefChildren,
            toggleKey,
            parentToggleKeys: [],
        })

        // Its refFields children
        if (hasRefChildren && f.refFields) {
            for (const rf of f.refFields) {
                result.push({
                    ...rf,
                    key: `${f.key}.${rf.key}`,
                    depth: 1,
                    parentToggleKeys: [toggleKey!],
                })
            }
        }
    }

    // ── Table part groups ──
    for (const [group, groupFields] of groups) {
        const tpToggle = `tp:${group}`

        // Table part header — pure group (not selectable)
        result.push({
            key: `__tp__${group}`,
            label: group,
            fieldType: "string",
            group,
            depth: 0,
            isGroupOnly: true,
            hasChildren: true,
            toggleKey: tpToggle,
            parentToggleKeys: [],
        })

        // Table part fields
        for (const f of groupFields) {
            const hasRefChildren = !!(f.refFields && f.refFields.length > 0)
            const refToggle = hasRefChildren ? `ref:${f.key}` : undefined

            // The field — selectable, optionally expandable
            result.push({
                ...f,
                depth: 1,
                hasChildren: hasRefChildren,
                toggleKey: refToggle,
                parentToggleKeys: [tpToggle],
            })

            // Its refFields children
            if (hasRefChildren && f.refFields) {
                for (const rf of f.refFields) {
                    result.push({
                        ...rf,
                        key: `${f.key}.${rf.key}`,
                        group,
                        depth: 2,
                        parentToggleKeys: [tpToggle, refToggle!],
                    })
                }
            }
        }
    }

    return result
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
    useEffect(() => {
        if (!open) return
        setSelected(initialSelectedKeys)
        setSearchQuery("")
        setAvailableHighlight(null)
        setSelectedHighlight(null)
        // Expand only table-part groups by default (ref sub-groups stay collapsed)
        const tree = buildTree(availableFields)
        const groups = new Set<string>()
        for (const n of tree) {
            if (n.isGroupOnly && n.toggleKey) groups.add(n.toggleKey)
        }
        setExpandedGroups(groups)
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [open])

    const handleOpenChange = (isOpen: boolean) => {
        onOpenChange(isOpen)
    }

    const selectedSet = useMemo(() => new Set(selected), [selected])

    const allNodes = useMemo(() => buildTree(availableFields), [availableFields])

    // Selectable nodes (everything except pure group headers)
    const selectableNodes = useMemo(
        () => allNodes.filter((n) => !n.isGroupOnly),
        [allNodes]
    )

    // Determine visibility: a node is visible if ALL its parentToggleKeys are expanded
    const isNodeVisible = useCallback(
        (node: TreeNode): boolean => {
            return node.parentToggleKeys.every((k) => expandedGroups.has(k))
        },
        [expandedGroups]
    )

    // Visible tree for the left panel (filtered by search, selection, and expansion)
    const visibleTree = useMemo(() => {
        const query = searchQuery.toLowerCase()

        // When searching, show matching leaves + their ancestors
        if (query) {
            const matchingKeys = new Set<string>()
            const neededToggles = new Set<string>()

            for (const n of allNodes) {
                if (n.isGroupOnly) continue
                if (!n.label.toLowerCase().includes(query)) continue
                matchingKeys.add(n.key)
                for (const pk of n.parentToggleKeys) neededToggles.add(pk)
            }

            return allNodes.filter((n) => {
                if (n.isGroupOnly || n.hasChildren) {
                    return n.toggleKey ? neededToggles.has(n.toggleKey) : false
                }
                return matchingKeys.has(n.key)
            })
        }

        // No search — show nodes based on parent expansion state.
        // Selected items stay visible so their children (refFields) remain accessible.
        return allNodes.filter((n) => {
            if (n.isGroupOnly) {
                return n.parentToggleKeys.every((k) => expandedGroups.has(k))
            }
            return isNodeVisible(n)
        })
    }, [allNodes, searchQuery, expandedGroups, isNodeVisible])

    // Selected fields for right panel
    const selectedFields = useMemo(() => {
        const nodeMap = new Map(selectableNodes.map((n) => [n.key, n]))
        return selected.map((key) => nodeMap.get(key)).filter(Boolean) as TreeNode[]
    }, [selectableNodes, selected])

    const toggleGroup = useCallback((toggleKey: string) => {
        setExpandedGroups((prev) => {
            const next = new Set(prev)
            if (next.has(toggleKey)) next.delete(toggleKey)
            else next.add(toggleKey)
            return next
        })
    }, [])

    // ── Actions ─────

    const moveToSelected = () => {
        if (!availableHighlight || selectedSet.has(availableHighlight)) return
        // Don't allow selecting pure group headers
        const node = allNodes.find((n) => n.key === availableHighlight)
        if (node?.isGroupOnly) return
        setSelected((prev) => [...prev, availableHighlight])
        setAvailableHighlight(null)
    }

    const moveToAvailable = () => {
        if (!selectedHighlight) return
        setSelected((prev) => prev.filter((k) => k !== selectedHighlight))
        setSelectedHighlight(null)
    }

    const moveAllToSelected = () => {
        const allKeys = selectableNodes.map((n) => n.key)
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

    // ── Render ─────

    /**
     * Render a single node in the available-fields tree.
     *
     * Three visual variants:
     * 1. Pure group header (table part) — folder icon, only toggles, not selectable
     * 2. Expandable selectable field (reference with refFields) — type icon + toggle button
     * 3. Leaf field — type icon, no toggle
     */
    function renderNode(node: TreeNode) {
        const indent = 12 + node.depth * 16
        const isHighlighted = availableHighlight === node.key
        const isExpanded = node.toggleKey ? expandedGroups.has(node.toggleKey) : false

        // ── Pure group header (e.g. "Товары") ──
        if (node.isGroupOnly) {
            return (
                <button
                    key={node.key}
                    className="flex w-full items-center gap-2 pr-3 py-1.5 text-left text-xs font-medium text-muted-foreground hover:bg-accent/30 transition-colors"
                    style={{ paddingLeft: `${indent}px` }}
                    onClick={() => node.toggleKey && toggleGroup(node.toggleKey)}
                >
                    <FolderOpen className="h-3.5 w-3.5 text-amber-500 shrink-0" />
                    {isExpanded ? (
                        <Minus className="h-3 w-3 text-muted-foreground/60 shrink-0" />
                    ) : (
                        <Plus className="h-3 w-3 text-muted-foreground/60 shrink-0" />
                    )}
                    <span>{node.label}</span>
                </button>
            )
        }

        // ── Selectable field (possibly expandable) ──
        const isAlreadySelected = selectedSet.has(node.key)

        return (
            <div
                key={node.key}
                className={cn(
                    "flex w-full items-center gap-2 pr-3 py-1.5 text-xs transition-colors",
                    isAlreadySelected
                        ? "opacity-50"
                        : "hover:bg-accent/50",
                    isHighlighted && !isAlreadySelected && "bg-primary/10 text-primary"
                )}
                style={{ paddingLeft: `${indent}px` }}
            >
                {/* Toggle button for expandable references */}
                {node.hasChildren ? (
                    <button
                        className="h-4 w-4 flex items-center justify-center shrink-0 rounded-sm hover:bg-accent text-muted-foreground/60 hover:text-muted-foreground"
                        onClick={(e) => {
                            e.stopPropagation()
                            if (node.toggleKey) toggleGroup(node.toggleKey)
                        }}
                        title={isExpanded ? "Свернуть" : "Развернуть"}
                    >
                        {isExpanded ? (
                            <Minus className="h-3 w-3" />
                        ) : (
                            <Plus className="h-3 w-3" />
                        )}
                    </button>
                ) : isAlreadySelected ? (
                    <Check className="h-3 w-3 text-primary/60 shrink-0" />
                ) : (
                    <Minus className="h-3 w-3 text-muted-foreground/50 shrink-0" />
                )}

                {/* Selectable area: click = highlight, dblclick = add */}
                <button
                    className={cn(
                        "flex items-center gap-2 flex-1 min-w-0 text-left",
                        isAlreadySelected && "cursor-default"
                    )}
                    onClick={() => {
                        if (!isAlreadySelected) setAvailableHighlight(node.key)
                    }}
                    onDoubleClick={() => {
                        if (!isAlreadySelected) {
                            setSelected((prev) => [...prev, node.key])
                        }
                    }}
                >
                    <FieldTypeIcon type={node.fieldType} />
                    <span className="truncate">{node.label}</span>
                </button>
            </div>
        )
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
                                {visibleTree.map((node) => renderNode(node))}

                                {visibleTree.length === 0 && (
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
