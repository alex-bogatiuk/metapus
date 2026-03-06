"use client"

import React, { useState } from "react"
import { ChevronDown, ChevronRight, Loader2 } from "lucide-react"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { cn } from "@/lib/utils"

// ── Types ───────────────────────────────────────────────────────────────

export interface TableSection {
  /** Section title (e.g. "Товары") */
  title: string
  /** Column definitions for the table */
  columns: { key: string; label: string; align?: "left" | "right" | "center" }[]
  /** Rows of data — each row is a record keyed by column key */
  rows: Record<string, React.ReactNode>[]
  /** Whether this section is open by default. Default = true. */
  defaultOpen?: boolean
}

export interface DocumentDetailsPanelProps {
  /** Document title line (e.g. "Приходная накладная №00001 от 06.02.2026") */
  title: string
  /** Key-value header fields shown above table sections */
  headerFields?: { label: string; value: React.ReactNode }[]
  /** Collapsible table sections (tabular parts) */
  sections?: TableSection[]
  /** Loading state */
  loading?: boolean
}

// ── Component ───────────────────────────────────────────────────────────

export function DocumentDetailsPanel({
  title,
  headerFields,
  sections,
  loading,
}: DocumentDetailsPanelProps) {
  if (loading) {
    return (
      <div className="flex items-center justify-center py-8 text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        <span className="text-xs">Загрузка…</span>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      {/* Document title */}
      <div className="text-xs font-medium text-foreground leading-snug">
        {title}
      </div>

      {/* Header fields */}
      {headerFields && headerFields.length > 0 && (
        <div className="flex flex-col gap-1.5">
          {headerFields.map((f) => (
            <div key={f.label} className="flex flex-col gap-0.5">
              <span className="text-[10px] text-muted-foreground">{f.label}</span>
              <span className="text-xs text-foreground">{f.value || "—"}</span>
            </div>
          ))}
        </div>
      )}

      {/* Collapsible table sections */}
      {sections && sections.map((section) => (
        <CollapsibleSection key={section.title} section={section} />
      ))}
    </div>
  )
}

// ── Collapsible Section ─────────────────────────────────────────────────

function CollapsibleSection({ section }: { section: TableSection }) {
  const [open, setOpen] = useState(section.defaultOpen ?? true)

  const alignClass = (align?: "left" | "right" | "center") => {
    if (align === "right") return "text-right"
    if (align === "center") return "text-center"
    return "text-left"
  }

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center gap-1 rounded px-1 py-1 text-xs font-medium text-foreground hover:bg-muted/60 transition-colors select-none">
        {open ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        {section.title}
        <span className="ml-auto text-[10px] font-normal text-muted-foreground">
          {section.rows.length}
        </span>
      </CollapsibleTrigger>

      <CollapsibleContent>
        {section.rows.length === 0 ? (
          <div className="rounded border border-dashed px-2 py-2 text-center text-[10px] text-muted-foreground">
            Нет строк
          </div>
        ) : (
          <div className="overflow-auto rounded border">
            <table className="w-full text-[11px]">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="w-[28px] px-1.5 py-1 text-center text-[10px] font-medium text-muted-foreground">
                    №
                  </th>
                  {section.columns.map((col) => (
                    <th
                      key={col.key}
                      className={cn(
                        "px-1.5 py-1 text-[10px] font-medium text-muted-foreground",
                        alignClass(col.align)
                      )}
                    >
                      {col.label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {section.rows.map((row, idx) => (
                  <tr key={idx} className="border-b last:border-b-0">
                    <td className="px-1.5 py-1 text-center text-muted-foreground">
                      {idx + 1}
                    </td>
                    {section.columns.map((col) => (
                      <td
                        key={col.key}
                        className={cn("px-1.5 py-1", alignClass(col.align))}
                      >
                        {row[col.key] ?? "—"}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}
