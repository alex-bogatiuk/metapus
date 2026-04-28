// components/shared/document-lines-toolbar.tsx
//
// Universal toolbar for document table parts: Добавить + Подбор + Excel + extra actions.
// Used above every TabsContent for goods lines across all document types.

import { Plus, Search, FileSpreadsheet, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import type { ReactNode } from "react"

// ── Types ───────────────────────────────────────────────────────────────

export interface DocumentLinesToolbarProps {
  /** Callback to add a new empty line */
  onAddLine: () => void
  /** Callback to open the product picker dialog */
  onOpenPicker: () => void
  /** Callback to trigger Excel export */
  onExport: () => void
  /** True while export is in progress (shows spinner) */
  exporting: boolean
  /** Number of lines — used to disable export when empty */
  linesCount: number
  /**
   * Extra actions rendered between Подбор and Excel.
   * Use for domain-specific buttons like "Заполнить цены", "Пересчитать" etc.
   */
  extraActions?: ReactNode
}

// ── Component ───────────────────────────────────────────────────────────

export function DocumentLinesToolbar({
  onAddLine,
  onOpenPicker,
  onExport,
  exporting,
  linesCount,
  extraActions,
}: DocumentLinesToolbarProps) {
  return (
    <div className="flex items-center gap-1 p-2 bg-card/50 border-b shrink-0">
      <Button variant="outline" size="sm" onClick={onAddLine}>
        <Plus className="mr-1 h-3 w-3" />
        Добавить
      </Button>
      <Button variant="outline" size="sm" onClick={onOpenPicker}>
        <Search className="mr-1 h-3 w-3" />
        Подбор
      </Button>
      {extraActions}
      <div className="ml-auto">
        <Button
          variant="ghost"
          size="sm"
          onClick={onExport}
          disabled={exporting || linesCount === 0}
          title="Экспорт в Excel (Ctrl+Shift+E)"
        >
          {exporting
            ? <Loader2 className="mr-1 h-3 w-3 animate-spin" />
            : <FileSpreadsheet className="mr-1 h-3 w-3" />}
          Excel
        </Button>
      </div>
    </div>
  )
}
