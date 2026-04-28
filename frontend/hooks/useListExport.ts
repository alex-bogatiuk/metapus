// hooks/useListExport.ts — universal list-to-XLSX export hook.
// POST /{basePath}/export-list with visible columns + current filters.
// Downloads the resulting .xlsx blob via <a download> trick.

"use client"

import { useState, useCallback } from "react"
import { apiFetchBlob } from "@/lib/api"
import { toast } from "sonner"
import type { AdvancedFilterItem } from "@/types/common"

// ── Types ───────────────────────────────────────────────────────────────

export interface ExportColumn {
    key: string
    label: string
}

export interface ExportListParams {
    /** Visible columns in display order */
    columns: ExportColumn[]
    /** Active advanced filters */
    filters?: AdvancedFilterItem[]
    /** Sort spec, e.g. "-date" */
    orderBy?: string
    /** Include soft-deleted records */
    includeDeleted?: boolean
    /** Current search text */
    search?: string
}

export interface UseListExportOptions {
    /** API base path, e.g. "/catalog/nomenclatures" */
    basePath: string
}

export interface UseListExportReturn {
    /** Trigger export with current list state */
    exportToExcel: (params: ExportListParams) => Promise<void>
    /** True while download is in progress */
    exporting: boolean
}

// ── Hook ────────────────────────────────────────────────────────────────

export function useListExport({ basePath }: UseListExportOptions): UseListExportReturn {
    const [exporting, setExporting] = useState(false)

    const exportToExcel = useCallback(
        async (params: ExportListParams) => {
            if (exporting) return
            setExporting(true)

            try {
                const body = {
                    columns: params.columns,
                    filter: params.filters ?? [],
                    orderBy: params.orderBy ?? "",
                    includeDeleted: params.includeDeleted ?? false,
                    search: params.search ?? "",
                }

                const { blob, filename } = await apiFetchBlob(
                    `${basePath}/export-list`,
                    {
                        method: "POST",
                        body: JSON.stringify(body),
                    }
                )

                // Trigger browser download via <a> element
                const url = URL.createObjectURL(blob)
                const a = document.createElement("a")
                a.href = url
                a.download = filename
                document.body.appendChild(a)
                a.click()

                // Cleanup
                setTimeout(() => {
                    document.body.removeChild(a)
                    URL.revokeObjectURL(url)
                }, 100)

                toast.success("Экспорт завершён")
            } catch (err) {
                console.error("Export failed:", err)
                toast.error("Не удалось выполнить экспорт")
            } finally {
                setExporting(false)
            }
        },
        [basePath, exporting]
    )

    return { exportToExcel, exporting }
}
