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
