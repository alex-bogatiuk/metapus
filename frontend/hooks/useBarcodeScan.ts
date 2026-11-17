"use client"

import { useEffect, useRef, type RefObject } from "react"
import { createScanDetector } from "@/lib/barcode-scan"

/** Marks a table part that takes scans (the hook's section). */
export const SCAN_TARGET_ATTR = "data-scan-target"

/** Sets a React-controlled input's value as typing would, so its onChange runs. */
function setInputValue(input: HTMLInputElement, text: string) {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set?.call(input, text)
    input.dispatchEvent(new Event("input", { bubbles: true }))
}

/**
 * Barcode scanner input for a table part (a warehouse keeps its hands on the
 * scanner, not the mouse). A scanner types a code in a burst ended by Enter
 * (lib/barcode-scan); the hook catches it anywhere on the page, takes the
 * scanned characters back out of whatever field they landed in, stops the
 * Enter, and hands the code over. Of several parts on a page the one holding
 * focus takes the scan, else the first visible one.
 */
export function useBarcodeScan(section: RefObject<HTMLElement | null>, enabled: boolean, onScan: (code: string) => void) {
    const handler = useRef(onScan)
    useEffect(() => { handler.current = onScan })

    useEffect(() => {
        if (!enabled) return
        const detector = createScanDetector()
        const isTarget = () => {
            const own = section.current
            if (!own || own.offsetParent === null) return false
            const active = document.activeElement
            const focused = active?.closest(`[${SCAN_TARGET_ATTR}]`)
            if (focused) return focused === own
            const first = [...document.querySelectorAll<HTMLElement>(`[${SCAN_TARGET_ATTR}]`)].find((element) => element.offsetParent !== null)
            return first === own
        }
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.metaKey || event.ctrlKey || event.altKey) {
                detector.reset()
                return
            }
            const typed = detector.pending()
            const code = detector.push(event.key, event.timeStamp)
            if (!code || !isTarget()) return
            // The scan's Enter neither submits nor opens anything.
            event.preventDefault()
            event.stopPropagation()
            const active = document.activeElement
            if (active instanceof HTMLInputElement && typed && active.value.endsWith(typed)) {
                setInputValue(active, active.value.slice(0, -typed.length))
            }
            handler.current(code)
        }
        document.addEventListener("keydown", onKeyDown, true)
        return () => document.removeEventListener("keydown", onKeyDown, true)
    }, [enabled, section])
}
