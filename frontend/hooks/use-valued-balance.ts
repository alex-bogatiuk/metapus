"use client"
import { useEffect, useState } from "react"
import useSWR from "swr"
import { api } from "@/lib/api"

export function useValuedBalance(merchantId: string | null, detailed = false) {
  const result = useSWR(
    merchantId ? ["valued-balance", merchantId, detailed] : null,
    () =>
      detailed
        ? api.portal.balanceDetailed(merchantId ?? undefined)
        : api.portal.balance(merchantId ?? undefined),
    { refreshInterval: 60000, keepPreviousData: false }
  )
  const [expiredAt, setExpiredAt] = useState<string | null>(null)
  const until = result.data?.freshUntil
  const { mutate } = result
  useEffect(() => {
    if (!until) return
    const timer = setTimeout(
      () => {
        setExpiredAt(until)
        void mutate().catch(() => undefined)
      },
      Math.max(0, new Date(until).getTime() - Date.now())
    )
    return () => clearTimeout(timer)
  }, [until, mutate])
  return { ...result, expired: !!until && expiredAt === until }
}
