"use client"

/**
 * Delete Marked Objects — admin tool page.
 *
 * Analogous to 1C's "Удаление помеченных объектов".
 * Lists all entities with deletion mark, checks for incoming references,
 * and allows safe physical deletion of unreferenced objects.
 */

import { useState, useCallback, useEffect } from "react"
import { useRouter } from "next/navigation"
import {
    Trash2,
    Loader2,
    ArrowLeft,
    RefreshCw,
    AlertTriangle,
    CheckCircle2,
    Search,
    ShieldAlert,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
} from "@/components/ui/tooltip"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { useTabsStore } from "@/stores/useTabsStore"
import { resolveTitleFromUrl } from "@/lib/tab-utils"
import { api } from "@/lib/api"
import type { MarkedObject } from "@/types/common"

export default function MarkedObjectsPage() {
    const router = useRouter()
    const openTab = useTabsStore((s) => s.openTab)
    const getEntityByName = useMetadataStore((s) => s.getEntityByName)
    const getLabel = useMetadataStore((s) => s.getLabel)

    // ── State ──────────────────────────────────────────────────────────
    const [items, setItems] = useState<MarkedObject[]>([])
    const [selected, setSelected] = useState<Set<string>>(new Set())
    const [loading, setLoading] = useState(false)
    const [deleting, setDeleting] = useState(false)
    const [loaded, setLoaded] = useState(false)
    const [deleteResult, setDeleteResult] = useState<{
        deleted: number
        skipped: number
        errors: number
    } | null>(null)

    // ── Load marked objects ──────────────────────────────────────────
    const loadItems = useCallback(async () => {
        setLoading(true)
        setDeleteResult(null)
        try {
            const data = await api.system.markedObjects.list()
            setItems(data.items)
            setSelected(new Set())
            setLoaded(true)
        } catch {
            setItems([])
        } finally {
            setLoading(false)
        }
    }, [])

    useEffect(() => {
        loadItems()
    }, [loadItems])

    // ── Selection ────────────────────────────────────────────────────
    const deletableItems = items.filter((i) => i.canDelete)
    const allDeletableSelected =
        deletableItems.length > 0 &&
        deletableItems.every((i) => selected.has(`${i.entityName}:${i.entityId}`))

    const toggleItem = useCallback((entityName: string, entityId: string) => {
        setSelected((prev) => {
            const key = `${entityName}:${entityId}`
            const next = new Set(prev)
            if (next.has(key)) {
                next.delete(key)
            } else {
                next.add(key)
            }
            return next
        })
    }, [])

    const toggleAll = useCallback(() => {
        if (allDeletableSelected) {
            setSelected(new Set())
        } else {
            setSelected(new Set(deletableItems.map((i) => `${i.entityName}:${i.entityId}`)))
        }
    }, [allDeletableSelected, deletableItems])

    // ── Delete ───────────────────────────────────────────────────────
    const handleDelete = useCallback(async () => {
        if (selected.size === 0) return

        const itemsToDelete = Array.from(selected).map((key) => {
            const [entityName, entityId] = key.split(":")
            return { entityName, entityId }
        })

        setDeleting(true)
        try {
            const result = await api.system.markedObjects.delete(itemsToDelete)
            setDeleteResult(result)
            // Reload list after deletion
            await loadItems()
        } catch {
            // Error handled by api layer
        } finally {
            setDeleting(false)
        }
    }, [selected, loadItems])

    // ── Navigate ─────────────────────────────────────────────────────
    const navigateToEntity = useCallback(
        (entityName: string, entityId: string) => {
            const meta = getEntityByName(entityName)
            if (!meta) return
            const prefix = meta.routePrefix ?? meta.key
            const section = meta.type === "catalog" ? "catalogs" : meta.key.replace(/_/g, "-")
            const url = `/${section}/${prefix}/${entityId}`
            openTab({ id: url, title: resolveTitleFromUrl(url), url })
            router.push(url)
        },
        [getEntityByName, openTab, router],
    )

    const navigateToFindRefs = useCallback(
        (entityName: string, entityId: string) => {
            const url = `/admin/find-references?entityName=${entityName}&entityId=${entityId}`
            openTab({ id: url, title: "Найти ссылки", url })
            router.push(url)
        },
        [openTab, router],
    )

    // ── Render ────────────────────────────────────────────────────────
    return (
        <ScrollArea className="flex-1">
        <div className="space-y-6 p-6">
            {/* Header */}
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                    <Button variant="ghost" size="icon" onClick={() => router.back()}>
                        <ArrowLeft className="h-4 w-4" />
                    </Button>
                    <div>
                        <h1 className="text-2xl font-semibold tracking-tight">
                            Удаление помеченных объектов
                        </h1>
                        <p className="text-sm text-muted-foreground mt-1">
                            Просмотр и безопасное удаление объектов, помеченных на удаление
                        </p>
                    </div>
                </div>
                <Button variant="outline" onClick={loadItems} disabled={loading}>
                    <RefreshCw className={`h-4 w-4 mr-2 ${loading ? "animate-spin" : ""}`} />
                    Обновить
                </Button>
            </div>

            {/* Delete result banner */}
            {deleteResult && (
                <Card className="border-green-500/30 bg-green-500/5">
                    <CardContent className="pt-4 pb-4">
                        <div className="flex items-center gap-3">
                            <CheckCircle2 className="h-5 w-5 text-green-600 shrink-0" />
                            <div className="text-sm">
                                <span className="font-medium">
                                    Удалено: {deleteResult.deleted}
                                </span>
                                {deleteResult.skipped > 0 && (
                                    <span className="text-muted-foreground ml-3">
                                        Пропущено (есть ссылки): {deleteResult.skipped}
                                    </span>
                                )}
                                {deleteResult.errors > 0 && (
                                    <span className="text-destructive ml-3">
                                        Ошибки: {deleteResult.errors}
                                    </span>
                                )}
                            </div>
                        </div>
                    </CardContent>
                </Card>
            )}

            {/* Main content */}
            <Card>
                <CardHeader className="pb-3">
                    <div className="flex items-center justify-between">
                        <div>
                            <CardTitle className="text-base flex items-center gap-2">
                                Помеченные объекты
                                {loaded && (
                                    <Badge variant="secondary" className="font-mono">
                                        {items.length}
                                    </Badge>
                                )}
                            </CardTitle>
                            <CardDescription>
                                Объекты с меткой удаления. Красные строки имеют входящие ссылки.
                            </CardDescription>
                        </div>
                        {selected.size > 0 && (
                            <Button
                                variant="destructive"
                                onClick={handleDelete}
                                disabled={deleting}
                            >
                                {deleting ? (
                                    <Loader2 className="h-4 w-4 animate-spin mr-2" />
                                ) : (
                                    <Trash2 className="h-4 w-4 mr-2" />
                                )}
                                Удалить выбранные ({selected.size})
                            </Button>
                        )}
                    </div>
                </CardHeader>
                <CardContent>
                    {loading && !loaded ? (
                        <div className="flex items-center justify-center py-12 text-muted-foreground">
                            <Loader2 className="h-5 w-5 animate-spin mr-2" />
                            Сканирование объектов…
                        </div>
                    ) : items.length === 0 ? (
                        <div className="text-center py-12 text-muted-foreground">
                            <CheckCircle2 className="h-8 w-8 mx-auto mb-3 text-green-500/60" />
                            <p className="text-sm font-medium">
                                Нет помеченных на удаление объектов
                            </p>
                            <p className="text-xs mt-1">
                                Все объекты системы в нормальном состоянии
                            </p>
                        </div>
                    ) : (
                        <TooltipProvider>
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead className="w-[40px]">
                                            <Checkbox
                                                checked={allDeletableSelected && deletableItems.length > 0}
                                                onCheckedChange={toggleAll}
                                                aria-label="Выбрать все"
                                            />
                                        </TableHead>
                                        <TableHead>Объект</TableHead>
                                        <TableHead>Тип</TableHead>
                                        <TableHead className="text-center">Ссылки</TableHead>
                                        <TableHead className="text-center">Статус</TableHead>
                                        <TableHead className="w-[80px]" />
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {items.map((item) => {
                                        const key = `${item.entityName}:${item.entityId}`
                                        const isSelected = selected.has(key)
                                        const entityLabel = getLabel(
                                            getEntityByName(item.entityName)?.key ?? item.entityName,
                                            "singular",
                                        )

                                        return (
                                            <TableRow
                                                key={key}
                                                className={
                                                    item.canDelete
                                                        ? "hover:bg-primary/5"
                                                        : "bg-destructive/5 hover:bg-destructive/10"
                                                }
                                            >
                                                <TableCell>
                                                    <Checkbox
                                                        checked={isSelected}
                                                        disabled={!item.canDelete}
                                                        onCheckedChange={() =>
                                                            toggleItem(item.entityName, item.entityId)
                                                        }
                                                    />
                                                </TableCell>
                                                <TableCell
                                                    className="font-medium text-sm cursor-pointer"
                                                    onClick={() => navigateToEntity(item.entityName, item.entityId)}
                                                >
                                                    {item.presentation || item.entityId.slice(0, 8) + "…"}
                                                </TableCell>
                                                <TableCell>
                                                    <Badge
                                                        variant={item.entityType === "document" ? "default" : "outline"}
                                                        className="text-xs"
                                                    >
                                                        {entityLabel}
                                                    </Badge>
                                                </TableCell>
                                                <TableCell className="text-center">
                                                    {item.refCount > 0 ? (
                                                        <Tooltip>
                                                            <TooltipTrigger asChild>
                                                                <Badge variant="destructive" className="font-mono text-xs cursor-help">
                                                                    {item.refCount}
                                                                </Badge>
                                                            </TooltipTrigger>
                                                            <TooltipContent>
                                                                Найдено {item.refCount} ссылок. Удаление невозможно.
                                                            </TooltipContent>
                                                        </Tooltip>
                                                    ) : (
                                                        <span className="text-xs text-muted-foreground">0</span>
                                                    )}
                                                </TableCell>
                                                <TableCell className="text-center">
                                                    {item.canDelete ? (
                                                        <Tooltip>
                                                            <TooltipTrigger>
                                                                <CheckCircle2 className="h-4 w-4 text-green-500 inline-block" />
                                                            </TooltipTrigger>
                                                            <TooltipContent>Можно удалить</TooltipContent>
                                                        </Tooltip>
                                                    ) : (
                                                        <Tooltip>
                                                            <TooltipTrigger>
                                                                <ShieldAlert className="h-4 w-4 text-destructive inline-block" />
                                                            </TooltipTrigger>
                                                            <TooltipContent>
                                                                Есть ссылки. Сначала удалите зависимости.
                                                            </TooltipContent>
                                                        </Tooltip>
                                                    )}
                                                </TableCell>
                                                <TableCell>
                                                    {item.refCount > 0 && (
                                                        <Tooltip>
                                                            <TooltipTrigger asChild>
                                                                <Button
                                                                    variant="ghost"
                                                                    size="icon"
                                                                    className="h-7 w-7"
                                                                    onClick={() =>
                                                                        navigateToFindRefs(item.entityName, item.entityId)
                                                                    }
                                                                >
                                                                    <Search className="h-3.5 w-3.5" />
                                                                </Button>
                                                            </TooltipTrigger>
                                                            <TooltipContent>
                                                                Найти ссылки
                                                            </TooltipContent>
                                                        </Tooltip>
                                                    )}
                                                </TableCell>
                                            </TableRow>
                                        )
                                    })}
                                </TableBody>
                            </Table>
                        </TooltipProvider>
                    )}
                </CardContent>
            </Card>
        </div>
        </ScrollArea>
    )
}
