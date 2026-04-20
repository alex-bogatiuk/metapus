"use client"

/**
 * AutoList — metadata-driven list page generator.
 *
 * Thin wrapper over CatalogListPage that resolves columns and fetcher
 * from UIRegistry / backend metadata. Used as fallback when an entity
 * has no custom listComponent registered.
 *
 * Features (inherited from CatalogListPage):
 *   - DataTable with checkboxes, column resize, compact mode
 *   - Infinite scroll (ScrollSentinel)
 *   - FilterSidebar (metadata-driven)
 *   - Column Chooser (reorder + show/hide)
 *   - Enum Badge rendering via enumEntity column marker
 */

import React, { useMemo, useCallback, useEffect, useState } from "react"
import { apiFetch } from "@/lib/api"
import { buildColumnsFromFields, formatCellValue, type MetadataField } from "@/lib/metadata-columns"
import { entityRegistry, type AutoListColumn } from "@/lib/entity-registry"
import { registerFromMetadata } from "@/lib/entity-registry-defaults"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { useEnumFormatter } from "@/hooks/useEntityFiltersMeta"
import { CatalogListPage } from "@/components/shared/catalog-list-page"
import type { Column } from "@/components/shared/data-table"
import type { CursorListParams, CursorListResponse } from "@/types/common"
import { buildListQS } from "@/lib/api"

/** Dynamic row type: always has `id`, all other fields are unknown. */
type DynamicRow = Record<string, unknown> & { id: string }

interface AutoListProps {
    /** PascalCase entity name (e.g. "Vehicle") */
    entityName: string
    /** Entity type for path construction */
    entityType: "catalog" | "document"
    /** Route prefix for API and navigation */
    routePrefix: string
}

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
        rawColumns.map((col): Column<Record<string, unknown>> => {
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
                    render: (item) => {
                        const raw = String(item[col.key] ?? "")
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
                render: (item) => {
                    const value = item[col.key]
                    return React.createElement("span", {
                        className: "text-xs",
                    }, formatCellValue(value, col.type ?? "string"))
                },
            }
        }),
    [rawColumns, formatters])
}

export default function AutoList({ entityName, entityType, routePrefix }: AutoListProps) {
    // Ensure entity registry is populated from metadata store (idempotent)
    registerFromMetadata()

    // Resolve UIRegistry entry (contains column overlays, entityKey, etc.)
    const regEntry = entityRegistry.getByRoute(routePrefix)
    const entityKey = regEntry?.entityKey ?? routePrefix

    // Metadata-generated columns (fallback when no overlay in registry)
    const [metaColumns, setMetaColumns] = useState<AutoListColumn[]>([])

    useEffect(() => {
        if (regEntry?.listColumns?.length) return
        let cancelled = false
        async function loadMeta() {
            try {
                const meta = await apiFetch<{ fields: MetadataField[] }>(`/meta/${entityName}`)
                if (cancelled) return
                const cols = buildColumnsFromFields(meta.fields, 8)
                setMetaColumns(cols.map(c => ({ key: c.key, label: c.label, type: c.type, sortable: true })))
            } catch { /* fallback columns */ }
        }
        loadMeta()
        return () => { cancelled = true }
    }, [entityName, regEntry])

    // Choose column source: registry overlay → metadata → fallback
    const rawColumns = useMemo<AutoListColumn[]>(() => {
        if (regEntry?.listColumns?.length) return regEntry.listColumns
        if (metaColumns.length) return metaColumns
        return [
            { key: "code", label: "Код", sortable: true, width: 120 },
            { key: "name", label: "Наименование", sortable: true },
        ]
    }, [regEntry, metaColumns])

    // Convert to DataTable Column[] with enum resolution
    const columns = useResolvedColumns(rawColumns)

    // Build generic fetcher from entityType + routePrefix
    const fetcher = useCallback(
        (params?: CursorListParams) => {
            const basePath = entityType === "catalog"
                ? `/catalog/${routePrefix}`
                : `/document/${routePrefix}`
            return apiFetch<CursorListResponse<DynamicRow>>(`${basePath}${buildListQS(params)}`)
        },
        [entityType, routePrefix],
    )

    // Resolve navigation paths
    const pathPrefix = entityType === "catalog" ? "catalogs" : "documents"
    const metaLabel = useMetadataStore(s => s.getLabel(entityKey, "plural"))
    const title = metaLabel || entityName

    return (
        <CatalogListPage
            config={{
                title,
                entityKey,
                createHref: `/${pathPrefix}/${routePrefix}/new`,
                editHref: (item) => `/${pathPrefix}/${routePrefix}/${item.id}`,
                columns,
                allColumns: columns,
                defaultVisibleKeys: regEntry?.defaultVisibleKeys ?? columns.map(c => c.key),
                defaultFilterKeys: regEntry?.defaultFilterKeys,
                fetcher,
                limit: 100,
            }}
        />
    )
}
