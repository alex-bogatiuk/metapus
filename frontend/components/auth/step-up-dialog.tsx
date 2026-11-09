"use client"

import { FormEvent, useEffect, useState } from "react"
import Image from "next/image"
import { AlertCircle, Copy, Loader2 } from "lucide-react"

import { MFAEnrollmentExpiry } from "@/components/auth/mfa-enrollment-expiry"
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
import { api, ApiError } from "@/lib/api"
import { STEP_UP_EVENT, type StepUpEventDetail } from "@/lib/step-up"
import { useAuthStore } from "@/stores/useAuthStore"
import type { MFAEnrollStartResponse } from "@/types/auth"

const SESSION_ENROLLMENT_ACTION = "auth.session_mfa.enroll"

export function StepUpDialog() {
  const [pending, setPending] = useState<StepUpEventDetail | null>(null)
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [setup, setSetup] = useState<MFAEnrollStartResponse | null>(null)
  const [backupCodes, setBackupCodes] = useState<string[] | null>(null)
  const [needsEnrollment, setNeedsEnrollment] = useState(false)
  const setAuth = useAuthStore((s) => s.setAuth)

  useEffect(() => {
    const listener = (event: Event) => {
      const detail = (event as CustomEvent<StepUpEventDetail>).detail
      if (!detail) return
      setPending((prev) => {
        prev?.reject(new Error("step-up superseded"))
        return detail
      })
      setPassword("")
      setCode("")
      setError(null)
      setNotice(null)
      setSetup(null)
      setBackupCodes(null)
      setNeedsEnrollment(detail.actionCode === SESSION_ENROLLMENT_ACTION)
    }
    window.addEventListener(STEP_UP_EVENT, listener)
    return () => window.removeEventListener(STEP_UP_EVENT, listener)
  }, [])

  const close = () => {
    if (submitting) return
    pending?.reject(new Error("step-up cancelled"))
    setPending(null)
    setPassword("")
    setCode("")
    setError(null)
    setNotice(null)
    setSetup(null)
    setBackupCodes(null)
    setNeedsEnrollment(false)
  }

  const startEnrollment = async () => {
    setSubmitting(true)
    setError(null)
    setNotice(null)
    try {
      const response = await api.auth.startSessionMFAEnrollment()
      setSetup(response)
      setCode("")
      setBackupCodes(null)
      setNeedsEnrollment(false)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Не удалось начать настройку MFA")
    } finally {
      setSubmitting(false)
    }
  }

  const confirmEnrollment = async () => {
    if (!setup) return
    setSubmitting(true)
    setError(null)
    setNotice(null)
    try {
      const response = await api.auth.confirmMFAEnrollment({
        challengeId: setup.challengeId,
        code,
      })
      setAuth(response.tokens, response.user)
      setBackupCodes(response.backupCodes)
      setSetup(null)
      setCode("")
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Код не подтверждён")
    } finally {
      setSubmitting(false)
    }
  }

  const continueAfterEnrollment = () => {
    if (!pending) return
    setBackupCodes(null)
    setCode("")
    if (pending.actionCode === SESSION_ENROLLMENT_ACTION) {
      pending.resolve()
      setPending(null)
      return
    }
    setNotice("MFA настроена. Введите новый TOTP-код, чтобы подтвердить действие.")
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!pending) return
    if (setup) {
      await confirmEnrollment()
      return
    }
    setSubmitting(true)
    setError(null)
    setNotice(null)
    try {
      await api.auth.stepUp({
        actionCode: pending.actionCode,
        password,
        code,
      })
      pending.resolve()
      setPending(null)
      setPassword("")
      setCode("")
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.parsedBody?.code === "MFA_DENIED" && err.parsedBody.details?.reason === "MFA_NOT_ENROLLED") {
          setNeedsEnrollment(true)
          setError("Для этого действия сначала нужно настроить MFA.")
          return
        }
        setError(err.message || "Проверка не пройдена")
      } else {
        setError("Не удалось подтвердить действие")
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={!!pending} onOpenChange={(open) => !open && close()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{setup || backupCodes || needsEnrollment ? "Настройка MFA" : "Подтверждение действия"}</DialogTitle>
          <DialogDescription>
            {setup
              ? "Отсканируйте QR-код и подтвердите первый код из приложения."
              : backupCodes
                ? "Сохраните резервные коды. Они больше не будут показаны."
                : needsEnrollment
                  ? "Для продолжения нужно подключить приложение-аутентификатор."
                  : "Для продолжения нужны пароль и код из приложения-аутентификатора."}
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={submit} className="flex flex-col gap-4">
          {notice && (
            <Alert>
              <AlertCircle />
              <AlertDescription>{notice}</AlertDescription>
            </Alert>
          )}
          {error && (
            <Alert variant="destructive">
              <AlertCircle />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          {backupCodes ? (
            <div className="grid grid-cols-1 gap-2 font-mono text-sm">
              {backupCodes.map((backupCode) => (
                <div key={backupCode} className="rounded-md border bg-muted px-3 py-2">
                  {backupCode}
                </div>
              ))}
            </div>
          ) : setup ? (
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

              <MFAEnrollmentExpiry expiresAt={setup.expiresAt} />

              <div className="flex flex-col gap-2">
                <Label htmlFor="step-up-enroll-code">Код TOTP</Label>
                <Input
                  id="step-up-enroll-code"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  value={code}
                  onChange={(event) => setCode(event.target.value)}
                  disabled={submitting}
                  required
                />
              </div>
            </>
          ) : needsEnrollment ? null : (
            <>
              <div className="flex flex-col gap-2">
                <Label htmlFor="step-up-password">Пароль</Label>
                <Input
                  id="step-up-password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  disabled={submitting}
                  required
                />
              </div>

              <div className="flex flex-col gap-2">
                <Label htmlFor="step-up-code">Код TOTP</Label>
                <Input
                  id="step-up-code"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  value={code}
                  onChange={(event) => setCode(event.target.value)}
                  disabled={submitting}
                  required
                />
              </div>
            </>
          )}

          <DialogFooter className="gap-2">
            <Button type="button" variant="outline" onClick={close} disabled={submitting}>
              Отмена
            </Button>
            {backupCodes ? (
              <Button type="button" onClick={continueAfterEnrollment} disabled={submitting}>
                Продолжить
              </Button>
            ) : needsEnrollment ? (
              <Button type="button" onClick={startEnrollment} disabled={submitting}>
                {submitting && <Loader2 data-icon="inline-start" className="animate-spin" />}
                Настроить MFA
              </Button>
            ) : (
            <Button type="submit" disabled={submitting}>
              {submitting && <Loader2 data-icon="inline-start" className="animate-spin" />}
              {setup ? "Подключить" : "Подтвердить"}
            </Button>
            )}
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
