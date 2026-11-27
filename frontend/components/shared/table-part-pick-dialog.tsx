"use client"

/**
 * "Подбор" (1C): picks catalog items into a table part through a cart.
 *
 * Like 1C's selection form, the catalog is on one side and what was picked
 * on the other, and "Перенести в документ" moves the cart into the part.
 * Beyond 1C:
 *   - the cart sits beside the catalog, so it stays in view while searching;
 *   - the keyboard never leaves the search: ↑/↓ choose, Enter adds one,
 *     "3*гвозди" adds three of what "гвозди" finds, Shift+Enter jumps to the
 *     quantity, Ctrl/Cmd+Enter transfers;
 *   - every item shows how much the document and the cart already hold;
 *   - items the document already has add to its lines instead of repeating
 *     them (a switch, on by default);
 *   - closing with a filled cart asks first instead of dropping the picks.
 *
 * A hierarchical catalog shows its group tree on the left, as 1C does: a
 * group shows its items and those of its subgroups (in_hierarchy), search
 * works within it, and the last group is remembered per catalog.
 */

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { ChevronRight, Folder, FolderOpen, Layers, Loader2, Minus, PackagePlus, Plus, Search, X } from "lucide-react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { ScrollSentinel } from "@/components/shared/scroll-sentinel"
import { apiFetch } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { JsonObject } from "@/lib/json-value"
import { resolveEntityFromEndpoint, withReferenceListParams } from "@/lib/reference-utils"
import { buildColumnsFromFields, formatCellValue, type AutoColumn, type MetadataField } from "@/lib/metadata-columns"
import { formatSummaryNumber } from "@/lib/selection-summary"
import {
    addToCart, cartTotal, parseQuantity, setCartQuantity, splitQuantityPrefix, type CartLine, type PickTarget,
} from "@/lib/pick-lines"
import { buildGroupTree, groupPath, visibleGroups, type GroupFolder } from "@/lib/group-tree"
import type { AdvancedFilterItem, CursorListResponse } from "@/types/common"
import { isImeComposing } from "@/lib/keyboard-utils"

type ItemRow = JsonObject & { id: string }

export interface TablePartPickDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    /** The part's title, e.g. "Товары". */
    title: string
    target: PickTarget
    /** List endpoint of the picked catalog, e.g. "/catalog/products". */
    endpoint: string
    /** How much of each item the document already holds. */
    inDocument: ReadonlyMap<string, string>
    onTransfer: (cart: CartLine[], merge: boolean) => void
}

const PAGE = 50
/** Groups loaded for the tree: pages of this size, at most GROUP_PAGES of them. */
const GROUP_PAGE = 500
const GROUP_PAGES = 4
const MERGE_KEY = "metapus-pick-merge"
const GROUP_KEY = "metapus-pick-group:"

function readStored(key: string): string | null {
    try { return localStorage.getItem(key) } catch { return null }
}
function writeStored(key: string, value: string | null) {
    try {
        if (value) localStorage.setItem(key, value)
        else localStorage.removeItem(key)
    } catch { /* a per-viewer convenience only */ }
}
const columnsCache = new Map<string, AutoColumn[]>()
const FALLBACK_COLUMNS: AutoColumn[] = [
    { key: "code", label: "Код", type: "string" },
    { key: "name", label: "Наименование", type: "string" },
]

/** Hierarchy fields: a pick is never a group, and its place in the tree is not a column. */
const HIERARCHY_FIELDS = new Set(["parentId", "isFolder"])

/** Catalog columns for picking: code and name first, then up to two more of the item's own fields. */
function pickColumns(fields: MetadataField[]): AutoColumn[] {
    const own = fields.filter((field) => !HIERARCHY_FIELDS.has(field.name))
    const rank = (field: MetadataField) => (field.name === "code" ? 0 : field.name === "name" ? 1 : 2)
    const ordered = own.map((field, index) => ({ field, index })).sort((a, b) => rank(a.field) - rank(b.field) || a.index - b.index).map(({ field }) => field)
    return buildColumnsFromFields(ordered, 4)
}

function itemName(item: JsonObject): string {
    return String(item.name ?? item.number ?? item.code ?? "")
}

function readMerge(): boolean {
    try {
        return localStorage.getItem(MERGE_KEY) !== "false"
    } catch {
        return true
    }
}

function Kbd({ children }: { children: React.ReactNode }) {
    return <kbd className="rounded border bg-muted px-1 font-sans text-[10px] font-medium text-muted-foreground">{children}</kbd>
}

export function TablePartPickDialog({ open, onOpenChange, title, target, endpoint, inDocument, onTransfer }: TablePartPickDialogProps) {
    const integer = target.quantity?.type === "integer"
    const hasQuantity = target.quantity !== null
    const resolved = useMemo(() => resolveEntityFromEndpoint(endpoint), [endpoint])
    const catalogTitle = resolved?.entity.presentation.plural ?? target.item.label ?? "Справочник"
    const entityName = resolved?.entityName ?? ""

    // ── Catalog columns from metadata (cached per entity) ───────────────
    const [loadedColumns, setLoadedColumns] = useState<Record<string, AutoColumn[]>>({})
    const columns = columnsCache.get(entityName) ?? loadedColumns[entityName] ?? FALLBACK_COLUMNS
    useEffect(() => {
        if (!open || !entityName || columnsCache.has(entityName)) return
        let cancelled = false
        apiFetch<{ fields: MetadataField[] }>(`/meta/${entityName}`)
            .then((meta) => {
                const built = pickColumns(meta.fields)
                const list = built.length ? built : FALLBACK_COLUMNS
                columnsCache.set(entityName, list)
                if (!cancelled) setLoadedColumns((previous) => ({ ...previous, [entityName]: list }))
            })
            .catch(() => {})
        return () => { cancelled = true }
    }, [open, entityName])

    // ── Group tree (hierarchical catalogs) ───────────────────────────────
    // The catalog's folders, loaded once per opening; none means a flat
    // catalog (or one without groups), and then there is no tree.
    const [folders, setFolders] = useState<GroupFolder[] | null>(null)
    const [group, setGroup] = useState<string | null>(null)
    const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())
    const treeRef = useRef<HTMLDivElement>(null)
    useEffect(() => {
        if (!open) return
        let cancelled = false
        const filter: AdvancedFilterItem[] = [{ field: "is_folder", operator: "eq", value: true }]
        const load = async () => {
            const found: GroupFolder[] = []
            let after: string | undefined
            for (let page = 0; page < GROUP_PAGES; page++) {
                const result = await apiFetch<CursorListResponse<ItemRow>>(withReferenceListParams(endpoint, { limit: GROUP_PAGE, orderBy: "name", filter, after }))
                for (const item of result.items ?? []) {
                    if (item.isFolder !== true) continue
                    found.push({ id: item.id, name: itemName(item), parentId: typeof item.parentId === "string" && item.parentId ? item.parentId : null })
                }
                if (!result.hasMore || !result.nextCursor) break
                after = result.nextCursor
            }
            return found
        }
        load()
            .then((found) => {
                if (cancelled) return
                setFolders(found)
                // Reopening returns to the group the user picked from last time.
                const last = readStored(GROUP_KEY + endpoint)
                if (last && found.some((folder) => folder.id === last)) {
                    setGroup(last)
                    setExpanded(new Set(groupPath(found, last).slice(0, -1)))
                }
            })
            .catch(() => { if (!cancelled) setFolders([]) })
        return () => { cancelled = true }
    }, [open, endpoint])
    const hasTree = !!folders && folders.length > 0
    const tree = useMemo(() => buildGroupTree(folders ?? []), [folders])
    const treeRows = visibleGroups(tree, expanded)
    const groupName = group ? folders?.find((folder) => folder.id === group)?.name : undefined
    const chooseGroup = (id: string | null) => {
        setGroup(id)
        writeStored(GROUP_KEY + endpoint, id)
    }
    const toggleGroup = (id: string, open?: boolean) => {
        setExpanded((previous) => {
            const next = new Set(previous)
            if (open ?? !next.has(id)) next.add(id)
            else next.delete(id)
            return next
        })
    }
    /** Items of the chosen group and its subgroups; groups themselves are never listed. */
    const listFilter = useMemo<AdvancedFilterItem[] | undefined>(() => {
        if (!hasTree) return undefined
        const items: AdvancedFilterItem[] = [{ field: "is_folder", operator: "eq", value: false }]
        if (group) items.push({ field: "parent_id", operator: "in_hierarchy", value: group })
        return items
    }, [hasTree, group])

    // The tree works from the keyboard like a Finder sidebar: ↑/↓ choose a
    // group (the list follows), ←/→ fold and unfold, Tab goes back to search.
    const onTreeKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
        const index = group ? treeRows.findIndex((row) => row.node.id === group) : -1
        const move = (to: number) => {
            event.preventDefault()
            if (to < 0) return chooseGroup(null)
            const row = treeRows[Math.min(to, treeRows.length - 1)]
            if (row) chooseGroup(row.node.id)
        }
        if (event.key === "ArrowDown") return move(index + 1)
        if (event.key === "ArrowUp") return move(index - 1)
        const row = index === -1 ? undefined : treeRows[index]
        if (event.key === "ArrowRight" && row?.hasChildren) {
            event.preventDefault()
            if (!expanded.has(row.node.id)) toggleGroup(row.node.id, true)
            else move(index + 1)
        }
        if (event.key === "ArrowLeft" && row) {
            event.preventDefault()
            if (expanded.has(row.node.id)) toggleGroup(row.node.id, false)
            else if (row.node.parentId && folders?.some((folder) => folder.id === row.node.parentId)) chooseGroup(row.node.parentId)
            else chooseGroup(null)
        }
        if (event.key === "Enter") {
            event.preventDefault()
            searchRef.current?.focus()
        }
    }
    useEffect(() => {
        treeRef.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.scrollIntoView({ block: "nearest" })
    }, [group])

    // ── Search: "3*гвозди" is a quantity and a query ────────────────────
    const [search, setSearch] = useState("")
    const { quantity: prefixQuantity, text: query } = splitQuantityPrefix(search, integer)
    const [items, setItems] = useState<ItemRow[]>([])
    const [loading, setLoading] = useState(false)
    const [loadingMore, setLoadingMore] = useState(false)
    const [nextCursor, setNextCursor] = useState<string | null>(null)
    const [highlight, setHighlight] = useState(0)
    const requestSeq = useRef(0)

    useEffect(() => {
        if (!open) return
        const seq = ++requestSeq.current
        const timer = setTimeout(() => {
            setLoading(true)
            apiFetch<CursorListResponse<ItemRow>>(withReferenceListParams(endpoint, { limit: PAGE, search: query.trim() || undefined, orderBy: "name", filter: listFilter }))
                .then((result) => {
                    if (seq !== requestSeq.current) return
                    setItems((result.items ?? []).filter((item) => item.isFolder !== true))
                    setNextCursor(result.hasMore ? result.nextCursor ?? null : null)
                    setHighlight(0)
                })
                .catch(() => { if (seq === requestSeq.current) setItems([]) })
                .finally(() => { if (seq === requestSeq.current) setLoading(false) })
        }, query ? 180 : 0)
        return () => clearTimeout(timer)
    }, [open, endpoint, query, listFilter])

    const loadMore = useCallback(() => {
        if (!nextCursor || loadingMore) return
        const seq = requestSeq.current
        setLoadingMore(true)
        apiFetch<CursorListResponse<ItemRow>>(withReferenceListParams(endpoint, { limit: PAGE, search: query.trim() || undefined, orderBy: "name", after: nextCursor, filter: listFilter }))
            .then((result) => {
                if (seq !== requestSeq.current) return
                setItems((previous) => [...previous, ...(result.items ?? []).filter((item) => item.isFolder !== true)])
                setNextCursor(result.hasMore ? result.nextCursor ?? null : null)
            })
            .catch(() => {})
            .finally(() => setLoadingMore(false))
    }, [endpoint, query, nextCursor, loadingMore, listFilter])

    // ── Cart ────────────────────────────────────────────────────────────
    const [cart, setCart] = useState<CartLine[]>([])
    const [merge, setMerge] = useState(readMerge)
    const [confirmClose, setConfirmClose] = useState(false)
    const [flash, setFlash] = useState<{ id: string; seq: number } | null>(null)
    const cartQuantity = new Map(cart.map((line) => [line.id, line.quantity]))
    const searchRef = useRef<HTMLInputElement>(null)
    const listRef = useRef<HTMLDivElement>(null)
    const cartRef = useRef<HTMLDivElement>(null)

    const reset = () => {
        setSearch("")
        setFolders(null)
        setCart([])
        setConfirmClose(false)
        setFlash(null)
    }
    const close = () => {
        reset()
        onOpenChange(false)
    }
    /** Closing keeps the picks safe: with a filled cart it asks first. */
    const requestClose = () => {
        if (cart.length) setConfirmClose(true)
        else close()
    }

    const pick = (item: ItemRow, focusQuantity = false) => {
        const quantity = prefixQuantity ?? "1"
        setCart((previous) => addToCart(previous, { id: item.id, name: itemName(item), code: typeof item.code === "string" ? item.code : undefined }, quantity))
        setFlash({ id: item.id, seq: Date.now() })
        setConfirmClose(false)
        // A quantity typed before "*" is used once; the query stays for the next pick.
        if (prefixQuantity) setSearch(query)
        if (focusQuantity && hasQuantity) {
            requestAnimationFrame(() => {
                const input = cartRef.current?.querySelector<HTMLInputElement>(`input[data-cart-id="${CSS.escape(item.id)}"]`)
                input?.focus()
                input?.select()
            })
        }
    }

    const transfer = () => {
        if (!cart.length) return
        onTransfer(cart, merge)
        close()
    }

    // The highlighted item stays in view as ↑/↓ move it.
    useEffect(() => {
        listRef.current?.querySelector<HTMLElement>(`[data-pick-index="${highlight}"]`)?.scrollIntoView({ block: "nearest" })
    }, [highlight])

    const onSearchKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
        const last = items.length - 1
        const step = (delta: number) => { event.preventDefault(); setHighlight((index) => Math.max(0, Math.min(last, index + delta))) }
        if (event.key === "ArrowDown") return step(1)
        if (event.key === "ArrowUp") return step(-1)
        if (event.key === "PageDown") return step(10)
        if (event.key === "PageUp") return step(-10)
        if (event.key === "Enter") {
            event.preventDefault()
            if (event.metaKey || event.ctrlKey) return transfer()
            const item = items[highlight]
            if (item) pick(item, event.shiftKey)
        }
    }

    const onCartQuantity = (id: string, text: string) => {
        const quantity = parseQuantity(text, integer)
        if (quantity) setCart((previous) => setCartQuantity(previous, id, quantity))
    }
    const stepCart = (line: CartLine, delta: 1 | -1) => {
        const next = Number(line.quantity) + delta
        setCart((previous) => setCartQuantity(previous, line.id, String(Math.max(0, next))))
    }

    const total = cartTotal(cart)

    return (
        <Dialog open={open} onOpenChange={(next) => { if (!next) requestClose() }}>
            <DialogContent
                className="flex h-[min(86vh,760px)] w-[min(1120px,96vw)] max-w-none flex-col gap-0 overflow-hidden p-0"
                onOpenAutoFocus={(event) => { event.preventDefault(); searchRef.current?.focus() }}
                onEscapeKeyDown={(event) => {
                    // Escape clears the search first, then asks before dropping picks.
                    if (search) { event.preventDefault(); setSearch(""); return }
                    if (cart.length) { event.preventDefault(); setConfirmClose(true) }
                }}
                onInteractOutside={(event) => { if (cart.length) { event.preventDefault(); setConfirmClose(true) } }}
            >
                <DialogHeader className="border-b px-5 py-3.5 text-left">
                    <DialogTitle className="flex items-center gap-2 text-base">
                        <PackagePlus className="h-4 w-4 text-muted-foreground" />
                        Подбор: {title}
                    </DialogTitle>
                    <DialogDescription className="text-xs">
                        {catalogTitle} → «{target.item.label || target.item.name}»{hasQuantity ? `, «${target.quantity?.label || target.quantity?.name}»` : ""}
                    </DialogDescription>
                </DialogHeader>

                <div className={cn("grid min-h-0 flex-1 grid-cols-1",
                    hasTree ? "md:grid-cols-[210px_minmax(0,1fr)_320px]" : "md:grid-cols-[minmax(0,1fr)_340px]")}>
                    {/* ── Groups ──────────────────────────────────────────── */}
                    {hasTree && (
                        <div className="hidden min-h-0 flex-col border-r bg-muted/20 md:flex">
                            <div className="border-b px-3 py-2.5 text-xs font-medium text-muted-foreground">Группы</div>
                            <div ref={treeRef} role="tree" aria-label="Группы" tabIndex={0} onKeyDown={onTreeKeyDown}
                                className="min-h-0 flex-1 overflow-auto py-1 text-sm outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-ring">
                                <div role="treeitem" aria-selected={group === null} aria-level={1}
                                    className={cn("mx-1 flex cursor-pointer items-center gap-1.5 rounded px-2 py-1",
                                        group === null ? "bg-accent font-medium shadow-[inset_2px_0_0_hsl(var(--ring))]" : "hover:bg-muted/60")}
                                    onClick={() => chooseGroup(null)}>
                                    <Layers className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                                    <span className="truncate">Все позиции</span>
                                </div>
                                {treeRows.map(({ node, depth, hasChildren }) => {
                                    const isOpen = expanded.has(node.id)
                                    const selected = group === node.id
                                    return (
                                        <div key={node.id} role="treeitem" aria-selected={selected} aria-level={depth + 2}
                                            aria-expanded={hasChildren ? isOpen : undefined}
                                            className={cn("mx-1 flex cursor-pointer items-center gap-1 rounded py-1 pr-2",
                                                selected ? "bg-accent font-medium shadow-[inset_2px_0_0_hsl(var(--ring))]" : "hover:bg-muted/60")}
                                            style={{ paddingLeft: 4 + depth * 14 }}
                                            onClick={() => chooseGroup(node.id)}
                                            onDoubleClick={() => hasChildren && toggleGroup(node.id)}>
                                            <button type="button" tabIndex={-1} aria-label={isOpen ? "Свернуть" : "Развернуть"}
                                                className={cn("flex h-4 w-4 shrink-0 items-center justify-center rounded text-muted-foreground hover:text-foreground", !hasChildren && "invisible")}
                                                onClick={(event) => { event.stopPropagation(); toggleGroup(node.id) }}>
                                                <ChevronRight className={cn("h-3 w-3 transition-transform duration-150 motion-reduce:transition-none", isOpen && "rotate-90")} />
                                            </button>
                                            {selected || isOpen
                                                ? <FolderOpen className="h-3.5 w-3.5 shrink-0 text-primary/80" />
                                                : <Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
                                            <span className="truncate">{node.name}</span>
                                        </div>
                                    )
                                })}
                            </div>
                        </div>
                    )}
                    {/* ── Catalog ─────────────────────────────────────────── */}
                    <div className="flex min-h-0 flex-col border-b md:border-b-0 md:border-r">
                        <div className="flex items-center gap-2 border-b px-3 py-2">
                            <div className="relative flex-1">
                                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                                <Input ref={searchRef} value={search} onChange={(event) => setSearch(event.target.value)} onKeyDown={onSearchKeyDown}
                                    placeholder={hasQuantity ? "Код или наименование · 3*гвозди — сразу три" : "Код или наименование"}
                                    aria-label="Поиск в справочнике" aria-controls="pick-list"
                                    aria-activedescendant={items[highlight] ? `pick-${items[highlight].id}` : undefined}
                                    className="h-8 pl-8 text-sm" />
                            </div>
                            {groupName && (
                                <span className="flex max-w-48 shrink-0 items-center gap-1 rounded-md border bg-background py-0.5 pl-2 pr-0.5 text-xs">
                                    <FolderOpen className="h-3 w-3 shrink-0 text-primary/80" />
                                    <span className="truncate" title={`Поиск в группе «${groupName}» и её подгруппах`}>{groupName}</span>
                                    <button type="button" aria-label="Искать во всех группах" className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
                                        onMouseDown={(event) => event.preventDefault()} onClick={() => chooseGroup(null)}>
                                        <X className="h-3 w-3" />
                                    </button>
                                </span>
                            )}
                            {prefixQuantity && (
                                <span className="shrink-0 rounded-md bg-primary/10 px-2 py-1 text-xs font-medium tabular-nums text-primary animate-in fade-in-0 zoom-in-95 duration-150 motion-reduce:animate-none">
                                    × {formatSummaryNumber(prefixQuantity)}
                                </span>
                            )}
                            {loading && <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground" />}
                        </div>
                        <div ref={listRef} id="pick-list" role="listbox" aria-label={catalogTitle} className="min-h-0 flex-1 overflow-auto">
                            <table className="w-full table-fixed border-collapse text-sm">
                                <thead className="sticky top-0 z-10 bg-muted/85 text-xs text-muted-foreground backdrop-blur-sm">
                                    <tr className="border-b">
                                        {columns.map((column) => (
                                            <th key={column.key} className={cn("truncate px-3 py-1.5 text-left font-medium", column.align === "right" && "text-right",
                                                column.key === "code" ? "w-24" : column.key !== "name" && "w-32")}>{column.label}</th>
                                        ))}
                                        <th className="w-28 px-3 py-1.5 text-right font-medium">В документе</th>
                                        <th className="w-10" aria-hidden />
                                    </tr>
                                </thead>
                                <tbody>
                                    {items.map((item, index) => {
                                        const inCart = cartQuantity.get(item.id)
                                        const held = inDocument.get(item.id)
                                        return (
                                            <tr key={item.id} id={`pick-${item.id}`} role="option" aria-selected={index === highlight} data-pick-index={index}
                                                className={cn("group cursor-pointer border-b border-border/60",
                                                    index === highlight ? "bg-accent shadow-[inset_2px_0_0_hsl(var(--ring))]" : "hover:bg-muted/50")}
                                                onMouseDown={(event) => { event.preventDefault(); searchRef.current?.focus() }}
                                                onClick={() => setHighlight(index)}
                                                onDoubleClick={() => pick(item)}>
                                                {columns.map((column) => (
                                                    <td key={column.key} className={cn("truncate px-3 py-1.5", column.align === "right" && "text-right tabular-nums",
                                                        column.key === "code" && "font-mono text-xs text-muted-foreground")}>
                                                        {formatCellValue(item[column.key], column.type, item)}
                                                    </td>
                                                ))}
                                                <td className="px-3 py-1.5 text-right text-xs tabular-nums">
                                                    {held && <span className="text-muted-foreground">{formatSummaryNumber(held)}</span>}
                                                    {inCart && (
                                                        <span key={flash?.id === item.id ? flash.seq : undefined}
                                                            className="ml-1.5 inline-block rounded bg-primary/10 px-1.5 font-medium text-primary animate-in zoom-in-95 duration-150 motion-reduce:animate-none">
                                                            +{formatSummaryNumber(inCart)}
                                                        </span>
                                                    )}
                                                </td>
                                                <td className="px-1 py-1 text-right">
                                                    <Button type="button" size="icon" variant="ghost" tabIndex={-1} aria-label={`Добавить «${itemName(item)}»`}
                                                        className="h-6 w-6 opacity-0 group-hover:opacity-100 group-aria-selected:opacity-100"
                                                        onClick={(event) => { event.stopPropagation(); setHighlight(index); pick(item) }}>
                                                        <Plus className="h-3.5 w-3.5" />
                                                    </Button>
                                                </td>
                                            </tr>
                                        )
                                    })}
                                    {!loading && items.length === 0 && (
                                        <tr><td colSpan={columns.length + 2} className="px-3 py-10 text-center text-sm text-muted-foreground">
                                            {query ? `Ничего не найдено по «${query}»${groupName ? ` в группе «${groupName}»` : ""}` : groupName ? "В группе нет позиций" : "Справочник пуст"}
                                        </td></tr>
                                    )}
                                </tbody>
                            </table>
                            <ScrollSentinel onIntersect={loadMore} loading={loadingMore} enabled={!!nextCursor} scrollContainer={listRef} rootMargin="0px 0px 400px 0px" />
                        </div>
                    </div>

                    {/* ── Cart ────────────────────────────────────────────── */}
                    <div className="flex min-h-0 flex-col bg-muted/20">
                        <div className="flex items-baseline justify-between border-b px-4 py-2.5">
                            <span className="text-sm font-semibold">Подобрано</span>
                            <span className="text-xs tabular-nums text-muted-foreground">
                                {cart.length ? `${cart.length} поз.${hasQuantity ? ` · ${formatSummaryNumber(total)}` : ""}` : ""}
                            </span>
                        </div>
                        <div ref={cartRef} className="min-h-0 flex-1 overflow-auto">
                            {cart.length === 0 ? (
                                <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center text-xs text-muted-foreground">
                                    <PackagePlus className="h-6 w-6 opacity-40" />
                                    <p>Выберите позиции слева: двойной клик или <Kbd>Enter</Kbd>.</p>
                                    {hasQuantity && <p><Kbd>3*</Kbd> перед поиском добавит сразу три, <Kbd>Shift</Kbd>+<Kbd>Enter</Kbd> — указать количество.</p>}
                                    {hasTree && <p><Kbd>Shift</Kbd>+<Kbd>Tab</Kbd> из поиска — к группам, <Kbd>Enter</Kbd> в группах — обратно.</p>}
                                </div>
                            ) : (
                                <ul className="divide-y divide-border/60">
                                    {cart.map((line) => (
                                        <li key={line.id} className="flex items-center gap-2 px-3 py-1.5 animate-in fade-in-0 slide-in-from-top-1 duration-200 motion-reduce:animate-none">
                                            <div className="min-w-0 flex-1">
                                                <div className="truncate text-sm">{line.name}</div>
                                                {line.code && <div className="truncate font-mono text-[11px] text-muted-foreground">{line.code}</div>}
                                            </div>
                                            {hasQuantity && (
                                                <div className="flex items-center rounded-md border bg-background">
                                                    <Button type="button" size="icon" variant="ghost" className="h-7 w-6 rounded-r-none" tabIndex={-1}
                                                        aria-label="Меньше" onClick={() => stepCart(line, -1)}><Minus className="h-3 w-3" /></Button>
                                                    <input key={line.quantity} data-cart-id={line.id} defaultValue={formatSummaryNumber(line.quantity)}
                                                        inputMode={integer ? "numeric" : "decimal"} aria-label={`Количество «${line.name}»`}
                                                        className="h-7 w-14 border-x bg-transparent text-center text-sm tabular-nums outline-none focus:bg-accent"
                                                        onBlur={(event) => onCartQuantity(line.id, event.target.value.replace(/\s/g, ""))}
                                                        onKeyDown={(event) => {
                                                            if (isImeComposing(event)) return
                                                            if (event.key === "Enter" || event.key === "Escape") {
                                                                event.preventDefault()
                                                                event.stopPropagation()
                                                                if (event.key === "Enter") onCartQuantity(line.id, event.currentTarget.value.replace(/\s/g, ""))
                                                                searchRef.current?.focus()
                                                            }
                                                        }} />
                                                    <Button type="button" size="icon" variant="ghost" className="h-7 w-6 rounded-l-none" tabIndex={-1}
                                                        aria-label="Больше" onClick={() => stepCart(line, 1)}><Plus className="h-3 w-3" /></Button>
                                                </div>
                                            )}
                                            <Button type="button" size="icon" variant="ghost" className="h-7 w-7 text-muted-foreground hover:text-destructive" tabIndex={-1}
                                                aria-label={`Убрать «${line.name}»`} onClick={() => setCart((previous) => previous.filter((item) => item.id !== line.id))}>
                                                <X className="h-3.5 w-3.5" />
                                            </Button>
                                        </li>
                                    ))}
                                </ul>
                            )}
                        </div>
                    </div>
                </div>

                {/* ── Footer ──────────────────────────────────────────────── */}
                <div className="flex flex-wrap items-center gap-3 border-t bg-background/85 px-5 py-3 backdrop-blur-md">
                    {confirmClose ? (
                        <div role="alert" className="flex flex-1 flex-wrap items-center gap-2 text-sm animate-in fade-in-0 duration-150 motion-reduce:animate-none">
                            <span className="font-medium">Подобранное ещё не перенесено в документ.</span>
                            <div className="ml-auto flex gap-2">
                                <Button type="button" variant="ghost" size="sm" onClick={close}>Закрыть без переноса</Button>
                                <Button type="button" size="sm" onClick={transfer}>Перенести в документ</Button>
                            </div>
                        </div>
                    ) : (
                        <>
                            {hasQuantity && (
                                <label className="flex cursor-pointer items-center gap-2 text-xs text-muted-foreground">
                                    <Switch checked={merge} onCheckedChange={(value) => {
                                        setMerge(value)
                                        try { localStorage.setItem(MERGE_KEY, String(value)) } catch { /* per-viewer convenience only */ }
                                    }} />
                                    Добавлять к строкам с теми же позициями
                                </label>
                            )}
                            <div className="ml-auto flex items-center gap-2">
                                <Button type="button" variant="ghost" size="sm" onClick={close}>Отмена</Button>
                                <Button type="button" size="sm" disabled={!cart.length} onClick={transfer} className="gap-2">
                                    Перенести в документ
                                    <span className="text-[10px] opacity-70">Ctrl+Enter</span>
                                </Button>
                            </div>
                        </>
                    )}
                </div>
            </DialogContent>
        </Dialog>
    )
}
