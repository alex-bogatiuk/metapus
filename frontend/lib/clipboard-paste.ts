/**
 * Clipboard paste utilities for document table parts.
 *
 * Parses TSV data from clipboard (Excel / Google Sheets) and maps columns
 * to FormLine fields with fuzzy header matching and batched reference resolution.
 *
 * Pure utilities — no React dependencies, fully testable.
 */

import { apiFetch, buildListQS } from "@/lib/api"
import type { CursorListResponse } from "@/types/common"

// ── Types ───────────────────────────────────────────────────────────────

export interface PastedRow {
  cells: string[]
}

export interface ParsedClipboard {
  rows: PastedRow[]
  columnCount: number
}

export type PasteColumnType = "text" | "number" | "ref"

export interface PasteColumnDef {
  /** FormLine field key (e.g. "nomenclatureName", "quantity") */
  key: string
  /** Display label for mapping UI */
  label: string
  /** Data type — determines matching strategy */
  type: PasteColumnType
  /** API endpoint for reference search (only for type: "ref") */
  refEndpoint?: string
  /** FormLine field that receives the resolved UUID (only for type: "ref") */
  idField?: string
}

export interface ColumnMapping {
  /** Index of the TSV column (0-based) */
  sourceIndex: number
  /** Target column definition */
  target: PasteColumnDef
}

export interface ResolvedRef {
  id: string
  name: string
  code?: string
}

export type PasteResolutionStatus = "resolved" | "not_found" | "ambiguous" | "pending"

export interface PasteResolution {
  /** Resolved reference or null if not found */
  resolved: ResolvedRef | null
  /** Suggestions when multiple results exist */
  suggestions: ResolvedRef[]
  /** Resolution status */
  status: PasteResolutionStatus
}

/** Data for a single resolved paste line, ready to become FormLine. */
export interface ResolvedPasteLine {
  nomenclatureId: string
  nomenclatureName: string
  nomenclatureCode: string
  unitId: string
  unitName: string
  quantity: string
  unitPrice: string
  vatRateId: string
  vatRateName: string
  vatPercent: string
  discountPercent: string
}

// ── Column definitions for standard document lines ──────────────────────

export const DOCUMENT_LINE_PASTE_COLUMNS: PasteColumnDef[] = [
  { key: "nomenclatureName", label: "Товар", type: "ref", refEndpoint: "/catalog/nomenclatures", idField: "nomenclatureId" },
  { key: "unitName", label: "Ед. изм.", type: "ref", refEndpoint: "/catalog/units", idField: "unitId" },
  { key: "quantity", label: "Количество", type: "number" },
  { key: "unitPrice", label: "Цена", type: "number" },
  { key: "vatRateName", label: "Ставка НДС", type: "ref", refEndpoint: "/catalog/vat-rates", idField: "vatRateId" },
  { key: "vatPercent", label: "% НДС", type: "number" },
  { key: "discountPercent", label: "Скидка %", type: "number" },
]

/** Column header aliases for fuzzy matching (normalized lowercase). */
const COLUMN_ALIASES: Record<string, string[]> = {
  nomenclatureName: ["товар", "наименование", "номенклатура", "product", "name", "item", "название"],
  unitName: ["единица", "ед", "ед изм", "единица измерения", "unit", "uom"],
  quantity: ["количество", "кол-во", "кол", "qty", "quantity", "count"],
  unitPrice: ["цена", "price", "unit price", "стоимость", "цена за ед"],
  vatRateName: ["ставка ндс", "vat rate", "налоговая ставка"],
  vatPercent: ["ндс", "ндс%", "vat", "vat%", "% ндс", "процент ндс"],
  discountPercent: ["скидка", "скидка%", "discount", "discount%", "% скидки"],
}

// ── TSV Parser ──────────────────────────────────────────────────────────

/**
 * Parse tab-separated values from clipboard text.
 * Handles Excel/Google Sheets format: tabs, \r\n, quoted cells, doubled quotes.
 */
export function parseTSV(raw: string): ParsedClipboard {
  if (!raw || !raw.trim()) {
    return { rows: [], columnCount: 0 }
  }

  const lines = parseTSVLines(raw)

  // Remove trailing empty rows (Excel often adds them)
  while (lines.length > 0 && lines[lines.length - 1].every((c) => c === "")) {
    lines.pop()
  }

  if (lines.length === 0) {
    return { rows: [], columnCount: 0 }
  }

  const columnCount = Math.max(...lines.map((l) => l.length))
  const rows: PastedRow[] = lines.map((cells) => ({
    cells: padArray(cells, columnCount, ""),
  }))

  return { rows, columnCount }
}

/**
 * Parse TSV lines with proper RFC 4180 quote handling.
 * Delimiter is always TAB (\t). Handles \r\n and \n line endings.
 */
function parseTSVLines(text: string): string[][] {
  const result: string[][] = []
  let current: string[] = []
  let cell = ""
  let inQuotes = false
  let i = 0

  while (i < text.length) {
    const ch = text[i]

    if (inQuotes) {
      if (ch === '"') {
        // Doubled quote → literal quote
        if (i + 1 < text.length && text[i + 1] === '"') {
          cell += '"'
          i += 2
        } else {
          // End of quoted section
          inQuotes = false
          i++
        }
      } else {
        cell += ch
        i++
      }
    } else {
      if (ch === '"' && cell === "") {
        inQuotes = true
        i++
      } else if (ch === "\t") {
        current.push(cell.trim())
        cell = ""
        i++
      } else if (ch === "\r") {
        current.push(cell.trim())
        cell = ""
        result.push(current)
        current = []
        i++
        if (i < text.length && text[i] === "\n") i++
      } else if (ch === "\n") {
        current.push(cell.trim())
        cell = ""
        result.push(current)
        current = []
        i++
      } else {
        cell += ch
        i++
      }
    }
  }

  // Flush last cell / row
  if (cell !== "" || current.length > 0) {
    current.push(cell.trim())
    result.push(current)
  }

  return result
}

function padArray(arr: string[], length: number, fill: string): string[] {
  const result = [...arr]
  while (result.length < length) result.push(fill)
  return result
}

// ── Header Detection ────────────────────────────────────────────────────

/**
 * Detect whether the first row is a header row.
 * Heuristic: if any cell in the first row fuzzy-matches a known column alias,
 * OR if the first row is predominantly non-numeric while the second is numeric.
 */
export function detectHasHeader(
  rows: PastedRow[],
  columnDefs: PasteColumnDef[] = DOCUMENT_LINE_PASTE_COLUMNS,
): boolean {
  if (rows.length < 2) return false

  const firstRow = rows[0].cells
  const secondRow = rows[1].cells

  // Check if any cell in first row matches known header aliases
  const normalized = firstRow.map(normalize)
  for (const aliases of Object.values(COLUMN_ALIASES)) {
    for (const alias of aliases) {
      if (normalized.some((h) => h === normalize(alias) || h.includes(normalize(alias)))) {
        return true
      }
    }
  }

  // Heuristic: first row has more text, second row has more numbers
  const firstNumericCount = firstRow.filter((c) => c && isNumericLike(c)).length
  const secondNumericCount = secondRow.filter((c) => c && isNumericLike(c)).length
  const numericDefs = columnDefs.filter((d) => d.type === "number").length

  // If there are numeric columns defined and second row has more numbers → header likely
  if (numericDefs > 0 && secondNumericCount > firstNumericCount && firstNumericCount === 0) {
    return true
  }

  return false
}

// ── Column Auto-Mapping ─────────────────────────────────────────────────

/**
 * Auto-map TSV columns to target fields using fuzzy header matching.
 * Returns a mapping array sorted by sourceIndex.
 */
export function autoMapColumns(
  columnDefs: PasteColumnDef[],
  headers: string[],
): ColumnMapping[] {
  const scores: { sourceIndex: number; def: PasteColumnDef; score: number }[] = []

  for (let i = 0; i < headers.length; i++) {
    const header = normalize(headers[i])
    if (!header) continue

    for (const def of columnDefs) {
      const aliases = COLUMN_ALIASES[def.key] ?? [normalize(def.label)]
      let bestScore = 0

      for (const alias of aliases) {
        const na = normalize(alias)
        if (header === na) {
          bestScore = Math.max(bestScore, 3) // exact
        } else if (header.startsWith(na) || na.startsWith(header)) {
          bestScore = Math.max(bestScore, 2) // prefix
        } else if (header.includes(na) || na.includes(header)) {
          bestScore = Math.max(bestScore, 1) // contains
        }
      }

      if (bestScore > 0) {
        scores.push({ sourceIndex: i, def, score: bestScore })
      }
    }
  }

  // Greedy assignment by score descending
  scores.sort((a, b) => b.score - a.score)
  const usedIndices = new Set<number>()
  const usedKeys = new Set<string>()
  const mappings: ColumnMapping[] = []

  for (const { sourceIndex, def } of scores) {
    if (usedIndices.has(sourceIndex) || usedKeys.has(def.key)) continue
    mappings.push({ sourceIndex, target: def })
    usedIndices.add(sourceIndex)
    usedKeys.add(def.key)
  }

  return mappings.sort((a, b) => a.sourceIndex - b.sourceIndex)
}

/**
 * Positional auto-map when no header is detected.
 * Priority order: nomenclatureName → quantity → unitPrice → unitName → ...
 */
export function autoMapByPosition(
  columnDefs: PasteColumnDef[],
  columnCount: number,
): ColumnMapping[] {
  const priority = ["nomenclatureName", "quantity", "unitPrice", "unitName", "vatPercent", "discountPercent", "vatRateName"]
  const sorted = [...columnDefs].sort((a, b) => {
    const ai = priority.indexOf(a.key)
    const bi = priority.indexOf(b.key)
    return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi)
  })

  const limit = Math.min(sorted.length, columnCount)
  return sorted.slice(0, limit).map((def, i) => ({ sourceIndex: i, target: def }))
}

// ── Reference Resolution ────────────────────────────────────────────────

/**
 * Batch-resolve reference names to entity IDs via the list API.
 * Groups unique search terms per endpoint, makes parallel requests.
 *
 * @returns Map<searchTerm, PasteResolution> keyed by `${endpoint}::${normalizedName}`
 */
export async function batchResolveReferences(
  mappings: ColumnMapping[],
  dataRows: PastedRow[],
): Promise<Map<string, PasteResolution>> {
  const result = new Map<string, PasteResolution>()

  // Collect unique (endpoint, searchTerm) pairs
  const refMappings = mappings.filter((m) => m.target.type === "ref" && m.target.refEndpoint)
  const searchGroups = new Map<string, Set<string>>() // endpoint → unique search terms

  for (const mapping of refMappings) {
    const endpoint = mapping.target.refEndpoint!
    if (!searchGroups.has(endpoint)) searchGroups.set(endpoint, new Set())
    const group = searchGroups.get(endpoint)!

    for (const row of dataRows) {
      const cellValue = row.cells[mapping.sourceIndex]?.trim()
      if (cellValue) group.add(cellValue)
    }
  }

  // Resolve each group in parallel
  const resolvePromises: Promise<void>[] = []

  for (const [endpoint, terms] of searchGroups) {
    for (const term of terms) {
      const cacheKey = `${endpoint}::${term.toLowerCase()}`
      if (result.has(cacheKey)) continue

      const promise = apiFetch<CursorListResponse<{ id: string; name: string; code?: string }>>(
        `${endpoint}${buildListQS({ search: term, limit: 5 })}`,
      ).then((res) => {
        const items = res.items ?? []
        const normalizedTerm = term.toLowerCase().trim()

        // Try exact match (case-insensitive)
        const exact = items.find((it) => it.name.toLowerCase().trim() === normalizedTerm)

        if (exact) {
          result.set(cacheKey, {
            resolved: { id: exact.id, name: exact.name, code: exact.code },
            suggestions: items.map((it) => ({ id: it.id, name: it.name, code: it.code })),
            status: "resolved",
          })
        } else if (items.length === 1) {
          // Single result — auto-select as best match
          result.set(cacheKey, {
            resolved: { id: items[0].id, name: items[0].name, code: items[0].code },
            suggestions: items.map((it) => ({ id: it.id, name: it.name, code: it.code })),
            status: "resolved",
          })
        } else if (items.length > 1) {
          result.set(cacheKey, {
            resolved: { id: items[0].id, name: items[0].name, code: items[0].code },
            suggestions: items.map((it) => ({ id: it.id, name: it.name, code: it.code })),
            status: "ambiguous",
          })
        } else {
          result.set(cacheKey, { resolved: null, suggestions: [], status: "not_found" })
        }
      }).catch(() => {
        result.set(cacheKey, { resolved: null, suggestions: [], status: "not_found" })
      })

      resolvePromises.push(promise)
    }
  }

  await Promise.all(resolvePromises)
  return result
}

/**
 * Build resolved paste lines from data rows + mappings + resolved references.
 */
export function buildResolvedLines(
  dataRows: PastedRow[],
  mappings: ColumnMapping[],
  resolutions: Map<string, PasteResolution>,
): ResolvedPasteLine[] {
  return dataRows.map((row) => {
    const line: ResolvedPasteLine = {
      nomenclatureId: "", nomenclatureName: "", nomenclatureCode: "",
      unitId: "", unitName: "",
      quantity: "", unitPrice: "",
      vatRateId: "", vatRateName: "", vatPercent: "20",
      discountPercent: "0",
    }

    for (const mapping of mappings) {
      const cellValue = row.cells[mapping.sourceIndex]?.trim() ?? ""
      const def = mapping.target

      if (def.type === "ref" && def.refEndpoint && def.idField) {
        const cacheKey = `${def.refEndpoint}::${cellValue.toLowerCase()}`
        const resolution = resolutions.get(cacheKey)

        // Set the name field from cell value (fallback)
        setLineField(line, def.key, cellValue)

        if (resolution?.resolved) {
          setLineField(line, def.idField, resolution.resolved.id)
          setLineField(line, def.key, resolution.resolved.name)
          if (def.key === "nomenclatureName" && resolution.resolved.code) {
            line.nomenclatureCode = resolution.resolved.code
          }
        }
      } else if (def.type === "number") {
        setLineField(line, def.key, parseNumericCell(cellValue))
      } else {
        setLineField(line, def.key, cellValue)
      }
    }

    return line
  })
}

/** Type-safe dynamic field setter for ResolvedPasteLine. */
function setLineField(line: ResolvedPasteLine, key: string, value: string): void {
  if (key in line) {
    (line as unknown as Record<string, string>)[key] = value
  }
}

// ── Helpers ─────────────────────────────────────────────────────────────

function normalize(s: string): string {
  return s.toLowerCase().trim().replace(/[.,:;!?]+/g, "").replace(/\s+/g, " ")
}

/** Check if a string looks like a number (handles comma as decimal separator). */
export function isNumericLike(s: string): boolean {
  if (!s) return false
  const cleaned = s.replace(/[\s]/g, "").replace(",", ".")
  return !isNaN(Number(cleaned)) && cleaned !== ""
}

/** Parse a numeric string (comma → dot for decimal separator). */
export function parseNumericCell(s: string): string {
  if (!s) return ""
  return s.replace(/[\s]/g, "").replace(",", ".")
}
