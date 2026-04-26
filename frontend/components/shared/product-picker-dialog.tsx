"use client"

/**
 * ProductPickerDialog — specialized nomenclature picker for goods receipt/issue documents.
 *
 * Based on the lsFusion-inspired prototype, adapted to the Metapus design system.
 *
 * Key UX features (matching prototype):
 *   - "Заказ" / "Подбор" tabs — view current order without closing dialog
 *   - Persistent summary bar: Позиций | Кол-во (always visible)
 *   - Category tree sidebar (from nomenclature folders)
 *   - Quick filters: "С заказом" (show only ordered items)
 *   - "Заказать" column with inline +/- quantity entry
 *   - Keyboard-first: ArrowUp/Down, Enter = +1, +/- = qty change
 *   - Visual feedback: rows with qty > 0 highlighted green
 *
 * UX patterns:
 *   - 1С: tree of categories on the left, Enter to add
 *   - SAP Fiori: always-visible summary counters
 *   - lsFusion: Заказ/Подбор tabs without closing modal
 *
 * Pattern #6: Composition — reuses usePickerDialog + CategoryTree + ScrollSentinel.
 * Pattern #4: Shared Components — reuses document-form types for integration.
 */

import { useState, useCallback, useMemo } from "react"
import {
    Search,
    Check,
    ChevronUp,
    ChevronDown,
    Package,
    X,
    Loader2,
    ShoppingCart,
    BarChart3,
} from "lucide-react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
    DialogFooter,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Separator } from "@/components/ui/separator"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { cn } from "@/lib/utils"
import { useCompactMode } from "@/hooks/useCompactMode"
import { CategoryTree } from "@/components/shared/category-tree"
import { ScrollSentinel } from "@/components/shared/scroll-sentinel"
import { usePickerDialog } from "@/hooks/usePickerDialog"
import { useColumnResize, type ColumnResizeDef } from "@/hooks/useColumnResize"
import type { PickerInitialItem } from "@/hooks/usePickerDialog"
import type { ProductPickerDialogProps, PickedItem } from "@/types/picker"

type PickerTab = "selection" | "order"

// ── Column definitions ──────────────────────────────────────────────────

interface PickerColumnDef {
    key: string
    label: string
    width: number
    minWidth?: number
    sortable?: boolean
    align?: "left" | "right" | "center"
    className?: string
}

const PICKER_COLUMNS: PickerColumnDef[] = [
    { key: "code", label: "Код", width: 70, minWidth: 50, sortable: true },
    { key: "name", label: "Наименование", width: 280, minWidth: 100, sortable: true },
    { key: "unit", label: "Ед.", width: 40, minWidth: 30, align: "center" },
    { key: "article", label: "Артикул", width: 100, minWidth: 50 },
    { key: "balance", label: "Остаток", width: 80, minWidth: 50, align: "right" },
    { key: "quantity", label: "Заказать", width: 100, minWidth: 80, align: "center", className: "bg-primary/5" },
]

const PICKER_RESIZE_DEFS: ColumnResizeDef[] = PICKER_COLUMNS.map((col) => ({
    key: col.key,
    width: col.width,
    minWidth: col.minWidth ?? 40,
}))

const COL_WIDTHS_STORAGE_KEY = "metapus-ppicker-colwidths"

function getStoredPickerWidths(): Record<string, number> | undefined {
    if (typeof window === "undefined") return undefined
    try {
        const raw = localStorage.getItem(COL_WIDTHS_STORAGE_KEY)
        return raw ? JSON.parse(raw) as Record<string, number> : undefined
    } catch { return undefined }
}

function savePickerWidths(widths: Record<string, number>): void {
    if (typeof window === "undefined") return
    try { localStorage.setItem(COL_WIDTHS_STORAGE_KEY, JSON.stringify(widths)) } catch { /* ignore */ }
}

// ── Component ───────────────────────────────────────────────────────────

export function ProductPickerDialog({
    open,
    onOpenChange,
    onPick,
    existingLines,
    warehouseId,
}: ProductPickerDialogProps) {
    // ── Reset key — increments each time the dialog closes, so all inner state resets on next open ──
    const [resetKey, setResetKey] = useState(0)

    const handleOpenChange = useCallback((value: boolean) => {
        if (!value) {
            // Dialog is closing — bump key for next open
            setResetKey((k) => k + 1)
        }
        onOpenChange(value)
    }, [onOpenChange])

    if (!open) return null

    return (
        <ProductPickerDialogInner
            key={resetKey}
            open={open}
            onOpenChange={handleOpenChange}
            onPick={onPick}
            existingLines={existingLines}
            warehouseId={warehouseId}
        />
    )
}

function ProductPickerDialogInner({
    open,
    onOpenChange,
    onPick,
    existingLines,
    warehouseId,
}: ProductPickerDialogProps) {
    // ── Tabs ────────────────────────────────────────────────────────────
    const [activeTab, setActiveTab] = useState<PickerTab>("selection")

    // ── Category filter ─────────────────────────────────────────────────
    const [selectedCategory, setSelectedCategory] = useState("all")

    // ── Quick filters ───────────────────────────────────────────────────
    const [showOnlyWithOrder, setShowOnlyWithOrder] = useState(false)

    // ── Extra params based on category ──────────────────────────────────
    const extraParams = useMemo(() => {
        if (selectedCategory === "all") return undefined
        return { parentId: selectedCategory }
    }, [selectedCategory])

    // ── Convert existingLines to initialData for the hook ────────────────
    const initialData = useMemo((): PickerInitialItem[] | undefined => {
        if (!existingLines || existingLines.length === 0) return undefined
        return existingLines
            .filter((l) => l.productId && l.quantity > 0)
            .map((l) => ({
                id: l.productId,
                name: l.productName,
                code: l.productCode,
                unitId: l.unitId,
                unitName: l.unitName,
                quantity: l.quantity,
            }))
    }, [existingLines])

    // ── Data via shared hook ────────────────────────────────────────────
    const {
        items: pickerItems,
        loading: pickerLoading,
        loadingMore: pickerLoadingMore,
        totalCount: pickerTotalCount,
        hasMore: pickerHasMore,
        search: pickerSearch,
        setSearch: pickerSetSearch,
        sortField: pickerSortField,
        sortDir: pickerSortDir,
        handleSort: pickerHandleSort,
        focusedId: pickerFocusedId,
        setFocusedId: pickerSetFocusedId,
        quantities: pickerQuantities,
        pickedItems: pickerPickedItems,
        setQuantity: pickerSetQuantity,
        pickedCount: pickerPickedCount,
        balanceMap: pickerBalanceMap,
        fetchMore: pickerFetchMore,
        handleKeyDown: pickerHandleKeyDown,
        scrollContainerRef: pickerScrollContainerRef,
        tableContainerRef: pickerTableContainerRef,
    } = usePickerDialog({
        apiEndpoint: "/catalog/nomenclature",
        open,
        extraParams,
        initialData,
        warehouseId,
    })

    // ── Column resize (persisted to localStorage) ────────────────────────
    const storedWidths = useMemo(() => getStoredPickerWidths(), [])
    const handleWidthsChange = useCallback(
        (widths: Record<string, number>) => savePickerWidths(widths),
        [],
    )
    const { colWidths, onResizeStart, isResizing } = useColumnResize({
        columns: PICKER_RESIZE_DEFS,
        storedWidths,
        onWidthsChange: handleWidthsChange,
    })

    // ── Filtered items (for "С заказом" filter) ──────────────────────────
    const displayItems = useMemo(() => {
        if (!showOnlyWithOrder) return pickerItems
        return pickerItems.filter((item) => (pickerQuantities.get(item.id) || 0) > 0)
    }, [pickerItems, showOnlyWithOrder, pickerQuantities])

    // ── Order lines (for "Заказ" tab) ───────────────────────────────────
    const orderLines = useMemo(() => {
        const lines: Array<{ id: string; name: string; code: string; unitId: string; quantity: number }> = []
        for (const [id, qty] of pickerQuantities.entries()) {
            if (qty <= 0) continue
            const item = pickerPickedItems.get(id)
            if (item) {
                lines.push({
                    id: item.id,
                    name: String(item.name ?? ""),
                    code: String(item.code ?? ""),
                    unitId: item.baseUnitId != null ? String(item.baseUnitId) : "",
                    quantity: qty,
                })
            }
        }
        return lines
    }, [pickerQuantities, pickerPickedItems])

    // ── Keyboard: Product-specific extensions ───────────────────────────
    const handleKeyDown = useCallback(
        (e: React.KeyboardEvent) => {
            // Don't intercept when input is focused
            const tag = (e.target as HTMLElement).tagName
            if (tag === "INPUT" || tag === "TEXTAREA") return

            const currentIndex = pickerFocusedId
                ? displayItems.findIndex((i) => i.id === pickerFocusedId)
                : -1
            const currentItem = currentIndex >= 0 ? displayItems[currentIndex] : null

            switch (e.key) {
                case "Enter": {
                    e.preventDefault()
                    if (currentItem) {
                        const current = pickerQuantities.get(currentItem.id) || 0
                        pickerSetQuantity(currentItem.id, current + 1)
                    }
                    break
                }
                case "+":
                case "=": {
                    e.preventDefault()
                    if (currentItem) {
                        const current = pickerQuantities.get(currentItem.id) || 0
                        pickerSetQuantity(currentItem.id, current + 1)
                    }
                    break
                }
                case "-": {
                    e.preventDefault()
                    if (currentItem) {
                        const current = pickerQuantities.get(currentItem.id) || 0
                        pickerSetQuantity(currentItem.id, Math.max(0, current - 1))
                    }
                    break
                }
                default:
                    pickerHandleKeyDown(e)
            }
        },
        [pickerFocusedId, pickerQuantities, pickerSetQuantity, pickerHandleKeyDown, displayItems],
    )

    // ── Confirm ─────────────────────────────────────────────────────────
    const handleConfirm = useCallback(() => {
        const result: PickedItem[] = []
        for (const [id, qty] of pickerQuantities.entries()) {
            if (qty <= 0) continue
            const item = pickerPickedItems.get(id)
            if (item) {
                result.push({
                    id: item.id,
                    name: String(item.name ?? ""),
                    code: item.code != null ? String(item.code) : undefined,
                    unitId: item.baseUnitId != null ? String(item.baseUnitId) : undefined,
                    quantity: qty,
                })
            }
        }
        onPick(result)
        onOpenChange(false)
    }, [pickerQuantities, pickerPickedItems, onPick, onOpenChange])

    // ── Computed totals ─────────────────────────────────────────────────
    const totalQty = useMemo(() => {
        let sum = 0
        for (const qty of pickerQuantities.values()) sum += qty
        return sum
    }, [pickerQuantities])

    const showInitialLoading = pickerLoading && pickerItems.length === 0

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent
                className="max-w-6xl w-[95vw] h-[85vh] flex flex-col gap-0 p-0"
                onOpenAutoFocus={(e) => e.preventDefault()}
            >
                {/* Header: title + tabs + summary */}
                <DialogHeader className="px-4 pt-3 pb-0 flex-none">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-3">
                            <DialogTitle className="text-sm">Подбор номенклатуры</DialogTitle>
                            <DialogDescription className="sr-only">
                                Выберите товары для добавления в документ
                            </DialogDescription>
                        </div>

                        <div className="flex items-center gap-3 mr-8">
                            {/* Заказ / Подбор tabs — lsFusion pattern */}
                            <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as PickerTab)}>
                                <TabsList className="h-7 p-0.5">
                                    <TabsTrigger value="order" className="h-6 px-3 text-xs">
                                        <ShoppingCart className="h-3 w-3 mr-1.5" />
                                        Заказ
                                        {pickerPickedCount > 0 && (
                                            <Badge variant="secondary" className="ml-1.5 h-4 px-1 text-[10px] min-w-[16px]">
                                                {pickerPickedCount}
                                            </Badge>
                                        )}
                                    </TabsTrigger>
                                    <TabsTrigger value="selection" className="h-6 px-3 text-xs">
                                        <BarChart3 className="h-3 w-3 mr-1.5" />
                                        Подбор
                                    </TabsTrigger>
                                </TabsList>
                            </Tabs>

                            <Separator orientation="vertical" className="h-5" />

                            {/* Always-visible summary — SAP Fiori pattern */}
                            <div className="flex items-center gap-3 text-xs">
                                <span className="text-muted-foreground">
                                    Позиций: <span className="font-medium text-foreground">{pickerPickedCount}</span>
                                </span>
                                <span className="text-muted-foreground">
                                    Кол-во: <span className="font-semibold text-foreground">{totalQty}</span>
                                </span>
                            </div>
                        </div>
                    </div>
                </DialogHeader>

                {/* Main content */}
                <div className="flex-1 flex overflow-hidden min-h-0 mt-2">
                    {activeTab === "order" ? (
                        /* ── ORDER TAB ─────────────────────────────────────── */
                        <OrderTab
                            lines={orderLines}
                            totalQty={totalQty}
                            onQuantityChange={(id, qty) => pickerSetQuantity(id, qty)}
                            onRemove={(id) => pickerSetQuantity(id, 0)}
                        />
                    ) : (
                        /* ── SELECTION TAB ──────────────────────────────────── */
                        <>
                            {/* Category tree sidebar */}
                            <CategoryTree
                                selectedId={selectedCategory}
                                onSelect={setSelectedCategory}
                                className="w-[200px] flex-none border-r"
                            />

                            {/* Right panel: search + table */}
                            <div className="flex-1 flex flex-col overflow-hidden">
                                {/* Search bar + filters */}
                                <div className="flex-none border-b bg-card px-2 py-1.5 flex items-center gap-3">
                                    <div className="relative flex-1 max-w-md">
                                        <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
                                        <Input
                                            placeholder="Поиск по названию, коду, штрихкоду…"
                                            value={pickerSearch}
                                            onChange={(e) => pickerSetSearch(e.target.value)}
                                            className="h-7 pl-7 text-xs"
                                        />
                                        {pickerSearch && (
                                            <Button
                                                variant="ghost"
                                                size="icon"
                                                className="absolute right-1 top-1/2 -translate-y-1/2 h-5 w-5"
                                                onClick={() => pickerSetSearch("")}
                                            >
                                                <X className="h-3 w-3" />
                                            </Button>
                                        )}
                                    </div>

                                    <Separator orientation="vertical" className="h-5" />

                                    {/* Quick filter: "С заказом" */}
                                    <label className="flex items-center gap-1.5 text-xs cursor-pointer select-none">
                                        <Checkbox
                                            checked={showOnlyWithOrder}
                                            onCheckedChange={(checked) => setShowOnlyWithOrder(!!checked)}
                                            className="h-3.5 w-3.5"
                                        />
                                        <span>С заказом</span>
                                    </label>

                                    <div className="flex-1" />

                                    <span className="text-xs text-muted-foreground">
                                        Найдено: {pickerTotalCount}
                                    </span>
                                </div>

                                {/* Products table */}
                                <div
                                    ref={pickerTableContainerRef}
                                    className="flex-1 overflow-hidden"
                                    onKeyDown={handleKeyDown}
                                    tabIndex={0}
                                >
                                    <div
                                        ref={pickerScrollContainerRef}
                                        className="h-full overflow-auto"
                                    >
                                <table className={cn("w-full text-xs border-collapse table-fixed", isResizing && "select-none")}>
                                            <colgroup>
                                                <col style={{ width: 28 }} />
                                                {PICKER_COLUMNS.map((col, i) => (
                                                    <col key={col.key} style={{ width: colWidths[i] ?? col.width }} />
                                                ))}
                                            </colgroup>
                                            <thead className="bg-muted/50 sticky top-0 z-10">
                                                <tr>
                                                    <th className="text-left font-medium px-2 py-1.5 w-[28px]" />
                                                    {PICKER_COLUMNS.map((col, colIndex) => (
                                                        <th
                                                            key={col.key}
                                                            className={cn(
                                                                "relative text-left font-medium px-2 py-1.5 select-none transition-colors",
                                                                col.sortable && "cursor-pointer hover:text-foreground",
                                                                col.align === "right" && "text-right",
                                                                col.align === "center" && "text-center",
                                                                col.className,
                                                            )}
                                                            onClick={col.sortable ? () => { if (!isResizing) pickerHandleSort(col.key) } : undefined}
                                                        >
                                                            <div className="truncate">
                                                                {col.label}
                                                                {col.sortable && (
                                                                    <SortIndicator field={col.key} sortField={pickerSortField} sortDir={pickerSortDir} />
                                                                )}
                                                            </div>
                                                            {/* Resize handle */}
                                                            <div
                                                                className="absolute right-0 top-0 h-full w-[5px] cursor-col-resize z-20 group/resize hover:bg-primary/30 active:bg-primary/50"
                                                                onMouseDown={(e) => onResizeStart(colIndex, e)}
                                                                onClick={(e) => e.stopPropagation()}
                                                            >
                                                                <div className="absolute right-0 top-1/4 h-1/2 w-[1px] bg-border group-hover/resize:bg-primary/60" />
                                                            </div>
                                                        </th>
                                                    ))}
                                                </tr>
                                            </thead>
                                            <tbody>
                                                {showInitialLoading ? (
                                                    <tr>
                                                        <td colSpan={8} className="py-12 text-center">
                                                            <Loader2 className="inline-block h-5 w-5 animate-spin text-muted-foreground" />
                                                        </td>
                                                    </tr>
                                                ) : displayItems.length === 0 ? (
                                                    <tr>
                                                        <td colSpan={8} className="text-center py-12 text-muted-foreground">
                                                            <Package className="h-8 w-8 mx-auto mb-2 opacity-30" />
                                                            <p className="text-sm">Ничего не найдено</p>
                                                            <p className="text-xs mt-0.5">
                                                                {showOnlyWithOrder
                                                                    ? "Нет товаров с заданным количеством"
                                                                    : "Измените поиск или выберите другую категорию"}
                                                            </p>
                                                        </td>
                                                    </tr>
                                                ) : (
                                                    displayItems.map((item) => (
                                                        <ProductRow
                                                            key={item.id}
                                                            item={item}
                                                            isFocused={pickerFocusedId === item.id}
                                                            quantity={pickerQuantities.get(item.id) || 0}
                                                            balance={pickerBalanceMap.get(item.id)}
                                                            onFocus={() => pickerSetFocusedId(item.id)}
                                                            onQuantityChange={(qty) => pickerSetQuantity(item.id, qty)}
                                                        />
                                                    ))
                                                )}
                                            </tbody>
                                        </table>

                                        <ScrollSentinel
                                            onIntersect={pickerFetchMore}
                                            loading={pickerLoadingMore}
                                            enabled={pickerHasMore && !pickerLoading}
                                            scrollContainer={pickerScrollContainerRef}
                                            rootMargin="100px"
                                        />
                                    </div>
                                </div>

                                {/* Keyboard hints */}
                                <div className="flex-none border-t bg-muted/50 px-3 py-1">
                                    <div className="flex items-center gap-4 text-[10px] text-muted-foreground">
                                        <span>
                                            <kbd className="px-1 py-0.5 bg-muted rounded text-[9px]">Enter</kbd> / <kbd className="px-1 py-0.5 bg-muted rounded text-[9px]">+</kbd> Добавить
                                        </span>
                                        <span>
                                            <kbd className="px-1 py-0.5 bg-muted rounded text-[9px]">-</kbd> Убрать
                                        </span>
                                        <span>
                                            <kbd className="px-1 py-0.5 bg-muted rounded text-[9px]">↑</kbd><kbd className="px-1 py-0.5 bg-muted rounded text-[9px]">↓</kbd> Перемещение
                                        </span>
                                    </div>
                                </div>
                            </div>
                        </>
                    )}
                </div>

                {/* Footer */}
                <DialogFooter className="px-4 py-2 border-t flex-row items-center justify-between sm:justify-between">
                    <div className="flex items-center gap-3 text-xs">
                        {activeTab === "selection" && (
                            <span className="text-muted-foreground">
                                Показано: <span className="font-medium text-foreground">{displayItems.length}</span> из {pickerTotalCount}
                            </span>
                        )}
                    </div>
                    <div className="flex gap-2">
                        <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onOpenChange(false)}
                        >
                            Отменить
                        </Button>
                        <Button
                            size="sm"
                            disabled={pickerPickedCount === 0}
                            onClick={handleConfirm}
                        >
                            Добавить ({pickerPickedCount})
                        </Button>
                    </div>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}

// ── Order Tab ────────────────────────────────────────────────────────────
// Shows current order contents — can be viewed without closing the picker.
// lsFusion pattern: "Заказ" tab alongside "Подбор".

function OrderTab({
    lines,
    totalQty,
    onQuantityChange,
    onRemove,
}: {
    lines: Array<{ id: string; name: string; code: string; unitId: string; quantity: number }>
    totalQty: number
    onQuantityChange: (id: string, qty: number) => void
    onRemove: (id: string) => void
}) {
    const compact = useCompactMode()
    return (
        <div className="flex-1 flex flex-col overflow-hidden">
            <div className="flex-1 overflow-auto">
                <table className="w-full text-xs">
                    <thead className="bg-muted/50 sticky top-0">
                        <tr className="border-b">
                            <th className="text-left font-medium px-2 py-1.5 w-[40px]">№</th>
                            <th className="text-left font-medium px-2 py-1.5 w-[110px]">Код</th>
                            <th className="text-left font-medium px-2 py-1.5">Наименование</th>
                            <th className="text-right font-medium px-2 py-1.5 w-[100px]">Кол-во</th>
                            <th className="w-[36px]" />
                        </tr>
                    </thead>
                    <tbody>
                        {lines.length === 0 ? (
                            <tr>
                                <td colSpan={5} className="text-center py-12 text-muted-foreground">
                                    <Package className="h-8 w-8 mx-auto mb-2 opacity-30" />
                                    <p className="text-sm">Нет товаров</p>
                                    <p className="text-xs mt-0.5">
                                        Перейдите на вкладку «Подбор» для добавления
                                    </p>
                                </td>
                            </tr>
                        ) : (
                            lines.map((line, idx) => (
                                    <tr key={line.id} className={cn("border-b hover:bg-muted/30", compact ? "h-7" : "h-8")}>
                                    <td className="px-2 py-0.5 text-muted-foreground">{idx + 1}</td>
                                    <td className="px-2 py-0.5 font-mono text-muted-foreground whitespace-nowrap">{line.code}</td>
                                    <td className="px-2 py-0.5 truncate max-w-[300px]" title={line.name}>{line.name}</td>
                                    <td className="px-2 py-1 text-right">
                                        <Input
                                            type="number"
                                            value={line.quantity}
                                            onChange={(e) => {
                                                const val = parseFloat(e.target.value) || 0
                                                onQuantityChange(line.id, Math.max(0, val))
                                            }}
                                            className={cn(
                                                "w-16 h-6 text-right text-xs ml-auto tabular-nums",
                                                "[appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none",
                                            )}
                                            min={0}
                                        />
                                    </td>
                                    <td className="px-1 py-1">
                                        <Button
                                            variant="ghost"
                                            size="icon"
                                            className="h-6 w-6 text-muted-foreground hover:text-destructive"
                                            onClick={() => onRemove(line.id)}
                                        >
                                            <X className="h-3 w-3" />
                                        </Button>
                                    </td>
                                </tr>
                            ))
                        )}
                    </tbody>
                    {lines.length > 0 && (
                        <tfoot className="bg-muted/50 font-medium">
                            <tr>
                                <td colSpan={3} className="px-2 py-1.5 text-right text-xs">
                                    Итого:
                                </td>
                                <td className="px-2 py-1.5 text-right text-xs font-bold tabular-nums">
                                    {totalQty}
                                </td>
                                <td />
                            </tr>
                        </tfoot>
                    )}
                </table>
            </div>
        </div>
    )
}

// ── Product Row ─────────────────────────────────────────────────────────

const UNIT_LABELS: Record<string, string> = {
    pcs: "шт",
    kg: "кг",
    l: "л",
    m: "м",
    m2: "м²",
    m3: "м³",
    t: "т",
}

function ProductRow({
    item,
    isFocused,
    quantity,
    balance,
    onFocus,
    onQuantityChange,
}: {
    item: Record<string, unknown> & { id: string }
    isFocused: boolean
    quantity: number
    balance: number | undefined
    onFocus: () => void
    onQuantityChange: (qty: number) => void
}) {
    const hasQty = quantity > 0
    const compact = useCompactMode()
    // Show unit short label if available
    const unitName = String(item.baseUnitName ?? item.unitName ?? "")
    const unitShort = UNIT_LABELS[unitName.toLowerCase()] ?? unitName.slice(0, 3) ?? ""

    // Format balance display
    const balanceDisplay = balance === undefined ? "—" : balance === 0 ? "0" : String(balance)
    const balanceColor =
        balance === undefined || balance === 0
            ? "text-muted-foreground"
            : balance > 0
                ? "text-emerald-600 dark:text-emerald-400"
                : "text-destructive"

    return (
        <tr
            data-row-id={item.id}
            onClick={onFocus}
            onDoubleClick={() => onQuantityChange(quantity + 1)}
            className={cn(
                "border-b cursor-pointer transition-colors",
                compact ? "h-7" : "h-8",
                isFocused && "bg-primary/10 outline outline-1 outline-primary/30",
                hasQty && !isFocused && "bg-emerald-50/50 dark:bg-emerald-950/20",
                !isFocused && !hasQty && "hover:bg-muted/30",
            )}
        >
            <td className="px-2 py-0.5 w-[28px]">
                {hasQty && <Check className="h-3 w-3 text-emerald-600" />}
            </td>
            <td className="px-2 py-0.5 font-mono text-muted-foreground overflow-hidden" title={String(item.code ?? "")}>
                <div className="truncate">{String(item.code ?? "")}</div>
            </td>
            <td className="px-2 py-0.5 overflow-hidden" title={String(item.name ?? "")}>
                <div className="truncate">{String(item.name ?? "")}</div>
            </td>
            <td className="px-2 py-0.5 text-center text-muted-foreground overflow-hidden">
                {unitShort}
            </td>
            <td className="px-2 py-0.5 font-mono text-muted-foreground overflow-hidden">
                <div className="truncate">{String(item.article ?? "")}</div>
            </td>
            <td className={cn("px-2 py-0.5 text-right tabular-nums text-xs font-medium overflow-hidden", balanceColor)}>
                {balanceDisplay}
            </td>
            <td className="px-1 py-0.5 bg-primary/5">
                <div className="flex items-center justify-center gap-1">
                    <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5"
                        onClick={(e) => {
                            e.stopPropagation()
                            onQuantityChange(Math.max(0, quantity - 1))
                        }}
                        disabled={quantity === 0}
                    >
                        <ChevronDown className="h-3 w-3" />
                    </Button>
                    <Input
                        type="number"
                        value={quantity || ""}
                        onChange={(e) => {
                            e.stopPropagation()
                            const val = parseFloat(e.target.value) || 0
                            onQuantityChange(Math.max(0, val))
                        }}
                        onClick={(e) => e.stopPropagation()}
                        className={cn(
                            "w-14 h-6 text-center text-xs tabular-nums",
                            "[appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none",
                            hasQty && "bg-emerald-100 dark:bg-emerald-900/50 font-medium",
                        )}
                        placeholder="0"
                    />
                    <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5"
                        onClick={(e) => {
                            e.stopPropagation()
                            onQuantityChange(quantity + 1)
                        }}
                    >
                        <ChevronUp className="h-3 w-3" />
                    </Button>
                </div>
            </td>
        </tr>
    )
}

// ── Sort Indicator ──────────────────────────────────────────────────────

function SortIndicator({
    field,
    sortField,
    sortDir,
}: {
    field: string
    sortField: string
    sortDir: "asc" | "desc"
}) {
    if (sortField !== field) return null
    return sortDir === "asc" ? (
        <span className="ml-1 text-primary">↑</span>
    ) : (
        <span className="ml-1 text-primary">↓</span>
    )
}
