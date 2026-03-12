"use client"

import { useEffect, useState } from "react"
import { Loader2 } from "lucide-react"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Input } from "@/components/ui/input"
import { api } from "@/lib/api"

// ── Types ────────────────────────────────────────────────────────────

interface FieldMeta {
  name: string
  label?: string
  type: string
}

interface TablePartMeta {
  name: string
  label?: string
  columns: FieldMeta[]
}

interface EntityMeta {
  name: string
  label?: string
  type: string
  fields: FieldMeta[]
  tableParts?: TablePartMeta[]
}

// ── Props ────────────────────────────────────────────────────────────

interface FlsFieldPickerProps {
  entityName: string
  allowedFields: string[]
  tableParts?: Record<string, string[]>
  onChangeFields: (fields: string[]) => void
  onChangeTableParts: (parts: Record<string, string[]>) => void
}

// ── Helpers ──────────────────────────────────────────────────────────

function parseDsl(dsl: string[]): { allAllowed: boolean; excluded: Set<string>; included: Set<string> } {
  const hasWildcard = dsl.includes("*")
  const excluded = new Set<string>()
  const included = new Set<string>()

  for (const token of dsl) {
    if (token === "*") continue
    if (token.startsWith("-")) {
      excluded.add(token.slice(1))
    } else {
      included.add(token)
    }
  }

  return { allAllowed: hasWildcard, excluded, included }
}

function buildDsl(allFields: string[], checkedFields: Set<string>): string[] {
  if (checkedFields.size === allFields.length) return ["*"]
  if (checkedFields.size === 0) return []

  const unchecked = allFields.filter((f) => !checkedFields.has(f))
  // If most are checked, use wildcard + exclusions
  if (checkedFields.size > allFields.length / 2) {
    return ["*", ...unchecked.map((f) => `-${f}`)]
  }
  // Otherwise list included
  return [...checkedFields]
}

// ── Component ────────────────────────────────────────────────────────

export function FlsFieldPicker({
  entityName,
  allowedFields,
  tableParts,
  onChangeFields,
  onChangeTableParts,
}: FlsFieldPickerProps) {
  const [meta, setMeta] = useState<EntityMeta | null>(null)
  const [loading, setLoading] = useState(!!entityName)
  const [dslMode, setDslMode] = useState(false)

  useEffect(() => {
    if (!entityName) return
    let cancelled = false
    api.meta.getEntity(entityName)
      .then((data) => { if (!cancelled) setMeta(data as EntityMeta) })
      .catch(() => { if (!cancelled) setMeta(null) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [entityName])

  if (loading) {
    return (
      <div className="flex items-center gap-2 py-2 text-xs text-muted-foreground">
        <Loader2 className="h-3 w-3 animate-spin" />
        Загрузка полей...
      </div>
    )
  }

  if (!meta || dslMode) {
    // Fallback: raw DSL input
    return (
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label className="text-[11px] text-muted-foreground">Поля шапки (DSL)</Label>
          {meta && (
            <button
              className="text-[10px] text-primary hover:underline"
              onClick={() => setDslMode(false)}
            >
              Визуальный режим
            </button>
          )}
        </div>
        <Input
          value={allowedFields.join(", ")}
          onChange={(e) => {
            const fields = e.target.value.split(",").map((s) => s.trim()).filter(Boolean)
            onChangeFields(fields)
          }}
          placeholder="*, -unit_price, -total_amount"
          className="h-8 text-xs font-mono"
        />
      </div>
    )
  }

  const { allAllowed, excluded, included } = parseDsl(allowedFields)
  const headerFields = meta.fields.filter((f) => f.name !== "id" && f.name !== "version")

  const isFieldChecked = (fieldName: string): boolean => {
    if (allAllowed) return !excluded.has(fieldName)
    return included.has(fieldName)
  }

  const toggleField = (fieldName: string) => {
    const checkedSet = new Set(headerFields.filter((f) => isFieldChecked(f.name)).map((f) => f.name))
    if (checkedSet.has(fieldName)) {
      checkedSet.delete(fieldName)
    } else {
      checkedSet.add(fieldName)
    }
    onChangeFields(buildDsl(headerFields.map((f) => f.name), checkedSet))
  }

  // Table parts
  const tablePartMeta = meta.tableParts ?? []

  const isTablePartFieldChecked = (partName: string, colName: string): boolean => {
    const partDsl = tableParts?.[partName] ?? ["*"]
    const parsed = parseDsl(partDsl)
    if (parsed.allAllowed) return !parsed.excluded.has(colName)
    return parsed.included.has(colName)
  }

  const toggleTablePartField = (partName: string, colName: string) => {
    const part = tablePartMeta.find((tp) => tp.name === partName)
    if (!part) return
    const allCols = part.columns.filter((c) => c.name !== "id").map((c) => c.name)
    const checkedSet = new Set(allCols.filter((c) => isTablePartFieldChecked(partName, c)))
    if (checkedSet.has(colName)) {
      checkedSet.delete(colName)
    } else {
      checkedSet.add(colName)
    }
    onChangeTableParts({
      ...(tableParts ?? {}),
      [partName]: buildDsl(allCols, checkedSet),
    })
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <Label className="text-[11px] text-muted-foreground">Видимые поля шапки</Label>
        <button
          className="text-[10px] text-primary hover:underline"
          onClick={() => setDslMode(true)}
        >
          Режим DSL
        </button>
      </div>

      {/* Header fields */}
      <div className="grid grid-cols-2 gap-x-4 gap-y-1.5">
        {headerFields.map((field) => (
          <label
            key={field.name}
            className="flex items-center gap-2 text-xs cursor-pointer hover:text-foreground transition-colors"
          >
            <Checkbox
              checked={isFieldChecked(field.name)}
              onCheckedChange={() => toggleField(field.name)}
              className="h-3.5 w-3.5"
            />
            <span className={isFieldChecked(field.name) ? "text-foreground" : "text-muted-foreground line-through"}>
              {field.label || field.name}
            </span>
          </label>
        ))}
      </div>

      {/* Table parts */}
      {tablePartMeta.map((tp) => {
        const cols = tp.columns.filter((c) => c.name !== "id")
        if (cols.length === 0) return null
        return (
          <div key={tp.name} className="rounded border bg-muted/30 p-2 space-y-2">
            <Label className="text-[11px] text-muted-foreground">
              {tp.label || tp.name}
            </Label>
            <div className="grid grid-cols-2 gap-x-4 gap-y-1.5">
              {cols.map((col) => (
                <label
                  key={col.name}
                  className="flex items-center gap-2 text-xs cursor-pointer hover:text-foreground transition-colors"
                >
                  <Checkbox
                    checked={isTablePartFieldChecked(tp.name, col.name)}
                    onCheckedChange={() => toggleTablePartField(tp.name, col.name)}
                    className="h-3.5 w-3.5"
                  />
                  <span className={isTablePartFieldChecked(tp.name, col.name) ? "text-foreground" : "text-muted-foreground line-through"}>
                    {col.label || col.name}
                  </span>
                </label>
              ))}
            </div>
          </div>
        )
      })}
    </div>
  )
}
