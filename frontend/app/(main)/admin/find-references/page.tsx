"use client"

/**
 * Find References — admin tool page.
 *
 * Analogous to 1C's "Найти ссылки на объект" or "Search for object references".
 * Allows selecting any entity via CompoundReferenceField and displays
 * all objects that reference it across all registered tables.
 */

import { useState, useCallback } from "react"
import { useRouter } from "next/navigation"
import { Search, Loader2, ArrowLeft, ExternalLink } from "lucide-react"
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
import { CompoundReferenceField } from "@/components/shared/compound-reference-field"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { useTabsStore } from "@/stores/useTabsStore"
import { resolveTitleFromUrl } from "@/lib/tab-utils"
import { api } from "@/lib/api"
import type { TypedRef, FoundReference } from "@/types/common"

export default function FindReferencesPage() {
    const router = useRouter()
    const openTab = useTabsStore((s) => s.openTab)
    const getEntityByName = useMetadataStore((s) => s.getEntityByName)
    const getLabel = useMetadataStore((s) => s.getLabel)

    // ── State ────────────────────────────────────────────────────────────
    const [targetRef, setTargetRef] = useState<TypedRef>({ refType: "", refId: "" })
    const [targetPresentation, setTargetPresentation] = useState("")
    const [results, setResults] = useState<FoundReference[]>([])
    const [loading, setLoading] = useState(false)
    const [searched, setSearched] = useState(false)

    // ── Handlers ─────────────────────────────────────────────────────────
    const handleRefChange = useCallback((ref: TypedRef, presentation: string) => {
        setTargetRef(ref)
        setTargetPresentation(presentation)
        setResults([])
        setSearched(false)
    }, [])

    const handleSearch = useCallback(async () => {
        if (!targetRef.refType || !targetRef.refId) return

        setLoading(true)
        try {
            const data = await api.system.findReferences({
                entityName: targetRef.refType,
                entityId: targetRef.refId,
            })
            setResults(data.items)
            setSearched(true)
        } catch {
            setResults([])
            setSearched(true)
        } finally {
            setLoading(false)
        }
    }, [targetRef])

    const navigateToEntity = useCallback(
        (entityName: string, entityId: string) => {
            const meta = getEntityByName(entityName)
            if (!meta) return

            const prefix = meta.routePrefix ?? meta.key
            const basePath = meta.type === "catalog" ? "catalogs" : meta.type === "document" ? entityName.toLowerCase() : ""
            const url = `/${basePath}/${prefix}/${entityId}`

            openTab({ id: url, title: resolveTitleFromUrl(url), url })
            router.push(url)
        },
        [getEntityByName, openTab, router],
    )

    // ── Render ────────────────────────────────────────────────────────────
    return (
        <ScrollArea className="flex-1">
        <div className="space-y-6 p-6">
            {/* Header */}
            <div className="flex items-center gap-3">
                <Button variant="ghost" size="icon" onClick={() => router.back()}>
                    <ArrowLeft className="h-4 w-4" />
                </Button>
                <div>
                    <h1 className="text-2xl font-semibold tracking-tight">
                        Найти ссылки на объект
                    </h1>
                    <p className="text-sm text-muted-foreground mt-1">
                        Поиск всех документов и справочников, ссылающихся на выбранный объект
                    </p>
                </div>
            </div>

            {/* Search form */}
            <Card>
                <CardHeader className="pb-4">
                    <CardTitle className="text-base">Объект для поиска</CardTitle>
                    <CardDescription>
                        Выберите тип и объект, ссылки на который нужно найти
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    <div className="flex gap-3 items-end">
                        <div className="flex-1">
                            <CompoundReferenceField
                                value={targetRef}
                                onChange={handleRefChange}
                                placeholder="Тип объекта…"
                            />
                        </div>
                        <Button
                            onClick={handleSearch}
                            disabled={!targetRef.refType || !targetRef.refId || loading}
                        >
                            {loading ? (
                                <Loader2 className="h-4 w-4 animate-spin mr-2" />
                            ) : (
                                <Search className="h-4 w-4 mr-2" />
                            )}
                            Найти
                        </Button>
                    </div>
                </CardContent>
            </Card>

            {/* Results */}
            {searched && (
                <Card>
                    <CardHeader className="pb-3">
                        <CardTitle className="text-base flex items-center gap-2">
                            Результаты
                            <Badge variant="secondary" className="font-mono">
                                {results.length}
                            </Badge>
                        </CardTitle>
                        {targetPresentation && (
                            <CardDescription>
                                Ссылки на: {targetPresentation}
                            </CardDescription>
                        )}
                    </CardHeader>
                    <CardContent>
                        {results.length === 0 ? (
                            <div className="text-center py-8 text-muted-foreground">
                                <p className="text-sm">
                                    Ссылки на данный объект не найдены.
                                </p>
                                <p className="text-xs mt-1">
                                    Объект можно безопасно удалить.
                                </p>
                            </div>
                        ) : (
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead className="w-[50px]">№</TableHead>
                                        <TableHead>Объект</TableHead>
                                        <TableHead>Тип</TableHead>
                                        <TableHead>Поле</TableHead>
                                        <TableHead className="w-[50px]" />
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {results.map((ref, idx) => {
                                        const entityLabel = getLabel(
                                            getEntityByName(ref.sourceEntityName)?.key ?? ref.sourceEntityName,
                                            "singular",
                                        )
                                        return (
                                            <TableRow
                                                key={`${ref.sourceEntityName}-${ref.sourceId}-${idx}`}
                                                className="cursor-pointer hover:bg-primary/5"
                                                onClick={() => navigateToEntity(ref.sourceEntityName, ref.sourceId)}
                                            >
                                                <TableCell className="text-muted-foreground text-xs">
                                                    {idx + 1}
                                                </TableCell>
                                                <TableCell className="font-medium text-sm">
                                                    {ref.presentation || ref.sourceId.slice(0, 8) + "…"}
                                                </TableCell>
                                                <TableCell>
                                                    <Badge
                                                        variant={ref.sourceEntityType === "document" ? "default" : "outline"}
                                                        className="text-xs"
                                                    >
                                                        {entityLabel}
                                                    </Badge>
                                                </TableCell>
                                                <TableCell className="text-xs text-muted-foreground font-mono">
                                                    {ref.sourceField}
                                                </TableCell>
                                                <TableCell>
                                                    <ExternalLink className="h-3.5 w-3.5 text-muted-foreground/60" />
                                                </TableCell>
                                            </TableRow>
                                        )
                                    })}
                                </TableBody>
                            </Table>
                        )}
                    </CardContent>
                </Card>
            )}
        </div>
        </ScrollArea>
    )
}
