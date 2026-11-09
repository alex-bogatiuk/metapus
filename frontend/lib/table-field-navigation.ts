/**
 * Table-cell field navigation utility.
 *
 * Advances focus/activation to the next editable field within a table row.
 * Used by:
 *  - ReferenceField (autoAdvance after selection)
 *  - DocumentLineRow (Tab from plain input to next combobox)
 *
 * Rules:
 *  - Combobox (`[role=combobox]`) → `.click()` to open its dropdown
 *  - Plain input → `.focus()`
 *  - Skips cells with no editable field (e.g. read-only amount columns)
 */

/**
 * Advance from the current element's `<td>` to the next editable field
 * in subsequent `<td>` siblings.
 *
 * @returns `true` if a next field was found and activated.
 */
export function advanceToNextField(fromElement: HTMLElement): boolean {
  const td = fromElement.closest("td")
  if (!td) return false

  let nextTd = td.nextElementSibling as HTMLElement | null
  while (nextTd) {
    const combobox = nextTd.querySelector<HTMLElement>("[role=combobox]")
    if (combobox) {
      combobox.click()
      return true
    }
    const input = nextTd.querySelector<HTMLInputElement>("input:not([type=hidden])")
    if (input) {
      input.focus()
      return true
    }
    nextTd = nextTd.nextElementSibling as HTMLElement | null
  }
  return false
}

/**
 * Activate the first editable field in a `<tr>` element.
 * Combobox → click, input → focus.
 */
export function activateFirstField(tr: Element | null): void {
  if (!tr) return
  const combobox = tr.querySelector<HTMLElement>("[role=combobox]")
  if (combobox) {
    combobox.click()
    return
  }
  tr.querySelector<HTMLElement>("input")?.focus()
}

/**
 * Advance to the first **empty** editable field in a `<tr>`.
 *
 * Used after cascading auto-fill (e.g. selecting a product fills unit + VAT rate).
 * Skips comboboxes that already have a value (`data-has-value="true"`),
 * and inputs that already have non-empty values.
 *
 * @returns `true` if an empty field was found and activated.
 */
export function advanceToFirstEmptyField(row: HTMLElement): boolean {
  const tds = row.querySelectorAll<HTMLElement>("td")
  for (const td of tds) {
    // Check comboboxes (reference fields)
    const combobox = td.querySelector<HTMLElement>("[role=combobox]")
    if (combobox && !combobox.dataset.hasValue) {
      combobox.click()
      return true
    }
    // Check plain inputs (quantity, price, etc.)
    const input = td.querySelector<HTMLInputElement>("input:not([type=hidden])")
    if (input && !input.value) {
      input.focus()
      return true
    }
  }
  return false
}
