import { useCallback, useMemo } from "react"
import { arrayMove } from "@dnd-kit/sortable"
import { type FormLine, emptyLine, fetchVatRatePercent, linesToExistingPickerLines, mergePickedIntoLines } from "@/lib/document-form"
import { apiFetch } from "@/lib/api"
import type { NomenclatureResponse } from "@/types/catalog"
import type { PickedItem } from "@/types/picker"
import type { ResolvedPasteLine } from "@/lib/clipboard-paste"

// ── Types ───────────────────────────────────────────────────────────────

/** Minimum form state shape required by the hook. */
interface LinesFormState {
  lines: FormLine[]
  nextKey: number
}

/** Update function from useFormDraft — accepts partial or functional updater. */
type UpdateFn<T> = (partialOrFn: Partial<T> | ((prev: T) => Partial<T>)) => void

interface UseDocumentLineActionsOptions {
  /**
   * When true, editing a line resets its server-computed amount/vatAmount
   * to `undefined`, forcing local recalculation.
   * Use `true` on `[id]` (edit) pages, `false` on `new` pages.
   */
  resetAmountsOnEdit?: boolean
}

// ── Hook ────────────────────────────────────────────────────────────────

/**
 * Generic hook for document table-part (lines) operations.
 *
 * Encapsulates all line manipulation callbacks:
 *   addLine, handlePick, handleUpdateField, handleUpdateRef,
 *   handleUpdateVatRate, handleRemoveLine, existingPickerLines.
 *
 * Eliminates ~50 lines of identical boilerplate per document form page.
 *
 * Usage:
 * ```tsx
 * const lineActions = useDocumentLineActions(update, markDirty, { resetAmountsOnEdit: true })
 * // lineActions.addLine, lineActions.handleUpdateField, etc.
 * ```
 */
export function useDocumentLineActions<T extends LinesFormState>(
  update: UpdateFn<T>,
  markDirty: () => void,
  options?: UseDocumentLineActionsOptions,
) {
  const resetAmounts = options?.resetAmountsOnEdit ?? false

  // Memoize amount reset patch to keep useCallback deps stable
  const amountReset = useMemo(
    () => (resetAmounts ? { amount: undefined, vatAmount: undefined } : {}),
    [resetAmounts],
  )

  const addLine = useCallback(() => {
    update((prev) => ({
      lines: [...prev.lines, emptyLine(prev.nextKey)],
      nextKey: prev.nextKey + 1,
    } as Partial<T>))
    markDirty()
  }, [update, markDirty])

  const handlePick = useCallback((items: PickedItem[], existingLines: FormLine[]) => {
    const knownIds = new Set(
      linesToExistingPickerLines(existingLines).map((l) => l.productId),
    )
    update((prev) => mergePickedIntoLines(prev.lines, items, prev.nextKey, knownIds) as Partial<T>)
    markDirty()
  }, [update, markDirty])

  const handleUpdateField = useCallback((key: number, field: keyof FormLine, value: string) => {
    const editableFields: (keyof FormLine)[] = ["quantity", "unitPrice", "vatPercent", "discountPercent"]
    update((prev) => ({
      lines: prev.lines.map((l) => {
        if (l._key !== key) return l
        const updated = { ...l, [field]: value }
        if (resetAmounts && editableFields.includes(field)) {
          updated.amount = undefined
          updated.vatAmount = undefined
        }
        return updated
      }),
    } as Partial<T>))
    markDirty()
  }, [update, markDirty, resetAmounts])

  const handleUpdateRef = useCallback((key: number, patch: Partial<FormLine>) => {
    update((prev) => ({
      lines: prev.lines.map((l) =>
        l._key === key ? { ...l, ...patch, ...amountReset } : l,
      ),
    } as Partial<T>))
    markDirty()
  }, [update, markDirty, amountReset])

  /**
   * Cascading product selection.
   *
   * 1. Immediately saves productId + productName
   * 2. Async-fetches the nomenclature card
   * 3. Cascade-fills: unitId, unitName, vatRateId, vatRateName, vatPercent
   *    from product defaults (baseUnitId, defaultVatRateId)
   *
   * Returns a Promise that resolves after all cascading is complete,
   * so the caller can trigger smart-advance to the first empty field.
   */
  const handleProductSelect = useCallback(async (key: number, id: string, name: string): Promise<void> => {
    // 1. Save product immediately
    update((prev) => ({
      lines: prev.lines.map((l) =>
        l._key === key ? { ...l, productId: id, productName: name, ...amountReset } : l,
      ),
    } as Partial<T>))
    markDirty()

    if (!id) return

    // 2. Fetch nomenclature for cascade defaults
    try {
      const product = await apiFetch<NomenclatureResponse>(`/catalog/nomenclatures/${id}`)
      const patch: Partial<FormLine> = {}

      if (product.baseUnitId && product.baseUnit) {
        patch.unitId = product.baseUnitId
        patch.unitName = product.baseUnit.name
      }
      if (product.defaultVatRateId && product.defaultVatRate) {
        patch.vatRateId = product.defaultVatRateId
        patch.vatRateName = product.defaultVatRate.name
      }

      // 3. Apply cascade patch if any defaults were found
      if (Object.keys(patch).length > 0) {
        update((prev) => ({
          lines: prev.lines.map((l) =>
            l._key === key ? { ...l, ...patch, ...amountReset } : l,
          ),
        } as Partial<T>))
      }

      // 4. Resolve vatPercent from vatRateId
      if (patch.vatRateId) {
        const pct = await fetchVatRatePercent(patch.vatRateId)
        update((prev) => ({
          lines: prev.lines.map((l) =>
            l._key === key ? { ...l, vatPercent: pct, ...amountReset } : l,
          ),
        } as Partial<T>))
      }
    } catch {
      // Cascade fill is best-effort — if fetch fails, user fills manually
    }
  }, [update, markDirty, amountReset])

  const handleUpdateVatRate = useCallback((key: number, id: string, name: string) => {
    update((prev) => ({
      lines: prev.lines.map((l) =>
        l._key === key
          ? { ...l, vatRateId: id, vatRateName: name, ...amountReset }
          : l,
      ),
    } as Partial<T>))
    markDirty()
    if (id) {
      fetchVatRatePercent(id).then((pct) => {
        update((prev) => ({
          lines: prev.lines.map((l) =>
            l._key === key
              ? { ...l, vatPercent: pct, ...amountReset }
              : l,
          ),
        } as Partial<T>))
      })
    }
  }, [update, markDirty, amountReset])

  const handleRemoveLine = useCallback((key: number) => {
    update((prev) => ({
      lines: prev.lines.filter((l) => l._key !== key),
    } as Partial<T>))
    markDirty()
  }, [update, markDirty])

  /** Reorder lines via drag-and-drop (called from SortableDocumentLines). */
  const handleReorderLines = useCallback((oldIndex: number, newIndex: number) => {
    update((prev) => ({
      lines: arrayMove(prev.lines, oldIndex, newIndex),
    } as Partial<T>))
    markDirty()
  }, [update, markDirty])

  /** Move line at index up by 1 (keyboard Alt+↑). */
  const handleMoveLineUp = useCallback((index: number) => {
    if (index <= 0) return
    update((prev) => ({
      lines: arrayMove(prev.lines, index, index - 1),
    } as Partial<T>))
    markDirty()
  }, [update, markDirty])

  /** Move line at index down by 1 (keyboard Alt+↓). */
  const handleMoveLineDown = useCallback((index: number, totalLines: number) => {
    if (index >= totalLines - 1) return
    update((prev) => ({
      lines: arrayMove(prev.lines, index, index + 1),
    } as Partial<T>))
    markDirty()
  }, [update, markDirty])

  /** Bulk-add resolved paste lines from clipboard (Excel / Google Sheets). */
  const handlePasteLines = useCallback((pastedLines: ResolvedPasteLine[]) => {
    if (pastedLines.length === 0) return
    update((prev) => {
      let key = prev.nextKey
      const newLines: FormLine[] = pastedLines.map((p) => ({
        ...emptyLine(key++),
        productId: p.productId,
        productName: p.productName,
        productCode: p.productCode,
        unitId: p.unitId,
        unitName: p.unitName,
        quantity: p.quantity,
        unitPrice: p.unitPrice,
        vatRateId: p.vatRateId,
        vatRateName: p.vatRateName,
        vatPercent: p.vatPercent || "20",
        discountPercent: p.discountPercent || "0",
      }))
      return {
        lines: [...prev.lines, ...newLines],
        nextKey: key,
      } as Partial<T>
    })
    markDirty()
  }, [update, markDirty])

  return {
    addLine,
    handlePick,
    handleUpdateField,
    handleUpdateRef,
    handleProductSelect,
    handleUpdateVatRate,
    handleRemoveLine,
    handleReorderLines,
    handleMoveLineUp,
    handleMoveLineDown,
    handlePasteLines,
  }
}

/**
 * Convenience: compute existingPickerLines from current form lines.
 * Separated from useDocumentLineActions to keep memo deps clean.
 */
export function useExistingPickerLines(lines: FormLine[]) {
  return useMemo(() => linesToExistingPickerLines(lines), [lines])
}
