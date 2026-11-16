"use client"

import { useId, useState } from "react"
import useSWR from "swr"
import { LoaderCircle, Pause, Play, RefreshCw } from "lucide-react"
import { toast } from "sonner"

import { api, ApiError } from "@/lib/api"
import { isSystemAdmin, useAuthStore } from "@/stores/useAuthStore"
import { usePermissions } from "@/hooks/use-permissions"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from "@/components/ui/field"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { formatTimestamp } from "./types-and-helpers"

interface ShadowControlSectionProps { networkId: string; networkCode: string }
interface ShadowCommand { paused: boolean; version: number; key: string }

const statusLabels: Record<string, string> = {
  financial: "Финансовая обработка",
  unavailable: "Недоступна",
  running: "Включена",
  paused: "Приостановлена",
  pausing: "Приостанавливается",
  starting: "Запускается",
  worker_unavailable: "Нет свежего состояния воркера",
}

export function ShadowControlSection({ networkId, networkCode }: ShadowControlSectionProps) {
  const reasonId = useId()
  const systemAdmin = useAuthStore((state) => isSystemAdmin(state.user))
  const { has } = usePermissions()
  const { data, error, isLoading, isValidating, mutate } = useSWR(
    ["admin-shadow-control", networkId], () => api.cryptoNetworks.shadowStatus(networkId),
    { refreshInterval: 5_000, revalidateOnFocus: true },
  )
  const [command, setCommand] = useState<ShadowCommand | null>(null)
  const [reason, setReason] = useState("")
  const [busy, setBusy] = useState(false)
  const [submitError, setSubmitError] = useState("")
  const reasonLength = Array.from(reason.trim()).length
  const invalidReason = reasonLength < 3 || reasonLength > 500

  const openCommand = () => {
    if (!data) return
    setReason("")
    setSubmitError("")
    setCommand({ paused: !data.paused, version: data.version, key: crypto.randomUUID() })
  }
  const submit = async () => {
    if (!command || invalidReason || busy) return
    setBusy(true)
    setSubmitError("")
    try {
      const updated = await api.cryptoNetworks.setShadowPaused(networkId,
        { paused: command.paused, version: command.version, reason: reason.trim() }, command.key)
      await mutate(updated, { revalidate: false })
      setCommand(null)
      toast.success("Команда принята. Статус обновится после применения воркером.")
    } catch (cause) {
      await mutate().catch(() => undefined)
      if (cause instanceof ApiError && (cause.status === 409 || cause.status === 400)) {
        setCommand(null)
        toast.error("Состояние сети изменилось. Проверьте текущий режим и повторите действие.")
      } else {
        setSubmitError("Не удалось подтвердить команду. Повторите запрос или обновите состояние.")
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <FieldSet>
      <FieldLegend>Теневая проверка блоков</FieldLegend>
      <FieldDescription>Проверка блочного обработчика до финансовой активации сети.</FieldDescription>
      {isLoading ? <Skeleton className="h-16 w-full" /> : error || !data ? (
        <Alert variant="destructive">
          <AlertTitle>Статус проверки недоступен</AlertTitle>
          <AlertDescription>
            <span>Обновите состояние, чтобы проверить доступность управления.</span>
            <Button type="button" variant="outline" size="sm" disabled={isValidating} onClick={() => void mutate()}>
              <RefreshCw data-icon="inline-start" />Обновить
            </Button>
          </AlertDescription>
        </Alert>
      ) : (
        <FieldGroup>
          <div className="flex flex-wrap items-center justify-between gap-3" aria-live="polite">
            <Badge variant={data.status === "running" ? "success" : data.status === "pausing" || data.status === "starting" ? "warning" : "secondary"}>
              {statusLabels[data.status] ?? "Состояние неизвестно"}
            </Badge>
            {data.canChange && systemAdmin && has("crypto_network:update") ? (
              <Button type="button" variant="outline" size="sm" onClick={openCommand}
                disabled={busy || data.status === "pausing" || data.status === "starting"}>
                {data.paused ? <Play data-icon="inline-start" /> : <Pause data-icon="inline-start" />}
                {data.paused ? "Возобновить проверку" : "Приостановить проверку"}
              </Button>
            ) : null}
          </div>
          {data.lockedReason === "financially_activated" ? (
            <Alert>
              <AlertTitle>Финансовая обработка активирована</AlertTitle>
              <AlertDescription>
                <p>Теневая проверка для этой сети недоступна. Управление рабочей обработкой выполняется отдельно.</p>
                <p>Активация: {formatTimestamp(data.activatedAt ?? "")}.</p>
              </AlertDescription>
            </Alert>
          ) : !data.canChange ? (
            <FieldDescription>Сеть должна быть настроена в теневом режиме до финансовой активации.</FieldDescription>
          ) : (
            <>
              <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div><dt>Проверенный блок</dt><dd className="text-right tabular-nums">{data.checkpointHeight?.toLocaleString("ru-RU") ?? "Ещё нет данных"}</dd></div>
                <div><dt>Последний прогресс</dt><dd>{data.lastProgressAt ? formatTimestamp(data.lastProgressAt) : "Проверка ещё не запускалась"}</dd></div>
              </dl>
              <FieldDescription>Приостановка сохраняет проверенную высоту. После возобновления обработчик продолжит с неё и проверит накопившуюся историю.</FieldDescription>
              {data.status === "worker_unavailable" ? <Alert>
                <AlertTitle>Воркер ещё не подтвердил запуск</AlertTitle>
                <AlertDescription>Проверка запустится при доступном воркере. Текущий статус не подтверждает обработку блоков.</AlertDescription>
              </Alert> : null}
              {!data.paused && data.lastErrorCode ? <Alert variant="destructive">
                <AlertTitle>Проверка ожидает продолжения</AlertTitle>
                <AlertDescription>Обработчик зафиксировал проблему чтения или согласования блоков. Проверьте журнал событий.</AlertDescription>
              </Alert> : null}
            </>
          )}
        </FieldGroup>
      )}
      <Dialog open={command !== null} onOpenChange={(open) => { if (!open && !busy) setCommand(null) }}>
        <DialogContent onEscapeKeyDown={(event) => { if (busy) event.preventDefault() }} onPointerDownOutside={(event) => { if (busy) event.preventDefault() }}>
          <DialogHeader>
            <DialogTitle>{command?.paused ? "Приостановить" : "Возобновить"} теневую проверку {networkCode}</DialogTitle>
            <DialogDescription>Изменение применит воркер. Проверенная высота сохранится. Причина и автор будут записаны в аудит.</DialogDescription>
          </DialogHeader>
          <form className="flex flex-col gap-4" onSubmit={(event) => { event.preventDefault(); void submit() }}>
            <FieldGroup>
              <Field data-invalid={reasonLength > 500 || undefined} data-disabled={busy || undefined}>
                <FieldLabel htmlFor={reasonId}>Причина действия</FieldLabel>
                <Textarea id={reasonId} required disabled={busy} value={reason} aria-invalid={reasonLength > 500}
                  onChange={(event) => { setReason(event.target.value); setCommand((current) => current ? { ...current, key: crypto.randomUUID() } : null) }} />
                <FieldDescription>От 3 до 500 символов. {reasonLength} / 500</FieldDescription>
                {submitError ? <FieldError>{submitError}</FieldError> : null}
              </Field>
            </FieldGroup>
            <DialogFooter>
              <Button type="button" variant="outline" disabled={busy} onClick={() => setCommand(null)}>Отмена</Button>
              <Button type="submit" disabled={busy || invalidReason}>
                {busy ? <LoaderCircle data-icon="inline-start" className="animate-spin" /> : null}
                {busy ? "Отправка…" : submitError ? "Повторить запрос" : "Подтвердить"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </FieldSet>
  )
}
