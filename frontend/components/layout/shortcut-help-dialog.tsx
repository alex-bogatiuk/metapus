"use client"

// components/layout/shortcut-help-dialog.tsx
//
// Keyboard shortcuts reference dialog (Ctrl+/).
// SAP Fiori pattern: context-aware list of all active shortcuts, grouped by category.

import { useMemo } from "react"
import { Keyboard } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog"
import { Separator } from "@/components/ui/separator"
import {
  useShortcutStore,
  GROUP_LABELS,
  GROUP_ORDER,
  type ShortcutEntry,
  type ShortcutGroup,
} from "@/stores/useShortcutStore"
import { formatCombo } from "@/lib/keyboard-utils"

interface ShortcutHelpDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ShortcutHelpDialog({ open, onOpenChange }: ShortcutHelpDialogProps) {
  // Subscribe to version to re-render when shortcuts change
  const version = useShortcutStore((s) => s.version)
  const getAll = useShortcutStore((s) => s.getAll)

  const grouped = useMemo(() => {
    const all = getAll()
    const groups = new Map<ShortcutGroup, ShortcutEntry[]>()

    for (const entry of all) {
      // Skip the help dialog shortcut itself from the list — we show it separately in footer
      if (entry.id === "general.help") continue
      const list = groups.get(entry.group) ?? []
      list.push(entry)
      groups.set(entry.group, list)
    }

    // Sort within each group alphabetically by label
    for (const list of groups.values()) {
      list.sort((a, b) => a.label.localeCompare(b.label, "ru"))
    }

    return groups
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [version, getAll])

  const hasAnyEntries = grouped.size > 0

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg p-0 gap-0">
        <DialogHeader className="px-5 pt-4 pb-3">
          <div className="flex items-center gap-2">
            <Keyboard className="h-4 w-4 text-muted-foreground" />
            <DialogTitle className="text-sm">Горячие клавиши</DialogTitle>
          </div>
          <DialogDescription className="text-xs text-muted-foreground mt-0.5">
            Доступные сочетания клавиш на текущей странице
          </DialogDescription>
        </DialogHeader>

        <Separator />

        <div className="px-5 py-3 max-h-[60vh] overflow-auto">
          {!hasAnyEntries ? (
            <p className="text-sm text-muted-foreground text-center py-4">
              Нет активных горячих клавиш
            </p>
          ) : (
            <div className="space-y-4">
              {GROUP_ORDER.map((group) => {
                const entries = grouped.get(group)
                if (!entries || entries.length === 0) return null

                return (
                  <div key={group}>
                    <h3 className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-2">
                      {GROUP_LABELS[group]}
                    </h3>
                    <div className="space-y-0.5">
                      {entries.map((entry) => (
                        <ShortcutRow key={entry.id} entry={entry} />
                      ))}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>

        <Separator />

        {/* Footer with the help shortcut hint */}
        <div className="px-5 py-2.5 flex items-center justify-between text-[11px] text-muted-foreground">
          <span>Нажмите клавишу для выполнения действия</span>
          <span>
            <KbdCombo combo="mod+/" /> — показать эту справку
          </span>
        </div>
      </DialogContent>
    </Dialog>
  )
}

// ── Row ─────────────────────────────────────────────────────────────────

function ShortcutRow({ entry }: { entry: ShortcutEntry }) {
  return (
    <div className="flex items-center justify-between py-1.5 px-2 rounded-md hover:bg-muted/50 transition-colors">
      <span className="text-xs text-foreground">{entry.label}</span>
      <KbdCombo combo={entry.keys} />
    </div>
  )
}

// ── Kbd badge ───────────────────────────────────────────────────────────

function KbdCombo({ combo }: { combo: string }) {
  const display = formatCombo(combo)

  return (
    <kbd className="inline-flex items-center gap-0.5 rounded border border-border bg-muted px-1.5 py-0.5 text-[11px] font-mono text-muted-foreground shadow-[0_1px_0_0_hsl(var(--border))]">
      {display}
    </kbd>
  )
}
