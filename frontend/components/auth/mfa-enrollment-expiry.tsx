"use client"

import { useEffect, useState } from "react"
import { AlertCircle, Clock3 } from "lucide-react"

import { Alert, AlertDescription } from "@/components/ui/alert"

interface MFAEnrollmentExpiryProps {
  expiresAt: string
}

function getRemainingSeconds(expiresAt: string) {
  const expiresAtMs = Date.parse(expiresAt)
  if (!Number.isFinite(expiresAtMs)) return 0

  return Math.max(0, Math.ceil((expiresAtMs - Date.now()) / 1000))
}

function formatRemainingTime(totalSeconds: number) {
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60

  return `${minutes}:${seconds.toString().padStart(2, "0")}`
}

export function MFAEnrollmentExpiry({ expiresAt }: MFAEnrollmentExpiryProps) {
  const [remainingSeconds, setRemainingSeconds] = useState<number | null>(null)

  useEffect(() => {
    function tick() {
      setRemainingSeconds(getRemainingSeconds(expiresAt))
    }

    tick()
    const intervalId = window.setInterval(tick, 1000)

    return () => window.clearInterval(intervalId)
  }, [expiresAt])

  const isExpired = remainingSeconds === 0

  return (
    <Alert
      variant={isExpired ? "destructive" : "default"}
      role={isExpired ? "alert" : "status"}
      aria-live={isExpired ? "polite" : "off"}
    >
      {isExpired ? <AlertCircle /> : <Clock3 />}
      <AlertDescription>
        {remainingSeconds === null
          ? "Срок настройки ограничен. После его окончания потребуется новый QR-код."
          : isExpired
            ? "Время настройки истекло. Начните настройку заново и отсканируйте новый QR-код."
            : `Настройка доступна ещё ${formatRemainingTime(remainingSeconds)}. После этого потребуется новый QR-код.`}
      </AlertDescription>
    </Alert>
  )
}
