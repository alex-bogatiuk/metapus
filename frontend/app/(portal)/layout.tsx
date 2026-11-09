"use client"

import { useEffect, useSyncExternalStore } from "react"
import { useRouter } from "next/navigation"
import { useAuthStore } from "@/stores/useAuthStore"
import { usePortalStore } from "@/stores/usePortalStore"
import { ThemeProvider } from "@/components/theme-provider"
import { PortalShell } from "@/components/portal/portal-shell"
import { api } from "@/lib/api"
import { hasPortalAccess } from "@/lib/access"
import { useAuthSessionBootstrap } from "@/hooks/useAuthSessionBootstrap"
import { SessionRecovery } from "@/components/auth/session-recovery"

const emptySubscribe = () => () => {}

export default function PortalLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const router = useRouter()
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const user = useAuthStore((s) => s.user)
  const generation = useAuthStore((s) => s.generation)
  const tokens = useAuthStore((s) => s.tokens)
  const setUser = useAuthStore((s) => s.setUser)
  const setMerchants = usePortalStore((s) => s.setMerchants)
  const hydrated = useSyncExternalStore(emptySubscribe, () => true, () => false)
  const { ready: authReady, error: sessionError, retry: retrySession } = useAuthSessionBootstrap({
    hydrated,
    isAuthenticated,
    accessToken: tokens?.accessToken,
  })

  // Auth guard: redirect to login if not authenticated
  useEffect(() => {
    if (hydrated && authReady && !isAuthenticated) {
      router.replace("/login")
    }
  }, [authReady, hydrated, isAuthenticated, router])

  // Portal access guard: redirect to / if no merchant access
  const portalAccess = hasPortalAccess(user)
  const merchantIdsCount = user?.merchantIds?.length ?? 0

  useEffect(() => {
    if (!authReady || !isAuthenticated || !tokens?.accessToken) return
    let cancelled = false
    api.auth.me()
      .then((currentUser) => {
        if (!cancelled) setUser(currentUser)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [authReady, isAuthenticated, tokens?.accessToken, setUser])

  useEffect(() => {
    if (hydrated && authReady && isAuthenticated && !portalAccess) {
      router.replace("/")
    }
  }, [authReady, hydrated, isAuthenticated, portalAccess, router])

  // Load available merchants from portal API
  useEffect(() => {
    if (!hydrated || !authReady || !isAuthenticated || !user || merchantIdsCount === 0) return

    api.portal.merchants()
      .then((res) => setMerchants(res.items))
      .catch(() => {
        // Silently fail — merchants will remain empty
      })
  }, [authReady, hydrated, isAuthenticated, merchantIdsCount, setMerchants, generation, user])

  if (sessionError) return <SessionRecovery onRetry={retrySession} />

  if (!hydrated || !authReady || !isAuthenticated || !user || !portalAccess) {
    return null
  }

  return (
    <ThemeProvider>
      <PortalShell key={generation}>{children}</PortalShell>
    </ThemeProvider>
  )
}
