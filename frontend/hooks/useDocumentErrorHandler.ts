import { useState, useCallback } from "react"
import { toast } from "sonner"

// ── Types ───────────────────────────────────────────────────────────────

interface UseDocumentErrorHandlerReturn {
  /** Current error message (banner), null when no error. */
  error: string | null
  /** Set error directly (for validation messages, etc). */
  setError: (msg: string | null) => void
  /** Map of field-level errors from backend (e.g. INVALID_REFERENCE). */
  fieldErrors: Record<string, string>
  /** Set field errors directly (accepts value or functional updater). */
  setFieldErrors: React.Dispatch<React.SetStateAction<Record<string, string>>>
  /**
   * Handle an error from a catch block.
   * Automatically detects INVALID_REFERENCE errors and extracts field names.
   *
   * @param err          — the caught error
   * @param fallbackMsg  — default message if error is not an Error instance
   * @param useToast     — if true, show via toast instead of banner (for post/unpost)
   */
  handleError: (err: unknown, fallbackMsg?: string, useToast?: boolean) => void
  /** Clear both banner error and field errors (call before save attempt). */
  clearErrors: () => void
}

interface ApiError { parsedBody?: { code?: string; details?: { field?: string }; [key: string]: unknown }; message?: string }

// ── Hook ────────────────────────────────────────────────────────────────

/**
 * Generic hook for document form error handling.
 *
 * Encapsulates the common INVALID_REFERENCE / generic error detection
 * pattern, eliminating duplicated catch blocks across document pages.
 *
 * Usage:
 * ```tsx
 * const { error, fieldErrors, handleError, clearErrors } = useDocumentErrorHandler()
 *
 * // In a save handler:
 * try {
 *   clearErrors()
 *   await api.goodsReceipts.update(id, payload)
 * } catch (err) {
 *   handleError(err, "Ошибка сохранения")
 * }
 *
 * // For post/unpost (toast-based):
 * } catch (err) {
 *   handleError(err, "Ошибка проведения", true)
 * }
 * ```
 */
export function useDocumentErrorHandler(): UseDocumentErrorHandlerReturn {
  const [error, setError] = useState<string | null>(null)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})

  const handleError = useCallback((err: unknown, fallbackMsg = "Ошибка", useToast = false) => {
    const apiErr = err as ApiError | undefined
    if (apiErr?.parsedBody?.code === "INVALID_REFERENCE" && apiErr.parsedBody.details?.field) {
      const fieldName = apiErr.parsedBody.details.field
      const msg = apiErr.message || (err instanceof Error ? err.message : fallbackMsg)
      setFieldErrors({ [fieldName]: msg })
      const bannerMsg = `Ошибка в поле: ${fieldName}`
      if (useToast) {
        toast.error(bannerMsg)
      } else {
        setError(bannerMsg)
      }
    } else {
      const msg = err instanceof Error ? err.message : fallbackMsg
      if (useToast) {
        toast.error(msg)
      } else {
        setError(msg)
      }
    }
  }, [])

  const clearErrors = useCallback(() => {
    setError(null)
    setFieldErrors({})
  }, [])

  return { error, setError, fieldErrors, setFieldErrors, handleError, clearErrors }
}
