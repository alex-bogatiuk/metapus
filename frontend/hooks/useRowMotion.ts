"use client"

import { useLayoutEffect, useRef, type RefObject } from "react"

/**
 * Motion of table lines, so the eye follows what a command did:
 *  - a line that changes place glides there (FLIP: it starts where it was
 *    seen and settles in ~240 ms, critically damped, no overshoot). A move
 *    during a move starts from where the line is on screen, so repeated
 *    Ctrl+Shift+↑ never queues or jumps;
 *  - a new line slides in (globals.css row-in);
 *  - flash(keys) tints lines for a moment (lines a pick, paste or scan touched).
 * Lines are found by data-row-key. With "reduce motion" the global rule in
 * globals.css cuts transitions and animations to nothing.
 */
const SETTLE = "transform 240ms cubic-bezier(0.2, 0, 0, 1)"

function currentShift(element: HTMLElement): number {
    const transform = getComputedStyle(element).transform
    if (!transform || transform === "none") return 0
    return new DOMMatrixReadOnly(transform).m42
}

export function useRowMotion(container: RefObject<HTMLElement | null>, keys: readonly string[]): { flash: (keys: readonly string[]) => void } {
    const positions = useRef(new Map<string, number>())
    const measured = useRef(false)
    const pendingFlash = useRef<readonly string[]>([])
    const signature = keys.join("|")

    useLayoutEffect(() => {
        const root = container.current
        if (!root) return
        const rows = [...root.querySelectorAll<HTMLElement>("tr[data-row-key]")]
        const next = new Map<string, number>()
        for (const row of rows) {
            const key = row.dataset.rowKey ?? ""
            const top = row.offsetTop
            next.set(key, top)
            const before = positions.current.get(key)
            if (before === undefined) {
                // A line that was not here: it slides in (not on the first render).
                if (measured.current && row.dataset.entryLine !== "true") {
                    row.classList.remove("animate-row-in")
                    void row.offsetWidth
                    row.classList.add("animate-row-in")
                }
                continue
            }
            // Where it is seen now (its old place plus any glide in progress) to where it is laid out.
            const delta = before + currentShift(row) - top
            if (Math.abs(delta) < 1) continue
            row.style.transition = "none"
            row.style.transform = `translateY(${delta}px)`
            void row.offsetHeight
            row.style.transition = SETTLE
            row.style.transform = ""
        }
        positions.current = next
        measured.current = true
    }, [container, signature])

    // Flashes run after the lines they name are on screen.
    useLayoutEffect(() => {
        const wanted = pendingFlash.current
        if (!wanted.length) return
        pendingFlash.current = []
        const root = container.current
        for (const key of wanted) {
            const row = root?.querySelector<HTMLElement>(`tr[data-row-key="${CSS.escape(key)}"]`)
            row?.animate([{ backgroundColor: "hsl(var(--primary) / 0.22)" }, { backgroundColor: "hsl(var(--primary) / 0)" }], { duration: 900, easing: "ease-out" })
        }
    })

    return {
        flash: (wanted) => { pendingFlash.current = [...pendingFlash.current, ...wanted] },
    }
}
