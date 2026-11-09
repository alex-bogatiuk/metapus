"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import { api } from "@/lib/api"

export function useRiskBalance(merchantId: string) {
  const result = useSWR(["risk-balance", merchantId], () => api.portal.riskBalance(merchantId), {
    refreshInterval: 15000, keepPreviousData: false,
  })
  const [expiredAt, setExpiredAt] = useState<string | null>(null)
  const until = result.data?.freshUntil
  const { mutate } = result
  useEffect(() => {
    if (!until) return
    const timer = setTimeout(() => {
      setExpiredAt(until)
      void mutate().catch(() => undefined)
    }, Math.max(0, new Date(until).getTime() - Date.now()))
    return () => clearTimeout(timer)
  }, [until, mutate])
  return { ...result, expired: !!until && expiredAt === until }
}
