"use client"

import Link from "next/link"
import useSWR from "swr"
import { apiFetch, assertUuidPathParam } from "@/lib/api/core"
import { usePermissions } from "@/hooks/use-permissions"
import type { CryptoPaymentResponse } from "@/configuration/crypto/types/generated/http-contracts"

/** Link from an invoice payment to the custody receipt it attributes. */
export function CryptoPaymentReceipt({ paymentId }: { paymentId: string }) {
  const permissions = usePermissions()
  const canRead = permissions.has("crypto_receipt:read")
  const { data } = useSWR(canRead ? ["payment-receipt", paymentId] : null,
    () => apiFetch<CryptoPaymentResponse>(`/document/crypto-payment/${assertUuidPathParam(paymentId, "paymentId")}`))
  if (!canRead || !data) return null
  return <section className="rounded-lg border bg-workspace-surface p-4">
    <h2 className="mb-2 font-semibold">Поступление</h2>
    {data.receiptId
      ? <p className="text-sm">
          Платёж распределяет учтённое поступление: <Link className="text-primary hover:underline" href={`/documents/crypto-custody-receipt/${data.receiptId}`}>открыть поступление</Link>.
          {!data.posted && " Распределение сторнировано откатом блока; при повторном включении перевода платёж распределит его снова."}
        </p>
      : <p className="text-sm text-muted-foreground">Поступление учитывается при финальности перевода, тогда платёж зачисляет его мерчанту.</p>}
  </section>
}
