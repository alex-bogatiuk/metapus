/**
 * PastePreviewDialog — Preview UI for clipboard paste into document table parts.
 *
 * Shows parsed TSV data with column mapping dropdowns, reference resolution
 * status, and first-row-is-header toggle. Integrates with useClipboardPaste hook.
 */

"use client"

import React, { useMemo } from "react"
import { Check, X, Loader2, AlertTriangle, ClipboardPaste } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { cn } from "@/lib/utils"
import type { PastePreviewState, UseClipboardPasteReturn } from "@/hooks/useClipboardPaste"

// ── Props ───────────────────────────────────────────────────────────────

interface PastePreviewDialogProps {
  state: PastePreviewState | null
  onClose: () => void
  onConfirm: () => void
  onToggleHeader: () => void
  onUpdateMapping: (sourceIndex: number, targetKey: string | null) => void
  onReResolve: () => void
  onPickSuggestion: UseClipboardPasteReturn["pickSuggestion"]
}

// ── Constants ───────────────────────────────────────────────────────────

const MAX_PREVIEW_ROWS = 50

// ── Component ───────────────────────────────────────────────────────────

export function PastePreviewDialog({
  state,
  onClose,
  onConfirm,
  onToggleHeader,
  onUpdateMapping,
  onReResolve,
  onPickSuggestion,
}: PastePreviewDialogProps) {
  const mappings = useMemo(() => state?.mappings ?? [], [state?.mappings])
  const dataRows = useMemo(() => state?.dataRows ?? [], [state?.dataRows])
  const resolutions = useMemo(() => state?.resolutions ?? new Map(), [state?.resolutions])

  // Build column index → mapping lookup
  const mappingByIndex = useMemo(() => {
    const map = new Map<number, (typeof mappings)[number]>()
    for (const m of mappings) map.set(m.sourceIndex, m)
    return map
  }, [mappings])

  // Already-mapped target keys (to disable in other dropdowns)
  const mappedKeys = useMemo(
    () => new Set(mappings.map((m) => m.target.key)),
    [mappings],
  )

  const hasRefColumns = mappings.some((m) => m.target.type === "ref")

  // Resolution stats
  const refStats = useMemo(() => {
    let resolved = 0
    let notFound = 0
    let ambiguous = 0

    if (!hasRefColumns) return { resolved: 0, notFound: 0, ambiguous: 0 }

    const counted = new Set<string>()
    for (const mapping of mappings) {
      if (mapping.target.type !== "ref" || !mapping.target.refEndpoint) continue
      for (const row of dataRows) {
        const cellValue = row.cells[mapping.sourceIndex]?.trim()
        if (!cellValue) continue
        const key = `${mapping.target.refEndpoint}::${cellValue.toLowerCase()}`
        if (counted.has(key)) continue
        counted.add(key)
        const res = resolutions.get(key)
        if (!res) continue
        if (res.status === "resolved") resolved++
        else if (res.status === "not_found") notFound++
        else if (res.status === "ambiguous") ambiguous++
      }
    }
    return { resolved, notFound, ambiguous }
  }, [mappings, dataRows, resolutions, hasRefColumns])

  if (!state) return null

  const { parsed, hasHeader, columnDefs, resolving } = state
  const totalDataRows = dataRows.length
  const previewRows = dataRows.slice(0, MAX_PREVIEW_ROWS)

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="max-w-4xl max-h-[85vh] flex flex-col p-0">
        <DialogHeader className="px-6 pt-6 pb-2 shrink-0">
          <DialogTitle className="flex items-center gap-2">
            <ClipboardPaste className="h-5 w-5 text-primary" />
            Вставка из буфера обмена
          </DialogTitle>
          <DialogDescription asChild>
            <div className="text-sm text-muted-foreground flex items-center gap-3">
              <span>{totalDataRows} {pluralRows(totalDataRows)} для добавления</span>
              {hasRefColumns && !resolving && (
                <>
                  {refStats.resolved > 0 && (
                    <Badge variant="outline" className="gap-1 text-emerald-600 border-emerald-200">
                      <Check className="h-3 w-3" /> {refStats.resolved} найдено
                    </Badge>
                  )}
                  {refStats.ambiguous > 0 && (
                    <Badge variant="outline" className="gap-1 text-amber-600 border-amber-200">
                      <AlertTriangle className="h-3 w-3" /> {refStats.ambiguous} неточно
                    </Badge>
                  )}
                  {refStats.notFound > 0 && (
                    <Badge variant="outline" className="gap-1 text-destructive border-destructive/20">
                      <X className="h-3 w-3" /> {refStats.notFound} не найдено
                    </Badge>
                  )}
                </>
              )}
              {resolving && (
                <Badge variant="outline" className="gap-1 text-muted-foreground">
                  <Loader2 className="h-3 w-3 animate-spin" /> Поиск справочников…
                </Badge>
              )}
            </div>
          </DialogDescription>
        </DialogHeader>

        {/* Header toggle */}
        <div className="flex items-center gap-2 px-6 py-2 border-b shrink-0">
          <Checkbox
            id="paste-has-header"
            checked={hasHeader}
            onCheckedChange={onToggleHeader}
          />
          <Label htmlFor="paste-has-header" className="text-xs cursor-pointer">
            Первая строка — заголовок
          </Label>
        </div>

        {/* Preview table */}
        <ScrollArea className="flex-1 min-h-0">
          <div className="px-6 pb-2">
            <table className="w-full text-xs border-separate border-spacing-0">
              {/* Mapping header row */}
              <thead className="sticky top-0 z-10 bg-background">
                <tr>
                  <th className="w-8 border-b px-1 py-1.5 text-center text-muted-foreground font-medium">
                    №
                  </th>
                  {Array.from({ length: parsed.columnCount }, (_, colIdx) => (
                    <th key={colIdx} className="border-b px-1 py-1">
                      <ColumnMappingSelect
                        colIndex={colIdx}
                        currentMapping={mappingByIndex.get(colIdx) ?? null}
                        columnDefs={columnDefs}
                        mappedKeys={mappedKeys}
                        headerLabel={hasHeader ? parsed.rows[0].cells[colIdx] : undefined}
                        onUpdate={onUpdateMapping}
                      />
                    </th>
                  ))}
                </tr>
                {/* Original header row (if has header) */}
                {hasHeader && (
                  <tr className="bg-muted/30">
                    <td className="border-b px-1 py-1 text-center text-muted-foreground/50">—</td>
                    {parsed.rows[0].cells.map((cell, colIdx) => (
                      <td key={colIdx} className="border-b px-2 py-1 text-muted-foreground font-medium truncate max-w-[160px]">
                        {cell}
                      </td>
                    ))}
                  </tr>
                )}
              </thead>

              {/* Data rows */}
              <tbody>
                {previewRows.map((row, rowIdx) => (
                  <tr key={rowIdx} className="hover:bg-muted/20 transition-colors">
                    <td className="border-b px-1 py-1 text-center text-muted-foreground/70">
                      {rowIdx + 1}
                    </td>
                    {row.cells.map((cell, colIdx) => {
                      const mapping = mappingByIndex.get(colIdx)
                      const isRef = mapping?.target.type === "ref" && mapping.target.refEndpoint
                      const cacheKey = isRef
                        ? `${mapping.target.refEndpoint}::${cell.toLowerCase().trim()}`
                        : null
                      const resolution = cacheKey ? resolutions.get(cacheKey) : null

                      return (
                        <td key={colIdx} className="border-b px-2 py-1 max-w-[200px]">
                          <CellContent
                            cell={cell}
                            isRef={!!isRef}
                            resolution={resolution ?? null}
                            resolving={resolving}
                            endpoint={isRef ? mapping.target.refEndpoint! : ""}
                            onPickSuggestion={onPickSuggestion}
                          />
                        </td>
                      )
                    })}
                  </tr>
                ))}
              </tbody>
            </table>

            {totalDataRows > MAX_PREVIEW_ROWS && (
              <div className="text-center py-2 text-xs text-muted-foreground">
                …ещё {totalDataRows - MAX_PREVIEW_ROWS} {pluralRows(totalDataRows - MAX_PREVIEW_ROWS)}
              </div>
            )}
          </div>
          <ScrollBar orientation="horizontal" />
        </ScrollArea>

        {/* Footer */}
        <DialogFooter className="px-6 py-4 border-t shrink-0">
          {hasRefColumns && (
            <Button variant="outline" size="sm" onClick={onReResolve} disabled={resolving} className="mr-auto">
              {resolving ? <Loader2 className="mr-1 h-3 w-3 animate-spin" /> : null}
              Обновить поиск
            </Button>
          )}
          <Button variant="ghost" onClick={onClose}>
            Отмена
          </Button>
          <Button onClick={onConfirm} disabled={resolving || mappings.length === 0}>
            <ClipboardPaste className="mr-1.5 h-3.5 w-3.5" />
            Вставить {totalDataRows} {pluralRows(totalDataRows)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ── Column Mapping Select ───────────────────────────────────────────────

function ColumnMappingSelect({
  colIndex,
  currentMapping,
  columnDefs,
  mappedKeys,
  headerLabel,
  onUpdate,
}: {
  colIndex: number
  currentMapping: { target: { key: string; label: string } } | null
  columnDefs: PastePreviewState["columnDefs"]
  mappedKeys: Set<string>
  headerLabel?: string
  onUpdate: (sourceIndex: number, targetKey: string | null) => void
}) {
  return (
    <Select
      value={currentMapping?.target.key ?? "__none__"}
      onValueChange={(val) => onUpdate(colIndex, val === "__none__" ? null : val)}
    >
      <SelectTrigger className="h-7 text-[11px] min-w-[100px]">
        <SelectValue placeholder={headerLabel || `Колонка ${colIndex + 1}`} />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="__none__">— Пропустить —</SelectItem>
        {columnDefs.map((def) => (
          <SelectItem
            key={def.key}
            value={def.key}
            disabled={mappedKeys.has(def.key) && currentMapping?.target.key !== def.key}
          >
            {def.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

// ── Cell Content with Resolution Status ─────────────────────────────────

function CellContent({
  cell,
  isRef,
  resolution,
  resolving,
  endpoint,
  onPickSuggestion,
}: {
  cell: string
  isRef: boolean
  resolution: {
    resolved: { id: string; name: string } | null
    suggestions: { id: string; name: string }[]
    status: string
  } | null
  resolving: boolean
  endpoint: string
  onPickSuggestion: (endpoint: string, searchTerm: string, id: string, name: string) => void
}) {
  if (!cell.trim()) {
    return <span className="text-muted-foreground/30">—</span>
  }

  if (!isRef) {
    return <span className="font-mono">{cell}</span>
  }

  // Reference cell — show resolution status
  if (resolving && !resolution) {
    return (
      <span className="flex items-center gap-1 text-muted-foreground">
        <Loader2 className="h-3 w-3 animate-spin shrink-0" />
        <span className="truncate">{cell}</span>
      </span>
    )
  }

  if (!resolution || resolution.status === "not_found") {
    return (
      <span className="flex items-center gap-1 text-destructive/80">
        <X className="h-3 w-3 shrink-0" />
        <span className="truncate" title={`Не найдено: ${cell}`}>{cell}</span>
      </span>
    )
  }

  if (resolution.status === "ambiguous") {
    return (
      <div className="flex items-center gap-1">
        <AlertTriangle className="h-3 w-3 shrink-0 text-amber-500" />
        <Select
          value={resolution.resolved?.id ?? ""}
          onValueChange={(id) => {
            const item = resolution.suggestions.find((s) => s.id === id)
            if (item) onPickSuggestion(endpoint, cell, item.id, item.name)
          }}
        >
          <SelectTrigger className={cn("h-5 text-[10px] border-amber-300 bg-amber-50/50 dark:bg-amber-900/20 min-w-[80px]")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {resolution.suggestions.map((s) => (
              <SelectItem key={s.id} value={s.id} className="text-xs">
                {s.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    )
  }

  // Resolved
  return (
    <span className="flex items-center gap-1 text-emerald-700 dark:text-emerald-400">
      <Check className="h-3 w-3 shrink-0" />
      <span className="truncate" title={resolution.resolved?.name}>{resolution.resolved?.name}</span>
    </span>
  )
}

// ── Helpers ─────────────────────────────────────────────────────────────

function pluralRows(n: number): string {
  const mod10 = n % 10
  const mod100 = n % 100
  if (mod10 === 1 && mod100 !== 11) return "строка"
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 10 || mod100 >= 20)) return "строки"
  return "строк"
}
