// hooks/useDocumentLinesExport.ts
//
// Composite hook for exporting document table part lines to Excel.
// Combines: useTablePartExport (server-side XLSX) + useShortcut (Ctrl+Shift+E)
// + buildLinesExportRows (FormLine → flat rows).
//
// Usage: one call per document page, replacing ~50 lines of boilerplate.

"use client"

import { useCallback } from "react"
import { useTablePartExport, type TablePartExportColumn } from "@/hooks/useTablePartExport"
import { useShortcut } from "@/hooks/useShortcut"
import {
  type FormLine,
  GOODS_LINE_EXPORT_COLUMNS,
  buildLinesExportRows,
} from "@/lib/document-form"

// ── Types ───────────────────────────────────────────────────────────────

export interface UseDocumentLinesExportOptions {
  /** Lines from form state */
  lines: FormLine[]
  /** Human-readable document title (e.g. "Поступление GR-0137 от 28.04.2026") */
  documentTitle: string
  /** Table part title (e.g. "Товары") — becomes the XLSX sheet name */
  tablePartTitle: string
  /** Currency decimal places for amount scaling */
  decimalPlaces: number
  /** Whether prices include VAT */
  amountIncludesVat: boolean
  /** Include amount/vatAmount columns (true for edit pages with server data) */
  includeAmounts?: boolean
  /**
   * Custom columns override. If not provided, uses GOODS_LINE_EXPORT_COLUMNS
   * based on `includeAmounts` flag. Useful for non-standard table parts.
   */
  columns?: TablePartExportColumn[]
  /**
   * Custom row builder. If not provided, uses buildLinesExportRows.
   * Useful for table parts with a different structure than FormLine.
   */
  buildRows?: (lines: FormLine[]) => Record<string, unknown>[]
}

export interface UseDocumentLinesExportReturn {
  /** Trigger export (call from button onClick or programmatically) */
  handleExport: () => void
  /** True while XLSX download is in progress */
  exporting: boolean
}

// ── Hook ────────────────────────────────────────────────────────────────

export function useDocumentLinesExport(
  options: UseDocumentLinesExportOptions,
): UseDocumentLinesExportReturn {
  const { exportTablePart, exporting } = useTablePartExport()

  const handleExport = useCallback(() => {
    // Resolve columns: custom → preset based on includeAmounts flag
    const columns: TablePartExportColumn[] = options.columns
      ? [...options.columns]
      : options.includeAmounts
        ? [...GOODS_LINE_EXPORT_COLUMNS.withAmounts]
        : [...GOODS_LINE_EXPORT_COLUMNS.withoutAmounts]

    // Resolve rows: custom builder → standard builder
    const rows = options.buildRows
      ? options.buildRows(options.lines)
      : buildLinesExportRows(
          options.lines,
          options.decimalPlaces,
          options.amountIncludesVat,
          { includeAmounts: options.includeAmounts },
        )

    exportTablePart({
      title: options.tablePartTitle,
      documentTitle: options.documentTitle,
      columns,
      rows,
    })
  }, [options, exportTablePart])

  // Register Ctrl+Shift+E shortcut (auto-unregisters on unmount)
  useShortcut(
    "doc-lines.export",
    "ctrl+shift+e",
    "Экспорт табличной части в Excel",
    "editing",
    handleExport,
    { enabled: options.lines.length > 0 },
  )

  return { handleExport, exporting }
}
