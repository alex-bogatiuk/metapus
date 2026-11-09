"use client"
import { api } from "@/lib/api"
import { useWidgetData } from "./useWidgetData"

export function useAdminNetworks(options: { isEditMode: boolean; enabled?: boolean }) {
  return useWidgetData(async () => ({ ...await api.cryptoNetworks.list(), evaluatedAt: Date.now() }), {
    ...options, key: "widget:admin-networks", pollInterval: 60_000,
  })
}
