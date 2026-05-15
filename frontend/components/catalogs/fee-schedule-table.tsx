"use client"

/**
 * FeeScheduleTable — reusable component for managing fee schedule entries.
 *
 * Used in two contexts:
 *   1. Merchant detail page (merchant-scoped overrides)
 *   2. System settings (global defaults)
 *
 * Design:
 *   - SAP-style «Information Register» pattern (1C: Регистр сведений)
 *   - Compact editable grid with inline editing via Dialog
 *   - Token column shows resolved name via reference resolver
 *   - Numbers right-aligned, tabular-nums (ERP convention)
 *   - Direction column uses semantic badges
 *
 * @see https://help.sap.com/docs/SAP_S4HANA — Pricing Conditions pattern
 */

import { useState, useEffect, useCallback, useMemo } from "react"
import { api } from "@/lib/api"
import type {
  FeeScheduleResponse,
  FeeScheduleUpsertRequest,
  FeeDirection,
} from "@/types/fee-schedule"
import { FEE_DIRECTIONS, formatBasisPoints } from "@/types/fee-schedule"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import {
  Plus,
  Trash2,
  RefreshCw,
  Pencil,
  ReceiptText,
  Info,
} from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"

// ── Props ──────────────────────────────────────────────────────────────────

interface FeeScheduleTableProps {
  /** Merchant ID for merchant-scoped mode. null = global defaults. */
  merchantId: string | null
  /** true when the parent entity hasn't been saved yet (hide until saved) */
  isNew?: boolean
  /** Optional: resolved token names map { tokenId → "USDT (TRC-20)" } */
  tokenNames?: Record<string, string>
}

// ── Direction badge colors (semantic) ──────────────────────────────────────

const directionVariant: Record<FeeDirection, "default" | "secondary" | "outline" | "destructive"> = {
  processing:  "default",
  withdrawal:  "secondary",
  payout:      "outline",
  settlement:  "outline",
  refund:      "destructive",
}

function directionLabel(d: FeeDirection): string {
  return FEE_DIRECTIONS.find((x) => x.value === d)?.label ?? d
}

// ── Main Component ─────────────────────────────────────────────────────────

export function FeeScheduleTable({ merchantId, isNew, tokenNames }: FeeScheduleTableProps) {
  const [entries, setEntries] = useState<FeeScheduleResponse[]>([])
  const [loading, setLoading] = useState(false)
  const [editEntry, setEditEntry] = useState<FeeScheduleResponse | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<FeeScheduleResponse | null>(null)

  // ── Token list from catalog (for dropdown in create/edit dialog) ──
  const [tokens, setTokens] = useState<{ id: string; name: string }[]>([])
  useEffect(() => {
    api.meta.getEntity("Token")
      .then(() => {
        // Load token list from catalog for the selector
        return api.nomenclature.list() // fallback — we'll use a simple fetch
      })
      .catch(() => {})
    // Try loading tokens directly
    import("@/lib/api").then(({ apiFetch }) => {
      apiFetch<{ items: { id: string; name: string; code: string }[] }>("/catalog/tokens")
        .then((r) => setTokens(r.items.map((t) => ({ id: t.id, name: `${t.code} — ${t.name}` }))))
        .catch(() => {})
    })
  }, [])

  const loadEntries = useCallback(async () => {
    if (isNew) return
    setLoading(true)
    try {
      const res = merchantId
        ? await api.merchantFeeSchedule.list(merchantId)
        : await api.feeSchedule.list()
      setEntries(res.items ?? [])
    } catch {
      toast.error("Не удалось загрузить тарифы")
    } finally {
      setLoading(false)
    }
  }, [merchantId, isNew])

  useEffect(() => { loadEntries() }, [loadEntries])

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      if (merchantId) {
        await api.merchantFeeSchedule.delete(merchantId, {
          tokenId: deleteTarget.tokenId,
          direction: deleteTarget.direction,
        })
      } else {
        await api.feeSchedule.delete({
          tokenId: deleteTarget.tokenId,
          direction: deleteTarget.direction,
        })
      }
      toast.success("Тариф удалён")
      setDeleteTarget(null)
      await loadEntries()
    } catch {
      toast.error("Не удалось удалить тариф")
    }
  }

  // ── Resolve token names ──
  const resolvedTokenName = useCallback(
    (tokenId: string): string => {
      if (tokenNames?.[tokenId]) return tokenNames[tokenId]
      const found = tokens.find((t) => t.id === tokenId)
      return found?.name ?? tokenId.slice(0, 8) + "…"
    },
    [tokenNames, tokens]
  )

  // ── Group entries by token for visual clarity ──
  const grouped = useMemo(() => {
    const map = new Map<string, FeeScheduleResponse[]>()
    for (const e of entries) {
      const key = e.tokenId
      if (!map.has(key)) map.set(key, [])
      map.get(key)!.push(e)
    }
    return map
  }, [entries])

  if (isNew) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-center text-muted-foreground gap-3">
        <ReceiptText className="h-8 w-8 opacity-40" />
        <p className="text-sm">Сохраните мерчанта, чтобы управлять тарифами</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm font-medium">
            {merchantId ? "Тарифы мерчанта" : "Глобальные тарифы"}
          </p>
          <p className="text-xs text-muted-foreground mt-0.5">
            {merchantId
              ? "Переопределяют глобальные настройки для этого мерчанта"
              : "Применяются ко всем мерчантам, если нет индивидуальных настроек"
            }
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" onClick={loadEntries} disabled={loading}>
            <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
          </Button>
          <Button size="sm" onClick={() => setShowCreate(true)}>
            <Plus className="mr-1.5 h-3.5 w-3.5" />
            Добавить
          </Button>
        </div>
      </div>

      <Separator />

      {/* Table */}
      {entries.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-10 text-center text-muted-foreground gap-3">
          <ReceiptText className="h-7 w-7 opacity-40" />
          <p className="text-sm">Нет настроенных тарифов</p>
          <p className="text-xs">
            {merchantId
              ? "Будут применяться глобальные настройки"
              : "Добавьте тарифы для активации комиссий"
            }
          </p>
        </div>
      ) : (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow className="text-xs">
                <TableHead className="w-[200px]">Токен</TableHead>
                <TableHead className="w-[110px]">Направление</TableHead>
                <TableHead className="w-[100px] text-right">Фикс</TableHead>
                <TableHead className="w-[80px] text-right">Процент</TableHead>
                <TableHead className="w-[100px] text-right">Мин</TableHead>
                <TableHead className="w-[100px] text-right">Макс</TableHead>
                <TableHead className="w-[60px]" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {Array.from(grouped.entries()).map(([tokenId, rows]) =>
                rows.map((entry, idx) => (
                  <TableRow
                    key={`${entry.tokenId}-${entry.direction}`}
                    className="group cursor-pointer hover:bg-muted/50"
                    onClick={() => setEditEntry(entry)}
                  >
                    {/* Token name — show only for first row of group */}
                    <TableCell className="text-sm font-medium">
                      {idx === 0 ? resolvedTokenName(tokenId) : ""}
                    </TableCell>
                    <TableCell>
                      <Badge variant={directionVariant[entry.direction]} className="text-[10px]">
                        {directionLabel(entry.direction)}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right tabular-nums text-sm">
                      {formatMinorUnits(entry.fixedFee)}
                    </TableCell>
                    <TableCell className="text-right tabular-nums text-sm">
                      {entry.percentBp > 0 ? formatBasisPoints(entry.percentBp) : "—"}
                    </TableCell>
                    <TableCell className="text-right tabular-nums text-sm text-muted-foreground">
                      {entry.minFee !== "0" ? formatMinorUnits(entry.minFee) : "—"}
                    </TableCell>
                    <TableCell className="text-right tabular-nums text-sm text-muted-foreground">
                      {entry.maxFee !== "0" ? formatMinorUnits(entry.maxFee) : "—"}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7"
                          onClick={(e) => { e.stopPropagation(); setEditEntry(entry) }}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7 text-muted-foreground hover:text-destructive"
                          onClick={(e) => { e.stopPropagation(); setDeleteTarget(entry) }}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      )}

      {/* Formula hint */}
      {entries.length > 0 && (
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-help">
                <Info className="h-3.5 w-3.5" />
                <span>Формула: clamp(Фикс + Сумма × Процент / 100, Мин, Макс)</span>
              </div>
            </TooltipTrigger>
            <TooltipContent side="bottom" className="max-w-sm text-xs">
              <p>Фиксированная часть + процент от суммы, ограниченные минимумом и максимумом.</p>
              <p className="mt-1">Мин = 0 → нет нижней границы. Макс = 0 → нет верхней границы.</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      )}

      {/* Create / Edit Dialog */}
      <FeeScheduleFormDialog
        open={showCreate || !!editEntry}
        editEntry={editEntry}
        merchantId={merchantId}
        tokens={tokens}
        onClose={() => { setShowCreate(false); setEditEntry(null) }}
        onSaved={() => { setShowCreate(false); setEditEntry(null); loadEntries() }}
      />

      {/* Delete Confirm */}
      <AlertDialog open={!!deleteTarget} onOpenChange={() => setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Удалить тариф?</AlertDialogTitle>
            <AlertDialogDescription>
              Тариф для направления «{deleteTarget && directionLabel(deleteTarget.direction)}»
              будет удалён. {merchantId
                ? "Будет применяться глобальный тариф."
                : "Комиссия по этому направлению не будет взиматься."
              }
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Отмена</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={handleDelete}
            >
              <Trash2 className="mr-1.5 h-3.5 w-3.5" />
              Удалить
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

// ── Form Dialog (Create / Edit) ───────────────────────────────────────────

function FeeScheduleFormDialog({
  open,
  editEntry,
  merchantId,
  tokens,
  onClose,
  onSaved,
}: {
  open: boolean
  editEntry: FeeScheduleResponse | null
  merchantId: string | null
  tokens: { id: string; name: string }[]
  onClose: () => void
  onSaved: () => void
}) {
  const isEdit = !!editEntry
  const [tokenId, setTokenId] = useState("")
  const [direction, setDirection] = useState<FeeDirection>("processing")
  const [fixedFee, setFixedFee] = useState("")
  const [percentBp, setPercentBp] = useState("")
  const [minFee, setMinFee] = useState("")
  const [maxFee, setMaxFee] = useState("")
  const [submitting, setSubmitting] = useState(false)

  // Reset form when opening/editing
  useEffect(() => {
    if (editEntry) {
      setTokenId(editEntry.tokenId)
      setDirection(editEntry.direction)
      setFixedFee(editEntry.fixedFee === "0" ? "" : editEntry.fixedFee)
      setPercentBp(editEntry.percentBp === 0 ? "" : String(editEntry.percentBp))
      setMinFee(editEntry.minFee === "0" ? "" : editEntry.minFee)
      setMaxFee(editEntry.maxFee === "0" ? "" : editEntry.maxFee)
    } else if (open) {
      setTokenId("")
      setDirection("processing")
      setFixedFee("")
      setPercentBp("")
      setMinFee("")
      setMaxFee("")
    }
  }, [editEntry, open])

  const handleSubmit = async () => {
    if (!tokenId) {
      toast.error("Выберите токен")
      return
    }

    const body: FeeScheduleUpsertRequest = {
      tokenId,
      direction,
      fixedFee: safeInt(fixedFee),
      percentBp: safeInt(percentBp),
      minFee: safeInt(minFee),
      maxFee: safeInt(maxFee),
    }

    // Validation
    if (body.percentBp < 0 || body.percentBp > 10000) {
      toast.error("Процент: 0–10000 б.п. (0–100%)")
      return
    }
    if (body.fixedFee === 0 && body.percentBp === 0) {
      toast.error("Укажите фиксированную комиссию или процент")
      return
    }

    setSubmitting(true)
    try {
      if (merchantId) {
        await api.merchantFeeSchedule.upsert(merchantId, body)
      } else {
        await api.feeSchedule.upsert(body)
      }
      toast.success(isEdit ? "Тариф обновлён" : "Тариф добавлен")
      onSaved()
    } catch {
      toast.error("Не удалось сохранить тариф")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Редактировать тариф" : "Добавить тариф"}</DialogTitle>
          <DialogDescription>
            {merchantId
              ? "Переопределение для этого мерчанта"
              : "Глобальный тариф по умолчанию"
            }
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {/* Token */}
          <div>
            <Label className="text-xs text-muted-foreground mb-1.5 block">
              Токен <span className="text-destructive">*</span>
            </Label>
            <Select value={tokenId} onValueChange={setTokenId} disabled={isEdit}>
              <SelectTrigger>
                <SelectValue placeholder="Выберите токен" />
              </SelectTrigger>
              <SelectContent>
                {tokens.map((t) => (
                  <SelectItem key={t.id} value={t.id}>
                    {t.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Direction */}
          <div>
            <Label className="text-xs text-muted-foreground mb-1.5 block">
              Направление <span className="text-destructive">*</span>
            </Label>
            <Select
              value={direction}
              onValueChange={(v) => setDirection(v as FeeDirection)}
              disabled={isEdit}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {FEE_DIRECTIONS.map((d) => (
                  <SelectItem key={d.value} value={d.value}>
                    <div className="flex flex-col">
                      <span>{d.label}</span>
                      <span className="text-xs text-muted-foreground">{d.description}</span>
                    </div>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Fee fields — 2×2 grid */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label className="text-xs text-muted-foreground mb-1.5 block">
                Фикс. комиссия
              </Label>
              <Input
                type="number"
                min={0}
                value={fixedFee}
                onChange={(e) => setFixedFee(e.target.value)}
                className="tabular-nums text-right"
                onKeyDown={(e) => e.key === "Enter" && handleSubmit()}
              />
              <p className="text-[10px] text-muted-foreground mt-1">Минорные единицы</p>
            </div>
            <div>
              <Label className="text-xs text-muted-foreground mb-1.5 block">
                Процент (б.п.)
              </Label>
              <Input
                type="number"
                min={0}
                max={10000}
                value={percentBp}
                onChange={(e) => setPercentBp(e.target.value)}
                className="tabular-nums text-right"
                onKeyDown={(e) => e.key === "Enter" && handleSubmit()}
              />
              <p className="text-[10px] text-muted-foreground mt-1">
                100 б.п. = 1%
                {percentBp && Number(percentBp) > 0 && (
                  <span className="ml-1 text-foreground">
                    ({formatBasisPoints(Number(percentBp))})
                  </span>
                )}
              </p>
            </div>
            <div>
              <Label className="text-xs text-muted-foreground mb-1.5 block">
                Мин. комиссия
              </Label>
              <Input
                type="number"
                min={0}
                value={minFee}
                onChange={(e) => setMinFee(e.target.value)}
                className="tabular-nums text-right"
                onKeyDown={(e) => e.key === "Enter" && handleSubmit()}
              />
              <p className="text-[10px] text-muted-foreground mt-1">0 = нет нижней границы</p>
            </div>
            <div>
              <Label className="text-xs text-muted-foreground mb-1.5 block">
                Макс. комиссия
              </Label>
              <Input
                type="number"
                min={0}
                value={maxFee}
                onChange={(e) => setMaxFee(e.target.value)}
                className="tabular-nums text-right"
                onKeyDown={(e) => e.key === "Enter" && handleSubmit()}
              />
              <p className="text-[10px] text-muted-foreground mt-1">0 = без ограничений</p>
            </div>
          </div>
        </div>

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={onClose} disabled={submitting}>
            Отмена
          </Button>
          <Button onClick={handleSubmit} disabled={submitting || !tokenId}>
            {submitting ? (
              <RefreshCw className="mr-1.5 h-3.5 w-3.5 animate-spin" />
            ) : isEdit ? (
              <Pencil className="mr-1.5 h-3.5 w-3.5" />
            ) : (
              <Plus className="mr-1.5 h-3.5 w-3.5" />
            )}
            {isEdit ? "Сохранить" : "Добавить"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ── Helpers ───────────────────────────────────────────────────────────────

/** Parse string to int, default 0 */
function safeInt(s: string): number {
  const n = parseInt(s, 10)
  return isNaN(n) ? 0 : n
}

/** Format minor units as a human-readable number with thousands separator */
function formatMinorUnits(value: string): string {
  const n = Number(value)
  if (n === 0) return "0"
  return n.toLocaleString("ru-RU")
}
