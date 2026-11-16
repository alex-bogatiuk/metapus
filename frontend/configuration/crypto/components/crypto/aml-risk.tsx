"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import useSWR from "swr"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { apiFetch } from "@/lib/api/core"
import type { AmlAssessmentDisplay, CryptoPaymentResponse, WalletResponse } from "@/configuration/crypto/types/generated/http-contracts"

const riskLabels: Record<string, string> = { low: "Низкий · low", medium: "Средний · medium", high: "Высокий · high", unknown: "Не определён · unknown" }
const sources: Record<string, string> = { testnet_bootstrap: "Начальная классификация тестового стенда", testnet_aml: "Эмулятор AML для тестовой сети", provider: "AML-провайдер", inventory: "Оценка состава средств", none: "Нет оценки" }
const reasons: Record<string, string> = {
  assessment_pending: "Ожидает оценки", assessment_expired: "Оценка просрочена", payment_unconfirmed: "Ожидает подтверждения платежа",
  insufficient_evidence: "Недостаточно подтверждённых данных", empty_inventory: "Средств нет", aml_disabled: "AML для актива не включён",
  queued: "В очереди проверки", submitting: "Запрос проверки", polling: "Ожидает ответа", completed: "Проверка завершена", needs_attention: "Проверка требует внимания",
  testnet_bootstrap: "Существующие средства отнесены к low при обновлении стенда",
  known_high_exposure: "Обнаружена связь поступлений с источником высокого риска",
  missing_inventory_coverage: "Проверками покрыты не все поступления",
  no_assessed_contributions: "Оценённых поступлений пока нет",
  conservative_inventory: "Учтены оценки всех поступлений; показан наиболее строгий класс",
  critical_signal: "AML-провайдер выявил критический фактор риска",
  score_band: "Уровень определён по риск-скору провайдера",
}
function useNow() {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])
  return now
}
function effectiveClass(riskClass: string, now: number, expiresAt?: string) {
  return riskClass !== "high" && expiresAt && Date.parse(expiresAt) <= now ? "unknown" : riskClass
}
export function AMLRiskBadge({ riskClass, enabled = true, empty = false, expiresAt, compact = false }: { riskClass: string; enabled?: boolean; empty?: boolean; expiresAt?: string; compact?: boolean }) {
  const value = effectiveClass(riskClass, useNow(), expiresAt)
  if (!enabled) return <Badge variant="outline">AML не включён</Badge>
  if (empty && value !== "high") return <Badge variant="outline">Нет средств</Badge>
  const label = compact ? ({ low: "Низкий", medium: "Средний", high: "Высокий", unknown: "Не оценён" }[value] ?? "Не оценён") : (riskLabels[value] ?? riskLabels.unknown)
  return <Badge variant={value === "high" ? "destructive" : value === "medium" ? "warning" : value === "low" ? "success" : "outline"}>{label}</Badge>
}
function date(value?: string) { return value ? new Date(value).toLocaleString("ru-RU") : "—" }
export function AMLAssessment({ value }: { value: AmlAssessmentDisplay }) {
  const now = useNow()
  const expired = value.riskClass !== "high" && value.expiresAt && Date.parse(value.expiresAt) <= now
  return <div className="space-y-1 text-sm">
    <p>{sources[value.source] ?? "Оценка AML"}</p>
    {reasons[expired ? "assessment_expired" : value.reason] && <p className="text-muted-foreground">{reasons[expired ? "assessment_expired" : value.reason]}</p>}
    {value.scoreBp !== undefined && <p>Риск-скор: {value.scoreBp / 100}%</p>}
    {value.evaluatedAt && <p className="text-muted-foreground">Оценено: {date(value.evaluatedAt)}</p>}
    {value.expiresAt && value.riskClass !== "high" && <p className="text-muted-foreground">Действует до: {date(value.expiresAt)}</p>}
  </div>
}
export function WalletAMLPanel({ walletId }: { walletId: string }) {
  const { data, error } = useSWR(["wallet-aml", walletId], () => apiFetch<WalletResponse>(`/catalog/wallets/${walletId}`), { refreshInterval: 10000 })
  return <Card>
    <CardHeader><CardTitle>Владелец и AML-риск средств</CardTitle><CardDescription>Класс определяется отдельно для каждого актива. Назначение адреса клиенту не меняет владельца средств.</CardDescription></CardHeader>
    <CardContent className="space-y-4">
      {error ? <p role="alert">Не удалось обновить сведения AML. Проверьте соединение и повторите открытие карточки.</p> : !data ? <p role="status">Загрузка сведений AML…</p> : <>
        <p>Владелец: {data.custodyMerchantId ? <Link className="text-primary hover:underline" href={`/catalogs/merchants/${data.custodyMerchantId}`}>{data.custodyMerchant?.name ?? data.custodyMerchantId}</Link> : "Не задан"}</p>
        <p>Назначение: {data.merchant?.name ?? "Не назначен клиенту"}</p>
        {data.depositRetiredAt && <p>Адрес выведен из обращения: {date(data.depositRetiredAt)}. Принадлежность клиенту сохранена; новые поступления требуют ручного разбора.</p>}
        <p>Готовность адреса: {data.allocationHeld ? `Удержан · ${(data.allocationHoldReasons ?? []).map(reason => ({ aml_baseline_pending: "Ожидает исходной сверки", aml_activation_pending: "Ожидает активации", reconciliation: "Требуется сверка", aml_high_risk: "Высокий риск", chain_balance_check_pending: "Ожидает сверки с сетью", aml_scope_baseline_missing: "Нет исходной сверки актива" }[reason] ?? reason)).join(", ")}` : data.chainActivationStatus !== "active" ? ({ unknown: "Ожидает проверки и активации", activating: "Активируется", held: "Активация приостановлена", failed: "Ошибка активации" }[data.chainActivationStatus] ?? "Ожидает активации") : ({ free: "Свободен", leased: "Выдан для приёма", assigned: "Назначен клиенту", sweep_pending: "Ожидает свипа", sweeping: "Выполняется свип", frozen: "Заблокирован" }[data.status] ?? data.status)}</p>
        {!data.riskEnabled ? <p>AML для активов этого кошелька не включён.</p> : <Table>
          <TableHeader><TableRow><TableHead>Актив</TableHead><TableHead>Фактический риск</TableHead><TableHead>Основание оценки</TableHead><TableHead>Приём в партию</TableHead></TableRow></TableHeader>
          <TableBody>{data.riskAssets.map(asset => <TableRow key={asset.tokenId}>
            <TableCell className="font-medium">{asset.symbol}</TableCell>
            <TableCell><AMLRiskBadge riskClass={asset.assessment.riskClass} empty={!asset.hasFunds} expiresAt={asset.assessment.expiresAt} /></TableCell>
            <TableCell>{asset.hasFunds || asset.assessment.riskClass === "high" ? <AMLAssessment value={asset.assessment} /> : "Средств нет; оценка поступлений появится после приёма"}</TableCell>
            <TableCell>{asset.intakeClass ? <div className="space-y-1"><AMLRiskBadge riskClass={asset.intakeClass} /><p className="text-sm text-muted-foreground">{({ open: "Приём открыт", closed: "Приём закрыт", held: "Исполнение приостановлено" })[asset.batchState ?? ""] ?? "Партия готовится"}</p></div> : "—"}</TableCell>
          </TableRow>)}</TableBody>
        </Table>}
      </>}
    </CardContent>
  </Card>
}
export function PaymentAMLPanel({ paymentId }: { paymentId: string }) {
  const { data, error } = useSWR(["payment-aml", paymentId], () => apiFetch<CryptoPaymentResponse>(`/document/crypto-payment/${paymentId}`), { refreshInterval: 10000 })
  return <Card>
    <CardHeader><CardTitle>AML-проверка платежа</CardTitle><CardDescription>Оценка относится к этому поступлению и может измениться после повторной проверки.</CardDescription></CardHeader>
    <CardContent className="space-y-3">
      {error ? <p role="alert">Не удалось обновить оценку AML. Повторите открытие карточки.</p> : !data ? <p role="status">Загрузка оценки AML…</p> : <>
        <AMLRiskBadge riskClass={data.aml.riskClass} enabled={data.aml.enabled} expiresAt={data.aml.expiresAt} />
        <AMLAssessment value={data.aml} />
      </>}
    </CardContent>
  </Card>
}
