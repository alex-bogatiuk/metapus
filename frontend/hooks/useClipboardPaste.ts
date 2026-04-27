/**
 * useClipboardPaste — React hook for clipboard paste lifecycle.
 *
 * Handles:
 * - onPaste event interception (skips input/textarea targets)
 * - TSV parsing + header detection + auto-mapping
 * - Preview dialog state management
 * - Reference resolution orchestration
 * - Large paste confirmation (>100 rows)
 */

"use client"

import { useState, useCallback, useRef } from "react"
import {
  parseTSV,
  detectHasHeader,
  autoMapColumns,
  autoMapByPosition,
  batchResolveReferences,
  buildResolvedLines,
  DOCUMENT_LINE_PASTE_COLUMNS,
  type ParsedClipboard,
  type PasteColumnDef,
  type ColumnMapping,
  type PasteResolution,
  type ResolvedPasteLine,
} from "@/lib/clipboard-paste"

// ── Types ───────────────────────────────────────────────────────────────

export interface PastePreviewState {
  /** Parsed clipboard data */
  parsed: ParsedClipboard
  /** Whether first row is header */
  hasHeader: boolean
  /** Current column mappings */
  mappings: ColumnMapping[]
  /** Available column definitions */
  columnDefs: PasteColumnDef[]
  /** Resolution results (keyed by "endpoint::lowerName") */
  resolutions: Map<string, PasteResolution>
  /** Whether reference resolution is in progress */
  resolving: boolean
  /** Data rows (excluding header if hasHeader=true) */
  dataRows: ParsedClipboard["rows"]
}

export interface UseClipboardPasteReturn {
  /** Props to spread on the paste container element */
  pasteContainerProps: {
    onPaste: (e: React.ClipboardEvent) => void
    tabIndex: number
  }
  /** Current preview state (null = no paste pending) */
  previewState: PastePreviewState | null
  /** Dismiss the preview dialog */
  closePreview: () => void
  /** Confirm paste — returns resolved lines */
  confirmPaste: () => ResolvedPasteLine[]
  /** Toggle "first row is header" */
  toggleHeader: () => void
  /** Update a single column mapping */
  updateMapping: (sourceIndex: number, targetKey: string | null) => void
  /** Re-run reference resolution (after mapping change) */
  reResolve: () => Promise<void>
  /** Update resolution for a specific cell (user picked from suggestions) */
  pickSuggestion: (endpoint: string, searchTerm: string, resolvedId: string, resolvedName: string) => void
}

// ── Constants ───────────────────────────────────────────────────────────

const LARGE_PASTE_THRESHOLD = 100

// ── Hook ────────────────────────────────────────────────────────────────

export function useClipboardPaste(
  onPasteLines: ((lines: ResolvedPasteLine[]) => void) | undefined,
  columnDefs: PasteColumnDef[] = DOCUMENT_LINE_PASTE_COLUMNS,
): UseClipboardPasteReturn {
  const [previewState, setPreviewState] = useState<PastePreviewState | null>(null)
  const resolveAbortRef = useRef<AbortController | null>(null)

  // ── Reference resolution ────────────────────────────────────────────
  const runResolve = useCallback(async (pasteState: PastePreviewState) => {
    // Abort previous resolution
    resolveAbortRef.current?.abort()
    const abort = new AbortController()
    resolveAbortRef.current = abort

    setPreviewState((prev) => prev ? { ...prev, resolving: true } : null)

    try {
      const resolutions = await batchResolveReferences(pasteState.mappings, pasteState.dataRows)
      if (abort.signal.aborted) return

      setPreviewState((prev) => prev ? { ...prev, resolutions, resolving: false } : null)
    } catch {
      if (abort.signal.aborted) return
      setPreviewState((prev) => prev ? { ...prev, resolving: false } : null)
    }
  }, [])

  // ── Paste event handler ─────────────────────────────────────────────
  const handlePaste = useCallback((e: React.ClipboardEvent) => {
    // Skip if paste is disabled (no callback) or if target is an editable element
    if (!onPasteLines) return

    const target = e.target as HTMLElement
    const tagName = target.tagName.toLowerCase()
    if (tagName === "input" || tagName === "textarea" || target.isContentEditable) {
      return // Let native paste work in form fields
    }

    const raw = e.clipboardData?.getData("text/plain")
    if (!raw?.trim()) return

    e.preventDefault()

    const parsed = parseTSV(raw)
    if (parsed.rows.length === 0 || parsed.columnCount === 0) return

    // Single cell paste — ignore (not tabular data)
    if (parsed.rows.length === 1 && parsed.columnCount === 1) return

    const hasHeader = detectHasHeader(parsed.rows, columnDefs)
    const dataRows = hasHeader ? parsed.rows.slice(1) : parsed.rows

    if (dataRows.length === 0) return

    // Large paste confirmation
    if (dataRows.length > LARGE_PASTE_THRESHOLD) {
      const confirmed = window.confirm(
        `Будет добавлено ${dataRows.length} строк из буфера обмена. Продолжить?`,
      )
      if (!confirmed) return
    }

    // Auto-map columns
    const headerRow = hasHeader ? parsed.rows[0].cells : []
    const mappings = hasHeader
      ? autoMapColumns(columnDefs, headerRow)
      : autoMapByPosition(columnDefs, parsed.columnCount)

    const pasteState: PastePreviewState = {
      parsed,
      hasHeader,
      mappings,
      columnDefs,
      resolutions: new Map(),
      resolving: false,
      dataRows,
    }

    setPreviewState(pasteState)

    // Auto-resolve references
    runResolve(pasteState)
  }, [onPasteLines, columnDefs, runResolve])

  // ── Actions ─────────────────────────────────────────────────────────
  const closePreview = useCallback(() => {
    resolveAbortRef.current?.abort()
    setPreviewState(null)
  }, [])

  const confirmPaste = useCallback((): ResolvedPasteLine[] => {
    if (!previewState) return []

    const lines = buildResolvedLines(
      previewState.dataRows,
      previewState.mappings,
      previewState.resolutions,
    )

    onPasteLines?.(lines)
    setPreviewState(null)
    return lines
  }, [previewState, onPasteLines])

  const toggleHeader = useCallback(() => {
    setPreviewState((prev) => {
      if (!prev) return null

      const newHasHeader = !prev.hasHeader
      const dataRows = newHasHeader ? prev.parsed.rows.slice(1) : prev.parsed.rows

      // Re-map columns based on new header state
      const headerRow = newHasHeader ? prev.parsed.rows[0].cells : []
      const mappings = newHasHeader
        ? autoMapColumns(prev.columnDefs, headerRow)
        : autoMapByPosition(prev.columnDefs, prev.parsed.columnCount)

      const newState: PastePreviewState = {
        ...prev,
        hasHeader: newHasHeader,
        dataRows,
        mappings,
        resolutions: new Map(), // Reset resolutions
        resolving: false,
      }

      // Trigger re-resolve
      runResolve(newState)
      return newState
    })
  }, [runResolve])

  const updateMapping = useCallback((sourceIndex: number, targetKey: string | null) => {
    setPreviewState((prev) => {
      if (!prev) return null

      let newMappings = prev.mappings.filter((m) => m.sourceIndex !== sourceIndex)

      if (targetKey) {
        // Remove any existing mapping for this target key
        newMappings = newMappings.filter((m) => m.target.key !== targetKey)

        const targetDef = prev.columnDefs.find((d) => d.key === targetKey)
        if (targetDef) {
          newMappings.push({ sourceIndex, target: targetDef })
          newMappings.sort((a, b) => a.sourceIndex - b.sourceIndex)
        }
      }

      return { ...prev, mappings: newMappings }
    })
  }, [])

  const reResolve = useCallback(async () => {
    if (!previewState) return
    await runResolve(previewState)
  }, [previewState, runResolve])

  const pickSuggestion = useCallback((endpoint: string, searchTerm: string, resolvedId: string, resolvedName: string) => {
    setPreviewState((prev) => {
      if (!prev) return null
      const key = `${endpoint}::${searchTerm.toLowerCase()}`
      const newResolutions = new Map(prev.resolutions)
      const existing = newResolutions.get(key)
      newResolutions.set(key, {
        resolved: { id: resolvedId, name: resolvedName },
        suggestions: existing?.suggestions ?? [],
        status: "resolved",
      })
      return { ...prev, resolutions: newResolutions }
    })
  }, [])

  return {
    pasteContainerProps: {
      onPaste: handlePaste,
      tabIndex: -1, // Focusable but not tabbable
    },
    previewState,
    closePreview,
    confirmPaste,
    toggleHeader,
    updateMapping,
    reResolve,
    pickSuggestion,
  }
}
