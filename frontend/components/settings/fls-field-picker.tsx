"use client"

import { useEffect, useState } from "react"
import { Loader2 } from "lucide-react"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { api } from "@/lib/api"
import type { FieldPolicyItem } from "@/types/security"

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

interface FlsFieldMatrixProps {
  entityName: string
  /** Read + Write policies for this entity (0-2 items) */
  readPolicy?: FieldPolicyItem
  writePolicy?: FieldPolicyItem
  onChange: (read: FieldPolicyItem | undefined, write: FieldPolicyItem | undefined) => void
}

// ── DSL Helpers ─────────────────────────────────────────────────────

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
  if (checkedFields.size > allFields.length / 2) {
    return ["*", ...unchecked.map((f) => `-${f}`)]
  }
  return [...checkedFields]
}

function isChecked(dsl: string[] | undefined, fieldName: string): boolean {
  if (!dsl || dsl.length === 0) return true // no policy = all allowed
  const { allAllowed, excluded, included } = parseDsl(dsl)
  if (allAllowed) return !excluded.has(fieldName)
  return included.has(fieldName)
}

function isCheckedTp(policy: FieldPolicyItem | undefined, partName: string, colName: string): boolean {
  const partDsl = policy?.tableParts?.[partName] ?? ["*"]
  return isChecked(partDsl, colName)
}

// ── Component ────────────────────────────────────────────────────────

export function FlsFieldMatrix({
  entityName,
  readPolicy,
  writePolicy,
  onChange,
}: FlsFieldMatrixProps) {
  const [meta, setMeta] = useState<EntityMeta | null>(null)
  const [loadState, setLoadState] = useState<"idle" | "loading" | "done">(entityName ? "loading" : "idle")

  useEffect(() => {
    if (!entityName) return
    let cancelled = false
    api.meta.getEntity(entityName)
      .then((data) => { if (!cancelled) setMeta(data as EntityMeta) })
      .catch(() => { if (!cancelled) setMeta(null) })
      .finally(() => { if (!cancelled) setLoadState("done") })
    return () => { cancelled = true }
  }, [entityName])

  const loading = loadState === "loading"

  if (loading) {
    return (
      <div className="flex items-center gap-2 py-2 text-xs text-muted-foreground">
        <Loader2 className="h-3 w-3 animate-spin" />
        Загрузка полей...
      </div>
    )
  }

  if (!meta) {
    return (
      <p className="text-xs text-muted-foreground py-2">
        Не удалось загрузить метаданные сущности
      </p>
    )
  }

  const headerFields = meta.fields.filter((f) => f.name !== "id" && f.name !== "version")
  const tablePartMeta = meta.tableParts ?? []
  const allFieldNames = headerFields.map((f) => f.name)

  const toggleHeader = (fieldName: string, action: "read" | "write") => {
    const policy = action === "read" ? readPolicy : writePolicy
    const dsl = policy?.allowedFields ?? ["*"]
    const checkedSet = new Set(allFieldNames.filter((f) => isChecked(dsl, f)))
    if (checkedSet.has(fieldName)) {
      checkedSet.delete(fieldName)
    } else {
      checkedSet.add(fieldName)
    }
    const newDsl = buildDsl(allFieldNames, checkedSet)
    const allAllowed = newDsl.length === 1 && newDsl[0] === "*"
    const tpUnchanged = !policy?.tableParts || Object.keys(policy.tableParts).length === 0
    const noRestriction = allAllowed && tpUnchanged
    const newPolicy: FieldPolicyItem | undefined = noRestriction
      ? undefined
      : {
          entityName,
          action,
          allowedFields: newDsl,
          tableParts: policy?.tableParts ?? {},
        }
    if (action === "read") onChange(newPolicy, writePolicy)
    else onChange(readPolicy, newPolicy)
  }

  const toggleTp = (partName: string, colName: string, action: "read" | "write") => {
    const policy = action === "read" ? readPolicy : writePolicy
    const part = tablePartMeta.find((tp) => tp.name === partName)
    if (!part) return
    const allCols = part.columns.filter((c) => c.name !== "id").map((c) => c.name)
    const partDsl = policy?.tableParts?.[partName] ?? ["*"]
    const checkedSet = new Set(allCols.filter((c) => isChecked(partDsl, c)))
    if (checkedSet.has(colName)) {
      checkedSet.delete(colName)
    } else {
      checkedSet.add(colName)
    }
    const newTp = {
      ...(policy?.tableParts ?? {}),
      [partName]: buildDsl(allCols, checkedSet),
    }
    // Clean up table parts that are all-allowed
    for (const [k, v] of Object.entries(newTp)) {
      if (v.length === 1 && v[0] === "*") delete newTp[k]
    }
    const headerDsl = policy?.allowedFields ?? ["*"]
    const allHeaderAllowed = headerDsl.length === 1 && headerDsl[0] === "*"
    const noRestriction = allHeaderAllowed && Object.keys(newTp).length === 0
    const newPolicy: FieldPolicyItem | undefined = noRestriction
      ? undefined
      : {
          entityName,
          action,
          allowedFields: headerDsl,
          tableParts: newTp,
        }
    if (action === "read") onChange(newPolicy, writePolicy)
    else onChange(readPolicy, newPolicy)
  }

  return (
    <div className="space-y-3">
      {/* Header fields matrix */}
      <div className="rounded border">
        <table className="w-full text-xs">
          <thead>
            <tr className="border-b bg-muted/40">
              <th className="px-3 py-1.5 text-left text-[11px] font-medium text-muted-foreground">
                Поле
              </th>
              <th className="px-3 py-1.5 text-center text-[11px] font-medium text-muted-foreground w-20">
                Просмотр
              </th>
              <th className="px-3 py-1.5 text-center text-[11px] font-medium text-muted-foreground w-24">
                Редактирование
              </th>
            </tr>
          </thead>
          <tbody>
            {headerFields.map((field) => {
              const readOk = isChecked(readPolicy?.allowedFields, field.name)
              const writeOk = isChecked(writePolicy?.allowedFields, field.name)
              return (
                <tr key={field.name} className="border-b last:border-b-0 hover:bg-muted/20">
                  <td className="px-3 py-1.5">
                    <span className={!readOk && !writeOk ? "text-muted-foreground line-through" : "text-foreground"}>
                      {field.label || field.name}
                    </span>
                    <span className="ml-1.5 text-[10px] text-muted-foreground/60 font-mono">{field.type}</span>
                  </td>
                  <td className="px-3 py-1.5 text-center">
                    <Checkbox
                      checked={readOk}
                      onCheckedChange={() => toggleHeader(field.name, "read")}
                      className="h-3.5 w-3.5"
                    />
                  </td>
                  <td className="px-3 py-1.5 text-center">
                    <Checkbox
                      checked={writeOk}
                      onCheckedChange={() => toggleHeader(field.name, "write")}
                      className="h-3.5 w-3.5"
                    />
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {/* Table parts */}
      {tablePartMeta.map((tp) => {
        const cols = tp.columns.filter((c) => c.name !== "id")
        if (cols.length === 0) return null
        return (
          <div key={tp.name} className="rounded border bg-muted/30 overflow-hidden">
            <Label className="block text-[11px] text-muted-foreground px-3 py-1.5 bg-muted/40 border-b">
              {tp.label || tp.name}
            </Label>
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b bg-muted/20">
                  <th className="px-3 py-1 text-left text-[10px] font-medium text-muted-foreground">Поле</th>
                  <th className="px-3 py-1 text-center text-[10px] font-medium text-muted-foreground w-20">Просмотр</th>
                  <th className="px-3 py-1 text-center text-[10px] font-medium text-muted-foreground w-24">Редактирование</th>
                </tr>
              </thead>
              <tbody>
                {cols.map((col) => {
                  const readOk = isCheckedTp(readPolicy, tp.name, col.name)
                  const writeOk = isCheckedTp(writePolicy, tp.name, col.name)
                  return (
                    <tr key={col.name} className="border-b last:border-b-0 hover:bg-muted/20">
                      <td className="px-3 py-1">
                        <span className={!readOk && !writeOk ? "text-muted-foreground line-through" : "text-foreground"}>
                          {col.label || col.name}
                        </span>
                      </td>
                      <td className="px-3 py-1 text-center">
                        <Checkbox
                          checked={readOk}
                          onCheckedChange={() => toggleTp(tp.name, col.name, "read")}
                          className="h-3.5 w-3.5"
                        />
                      </td>
                      <td className="px-3 py-1 text-center">
                        <Checkbox
                          checked={writeOk}
                          onCheckedChange={() => toggleTp(tp.name, col.name, "write")}
                          className="h-3.5 w-3.5"
                        />
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )
      })}
    </div>
  )
}
