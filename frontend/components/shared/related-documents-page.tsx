"use client"

/**
 * RelatedDocumentsPage — full-page view of documents related to the current one.
 *
 * Designed in the style of 1C "Структура подчинённости" — a recursive tree showing
 * the full document subordination chain (basis_type/basis_id hierarchy).
 *
 * Features:
 * - Human-readable header (type + number + date from API self field)
 * - Recursive tree with collapsible nodes
 * - Context menu with quick actions (post, unpost, deletion mark, create based on)
 * - Hover preview card with key document fields + metadata-driven preview data
 * - Auto-refresh after any action
 * - Flat groups for FK-references outside the basis chain
 */

import { useEffect, useState, useCallback } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import {
    ArrowLeft,
    Loader2,
    FileText,
    ChevronRight,
    ChevronDown,
    ExternalLink,
    Play,
    Undo2,
    Trash2,
    ShieldOff,
    PlusCircle,
    RefreshCw,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Badge } from "@/components/ui/badge"
import {
    ContextMenu,
    ContextMenuContent,
    ContextMenuItem,
    ContextMenuSeparator,
    ContextMenuTrigger,
} from "@/components/ui/context-menu"
import {
    HoverCard,
    HoverCardContent,
    HoverCardTrigger,
} from "@/components/ui/hover-card"
import { buildEntityUrlByRoute } from "@/lib/entity-url"
import { useCurrencyScale } from "@/hooks/useCurrencyScale"
import { fmtAmount, fmtDate } from "@/lib/format"
import { apiFetch } from "@/lib/api"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import type {
    RelatedDocGroup,
    RelatedDocItem,
    RelatedDocTreeNode,
    RelatedDocumentsResponse,
} from "@/types/common"

// ── Formatted amount helper ─────────────────────────────────────────────

function FormattedAmount({ minor, currencyId }: { minor: number; currencyId?: string }) {
    const { decimalPlaces, symbol } = useCurrencyScale(currencyId)
    return (
        <>
            {fmtAmount(minor, Math.max(0, decimalPlaces))}
            {symbol ? ` ${symbol}` : ""}
        </>
    )
}

// ── Status badge helper ─────────────────────────────────────────────────

function StatusBadge({ item }: { item: RelatedDocItem }) {
    if (item.deletionMark) {
        return (
            <span className="inline-flex items-center justify-center rounded bg-rose-100 px-1.5 py-0.5 text-[10px] font-medium text-rose-700 dark:bg-rose-950 dark:text-rose-300 w-16 text-center">
                Удален
            </span>
        )
    }
    if (!item.posted) {
        return (
            <span className="inline-flex items-center justify-center rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-300 w-16 text-center">
                Черновик
            </span>
        )
    }
    return (
        <span className="inline-flex items-center justify-center rounded bg-emerald-100 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300 w-16 text-center">
            Проведён
        </span>
    )
}

// ── Status icon for tree (compact) ──────────────────────────────────────

function StatusIcon({ item }: { item: RelatedDocItem }) {
    if (item.deletionMark) {
        return <span className="text-rose-500 text-xs" title="Помечен на удаление">✕</span>
    }
    if (item.posted) {
        return <span className="text-emerald-500 text-xs" title="Проведён">✓</span>
    }
    return <span className="text-muted-foreground text-xs" title="Черновик">○</span>
}

// ── Document action helpers ─────────────────────────────────────────────

interface CreateBasedOnOption {
    label: string
    routePrefix: string
    basisType: string
}

interface DocumentActionConfig {
    apiBasePath: string
    routePrefix: string
    entityTypeLabel: string
    createBasedOn?: CreateBasedOnOption[]
}

// ── Props ───────────────────────────────────────────────────────────────

interface RelatedDocumentsPageProps {
    documentId: string
    backHref: string
    entityTypeLabel: string
    fetcher: (id: string) => Promise<RelatedDocumentsResponse>
    documentConfig: DocumentActionConfig
    /** Mapping from entityName to action config for related document types */
    relatedConfigs?: Record<string, DocumentActionConfig>
}

export function RelatedDocumentsPage({
    documentId,
    backHref,
    entityTypeLabel,
    fetcher,
    documentConfig,
    relatedConfigs,
}: RelatedDocumentsPageProps) {
    const router = useRouter()
    const [data, setData] = useState<RelatedDocumentsResponse | null>(null)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<string | null>(null)
    const [actionLoading, setActionLoading] = useState(false)

    const doFetch = useCallback(() => {
        setLoading(true)
        setError(null)
        fetcher(documentId)
            .then((res) => setData(res))
            .catch((e) => setError(e instanceof Error ? e.message : "Ошибка загрузки"))
            .finally(() => setLoading(false))
    }, [documentId, fetcher])

    useEffect(() => {
        doFetch()
    }, [doFetch])

    const tree = data?.tree
    const flatGroups = data?.flatGroups ?? []

    // Human-readable title from tree root (current document is always in the tree)
    const rootDoc = tree?.isCurrent ? tree : undefined
    const headerTitle = rootDoc
        ? `${entityTypeLabel} ${rootDoc.number} от ${fmtDate(rootDoc.date)}`
        : entityTypeLabel

    // Count total items
    const totalItems = data?.total ?? 0

    // ── Quick actions ───────────────────────────────────────────────────

    const executeAction = async (
        actionFn: () => Promise<void>,
        successMessage: string,
    ) => {
        setActionLoading(true)
        try {
            await actionFn()
            toast.success(successMessage)
            doFetch() // auto-refresh
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Ошибка выполнения действия")
        } finally {
            setActionLoading(false)
        }
    }

    const handlePost = (apiPath: string, id: string) =>
        executeAction(
            () => apiFetch<void>(`${apiPath}/${id}/post`, { method: "POST" }),
            "Документ проведён",
        )

    const handleUnpost = (apiPath: string, id: string) =>
        executeAction(
            () => apiFetch<void>(`${apiPath}/${id}/unpost`, { method: "POST" }),
            "Проведение отменено",
        )

    const handleToggleDeletionMark = (apiPath: string, id: string, currentlyMarked: boolean) =>
        executeAction(
            () =>
                apiFetch<void>(`${apiPath}/${id}/deletion-mark`, {
                    method: "POST",
                    body: JSON.stringify({ marked: !currentlyMarked }),
                }),
            currentlyMarked ? "Пометка удаления снята" : "Документ помечен на удаление",
        )

    const getConfigForEntity = (entityName: string): DocumentActionConfig | undefined => {
        if (entityName === documentConfig.routePrefix || entityName === documentConfig.entityTypeLabel) {
            return documentConfig
        }
        return relatedConfigs?.[entityName]
    }

    // Resolve config from tree node
    const getConfigForNode = (node: RelatedDocTreeNode): DocumentActionConfig | undefined => {
        // Try direct entityName match first
        const config = relatedConfigs?.[node.entityName]
        if (config) return config

        // Check if it's the current document type
        if (node.routePrefix === documentConfig.routePrefix) return documentConfig

        // Fallback: build a minimal config from the node data
        if (node.routePrefix) {
            return {
                apiBasePath: `/document/${node.routePrefix}`,
                routePrefix: node.routePrefix,
                entityTypeLabel: node.entityName,
                createBasedOn: relatedConfigs?.[node.entityName]?.createBasedOn,
            }
        }

        return undefined
    }

    return (
        <div className="flex h-full flex-col">
            {/* Header */}
            <div className="border-b bg-card sticky top-0 z-20 shadow-sm">
                <div className="flex items-center gap-2 px-4 py-2">
                    <Button variant="ghost" size="icon" className="h-7 w-7" asChild>
                        <Link href={backHref}>
                            <ArrowLeft className="h-3.5 w-3.5" />
                        </Link>
                    </Button>
                    <h1 className="text-sm font-semibold truncate">
                        Связанные документы — {headerTitle}
                    </h1>
                    <span className="ml-auto flex items-center gap-2 shrink-0">
                        <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7"
                            onClick={doFetch}
                            disabled={loading || actionLoading}
                            title="Обновить"
                        >
                            <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
                        </Button>
                        <span className="text-xs text-muted-foreground tabular-nums">
                            Всего: {totalItems}
                        </span>
                    </span>
                </div>
            </div>

            {/* Loading overlay for actions */}
            {actionLoading && (
                <div className="bg-background/50 absolute inset-0 z-30 flex items-center justify-center">
                    <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                </div>
            )}

            {/* Content */}
            {loading && !data && (
                <div className="flex flex-1 items-center justify-center">
                    <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                </div>
            )}

            {error && (
                <div className="flex flex-1 items-center justify-center text-sm text-destructive">
                    {error}
                </div>
            )}

            {!loading && !error && !tree && flatGroups.length === 0 && (
                <div className="flex flex-1 flex-col items-center justify-center text-muted-foreground">
                    <FileText className="h-8 w-8 mb-2 opacity-40" />
                    <span className="text-sm">Связанные документы не найдены</span>
                </div>
            )}

            {data && (
                <ScrollArea className="flex-1">
                <div className="p-4">
                    <div className="max-w-4xl mx-auto space-y-4">
                        {/* ── Subordination tree ── */}
                        {tree && (
                            <div>
                                <TreeNodeComponent
                                    node={tree}
                                    depth={0}
                                    getConfig={getConfigForNode}
                                    onPost={(apiPath, id) => handlePost(apiPath, id)}
                                    onUnpost={(apiPath, id) => handleUnpost(apiPath, id)}
                                    onToggleDeletionMark={(apiPath, id, marked) =>
                                        handleToggleDeletionMark(apiPath, id, marked)
                                    }
                                    onCreateBasedOn={(routePrefix, basisType, basisId) =>
                                        router.push(
                                            `/documents/${routePrefix}s/new?basisType=${basisType}&basisId=${basisId}`,
                                        )
                                    }
                                />
                            </div>
                        )}

                        {/* ── Flat groups (FK-references not in basis chain) ── */}
                        {flatGroups.length > 0 && (
                            <div className="space-y-2">
                                <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wide px-1">
                                    Прочие ссылки
                                </h2>
                                {flatGroups.map((group) => (
                                    <FlatGroup
                                        key={group.entityName}
                                        group={group}
                                        config={getConfigForEntity(group.entityName)}
                                        onPost={(apiPath, id) => handlePost(apiPath, id)}
                                        onUnpost={(apiPath, id) => handleUnpost(apiPath, id)}
                                        onToggleDeletionMark={(apiPath, id, marked) =>
                                            handleToggleDeletionMark(apiPath, id, marked)
                                        }
                                        onCreateBasedOn={(routePrefix, basisType, basisId) =>
                                            router.push(
                                                `/documents/${routePrefix}s/new?basisType=${basisType}&basisId=${basisId}`,
                                            )
                                        }
                                    />
                                ))}
                            </div>
                        )}
                    </div>
                </div>
                </ScrollArea>
            )}
        </div>
    )
}

// ── Recursive Tree Node ─────────────────────────────────────────────────

interface TreeNodeComponentProps {
    node: RelatedDocTreeNode
    depth: number
    getConfig: (node: RelatedDocTreeNode) => DocumentActionConfig | undefined
    onPost: (apiPath: string, id: string) => void
    onUnpost: (apiPath: string, id: string) => void
    onToggleDeletionMark: (apiPath: string, id: string, currentlyMarked: boolean) => void
    onCreateBasedOn: (routePrefix: string, basisType: string, basisId: string) => void
}

function TreeNodeComponent({
    node,
    depth,
    getConfig,
    onPost,
    onUnpost,
    onToggleDeletionMark,
    onCreateBasedOn,
}: TreeNodeComponentProps) {
    const router = useRouter()
    const [collapsed, setCollapsed] = useState(false)
    const hasChildren = (node.children?.length ?? 0) > 0
    const config = getConfig(node)
    const url = node.routePrefix
        ? buildEntityUrlByRoute(node.routePrefix, node.entityType, node.id)
        : "#"

    return (
        <div>
            <ContextMenu>
                <ContextMenuTrigger asChild>
                    <HoverCard openDelay={400} closeDelay={100}>
                        <HoverCardTrigger asChild>
                            <div
                                className={cn(
                                    "flex items-center gap-2 px-3 py-2 rounded-md transition-colors cursor-default group",
                                    node.isCurrent
                                        ? "bg-primary/5 border border-primary/20 hover:bg-primary/10"
                                        : "hover:bg-muted/30",
                                )}
                                style={{ marginLeft: depth * 24 }}
                            >
                                {/* Expand/collapse button */}
                                {hasChildren ? (
                                    <button
                                        onClick={() => setCollapsed(!collapsed)}
                                        className="shrink-0 p-0.5 rounded hover:bg-muted/50 transition-colors"
                                    >
                                        {collapsed ? (
                                            <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
                                        ) : (
                                            <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                                        )}
                                    </button>
                                ) : (
                                    <span className="w-5 shrink-0" />
                                )}

                                {/* Tree connector */}
                                {depth > 0 && (
                                    <span className="text-muted-foreground/40 text-xs select-none shrink-0">
                                        {hasChildren ? "┬" : "─"}
                                    </span>
                                )}

                                <StatusIcon item={node} />

                                <Link
                                    href={url}
                                    className={cn(
                                        "text-[13px] hover:underline underline-offset-4 truncate text-foreground decoration-muted-foreground/40",
                                    )}
                                >
                                    {node.presentation || `${node.entityName} ${node.number}`}
                                </Link>

                                <span className="ml-auto text-right font-mono text-[13px] tabular-nums text-muted-foreground shrink-0">
                                    {node.amount ? (
                                        <FormattedAmount minor={node.amount} currencyId={node.currencyId} />
                                    ) : null}
                                </span>

                                <StatusBadge item={node} />
                            </div>
                        </HoverCardTrigger>
                        <HoverCardContent className="w-80" side="right" align="start">
                            <DocumentPreviewCard item={node} entityTypeLabel={node.entityName} />
                        </HoverCardContent>
                    </HoverCard>
                </ContextMenuTrigger>
                <DocumentContextMenu
                    item={node}
                    url={url}
                    onOpen={() => router.push(url)}
                    onPost={
                        !node.posted && !node.deletionMark && config
                            ? () => onPost(config.apiBasePath, node.id)
                            : undefined
                    }
                    onUnpost={
                        node.posted && config
                            ? () => onUnpost(config.apiBasePath, node.id)
                            : undefined
                    }
                    onToggleDeletionMark={
                        config
                            ? () => onToggleDeletionMark(config.apiBasePath, node.id, node.deletionMark)
                            : undefined
                    }
                    isDeletionMarked={node.deletionMark}
                    createBasedOn={config?.createBasedOn}
                    onCreateBasedOn={
                        config?.createBasedOn
                            ? (routePrefix, basisType) =>
                                  onCreateBasedOn(routePrefix, basisType, node.id)
                            : undefined
                    }
                />
            </ContextMenu>

            {/* Children */}
            {!collapsed &&
                node.children?.map((child) => (
                    <TreeNodeComponent
                        key={child.id}
                        node={child}
                        depth={depth + 1}
                        getConfig={getConfig}
                        onPost={onPost}
                        onUnpost={onUnpost}
                        onToggleDeletionMark={onToggleDeletionMark}
                        onCreateBasedOn={onCreateBasedOn}
                    />
                ))}
        </div>
    )
}

// ── Flat Group (FK-references) ──────────────────────────────────────────

function FlatGroup({
    group,
    config,
    onPost,
    onUnpost,
    onToggleDeletionMark,
    onCreateBasedOn,
}: {
    group: RelatedDocGroup
    config?: DocumentActionConfig
    onPost: (apiPath: string, id: string) => void
    onUnpost: (apiPath: string, id: string) => void
    onToggleDeletionMark: (apiPath: string, id: string, currentlyMarked: boolean) => void
    onCreateBasedOn: (routePrefix: string, basisType: string, basisId: string) => void
}) {
    const router = useRouter()
    const [collapsed, setCollapsed] = useState(false)

    return (
        <div className="ml-2 border-l-2 border-muted">
            <button
                onClick={() => setCollapsed(!collapsed)}
                className="flex items-center gap-2 pl-4 py-1.5 w-full text-left hover:bg-muted/30 transition-colors"
            >
                {collapsed ? (
                    <ChevronRight className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                ) : (
                    <ChevronDown className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                )}
                <FileText className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                <span className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wide">
                    {group.presentation}
                </span>
                <Badge variant="secondary" className="text-[10px] h-5">
                    {group.totalCount}
                </Badge>
            </button>

            {!collapsed && (
                <div className="space-y-0">
                    {group.items.map((item, idx) => {
                        const isLast = idx === group.items.length - 1 && group.totalCount <= group.items.length
                        const url = buildEntityUrlByRoute(group.routePrefix, group.entityType, item.id)

                        return (
                            <ContextMenu key={item.id}>
                                <ContextMenuTrigger asChild>
                                    <HoverCard openDelay={400} closeDelay={100}>
                                        <HoverCardTrigger asChild>
                                            <div className="flex items-center gap-2 pl-4 pr-3 py-2 hover:bg-muted/30 transition-colors cursor-default group">
                                                <span className="text-muted-foreground/40 text-xs select-none w-4 text-center shrink-0">
                                                    {isLast ? "└" : "├"}
                                                </span>
                                                <StatusIcon item={item} />
                                                <Link
                                                    href={url}
                                                    className="text-[13px] text-foreground hover:underline underline-offset-4 decoration-muted-foreground/40 truncate"
                                                >
                                                    {item.presentation}
                                                </Link>
                                                <span className="ml-auto text-right font-mono text-[13px] tabular-nums text-muted-foreground shrink-0">
                                                    {item.amount ? (
                                                        <FormattedAmount minor={item.amount} currencyId={item.currencyId} />
                                                    ) : null}
                                                </span>
                                                <StatusBadge item={item} />
                                            </div>
                                        </HoverCardTrigger>
                                        <HoverCardContent className="w-80" side="right" align="start">
                                            <DocumentPreviewCard item={item} entityTypeLabel={group.presentation} />
                                        </HoverCardContent>
                                    </HoverCard>
                                </ContextMenuTrigger>
                                <DocumentContextMenu
                                    item={item}
                                    url={url}
                                    onOpen={() => router.push(url)}
                                    onPost={
                                        !item.posted && !item.deletionMark && config
                                            ? () => onPost(config.apiBasePath, item.id)
                                            : undefined
                                    }
                                    onUnpost={
                                        item.posted && config
                                            ? () => onUnpost(config.apiBasePath, item.id)
                                            : undefined
                                    }
                                    onToggleDeletionMark={
                                        config
                                            ? () => onToggleDeletionMark(config.apiBasePath, item.id, item.deletionMark)
                                            : undefined
                                    }
                                    isDeletionMarked={item.deletionMark}
                                    createBasedOn={config?.createBasedOn}
                                    onCreateBasedOn={
                                        config?.createBasedOn
                                            ? (routePrefix, basisType) =>
                                                  onCreateBasedOn(routePrefix, basisType, item.id)
                                            : undefined
                                    }
                                />
                            </ContextMenu>
                        )
                    })}

                    {group.totalCount > group.items.length && (
                        <div className="pl-10 py-1.5">
                            <span className="text-[11px] text-muted-foreground italic">
                                показано {group.items.length} из {group.totalCount}…
                            </span>
                        </div>
                    )}
                </div>
            )}
        </div>
    )
}

// ── Context Menu ────────────────────────────────────────────────────────

function DocumentContextMenu({
    item,
    url,
    onOpen,
    onPost,
    onUnpost,
    onToggleDeletionMark,
    isDeletionMarked,
    createBasedOn,
    onCreateBasedOn,
}: {
    item: RelatedDocItem
    url: string
    onOpen: () => void
    onPost?: () => void
    onUnpost?: () => void
    onToggleDeletionMark?: () => void
    isDeletionMarked: boolean
    createBasedOn?: CreateBasedOnOption[]
    onCreateBasedOn?: (routePrefix: string, basisType: string) => void
}) {
    return (
        <ContextMenuContent className="w-56">
            <ContextMenuItem onClick={onOpen}>
                <ExternalLink className="mr-2 h-3.5 w-3.5" />
                Открыть
            </ContextMenuItem>

            {(onPost || onUnpost) && <ContextMenuSeparator />}

            {onPost && (
                <ContextMenuItem onClick={onPost}>
                    <Play className="mr-2 h-3.5 w-3.5" />
                    Провести
                </ContextMenuItem>
            )}

            {onUnpost && (
                <ContextMenuItem onClick={onUnpost}>
                    <Undo2 className="mr-2 h-3.5 w-3.5" />
                    Отменить проведение
                </ContextMenuItem>
            )}

            {onToggleDeletionMark && (
                <>
                    <ContextMenuSeparator />
                    <ContextMenuItem
                        onClick={onToggleDeletionMark}
                        className={isDeletionMarked ? "" : "text-destructive focus:text-destructive"}
                    >
                        {isDeletionMarked ? (
                            <>
                                <ShieldOff className="mr-2 h-3.5 w-3.5" />
                                Снять пометку удаления
                            </>
                        ) : (
                            <>
                                <Trash2 className="mr-2 h-3.5 w-3.5" />
                                Пометить на удаление
                            </>
                        )}
                    </ContextMenuItem>
                </>
            )}

            {createBasedOn && createBasedOn.length > 0 && onCreateBasedOn && (
                <>
                    <ContextMenuSeparator />
                    {createBasedOn.map((opt) => (
                        <ContextMenuItem
                            key={opt.basisType}
                            onClick={() => onCreateBasedOn(opt.routePrefix, opt.basisType)}
                        >
                            <PlusCircle className="mr-2 h-3.5 w-3.5" />
                            {opt.label}
                        </ContextMenuItem>
                    ))}
                </>
            )}
        </ContextMenuContent>
    )
}

// ── Hover Preview Card ──────────────────────────────────────────────────

function DocumentPreviewCard({
    item,
    entityTypeLabel,
}: {
    item: RelatedDocItem
    entityTypeLabel: string
}) {
    return (
        <div className="space-y-2">
            <div className="flex items-start justify-between gap-2">
                <div>
                    <p className="text-xs text-muted-foreground">{entityTypeLabel}</p>
                    <p className="text-sm font-semibold">{item.number}</p>
                </div>
                <StatusBadge item={item} />
            </div>

            <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
                <div>
                    <span className="text-muted-foreground">Дата:</span>
                    <span className="ml-1 font-medium">{fmtDate(item.date)}</span>
                </div>
                {item.amount != null && item.amount !== 0 && (
                    <div>
                        <span className="text-muted-foreground">Сумма:</span>
                        <span className="ml-1 font-medium font-mono tabular-nums">
                            <FormattedAmount minor={item.amount} currencyId={item.currencyId} />
                        </span>
                    </div>
                )}
            </div>

            {/* Dynamic preview fields from metadata */}
            {item.previewData && Object.keys(item.previewData).length > 0 && (
                <div className="grid grid-cols-1 gap-y-1 text-xs border-t pt-2">
                    {Object.entries(item.previewData).map(([label, value]) => (
                        <div key={label} className="flex items-baseline gap-1">
                            <span className="text-muted-foreground shrink-0">{label}:</span>
                            <span className="font-medium truncate">{value}</span>
                        </div>
                    ))}
                </div>
            )}

            <p className="text-[11px] text-muted-foreground">{item.presentation}</p>
        </div>
    )
}
