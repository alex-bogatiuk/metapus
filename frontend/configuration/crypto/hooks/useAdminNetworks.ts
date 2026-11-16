"use client"

import { useWidgetData } from "@/hooks/useWidgetData"

import { cryptoApi } from "@/configuration/crypto/lib/api"
export function useAdminNetworks(options: { isEditMode: boolean; enabled?: boolean }) {
  return useWidgetData(async () => ({ ...await cryptoApi.cryptoNetworks.list(), evaluatedAt: Date.now() }), {
    ...options, key: "widget:admin-networks", pollInterval: 60_000,
  })
}
