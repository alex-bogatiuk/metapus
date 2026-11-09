"use client"

import { useState } from "react"
import { RefreshCw } from "lucide-react"
import { toast } from "sonner"
import { AMLAssessment, AMLRiskBadge } from "@/components/crypto/aml-risk"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { api, ApiError } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { usePortalStore } from "@/stores/usePortalStore"
import type { PortalDepositAddressItem, PortalRotateDepositAddressRequest, PortalRotateDepositAddressResponse } from "@/types/portal-api"

const blockedReasons: Record<string, string> = {
  unused_address: "Неиспользованный адрес можно заменить только после первого подтверждённого пополнения. Это защищает пул от исчерпания.",
  sweep_in_progress: "Дождитесь завершения свипа, затем обновите список адресов.",
  operation_in_progress: "Дождитесь завершения текущего платежа или свипа.",
  rotation_cooldown: "Адреса одного клиента можно заменять не чаще одного раза в 24 часа.",
  merchant_rotation_limit: "Достигнут суточный лимит замен адресов для мерчанта.",
  retired: "Этот адрес уже выведен из обращения. Обновите список.",
}

interface DepositAddressRiskProps { item: PortalDepositAddressItem }

export function DepositAddressRisk({ item }: DepositAddressRiskProps) {
  return <Dialog>
    <DialogTrigger asChild>
      <Button variant="ghost" size="sm" aria-label={`AML-риск адреса ${item.customerId}`} onClick={event => event.stopPropagation()}>
        <AMLRiskBadge riskClass={item.riskClass} enabled={item.riskEnabled} expiresAt={item.riskExpiresAt} compact />
      </Button>
    </DialogTrigger>
    <DialogContent onClick={event => event.stopPropagation()}>
      <DialogHeader>
        <DialogTitle>AML-риск депозитного адреса</DialogTitle>
        <DialogDescription>Оценка относится к средствам и данным последней проверки. Новый адрес не гарантирует отсутствие ограничений на стороне бирж.</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-4">
        <p className="break-all font-mono text-sm">{item.address}</p>
        {item.riskAssets.length === 0 ? <p className="text-sm text-muted-foreground">{item.riskEnabled ? "Оценка ещё не получена." : "AML-проверка для этого адреса не включена."}</p> : item.riskAssets.map(asset =>
          <div key={asset.tokenId} className="flex flex-col gap-2">
            <p className="font-medium">{asset.symbol}</p>
            <AMLRiskBadge riskClass={asset.assessment.riskClass} expiresAt={asset.assessment.expiresAt} />
            <AMLAssessment value={asset.assessment} />
          </div>
        )}
        {item.riskClass === "high" && <Alert variant="destructive"><AlertDescription>Высокий риск. Рекомендуется заменить адрес для дальнейших пополнений. Ограничения на прежние средства сохраняются.</AlertDescription></Alert>}
      </div>
    </DialogContent>
  </Dialog>
}

interface DepositAddressRotationActionProps {
  item: PortalDepositAddressItem
  merchantId: string
  onRotated: (result: PortalRotateDepositAddressResponse) => void
}

export function DepositAddressRotationAction({ item, merchantId, onRotated }: DepositAddressRotationActionProps) {
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState<PortalRotateDepositAddressRequest["reason"]>(item.riskClass === "high" ? "aml_high_risk" : "customer_request")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const explanation = blockedReasons[item.rotationBlockedReason] ?? "Заменить адрес клиента"

  async function rotate() {
    setSaving(true)
    setError(null)
    try {
      const result = await api.portal.depositAddresses.rotate(merchantId, item.id, { reason })
      if (usePortalStore.getState().activeMerchantId !== merchantId) return
      setOpen(false)
      toast.success("Адрес заменён. Старый адрес выведен из обращения.")
      onRotated(result)
    } catch (cause) {
      if (usePortalStore.getState().activeMerchantId !== merchantId) return
      const reasonCode = cause instanceof ApiError ? cause.parsedBody?.details?.reason : undefined
      const message = typeof reasonCode === "string" ? blockedReasons[reasonCode] : undefined
      setError(message ?? (cause instanceof ApiError && cause.parsedBody?.code === "WALLET_POOL_EXHAUSTED"
        ? "Свободных адресов пока нет. Повторите попытку после пополнения пула; текущий адрес сохранён."
        : "Не удалось подтвердить замену. Повторите запрос: повторная попытка безопасна."))
    } finally { setSaving(false) }
  }

  return <>
    <TooltipProvider><Tooltip><TooltipTrigger asChild>
      <span tabIndex={item.canRotate ? undefined : 0}>
        <Button variant={item.riskClass === "high" ? "destructive" : "outline"} size="sm" disabled={!item.canRotate}
          onClick={() => { setError(null); setOpen(true) }}>
          <RefreshCw data-icon="inline-start" />Заменить
        </Button>
      </span>
    </TooltipTrigger><TooltipContent><p>{explanation}</p>{item.rotationBlockedReason === "rotation_cooldown" && item.nextRotationAt && <p>Доступно после {formatDateTime(item.nextRotationAt)}</p>}</TooltipContent></Tooltip></TooltipProvider>
    <Dialog open={open} onOpenChange={value => { if (!saving) setOpen(value) }}>
      <DialogContent>
        <DialogHeader><DialogTitle>Заменить депозитный адрес?</DialogTitle>
          <DialogDescription>Клиент {item.customerId} получит новый адрес в сети {item.networkName || item.networkCode}. Повторная замена будет доступна через 24 часа.</DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field><FieldLabel>Причина замены</FieldLabel>
            <ToggleGroup type="single" variant="outline" value={reason} disabled={saving} onValueChange={value => {
              if (value === "aml_high_risk" || value === "customer_request") setReason(value)
            }} aria-label="Причина замены">
              <ToggleGroupItem value="aml_high_risk">Высокий риск AML</ToggleGroupItem>
              <ToggleGroupItem value="customer_request">Запрос клиента</ToggleGroupItem>
            </ToggleGroup>
          </Field>
        </FieldGroup>
        <Alert><AlertDescription>Старый адрес останется в истории клиента и больше не будет выдаваться. Поступления на него потребуют ручного разбора и не будут автоматически зачислены.</AlertDescription></Alert>
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        <DialogFooter><Button variant="outline" disabled={saving} onClick={() => setOpen(false)}>Отмена</Button><Button disabled={saving} onClick={rotate}>{saving ? "Замена…" : "Подтвердить замену"}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  </>
}
