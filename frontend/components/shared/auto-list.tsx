"use client"

/**
 * AutoList — metadata-driven list page generator.
 *
 * Generates a list page from UIRegistry columns or /api/v1/meta/:name endpoint.
 * Used as fallback when an entity has no custom listComponent registered.
 *
 * Features:
 *   - Cursor-based pagination (useEntityListPage pattern)
 *   - Search with debounce
 *   - Sorting by column
 *   - Create new button → navigates to /ext/:type/:prefix/new
 */

import { useEffect, useState, useMemo, useCallback } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import { apiFetch, ApiError } from "@/lib/api"
import { buildColumnsFromFields, formatCellValue, type MetadataField } from "@/lib/metadata-columns"
import { entityRegistry, type AutoListColumn } from "@/lib/entity-registry"
import { useMetadataStore } from "@/stores/useMetadataStore"
import type { CursorListResponse } from "@/types/common"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow
} from "@/components/ui/table"
import {
    Loader2,
    Plus,
    ChevronLeft,
    ChevronRight,
    Search,
    ArrowUp,
    ArrowDown,
} from "lucide-react"


interface AutoListProps {
    /** PascalCase entity name (e.g. "Vehicle") */
    entityName: string
    /** Entity type for path construction */
    entityType: "catalog" | "document"
    /** Route prefix for API and navigation */
    routePrefix: string
}

const DEFAULT_LIMIT = 50

/** Format a cell value for display based on column type */
function formatCell(value: unknown, col: AutoListColumn): string {
    return formatCellValue(value, col.type ?? "string")
}

export default function AutoList({ entityName, entityType, routePrefix }: AutoListProps) {
    const router = useRouter()
    const searchParams = useSearchParams()

    const [items, setItems] = useState<Record<string, unknown>[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<string | null>(null)
    const [totalCount, setTotalCount] = useState(0)
    const [hasMore, setHasMore] = useState(false)
    const [hasPrev, setHasPrev] = useState(false)
    const [nextCursor, setNextCursor] = useState<string | null>(null)
    const [prevCursor, setPrevCursor] = useState<string | null>(null)
    const [search, setSearch] = useState(searchParams.get("search") ?? "")
    const [sortField, setSortField] = useState(searchParams.get("sort") ?? "name")
    const [sortDir, setSortDir] = useState<"asc" | "desc">(
        searchParams.get("dir") === "desc" ? "desc" : "asc"
    )

    // Get metadata for presentation labels
    const metaStore = useMetadataStore()
    const entityMeta = metaStore.getEntityByName?.(entityName)
    const label = entityMeta?.presentation?.plural ?? entityName

    // Resolve columns: UIRegistry custom → metadata fallback → generic
    const [metaColumns, setMetaColumns] = useState<AutoListColumn[]>([])

    const regEntry = entityRegistry.getByRoute(routePrefix)
    const columns = useMemo<AutoListColumn[]>(() => {
        if (regEntry?.listColumns?.length) return regEntry.listColumns
        if (metaColumns.length) return metaColumns
        // Absolute fallback: code + name
        return [
            { key: "code", label: "Код", width: "120px" },
            { key: "name", label: "Наименование" },
        ]
    }, [regEntry, metaColumns])

    // Load metadata columns if not provided via UIRegistry
    useEffect(() => {
        if (regEntry?.listColumns?.length) return
        let cancelled = false
        async function loadMeta() {
            try {
                const meta = await apiFetch<{
                    fields: MetadataField[]
                }>(`/meta/${entityName}`)
                if (cancelled) return

                const cols = buildColumnsFromFields(meta.fields, 8)
                setMetaColumns(
                    cols.map((c) => ({
                        key: c.key,
                        label: c.label,
                        type: c.type,
                    }))
                )
            } catch {
                // Ignore — will use fallback columns
            }
        }
        loadMeta()
        return () => { cancelled = true }
    }, [entityName, regEntry])

    // Fetch list data
    const fetchData = useCallback(async (cursor?: string, direction?: "after" | "before") => {
        setLoading(true)
        setError(null)
        try {
            const basePath = entityType === "catalog"
                ? `/catalog/${routePrefix}`
                : `/document/${routePrefix}`

            const params = new URLSearchParams()
            params.set("limit", String(DEFAULT_LIMIT))
            if (search) params.set("search", search)
            const orderBy = sortDir === "desc" ? `-${sortField}` : sortField
            params.set("orderBy", orderBy)
            if (cursor) params.set(direction === "before" ? "before" : "after", cursor)

            const result = await apiFetch<CursorListResponse<Record<string, unknown>>>(
                `${basePath}?${params.toString()}`
            )
            setItems(result.items ?? [])
            setTotalCount(result.totalCount ?? 0)
            setHasMore(result.hasMore)
            setHasPrev(result.hasPrev)
            setNextCursor(result.nextCursor ?? null)
            setPrevCursor(result.prevCursor ?? null)
        } catch (err) {
            if (err instanceof ApiError) {
                setError(err.parsedBody?.message ?? `Ошибка ${err.status}`)
            } else {
                setError("Ошибка загрузки данных")
            }
        } finally {
            setLoading(false)
        }
    }, [entityType, routePrefix, search, sortField, sortDir])

    // Debounced search
    useEffect(() => {
        const timer = setTimeout(() => fetchData(), 300)
        return () => clearTimeout(timer)
    }, [fetchData])

    const handleSort = (key: string) => {
        if (sortField === key) {
            setSortDir((d) => (d === "asc" ? "desc" : "asc"))
        } else {
            setSortField(key)
            setSortDir("asc")
        }
    }

    const handleRowClick = (item: Record<string, unknown>) => {
        if (!item.id) return
        const path = entityType === "catalog"
            ? `/catalogs/${routePrefix}/${item.id}`
            : `/documents/${routePrefix}/${item.id}`
        router.push(path)
    }

    const handleCreate = () => {
        const path = entityType === "catalog"
            ? `/catalogs/${routePrefix}/new`
            : `/documents/${routePrefix}/new`
        router.push(path)
    }

    return (
        <div className="flex flex-col gap-4 p-4">
            {/* Header */}
            <div className="flex items-center justify-between">
                <h1 className="text-lg font-semibold">{label}</h1>
                <Button onClick={handleCreate} size="sm">
                    <Plus className="mr-2 h-4 w-4" /> Создать
                </Button>
            </div>

            {/* Search */}
            <div className="relative max-w-sm">
                <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                    placeholder="Поиск..."
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    className="pl-8"
                />
            </div>

            {/* Table */}
            {error ? (
                <div className="rounded border border-destructive/20 bg-destructive/10 p-4 text-sm text-destructive">
                    {error}
                </div>
            ) : (
                <div className="rounded-md border">
                    <Table>
                        <TableHeader>
                            <TableRow>
                                {columns.map((col) => (
                                    <TableHead
                                        key={col.key}
                                        className="cursor-pointer select-none"
                                        style={{ width: col.width }}
                                        onClick={() => col.sortable !== false && handleSort(col.key)}
                                    >
                                        <div className="flex items-center gap-1">
                                            {col.label}
                                            {sortField === col.key && (
                                                sortDir === "asc"
                                                    ? <ArrowUp className="h-3 w-3" />
                                                    : <ArrowDown className="h-3 w-3" />
                                            )}
                                        </div>
                                    </TableHead>
                                ))}
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {loading ? (
                                <TableRow>
                                    <TableCell colSpan={columns.length} className="py-10 text-center">
                                        <Loader2 className="inline-block h-5 w-5 animate-spin text-muted-foreground" />
                                    </TableCell>
                                </TableRow>
                            ) : items.length === 0 ? (
                                <TableRow>
                                    <TableCell colSpan={columns.length} className="py-10 text-center text-muted-foreground">
                                        Нет данных
                                    </TableCell>
                                </TableRow>
                            ) : (
                                items.map((item, idx) => (
                                    <TableRow
                                        key={item.id ? String(item.id) : idx}
                                        className="cursor-pointer hover:bg-muted/50"
                                        onClick={() => handleRowClick(item)}
                                    >
                                        {columns.map((col) => (
                                            <TableCell key={col.key} className="truncate">
                                                {formatCell(item[col.key], col)}
                                            </TableCell>
                                        ))}
                                    </TableRow>
                                ))
                            )}
                        </TableBody>
                    </Table>
                </div>
            )}

            {/* Pagination */}
            <div className="flex items-center justify-between text-sm text-muted-foreground">
                <span>Всего: {totalCount}</span>
                <div className="flex gap-1">
                    <Button
                        variant="outline"
                        size="icon"
                        className="h-8 w-8"
                        disabled={!hasPrev || loading}
                        onClick={() => prevCursor && fetchData(prevCursor, "before")}
                    >
                        <ChevronLeft className="h-4 w-4" />
                    </Button>
                    <Button
                        variant="outline"
                        size="icon"
                        className="h-8 w-8"
                        disabled={!hasMore || loading}
                        onClick={() => nextCursor && fetchData(nextCursor, "after")}
                    >
                        <ChevronRight className="h-4 w-4" />
                    </Button>
                </div>
            </div>
        </div>
    )
}
