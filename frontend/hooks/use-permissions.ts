"use client"
import { useMemo } from "react"
import useSWR from "swr"
import { api } from "@/lib/api"
import { useAuthStore } from "@/stores/useAuthStore"

export function usePermissions() {
  const user = useAuthStore((s) => s.user)
  const result = useSWR(user && !user.isAdmin ? ["permissions", user.id] : null, () =>
    api.auth.permissions()
  )
  const items = result.data?.items
  // Stable while the user and the permission list are unchanged, so callers can memoize on it.
  return useMemo(() => ({
    has: (code: string) => !!user && (user.isAdmin || !!items?.includes(code)),
    loading: result.isLoading,
    error: result.error
  }), [user, items, result.isLoading, result.error])
}
