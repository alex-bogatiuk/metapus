"use client"

import { type FormEvent, useEffect, useMemo, useState } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import useSWR from "swr"
import { toast } from "sonner"
import { Plus, RefreshCw } from "lucide-react"

import { api } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { usePermissions } from "@/hooks/use-permissions"
import { isSystemAdmin, useAuthStore } from "@/stores/useAuthStore"
import { ReferenceField } from "@/components/shared/reference-field"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import type { FinancialAdjustmentSourceResponse } from "@/types/generated/http-contracts"

const statusLabels: Record<string, string> = {
  pending_approval: "Ожидает согласования",
  posted: "Проведена",
  rejected: "Отклонена",
}

const reasonLabels: Record<string, string> = {
  incorrect_payment_posting: "Ошибка проведения платежа",
  duplicate_financial_effect: "Повторный учёт платежа",
  payment_other: "Другая ошибка учёта платежа",
  wallet_receipt: "Начисление по кошельку",
  wallet_expense: "Списание по кошельку",
  wallet_other: "Другая корректировка кошелька",
}
const registerLabels: Record<string, string> = {
  suspense: "Невыясненное поступление", merchant: "Обязательство мерчанту", fee: "Комиссия",
}
const validAmount = (value: string) => /^[1-9][0-9]{0,77}$/.test(value)
const lineKey = (line: FinancialAdjustmentSourceResponse) => `${line.register}:${line.lineId}`

export default function CryptoAdjustmentsPage() {
  const router = useRouter()
  const permissions = usePermissions()
  const user = useAuthStore((state) => state.user)
  const canRead = permissions.has("crypto_reconciliation:read")
  const canPropose = permissions.has("crypto_reconciliation:classify")
  const canDirect = isSystemAdmin(user) || permissions.has("crypto_adjustment:post_direct")
  const [status, setStatus] = useState("")
  const [filterPaymentId, setFilterPaymentId] = useState("")
  const [createOpen, setCreateOpen] = useState(false)
  const [mode, setMode] = useState<"payment_reversal" | "wallet_balance">("payment_reversal")
  const [paymentId, setPaymentId] = useState("")
  const [walletId, setWalletId] = useState("")
  const [tokenId, setTokenId] = useState("")
  const [direction, setDirection] = useState<"receipt" | "expense">("receipt")
  const [amount, setAmount] = useState("")
  const [scope, setScope] = useState<"full" | "partial">("full")
  const [selected, setSelected] = useState<Record<string, string>>({})
  const [reasonCode, setReasonCode] = useState("")
  const [evidence, setEvidence] = useState("")
  const [saving, setSaving] = useState(false)
  const { data, error, isLoading, mutate } = useSWR(
    canRead ? ["financial-adjustments", status, filterPaymentId] : null,
    () => api.financialAdjustments.list(status, filterPaymentId),
  )
  const { data: sources, error: sourcesError, isLoading: sourcesLoading } = useSWR(
    createOpen && mode === "payment_reversal" && /^[0-9a-f-]{36}$/i.test(paymentId)
      ? ["adjustment-payment-sources", paymentId] : null,
    () => api.financialAdjustments.paymentSources(paymentId),
  )
  useEffect(() => {
    const basis = new URLSearchParams(window.location.search).get("paymentId")
    if (basis) { setPaymentId(basis); setFilterPaymentId(basis); setMode("payment_reversal"); setCreateOpen(true) }
  }, [])
  const totals = useMemo(() => {
    const result: Record<string, bigint> = { suspense: 0n, merchant: 0n, fee: 0n }
    for (const line of sources?.lines ?? []) {
      const value = scope === "full" ? line.available : selected[lineKey(line)]
      if (value && validAmount(value)) result[line.register] += BigInt(value)
    }
    return result
  }, [sources, scope, selected])
  const balanced = totals.suspense > 0n && totals.suspense === totals.merchant + totals.fee

  async function propose(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!reasonCode || evidence.trim().length < 10) {
      toast.error("Выберите причину и опишите основание не короче 10 символов")
      return
    }
    if (mode === "payment_reversal") {
      if (!sources || !balanced) { toast.error("Сумма кошелька должна равняться обязательству и комиссии"); return }
      for (const line of sources.lines) {
        const value = selected[lineKey(line)]
        if (scope === "partial" && value && (!validAmount(value) || BigInt(value) > BigInt(line.available))) {
          toast.error("Сумма сторно превышает доступный остаток строки"); return
        }
      }
    } else if (!walletId || !tokenId || !validAmount(amount)) {
      toast.error("Выберите кошелёк, токен и положительную сумму в минимальных единицах")
      return
    }
    setSaving(true)
    try {
      const adjustment = await api.financialAdjustments.propose({
        mode,
        ...(mode === "payment_reversal"
          ? { paymentId, expectedPostedVersion: sources?.postedVersion, sourceLines: scope === "partial" ? sources?.lines.filter((line) => selected[lineKey(line)])
              .map((line) => ({ register: line.register, lineId: line.lineId, amount: selected[lineKey(line)] })) : undefined }
          : { walletId, tokenId, direction, amount }),
        reasonCode, evidence: { description: evidence.trim() },
      })
      setCreateOpen(false)
      toast.success(adjustment.status === "posted" ? "Корректировка проведена" : "Корректировка передана на согласование")
      router.push(`/documents/crypto-adjustments/${adjustment.id}`)
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : "Не удалось создать корректировку")
    } finally {
      setSaving(false)
    }
  }

  return (
    <main className="mx-auto flex min-h-0 w-full max-w-[1400px] flex-1 flex-col gap-5 overflow-y-auto p-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">Криптокорректировки</h1>
          <p className="text-sm text-muted-foreground">
            Сторно движений платежа или начисление и списание по кошельку. Факты блокчейна не изменяются.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => void mutate()} disabled={!canRead}>
            <RefreshCw className="mr-2 h-4 w-4" />Обновить
          </Button>
          {canPropose && <Button onClick={() => setCreateOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />Создать корректировку
          </Button>}
        </div>
      </header>

      {!permissions.loading && !canRead ? <p>Нет права на просмотр криптокорректировок.</p> : <>
        <div className="flex items-center gap-3">
          <Label htmlFor="adjustment-status">Статус</Label>
          <select id="adjustment-status" value={status} onChange={(event) => setStatus(event.target.value)}
            className="h-9 rounded-md border border-input bg-background px-3 text-sm">
            <option value="">Все</option>
            <option value="pending_approval">Ожидают согласования</option>
            <option value="posted">Проведены</option>
            <option value="rejected">Отклонены</option>
          </select>
          {filterPaymentId && <Button size="sm" variant="ghost" onClick={() => setFilterPaymentId("")}>Показать все корректировки</Button>}
        </div>
        {error && <p role="alert" className="text-sm text-destructive">Не удалось загрузить корректировки: {String(error)}</p>}
        <Table>
          <TableHeader><TableRow>
            <TableHead>Создана</TableHead><TableHead>Сценарий и основание</TableHead>
            <TableHead>Причина</TableHead><TableHead>Статус</TableHead><TableHead>Автор</TableHead>
          </TableRow></TableHeader>
          <TableBody>
            {data?.items.map((item) => <TableRow key={item.id}>
              <TableCell>{formatDateTime(item.createdAt)}</TableCell>
              <TableCell><Link className="text-primary underline-offset-4 hover:underline" href={`/documents/crypto-adjustments/${item.id}`}>
                {item.mode === "wallet_balance" ? `${item.direction === "receipt" ? "Начисление" : "Списание"} · ${item.amount} · ${item.walletId}` : `Сторно платежа · ${item.paymentId}`}
              </Link></TableCell>
              <TableCell>{reasonLabels[item.reasonCode] ?? item.reasonCode}</TableCell>
              <TableCell>{statusLabels[item.status] ?? item.status}</TableCell>
              <TableCell className="font-mono text-xs">{item.makerId}</TableCell>
            </TableRow>)}
            {!isLoading && !error && data?.items.length === 0 && <TableRow>
              <TableCell colSpan={5} className="py-10 text-center text-muted-foreground">Корректировок пока нет</TableCell>
            </TableRow>}
          </TableBody>
        </Table>
        {isLoading && <p className="text-sm text-muted-foreground">Загрузка…</p>}
      </>}

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-h-[90vh] max-w-3xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Новая криптокорректировка</DialogTitle>
            <DialogDescription>После подтверждения входа {canDirect ? "корректировка будет сразу проведена" : "корректировка пойдёт на согласование другому пользователю"}.</DialogDescription>
          </DialogHeader>
          <form onSubmit={(event) => void propose(event)} className="space-y-4">
            <fieldset className="space-y-2"><legend className="text-sm font-medium">Что нужно скорректировать?</legend>
              <div className="flex flex-wrap gap-4 text-sm">
                <label className="flex items-center gap-2"><input type="radio" checked={mode === "payment_reversal"} onChange={() => { setMode("payment_reversal"); setReasonCode("") }} />Сторно платежа</label>
                <label className="flex items-center gap-2"><input type="radio" checked={mode === "wallet_balance"} onChange={() => { setMode("wallet_balance"); setReasonCode("") }} />Начисление / списание по кошельку</label>
              </div>
            </fieldset>
            {mode === "payment_reversal" ? <div className="space-y-4">
              <div className="space-y-2"><Label>Криптоплатёж</Label>
                <ReferenceField value={paymentId} onChange={(id) => { setPaymentId(id); setSelected({}) }} apiEndpoint="/document/crypto-payment" placeholder="Найти платёж" /></div>
              {sourcesLoading && <p className="text-sm text-muted-foreground">Загрузка движений…</p>}
              {sourcesError && <p role="alert" className="text-sm text-destructive">Не удалось загрузить движения: {String(sourcesError)}</p>}
              {sources && <><p className="text-sm text-muted-foreground">Актуальная версия проведения {sources.postedVersion} определена автоматически.</p>
                <div className="flex gap-4 text-sm"><label className="flex items-center gap-2"><input type="radio" checked={scope === "full"} onChange={() => setScope("full")} />Всё доступное</label>
                  <label className="flex items-center gap-2"><input type="radio" checked={scope === "partial"} onChange={() => setScope("partial")} />Часть движений</label></div>
                {scope === "partial" && <div className="max-h-60 overflow-auto rounded-md border"><Table><TableHeader><TableRow>
                  <TableHead>Движение</TableHead><TableHead>Исходно</TableHead><TableHead>Уже сторнировано</TableHead><TableHead>Доступно</TableHead><TableHead>Сторнировать</TableHead>
                </TableRow></TableHeader><TableBody>{sources.lines.map((line) => <TableRow key={lineKey(line)}>
                  <TableCell className="text-xs">{registerLabels[line.register]}<br />{line.dimension}</TableCell>
                  <TableCell>{line.amount}</TableCell><TableCell>{line.reversed}</TableCell><TableCell>{line.available}</TableCell>
                  <TableCell><Input aria-label={`Сумма сторно ${registerLabels[line.register]} ${line.dimension}`} inputMode="numeric" placeholder="0" value={selected[lineKey(line)] ?? ""}
                    onChange={(event) => setSelected((current) => ({ ...current, [lineKey(line)]: event.target.value }))} disabled={line.available === "0"} /></TableCell>
                </TableRow>)}</TableBody></Table></div>}
                <p className={balanced ? "text-sm text-muted-foreground" : "text-sm text-destructive"}>
                  Невыясненные: {totals.suspense.toString()} · Мерчант: {totals.merchant.toString()} · Комиссия: {totals.fee.toString()}
                  {!balanced && " — суммы должны сходиться"}
                </p><p className="text-xs text-muted-foreground">Суммы указаны в минимальных единицах токена.</p></>}
            </div> : <div className="space-y-4">
              <div className="space-y-2"><Label>Кошелёк</Label><ReferenceField value={walletId} onChange={(id) => setWalletId(id)} apiEndpoint="/catalog/wallets" placeholder="Найти кошелёк" /></div>
              <div className="space-y-2"><Label>Токен</Label><ReferenceField value={tokenId} onChange={(id) => setTokenId(id)} apiEndpoint="/catalog/tokens" placeholder="Найти токен" /></div>
              <div className="space-y-2"><Label htmlFor="adjustment-direction">Операция</Label><select id="adjustment-direction" value={direction} onChange={(event) => { setDirection(event.target.value as "receipt" | "expense"); setReasonCode("") }} className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"><option value="receipt">Начислить</option><option value="expense">Списать</option></select></div>
              <div className="space-y-2"><Label htmlFor="adjustment-amount">Сумма в минимальных единицах токена</Label><Input id="adjustment-amount" inputMode="numeric" value={amount} onChange={(event) => setAmount(event.target.value)} placeholder="1000000" required /></div>
              <p className="text-xs text-muted-foreground">Меняется только учётный остаток кошелька. Факт в блокчейне не изменяется; сверка может показать расхождение.</p>
            </div>}
            <div className="space-y-2"><Label htmlFor="adjustment-reason">Причина</Label><select id="adjustment-reason" value={reasonCode} onChange={(event) => setReasonCode(event.target.value)} className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" required>
              <option value="">Выберите причину</option>{(mode === "payment_reversal" ? ["incorrect_payment_posting", "duplicate_financial_effect", "payment_other"] : [direction === "receipt" ? "wallet_receipt" : "wallet_expense", "wallet_other"]).map((code) => <option key={code} value={code}>{reasonLabels[code]}</option>)}
            </select></div>
            <div className="space-y-2"><Label htmlFor="adjustment-evidence">Обоснование</Label>
              <Textarea id="adjustment-evidence" value={evidence} onChange={(event) => setEvidence(event.target.value)} rows={4} required /></div>
            <DialogFooter><Button type="button" variant="outline" onClick={() => setCreateOpen(false)}>Отмена</Button>
              <Button type="submit" disabled={saving}>{saving ? "Сохранение…" : canDirect ? "Подтвердить вход и провести" : "Отправить на согласование"}</Button></DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </main>
  )
}
