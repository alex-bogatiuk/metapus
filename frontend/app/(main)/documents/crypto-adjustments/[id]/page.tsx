"use client"

import { use, useState } from "react"
import Link from "next/link"
import useSWR from "swr"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { buildEntityUrlByRoute } from "@/lib/entity-url"
import { formatDateTime } from "@/lib/format"
import { usePermissions } from "@/hooks/use-permissions"
import { useAuthStore } from "@/stores/useAuthStore"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"

const statusLabels: Record<string, string> = {
  pending_approval: "Ожидает согласования",
  posted: "Проведена",
  rejected: "Отклонена",
}
const reasonLabels: Record<string, string> = {
  incorrect_payment_posting: "Ошибка проведения платежа", duplicate_financial_effect: "Повторный учёт платежа",
  payment_other: "Другая ошибка учёта платежа", wallet_receipt: "Начисление по кошельку",
  wallet_expense: "Списание по кошельку", wallet_other: "Другая корректировка кошелька",
}
const registerLabels: Record<string, string> = {
  custody: "Кошелёк", merchant: "Обязательство мерчанту", fee: "Комиссия",
}
const basisLabels: Record<string, string> = {
  legacy: "Историческая корректировка",
  legacy_payment: "Исходные движения платежа без признанного chain effect",
  payment_compensation: "Компенсация признанного поколения платежа",
  wallet_variance: "Расхождение остатка кошелька",
}

export default function CryptoAdjustmentDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params)
  const permissions = usePermissions()
  const user = useAuthStore((state) => state.user)
  const canRead = permissions.has("crypto_reconciliation:read")
  const canResolve = permissions.has("crypto_reconciliation:resolve")
  const { data, error, isLoading, mutate } = useSWR(
    canRead ? ["financial-adjustment", id] : null,
    () => api.financialAdjustments.get(id),
  )
  const [decisionReason, setDecisionReason] = useState("")
  const [saving, setSaving] = useState(false)
  const canDecide = canResolve && data?.status === "pending_approval" && !!user && user.id !== data.makerId

  async function decide(action: "approve" | "reject") {
    if (!canDecide || decisionReason.trim().length < 3) {
      toast.error("Укажите причину решения не короче трёх символов")
      return
    }
    setSaving(true)
    try {
      const updated = action === "approve"
        ? await api.financialAdjustments.approve(id, decisionReason.trim())
        : await api.financialAdjustments.reject(id, decisionReason.trim())
      await mutate(updated, { revalidate: false })
      toast.success(action === "approve" ? "Корректировка проведена" : "Корректировка отклонена")
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : "Не удалось сохранить решение")
    } finally {
      setSaving(false)
    }
  }

  return <main className="mx-auto flex min-h-0 w-full max-w-[1100px] flex-1 flex-col gap-5 overflow-y-auto p-4">
    <Link href="/documents/crypto-adjustments" className="text-sm text-primary hover:underline">← Все корректировки</Link>
    <header>
      <h1 className="text-2xl font-bold">Криптокорректировка</h1>
      <p className="break-all font-mono text-sm text-muted-foreground">{id}</p>
    </header>
    {!permissions.loading && !canRead && <p>Нет права на просмотр криптокорректировок.</p>}
    {error && <p role="alert" className="text-sm text-destructive">Не удалось загрузить корректировку: {String(error)}</p>}
    {isLoading && <p className="text-sm text-muted-foreground">Загрузка…</p>}
    {data && <>
      <section className="grid gap-4 rounded-lg border bg-workspace-surface p-5 sm:grid-cols-2">
        <div><p className="text-xs text-muted-foreground">Статус</p><p className="font-medium">{statusLabels[data.status] ?? data.status}</p></div>
        <div><p className="text-xs text-muted-foreground">Основание учёта</p><p>{basisLabels[data.basisKind] ?? data.basisKind}</p></div>
        {data.sourcePostingId && <div><p className="text-xs text-muted-foreground">Исходное признание перевода</p><p className="break-all font-mono text-sm">{data.sourcePostingId}</p></div>}
        {data.mode === "payment_reversal" ? <>
          <div><p className="text-xs text-muted-foreground">Платёж</p>
            {data.paymentId && <Link className="break-all text-primary hover:underline" href={buildEntityUrlByRoute("crypto-payment", "document", data.paymentId)}>{data.paymentId}</Link>}</div>
          <div><p className="text-xs text-muted-foreground">Версия проведения</p><p>{data.postedVersion}</p></div>
        </> : <>
          <div><p className="text-xs text-muted-foreground">Операция</p><p>{data.direction === "receipt" ? "Начисление" : "Списание"}</p></div>
          <div><p className="text-xs text-muted-foreground">Кошелёк / токен</p><p className="break-all text-sm">{data.walletId} / {data.tokenId}</p></div>
          <div><p className="text-xs text-muted-foreground">Сумма в минимальных единицах</p><p>{data.amount}</p></div>
        </>}
        <div><p className="text-xs text-muted-foreground">Причина</p><p>{reasonLabels[data.reasonCode] ?? data.reasonCode}</p></div>
        <div><p className="text-xs text-muted-foreground">Маршрут</p><p>{data.decisionMode === "direct" ? "Прямое проведение" : "Согласование другим пользователем"}</p></div>
        <div><p className="text-xs text-muted-foreground">Автор</p><p className="break-all font-mono text-sm">{data.makerId}</p></div>
        <div><p className="text-xs text-muted-foreground">Создана</p><p>{formatDateTime(data.createdAt)}</p></div>
        {data.checkerId && <div><p className="text-xs text-muted-foreground">Утвердил / отклонил</p><p className="break-all font-mono text-sm">{data.checkerId}</p></div>}
        {data.decidedAt && <div><p className="text-xs text-muted-foreground">Решение принято</p><p>{formatDateTime(data.decidedAt)}</p></div>}
        {data.decisionReason && <div className="sm:col-span-2"><p className="text-xs text-muted-foreground">Причина решения</p><p>{data.decisionReason}</p></div>}
      </section>
      {data.basisKind === "wallet_variance" && <p className="text-sm text-muted-foreground">
        Эта корректировка учитывает расхождение остатка. Указание хеша транзакции в обосновании не подтверждает перевод.
      </p>}
      {!!data.sourceLines?.length && <section className="space-y-2 rounded-lg border bg-workspace-surface p-5">
        <h2 className="font-semibold">Выбранные движения</h2>
        <ul className="space-y-1 text-sm">{data.sourceLines.map((line) => <li key={`${line.register}:${line.lineId}`}>
          {registerLabels[line.register] ?? line.register}: {line.amount} · <span className="font-mono text-xs">{line.lineId}</span>
        </li>)}</ul>
      </section>}
      <section className="space-y-2 rounded-lg border bg-workspace-surface p-5">
        <h2 className="font-semibold">Обоснование</h2>
        <p className="whitespace-pre-wrap break-words text-sm">{typeof data.evidence === "object" && data.evidence && "description" in data.evidence ? String(data.evidence.description) : JSON.stringify(data.evidence)}</p>
      </section>
      {data.status === "pending_approval" && user?.id === data.makerId &&
        <p className="text-sm text-muted-foreground">Автор предложения не может принять решение по своей корректировке.</p>}
      {canDecide && <section className="space-y-3 rounded-lg border bg-workspace-surface p-5">
        <h2 className="font-semibold">Решение второго оператора</h2>
        <p className="text-sm text-muted-foreground">Утверждение проведёт выбранные движения. Операция необратима.</p>
        <Label htmlFor="adjustment-decision-reason">Причина решения</Label>
        <Textarea id="adjustment-decision-reason" value={decisionReason}
          onChange={(event) => setDecisionReason(event.target.value)} rows={3} />
        <div className="flex gap-2">
          <Button onClick={() => void decide("approve")} disabled={saving || decisionReason.trim().length < 3}>Утвердить и провести</Button>
          <Button variant="outline" onClick={() => void decide("reject")} disabled={saving || decisionReason.trim().length < 3}>Отклонить</Button>
        </div>
      </section>}
    </>}
  </main>
}
