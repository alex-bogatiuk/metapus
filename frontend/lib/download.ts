/** How long a downloaded blob's URL stays valid after the click, ms. */
const URL_LIFETIME = 30_000

/**
 * Saves a blob as a file through a temporary link. The object URL outlives
 * the click: revoking it at once can cancel the download in Firefox and Safari.
 */
export function downloadBlob(blob: Blob, filename: string): void {
    const url = URL.createObjectURL(blob)
    const link = document.createElement("a")
    link.href = url
    link.download = filename
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), URL_LIFETIME)
}
