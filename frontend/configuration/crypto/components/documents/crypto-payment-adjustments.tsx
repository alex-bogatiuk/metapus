"use client"

import Link from "next/link"
import useSWR from "swr"

import { usePermissions } from "@/hooks/use-permissions"
import { Button } from "@/components/ui/button"

import { cryptoApi } from "@/configuration/crypto/lib/api"
const reasonLabels: Record<string, string> = {
  incorrect_payment_posting: "Ошибка проведения платежа",
  duplicate_financial_effect: "Повторный учёт платежа",
  payment_other: "Другая ошибка учёта платежа",
}

export function CryptoPaymentAdjustments({ paymentId }: { paymentId: string }) {
  const permissions = usePermissions()
  const canRead = permissions.has("crypto_reconciliation:read")
  const { data } = useSWR(canRead ? ["payment-adjustments", paymentId] : null,
    () => cryptoApi.financialAdjustments.list("", paymentId))
  if (!canRead) return null
  return <section className="rounded-lg border bg-workspace-surface p-4">
    <h2 className="mb-2 font-semibold">Криптокорректировки</h2>
    <p className="mb-3 text-sm text-muted-foreground">Создайте полное или частичное сторно финансовых движений этого платежа. Версия проведения определяется автоматически.</p>
    {permissions.has("crypto_reconciliation:classify") && <Button asChild><Link href={`/documents/crypto-adjustments?paymentId=${encodeURIComponent(paymentId)}`}>
      Создать корректировку на основании платежа
    </Link></Button>}
    {!!data?.items.length && <ul className="mt-3 space-y-1 text-sm">
      {data.items.map((item) => <li key={item.id}><Link className="text-primary hover:underline" href={`/documents/crypto-adjustments/${item.id}`}>
        {item.status === "posted" ? "Проведена" : item.status === "pending_approval" ? "Ожидает согласования" : "Отклонена"} · {reasonLabels[item.reasonCode] ?? item.reasonCode}
      </Link></li>)}
    </ul>}
  </section>
}
