"use client"

import { useId, useState } from "react"
import { Eye, EyeOff, LoaderCircle } from "lucide-react"
import { toast } from "sonner"

import { api, ApiError } from "@/lib/api"
import { usePermissions } from "@/hooks/use-permissions"
import type { CryptoNetworkListResponse, CryptoNetworkState } from "@/types/crypto-network"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from "@/components/ui/field"
import { Textarea } from "@/components/ui/textarea"

interface NetworkWatchingSectionProps {
  state: CryptoNetworkState
  onUpdated: () => Promise<CryptoNetworkListResponse | undefined>
}

interface WatchingCommand { watching: boolean; key: string }

// SQL guards name the condition that keeps the network read; the operator
// sees what to finish first.
const refusals: Record<string, string> = {
  "network watching cannot stop while operations processing is enabled": "Сначала выключите обработку операций и сохраните настройки сети.",
  "network watching cannot stop while outbound transactions await the chain": "В сети есть исходящие транзакции, которые ещё не подтверждены. Дождитесь их завершения.",
  "network watching cannot stop while invoices await payment or finality": "В сети есть счета, ожидающие оплаты или подтверждений. Дождитесь их завершения или истечения.",
}


export function NetworkWatchingSection({ state, onUpdated }: NetworkWatchingSectionProps) {
  const reasonId = useId()
  const { has } = usePermissions()
  const [command, setCommand] = useState<WatchingCommand | null>(null)
  const [reason, setReason] = useState("")
  const [busy, setBusy] = useState(false)
  const [submitError, setSubmitError] = useState("")
  const reasonLength = Array.from(reason.trim()).length
  const invalidReason = reasonLength < 3 || reasonLength > 500
  const code = state.network.code

  const openCommand = () => {
    setReason("")
    setSubmitError("")
    setCommand({ watching: !state.watching, key: crypto.randomUUID() })
  }
  const submit = async () => {
    if (!command || invalidReason || busy) return
    setBusy(true)
    setSubmitError("")
    try {
      await api.cryptoNetworks.setWatching(state.network.id, { watching: command.watching, reason: reason.trim() }, command.key)
      await onUpdated()
      setCommand(null)
      toast.success(command.watching
        ? "Наблюдение возобновлено. Воркер дочитает сеть с последней проверенной точки."
        : "Наблюдение остановлено. Воркер перестанет обращаться к сети в течение минуты.")
    } catch (cause) {
      const refusal = cause instanceof ApiError ? refusals[cause.parsedBody?.message ?? ""] : undefined
      if (refusal) setSubmitError(refusal)
      else if (cause instanceof ApiError && cause.status === 409) setSubmitError("Настройки сети меняются в другом сеансе. Повторите действие.")
      else setSubmitError("Не удалось подтвердить команду. Повторите запрос или обновите состояние.")
    } finally {
      setBusy(false)
    }
  }

  return (
    <FieldSet>
      <FieldLegend>Наблюдение за сетью</FieldLegend>
      <FieldDescription>
        Пока наблюдение включено, воркер читает сеть: входящие переводы, подтверждения отправленных транзакций и сверку балансов.
        Без наблюдения он не обращается к RPC этой сети совсем, а пропущенные переводы прочитает после возобновления.
      </FieldDescription>
      <FieldGroup>
        <div className="flex flex-wrap items-center justify-between gap-3" aria-live="polite">
          <Badge variant={state.watching ? "success" : "secondary"}>{state.watching ? "Сеть читается" : "Сеть не читается"}</Badge>
          {has("crypto_network:update") ? (
            <Button type="button" variant="outline" size="sm" onClick={openCommand} disabled={busy}>
              {state.watching ? <EyeOff data-icon="inline-start" /> : <Eye data-icon="inline-start" />}
              {state.watching ? "Остановить наблюдение" : "Возобновить наблюдение"}
            </Button>
          ) : null}
        </div>
        {state.watching ? (
          <FieldDescription>
            Остановить наблюдение можно при выключенной обработке операций, без неподтверждённых исходящих транзакций и без открытых счетов.
          </FieldDescription>
        ) : null}
      </FieldGroup>
      <Dialog open={command !== null} onOpenChange={(open) => { if (!open && !busy) setCommand(null) }}>
        <DialogContent onEscapeKeyDown={(event) => { if (busy) event.preventDefault() }} onPointerDownOutside={(event) => { if (busy) event.preventDefault() }}>
          <DialogHeader>
            <DialogTitle>{command?.watching ? "Возобновить" : "Остановить"} наблюдение за {code}</DialogTitle>
            <DialogDescription>
              {command?.watching
                ? "Воркер начнёт читать сеть и дочитает переводы, пришедшие за время паузы. Причина и автор будут записаны в аудит."
                : "Воркер перестанет читать сеть: входящие переводы не будут видны до возобновления. Причина и автор будут записаны в аудит."}
            </DialogDescription>
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
