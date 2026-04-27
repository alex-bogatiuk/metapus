import { useCallback, useEffect, useRef } from "react"
import { activateFirstField } from "@/lib/table-field-navigation"

/**
 * Wraps an `addLine` callback to auto-scroll and activate the first editable
 * field in the newly created row (opens combobox dropdown or focuses input).
 *
 * Usage:
 * ```tsx
 * const tableRef = useRef<HTMLTableElement>(null)
 * const { addLineAndFocus } = useAddLineFocus(addLine, tableRef)
 * // Use addLineAndFocus instead of addLine for the "Добавить" button
 * ```
 */
export function useAddLineFocus(
  addLine: () => void,
  tableRef: React.RefObject<HTMLTableElement | null>,
) {
  const pendingFocus = useRef(false)

  const addLineAndFocus = useCallback(() => {
    pendingFocus.current = true
    addLine()
  }, [addLine])

  // After React commits a new row to the DOM, activate its first field
  useEffect(() => {
    if (!pendingFocus.current) return

    // Double rAF: React may batch setState, so we wait two frames
    // to ensure the DOM has the new <tr>.
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        if (!pendingFocus.current) return
        pendingFocus.current = false

        const tbody = tableRef.current?.querySelector("tbody")
        if (!tbody) return

        const lastRow = tbody.lastElementChild as HTMLElement | null
        if (!lastRow) return

        // Scroll the new row into view
        lastRow.scrollIntoView({ block: "nearest", behavior: "smooth" })

        // Activate the first editable field (combobox → click, input → focus)
        activateFirstField(lastRow)
      })
    })
  })

  return { addLineAndFocus }
}
