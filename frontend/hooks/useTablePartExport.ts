// hooks/useTablePartExport.ts — export document table part to XLSX via server-side rendering.
// Sends pre-resolved rows (human-readable data from form state) to POST /export-table-part.
// The backend renders XLSX with consistent formatting (styles, dates, numbers).

"use client"

import { useState, useCallback } from "react"
import { apiFetchBlob } from "@/lib/api"
import { toast } from "sonner"

// ── Types ───────────────────────────────────────────────────────────────

export interface TablePartExportColumn {
    key: string
    label: string
}

export interface TablePartExportParams {
    /** Title of the table part (e.g. "Товары") */
    title: string
    /** Full document title (e.g. "Поступление GR-0137 от 28.04.2026") */
    documentTitle: string
    /** Columns in display order */
    columns: TablePartExportColumn[]
    /** Pre-resolved rows — human-readable values, already scaled */
    rows: Record<string, unknown>[]
}

export interface UseTablePartExportReturn {
    /** Trigger export with current table part data */
    exportTablePart: (params: TablePartExportParams) => Promise<void>
    /** True while download is in progress */
    exporting: boolean
}

// ── Hook ────────────────────────────────────────────────────────────────

export function useTablePartExport(): UseTablePartExportReturn {
    const [exporting, setExporting] = useState(false)

    const exportTablePart = useCallback(
        async (params: TablePartExportParams) => {
            if (exporting) return
            if (params.rows.length === 0) {
                toast.info("Нет данных для экспорта")
                return
            }

            setExporting(true)

            try {
                const body = {
                    title: params.title,
                    documentTitle: params.documentTitle,
                    columns: params.columns,
                    rows: params.rows,
                }

                const { blob, filename } = await apiFetchBlob(
                    "/export-table-part",
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
                console.error("Table part export failed:", err)
                toast.error("Не удалось выполнить экспорт")
            } finally {
                setExporting(false)
            }
        },
        [exporting]
    )

    return { exportTablePart, exporting }
}
