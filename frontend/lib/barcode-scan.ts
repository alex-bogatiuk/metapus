/**
 * Telling a barcode scanner from a person: a scanner types a whole code in a
 * burst (a few milliseconds per key) and ends it with Enter; people type
 * slower and unevenly. A burst of at least MIN_LENGTH printable keys, each
 * within MAX_GAP of the one before, ended by Enter, is a scan.
 */
export const SCAN_MAX_GAP_MS = 40
export const SCAN_MIN_LENGTH = 4

export interface ScanDetector {
    /**
     * Feeds a key with its time. Returns the scanned code when Enter ends a
     * burst, else null. A slow key starts a new burst.
     */
    push: (key: string, time: number) => string | null
    /** The keys of the current burst (what a scan typed so far). */
    pending: () => string
    reset: () => void
}

export function createScanDetector(maxGap = SCAN_MAX_GAP_MS, minLength = SCAN_MIN_LENGTH): ScanDetector {
    let buffer = ""
    let last = -Infinity
    return {
        push(key, time) {
            const quick = time - last <= maxGap
            last = time
            if (key === "Enter") {
                const code = quick && buffer.length >= minLength ? buffer : null
                buffer = ""
                return code
            }
            if (key.length !== 1) {
                buffer = ""
                return null
            }
            buffer = quick ? buffer + key : key
            return null
        },
        pending: () => buffer,
        reset() {
            buffer = ""
            last = -Infinity
        },
    }
}
