"use client"

/**
 * Delete Marked Objects — admin tool page.
 *
 * Analogous to 1C's "Удаление помеченных объектов". The server builds a
 * deletion plan for the selected marked objects: an object is deleted when
 * everything that references it is deleted in the same run, so a marked
 * chain (contract → counterparty) goes in one pass, referencing objects
 * first. For each object the page shows where it is used and offers to mark
 * or select the referencing objects; nothing is marked implicitly.
 */

import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useRouter } from "next/navigation"
import { toast } from "sonner"
import {
    AlertTriangle,
    ArrowLeft,
    CheckCircle2,
    ChevronRight,
    Loader2,
    RefreshCw,
    Search,
    ShieldAlert,
    Trash2,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { useTabsStore } from "@/stores/useTabsStore"
import { useLatestRequest } from "@/hooks/useLatestRequest"
import { resolveTitleFromUrl } from "@/lib/tab-utils"
import { buildEntityUrlByRoute } from "@/lib/entity-url"
import { api } from "@/lib/api"
import { apiFetch } from "@/lib/api"
import { cn } from "@/lib/utils"
import {
    deletionStatusView,
    markedKey,
    parseMarkedKey,
    plural,
    referenceAction,
    referenceSourceName,
    statusExplanation,
} from "@/lib/marked-objects"
import type {
    DeleteMarkedResult,
    DeletionPlan,
    IncomingReference,
    MarkedObject,
    MarkedObjectRef,
} from "@/types/common"

const REPLAN_DELAY_MS = 250

const toneClass = {
    success: "text-success",
    info: "text-info",
    destructive: "text-destructive",
    muted: "text-muted-foreground",
} as const

export default function MarkedObjectsPage() {
    const router = useRouter()
    const openTab = useTabsStore((s) => s.openTab)
    const getEntityByName = useMetadataStore((s) => s.getEntityByName)
    const getLabel = useMetadataStore((s) => s.getLabel)
    const requests = useLatestRequest()

    const [plan, setPlan] = useState<DeletionPlan | null>(null)
    const [selected, setSelected] = useState<Set<string>>(new Set())
    const [expanded, setExpanded] = useState<Set<string>>(new Set())
    const [loading, setLoading] = useState(true)
    const [planning, setPlanning] = useState(false)
    const [deleting, setDeleting] = useState(false)
    const [confirmOpen, setConfirmOpen] = useState(false)
    const [markingKey, setMarkingKey] = useState<string | null>(null)
    const [result, setResult] = useState<DeleteMarkedResult | null>(null)
    const replanTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

    const entityLabel = useCallback(
        (entityName: string) => {
            const meta = getEntityByName(entityName)
            return meta ? getLabel(meta.key, "singular") : entityName
        },
        [getEntityByName, getLabel],
    )

    // ── Loading ──────────────────────────────────────────────────────
    // Without a selection the server selects every marked object.
    const loadPlan = useCallback(
        async (selection?: MarkedObjectRef[]) => {
            const current = requests.begin()
            setPlanning(true)
            try {
                const next = selection
                    ? await api.system.markedObjects.plan(selection)
                    : await api.system.markedObjects.list()
                if (!current()) return
                setPlan(next)
                setSelected(new Set(next.items.filter((i) => i.status !== "excluded").map(markedKey)))
            } catch (e) {
                if (current()) toast.error(e instanceof Error ? e.message : "Не удалось построить план удаления")
            } finally {
                if (current()) {
                    setPlanning(false)
                    setLoading(false)
                }
            }
        },
        [requests],
    )

    useEffect(() => {
        void loadPlan()
        return () => {
            if (replanTimer.current) clearTimeout(replanTimer.current)
        }
    }, [loadPlan])

    const replan = useCallback(
        (next: Set<string>, immediate = false) => {
            setSelected(next)
            setPlanning(true)
            if (replanTimer.current) clearTimeout(replanTimer.current)
            const run = () => void loadPlan(Array.from(next, parseMarkedKey))
            if (immediate) run()
            else replanTimer.current = setTimeout(run, REPLAN_DELAY_MS)
        },
        [loadPlan],
    )

    const reloadAll = useCallback(() => {
        if (replanTimer.current) clearTimeout(replanTimer.current)
        setResult(null)
        void loadPlan()
    }, [loadPlan])

    // ── Selection ────────────────────────────────────────────────────
    const items = useMemo(() => plan?.items ?? [], [plan])
    const allSelected = items.length > 0 && items.every((i) => selected.has(markedKey(i)))
    const someSelected = !allSelected && items.some((i) => selected.has(markedKey(i)))

    const toggleItem = (item: MarkedObject) => {
        const next = new Set(selected)
        const key = markedKey(item)
        if (next.has(key)) next.delete(key)
        else next.add(key)
        replan(next)
    }

    const toggleAll = () => replan(allSelected ? new Set() : new Set(items.map(markedKey)))

    const toggleExpanded = (key: string) =>
        setExpanded((prev) => {
            const next = new Set(prev)
            if (next.has(key)) next.delete(key)
            else next.add(key)
            return next
        })

    // ── Navigation ───────────────────────────────────────────────────
    const openEntity = useCallback(
        (entityName: string, entityId: string) => {
            const meta = getEntityByName(entityName)
            if (!meta?.routePrefix || (meta.type !== "catalog" && meta.type !== "document")) return
            const url = buildEntityUrlByRoute(meta.routePrefix, meta.type, entityId)
            openTab({ id: url, title: resolveTitleFromUrl(url), url })
            router.push(url)
        },
        [getEntityByName, openTab, router],
    )

    const openFindReferences = useCallback(
        (item: MarkedObject) => {
            const params = new URLSearchParams({ entityName: item.entityName, entityId: item.entityId })
            const url = `/admin/find-references?${params.toString()}`
            openTab({ id: url, title: "Найти ссылки", url })
            router.push(url)
        },
        [openTab, router],
    )

    // ── Referencing objects ──────────────────────────────────────────
    const deletionMarkPath = useCallback(
        (entityName: string): string | null => {
            const meta = getEntityByName(entityName)
            if (!meta?.routePrefix) return null
            if (meta.type === "catalog") return `/catalog/${meta.routePrefix}`
            if (meta.type === "document" && meta.capabilities?.setDeletionMark !== false) return `/document/${meta.routePrefix}`
            return null
        },
        [getEntityByName],
    )

    const setMark = useCallback(
        async (ref: IncomingReference, marked: boolean) => {
            const path = ref.sourceEntityName ? deletionMarkPath(ref.sourceEntityName) : null
            if (!path || !ref.sourceId || !ref.sourceEntityName) return
            await apiFetch<void>(`${path}/${ref.sourceId}/deletion-mark`, {
                method: "POST",
                body: JSON.stringify({ marked }),
            })
        },
        [deletionMarkPath],
    )

    const markSource = async (ref: IncomingReference) => {
        if (!ref.sourceEntityName || !ref.sourceId) return
        const source = { entityName: ref.sourceEntityName, entityId: ref.sourceId }
        const key = markedKey(source)
        const name = referenceSourceName(ref, entityLabel)
        setMarkingKey(key)
        try {
            await setMark(ref, true)
            const next = new Set(selected)
            next.add(key)
            replan(next, true)
            toast.success(`«${name}» помечен на удаление`, {
                action: {
                    label: "Отменить",
                    onClick: () => {
                        void setMark(ref, false)
                            .then(() => {
                                const restored = new Set(next)
                                restored.delete(key)
                                replan(restored, true)
                            })
                            .catch((e) => toast.error(e instanceof Error ? e.message : "Не удалось снять пометку"))
                    },
                },
            })
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Не удалось пометить объект")
        } finally {
            setMarkingKey(null)
        }
    }

    const selectSource = (ref: IncomingReference) => {
        if (!ref.sourceEntityName || !ref.sourceId) return
        const next = new Set(selected)
        next.add(markedKey({ entityName: ref.sourceEntityName, entityId: ref.sourceId }))
        replan(next, true)
    }

    // ── Delete ───────────────────────────────────────────────────────
    const handleDelete = async () => {
        if (replanTimer.current) clearTimeout(replanTimer.current)
        setConfirmOpen(false)
        setDeleting(true)
        try {
            const outcome = await api.system.markedObjects.delete(Array.from(selected, parseMarkedKey))
            setResult(outcome)
            await loadPlan()
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Не удалось удалить объекты")
        } finally {
            setDeleting(false)
        }
    }

    // ── Grouping ─────────────────────────────────────────────────────
    const groups = useMemo(() => {
        const byEntity = new Map<string, MarkedObject[]>()
        for (const item of items) {
            const list = byEntity.get(item.entityName) ?? []
            list.push(item)
            byEntity.set(item.entityName, list)
        }
        return Array.from(byEntity, ([entityName, list]) => ({
            entityName,
            label: entityLabel(entityName),
            items: [...list].sort((a, b) => (a.presentation || a.entityId).localeCompare(b.presentation || b.entityId, "ru")),
        })).sort((a, b) => a.label.localeCompare(b.label, "ru"))
    }, [items, entityLabel])

    const deletable = plan?.deletable ?? 0
    const failures = result?.items.filter((i) => i.outcome !== "deleted") ?? []

    // ── Render ───────────────────────────────────────────────────────
    return (
        <ScrollArea className="flex-1">
            <div className="space-y-6 p-6">
                <div className="flex items-center justify-between gap-4">
                    <div className="flex items-center gap-3">
                        <Button variant="ghost" size="icon" onClick={() => router.back()} aria-label="Назад">
                            <ArrowLeft className="h-4 w-4" />
                        </Button>
                        <div>
                            <h1 className="text-2xl font-semibold tracking-tight">Удаление помеченных объектов</h1>
                            <p className="text-sm text-muted-foreground mt-1">
                                Объект удаляется, если на него ссылаются только объекты, которые удаляются вместе с ним
                            </p>
                        </div>
                    </div>
                    <div className="flex items-center gap-2">
                        <Button variant="outline" onClick={reloadAll} disabled={planning || deleting}>
                            <RefreshCw className={cn("h-4 w-4 mr-2", planning && "animate-spin")} />
                            Обновить
                        </Button>
                        <Button
                            variant="destructive"
                            onClick={() => setConfirmOpen(true)}
                            disabled={deletable === 0 || planning || deleting}
                        >
                            {deleting ? <Loader2 className="h-4 w-4 animate-spin mr-2" /> : <Trash2 className="h-4 w-4 mr-2" />}
                            Удалить ({deletable})
                        </Button>
                    </div>
                </div>

                {result && (
                    <Card className={failures.length > 0 ? "border-warning/30 bg-warning/5" : "border-success/30 bg-success/5"}>
                        <CardContent className="py-4 space-y-2">
                            <div className="flex items-center gap-3 text-sm">
                                {failures.length > 0 ? (
                                    <AlertTriangle className="h-5 w-5 text-warning shrink-0" />
                                ) : (
                                    <CheckCircle2 className="h-5 w-5 text-success shrink-0" />
                                )}
                                <span className="font-medium">Удалено: {result.deleted}</span>
                                {result.skipped > 0 && <span className="text-muted-foreground">Пропущено: {result.skipped}</span>}
                                {result.errors > 0 && <span className="text-destructive">Ошибки: {result.errors}</span>}
                            </div>
                            {failures.length > 0 && (
                                <ul className="ml-8 space-y-1 text-xs text-muted-foreground">
                                    {failures.map((f) => (
                                        <li key={markedKey(f)}>
                                            <span className="text-foreground">
                                                {f.presentation || `${entityLabel(f.entityName)} ${f.entityId.slice(0, 8)}…`}
                                            </span>
                                            {f.message ? ` — ${f.message}` : ""}
                                        </li>
                                    ))}
                                </ul>
                            )}
                        </CardContent>
                    </Card>
                )}

                <Card>
                    <CardHeader className="pb-3">
                        <CardTitle className="text-base flex items-center gap-2">
                            Помеченные объекты
                            {plan && <Badge variant="secondary" className="font-mono">{plan.total}</Badge>}
                            {planning && !loading && <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />}
                        </CardTitle>
                        {plan && plan.total > 0 && (
                            <CardDescription className="flex flex-wrap gap-x-4 gap-y-1">
                                <span>Выбрано: {plan.selected}</span>
                                <span className="text-success">Удалится: {plan.deletable}</span>
                                {plan.blocked > 0 && <span className="text-destructive">Не удалится: {plan.blocked}</span>}
                            </CardDescription>
                        )}
                        {plan?.truncated && (
                            <p className="text-xs text-warning">
                                Показаны первые 500 объектов каждого типа. Остальные появятся после удаления показанных.
                            </p>
                        )}
                    </CardHeader>
                    <CardContent>
                        {loading ? (
                            <div className="flex items-center justify-center py-12 text-muted-foreground">
                                <Loader2 className="h-5 w-5 animate-spin mr-2" />
                                Строим план удаления…
                            </div>
                        ) : items.length === 0 ? (
                            <div className="text-center py-12 text-muted-foreground">
                                <CheckCircle2 className="h-8 w-8 mx-auto mb-3 text-success/60" />
                                <p className="text-sm font-medium">Нет объектов с пометкой удаления</p>
                            </div>
                        ) : (
                            <TooltipProvider>
                                <Table>
                                    <TableHeader>
                                        <TableRow>
                                            <TableHead className="w-[40px]">
                                                <Checkbox
                                                    checked={allSelected ? true : someSelected ? "indeterminate" : false}
                                                    onCheckedChange={toggleAll}
                                                    aria-label="Выбрать все"
                                                />
                                            </TableHead>
                                            <TableHead className="w-[32px]" />
                                            <TableHead>Объект</TableHead>
                                            <TableHead className="text-center w-[90px]">Ссылки</TableHead>
                                            <TableHead className="w-[230px]">Результат</TableHead>
                                            <TableHead className="w-[48px]" />
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        {groups.map((group) => (
                                            <Fragment key={group.entityName}>
                                                <TableRow className="bg-muted/40 hover:bg-muted/40">
                                                    <TableCell colSpan={6} className="py-1.5 text-xs font-medium text-muted-foreground">
                                                        {group.label} · {group.items.length}
                                                    </TableCell>
                                                </TableRow>
                                                {group.items.map((item) => {
                                                    const key = markedKey(item)
                                                    const isExpanded = expanded.has(key)
                                                    const view = deletionStatusView[item.status]
                                                    const explanation = statusExplanation(item, entityLabel)
                                                    return (
                                                        <Fragment key={key}>
                                                            <TableRow
                                                                className={cn(item.status === "blocked" && "bg-destructive/5 hover:bg-destructive/10")}
                                                                data-state={selected.has(key) ? "selected" : undefined}
                                                            >
                                                                <TableCell>
                                                                    <Checkbox
                                                                        checked={selected.has(key)}
                                                                        onCheckedChange={() => toggleItem(item)}
                                                                        aria-label={`Выбрать ${item.presentation || item.entityId}`}
                                                                    />
                                                                </TableCell>
                                                                <TableCell className="px-0">
                                                                    {item.references.length > 0 && (
                                                                        <Button
                                                                            variant="ghost"
                                                                            size="icon"
                                                                            className="h-7 w-7"
                                                                            onClick={() => toggleExpanded(key)}
                                                                            aria-expanded={isExpanded}
                                                                            aria-label={isExpanded ? "Скрыть ссылки" : "Показать ссылки"}
                                                                        >
                                                                            <ChevronRight className={cn("h-4 w-4 transition-transform", isExpanded && "rotate-90")} />
                                                                        </Button>
                                                                    )}
                                                                </TableCell>
                                                                <TableCell className="text-sm">
                                                                    <button
                                                                        type="button"
                                                                        className="font-medium hover:underline text-left"
                                                                        onClick={() => openEntity(item.entityName, item.entityId)}
                                                                    >
                                                                        {item.presentation || `${item.entityId.slice(0, 8)}…`}
                                                                    </button>
                                                                    {explanation && (
                                                                        <button
                                                                            type="button"
                                                                            className={cn(
                                                                                "block text-xs text-left mt-0.5",
                                                                                item.status === "blocked" ? "text-destructive" : "text-muted-foreground",
                                                                            )}
                                                                            onClick={() => toggleExpanded(key)}
                                                                        >
                                                                            {explanation}
                                                                        </button>
                                                                    )}
                                                                </TableCell>
                                                                <TableCell className="text-center text-xs font-mono">
                                                                    {item.refCount > 0 ? item.refCount : <span className="text-muted-foreground">0</span>}
                                                                </TableCell>
                                                                <TableCell>
                                                                    <span className={cn("inline-flex items-center gap-1.5 text-xs", toneClass[view.tone])}>
                                                                        {item.status === "blocked" ? (
                                                                            <ShieldAlert className="h-3.5 w-3.5" />
                                                                        ) : item.status === "excluded" ? null : (
                                                                            <CheckCircle2 className="h-3.5 w-3.5" />
                                                                        )}
                                                                        {view.label}
                                                                    </span>
                                                                </TableCell>
                                                                <TableCell>
                                                                    {item.refCount > 0 && (
                                                                        <Tooltip>
                                                                            <TooltipTrigger asChild>
                                                                                <Button
                                                                                    variant="ghost"
                                                                                    size="icon"
                                                                                    className="h-7 w-7"
                                                                                    onClick={() => openFindReferences(item)}
                                                                                    aria-label="Найти ссылки"
                                                                                >
                                                                                    <Search className="h-3.5 w-3.5" />
                                                                                </Button>
                                                                            </TooltipTrigger>
                                                                            <TooltipContent>Найти ссылки</TooltipContent>
                                                                        </Tooltip>
                                                                    )}
                                                                </TableCell>
                                                            </TableRow>
                                                            {isExpanded && (
                                                                <TableRow className="hover:bg-transparent">
                                                                    <TableCell />
                                                                    <TableCell colSpan={5} className="pt-0 pb-3">
                                                                        <ReferenceList
                                                                            item={item}
                                                                            selected={selected}
                                                                            markingKey={markingKey}
                                                                            entityLabel={entityLabel}
                                                                            canSetDeletionMark={(name) => deletionMarkPath(name) !== null}
                                                                            onOpen={openEntity}
                                                                            onMark={markSource}
                                                                            onSelect={selectSource}
                                                                            onFindAll={() => openFindReferences(item)}
                                                                        />
                                                                    </TableCell>
                                                                </TableRow>
                                                            )}
                                                        </Fragment>
                                                    )
                                                })}
                                            </Fragment>
                                        ))}
                                    </TableBody>
                                </Table>
                            </TooltipProvider>
                        )}
                    </CardContent>
                </Card>
            </div>

            <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>
                            Удалить {deletable} {plural(deletable, "объект", "объекта", "объектов")}?
                        </AlertDialogTitle>
                        <AlertDialogDescription>
                            Объекты будут удалены из базы без возможности восстановления.
                            {plan && plan.blocked > 0
                                ? ` ${plan.blocked} ${plural(plan.blocked, "объект останется", "объекта останутся", "объектов останутся")} с пометкой удаления.`
                                : ""}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>Отмена</AlertDialogCancel>
                        <AlertDialogAction
                            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                            onClick={() => void handleDelete()}
                        >
                            Удалить
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </ScrollArea>
    )
}

interface ReferenceListProps {
    item: MarkedObject
    selected: Set<string>
    markingKey: string | null
    entityLabel: (entityName: string) => string
    canSetDeletionMark: (entityName: string) => boolean
    onOpen: (entityName: string, entityId: string) => void
    onMark: (ref: IncomingReference) => void
    onSelect: (ref: IncomingReference) => void
    onFindAll: () => void
}

/** "Где используется": everything that references one marked object. */
function ReferenceList({
    item,
    selected,
    markingKey,
    entityLabel,
    canSetDeletionMark,
    onOpen,
    onMark,
    onSelect,
    onFindAll,
}: ReferenceListProps) {
    return (
        <div className="rounded-md border bg-background">
            <div className="px-3 py-2 text-xs font-medium text-muted-foreground border-b">Где используется</div>
            <ul className="divide-y">
                {item.references.map((ref, index) => {
                    const sourceKey = ref.sourceEntityName && ref.sourceId
                        ? markedKey({ entityName: ref.sourceEntityName, entityId: ref.sourceId })
                        : `system:${ref.sourceField}:${index}`
                    const action = referenceAction(
                        ref,
                        selected.has(sourceKey),
                        ref.sourceEntityName ? canSetDeletionMark(ref.sourceEntityName) : false,
                    )
                    const sourceView = ref.sourceStatus ? deletionStatusView[ref.sourceStatus] : null
                    const name = referenceSourceName(ref, entityLabel)
                    return (
                        <li key={`${sourceKey}:${ref.sourceField}`} className="flex items-center gap-3 px-3 py-2 text-sm">
                            <div className="min-w-0 flex-1">
                                {ref.sourceEntityName && ref.sourceId && ref.sourceEntityType !== "register" ? (
                                    <button
                                        type="button"
                                        className="hover:underline text-left truncate max-w-full"
                                        onClick={() => onOpen(ref.sourceEntityName!, ref.sourceId!)}
                                    >
                                        {name}
                                    </button>
                                ) : (
                                    <span className="truncate">{name}</span>
                                )}
                                <div className="text-xs text-muted-foreground">
                                    {ref.sourceEntityName ? `${entityLabel(ref.sourceEntityName)} · ` : ""}
                                    {ref.sourceField}
                                    {ref.count > 1 ? ` · ${ref.count} ${plural(ref.count, "запись", "записи", "записей")}` : ""}
                                </div>
                            </div>
                            <div className="flex items-center gap-2 shrink-0">
                                {ref.sourcePosted && <Badge variant="outline" className="text-xs">Проведён</Badge>}
                                {ref.sourceMarked ? (
                                    <Badge variant="secondary" className="text-xs">
                                        Помечен{sourceView ? ` · ${sourceView.label.toLowerCase()}` : ""}
                                    </Badge>
                                ) : (
                                    <Badge variant="destructive" className="text-xs">Без пометки</Badge>
                                )}
                                {action.kind === "mark" && (
                                    <Button
                                        variant="outline"
                                        size="sm"
                                        className="h-7 text-xs"
                                        disabled={markingKey === sourceKey}
                                        onClick={() => onMark(ref)}
                                    >
                                        {markingKey === sourceKey && <Loader2 className="h-3 w-3 animate-spin mr-1" />}
                                        Пометить на удаление
                                    </Button>
                                )}
                                {action.kind === "select" && (
                                    <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => onSelect(ref)}>
                                        Выбрать для удаления
                                    </Button>
                                )}
                                {action.kind === "none" && action.hint && (
                                    <span className="text-xs text-muted-foreground max-w-[220px] text-right">{action.hint}</span>
                                )}
                            </div>
                        </li>
                    )
                })}
            </ul>
            {item.referencesTruncated && (
                <div className="flex items-center justify-between gap-3 px-3 py-2 border-t text-xs text-muted-foreground">
                    <span>
                        Показаны не все объекты без пометки: всего {item.liveRefCount}
                    </span>
                    <Button variant="link" size="sm" className="h-auto p-0 text-xs" onClick={onFindAll}>
                        Найти все ссылки
                    </Button>
                </div>
            )}
        </div>
    )
}
