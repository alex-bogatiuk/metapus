"use client"

import { useRef, type ClipboardEvent, type KeyboardEvent } from "react"

import { Input } from "@/components/ui/input"
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

export function OtpInput({
  value,
  onChange,
  length = 6,
  disabled = false,
  hasError = false,
  autoFocus = false,
  onComplete,
}: OtpInputProps) {
  const inputsRef = useRef<Array<HTMLInputElement | null>>([])
  const digits = Array.from({ length }, (_, index) => value[index] ?? "")

  function focusIndex(index: number) {
    const clamped = Math.max(0, Math.min(length - 1, index))
    inputsRef.current[clamped]?.focus()
    inputsRef.current[clamped]?.select()
  }

  function commit(nextValue: string) {
    const normalized = nextValue.replace(/\D/g, "").slice(0, length)
    onChange(normalized)

    if (normalized.length === length) {
      onComplete?.(normalized)
    }
  }

  function handleChange(index: number, rawValue: string) {
    const digit = rawValue.replace(/\D/g, "").slice(-1)
    if (!digit) return

    const nextDigits = digits.slice()
    nextDigits[index] = digit
    commit(nextDigits.join(""))

    if (index < length - 1) {
      focusIndex(index + 1)
    }
  }

  function handleKeyDown(index: number, event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Backspace") {
      event.preventDefault()

      const nextDigits = digits.slice()
      if (nextDigits[index]) {
        nextDigits[index] = ""
        commit(nextDigits.join(""))
        return
      }

      if (index > 0) {
        nextDigits[index - 1] = ""
        commit(nextDigits.join(""))
        focusIndex(index - 1)
      }
    }

    if (event.key === "ArrowLeft") {
      event.preventDefault()
      focusIndex(index - 1)
    }

    if (event.key === "ArrowRight") {
      event.preventDefault()
      focusIndex(index + 1)
    }
  }

  function handlePaste(event: ClipboardEvent<HTMLInputElement>) {
    event.preventDefault()
    const pastedValue = event.clipboardData.getData("text")

    if (!pastedValue) return

    commit(pastedValue)
    focusIndex(pastedValue.replace(/\D/g, "").length)
  }

  return (
    <div
      className={cn(
        "flex items-center justify-between gap-2 sm:gap-3",
        hasError && "animate-shake"
      )}
      role="group"
      aria-label="Шестизначный код подтверждения"
      data-invalid={hasError || undefined}
    >
      {digits.map((digit, index) => (
        <Input
          key={index}
          ref={(element) => {
            inputsRef.current[index] = element
          }}
          type="text"
          inputMode="numeric"
          autoComplete={index === 0 ? "one-time-code" : "off"}
          pattern="[0-9]*"
          maxLength={1}
          value={digit}
          disabled={disabled}
          autoFocus={autoFocus && index === 0}
          aria-label={`Цифра ${index + 1}`}
          aria-invalid={hasError}
          onChange={(event) => handleChange(index, event.target.value)}
          onKeyDown={(event) => handleKeyDown(index, event)}
          onPaste={handlePaste}
          onFocus={(event) => event.target.select()}
          className={cn(
            "h-14 min-w-0 rounded-xl bg-card p-0 text-center font-mono text-2xl font-semibold tabular-nums caret-primary sm:h-16 sm:text-3xl",
            "hover:border-muted-foreground/40",
            hasError && "border-destructive text-destructive focus-visible:border-destructive focus-visible:ring-destructive/30"
          )}
        />
      ))}
    </div>
  )
}
