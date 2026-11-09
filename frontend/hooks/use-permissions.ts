"use client"
import useSWR from "swr"
import { api } from "@/lib/api"
import { useAuthStore } from "@/stores/useAuthStore"

export function usePermissions() {
  const user = useAuthStore((s) => s.user)
  const result = useSWR(user && !user.isAdmin ? ["permissions", user.id] : null, () =>
    api.auth.permissions()
  )
  return {
    has: (code: string) => !!user && (user.isAdmin || !!result.data?.items.includes(code)),
    loading: result.isLoading,
    error: result.error
  }
}
