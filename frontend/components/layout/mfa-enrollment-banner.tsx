"use client"

import { useState } from "react"
import Image from "next/image"
import { AlertCircle, Copy, Loader2, ShieldCheck } from "lucide-react"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api } from "@/lib/api"
import { useAuthStore } from "@/stores/useAuthStore"
import type { MFAEnrollStartResponse } from "@/types/auth"

export function MFAEnrollmentBanner() {
  const user = useAuthStore((s) => s.user)
  const setAuth = useAuthStore((s) => s.setAuth)
  const [setup, setSetup] = useState<MFAEnrollStartResponse | null>(null)
  const [code, setCode] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  if (!user?.mfaPending || user.mfaEnabled) return null

  async function startEnrollment() {
    setLoading(true)
    setError(null)
    try {
      const response = await api.auth.startSessionMFAEnrollment()
      setSetup(response)
      setCode("")
    } catch {
      setError("Не удалось начать настройку MFA")
    } finally {
      setLoading(false)
    }
  }

  async function confirmEnrollment() {
    if (!setup) return
    setLoading(true)
    setError(null)
    try {
      const response = await api.auth.confirmMFAEnrollment({
        challengeId: setup.challengeId,
        code,
      })
      setAuth(response.tokens, response.user)
      closeDialog()
    } catch {
      setError("Код не подтверждён")
    } finally {
      setLoading(false)
    }
  }

  function closeDialog() {
    setSetup(null)
    setCode("")
    setError(null)
  }

  return (
    <>
      <div className="border-b bg-muted/60 px-4 py-2">
        <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
          <div className="flex items-center gap-2">
            <ShieldCheck />
            <span>MFA для admin-сессии ещё не включена.</span>
          </div>
          <Button type="button" size="sm" onClick={startEnrollment} disabled={loading}>
            {loading && <Loader2 data-icon="inline-start" className="animate-spin" />}
            Настроить MFA
          </Button>
        </div>
      </div>

      <Dialog open={!!setup} onOpenChange={(open) => !open && closeDialog()}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Настройка MFA</DialogTitle>
            <DialogDescription>
              Отсканируйте QR-код и подтвердите первый код из приложения.
            </DialogDescription>
          </DialogHeader>

          <div className="flex flex-col gap-4">
            {error && (
              <Alert variant="destructive">
                <AlertCircle />
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}

            {setup ? (
              <>
                <div className="flex flex-col items-center gap-3">
                  <Image
                    src={setup.qrCodeDataUrl}
                    alt="QR code"
                    width={192}
                    height={192}
                    unoptimized
                    className="size-48 rounded-md border bg-white p-2"
                  />
                  <div className="flex w-full items-center gap-2 rounded-md border bg-muted px-3 py-2 font-mono text-xs">
                    <span className="min-w-0 flex-1 break-all">{setup.manualSecret}</span>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      onClick={() => navigator.clipboard?.writeText(setup.manualSecret)}
                      aria-label="Скопировать секрет"
                    >
                      <Copy />
                    </Button>
                  </div>
                </div>

                <div className="flex flex-col gap-2">
                  <Label htmlFor="session-mfa-code">Код TOTP</Label>
                  <Input
                    id="session-mfa-code"
                    value={code}
                    onChange={(event) => setCode(event.target.value)}
                    inputMode="numeric"
                    autoComplete="one-time-code"
                  />
                </div>
              </>
            ) : null}
          </div>

          <DialogFooter className="gap-2">
            <Button type="button" variant="outline" onClick={closeDialog} disabled={loading}>
              Позже
            </Button>
            <Button type="button" onClick={confirmEnrollment} disabled={loading || !code}>
              {loading && <Loader2 data-icon="inline-start" className="animate-spin" />}
              Подтвердить
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
