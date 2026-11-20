/**
 * Copies text: the Clipboard API, else a hidden textarea and execCommand for
 * browsers or embedded views that deny clipboard permission. Call it from a
 * user gesture (click, key press), which the fallback needs.
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    // Selecting the textarea takes focus; give it back so the keyboard stays where the user was.
    const focused = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const area = document.createElement("textarea")
    area.value = text
    area.setAttribute("readonly", "")
    area.style.position = "fixed"
    area.style.opacity = "0"
    document.body.appendChild(area)
    area.select()
    try {
      return document.execCommand("copy")
    } catch {
      return false
    } finally {
      area.remove()
      focused?.focus({ preventScroll: true })
    }
  }
}
