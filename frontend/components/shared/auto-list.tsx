"use client"

import type { JsonObject } from "@/lib/json-value"

/**
 * AutoList — metadata-driven list page generator.
 *
 * Thin wrapper over CatalogListPage that resolves columns and fetcher
 * from UIRegistry / backend metadata. Used as fallback when an entity
 * has no custom listComponent registered.
 *
 * Features (inherited from CatalogListPage):
 *   - DataTable with checkboxes, column resize, compact mode
 *   - Current-row focus on click (catalogs and documents)
 *   - Infinite scroll (ScrollSentinel)
 *   - FilterSidebar (metadata-driven)
 *   - Column Chooser (metadata catalog + overlay presentation)
 *   - Enum Badge rendering via enumEntity column marker
 */

import React, { useMemo, useCallback, useEffect, useState } from "react"
import { Ban, CircleCheck, Circle } from "lucide-react"
import { api, apiFetch, readCachedMetaEntity } from "@/lib/api"
import {
    formatCellValue,
    getFieldValue,
    mergeListColumns,
    resolveDefaultVisibleKeys,
    type MetadataField,
} from "@/lib/metadata-columns"
import { entityRegistry, type AutoListColumn } from "@/lib/entity-registry"
import { registerFromMetadata } from "@/lib/entity-registry-defaults"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { useEnumFormatter } from "@/hooks/useEntityFiltersMeta"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import { DataTableSkeleton } from "@/components/shared/data-table-skeleton"
import { createTabStateKey } from "@/stores/useTabStateStore"
import type { Column } from "@/components/shared/data-table"
import type { CursorListParams, CursorListResponse } from "@/types/common"
import { buildListQS } from "@/lib/api"
import type { DocumentCapabilities } from "@/types/metadata"
import { buildEntityUrlByRoute } from "@/lib/entity-url"

/** Dynamic row type: always has `id`, all other fields are unknown. */
type DynamicRow = JsonObject & { id: string }
const rowsState = createTabStateKey<DynamicRow[]>("autoList.items")

interface AutoListProps {
    additionalColumns?: AutoListColumn[]
    additionalFilterFields?: readonly import("@/components/shared/filter-config-dialog").FilterFieldMeta[]
    transformParams?: (params?: CursorListParams) => CursorListParams | undefined
    /** PascalCase entity name (e.g. "Vehicle") */
    entityName: string
    /** Entity type for path construction */
    entityType: "catalog" | "document"
    /** Route prefix for API and navigation */
    routePrefix: string
    renderSelectionPanel?: (item: DynamicRow | null, refresh: () => void) => React.ReactNode
}

const EMPTY_METADATA_FIELDS: MetadataField[] = []

/**
 * Converts AutoListColumn[] to Column<Record<string, unknown>>[]
 * with enum label resolution via useEnumFormatter.
 */
function useResolvedColumns(
    rawColumns: AutoListColumn[],
): Column<DynamicRow>[] {
    // Collect unique enumEntity values for batch resolution
    const enumEntities = useMemo(
        () => [...new Set(rawColumns.filter(c => c.enumEntity).map(c => c.enumEntity!))],
        [rawColumns],
    )

    // Create formatters for each unique enum entity
    // (useEnumFormatter is called per entity — safe because enumEntities is stable)
    const formatEnum0 = useEnumFormatter(enumEntities[0] ?? "")
    const formatEnum1 = useEnumFormatter(enumEntities[1] ?? "")
    const formatEnum2 = useEnumFormatter(enumEntities[2] ?? "")

    const formatters = useMemo(() => {
        const map = new Map<string, (key: string, value: string) => string>()
        if (enumEntities[0]) map.set(enumEntities[0], formatEnum0)
        if (enumEntities[1]) map.set(enumEntities[1], formatEnum1)
        if (enumEntities[2]) map.set(enumEntities[2], formatEnum2)
        return map
    }, [enumEntities, formatEnum0, formatEnum1, formatEnum2])

    return useMemo(() =>
        rawColumns.map((col): Column<DynamicRow> => {
            // Priority 1: explicit render function
            if (col.render) {
                return {
                    key: col.key,
                    label: col.label,
                    sortable: col.sortable,
                    width: col.width,
                    minWidth: col.minWidth,
                    align: col.align,
                    className: col.className,
                    filterKey: col.filterKey,
                    render: col.render,
                }
            }

            // Priority 2: enumEntity → Badge with resolved label
            if (col.enumEntity) {
                const fmt = formatters.get(col.enumEntity)
                return {
                    key: col.key,
                    label: col.label,
                    sortable: col.sortable,
                    width: col.width ?? 120,
                    filterKey: col.filterKey,
                    render: (item) => {
                        const raw = String(getFieldValue(item, col.key) ?? "")
                        const label = fmt ? fmt(col.key, raw) : raw
                        return React.createElement("span", {
                            className: "inline-flex items-center rounded-md border px-2 py-0.5 text-[10px] font-semibold transition-colors border-border",
                        }, label)
                    },
                }
            }

            // Priority 3: type-based formatting
            return {
                key: col.key,
                label: col.label,
                sortable: col.sortable,
                width: col.width,
                minWidth: col.minWidth,
                align: col.align,
                className: col.className,
                filterKey: col.filterKey,
                render: (item) => {
                    const value = getFieldValue(item, col.key)
                    return React.createElement("span", {
                        className: "text-xs",
                    }, formatCellValue(value, col.type ?? "string", item))
                },
            }
        }),
    [rawColumns, formatters])
}

export default function AutoList({ entityName, entityType, routePrefix, renderSelectionPanel, additionalColumns, additionalFilterFields, transformParams }: AutoListProps) {
    // Ensure entity registry is populated from metadata store (idempotent)
    registerFromMetadata()

    // Resolve UIRegistry entry (contains column overlays, entityKey, etc.)
    const regEntry = entityRegistry.getByRoute(routePrefix)
    const entityKey = regEntry?.entityKey ?? routePrefix

    const [metadata, setMetadata] = useState<{
        entityName: string
        fields: MetadataField[]
        customFields: MetadataField[]
        failed: boolean
    } | null>(null)

    useEffect(() => {
        let cancelled = false
        api.meta.getEntity(entityName)
            .then((meta) => {
                if (cancelled) return
                setMetadata({
                    entityName,
                    fields: meta.fields ?? [],
                    customFields: meta.customFields ?? [],
                    failed: false,
                })
            })
            .catch(() => {
                if (!cancelled) {
                    setMetadata({ entityName, fields: [], customFields: [], failed: true })
                }
            })
        return () => { cancelled = true }
    }, [entityName])

    const cached = readCachedMetaEntity(entityName)
    const effectiveMetadata = (metadata?.entityName === entityName)
        ? metadata
        : (cached ? { entityName, fields: cached.fields ?? [], customFields: cached.customFields ?? [], failed: false } : null)

    const metaFields = effectiveMetadata?.fields ?? null
    const customFields = effectiveMetadata?.customFields ?? EMPTY_METADATA_FIELDS
    const metaFailed = effectiveMetadata?.failed ?? false

    const rawColumns = useMemo<AutoListColumn[]>(() => {
        if (metaFields) {
            return mergeListColumns({
                fields: metaFields,
                customFields,
                overlay: [...(regEntry?.listColumns ?? []), ...(additionalColumns ?? [])],
                entityName,
            })
        }
        if (metaFailed && regEntry?.listColumns?.length) {
            return regEntry.listColumns
        }
        if (metaFailed) {
            return [
                { key: "code", label: "Код", sortable: true, width: 120 },
                { key: "name", label: "Наименование", sortable: true },
            ]
        }
        return []
    }, [metaFields, customFields, metaFailed, regEntry, entityName, additionalColumns])

    const defaultVisibleKeys = useMemo(
        () => resolveDefaultVisibleKeys(regEntry?.defaultVisibleKeys, rawColumns, entityType),
        [regEntry?.defaultVisibleKeys, rawColumns, entityType],
    )

    // Convert to DataTable Column[] with enum resolution
    const columns = useResolvedColumns(rawColumns)

    // Build generic fetcher from entityType + routePrefix
    const fetcher = useCallback(
        (params?: CursorListParams) => {
            const basePath = entityType === "catalog"
                ? `/catalog/${routePrefix}`
                : `/document/${routePrefix}`
            return apiFetch<CursorListResponse<DynamicRow>>(`${basePath}${buildListQS(transformParams ? transformParams(params) : params)}`)
        },
        [entityType, routePrefix, transformParams],
    )

    // Resolve navigation paths
    const metaLabel = useMetadataStore(s => s.getLabel(entityKey, "plural"))
    const entityMetaByKey = useMetadataStore(s => s.getEntity(entityKey))
    const entityMetaByRoute = useMetadataStore(s => s.getEntityByRoute(routePrefix))
    const documentCapabilities: DocumentCapabilities | undefined = entityType === "document"
        ? (entityMetaByRoute ?? entityMetaByKey)?.capabilities
        : undefined
    const title = metaLabel || entityName

    // Status icons (documents) and deletion mark styling (documents and catalogs)
    const isDocument = entityType === "document"
    const canCreate = !isDocument || documentCapabilities?.create !== false

    const renderPrefix = useMemo(() => {
        if (!isDocument) return undefined
        return function DocumentStatusIcon(item: DynamicRow) {
            if (item.deletionMark) return React.createElement(Ban, { className: "h-4 w-4 text-destructive" })
            if (item.posted) return React.createElement(CircleCheck, { className: "h-4 w-4 text-success" })
            return React.createElement(Circle, { className: "h-4 w-4 text-muted-foreground" })
        }
    }, [isDocument])

    const rowClassName = useMemo(() => {
        return (item: DynamicRow) =>
            item.deletionMark ? "opacity-60 line-through decoration-destructive/40" : undefined
    }, [])

    const apiBasePath = isDocument ? `/document/${routePrefix}` : `/catalog/${routePrefix}`

    if (!metaFields && !metaFailed) {
        return <DataTableSkeleton showToolbar={true} />
    }

    return (
        <CatalogListPage
            config={{
                rowsState,
                title,
                entityKey,
                createHref: canCreate ? buildEntityUrlByRoute(routePrefix, entityType, undefined, "new") : undefined,
                editHref: (item) => buildEntityUrlByRoute(routePrefix, entityType, item.id),
                columns,
                allColumns: columns,
                defaultVisibleKeys,
                defaultFilterKeys: regEntry?.defaultFilterKeys,
                additionalFilterFields,
                fetcher,
                limit: 100,
                basePath: apiBasePath,
                periodField: isDocument ? "date" : undefined,
                renderPrefix,
                rowClassName,
                renderSelectionPanel,
                documentMode: isDocument ? { basePath: apiBasePath, capabilities: documentCapabilities } : undefined,
            }}
        />
    )
}
