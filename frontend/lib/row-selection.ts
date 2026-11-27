/**
 * Row selection of a grid by clicks, as in 1C and spreadsheets:
 *  - a click makes the row current and the only selected one;
 *  - Shift+click selects the rows from the anchor (the row clicked last
 *    without Shift) to the clicked one, which becomes current;
 *  - Cmd/Ctrl+click adds or removes the clicked row and makes it current.
 */
export interface RowSelection {
    selected: readonly string[]
    current: string | null
    anchor: string | null
}

export const EMPTY_ROW_SELECTION: RowSelection = { selected: [], current: null, anchor: null }

export function clickRow(state: RowSelection, keys: readonly string[], key: string, modifiers: { shift: boolean; toggle: boolean }): RowSelection {
    if (modifiers.shift) {
        const anchor = state.anchor ?? state.current ?? key
        const from = keys.indexOf(anchor)
        const to = keys.indexOf(key)
        if (from === -1 || to === -1) return { selected: [key], current: key, anchor: key }
        return { selected: keys.slice(Math.min(from, to), Math.max(from, to) + 1), current: key, anchor }
    }
    if (modifiers.toggle) {
        const selected = state.selected.includes(key) ? state.selected.filter((k) => k !== key) : [...state.selected, key]
        return { selected, current: key, anchor: key }
    }
    return { selected: [key], current: key, anchor: key }
}

/** Drops rows that no longer exist (removed lines). */
export function keepExisting(state: RowSelection, keys: readonly string[]): RowSelection {
    const present = new Set(keys)
    const selected = state.selected.filter((k) => present.has(k))
    const current = state.current && present.has(state.current) ? state.current : null
    const anchor = state.anchor && present.has(state.anchor) ? state.anchor : null
    return selected.length === state.selected.length && current === state.current && anchor === state.anchor ? state : { selected, current, anchor }
}

/** The rows totals cover: the selection, when it holds the current row and more than one row. */
export function summaryRows(state: RowSelection): readonly string[] | null {
    if (!state.current || state.selected.length < 2 || !state.selected.includes(state.current)) return null
    return state.selected
}

/** Keys that move the current row of a grid. */
export const ROW_MOVE_KEYS = new Set(["ArrowUp", "ArrowDown", "PageUp", "PageDown", "Home", "End"])

/**
 * The row index a key moves the current row to, clamped to the rows; null for
 * a key that does not move rows or a grid without rows. With no current row
 * (index -1) a step down starts at the first row and a step up at the last.
 * A pageSize function is called only for PageUp/PageDown, so a caller can
 * measure the viewport lazily instead of on every key.
 */
export function rowMoveTarget(key: string, index: number, length: number, pageSize: number | (() => number)): number | null {
    if (length === 0 || !ROW_MOVE_KEYS.has(key)) return null
    const page = () => Math.max(1, typeof pageSize === "function" ? pageSize() : pageSize)
    const clamp = (value: number) => Math.min(length - 1, Math.max(0, value))
    switch (key) {
        case "Home": return 0
        case "End": return length - 1
        case "ArrowDown": return index === -1 ? 0 : clamp(index + 1)
        case "ArrowUp": return index === -1 ? length - 1 : clamp(index - 1)
        case "PageDown": return index === -1 ? clamp(page() - 1) : clamp(index + page())
        default: return index === -1 ? 0 : clamp(index - page())
    }
}

/** The keys from the anchor to the target, inclusive, in grid order. */
export function rowRange(keys: readonly string[], anchor: string, target: string): string[] {
    const from = keys.indexOf(anchor)
    const to = keys.indexOf(target)
    if (from === -1 || to === -1) return [target]
    return keys.slice(Math.min(from, to), Math.max(from, to) + 1)
}
