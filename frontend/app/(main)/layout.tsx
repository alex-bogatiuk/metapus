"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { AppShell } from "@/components/layout/app-shell"
import { useAuthStore } from "@/stores/useAuthStore"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { ThemeProvider } from "@/components/theme-provider"
import { isPortalOnly } from "@/lib/access"
import { api } from "@/lib/api"
import { useAuthSessionBootstrap } from "@/hooks/useAuthSessionBootstrap"
import { SessionRecovery } from "@/components/auth/session-recovery"

export default function MainLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const router = useRouter()
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const generation = useAuthStore((s) => s.generation)
  const tokens = useAuthStore((s) => s.tokens)
  const setUser = useAuthStore((s) => s.setUser)
  const [hydrated, setHydrated] = useState(false)
  const prefsLoaded = useUserPrefsStore((s) => s.isLoaded)
  const loadPreferences = useUserPrefsStore((s) => s.loadPreferences)
  const { ready: authReady, error: sessionError, retry: retrySession } = useAuthSessionBootstrap({
    hydrated,
    isAuthenticated,
    accessToken: tokens?.accessToken,
  })

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setHydrated(true)
  }, [])

  const user = useAuthStore((s) => s.user)

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
    if (hydrated && authReady && !isAuthenticated) {
      router.replace("/login")
    }
  }, [authReady, hydrated, isAuthenticated, router])

  // Redirect portal-only users away from ERP layout.
  const portalOnly = isPortalOnly(user)
  useEffect(() => {
    if (hydrated && authReady && isAuthenticated && portalOnly) {
      router.replace("/portal")
    }
  }, [authReady, hydrated, isAuthenticated, portalOnly, router])

  // Load user preferences from server once authenticated
  useEffect(() => {
    if (hydrated && authReady && isAuthenticated && !prefsLoaded) {
      loadPreferences()
    }
  }, [authReady, hydrated, isAuthenticated, prefsLoaded, loadPreferences, generation])

  // Synchronous render gate: never show ERP shell to portal-only merchants.
  // The useEffect above handles the actual redirect; this prevents any flash.
  if (sessionError) return <SessionRecovery onRetry={retrySession} />

  if (!hydrated || !authReady || !isAuthenticated || !user || portalOnly) {
    return null
  }

  return (
    <ThemeProvider>
      <AppShell key={generation}>{children}</AppShell>
    </ThemeProvider>
  )
}
