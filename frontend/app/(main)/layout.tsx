"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { AppShell } from "@/components/layout/app-shell"
import { useAuthStore } from "@/stores/useAuthStore"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { ThemeProvider } from "@/components/theme-provider"

export default function MainLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const router = useRouter()
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const [hydrated, setHydrated] = useState(false)
  const { isLoaded: prefsLoaded, loadPreferences } = useUserPrefsStore()

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setHydrated(true)
  }, [])

  const user = useAuthStore((s) => s.user)

  useEffect(() => {
    if (hydrated && !isAuthenticated) {
      router.replace("/login")
    }
  }, [hydrated, isAuthenticated, router])

  // Redirect portal-only users away from ERP layout.
  useEffect(() => {
    if (!hydrated || !isAuthenticated || !user) return
    const hasErpAccess = user.isAdmin || (user.roles?.length ?? 0) > 0
    const hasPortalAccess = (user.merchantIds?.length ?? 0) > 0
    if (!hasErpAccess && hasPortalAccess) {
      router.replace("/portal")
    }
  }, [hydrated, isAuthenticated, user, router])

  // Load user preferences from server once authenticated
  useEffect(() => {
    if (hydrated && isAuthenticated && !prefsLoaded) {
      loadPreferences()
    }
  }, [hydrated, isAuthenticated, prefsLoaded, loadPreferences])

  if (!hydrated || !isAuthenticated) {
    return null
  }

  return (
    <ThemeProvider>
      <AppShell>{children}</AppShell>
    </ThemeProvider>
  )
}
