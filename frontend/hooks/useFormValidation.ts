"use client"

import { useState, useCallback, useRef, useEffect } from "react"

// ── Types ───────────────────────────────────────────────────────────────

/** Single field validation rule. */
export interface FieldRule<TState> {
  /** Field key — must match the `data-field` attribute on the DOM element. */
  field: string
  /** Validation function. Return error message or null if valid. */
  validate: (state: TState) => string | null
  /** When to validate. Default: "blur". "submit" means only on form submit. */
  trigger?: "blur" | "submit"
}

interface UseFormValidationOptions<TState> {
  /** Field validation rules. */
  rules: FieldRule<TState>[]
}

interface UseFormValidationReturn<TState> {
  /** Map of field key → error message. Empty = no errors. */
  fieldErrors: Record<string, string>
  /** Set a single field error manually (e.g. from backend response). */
  setFieldError: (field: string, message: string) => void
  /** Clear error for a specific field. */
  clearFieldError: (field: string) => void
  /** Clear all field errors. */
  clearAllErrors: () => void
  /** On-blur handler — validates the blurred field, shows error + shake if invalid. */
  handleBlur: (field: string, state: TState) => void
  /**
   * Run all rules (blur + submit) and scroll to the first error.
   * Returns true if validation passed, false otherwise.
   */
  validateAll: (state: TState) => boolean
  /**
   * Generate `data-field` and `onBlur` props for a form field.
   * Usage: <Input {...fieldProps("name", formState)} />
   */
  fieldProps: (field: string, state: TState) => {
    "data-field": string
    onBlur: () => void
  }
}

// ── Helpers ─────────────────────────────────────────────────────────────

/** Apply shake animation to the DOM element with matching `data-field`. */
function shakeField(field: string): void {
  const el = document.querySelector<HTMLElement>(`[data-field="${field}"]`)
  if (!el) return

  // Remove class first to allow re-triggering on repeated failures
  el.classList.remove("animate-shake")
  // Force reflow so the animation restarts
  void el.offsetWidth
  el.classList.add("animate-shake")
  // Clean up after animation completes
  el.addEventListener("animationend", () => {
    el.classList.remove("animate-shake")
  }, { once: true })
}

/** Scroll the first error field into view smoothly. */
function scrollToField(field: string): void {
  const el = document.querySelector<HTMLElement>(`[data-field="${field}"]`)
  if (!el) return

  el.scrollIntoView({ behavior: "smooth", block: "center" })

  // Focus the input inside the field container (if any)
  requestAnimationFrame(() => {
    const input = el.querySelector<HTMLElement>("input, textarea, select, [tabindex]")
    if (input) input.focus()
  })
}

// ── Hook ────────────────────────────────────────────────────────────────

/**
 * Universal on-blur field validation with shake animation and scroll-to-error (M15).
 *
 * Features:
 * - Per-field on-blur validation with declarative rules
 * - Shake animation on error (uses CSS `.animate-shake`)
 * - `validateAll()` for submit-time validation with auto-scroll to first error
 * - `fieldProps()` helper for zero-boilerplate wiring
 *
 * Usage:
 * ```tsx
 * const { fieldErrors, fieldProps, validateAll } = useFormValidation({
 *   rules: [
 *     { field: "name", validate: (s) => s.name.trim() ? null : "Наименование обязательно" },
 *     { field: "quantity", validate: (s) => s.quantity > 0 ? null : "Количество должно быть больше 0" },
 *   ],
 * })
 *
 * // In JSX:
 * <Input {...fieldProps("name", formState)} value={formState.name} />
 * {fieldErrors.name && <p className="text-destructive text-xs">{fieldErrors.name}</p>}
 *
 * // On submit:
 * if (!validateAll(formState)) return
 * ```
 */
export function useFormValidation<TState>(
  options: UseFormValidationOptions<TState>,
): UseFormValidationReturn<TState> {
  const { rules } = options
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const rulesRef = useRef(rules)
  useEffect(() => {
    rulesRef.current = rules
  })

  const setFieldError = useCallback((field: string, message: string) => {
    setFieldErrors((prev) => ({ ...prev, [field]: message }))
  }, [])

  const clearFieldError = useCallback((field: string) => {
    setFieldErrors((prev) => {
      const next = { ...prev }
      delete next[field]
      return next
    })
  }, [])

  const clearAllErrors = useCallback(() => {
    setFieldErrors({})
  }, [])

  const handleBlur = useCallback((field: string, state: TState) => {
    const rule = rulesRef.current.find((r) => r.field === field && r.trigger !== "submit")
    if (!rule) return

    const error = rule.validate(state)
    if (error) {
      setFieldErrors((prev) => ({ ...prev, [field]: error }))
      shakeField(field)
    } else {
      // Clear error when field becomes valid
      setFieldErrors((prev) => {
        const next = { ...prev }
        delete next[field]
        return next
      })
    }
  }, [])

  const validateAll = useCallback((state: TState): boolean => {
    const errors: Record<string, string> = {}
    let firstErrorField: string | null = null

    for (const rule of rulesRef.current) {
      const error = rule.validate(state)
      if (error) {
        errors[rule.field] = error
        if (!firstErrorField) firstErrorField = rule.field
      }
    }

    setFieldErrors(errors)

    if (firstErrorField) {
      shakeField(firstErrorField)
      scrollToField(firstErrorField)
      return false
    }

    return true
  }, [])

  const fieldPropsFactory = useCallback(
    (field: string, state: TState) => ({
      "data-field": field,
      onBlur: () => handleBlur(field, state),
    }),
    [handleBlur],
  )

  return {
    fieldErrors,
    setFieldError,
    clearFieldError,
    clearAllErrors,
    handleBlur,
    validateAll,
    fieldProps: fieldPropsFactory,
  }
}
