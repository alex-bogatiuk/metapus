"use client"

import type { ClipboardEvent, SyntheticEvent } from "react"

import { cn } from "@/lib/utils"

interface OtpInputProps {
  value: string
  onChange: (value: string) => void
  length?: number
  disabled?: boolean
  hasError?: boolean
  autoFocus?: boolean
  onComplete?: (value: string) => void
}

/**
 * A one-time code in one text field drawn as separate cells. A single field
 * is what one-time-code autofill (iOS, macOS, password managers), paste and
 * screen readers expect; split fields lose all but the first digit.
 */
export function OtpInput({
  value,
  onChange,
  length = 6,
  disabled = false,
  hasError = false,
  autoFocus = false,
  onComplete,
}: OtpInputProps) {
  const digits = Array.from({ length }, (_, index) => value[index] ?? "")
  // The cell the next digit goes to; the last one once the code is full.
  const activeIndex = Math.min(value.length, length - 1)

  function commit(rawValue: string) {
    const normalized = rawValue.replace(/\D/g, "").slice(0, length)
    onChange(normalized)
    if (normalized.length === length) onComplete?.(normalized)
  }

  // Pasted text may carry spaces or dashes ("123 456"): keep its digits, which
  // maxLength would otherwise cut before they are cleaned.
  function handlePaste(event: ClipboardEvent<HTMLInputElement>) {
    event.preventDefault()
    commit(event.clipboardData.getData("text"))
  }

  // The text itself is invisible, so the caret always stays at its end.
  function keepCaretAtEnd(event: SyntheticEvent<HTMLInputElement>) {
    const input = event.currentTarget
    const end = input.value.length
    if (input.selectionStart !== end || input.selectionEnd !== end) input.setSelectionRange(end, end)
  }

  return (
    <div
      className={cn("group relative", hasError && "animate-shake")}
      data-invalid={hasError || undefined}
    >
      <input
        type="text"
        inputMode="numeric"
        autoComplete="one-time-code"
        pattern={`\\d{${length}}`}
        maxLength={length}
        spellCheck={false}
        value={value}
        disabled={disabled}
        autoFocus={autoFocus}
        aria-label="Код подтверждения"
        aria-invalid={hasError}
        onChange={(event) => commit(event.target.value)}
        onPaste={handlePaste}
        onSelect={keepCaretAtEnd}
        // 16px keeps iOS from zooming in; the text is transparent over the cells.
        className="absolute inset-0 z-10 h-full w-full cursor-text bg-transparent text-base text-transparent caret-transparent outline-none selection:bg-transparent disabled:cursor-not-allowed"
      />
      <div className="flex items-center justify-between gap-2 sm:gap-3" aria-hidden="true">
        {digits.map((digit, index) => (
          <div
            key={index}
            data-active={index === activeIndex || undefined}
            className={cn(
              "relative flex h-14 min-w-0 flex-1 items-center justify-center rounded-xl border border-input bg-card font-mono text-2xl font-semibold tabular-nums transition-colors sm:h-16 sm:text-3xl",
              "group-hover:border-muted-foreground/40",
              "group-focus-within:data-[active]:border-ring group-focus-within:data-[active]:ring-2 group-focus-within:data-[active]:ring-ring/30",
              hasError && "border-destructive text-destructive group-focus-within:data-[active]:border-destructive group-focus-within:data-[active]:ring-destructive/30",
              disabled && "opacity-50"
            )}
          >
            {digit}
            {/* The caret of the empty cell the next digit goes to. */}
            {!digit && index === activeIndex && (
              <span className="hidden h-7 w-0.5 rounded-full bg-primary group-focus-within:block motion-safe:animate-pulse" />
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
