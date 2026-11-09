"use client"

import { useEffect, useState, type FormEvent } from "react"
import Image from "next/image"
import { useRouter } from "next/navigation"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import {
  AlertCircle,
  ArrowLeft,
  AtSign,
  Check,
  Copy,
  Eye,
  EyeOff,
  KeyRound,
  Loader2,
  Lock,
  ShieldCheck,
} from "lucide-react"

import { OtpInput } from "@/components/auth/otp-input"
import { MFAEnrollmentExpiry } from "@/components/auth/mfa-enrollment-expiry"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { isPortalOnly } from "@/lib/access"
import { api, ApiError } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useAuthStore } from "@/stores/useAuthStore"
import type { AuthLoginResponse, LoginResponse, MFAChallengeResponse, MFAEnrollStartResponse } from "@/types/auth"

const CODE_LENGTH = 6
const TOTP_PERIOD = 30

const loginSchema = z.object({
  email: z
    .string()
    .min(1, "Электронная почта не указана")
    .email("Некорректный адрес электронной почты"),
  password: z
    .string()
    .min(1, "Пароль не указан"),
})

type LoginFormValues = z.infer<typeof loginSchema>

export function LoginForm() {
  const router = useRouter()
  const setAuth = useAuthStore((state) => state.setAuth)
  const [showPassword, setShowPassword] = useState(false)
  const [serverError, setServerError] = useState<string | null>(null)
  const [challenge, setChallenge] = useState<MFAChallengeResponse | null>(null)
  const [enrollment, setEnrollment] = useState<MFAEnrollStartResponse | null>(null)
  const [totpCode, setTotpCode] = useState("")
  const [backupCode, setBackupCode] = useState("")
  const [showBackupCode, setShowBackupCode] = useState(false)
  const [mfaSubmitting, setMfaSubmitting] = useState(false)
  const [backupCodes, setBackupCodes] = useState<string[] | null>(null)
  const [secondsLeft, setSecondsLeft] = useState(TOTP_PERIOD)

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<LoginFormValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: {
      email: "",
      password: "",
    },
  })

  useEffect(() => {
    if (!challenge || showBackupCode) return

    function tick() {
      setSecondsLeft(TOTP_PERIOD - (Math.floor(Date.now() / 1000) % TOTP_PERIOD))
    }

    tick()
    const intervalId = window.setInterval(tick, 1000)

    return () => window.clearInterval(intervalId)
  }, [challenge, showBackupCode])

  const progress = (secondsLeft / TOTP_PERIOD) * 100
  const isMfaScreen = Boolean(challenge || backupCodes)
  const isBackupMode = showBackupCode && !enrollment
  const canSubmitMFA = isBackupMode ? backupCode.trim().length > 0 : totpCode.length === CODE_LENGTH

  function completeLogin(response: LoginResponse) {
    setAuth(response.tokens, response.user)
    if (isPortalOnly(response.user)) {
      router.push("/portal")
    } else {
      router.push("/")
    }
  }

  async function startEnrollment(nextChallenge: MFAChallengeResponse) {
    const setup = await api.auth.startMFAEnrollment({ challengeId: nextChallenge.challengeId })
    setEnrollment(setup)
    setChallenge(nextChallenge)
    setTotpCode("")
    setBackupCode("")
    setShowBackupCode(false)
  }

  async function onSubmit(values: LoginFormValues) {
    setServerError(null)

    try {
      const response: AuthLoginResponse = await api.auth.login({
        email: values.email,
        password: values.password,
      })

      if ("tokens" in response) {
        completeLogin(response)
        return
      }

      setChallenge(response)
      if (response.status === "mfa_enrollment_required") {
        await startEnrollment(response)
      }
    } catch (error) {
      if (error instanceof ApiError) {
        if (error.status === 401) {
          setServerError("Неверный адрес электронной почты или пароль")
        } else if (error.status === 403) {
          setServerError("Учётная запись заблокирована. Обратитесь к администратору")
        } else if (error.status === 404) {
          setServerError(error.parsedBody?.message || "Организация (тенант) не найдена")
        } else {
          setServerError(error.parsedBody?.message || "Произошёл сбой на сервере. Попробуйте позже")
        }
      } else {
        setServerError("Не удалось подключиться к серверу")
      }
    }
  }

  async function verifyMFA(code = totpCode) {
    if (!challenge) return
    setMfaSubmitting(true)
    setServerError(null)

    try {
      const response = isBackupMode
        ? await api.auth.useMFABackupCode({ challengeId: challenge.challengeId, backupCode: backupCode.trim() })
        : await api.auth.verifyMFA({ challengeId: challenge.challengeId, code })
      completeLogin(response)
    } catch (error) {
      if (error instanceof ApiError) {
        setServerError(error.parsedBody?.code === "MFA_LOCKED"
          ? "Проверка MFA временно заблокирована"
          : "Код не подтверждён")
      } else {
        setServerError("Не удалось подтвердить вход")
      }

      if (isBackupMode) {
        setBackupCode("")
      } else {
        setTotpCode("")
      }
    } finally {
      setMfaSubmitting(false)
    }
  }

  async function confirmEnrollment(code = totpCode) {
    if (!challenge) return
    setMfaSubmitting(true)
    setServerError(null)

    try {
      const response = await api.auth.confirmMFAEnrollment({
        challengeId: challenge.challengeId,
        code,
      })
      setAuth(response.tokens, response.user)
      setBackupCodes(response.backupCodes)
    } catch {
      setServerError("Код не подтверждён")
      setTotpCode("")
    } finally {
      setMfaSubmitting(false)
    }
  }

  function continueAfterBackupCodes() {
    const user = useAuthStore.getState().user
    if (isPortalOnly(user)) {
      router.push("/portal")
    } else {
      router.push("/")
    }
  }

  function resetChallenge() {
    setChallenge(null)
    setEnrollment(null)
    setServerError(null)
    setTotpCode("")
    setBackupCode("")
    setShowBackupCode(false)
  }

  function handleTotpChange(value: string) {
    if (serverError) {
      setServerError(null)
    }
    setTotpCode(value)
  }

  function handleBackupCodeChange(value: string) {
    if (serverError) {
      setServerError(null)
    }
    setBackupCode(value)
  }

  function handleTotpComplete(value: string) {
    if (!challenge || mfaSubmitting || isBackupMode) return

    if (enrollment) {
      void confirmEnrollment(value)
    } else {
      void verifyMFA(value)
    }
  }

  function handleMfaSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canSubmitMFA || mfaSubmitting) return

    if (enrollment) {
      void confirmEnrollment()
    } else {
      void verifyMFA()
    }
  }

  function copyText(text: string) {
    void navigator.clipboard?.writeText(text)
  }

  return (
    <div className="w-full max-w-[440px]">
      <Card className="overflow-hidden rounded-3xl border-border bg-card shadow-[0_1px_2px_rgba(0,0,0,0.04),0_12px_32px_-12px_rgba(0,0,0,0.12)]">
        <CardHeader className="p-6 pb-4 sm:p-9 sm:pb-5">
          <div className="flex flex-col gap-2">
            {backupCodes ? (
              <>
                <div className="flex items-center gap-2 text-success">
                  <Check className="size-4" aria-hidden="true" />
                  <span className="text-xs font-semibold uppercase">MFA подключена</span>
                </div>
                <CardTitle className="text-2xl">Сохраните резервные коды</CardTitle>
                <CardDescription className="text-[15px] leading-relaxed">
                  Они показываются один раз и нужны для восстановления входа.
                </CardDescription>
              </>
            ) : isMfaScreen ? (
              <>
                <div className="flex items-center gap-2 text-primary">
                  <ShieldCheck className="size-4" aria-hidden="true" />
                  <span className="text-xs font-semibold uppercase">
                    {enrollment ? "Настройка MFA" : "Двухфакторная защита"}
                  </span>
                </div>
                <CardTitle className="text-2xl">
                  {enrollment ? "Подключите приложение" : "Подтвердите, что это вы"}
                </CardTitle>
                <CardDescription className="text-[15px] leading-relaxed">
                  {enrollment
                    ? "Отсканируйте QR-код, затем введите шестизначный код из приложения."
                    : "Введите шестизначный код из приложения-аутентификатора для аккаунта Metapus."}
                </CardDescription>
              </>
            ) : (
              <>
                <CardTitle className="text-pretty text-2xl font-semibold tracking-tight text-foreground">
                  Вход в аккаунт
                </CardTitle>
                <CardDescription className="text-[15px] leading-relaxed">
                  Продолжите работу в Metapus. Введите email и пароль от вашего аккаунта.
                </CardDescription>
              </>
            )}
          </div>
        </CardHeader>

        {backupCodes ? (
          <>
            <CardContent className="flex flex-col gap-4 px-6 pb-6 sm:px-8">
              <Alert>
                <AlertCircle />
                <AlertDescription>
                  Каждый код можно использовать только один раз. После продолжения список будет недоступен.
                </AlertDescription>
              </Alert>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {backupCodes.map((code) => (
                  <div
                    key={code}
                    className="rounded-lg border bg-muted px-3 py-2 font-mono text-sm tabular-nums"
                  >
                    {code}
                  </div>
                ))}
              </div>
            </CardContent>
            <CardFooter className="flex gap-2 px-6 pb-6 sm:px-8 sm:pb-8">
              <Button type="button" variant="outline" className="flex-1" onClick={() => copyText(backupCodes.join("\n"))}>
                <Copy data-icon="inline-start" />
                Копировать
              </Button>
              <Button type="button" className="flex-1" onClick={continueAfterBackupCodes}>
                Продолжить
              </Button>
            </CardFooter>
          </>
        ) : challenge ? (
          <form onSubmit={handleMfaSubmit}>
            <CardContent className="flex flex-col gap-5 px-6 pb-5 sm:px-8">
              {serverError && (
                <Alert variant="destructive">
                  <AlertCircle />
                  <AlertDescription>{serverError}</AlertDescription>
                </Alert>
              )}

              {enrollment && (
                <>
                  <div className="flex flex-col items-center gap-3">
                    <Image
                      src={enrollment.qrCodeDataUrl}
                      alt="QR-код для подключения MFA"
                      width={192}
                      height={192}
                      unoptimized
                      className="size-48 rounded-lg border bg-white p-2"
                    />
                    <div className="flex w-full items-center gap-2 rounded-lg border bg-muted px-3 py-2 font-mono text-xs">
                      <span className="min-w-0 flex-1 break-all">{enrollment.manualSecret}</span>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        onClick={() => copyText(enrollment.manualSecret)}
                        aria-label="Скопировать секрет"
                      >
                        <Copy />
                      </Button>
                    </div>
                  </div>
                  <MFAEnrollmentExpiry expiresAt={enrollment.expiresAt} />
                </>
              )}

              {isBackupMode ? (
                <div className="flex flex-col gap-2">
                  <Label htmlFor="backupCode">Резервный код</Label>
                  <Input
                    id="backupCode"
                    value={backupCode}
                    onChange={(event) => handleBackupCodeChange(event.target.value)}
                    autoComplete="one-time-code"
                    autoFocus
                    aria-invalid={Boolean(serverError)}
                    className="h-11 font-mono text-base"
                  />
                </div>
              ) : (
                <div className="flex flex-col gap-3">
                  <OtpInput
                    value={totpCode}
                    onChange={handleTotpChange}
                    length={CODE_LENGTH}
                    disabled={mfaSubmitting}
                    hasError={Boolean(serverError)}
                    autoFocus
                    onComplete={handleTotpComplete}
                  />
                  <div className="flex min-h-5 items-center justify-between text-xs text-muted-foreground">
                    <span className={cn(serverError && "text-destructive")}>
                      {serverError ? "Введите новый код" : "Новый код через " + secondsLeft + " с"}
                    </span>
                    {!serverError && (
                      <span className="relative inline-flex size-4 items-center justify-center" aria-hidden="true">
                        <svg className="size-4 -rotate-90" viewBox="0 0 20 20">
                          <circle cx="10" cy="10" r="8" fill="none" stroke="currentColor" strokeWidth="3" className="text-border" />
                          <circle
                            cx="10"
                            cy="10"
                            r="8"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth="3"
                            strokeLinecap="round"
                            className="text-primary transition-[stroke-dashoffset] duration-1000 ease-linear"
                            strokeDasharray={2 * Math.PI * 8}
                            strokeDashoffset={2 * Math.PI * 8 * (1 - progress / 100)}
                          />
                        </svg>
                      </span>
                    )}
                  </div>
                </div>
              )}
            </CardContent>

            <CardFooter className="flex flex-col gap-2 px-6 pb-6 sm:px-8 sm:pb-8">
              <Button type="submit" size="lg" className="w-full" disabled={!canSubmitMFA || mfaSubmitting}>
                {mfaSubmitting && <Loader2 data-icon="inline-start" className="animate-spin" />}
                {enrollment ? "Подключить MFA" : "Подтвердить"}
              </Button>
              {!enrollment && (
                <Button
                  type="button"
                  variant="ghost"
                  size="lg"
                  className="w-full text-muted-foreground"
                  onClick={() => {
                    setShowBackupCode((previous) => !previous)
                    setServerError(null)
                    setTotpCode("")
                    setBackupCode("")
                  }}
                  disabled={mfaSubmitting}
                >
                  <KeyRound data-icon="inline-start" />
                  {isBackupMode ? "Ввести код TOTP" : "Войти с резервным кодом"}
                </Button>
              )}
            </CardFooter>
          </form>
        ) : (
          <form onSubmit={handleSubmit(onSubmit)}>
            <CardContent className="flex flex-col gap-4 px-6 pb-5 sm:px-9">
              {serverError && (
                <Alert variant="destructive">
                  <AlertCircle />
                  <AlertDescription>{serverError}</AlertDescription>
                </Alert>
              )}

              <div className="flex flex-col gap-2">
                <Label htmlFor="email">Email</Label>
                <div className="relative">
                  <AtSign
                    className="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
                    aria-hidden="true"
                  />
                  <Input
                    id="email"
                    type="email"
                    autoComplete="email"
                    autoFocus
                    aria-invalid={Boolean(errors.email)}
                    placeholder="you@company.com"
                    className={cn(
                      "h-11 rounded-xl bg-background pl-10 pr-3.5 text-[15px] placeholder:text-muted-foreground/60",
                      errors.email && "border-destructive focus-visible:border-destructive focus-visible:ring-destructive/25"
                    )}
                    {...register("email")}
                  />
                </div>
                {errors.email && (
                  <p className="text-xs text-destructive">{errors.email.message}</p>
                )}
              </div>

              <div className="flex flex-col gap-2">
                <Label htmlFor="password">Пароль</Label>
                <div className="relative">
                  <Lock
                    className="pointer-events-none absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
                    aria-hidden="true"
                  />
                  <Input
                    id="password"
                    type={showPassword ? "text" : "password"}
                    autoComplete="current-password"
                    aria-invalid={Boolean(errors.password)}
                    placeholder="Введите пароль"
                    className={cn(
                      "h-11 rounded-xl bg-background pl-10 pr-11 text-[15px] placeholder:text-muted-foreground/60",
                      errors.password && "border-destructive focus-visible:border-destructive focus-visible:ring-destructive/25"
                    )}
                    {...register("password")}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    tabIndex={-1}
                    className="absolute right-1.5 top-1/2 size-8 -translate-y-1/2 rounded-lg text-muted-foreground hover:text-foreground"
                    onClick={() => setShowPassword((previous) => !previous)}
                    aria-label={showPassword ? "Скрыть пароль" : "Показать пароль"}
                  >
                    {showPassword ? <EyeOff /> : <Eye />}
                  </Button>
                </div>
                {errors.password && (
                  <p className="text-xs text-destructive">{errors.password.message}</p>
                )}
              </div>
            </CardContent>

            <CardFooter className="px-6 pb-6 sm:px-9 sm:pb-9">
              <Button type="submit" size="lg" className="h-11 w-full text-[15px]" disabled={isSubmitting}>
                {isSubmitting && <Loader2 data-icon="inline-start" className="animate-spin" />}
                Войти
              </Button>
            </CardFooter>
          </form>
        )}
      </Card>

      <div className="mt-5 flex items-center justify-center">
        {challenge && !backupCodes ? (
          <Button type="button" variant="ghost" size="sm" className="text-muted-foreground" onClick={resetChallenge} disabled={mfaSubmitting}>
            <ArrowLeft data-icon="inline-start" />
            Вернуться ко входу
          </Button>
        ) : (
          <p className="text-center text-xs leading-5 text-muted-foreground">
            Нет доступа? Обратитесь к администратору системы.
          </p>
        )}
      </div>
    </div>
  )
}
